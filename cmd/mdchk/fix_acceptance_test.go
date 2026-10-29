package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

func buildFixFixture(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	changes := map[string]string{}
	for _, pair := range [][2]string{{"providers.go", "fix_analysis_providers.go"}, {"fix_providers.go", "fix_correction_providers.go"}} {
		changes[filepath.Join(cwd, "..", "..", "internal", "cli", pair[0])] = filepath.Join(cwd, "testdata", pair[1])
	}
	data, err := json.Marshal(struct{ Replace map[string]string }{changes})
	if err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(t.TempDir(), "overlay.json")
	if err := os.WriteFile(overlay, data, 0600); err != nil {
		t.Fatal(err)
	}
	return buildConfigurationExecutable(t, overlay)
}
func fixFixtureConfiguration() string {
	var b strings.Builder
	b.WriteString("version: 1\nflavor: commonmark\nrules:\n")
	for _, meta := range engine.Catalog() {
		fmt.Fprintf(&b, "  %s: {enabled: %t}\n", meta.ID, meta.ID == "MD001" || meta.ID == "MD094")
	}
	return b.String()
}
func TestCompiledFixDryRunCandidatesAndFailures(t *testing.T) {
	binary := buildFixFixture(t)
	for _, tc := range []struct {
		name, source string
		status       int
		changed      bool
	}{{"clean-prediction", "text\n", 0, true}, {"two-rounds", "two-round-fixture\n", 0, true}, {"shared-duplicate", "duplicate-fixture\n", 0, true}, {"default-scaling", strings.Repeat("p\n\n", 8000), 0, true}, {"remaining-Unicode", "text 雪\n", 10, true}, {"CRLF", "text\r\n", 0, true}, {"lone-CR", "text\r", 0, true}, {"missing-ending", "text", 0, true}, {"empty", "", 0, true}, {"conflict", "conflict-fixture\n", 10, false}, {"unsafe", "unsafe-fixture\n", 10, false}, {"unsafe-observations", "unsafe-fixture 雪\n", 10, false}, {"cycle", "cycle-fixture\n", 15, false}, {"cancel", "cancel-fixture\n", 15, false}, {"internal", "internal-fixture\n", 14, false}} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", fixFixtureConfiguration())
			putConfigurationFile(t, root, "doc.md", tc.source)
			before, err := os.Stat(filepath.Join(root, "doc.md"))
			if err != nil {
				t.Fatal(err)
			}
			for _, format := range []string{"json", "text"} {
				got := runFixConfiguration(t, binary, root, "fix", "doc.md", "--dry-run", "--format="+format)
				if got.Status != tc.status {
					t.Fatalf("status %d want %d: %s", got.Status, tc.status, got.Stdout)
				}
				if format == "text" {
					if strings.Contains(got.Stdout, "proposed doc.md") != tc.changed {
						t.Fatal(got.Stdout)
					}
					continue
				}
				r := got.Result
				if r.Operation != "fix" || r.DryRun == nil || !*r.DryRun || len(r.Documents) != 0 {
					t.Fatal(r)
				}
				for _, diagnostic := range r.Diagnostics {
					if r.Files[0].ResultSnapshot != nil && diagnostic.Snapshot != *r.Files[0].ResultSnapshot {
						t.Fatal("provisional finding mislabeled as final", diagnostic, r.Files[0])
					}
				}
				if (len(r.Changes) > 0) != tc.changed {
					t.Fatalf("changes: %+v", r)
				}
				if tc.changed {
					change := r.Changes[0]
					candidate := "\n" + tc.source
					if tc.name == "two-rounds" {
						candidate = "\n" + candidate
					}
					if change.Before.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.source))) || change.Candidate.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(candidate))) || change.Before.SizeBytes != len(tc.source) || change.Candidate.SizeBytes != len(candidate) || change.Disposition != "proposed" || change.Diff == nil || r.Files[0].WriteStatus != "proposed" || !reflect.DeepEqual(r.Files[0].ChangeIDs, []string{change.ID}) {
						t.Fatal(change, r.Files)
					}
					if tc.name == "shared-duplicate" && change.EditCount != 1 {
						t.Fatal("duplicate application counted twice", change)
					}
					if len(r.Diagnostics) > 0 {
						d := r.Diagnostics[0]
						if d.Range.Start.Line != 2 || d.Range.Start.Column != 6 || d.ByteRange.Start != 1+strings.Index(tc.source, "雪") || d.Snapshot != change.Candidate {
							t.Fatal(d)
						}
					}
				} else {
					if r.Files[0].WriteStatus != "not-needed" {
						t.Fatal(r.Files)
					}
					if (tc.name == "conflict" || tc.name == "unsafe") && len(r.DeferredFixes) == 0 {
						t.Fatal("missing deferral", r)
					}
				}
			}
			after, err := os.Stat(filepath.Join(root, "doc.md"))
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("dry-run changed metadata")
			}
		})
	}
}
func TestCompiledFixCandidateSuppressionAndIndependentIO(t *testing.T) {
	binary := buildFixFixture(t)
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", fixFixtureConfiguration())
	source := "text\n<!-- mdchk:disable MD001 -->\n雪\n"
	putConfigurationFile(t, root, "doc.md", source)
	got := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "missing.md", "--format=json")
	if got.Status != 13 {
		t.Fatal(got.Status, got.Stdout)
	}
	r := got.Result
	if len(r.Changes) != 1 || len(r.SuppressedDiagnostics) != 1 || len(r.Files) != 2 {
		t.Fatal(r)
	}
	var hidden struct {
		Diagnostic struct {
			Snapshot struct {
				View, SHA256 string
				SizeBytes    int `json:"size_bytes"`
			} `json:"snapshot"`
			ByteRange struct{ Start, End int }                   `json:"byte_range"`
			Range     struct{ Start struct{ Line, Column int } } `json:"range"`
		} `json:"diagnostic"`
	}
	if err := json.Unmarshal(r.SuppressedDiagnostics[0], &hidden); err != nil {
		t.Fatal(err)
	}
	if hidden.Diagnostic.Snapshot.View != "candidate" || hidden.Diagnostic.Snapshot.SHA256 != r.Changes[0].Candidate.SHA256 || hidden.Diagnostic.Range.Start.Line != 4 || hidden.Diagnostic.Range.Start.Column != 1 || hidden.Diagnostic.ByteRange.Start != strings.Index(source, "雪")+1 {
		t.Fatal(hidden)
	}
	// An independently changed source identity must fail semantic validation.
	python := os.Getenv("MDCHK_RESULT_VALIDATOR_PYTHON")
	if python != "" {
		assertFixSuppressionRejectsIdentity(t, python, []byte(got.Stdout))
	}
}

func assertFixSuppressionRejectsIdentity(t *testing.T, python string, data []byte) {
	t.Helper()
	var r map[string]any
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	hidden := r["suppressed_diagnostics"].([]any)[0].(map[string]any)["diagnostic"].(map[string]any)
	hidden["snapshot"].(map[string]any)["sha256"] = strings.Repeat("c", 64)
	broken, _ := json.Marshal(r)
	path := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(path, broken, 0600); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	validator := filepath.Join(cwd, "..", "..", "docs", "contracts", "results", "validate.py")
	script := "import json,runpy,sys; v=runpy.run_path(sys.argv[1]); x=json.load(open(sys.argv[2])); v['validator'].validate(x); v['semantic'](x)"
	//nolint:gosec // Explicit test interpreter and controlled fixture paths.
	if output, err := exec.Command(python, "-c", script, validator, path).CombinedOutput(); err == nil || !strings.Contains(string(output), "suppressed snapshot mismatch") {
		t.Fatalf("unrelated suppressed identity accepted: %v %s", err, output)
	}
}

func runFixConfiguration(t *testing.T, binary, root string, args ...string) configurationRun {
	t.Helper()
	before := configurationBytes(t, root)
	out := runConfigurationProcess(t, binary, root, args...)
	after := configurationBytes(t, root)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("dry-run changed directory inventory or bytes")
	}
	for _, arg := range args {
		if arg == "--format=json" {
			out.Result = validateConfigurationOperationJSON(t, []byte(out.Stdout), "fix")
		}
	}
	return out
}

func TestCompiledProductionFixBoundaryAndNoWrites(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	config := strings.ReplaceAll(fixFixtureConfiguration(), "MD001: {enabled: true}", "MD001: {enabled: false}")
	config = strings.ReplaceAll(config, "MD047: {enabled: false}", "MD047: {enabled: true}")
	putConfigurationFile(t, root, ".mdchk.yaml", config)
	// Keep this boundary matrix report-only through an indented-code EOF.
	putConfigurationFile(t, root, "doc.md", "    snow 雪")
	putConfigurationFile(t, root, "-dash.md", "text\n")
	for _, tc := range []struct {
		args   []string
		status int
	}{
		{[]string{"fix", "doc.md", "--dry-run", "--format=json"}, 10},
		{[]string{"fix", "--dry-run", "--format=json", "--", "-dash.md"}, 0},
		{[]string{"fix", "--config=missing.yaml", "missing.md", "--format=json"}, 12},
		{[]string{"fix", "--dry-run", "--config=missing.yaml", "doc.md", "--format=json"}, 12},
		{[]string{"fix", "--dry-run", "missing.md", "--format=json"}, 13},
		{[]string{"fix", "--dry-run", "doc.md", "--limit=work-units=1", "--format=json"}, 15},
		{[]string{"fix", "--dry-run", "doc.md", "--limit=document-bytes=1", "--format=json"}, 11},
		{[]string{"fix", "--dry-run", "doc.md", "--limit=reporting-time=1ns", "--format=json"}, 15},
		{[]string{"fix", "--dry-run", "doc.md", "--limit=reporting-time=1us", "--format=json"}, -1},
	} {
		got := runFixConfiguration(t, binary, root, tc.args...)
		if tc.status == -1 {
			switch got.Status {
			case 10:
				if !got.Result.Complete || len(got.Result.Interruptions) != 0 || len(got.Result.Diagnostics) != 1 {
					t.Fatal("completed deadline branch lost finding", got.Result)
				}
			case 15:
				if got.Result.Complete || len(got.Result.Interruptions) == 0 || len(got.Result.Omissions) == 0 {
					t.Fatal("interrupted deadline branch lacks attribution", got.Result)
				}
			default:
				t.Fatal("invalid deadline status", got.Status, got.Stdout)
			}
		} else if got.Status != tc.status {
			t.Fatalf("%v: %d want %d %s", tc.args, got.Status, tc.status, got.Stdout)
		}
		if len(got.Result.Changes) != 0 || len(got.Result.Documents) != 0 {
			t.Fatal(got.Result)
		}
		for _, d := range got.Result.Diagnostics {
			if d.FixAvailable {
				t.Fatal("unsafe production correction advertised")
			}
		}
	}
	// The untouched catalog defaults still expose reduced coverage honestly.
	putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\nflavor: commonmark\n")
	got := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
	if got.Status != 15 || len(got.Result.Coverage) == 0 {
		t.Fatal(got.Status, got.Stdout)
	}
}

func TestCompiledFixInternalPrecedenceAndClosedOutput(t *testing.T) {
	binary := buildFixFixture(t)
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", fixFixtureConfiguration())
	putConfigurationFile(t, root, "doc.md", "internal-fixture\n")
	got := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "missing.md", "--format=json")
	if got.Status != 14 {
		t.Fatal(got.Status, got.Stdout)
	}
	cats := map[string]bool{}
	for _, e := range got.Result.Errors {
		cats[string(e.Category)] = true
	}
	if !cats["internal"] || !cats["io"] {
		t.Fatal(got.Result.Errors)
	}
	putConfigurationFile(t, root, "doc.md", "text\n")
	before := configurationBytes(t, root)
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	//nolint:gosec // Execute only the compiled fixture with test-owned paths.
	command := exec.Command(binary, "fix", "--dry-run", "doc.md", "--format=json")
	command.Dir = root
	command.Stdout = write
	err = command.Run()
	_ = write.Close()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 13 {
		t.Fatalf("closed output: %v", err)
	}
	if after := configurationBytes(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("output failure changed fixture")
	}
}

func TestCompiledFixRollbackAttributionAndIndependentRetention(t *testing.T) {
	binary := buildFixFixture(t)
	for _, tc := range []struct {
		name, source string
		runtime      bool
	}{{"cycle", "cycle-fixture\n", false}, {"late-runtime", "late-cancel-fixture\n", true}, {"late-quota", "late-quota-fixture\n", true}} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", fixFixtureConfiguration())
			putConfigurationFile(t, root, "a-independent.md", "safe\n")
			putConfigurationFile(t, root, "z-rollback.md", tc.source)
			out := runFixConfiguration(t, binary, root, "fix", "--dry-run", "a-independent.md", "z-rollback.md", "--format=json")
			if out.Status != 15 {
				t.Fatal(out.Status, out.Stdout)
			}
			r := out.Result
			if len(r.Changes) != 1 || r.Changes[0].Path != "a-independent.md" || r.Files[0].WriteStatus != "proposed" || r.Files[1].WriteStatus != "not-needed" || r.Files[1].ResultSnapshot.SHA256 != r.Files[1].InputSnapshot.SHA256 {
				t.Fatal(r)
			}
			count := 0
			rounds := map[string]bool{}
			for _, d := range r.DeferredFixes {
				if d.Path != "z-rollback.md" || d.Rule != "MD001" || d.Range == nil || d.Range.Start.Line != 1 || d.Range.Start.Column != 1 {
					t.Fatal(d)
				}
				rounds[d.GroupID] = true
				count++
				want := "fix-nonconvergent"
				if tc.runtime {
					want = "cancelled"
					if tc.name == "late-quota" {
						want = "limit-exceeded"
					}
					if len(d.CauseIDs) != 1 {
						t.Fatal("runtime group lacks cause", d)
					}
				}
				if d.Reason != want {
					t.Fatal(d)
				}
				expected := tc.source
				if d.GroupID == "round-2:boundary" {
					expected = "\n" + expected
				}
				if d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(expected))) || d.Snapshot.SizeBytes != len(expected) {
					t.Fatal("wrong actual attempt source", d)
				}
			}
			if tc.runtime && count == 0 {
				for _, o := range r.Omissions {
					if o.Section == "/deferred_fixes" && o.Path == "z-rollback.md" && o.Count != nil {
						count += *o.Count
					}
				}
				if count != 1 {
					t.Fatal("known runtime rollback group disappeared", r)
				}
			} else if !tc.runtime && (count != 2 || !rounds["round-1:boundary"] || !rounds["round-2:boundary"]) {
				t.Fatal("cycle attempts disappeared", r.DeferredFixes)
			}
		})
	}
}
func TestCompiledFixUnstartedTargetsHaveNoCandidate(t *testing.T) {
	production := buildConfigurationExecutable(t, "")
	synthetic := buildFixFixture(t)
	for _, tc := range []struct {
		name, first string
		binary      string
		status      int
	}{{"invalid-encoding", string([]byte{255}), production, 13}, {"cancel-before-later-analysis", "cancel-analysis-fixture\n", synthetic, 15}} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			configuration := fixFixtureConfiguration()
			if tc.binary == production {
				configuration = strings.ReplaceAll(configuration, "MD001: {enabled: true}", "MD001: {enabled: false}")
				configuration = strings.ReplaceAll(configuration, "MD047: {enabled: false}", "MD047: {enabled: true}")
			}
			putConfigurationFile(t, root, ".mdchk.yaml", configuration)
			putConfigurationFile(t, root, "a.md", tc.first)
			putConfigurationFile(t, root, "b.md", "hello")
			got := runFixConfiguration(t, tc.binary, root, "fix", "--dry-run", "a.md", "b.md", "--format=json")
			if got.Status != tc.status {
				t.Fatal(got.Status, got.Stdout)
			}
			r := got.Result
			if len(r.Files) != 2 {
				t.Fatal(r)
			}
			// Encoding is local; cancellation still leaves later work unstarted.
			if tc.name == "invalid-encoding" {
				candidate := r.Files[1].ResultSnapshot
				if len(r.Files) != 2 || r.Files[0].Analysis != "failed" || r.Files[1].Analysis != "complete" || candidate == nil || candidate.View != "candidate" || candidate.SizeBytes != 6 || candidate.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte("hello\n"))) || !reflect.DeepEqual(r.CheckedFiles, []string{"a.md", "b.md"}) || len(r.Changes) != 1 || r.Changes[0].Path != "b.md" || r.Changes[0].Disposition != "proposed" || r.Changes[0].EditCount != 1 || !reflect.DeepEqual(r.Changes[0].Rules, []string{"MD047"}) || len(r.Errors) != 1 || r.Errors[0].Code != "invalid-utf8" || r.Errors[0].Path != "a.md" || r.Errors[0].ByteOffset == nil || *r.Errors[0].ByteOffset != 0 || len(r.Diagnostics) != 0 || r.Complete {
					t.Fatal(r)
				}
			} else if len(r.Files) != 2 || r.Files[1].Analysis != "not-started" || r.Files[1].ResultSnapshot != nil || len(r.CheckedFiles) != 1 || r.CheckedFiles[0] != "a.md" || len(r.Files[1].CoverageIDs) == 0 {
				t.Fatal(r)
			}
			identity := r.Files[1].InputSnapshot
			if identity == nil || identity.View != "original" || identity.SizeBytes != 5 || identity.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte("hello"))) {
				t.Fatal(identity)
			}
		})
	}
}

func TestCompiledFixIndependentDeferralsKeepTheirActualCauses(t *testing.T) {
	binary := buildFixFixture(t)
	for _, tc := range []struct {
		name, source, reason, category string
		status                         int
	}{{"cancel", "late-cancel-fixture\n", "cancelled", "interruption", 15}, {"internal", "late-internal-fixture\n", "validation-failed", "internal", 14}} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", fixFixtureConfiguration())
			sources := map[string]string{"a-conflict.md": "conflict-fixture\n", "b-unsafe.md": "unsafe-fixture\n", "c-cycle.md": "cycle-fixture\n", "d-safe.md": "safe\n", "z-runtime.md": tc.source}
			for path, source := range sources {
				putConfigurationFile(t, root, path, source)
			}
			out := runFixConfiguration(t, binary, root, "fix", "--dry-run", ".", "--format=json")
			if out.Status != tc.status {
				t.Fatal(out.Status, out.Stdout)
			}
			r := out.Result
			if len(r.Changes) != 1 || r.Changes[0].Path != "d-safe.md" || r.Changes[0].Disposition != "proposed" {
				t.Fatal("independent safe proposal changed disposition", r.Changes)
			}
			counts := map[string]int{}
			runtimeCause := ""
			if tc.category == "interruption" {
				for _, stop := range r.Interruptions {
					if stop.Reason == "cancelled" && stop.Phase == "rechecking" {
						runtimeCause = stop.ID
					}
				}
			} else {
				for _, e := range r.Errors {
					if e.Category == "internal" && e.Code == "validation-failed" {
						runtimeCause = e.ID
					}
				}
			}
			if runtimeCause == "" {
				t.Fatal("missing actual runtime cause", r)
			}
			for _, d := range r.DeferredFixes {
				counts[d.Path]++
				expected := sources[d.Path]
				if d.GroupID == "round-2:boundary" {
					expected = "\n" + expected
				}
				if d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(expected))) || d.Snapshot.SizeBytes != len(expected) || d.Range == nil || d.Range.Start.Line != 1 || d.Range.Start.Column != 1 {
					t.Fatal("deferral lost exact attempted source or anchor", d)
				}
				switch d.Path {
				case "a-conflict.md":
					if d.Reason != "conflict" || len(d.ConflictGroupIDs) == 0 || len(d.CauseIDs) != 0 {
						t.Fatal("intrinsic conflict inherited unrelated runtime cause", d)
					}
				case "b-unsafe.md":
					if d.Reason != "meaning-not-preserved" || len(d.CauseIDs) != 0 {
						t.Fatal("intrinsic unsafe decision inherited unrelated runtime cause", d)
					}
				case "c-cycle.md":
					if d.Reason != "fix-nonconvergent" || len(d.CauseIDs) != 0 {
						t.Fatal("intrinsic nonconvergence inherited unrelated runtime cause", d)
					}
				case "z-runtime.md":
					if d.Reason != tc.reason || !reflect.DeepEqual(d.CauseIDs, []string{runtimeCause}) {
						t.Fatal("actual runtime rollback lost its canonical cause", d)
					}
				default:
					t.Fatal("independent retained proposal became a deferral", d)
				}
			}
			if counts["a-conflict.md"] != 2 || counts["b-unsafe.md"] != 1 || counts["c-cycle.md"] != 2 || counts["z-runtime.md"] != 1 {
				t.Fatal("known groups or exact decisions disappeared", counts, r)
			}
			if len(r.Omissions) != 0 {
				t.Fatal("unexpected known-record omission", r.Omissions)
			}
			for _, file := range r.Files {
				if file.Path == "d-safe.md" {
					if file.WriteStatus != "proposed" {
						t.Fatal(file)
					}
				} else if file.WriteStatus != "not-needed" || file.InputSnapshot == nil || file.ResultSnapshot == nil || file.InputSnapshot.SHA256 != file.ResultSnapshot.SHA256 {
					t.Fatal("independent target retention changed", file)
				}
			}
		})
	}
}
