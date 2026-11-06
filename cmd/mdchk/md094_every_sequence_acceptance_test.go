package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/result"
)

// These tests carry the TestCompiledFix prefix so that factory/checks.sh also
// runs their emitted JSON through the schema and semantic validator.

var md094ThreeSequences = []byte("a\xffb\xe2\x82c\xff\r\nd\n")

var md094ThreeMessages = []struct {
	column  int
	message string
}{
	{2, "Invalid UTF-8 byte sequence 0xFF at byte offset 1."},
	{4, "Invalid UTF-8 byte sequence 0xE2 0x82 at byte offset 3."},
	{6, "Invalid UTF-8 byte sequence 0xFF at byte offset 6."},
}

// md094Streams keeps stderr, which editor formats use for operational records.
func md094Streams(t *testing.T, binary, cwd string, args ...string) (int, string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	//nolint:gosec // Execute only our compiled binary with test-owned arguments and fixtures.
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = cwd
	command.Env = md014NativeEnvironment(t, cwd)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	status := 0
	if err := command.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatal(err)
		}
		status = exit.ExitCode()
	}
	return status, stdout.String(), stderr.String()
}

// md094JSON runs one JSON report through both validators for its operation.
func md094JSON(t *testing.T, binary, root string, args ...string) (int, result.Result) {
	t.Helper()
	status, stdout, stderr := md094Streams(t, binary, root, append(args, "--format=json")...)
	if stderr != "" {
		t.Fatal(args, stderr)
	}
	run := configurationRun{Status: status, Stdout: stdout}
	if args[0] == "fix" {
		return status, md094FixJSON(t, run)
	}
	return status, md094ResultJSON(t, run, root, args[0])
}

func md094CheckThree(t *testing.T, errors []result.Error, scope string) {
	t.Helper()
	if len(errors) != 3 {
		t.Fatal(errors)
	}
	for i, span := range []result.ByteRange{{Start: 1, End: 2}, {Start: 3, End: 5}, {Start: 6, End: 7}} {
		e, want := errors[i], md094ThreeMessages[i]
		point := result.Position{Line: 1, Column: want.column}
		if e.Code != "invalid-utf8" || e.Scope != scope || e.Message != want.message || e.ByteOffset == nil || *e.ByteOffset != span.Start || e.ByteRange == nil || *e.ByteRange != span || e.Range == nil || e.Range.Start != point || e.Range.End != point {
			t.Fatalf("%d: %+v", i, e)
		}
	}
}

func TestCompiledFixMD094EveryFormatAndCommand(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	files := map[string][]byte{"bad.md": md094ThreeSequences}
	for _, command := range [][]string{{"lint"}, {"fix"}, {"fix", "--dry-run"}} {
		t.Run(strings.Join(command, " "), func(t *testing.T) {
			t.Parallel()
			root := md094Project(t, md094Configuration(""), files)
			target := append(slices.Clone(command), "bad.md")
			status, out := md094JSON(t, binary, root, target...)
			if status != 13 {
				t.Fatal(status)
			}
			md094CheckThree(t, out.Errors, "operation")
			md094CheckFailedFile(t, out, "bad.md")

			status, stdout, _ := md094Streams(t, binary, root, target...)
			if status != 13 {
				t.Fatal("text", status)
			}
			for _, want := range md094ThreeMessages {
				if line := fmt.Sprintf("bad.md:1:%d error invalid-utf8 (io): %s\n", want.column, want.message); !strings.Contains(stdout, line) {
					t.Fatal("text", line, stdout)
				}
			}

			status, stdout, _ = md094Streams(t, binary, root, append(target, "--format=vscode")...)
			if status != 13 {
				t.Fatal("vscode", status)
			}
			for _, want := range md094ThreeMessages {
				if line := fmt.Sprintf("bad.md:1:%d: error: invalid-utf8: %s\n", want.column, want.message); !strings.Contains(stdout, line) {
					t.Fatal("vscode", line, stdout)
				}
			}

			status, stdout, _ = md094Streams(t, binary, root, append(target, "--format=github")...)
			if status != 13 {
				t.Fatal("github", status)
			}
			for _, want := range md094ThreeMessages {
				if line := "::error file=bad.md,line=1,title=invalid-utf8::io " + want.message + "\n"; strings.Count(stdout, line) != 1 {
					t.Fatal("github", line, stdout)
				}
			}
		})
	}
	root := md094Project(t, md094Configuration(""), files)
	status, out := md094JSON(t, binary, root, "inspect", "bad.md")
	if status != 0 || out.Status != "inspected" || out.Complete {
		t.Fatal(status, out.Status, out.Complete)
	}
	md094CheckThree(t, out.Errors, "observation")
	status, stdout, stderr := md094Streams(t, binary, root, "inspect", "bad.md")
	if status != 0 || stderr != "" {
		t.Fatal(status, stderr)
	}
	for _, want := range md094ThreeMessages {
		if line := fmt.Sprintf("bad.md:1:%d error invalid-utf8 (io): %s\n", want.column, want.message); !strings.Contains(stdout, line) {
			t.Fatal("inspect text", line, stdout)
		}
	}
}

func TestCompiledFixMD094GateUnchanged(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	files := map[string][]byte{"bad.md": md094ThreeSequences}
	enabledRoot := md094Project(t, md094Configuration("{enabled: true}"), files)
	disabledRoot := md094Project(t, md094Configuration("{enabled: false}"), files)
	for _, command := range [][]string{{"lint", "bad.md"}, {"fix", "--dry-run", "bad.md"}, {"inspect", "bad.md"}} {
		enabledStatus, enabled := md094JSON(t, binary, enabledRoot, command...)
		disabledStatus, disabled := md094JSON(t, binary, disabledRoot, command...)
		if enabledStatus != disabledStatus {
			t.Fatal(command, enabledStatus, disabledStatus)
		}
		if len(enabled.Diagnostics)+len(enabled.SuppressedDiagnostics) != 0 || enabled.Files[0].Analysis != "failed" || len(enabled.Errors) != 3 {
			t.Fatal(command, enabled)
		}
		var decoding []result.Coverage
		for _, c := range enabled.Coverage {
			if c.Reason == "decoding-failed" {
				decoding = append(decoding, c)
			}
		}
		ids := []string{enabled.Errors[0].ID, enabled.Errors[1].ID, enabled.Errors[2].ID}
		if len(decoding) != 1 || !md094SameIDs(decoding[0].CauseIDs, ids) || !md094SameIDs(enabled.Files[0].ErrorIDs, ids) {
			t.Fatal(command, enabled.Coverage, enabled.Files)
		}
		for i := range enabled.Errors {
			if enabled.Errors[i].Rule != "MD094" || disabled.Errors[i].Rule != "" {
				t.Fatal(command, enabled.Errors[i], disabled.Errors[i])
			}
			disabled.Errors[i].Rule = "MD094"
		}
		if !reflect.DeepEqual(enabled.Errors, disabled.Errors) || !reflect.DeepEqual(enabled.Coverage, disabled.Coverage) || !reflect.DeepEqual(enabled.Files, disabled.Files) || !reflect.DeepEqual(enabled.Omissions, disabled.Omissions) || enabled.Status != disabled.Status || enabled.Complete != disabled.Complete {
			t.Fatal(command, enabled, disabled)
		}
	}
	// A valid sibling is still analysed and its findings reported.
	conf := strings.Replace(md094Configuration(""), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	root := md094Project(t, conf, map[string][]byte{"bad.md": md094ThreeSequences, "ok.md": []byte("ok")})
	status, out := md094JSON(t, binary, root, "lint", "bad.md", "ok.md")
	if status != 13 || len(out.Errors) != 3 || len(out.Diagnostics) != 1 || out.Diagnostics[0].Path != "ok.md" {
		t.Fatal(status, out)
	}
}

func TestCompiledFixMD094StaysBlocked(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	conf := strings.Replace(md094Configuration(""), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	root := md094Project(t, conf, map[string][]byte{"bad.md": md094ThreeSequences, "ok.md": []byte("ok")})
	before := sha256.Sum256(md094ThreeSequences)
	read := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join(root, name)) //nolint:gosec // Test-owned fixture path.
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	status, out := md094JSON(t, binary, root, "fix", "--dry-run", "bad.md")
	if status != 13 || len(out.Changes)+len(out.Documents) != 0 || sha256.Sum256(read("bad.md")) != before {
		t.Fatal(status, out)
	}
	status, out = md094JSON(t, binary, root, "fix", "bad.md")
	if status != 13 || len(out.Changes)+len(out.Documents) != 0 || sha256.Sum256(read("bad.md")) != before {
		t.Fatal(status, out)
	}
	status, stdout, _ := md094Streams(t, binary, root, "fix", "--dry-run", "bad.md")
	if status != 13 || strings.Contains(stdout, "--- bad.md") {
		t.Fatal(status, stdout)
	}
	// A valid independent target in the same run is still fixed.
	status, out = md094JSON(t, binary, root, "fix", "bad.md", "ok.md")
	if status != 13 || len(out.Changes) != 1 || out.Changes[0].Path != "ok.md" || sha256.Sum256(read("bad.md")) != before || string(read("ok.md")) != "ok\n" {
		t.Fatal(status, out)
	}
}

func md094CheckCapOmission(t *testing.T, out result.Result, path string, count int) {
	t.Helper()
	for _, o := range out.Omissions {
		if o.Path == path {
			if o.Section != "/errors" || o.Reason != "output-limit" || o.Count == nil || *o.Count != count {
				t.Fatal(o)
			}
			return
		}
	}
	t.Fatal("no omission for", path, out.Omissions)
}

func TestCompiledFixMD094FileCap(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	root := md094Project(t, md094Configuration(""), map[string][]byte{
		"exact.md": bytes.Repeat([]byte{0xFF}, 1000),
		"over.md":  bytes.Repeat([]byte{0xFF}, 1001),
	})
	for _, command := range [][]string{{"lint"}, {"fix", "--dry-run"}} {
		status, out := md094JSON(t, binary, root, append(slices.Clone(command), "exact.md")...)
		if status != 13 || len(out.Errors) != 1000 || len(out.Omissions) != 0 {
			t.Fatal(command, status, len(out.Errors), out.Omissions)
		}
		status, out = md094JSON(t, binary, root, append(slices.Clone(command), "over.md")...)
		if status != 13 || len(out.Errors) != 1000 || len(out.Omissions) != 1 || out.Complete {
			t.Fatal(command, status, len(out.Errors), out.Omissions)
		}
		md094CheckCapOmission(t, out, "over.md", 1)
		target := append(slices.Clone(command), "over.md")
		_, stdout, _ := md094Streams(t, binary, root, target...)
		if !strings.Contains(stdout, "omitted 1 /errors over.md (output-limit)") {
			t.Fatal("text omission", command)
		}
		_, _, stderr := md094Streams(t, binary, root, append(target, "--format=vscode")...)
		if !strings.Contains(stderr, "omitted 1 /errors over.md output-limit") {
			t.Fatal("vscode omission", command, stderr)
		}
	}
	status, stdout, _ := md094Streams(t, binary, root, "inspect", "over.md")
	if status != 0 || !strings.Contains(stdout, "omitted 1 /errors over.md (output-limit)") {
		t.Fatal("inspect text omission", status)
	}
}

func TestCompiledFixMD094DocumentLimit(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	root := md094Project(t, md094Configuration(""), map[string][]byte{"bad.md": bytes.Repeat([]byte{0xFF}, 1048576)})
	status, stdout, stderr := md094Streams(t, binary, root, "lint", "bad.md", "--format=json")
	run := configurationRun{Status: status, Stdout: stdout}
	out := md094ResultJSON(t, run, root, "lint")
	if status != 13 || stderr != "" || len(out.Errors) != 1000 || out.Complete || int64(len(stdout)) > 8<<20 {
		t.Fatal(status, stderr, len(out.Errors), out.Complete, len(stdout))
	}
	for i, e := range out.Errors {
		if *e.ByteOffset != i {
			t.Fatal(i, e)
		}
	}
	md094CheckCapOmission(t, out, "bad.md", 1047576)
	status, inspected := md094JSON(t, binary, root, "inspect", "bad.md")
	if status != 0 || inspected.Status != "inspected" || inspected.Complete || len(inspected.Errors) != 1000 {
		t.Fatal(status, inspected.Status, inspected.Complete, len(inspected.Errors))
	}
	md094CheckCapOmission(t, inspected, "bad.md", 1047576)
}

func TestCompiledFixMD094OperationCap(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	files, targets := map[string][]byte{}, []string{}
	for i := range 12 {
		name := fmt.Sprintf("f%02d.md", i)
		files[name] = bytes.Repeat([]byte{0xFF}, 1500)
		targets = append(targets, name)
	}
	root := md094Project(t, md094Configuration(""), files)
	var retained [][]result.Error
	for _, command := range [][]string{{"lint"}, {"fix", "--dry-run"}} {
		status, out := md094JSON(t, binary, root, append(slices.Clone(command), targets...)...)
		if status != 13 || len(out.Errors) != 10002 {
			t.Fatal(command, status, len(out.Errors))
		}
		perFile := map[string]int{}
		for _, e := range out.Errors {
			perFile[e.Path]++
		}
		for i, name := range targets {
			want := 1000
			if i >= 10 {
				want = 1
			}
			if perFile[name] != want {
				t.Fatal(command, name, perFile[name])
			}
			md094CheckCapOmission(t, out, name, 1500-want)
		}
		for _, f := range out.Files {
			if len(f.ErrorIDs) == 0 {
				t.Fatal(command, f)
			}
		}
		for _, c := range out.Coverage {
			if c.Reason == "decoding-failed" && len(c.CauseIDs) == 0 {
				t.Fatal(command, c)
			}
		}
		locations := make([]result.Error, len(out.Errors))
		for i, e := range out.Errors {
			locations[i] = result.Error{Path: e.Path, Message: e.Message}
		}
		retained = append(retained, locations)
	}
	if !reflect.DeepEqual(retained[0], retained[1]) {
		t.Fatal("fix and lint retained different records")
	}
}

func TestCompiledFixMD094TightResponseLimit(t *testing.T) {
	t.Parallel()
	// A bound of exactly 65,536 equals the minimum report-reserve-bytes, so the
	// documented pre-analysis manifest check refuses every target with exit 15. This
	// is the nearest round bound all three commands admit.
	const bound = 70000
	limit := fmt.Sprintf("--limit=response-bytes=%d", bound)
	binary := sharedExecutable(t)
	root := md094Project(t, md094Configuration(""), map[string][]byte{"bad.md": bytes.Repeat([]byte{0xFF}, 1048576)})
	for _, command := range [][]string{{"lint"}, {"fix", "--dry-run"}} {
		for _, format := range []string{"text", "json", "vscode", "github"} {
			args := append(slices.Clone(command), "bad.md", "--format="+format, limit)
			status, stdout, stderr := md094Streams(t, binary, root, args...)
			if status != 13 || len(stdout)+len(stderr) > bound {
				t.Fatal(args, status, len(stdout), len(stderr))
			}
			if !strings.Contains(stdout+stderr, "/errors") {
				t.Fatal(args, "no /errors omission")
			}
			if format != "json" {
				continue
			}
			run := configurationRun{Status: status, Stdout: stdout}
			out := validateConfigurationOperationJSON(t, []byte(run.Stdout), command[0])
			if len(out.Errors) == 0 || *out.Errors[0].ByteOffset != 0 {
				t.Fatal(args, out.Errors)
			}
			decoding := 0
			for _, c := range out.Coverage {
				if c.Reason == "decoding-failed" {
					decoding++
					if !md094SameIDs(c.CauseIDs, out.Files[0].ErrorIDs) {
						t.Fatal(args, c)
					}
				}
			}
			if decoding != 1 {
				t.Fatal(args, out.Coverage)
			}
			exact := false
			for _, o := range out.Omissions {
				if o.Section == "/errors" && o.Path == "" && o.Reason == "output-limit" && o.Count != nil && *o.Count == 1000-len(out.Errors) {
					exact = true
				}
			}
			if !exact {
				t.Fatal(args, len(out.Errors), out.Omissions)
			}
			md094CheckCapOmission(t, out, "bad.md", 1047576)
		}
	}
	status, stdout, stderr := md094Streams(t, binary, root, "inspect", "bad.md", "--format=json", limit)
	if status != 15 || stderr != "" || len(stdout) > bound {
		t.Fatal(status, stderr, len(stdout))
	}
	validateConfigurationOperationJSON(t, []byte(stdout), "inspect")
}

// A bound under each file's first error still gets a report, not just an exit code.
func TestCompiledFixMD094MinimalLintReport(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	files, targets := map[string][]byte{}, []string{}
	for i := range 1000 {
		name := fmt.Sprintf("f%04d.md", i)
		files[name] = bytes.Repeat([]byte{0xFF}, 1100)
		targets = append(targets, name)
	}
	root := md094Project(t, md094Configuration(""), files)
	lint := append([]string{"lint"}, targets...)
	for _, bound := range []int{230000, 260000, 320000, 500000, 680000} {
		limit := fmt.Sprintf("--limit=response-bytes=%d", bound)
		status, stdout, stderr := md094Streams(t, binary, root, append(slices.Clone(lint), "--format=json", limit)...)
		if status != 13 || stderr != "" || len(stdout) == 0 || len(stdout) > bound {
			t.Fatal(bound, status, stderr, len(stdout))
		}
		out := validateConfigurationOperationJSON(t, []byte(stdout), "lint")
		if len(out.Files) != 1000 || out.Complete || out.Status != "error" {
			t.Fatal(bound, len(out.Files), out.Complete, out.Status)
		}
		for i, f := range out.Files {
			if f.Path != targets[i] || f.Analysis != "failed" {
				t.Fatal(bound, f)
			}
		}
		status, stdout, stderr = md094Streams(t, binary, root, append(slices.Clone(lint), limit)...)
		if status != 13 || stderr != "" || !strings.Contains(stdout, "/errors") || len(stdout) > bound {
			t.Fatal("text", bound, status, stderr, len(stdout))
		}
	}
}

// Per-file omissions grow with the number of invalid files. They must not be what
// empties a report whose first errors and file outcomes would fit.
func TestCompiledFixMD094ManyFilesStillReport(t *testing.T) {
	t.Parallel()
	const bound = 480000
	binary := sharedExecutable(t)
	files, targets := map[string][]byte{}, []string{}
	for i := range 600 {
		name := fmt.Sprintf("f%03d.md", i)
		files[name] = bytes.Repeat([]byte{0xFF}, 1100)
		targets = append(targets, name)
	}
	root := md094Project(t, md094Configuration(""), files)
	args := append(append([]string{"lint"}, targets...), "--format=json", fmt.Sprintf("--limit=response-bytes=%d", bound))
	status, stdout, stderr := md094Streams(t, binary, root, args...)
	if status != 13 || stderr != "" || len(stdout) == 0 || len(stdout) > bound {
		t.Fatal(status, stderr, len(stdout))
	}
	out := validateConfigurationOperationJSON(t, []byte(stdout), "lint")
	first := map[string]bool{}
	for _, e := range out.Errors {
		first[e.Path] = true
	}
	if len(first) != 600 {
		t.Fatal(len(first))
	}
	total := 0
	for _, o := range out.Omissions {
		if o.Path != "" {
			t.Fatal(o)
		}
		if strings.HasPrefix(o.Message, "invalid UTF-8 records past the retention cap") {
			total += *o.Count
		}
	}
	// The first ten files spend the operation allowance; the rest keep one error.
	if total != 10*100+590*1099 {
		t.Fatal(total, out.Omissions)
	}
}

// Logical order puts ../ro/z.md before a.md and native order after it. A failed
// write must still report against the right targets, next to a blocked invalid file.
func TestCompiledFixMD094PartialWriteAcrossPathOrders(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test user can't write to")
	}
	binary := sharedExecutable(t)
	conf := strings.Replace(md094Configuration(""), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	root := md094Project(t, conf, nil)
	work := filepath.Join(root, "c")
	readOnly := filepath.Join(root, "ro")
	for _, dir := range []string{work, readOnly} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	bad := []byte("x\xff\n")
	for path, contents := range map[string][]byte{filepath.Join(work, "a.md"): []byte("a"), filepath.Join(readOnly, "z.md"): []byte("z"), filepath.Join(readOnly, "bad.md"): bad} {
		if err := os.WriteFile(path, contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(readOnly, 0500); err != nil { //nolint:gosec // The test needs an unwritable directory.
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0700) }) //nolint:gosec // Restores the fixture so it can be removed.
	status, stdout, stderr := md094Streams(t, binary, work, "fix", "../ro/bad.md", "../ro/z.md", "a.md", "--format=json")
	if status != 13 || stderr != "" {
		t.Fatal(status, stderr, stdout)
	}
	out := validateConfigurationOperationJSON(t, []byte(stdout), "fix")
	outcome := map[string]result.File{}
	for _, f := range out.Files {
		outcome[f.Path] = f
	}
	if outcome["a.md"].WriteStatus != "written" || outcome["../ro/z.md"].WriteStatus == "written" || outcome["../ro/bad.md"].Analysis != "failed" || len(outcome["../ro/bad.md"].ChangeIDs) != 0 {
		t.Fatal(outcome)
	}
	for _, e := range out.Errors {
		if e.Code == "invalid-utf8" && e.Path != "../ro/bad.md" {
			t.Fatal(e)
		}
	}
	for path, want := range map[string]string{filepath.Join(work, "a.md"): "a\n", filepath.Join(readOnly, "z.md"): "z", filepath.Join(readOnly, "bad.md"): string(bad)} {
		got, err := os.ReadFile(path) //nolint:gosec // Test-owned fixture path.
		if err != nil || string(got) != want {
			t.Fatal(path, err, string(got))
		}
	}
}
