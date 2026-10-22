package cli

import (
	"bytes"
	"context"
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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/result"
)

type editorExecution struct {
	status         int
	stdout, stderr string
}

// Explicit -o paths do not acquire Go's default Windows executable suffix.
func editorExecutableName(goos string) string {
	if goos == "windows" {
		return "mdchk.exe"
	}
	return "mdchk"
}

func TestEditorExecutableNativeName(t *testing.T) {
	for _, test := range []struct{ goos, name string }{
		{"windows", "mdchk.exe"}, {"darwin", "mdchk"}, {"linux", "mdchk"},
	} {
		if got := editorExecutableName(test.goos); got != test.name {
			t.Fatalf("%s output name %q want %q", test.goos, got, test.name)
		}
	}
}

func editorExecutable(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), editorExecutableName(runtime.GOOS))
	command := exec.Command("go", "build", "-o", binary, "./cmd/mdchk") //nolint:gosec // Fixed package and test-owned executable destination.
	command.Dir = filepath.Join(executionPackageDir, "../..")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	return binary
}
func editorExecute(t *testing.T, binary, root string, args ...string) editorExecution {
	t.Helper()
	command := exec.Command(binary, args...) //nolint:gosec // Test-owned compiled binary and fixture arguments.
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	command.Dir = physicalRoot
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	status := 0
	if err := command.Run(); err != nil {
		var failure *exec.ExitError
		if !errors.As(err, &failure) {
			t.Fatal(err)
		}
		status = failure.ExitCode()
	}
	return editorExecution{status, stdout.String(), stderr.String()}
}
func editorObservations(t *testing.T, format, stdout string) []workflowCommand {
	t.Helper()
	var records []workflowCommand
	for _, line := range strings.Split(strings.TrimSuffix(stdout, "\n"), "\n") {
		if line == "" {
			continue
		}
		if format == "github" {
			command := parseWorkflow(line)
			if command == nil || command.name != "error" {
				t.Fatalf("unexpected workflow output %q", line)
			}
			records = append(records, *command)
		} else {
			fields := documentedMatcher(t).FindStringSubmatch(line)
			if len(fields) != 7 {
				t.Fatalf("unmatched diagnostic %q", line)
			}
			records = append(records, workflowCommand{name: "error", data: fields[6], properties: map[string]string{"file": fields[1], "line": fields[2], "column": fields[3], "title": fields[5]}})
		}
	}
	return records
}
func assertEditorTruth(t *testing.T, format string, got editorExecution, truth result.Result) {
	t.Helper()
	records := editorObservations(t, format, got.stdout)
	expected := 0
	check := func(path, code, message string, at *result.Position) {
		located := path != "" && at != nil && at.Line > 0
		outside := path == ".." || strings.HasPrefix(path, "../")
		if format == "vscode" && !located {
			if !strings.Contains(got.stderr, code) {
				t.Fatalf("missing stderr %s: %+v", code, got)
			}
			return
		}
		expected++
		for _, record := range records {
			if record.properties["title"] != code {
				continue
			}
			if located && !(format == "github" && outside) {
				if record.properties["file"] != path || record.properties["line"] != strconv.Itoa(at.Line) {
					continue
				}
				if format == "vscode" && record.properties["column"] != strconv.Itoa(at.Column) {
					continue
				}
			} else if record.properties["file"] != "" || record.properties["line"] != "" {
				continue
			}
			// These fixtures have ordinary messages except the deliberately hostile filename.
			if !strings.Contains(record.data, strings.ReplaceAll(message, ":", func() string {
				if format == "vscode" {
					return `\x3a`
				}
				return ":"
			}())) {
				continue
			}
			if format == "github" && outside && !strings.Contains(record.data, "Location unavailable") {
				continue
			}
			return
		}
		t.Fatalf("missing canonical observation %s %s %v: %+v", path, code, at, got)
	}
	for _, d := range truth.Diagnostics {
		at := d.Range.Start
		check(d.Path, d.Rule, d.Message, &at)
	}
	for _, e := range truth.Errors {
		var at *result.Position
		if e.Range != nil {
			at = &e.Range.Start
		}
		check(e.Path, e.Code, e.Message, at)
	}
	// Additional GitHub coverage/stops/incomplete annotations are independently
	// recognized above; clean complete results must remain completely silent.
	if len(truth.Coverage) == 0 && len(truth.Interruptions) == 0 && len(truth.Omissions) == 0 && truth.Complete && len(records) != expected {
		t.Fatalf("records %d want %d: %+v", len(records), expected, got)
	}
	if expected == 0 && len(truth.Errors) == 0 && truth.Complete && len(truth.Coverage) == 0 && got.stderr != "" {
		t.Fatalf("clean stderr %+v", got)
	}
	if format == "github" {
		for _, line := range strings.Split(got.stderr, "\n") {
			if parseWorkflow(line) != nil {
				t.Fatalf("human workflow command %q", line)
			}
		}
	}
}

func TestCompiledEditorCommandsCanonicalTruth(t *testing.T) {
	binary := editorExecutable(t)
	cases := []struct {
		name, raw, configuration string
		args                     []string
		status                   int
	}{
		{"lint-Unicode", "\t雪😀x", fixConfiguration(), []string{"lint", "nested/doc.md"}, 10},
		{"lint-clean", "clean\n", fixConfiguration(), []string{"lint", "nested/doc.md"}, 0},
		{"lint-coverage", "clean\n", availableConfig, []string{"lint", "nested/doc.md"}, 15},
		{"safe-preview", "UNIQUE raw Markdown", fixConfiguration(), []string{"fix", "--dry-run", "nested/doc.md"}, 0},
		{"protected-preview", "\n<!-- 😀x", fixConfiguration(), []string{"fix", "--dry-run", "nested/doc.md"}, 10},
		{"apply", "safe", fixConfiguration(), []string{"fix", "nested/doc.md"}, -1},
		{"config-invalid", "doc", "version: 1\nflavor: commonmark\nrules:\n  MD013: {length: -1}\n", []string{"config", "validate", "nested"}, 12},
		{"config-clean", "doc", fixConfiguration(), []string{"config", "validate", "nested"}, 0},
		{"missing-target", "doc", fixConfiguration(), []string{"lint", "missing.md"}, 13},
		{"empty-lint", "doc", fixConfiguration(), []string{"lint", "empty"}, 11},
		{"empty-fix", "doc", fixConfiguration(), []string{"fix", "empty"}, 11},
		{"config-early", "doc", "invalid: [", []string{"lint", "nested/doc.md"}, 12},
		{"config-quota", "doc", fixConfiguration(), []string{"config", "validate", "nested", "--limit=work-units=1"}, 15},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var truth result.Result
			expectedStatus := tc.status
			root := t.TempDir()
			root, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(root, "empty"), 0700); err != nil {
				t.Fatal(err)
			}
			for _, format := range []string{"json", "text", "vscode", "github"} {
				put(t, filepath.Join(root, ".mdchk.yaml"), tc.configuration)
				put(t, filepath.Join(root, "nested/.mdchk.yaml"), tc.configuration)
				put(t, filepath.Join(root, "nested/doc.md"), tc.raw)
				before, err := os.Stat(filepath.Join(root, "nested/doc.md"))
				if err != nil {
					t.Fatal(err)
				}
				args := append(append([]string{}, tc.args...), "--format="+format)
				got := editorExecute(t, binary, root, args...)
				if format == "json" {
					truth = validateEmitted(t, t.TempDir(), []byte(got.stdout))
					if expectedStatus == -1 {
						expectedStatus = got.status
						if expectedStatus != 0 && expectedStatus != 13 {
							t.Fatal(got)
						}
					}
				}
				if got.status != expectedStatus {
					t.Fatalf("%s status want%d got%+v", format, expectedStatus, got)
				}
				if format == "vscode" || format == "github" {
					assertEditorTruth(t, format, got, truth)
					if strings.Contains(got.stdout+got.stderr, "UNIQUE raw Markdown") || strings.Contains(got.stdout, "@@") {
						t.Fatal("editor preview leaked Markdown/diff", got)
					}
				} else if got.stderr != "" {
					t.Fatal("legacy stderr", got)
				}
				after, err := os.ReadFile(filepath.Join(root, "nested/doc.md"))
				if err != nil {
					t.Fatal(err)
				}
				afterInfo, statErr := os.Stat(filepath.Join(root, "nested/doc.md"))
				if statErr != nil || before.Mode() != afterInfo.Mode() {
					t.Fatal("mode preservation", statErr)
				}
				if tc.name != "apply" && !os.SameFile(before, afterInfo) {
					t.Fatal("read-only command replaced target")
				}
				want := tc.raw
				if tc.name == "apply" && expectedStatus == 0 {
					want += "\n"
					again := editorExecute(t, binary, root, args...)
					if again.status != 0 {
						t.Fatal(again)
					}
					if (format == "github" || format == "vscode") && again.stdout+again.stderr != "" {
						t.Fatal("non-idempotent editor", again)
					}
				}
				if string(after) != want {
					t.Fatalf("unexpected bytes %q want%q", after, want)
				}
			}
		})
	}
}

func TestCompiledEditorPathAuthorityAndDelivery(t *testing.T) {
	binary := editorExecutable(t)
	outer := t.TempDir()
	outer, err := filepath.EvalSymlinks(outer)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(outer, "checkout")
	put(t, filepath.Join(root, "nested/.mdchk.yaml"), fixConfiguration())
	put(t, filepath.Join(root, "nested/doc.md"), "x")
	put(t, filepath.Join(outer, "outside.md"), "x")
	put(t, filepath.Join(outer, "external/.mdchk.yaml"), "version: 1\nflavor: commonmark\nrules:\n  MD013: {length: -1}\n")
	for _, args := range [][]string{
		{"lint", "nested/doc.md", "--config=nested/.mdchk.yaml"},
		{"lint", filepath.Join(root, "nested/doc.md"), "--config=nested/.mdchk.yaml"},
		{"lint", "../outside.md", "--config=nested/.mdchk.yaml"},
		{"config", "validate", "../external"},
	} {
		baseline := editorExecute(t, binary, root, append(append([]string{}, args...), "--format=json")...)
		var truth result.Result
		if err := json.Unmarshal([]byte(baseline.stdout), &truth); err != nil {
			t.Fatal(err, baseline)
		}
		for _, format := range []string{"vscode", "github"} {
			got := editorExecute(t, binary, root, append(append([]string{}, args...), "--format="+format)...)
			if got.status != baseline.status {
				t.Fatal(got, baseline)
			}
			assertEditorTruth(t, format, got, truth)
		}
	}
	if runtime.GOOS != "windows" {
		name := "literal\\backslash:##[group]::stop-commands::.md"
		put(t, filepath.Join(root, name), "x")
		args := []string{"lint", name, "missing-##[error]::group::.md", "--config=nested/.mdchk.yaml"}
		baseline := editorExecute(t, binary, root, append(append([]string{}, args...), "--format=json")...)
		var truth result.Result
		if err := json.Unmarshal([]byte(baseline.stdout), &truth); err != nil {
			t.Fatal(err)
		}
		for _, format := range []string{"vscode", "github"} {
			got := editorExecute(t, binary, root, append(append([]string{}, args...), "--format="+format)...)
			if got.status != 13 {
				t.Fatal(got)
			}
			assertEditorTruth(t, format, got, truth)
		}
	}
	// A real read-only descriptor fails OS delivery; no mock writer is involved.
	//nolint:gosec // Read only the synthetic test fixture for a real descriptor failure.
	readonly, err := os.Open(filepath.Join(root, "nested/doc.md"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := readonly.Close(); err != nil {
			t.Error(err)
		}
	}()
	command := exec.Command(binary, "lint", "nested/doc.md", "--config=nested/.mdchk.yaml", "--format=github") //nolint:gosec // Test-owned executable and fixed arguments.
	command.Dir = root
	command.Stdout = readonly
	command.Stderr = io.Discard
	var failure *exec.ExitError
	if err = command.Run(); !errors.As(err, &failure) || failure.ExitCode() != 13 {
		t.Fatalf("descriptor failure %v", err)
	}
}

func TestEditorCommandArgumentsAndIndependentLifetime(t *testing.T) {
	root := project(t)
	put(t, filepath.Join(root, ".mdchk.yaml"), fixConfiguration())
	put(t, filepath.Join(root, "doc.md"), "x")
	for _, command := range [][]string{{"lint"}, {"fix", "--dry-run"}, {"config", "validate"}, {"unknown"}, {"config", "unknown"}} {
		for _, format := range []string{"vscode", "github"} {
			for _, tail := range [][]string{{"--bad", "--format=" + format}, {"--format", format, "--bad"}, {"--format=bad", "--format=" + format}, {"--limit=bad=1", "--format=" + format}, {"--format=json", "--format=" + format, "--bad"}} {
				args := append(append([]string{}, command...), tail...)
				var stdout, stderr bytes.Buffer
				if status := Run(args, &stdout, &stderr, "test"); status != 11 {
					t.Fatalf("%v %d %s %s", args, status, &stdout, &stderr)
				}
				if format == "vscode" {
					if stdout.Len() != 0 || !strings.Contains(stderr.String(), "usage") {
						t.Fatalf("lost early stderr %v %s %s", args, &stdout, &stderr)
					}
				} else if records := editorObservations(t, format, stdout.String()); len(records) == 0 {
					t.Fatal("lost annotation", args)
				}
			}
		}
	}
	for _, args := range [][]string{{"--config=--format=github"}, {"--", "--format=github"}, {"--format=github", "--format=bad"}} {
		if format := recoverFormat(args); format != "text" {
			t.Fatal(args, format)
		}
	}
	for _, format := range []string{"vscode", "github"} {
		for _, bad := range []bool{false, true} {
			args := []string{"inspect", "missing.md", "--config=missing", "--format=" + format}
			if bad {
				args = append([]string{"inspect", "--bad"}, args[1:]...)
			}
			var stdout, stderr bytes.Buffer
			if status := Run(args, &stdout, &stderr, "test"); status != 11 || strings.Contains(stdout.String(), "::error") || stderr.Len() != 0 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
		}
	}
	for _, command := range [][]string{{"lint", "doc.md"}, {"fix", "--dry-run", "doc.md"}, {"config", "validate"}} {
		for _, format := range []string{"vscode", "github"} {
			for _, reason := range []string{"cancelled", "deadline-exceeded", "limit-exceeded"} {
				ctx := context.Background()
				args := append(append([]string{}, command...), "--format="+format)
				switch reason {
				case "cancelled":
					cancelled, cancel := context.WithCancel(ctx)
					cancel()
					ctx = cancelled
				case "deadline-exceeded":
					expired, cancel := context.WithDeadline(ctx, time.Time{})
					defer cancel()
					ctx = expired
				case "limit-exceeded":
					args = append(args, "--limit=work-units=1")
				}
				var stdout, stderr bytes.Buffer
				status := RunContext(ctx, args, &stdout, &stderr, "test")
				if status != 15 || !strings.Contains(stdout.String()+stderr.String(), reason) {
					t.Fatalf("original cause %v %s status%d %s %s", args, reason, status, &stdout, &stderr)
				}
				if strings.Contains(stdout.String()+stderr.String(), "reporting-time exhausted") {
					t.Fatal("expired operation consumed independent presentation", stdout.String(), stderr.String())
				}
			}
		}
	}
}

func TestEditorRealCommandWriterFailuresAndBounds(t *testing.T) {
	root := project(t)
	put(t, filepath.Join(root, ".mdchk.yaml"), fixConfiguration())
	put(t, filepath.Join(root, "doc.md"), "x")
	for _, short := range []bool{false, true} {
		for _, failStream := range []string{"stdout", "stderr"} {
			bad := &vscodeFailWriter{short: short}
			var good bytes.Buffer
			var stdout, stderr io.Writer = &good, bad
			if failStream == "stdout" {
				stdout, stderr = bad, &good
			}
			// One actual lint produces both a located MD047 finding and an unlocated
			// failed target; VS Code must attempt both real command streams.
			status := Run([]string{"lint", "doc.md", "missing.md", "--format=vscode"}, stdout, stderr, "test")
			if status != 13 || bad.calls == 0 || good.Len() == 0 {
				t.Fatalf("%s short%v status%d calls%d other%q", failStream, short, status, bad.calls, &good)
			}
		}
	}
	for _, format := range []string{"vscode", "github"} {
		for _, timeout := range []string{"1ns", "1us", "2s"} {
			var stdout, stderr bytes.Buffer
			status := Run([]string{"lint", "doc.md", "--format=" + format, "--limit=reporting-time=" + timeout}, &stdout, &stderr, "test")
			if status != 10 && status != 15 {
				t.Fatal(format, timeout, status, stdout.String(), stderr.String())
			}
			if status == 15 && !strings.Contains(stdout.String()+stderr.String(), "reporting") {
				t.Fatal("unexplained reduction")
			}
			if stdout.Len()+stderr.Len() > int(limitDefaults["response-bytes"]) {
				t.Fatal("combined limit")
			}
		}
	}
	// The original config scan's cause is separately preserved under a tiny
	// reporting timer, without creating Markdown coverage or changing precedence.
	for _, format := range []string{"vscode", "github"} {
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		var stdout, stderr bytes.Buffer
		status := RunContext(cancelled, []string{"config", "validate", "--format=" + format, "--limit=reporting-time=1ns"}, &stdout, &stderr, "test")
		if status != 15 || !strings.Contains(stdout.String()+stderr.String(), "cancelled") {
			t.Fatal(status, stdout.String(), stderr.String())
		}
	}
}

func TestEditorFixMandatoryManifestBeforeWrites(t *testing.T) {
	root := project(t)
	put(t, filepath.Join(root, ".mdchk.yaml"), fixConfiguration())
	// Compact editor output cannot authorize writes when the full JSON-sized
	// identity/outcome manifest plus reserve exceeds the response allowance.
	for i := 0; i < 120; i++ {
		put(t, filepath.Join(root, fmt.Sprintf("%03d-%s.md", i, strings.Repeat("x", 160))), "x")
	}
	before := snapshotEditorFiles(t, root)
	for _, format := range []string{"json", "text", "vscode", "github"} {
		var stdout, stderr bytes.Buffer
		status := Run([]string{"fix", ".", "--format=" + format, "--limit=response-bytes=65536"}, &stdout, &stderr, "test")
		if status != 15 || !strings.Contains(stdout.String()+stderr.String(), "mandatory target manifest") {
			t.Fatalf("%s status%d %s %s", format, status, &stdout, &stderr)
		}
		if stdout.Len()+stderr.Len() > 65536 {
			t.Fatal("combined response budget exceeded")
		}
		if after := snapshotEditorFiles(t, root); !reflect.DeepEqual(before, after) {
			t.Fatal("admission failure changed files")
		}
	}
}
func snapshotEditorFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			//nolint:gosec // Enumerate only the synthetic admission fixture.
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			files[path] = fmt.Sprintf("%v:%s", info.Mode(), data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
