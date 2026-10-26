package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

const availableConfiguration = "version: 1\nflavor: commonmark\ndirectives: {enabled: false}\n"

type configurationRun struct {
	Status int
	Stdout string
	Result result.Result
}

func buildConfigurationExecutable(t *testing.T, overlay string) string {
	t.Helper()
	name := "mdchk"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	args := []string{"build", "-o", binary}
	if overlay != "" {
		args = append(args, "-overlay", overlay)
	}
	args = append(args, ".")
	//nolint:gosec // Fixed Go tool builds this package into a test-owned destination.
	command := exec.CommandContext(ctx, "go", args...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build executable: %v\n%s", err, output)
	}
	return binary
}

func configurationProject(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func putConfigurationFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func cleanConfiguration() string {
	var b strings.Builder
	b.WriteString(availableConfiguration + "rules:\n")
	for _, rule := range engine.Catalog() {
		fmt.Fprintf(&b, "  %s: {enabled: false}\n", rule.ID)
	}
	return b.String()
}

func configurationBytes(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	snapshots := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		//nolint:gosec // Paths originate in the test-owned fixture tree.
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snapshots[path] = sha256.Sum256(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshots
}

func runConfiguration(t *testing.T, binary, cwd, snapshotRoot string, args ...string) configurationRun {
	t.Helper()
	before := configurationBytes(t, snapshotRoot)
	defer func() {
		after := configurationBytes(t, snapshotRoot)
		if !reflect.DeepEqual(before, after) {
			t.Error("lint changed source/configuration fixture bytes or files")
		}
	}()
	out := runConfigurationProcess(t, binary, cwd, args...)
	jsonFormat := false
	for i, arg := range args {
		if arg == "--format=json" || arg == "--format" && i+1 < len(args) && args[i+1] == "json" {
			jsonFormat = true
		}
	}
	if jsonFormat {
		out.Result = validateConfigurationJSON(t, []byte(out.Stdout))
		checkConfigurationLinks(t, cwd, out)
	}
	return out
}

// runConfigurationProcess runs the actual executable. Callers independently
// guard fixture bytes; metadata and permission tests need different guards.
func runConfigurationProcess(t *testing.T, binary, cwd string, args ...string) configurationRun {
	t.Helper()
	return runConfigurationProcessEnv(t, binary, cwd, nil, args...)
}

func runConfigurationProcessEnv(t *testing.T, binary, cwd string, env []string, args ...string) configurationRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	//nolint:gosec // Execute only our compiled binary with test-owned arguments and fixtures.
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = cwd
	command.Env = env
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	out := configurationRun{Stdout: stdout.String()}
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("run executable: %v", err)
		}
		out.Status = exit.ExitCode()
	}
	if ctx.Err() != nil {
		t.Fatalf("executable deadline: %v", ctx.Err())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", &stderr)
	}
	return out
}

func validateConfigurationJSON(t *testing.T, data []byte) result.Result {
	t.Helper()
	return validateConfigurationOperationJSON(t, data, "lint")
}

func validateConfigurationOperationJSON(t *testing.T, data []byte, operation string) result.Result {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	var out result.Result
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("JSON report: %v: %s", err, data)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		t.Fatalf("report contains trailing JSON: %v", err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) || out.SchemaVersion != 1 || out.Operation != operation {
		t.Fatalf("invalid report envelope: %s", data)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"checked_files", "skipped_files", "files", "diagnostics", "coverage", "errors", "interruptions", "effective_limits", "changes", "deferred_fixes", "notes", "omissions", "documents", "suppressed_diagnostics"} {
		if raw := bytes.TrimSpace(fields[name]); len(raw) == 0 || raw[0] != '[' {
			t.Fatalf("mandatory %s is not an array: %s", name, data)
		}
	}
	for _, name := range out.CheckedFiles {
		if filepath.IsAbs(name) || (runtime.GOOS == "windows" && strings.Contains(name, "\\")) {
			t.Fatalf("nonportable checked path %q", name)
		}
	}
	if python := os.Getenv("MDCHK_RESULT_VALIDATOR_PYTHON"); python != "" {
		fixture := filepath.Join(t.TempDir(), "actual-result.json")
		if err := os.WriteFile(fixture, data, 0600); err != nil {
			t.Fatal(err)
		}
		validator, err := filepath.Abs("../../docs/contracts/results/validate.py")
		if err != nil {
			t.Fatal(err)
		}
		script := "import json,runpy,sys; v=runpy.run_path(sys.argv[1]); x=json.load(open(sys.argv[2])); v['validator'].validate(x); v['semantic'](x)"
		//nolint:gosec // Explicit test interpreter, checked-in validator and test-owned report.
		if output, err := exec.Command(python, "-c", script, validator, fixture).CombinedOutput(); err != nil {
			t.Fatalf("actual report schema/semantics: %v\n%s", err, output)
		}
	}
	return out
}

func TestConfigurationAcceptance(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	t.Run("strict definitions", func(t *testing.T) { testConfigurationStrict(t, binary) })
	t.Run("discovery and policy", func(t *testing.T) { testConfigurationDiscovery(t, binary) })
}

// The syntax context admits production CommonMark with directives disabled.
// Keep every independent negative otherwise admissible so an unrelated default
// capability failure cannot hide a missing syntax or domain check.
const configurationSyntaxContext = "flavor: commonmark\ndirectives: {enabled: false}\n"

type configurationNegative struct {
	name, yaml, field, message string
	line, column               int
}

func assertConfigurationNegative(t *testing.T, run configurationRun, tc configurationNegative) {
	t.Helper()
	if run.Status != 12 || len(run.Result.Errors) == 0 || len(run.Result.CheckedFiles) != 0 || len(run.Result.Diagnostics) != 0 || len(run.Result.Files) != 0 || len(run.Result.Coverage) != 0 {
		t.Fatalf("configuration preflight: status=%d result=%+v", run.Status, run.Result)
	}
	for _, e := range run.Result.Errors {
		if e.Category != result.Configuration || e.Path != ".mdchk.yaml" || !strings.Contains(e.Message, tc.message) || (tc.field != "" && !strings.Contains(e.Message, "("+tc.field+")")) {
			t.Fatalf("expected %s error %q, got %+v", tc.field, tc.message, e)
		}
		if tc.line == 0 {
			if e.Range != nil {
				t.Fatalf("invented location for unlocated document error: %+v", e)
			}
		} else if e.Range == nil || e.Range.Start.Line != tc.line || e.Range.Start.Column != tc.column {
			t.Fatalf("wrong original error location; want %d:%d: %+v", tc.line, tc.column, e)
		}
	}
}

func testConfigurationStrict(t *testing.T, binary string) {
	for _, tc := range []configurationNegative{
		{"missing version", configurationSyntaxContext + "rules: {}\n", "", "required version field is missing", 1, 7},
		{"quoted version", configurationSyntaxContext + "version: '1'\n", "version", "expected integer, got string", 3, 10},
		{"fractional version", configurationSyntaxContext + "version: 1.9\n", "version", "expected integer, got number", 3, 10},
		{"leading zero", configurationSyntaxContext + "version: 01\n", "version", "integer must use decimal notation without leading zeros", 3, 10},
		{"unsupported version", configurationSyntaxContext + "version: 2\n", "version", "unsupported schema version", 3, 10},
		{"null document", "null\n", "", "null settings are not permitted", 1, 1},
		{"empty document", "", "", "expected a nonempty mapping document", 0, 0},
		{"cycle", configurationSyntaxContext + "version: 1\nrules: &r {MD013: *r}\n", "rules.MD013", "cyclic alias is not permitted", 4, 19},
		{"merge", configurationSyntaxContext + "version: 1\nrules:\n  MD013: &r {enabled: false}\n  MD001: {<<: *r}\n", "rules.MD001", "merge keys are not permitted", 6, 11},
		{"multiple documents", availableConfiguration + "---\nversion: 1\n", "", "expected exactly one nonempty YAML document", 0, 0},
		{"nonstring key", configurationSyntaxContext + "version: 1\nrules: {13: {}}\n", "rules", "mapping keys must be strings", 4, 9},
		{"null rules", configurationSyntaxContext + "version: 1\nrules: null\n", "rules", "null settings are not permitted", 4, 8},
		{"unknown ID", availableConfiguration + "rules: {MD999: {}}\n", "rules.MD999", "unknown or reserved rule", 4, 16},
		{"reserved ID", availableConfiguration + "rules: {MD002: {}}\n", "rules.MD002", "unknown or reserved rule", 4, 16},
		{"lowercase ID", availableConfiguration + "rules: {md013: {}}\n", "rules.md013", "unknown or reserved rule", 4, 16},
		{"descriptive ID", availableConfiguration + "rules: {line-length: {}}\n", "rules.line-length", "unknown or reserved rule", 4, 22},
		{"invalid disabled domain", availableConfiguration + "rules: {MD013: {enabled: false, length: -1}}\n", "rules.MD013.length", "value outside integer option domain", 4, 41},
		{"invalid disabled type", availableConfiguration + "rules: {MD013: {enabled: false, length: '100'}}\n", "rules.MD013.length", "value outside integer option domain", 4, 41},
		{"custom tag", configurationSyntaxContext + "version: !custom 1\n", "version", "custom or unsupported YAML tag !custom", 3, 10},
		{"null rule", availableConfiguration + "rules: {MD013: null}\n", "rules.MD013", "null settings are not permitted", 4, 16},
		{"unknown disabled option", availableConfiguration + "rules: {MD013: {enabled: false, lenght: 80}}\n", "rules.MD013.lenght", "unknown option", 4, 41},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", tc.yaml)
			putConfigurationFile(t, root, "bad.md", "bad\xff")
			run := runConfiguration(t, binary, root, root, "lint", "--format=json", "bad.md", "missing.md")
			assertConfigurationNegative(t, run, tc)
		})
	}
	t.Run("duplicate keys", func(t *testing.T) {
		for _, tc := range []struct {
			name, yaml, field string
			positions         [2][2]int
		}{
			{"top", configurationSyntaxContext + "version: 1\nversion: 1\n", "version", [2][2]int{{3, 1}, {4, 1}}},
			{"rule", availableConfiguration + "rules:\n  MD013: {}\n  MD013: {}\n", "rules.MD013", [2][2]int{{5, 3}, {6, 3}}},
			{"option", availableConfiguration + "rules:\n  MD013: {length: 100, length: 100}\n", "rules.MD013.length", [2][2]int{{5, 11}, {5, 24}}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", tc.yaml)
				run := runConfiguration(t, binary, root, root, "lint", "--format=json", "missing.md")
				if len(run.Result.Errors) != 2 {
					t.Fatalf("duplicate lost an occurrence: %+v", run)
				}
				for i, e := range run.Result.Errors {
					one := run
					one.Result.Errors = []result.Error{e}
					assertConfigurationNegative(t, one, configurationNegative{field: tc.field, message: "duplicate key", line: tc.positions[i][0], column: tc.positions[i][1]})
					if !strings.Contains(e.Message, "previous occurrence") {
						t.Fatalf("duplicate context lost: %+v", e)
					}
				}
			})
		}
	})
	t.Run("capability preflight", func(t *testing.T) {
		for _, tc := range []configurationNegative{
			{"explicit enabled default", availableConfiguration + "rules: {MD009: {}}\n", "rules.MD009", "explicitly enabled rule is unavailable", 4, 16},
			{"explicit enabled unavailable", availableConfiguration + "rules: {MD013: {enabled: true}}\n", "rules.MD013", "explicitly enabled rule is unavailable", 4, 16},
			{"removed flavor", "version: 1\nflavor: pandoc\ndirectives: {enabled: false}\n", "flavor", "expected commonmark or gfm", 2, 9},
			{"resources unavailable", availableConfiguration + "resources: {index_file: index.md}\n", "resources.index_file", "resource index handling is unavailable", 4, 25},
		} {
			t.Run(tc.name, func(t *testing.T) {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", tc.yaml)
				putConfigurationFile(t, root, "bad.md", "bad\xff")
				run := runConfiguration(t, binary, root, root, "lint", "--format=json", "bad.md", "missing.md")
				assertConfigurationNegative(t, run, tc)
			})
		}
	})
	// Corrected semantic controls must reach target processing. Keeping the same
	// invalid UTF-8/missing targets demonstrates admission without a clean-result
	// shortcut, and proves that the negatives cannot pass via an unrelated capability.
	t.Run("corrected semantic controls", func(t *testing.T) {
		for _, rules := range []string{"rules: {MD013: {enabled: false, length: 100}}\n", "rules: {MD013: {enabled: false, length: 80}}\n", "rules: {MD013: {}}\n", "documents: {slides: ['talk/**']}\n"} {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", availableConfiguration+rules)
			putConfigurationFile(t, root, "bad.md", "bad\xff")
			run := runConfiguration(t, binary, root, root, "lint", "--format=json", "bad.md", "missing.md")
			if run.Status != 13 || len(run.Result.Errors) != 2 || len(run.Result.Files) != 2 {
				t.Fatalf("corrected semantic input did not reach targets: %+v", run)
			}
			for _, e := range run.Result.Errors {
				if e.Category != result.IO {
					t.Fatalf("unrelated preflight failure: %+v", e)
				}
			}
		}
	})
	t.Run("production complete read-only", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", cleanConfiguration())
		putConfigurationFile(t, root, "good.md", "# 雪\r\n")
		for _, format := range []string{"json", "text"} {
			run := runConfiguration(t, binary, root, root, "lint", "--format="+format, "good.md")
			if run.Status != 0 || !strings.Contains(run.Stdout, "clean") {
				t.Fatalf("complete production: %+v", run)
			}
		}
	})
	for _, rules := range []string{"rules: {}\n", "rules: {MD013: {}}\n", "rules: {MD013: {length: 100}}\n", "rules:\n  MD013: &off {enabled: false}\n  MD003: *off\n"} {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", availableConfiguration+rules)
		putConfigurationFile(t, root, "good.md", "# good\r\n")
		run := runConfiguration(t, binary, root, root, "lint", "--format=json", "good.md")
		if run.Status != 15 || run.Result.Status != "incomplete" || run.Result.Complete || len(run.Result.CheckedFiles) != 1 {
			t.Fatalf("valid configuration/default coverage: %+v", run)
		}
		md005 := false
		for _, c := range run.Result.Coverage {
			if c.Rule == "MD009" {
				md005 = true
			}
			if c.Rule == "MD013" {
				t.Fatal("disabled MD013 incurred coverage")
			}
		}
		if !md005 {
			t.Fatal("implicit enabled MD009 disappeared")
		}
	}
	root := configurationProject(t)
	raw := "version: 1\r\n# 雪\r\nrules:\r\n  MD013: {length: 100, length: 100}\r\nflavor: commonmark\r\ndirectives: {enabled: false}\r\n"
	putConfigurationFile(t, root, ".mdchk.yaml", raw)
	run := runConfiguration(t, binary, root, root, "lint", "--format=json", "missing.md")
	if run.Status != 12 || len(run.Result.Errors) != 2 {
		t.Fatalf("duplicate result: %+v", run)
	}
	e := run.Result.Errors[0]
	if e.Range == nil || e.Range.Start.Line != 4 || e.Range.Start.Column != 11 || run.Result.Errors[1].Range == nil || run.Result.Errors[1].Range.Start.Line != 4 || run.Result.Errors[1].Range.Start.Column != 24 || e.Snapshot == nil || e.Snapshot.SizeBytes != len(raw) || e.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(raw))) || !strings.Contains(e.Message, "length") {
		t.Fatalf("original duplicate location: %+v", e)
	}
}

// Check linked records against the actual process fixture, independently of the
// individual scenario assertions and of the optional Python schema validator.
func checkConfigurationLinks(t *testing.T, cwd string, run configurationRun) {
	t.Helper()
	r := run.Result
	ids := map[string]bool{}
	for _, e := range r.Errors {
		ids[e.ID] = true
	}
	for _, i := range r.Interruptions {
		ids[i.ID] = true
	}
	coverage := map[string]bool{}
	for _, c := range r.Coverage {
		coverage[c.ID] = true
		for _, cause := range c.CauseIDs {
			if !ids[cause] {
				t.Fatalf("dangling cause %q", cause)
			}
		}
	}
	checkSnapshot := func(path string, snapshot *result.Snapshot) {
		if snapshot == nil {
			return
		}
		if path == "" || filepath.IsAbs(path) || strings.Contains(path, "\\") {
			t.Fatalf("invalid snapshot path %q", path)
		}
		//nolint:gosec // Report paths refer to the controlled fixture, whose bytes are retained.
		data, err := os.ReadFile(filepath.Join(cwd, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("snapshot source %s: %v", path, err)
		}
		if snapshot.View != "original" || snapshot.SizeBytes != len(data) || snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
			t.Fatalf("snapshot differs from fixture: %+v", snapshot)
		}
	}
	for _, e := range r.Errors {
		checkSnapshot(e.Path, e.Snapshot)
	}
	for _, c := range r.Coverage {
		checkSnapshot(c.Path, c.Snapshot)
	}
	for _, d := range r.Diagnostics {
		checkSnapshot(d.Path, &d.Snapshot)
	}
	for _, f := range r.Files {
		checkSnapshot(f.Path, f.InputSnapshot)
		for _, id := range f.ErrorIDs {
			if !ids[id] {
				t.Fatalf("dangling file error %q", id)
			}
		}
		for _, id := range f.CoverageIDs {
			if !coverage[id] {
				t.Fatalf("dangling file coverage %q", id)
			}
		}
	}
	expected := "error"
	switch run.Status {
	case 0:
		expected = "clean"
	case 10:
		expected = "violations"
	case 15:
		expected = "incomplete"
	}
	if r.Status != expected {
		t.Fatalf("process status %d disagrees with report %s", run.Status, r.Status)
	}
	if (run.Status == 0 || run.Status == 10) && !r.Complete {
		t.Fatal("successful completed process reported incomplete")
	}
	if run.Status == 15 && r.Complete {
		t.Fatal("partial process reported complete")
	}
}
