package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/testenv"
)

// Follow one ordinary binary and physical project through every delivered
// operation. Expected bytes and scalar positions are fixture literals, not
// values obtained from the correction provider or another command's output.
func TestCompiledCLIReadinessJourney(t *testing.T) {
	// Not parallel: t.Setenv changes the whole test process environment.
	testenv.Clear(t)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	gitHome, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(gitHome, "absent-gitconfig"))
	binary := sharedExecutable(t)
	root := configurationProject(t)
	const original = "# 雪\r\nπ\tend"
	const candidate = "# 雪\r\nπ\tend\r\n"
	const suppressed = "<!-- mdchk:disable MD047 -->\r\n雪"
	const protected = "    protected 雪"
	putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
	putConfigurationFile(t, root, "doc.md", original)
	putConfigurationFile(t, root, "suppressed.md", suppressed)
	putConfigurationFile(t, root, "protected.md", protected)
	// Permission is inherited independently of the selected child settings.
	putConfigurationFile(t, root, "restricted/.mdchk.yaml", md047Configuration(true, false))
	putConfigurationFile(t, root, "restricted/child/.mdchk.yaml", md047Configuration(true, true))
	putConfigurationFile(t, root, "restricted/child/blocked.md", suppressed)
	putConfigurationFile(t, root, "nested/.mdchk.yaml", md047Configuration(false, true))
	putConfigurationFile(t, root, "nested/doc.md", "nested")
	before := readinessState(t, root)
	expected := before
	readOnly := func(cwd string, args ...string) configurationRun {
		t.Helper()
		got := runConfigurationProcess(t, binary, cwd, args...)
		readinessAssertEqual(t, strings.Join(args, " "), expected, readinessState(t, root))
		return got
	}
	run := func(status int, operation string, args ...string) result.Result {
		t.Helper()
		got := readOnly(root, args...)
		if got.Status != status {
			t.Fatalf("%v: status %d want %d: %s", args, got.Status, status, got.Stdout)
		}
		return validateConfigurationOperationJSON(t, []byte(got.Stdout), operation)
	}
	scan := run(0, "config validate", "config", "validate", ".", "--format=json")
	if !scan.Complete || scan.ConfigValidation == nil || scan.ConfigValidation.DefinitionsChecked != 4 || scan.ConfigValidation.InvalidDefinitions != 0 || !reflect.DeepEqual(scan.CheckedFiles, []string{".mdchk.yaml", "nested/.mdchk.yaml", "restricted/.mdchk.yaml", "restricted/child/.mdchk.yaml"}) || len(scan.Diagnostics) != 0 {
		t.Fatal(scan)
	}
	lint := run(10, "lint", "lint", "doc.md", "suppressed.md", "protected.md", "--format=json")
	if !lint.Complete || lint.Status != "violations" || len(lint.Diagnostics) != 2 || len(lint.SuppressedDiagnostics) != 1 {
		t.Fatal(lint)
	}
	d := readinessDiagnostic(t, lint, "doc.md")
	if d.Rule != "MD047" || d.Range.Start.Line != 2 || d.Range.Start.Column != 6 || d.ByteRange.Start != len(original) || d.ByteRange.End != len(original) || !d.FixAvailable {
		t.Fatal(d)
	}
	readinessSnapshot(t, d.Snapshot, "original", original)
	var hidden result.SuppressedDiagnostic
	if err := json.Unmarshal(lint.SuppressedDiagnostics[0], &hidden); err != nil {
		t.Fatal(err)
	}
	readinessSnapshot(t, hidden.Diagnostic.Snapshot, "original", suppressed)
	if hidden.Diagnostic.Path != "suppressed.md" || hidden.Diagnostic.Range.Start.Line != 2 || hidden.Diagnostic.Range.Start.Column != 2 {
		t.Fatal(hidden)
	}
	metadata := readOnly(root, "--version")
	if metadata.Status != 0 || !strings.HasPrefix(metadata.Stdout, "mdchk ") {
		t.Fatal(metadata)
	}
	version := strings.TrimPrefix(strings.TrimSpace(metadata.Stdout), "mdchk ")
	inspect := run(0, "inspect", "inspect", "doc.md", "--format=json")
	var projection inspection.Projection
	if err := json.Unmarshal(inspect.Inspection, &projection); err != nil {
		t.Fatal(err)
	}
	if inspect.Status != "inspected" || !inspect.Complete || projection.ProjectionVersion != 1 || projection.File.BinaryVersion != version || projection.File.SHA256 != readinessHash(original) || projection.File.SizeBytes != int64(len(original)) || projection.File.ResolvedPath != filepath.Join(root, "doc.md") || !reflect.DeepEqual(inspect.Diagnostics, []result.Diagnostic{d}) {
		t.Fatal(projection.File, inspect)
	}
	allowed := run(0, "inspect", "inspect", "suppressed.md", "--format=json")
	readinessDirective(t, allowed, "allowed", "effective")
	// Run from the child so both lint and preview select the attempted relaxation.
	child := filepath.Join(root, "restricted", "child")
	for _, args := range [][]string{{"lint", "blocked.md", "--format=json"}, {"fix", "--dry-run", "blocked.md", "--format=json"}} {
		got := readOnly(child, args...)
		want := 10
		if args[0] == "fix" {
			want = 0
		}
		if got.Status != want {
			t.Fatal(got)
		}
		r := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
		if len(r.SuppressedDiagnostics) != 0 || (args[0] == "lint" && len(r.Diagnostics) != 1) || (args[0] == "fix" && len(r.Changes) != 1) {
			t.Fatal(r)
		}
	}
	blocked := run(0, "inspect", "inspect", "restricted/child/blocked.md", "--format=json")
	readinessDirective(t, blocked, "denied", "blocked")
	preview := run(10, "fix", "fix", "--dry-run", "doc.md", "suppressed.md", "protected.md", "--format=json")
	if !preview.Complete || preview.DryRun == nil || !*preview.DryRun || len(preview.Changes) != 1 || len(preview.DeferredFixes) != 1 || len(preview.Diagnostics) != 1 || preview.Diagnostics[0].Path != "protected.md" {
		t.Fatal(preview)
	}
	change := preview.Changes[0]
	readinessSnapshot(t, change.Before, "original", original)
	readinessSnapshot(t, change.Candidate, "candidate", candidate)
	if change.Path != "doc.md" || change.Disposition != "proposed" || change.EditCount != 1 || change.Diff == nil || len(change.Diff.Hunks) != 1 || !reflect.DeepEqual(change.Diff.Hunks[0].Lines, []result.DiffLine{{Kind: "context", Text: "# 雪", LineEnding: "crlf"}, {Kind: "remove", Text: "π\tend", LineEnding: "none"}, {Kind: "add", Text: "π\tend", LineEnding: "crlf"}}) {
		t.Fatal(change)
	}
	readinessSnapshot(t, *readinessFile(t, preview, "doc.md").ResultSnapshot, "candidate", candidate)
	// Root-selected lint/preview intentionally differ from target-selected inspect.
	nestedLint := run(10, "lint", "lint", "nested/doc.md", "--format=json")
	if len(nestedLint.Diagnostics) != 1 {
		t.Fatal(nestedLint)
	}
	nestedPreview := run(0, "fix", "fix", "--dry-run", "nested/doc.md", "--format=json")
	if len(nestedPreview.Changes) != 1 {
		t.Fatal(nestedPreview)
	}
	nestedInspect := run(0, "inspect", "inspect", "nested/doc.md", "--format=json")
	if !nestedInspect.Complete || len(nestedInspect.Diagnostics) != 0 {
		t.Fatal(nestedInspect)
	}
	for _, op := range []string{"lint", "fix"} {
		args := []string{op, "doc.md", "missing.md", "--format=json"}
		if op == "fix" {
			args = append(args, "--dry-run")
		}
		r := run(13, op, args...)
		failed := readinessFile(t, r, "missing.md")
		if r.Complete || failed.InputSnapshot != nil || failed.ResultSnapshot != nil || len(failed.ErrorIDs) == 0 || len(r.Errors) == 0 {
			t.Fatal(r)
		}
		linkedIO := false
		for _, e := range r.Errors {
			for _, id := range failed.ErrorIDs {
				if e.ID == id && e.Category == result.IO && e.Path == "missing.md" && e.Snapshot == nil {
					linkedIO = true
				}
			}
		}
		if !linkedIO {
			t.Fatal("missing target lacks linked IO error", r.Errors, failed)
		}
		if op == "lint" {
			readinessSnapshot(t, readinessDiagnostic(t, r, "doc.md").Snapshot, "original", original)
		} else {
			if len(r.Changes) != 1 {
				t.Fatal(r)
			}
			readinessSnapshot(t, r.Changes[0].Candidate, "candidate", candidate)
		}
	}
	editor := readOnly(root, "lint", "doc.md", "--format=vscode")
	if editor.Status != 10 || editor.Stdout != "doc.md:2:6: error: MD047: File must end with a line-ending sequence.\n" {
		t.Fatal(editor)
	}
	text := readOnly(root, "fix", "--dry-run", "doc.md")
	if text.Status != 0 || !strings.Contains(text.Stdout, "proposed doc.md") {
		t.Fatal(text)
	}
	readinessAssertEqual(t, "complete read-only journey", before, readinessState(t, root))
	// A malformed unselected descendant is a downward validation concern only.
	putConfigurationFile(t, root, "nested/bad/.mdchk.yaml", "version: [\n")
	malformedBefore := readinessState(t, root)
	expected = malformedBefore
	invalid := run(12, "config validate", "config", "validate", ".", "--format=json")
	if !invalid.Complete || invalid.ConfigValidation == nil || invalid.ConfigValidation.InvalidDefinitions != 1 || invalid.ConfigValidation.DefinitionsChecked != 5 {
		t.Fatal(invalid)
	}
	found := false
	for _, e := range invalid.Errors {
		if e.Path == "nested/bad/.mdchk.yaml" && e.Category == result.Configuration && e.Code == "config-definition" {
			found = true
		}
	}
	if !found {
		t.Fatal(invalid.Errors)
	}
	run(10, "lint", "lint", "doc.md", "--format=json")
	run(0, "fix", "fix", "--dry-run", "doc.md", "--format=json")
	readinessAssertEqual(t, "malformed descendant discovery", malformedBefore, readinessState(t, root))
	// Actual publication is the capability probe: no GOOS guess can establish
	// APFS/cgo support. A refusal must name the unsupported adapter specifically.
	applied := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
	saved := validateConfigurationOperationJSON(t, []byte(applied.Stdout), "fix")
	if applied.Status == 13 {
		if !strings.Contains(applied.Stdout, "unsupported") || len(saved.Changes) != 0 {
			t.Fatal("not an explicit no-effect adapter refusal", applied)
		}
		readinessAssertEqual(t, "unsupported apply refusal", malformedBefore, readinessState(t, root))
		t.Log("actual native adapter unsupported; exact refusal and no effects verified")
		return
	}
	if applied.Status != 0 || !saved.Complete || len(saved.Changes) != 1 || saved.Changes[0].Disposition != "written" || readinessFile(t, saved, "doc.md").WriteStatus != "written" {
		t.Fatal(applied)
	}
	readinessSnapshot(t, *readinessFile(t, saved, "doc.md").ResultSnapshot, "saved", candidate)
	//nolint:gosec // Test-owned physical fixture target.
	data, err := os.ReadFile(filepath.Join(root, "doc.md"))
	if err != nil || string(data) != candidate {
		t.Fatal("saved bytes", err, string(data))
	}
	afterSave := readinessState(t, root)
	// Only publication's target and its parent mtime may change. Retain the
	// parent's identity, type and mode, and compare every other entry in full.
	unrelatedBefore := make(map[string]readinessEntry, len(malformedBefore))
	unrelatedAfter := make(map[string]readinessEntry, len(afterSave))
	for path, entry := range malformedBefore {
		if path != "doc.md" {
			unrelatedBefore[path] = entry
		}
	}
	for path, entry := range afterSave {
		if path != "doc.md" {
			unrelatedAfter[path] = entry
		}
	}
	parentBefore, parentAfter := unrelatedBefore["."], unrelatedAfter["."]
	readinessAssertParentAfterSave(t, parentBefore, parentAfter)
	delete(unrelatedBefore, ".")
	delete(unrelatedAfter, ".")
	readinessAssertEqual(t, "apply unrelated entries", unrelatedBefore, unrelatedAfter)
	expected = afterSave
	run(0, "lint", "lint", "doc.md", "--format=json")
	afterInspect := run(0, "inspect", "inspect", "doc.md", "--format=json")
	readinessSnapshot(t, *readinessFile(t, afterInspect, "doc.md").InputSnapshot, "original", candidate)
	second := run(0, "fix", "fix", "doc.md", "--format=json")
	if len(second.Changes) != 0 || readinessFile(t, second, "doc.md").WriteStatus != "not-needed" {
		t.Fatal("apply not idempotent", second)
	}
	readinessAssertEqual(t, "second apply", afterSave, readinessState(t, root))
	t.Log("actual native saved bytes, observed saved identity and inode-preserving idempotence verified")
}

func readinessSnapshot(t *testing.T, s result.Snapshot, view, contents string) {
	t.Helper()
	if s.View != view || s.SHA256 != readinessHash(contents) || s.SizeBytes != len(contents) {
		t.Fatalf("snapshot %+v want %s/%s/%d", s, view, readinessHash(contents), len(contents))
	}
}
func readinessFile(t *testing.T, r result.Result, path string) result.File {
	t.Helper()
	for _, f := range r.Files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("missing file %s: %+v", path, r.Files)
	return result.File{}
}
func readinessDiagnostic(t *testing.T, r result.Result, path string) result.Diagnostic {
	t.Helper()
	for _, d := range r.Diagnostics {
		if d.Path == path {
			return d
		}
	}
	t.Fatalf("missing finding %s", path)
	return result.Diagnostic{}
}
func readinessDirective(t *testing.T, r result.Result, permission, state string) {
	t.Helper()
	var p inspection.Projection
	if err := json.Unmarshal(r.Inspection, &p); err != nil {
		t.Fatal(err)
	}
	if p.Directives.Data == nil || len(*p.Directives.Data) != 1 {
		t.Fatal(p.Directives)
	}
	d := (*p.Directives.Data)[0]
	if d.State != state || len(d.Effects) != 1 || d.Effects[0].Rule != "MD047" || d.Effects[0].Permission != permission || (permission == "denied" && len(d.Effects[0].Provenance) == 0) {
		t.Fatal(d)
	}
}

func readinessAssertParentAfterSave(t *testing.T, before, after readinessEntry) {
	t.Helper()
	if before.Info == nil || after.Info == nil {
		t.Fatal("apply parent: missing captured metadata")
	}
	if !os.SameFile(before.Info, after.Info) {
		t.Fatal("apply parent: native identity changed")
	}
	if before.Info.Mode() != after.Info.Mode() {
		t.Fatalf("apply parent: type/mode before=%s after=%s", before.Info.Mode(), after.Info.Mode())
	}
}
