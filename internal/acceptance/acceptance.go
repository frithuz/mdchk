// Package acceptance holds the helpers shared by tests that build and run the
// compiled mdchk executable: builds, fixture projects, process runs, report
// validation and filesystem state checks.
//
// It is for tests only. Production code must not import it, and it must not
// import internal/rules or internal/cli, so that tests in those packages can.
package acceptance

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
	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/testutil/filestat"
)

// AvailableConfiguration admits production CommonMark with directives disabled.
const AvailableConfiguration = "version: 1\nflavor: commonmark\ndirectives: {enabled: false}\n"

// Run is the observed outcome of one executable process.
type Run struct {
	Status int
	Stdout string
	Result result.Result
}

// Project creates an empty project root that discovery treats as a repository.
func Project(t *testing.T) string {
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

// PutFile writes contents to the slash-separated name under root.
func PutFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

// CleanConfiguration returns a configuration with every catalog rule disabled.
func CleanConfiguration() string {
	var b strings.Builder
	b.WriteString(AvailableConfiguration + "rules:\n")
	for _, rule := range engine.Catalog() {
		fmt.Fprintf(&b, "  %s: {enabled: false}\n", rule.ID)
	}
	return b.String()
}

// MD047Configuration returns CleanConfiguration with directives enabled and
// MD047 set to enabled and allow_directives as given.
func MD047Configuration(enabled, permission bool) string {
	config := strings.ReplaceAll(CleanConfiguration(), "directives: {enabled: false}", "directives: {enabled: true}")
	return strings.Replace(config, "MD047: {enabled: false}", fmt.Sprintf("MD047: {enabled: %t, allow_directives: %t}", enabled, permission), 1)
}

// RuleConfiguration returns CleanConfiguration with the given flavor and the
// rule id set to setting.
func RuleConfiguration(flavor, id, setting string) string {
	config := strings.Replace(CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
	return strings.Replace(config, id+": {enabled: false}", id+": "+setting, 1)
}

// StableStat returns the file information of an open handle to path.
func StableStat(path string) (os.FileInfo, error) {
	return filestat.StableStat(path)
}

// Digest returns the hexadecimal SHA-256 of text.
func Digest(text string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(text))) }

// Saved returns the bytes of doc.md under root.
func Saved(t *testing.T, root string) string {
	t.Helper()
	//nolint:gosec // Paths originate in the test-owned fixture tree.
	data, err := os.ReadFile(filepath.Join(root, "doc.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// ConfigurationBytes returns a digest of every regular file under root.
func ConfigurationBytes(t *testing.T, root string) map[string][32]byte {
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

// RunProcess runs the actual executable. Callers independently guard fixture
// bytes; metadata and permission tests need different guards.
func RunProcess(t *testing.T, binary, cwd string, args ...string) Run {
	t.Helper()
	return RunProcessEnv(t, binary, cwd, nil, args...)
}

// RunProcessEnv is RunProcess with an explicit environment. A nil env inherits
// the test process environment.
func RunProcessEnv(t *testing.T, binary, cwd string, env []string, args ...string) Run {
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
	out := Run{Stdout: stdout.String()}
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

// ValidateOperationJSON decodes one report for operation and checks its
// envelope. When MDCHK_RESULT_VALIDATOR_PYTHON is set, it also checks the report
// against the result contract.
func ValidateOperationJSON(t *testing.T, data []byte, operation string) result.Result {
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
		validator, err := validatorPath()
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

// validatorPath returns the result contract validator in the current module.
func validatorPath() (string, error) {
	root, err := currentModuleRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "docs", "contracts", "results", "validate.py"), nil
}

// RunFix runs the executable in root and, for --format=json, decodes and
// checks the fix report.
func RunFix(t *testing.T, binary, root string, args ...string) Run {
	t.Helper()
	got := RunProcess(t, binary, root, args...)
	for _, arg := range args {
		if arg == "--format=json" {
			got.Result = ValidateOperationJSON(t, []byte(got.Stdout), "fix")
		}
	}
	return got
}

// RunFixDryRun is RunFix for a run that must leave every file under root
// unchanged.
func RunFixDryRun(t *testing.T, binary, root string, args ...string) Run {
	t.Helper()
	before := ConfigurationBytes(t, root)
	out := RunProcess(t, binary, root, args...)
	after := ConfigurationBytes(t, root)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("dry-run changed directory inventory or bytes")
	}
	for _, arg := range args {
		if arg == "--format=json" {
			out.Result = ValidateOperationJSON(t, []byte(out.Stdout), "fix")
		}
	}
	return out
}

// ValidationEnvironment returns the test process environment without Git,
// home and PATH settings, pointing HOME, XDG_CONFIG_HOME and the Git global
// configuration at a fresh temporary directory and PATH at a missing one.
func ValidationEnvironment(t *testing.T) []string {
	t.Helper()
	var env []string
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		if !strings.HasPrefix(key, "GIT_") && key != "HOME" && key != "XDG_CONFIG_HOME" && key != "PATH" {
			env = append(env, v)
		}
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return append(env, "PATH="+filepath.Join(home, "no-runtime-programs"), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, "xdg"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(home, "gitconfig"), "GIT_TERMINAL_PROMPT=0")
}

// RunInspection runs inspect, requires that it leaves snapshotRoot unchanged and
// decodes the projection from a JSON report.
func RunInspection(t *testing.T, binary, cwd, snapshotRoot string, args ...string) (Run, inspection.Projection) {
	t.Helper()
	before := ConfigurationBytes(t, snapshotRoot)
	beforeModes := InspectionModes(t, snapshotRoot)
	defer func() {
		if !reflect.DeepEqual(before, ConfigurationBytes(t, snapshotRoot)) || !reflect.DeepEqual(beforeModes, InspectionModes(t, snapshotRoot)) {
			t.Error("inspect changed source/configuration bytes or inventory")
		}
	}()
	got := RunProcess(t, binary, cwd, args...)
	p := inspection.Projection{}
	for _, arg := range args {
		if arg == "--format=json" {
			got.Result = ValidateOperationJSON(t, []byte(got.Stdout), "inspect")
			if len(got.Result.Inspection) > 0 {
				if err := json.Unmarshal(got.Result.Inspection, &p); err != nil {
					t.Fatal(err)
				}
			}
			break
		}
	}
	return got, p
}

// InspectionModes returns the mode of every entry under root.
func InspectionModes(t *testing.T, root string) map[string]fs.FileMode {
	t.Helper()
	modes := map[string]fs.FileMode{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		modes[path] = info.Mode()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return modes
}

// MD038NativeRefusal recognizes the explicit unsupported-host refusal. Native
// acquisition can terminate before any document enters validation. Hosts outside
// linux, darwin and windows refuse apply before any analysis; every supported
// host applies, so the callers that use this helper adapt to either.
func MD038NativeRefusal(t *testing.T, got Run, out result.Result) bool {
	t.Helper()
	if len(out.Errors) != 1 || out.Errors[0].Code != "write-acquisition" {
		return false
	}
	e := out.Errors[0]
	if got.Status != 13 || out.Status != "error" || !out.Complete || e.Category != result.IO || e.Scope != "operation" || !strings.HasPrefix(e.Message, "native replacement is unsupported on ") {
		t.Fatal(got)
	}
	if len(out.CheckedFiles)+len(out.Files)+len(out.Diagnostics)+len(out.SuppressedDiagnostics)+len(out.Coverage)+len(out.Changes)+len(out.DeferredFixes)+len(out.Documents)+len(out.Interruptions)+len(out.Omissions) != 0 {
		t.Fatal("unsupported adapter invented analysis", got)
	}
	return true
}

// AssertCGO0Apply verifies that a CGO_ENABLED=0 build applies like any other:
// native replacement is not refused, the operation completes, and the tree
// changes exactly when a file reports that it was written.
func AssertCGO0Apply(t *testing.T, got Run, out result.Result, before, after map[string]ReadinessEntry) {
	t.Helper()
	for _, e := range out.Errors {
		if e.Code == "write-acquisition" {
			t.Fatal("CGO_ENABLED=0 build refused native replacement", got)
		}
	}
	if got.Status == 13 || !out.Complete {
		t.Fatal(got)
	}
	written := false
	for _, f := range out.Files {
		if f.WriteStatus == "written" {
			written = true
		}
	}
	if changed := len(ReadinessDifferences(before, after)) != 0; written != changed {
		t.Fatalf("write status and fixture disagree: written=%t changed=%t\n%v", written, changed, got)
	}
}
