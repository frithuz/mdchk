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
