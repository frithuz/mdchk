package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

func validateEmitted(t *testing.T, root string, data []byte) result.Result {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	var out result.Result
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("invalid emitted JSON: %v: %q", err, data)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatalf("multiple or trailing JSON values: %v", err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) || bytes.Contains(data, []byte(":null")) {
		t.Fatalf("invalid framing/required arrays: %s", data)
	}
	if python := os.Getenv("MDCHK_RESULT_VALIDATOR_PYTHON"); python != "" {
		path := filepath.Join(root, "emitted-result.json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		validator := filepath.Join(executionPackageDir, "../../docs/contracts/results/validate.py")
		script := "import json,runpy,sys; v=runpy.run_path(sys.argv[1]); x=json.load(open(sys.argv[2])); v['validator'].validate(x); v['semantic'](x)"
		output, err := exec.Command(python, "-c", script, validator, path).CombinedOutput() //nolint:gosec // Explicit test interpreter and controlled source/fixture paths.
		if err != nil {
			t.Fatalf("emitted schema: %v\n%s", err, output)
		}
	}
	return out
}
func TestReportPresentationRecovery(t *testing.T) {
	root := project(t)
	for _, args := range [][]string{
		{"lint", "--bad", "--format=json"}, {"lint", "--format=json", "--bad"},
		{"lint", "--limit=bad=1", "--format", "json"}, {"lint", "--config", "--format=json"},
		{"lint", "--format=yaml", "--format=json"}, {"unknown", "--format=json"},
		{"--bad", "lint", "--format=json"},
	} {
		var stdout, stderr bytes.Buffer
		if status := Run(args, &stdout, &stderr, "test"); status != 11 || stderr.Len() != 0 {
			t.Fatalf("%v status=%d stderr=%q", args, status, &stderr)
		}
		out := validateEmitted(t, root, stdout.Bytes())
		if len(out.Errors) != 1 || out.Errors[0].Category != result.Usage || out.Status != "error" {
			t.Fatalf("%v: %+v", args, out)
		}
	}
	for _, args := range [][]string{{"--", "--format=json"}, {"--config=--format=json"}, {"--format=json", "--format=text"}, {"--format=json", "--format=bad"}} {
		if got := recoverFormat(args); got != "text" {
			t.Fatalf("%v recovered %s", args, got)
		}
	}
	for _, format := range []string{"vscode", "github"} {
		accessed := false
		status := run([]string{"lint", "--format=" + format}, io.Discard, io.Discard, "test", func(o lintOptions) (*lintInvocation, error) {
			accessed = true
			return &lintInvocation{options: o}, nil
		}, func(*lintInvocation, io.Writer, io.Writer) int { return 0 })
		if status != 0 || !accessed {
			t.Fatalf("format %s project access=%v status=%d", format, accessed, status)
		}
	}
}
func TestReportActualDiagnosticsCoordinatesAndStrings(t *testing.T) {
	root := validProject(t)
	raw := "# 😀é\r\n\t雪 target\n"
	name := "雪\r\nforged.md"
	put(t, filepath.Join(root, name), raw)
	message := "bad\r\nforged diagnostic"
	registry := executionRegistry(t, map[string]engine.Check{"MD001": func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		n := bytes.Index(c.Source().Bytes(), []byte("target"))
		span, err := c.Source().Span(n, n+6)
		if err != nil {
			t.Fatal(err)
		}
		return engine.RuleOutcome{Findings: []engine.Finding{{Message: message, Span: span, FixEligible: true}}}
	}}, false)
	var jsonBytes []byte
	for _, format := range []string{"json", "text"} {
		var stdout, stderr bytes.Buffer
		status := runContext(context.Background(), []string{"lint", "--format", format, name}, &stdout, &stderr, "test", func(o lintOptions) (*lintInvocation, error) {
			return prepareLintWith(o, registry, engine.ValidationProviders{}, config.Capabilities{CommonMark: true})
		}, func(ctx context.Context, inv *lintInvocation, _ io.Writer, w io.Writer) int {
			return acquireLintContext(ctx, inv, w)
		})
		if status != 10 || stderr.Len() != 0 {
			t.Fatalf("%s status=%d stderr=%q", format, status, &stderr)
		}
		if format == "json" {
			jsonBytes = append([]byte{}, stdout.Bytes()...)
			out := validateEmitted(t, root, jsonBytes)
			d := out.Diagnostics[0]
			if d.Path != name || d.Message != message || d.Range.Start.Line != 2 || d.Range.Start.Column != 4 || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(raw))) || d.FixAvailable {
				t.Fatalf("diagnostic: %+v", d)
			}
		} else if strings.Contains(stdout.String(), "\r") || strings.Contains(stdout.String(), "\nforged") || !strings.Contains(stdout.String(), `\r\n`) {
			t.Fatalf("forged physical text line: %q", &stdout)
		}
	}
	if len(jsonBytes) == 0 {
		t.Fatal("no JSON")
	}
}
func TestReportConfigSnapshotAfterSourceReplacement(t *testing.T) {
	root := project(t)
	path := filepath.Join(root, config.Filename)
	raw := "version: 1\r\n# 雪\r\nrules:\r\n  MD013: {length: -1}\r\n"
	put(t, path, raw)
	_, err := prepareLint(lintOptions{})
	if err == nil {
		t.Fatal("expected configuration error")
	}
	put(t, path, "replacement")
	var stdout bytes.Buffer
	if status := reportFailure(err, "config-definition", result.Configuration, "json", &stdout, nil); status != 12 {
		t.Fatal(status)
	}
	out := validateEmitted(t, root, stdout.Bytes())
	e := out.Errors[0]
	if e.Path != config.Filename || e.Snapshot == nil || e.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(raw))) || e.Snapshot.SizeBytes != len(raw) || e.Range == nil || e.Range.Start.Line != 4 || e.Range.Start != e.Range.End {
		t.Fatalf("lost original identity/location: %+v", e)
	}
}

func TestCompiledReportStreamsAndExits(t *testing.T) {
	root := validProject(t)
	binary := filepath.Join(t.TempDir(), "mdchk")
	build := exec.Command("go", "build", "-o", binary, "./cmd/mdchk") //nolint:gosec // Test-owned binary destination, fixed package.
	build.Dir = filepath.Join(executionPackageDir, "../..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	put(t, filepath.Join(root, "good.md"), "# good\n")
	put(t, filepath.Join(root, "bad.md"), "bad\xff")
	if err := os.Mkdir(filepath.Join(root, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	disabled := "rules:\n"
	for _, m := range engine.Catalog() {
		disabled += fmt.Sprintf("  %s: {enabled: false}\n", m.ID)
	}
	cleanConfig := availableConfig + disabled
	put(t, filepath.Join(root, config.Filename), cleanConfig)
	for _, tc := range []struct {
		args   []string
		status int
		json   bool
	}{
		{[]string{"lint", "good.md"}, 0, false},
		{[]string{"lint", "--format=json", "good.md"}, 0, true},
		{[]string{"lint", "--format=json", "missing.md"}, 13, true},
		{[]string{"lint", "--format=json", "empty"}, 11, true},
		{[]string{"lint", "--format=json", "bad.md", "good.md"}, 13, true},
		{[]string{"lint", "--format=json", "--config=absent.yaml"}, 12, true},
		{[]string{"lint", "--format=json", "--bad"}, 11, true},
	} {
		cmd := exec.Command(binary, tc.args...) //nolint:gosec // Test-owned compiled executable and fixed fixture arguments.
		cmd.Dir = root
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		status := 0
		if err != nil {
			var e *exec.ExitError
			if errors.As(err, &e) {
				status = e.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		if status != tc.status || stderr.Len() != 0 {
			t.Fatalf("%v status=%d stderr=%q", tc.args, status, &stderr)
		}
		if tc.json {
			validateEmitted(t, root, stdout.Bytes())
		} else if !strings.Contains(stdout.String(), "clean") {
			t.Fatalf("missing text summary: %q", &stdout)
		}
	}
	put(t, filepath.Join(root, config.Filename), availableConfig)
	for _, format := range []string{"text", "json"} {
		cmd := exec.Command(binary, "lint", "--format="+format, "good.md") //nolint:gosec // Controlled compiled fixture.
		cmd.Dir = root
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		var processExit *exec.ExitError
		if err := cmd.Run(); !errors.As(err, &processExit) || processExit.ExitCode() != 15 {
			t.Fatalf("%s incomplete analysis must fail CI with exit 15: %v", format, err)
		}
		if stderr.Len() != 0 {
			t.Fatalf("%s stderr=%q", format, &stderr)
		}
		if format == "json" {
			r := validateEmitted(t, root, stdout.Bytes())
			if r.Status != "incomplete" || r.Complete || len(r.Diagnostics) != 0 || len(r.Coverage) == 0 {
				t.Fatalf("zero-finding incomplete result: %+v", r)
			}
			for _, c := range r.Coverage {
				if c.Reason != result.RuleUnavailable || c.Rule == "" || c.Path != "good.md" || c.Range != nil {
					t.Fatalf("fabricated or missing default-rule coverage: %+v", c)
				}
			}
		} else if !strings.Contains(stdout.String(), "Found 0 violations.") || !strings.Contains(stdout.String(), "incomplete (complete: false)") || !strings.Contains(stdout.String(), "rule-unavailable:") {
			t.Fatalf("incomplete text: %q", &stdout)
		}
	}
	// A real read-only stdout descriptor must become IO/13, without an alternate
	// report on stderr. The executable is built once for all process checks.
	f, err := os.Open(filepath.Join(root, "good.md")) //nolint:gosec // Open only the controlled test fixture as a read-only stdout descriptor.
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	cmd := exec.Command(binary, "lint", "--format=json", "good.md") //nolint:gosec // Controlled compiled fixture.
	cmd.Dir = root
	cmd.Stdout = f
	var processExit *exec.ExitError
	if err := cmd.Run(); !errors.As(err, &processExit) || processExit.ExitCode() != 13 {
		t.Fatalf("read-only stdout: %v", err)
	}
}

func TestReportManifestFeasibilityBeforeAnalysis(t *testing.T) {
	root := validProject(t)
	args := []string{"lint", "--format=json", "--limit=response-bytes=65536"}
	for i := 0; i < 400; i++ {
		args = append(args, fmt.Sprintf("%03d-%s.md", i, strings.Repeat("a", 200)))
	}
	// Supplied paths may select no files (directories, excluded paths), so they
	// cannot be treated as mandatory file identities before discovery.
	prepared := false
	var stdout bytes.Buffer
	status := run(args, &stdout, io.Discard, "test", func(o lintOptions) (*lintInvocation, error) { prepared = true; return &lintInvocation{options: o}, nil }, func(*lintInvocation, io.Writer, io.Writer) int { return 0 })
	if status != 0 || !prepared {
		t.Fatalf("paths fabricated a known manifest: status=%d prepared=%t", status, prepared)
	}
	for _, path := range args[3:] {
		if strings.HasPrefix(path, "--") {
			continue
		}
		put(t, filepath.Join(root, "many", path), "# x\n")
	}
	calls := 0
	registry := executionRegistry(t, map[string]engine.Check{"MD001": func(context.Context, engine.RuleContext) engine.RuleOutcome { calls++; return engine.RuleOutcome{} }}, false)
	stdout.Reset()
	status = runContext(context.Background(), []string{"lint", "--format=json", "--limit=response-bytes=65536", "many"}, &stdout, io.Discard, "test", func(o lintOptions) (*lintInvocation, error) {
		return prepareLintWith(o, registry, engine.ValidationProviders{}, config.Capabilities{CommonMark: true})
	}, func(ctx context.Context, inv *lintInvocation, _ io.Writer, w io.Writer) int {
		return acquireLintContext(ctx, inv, w)
	})
	if status != 15 || calls != 0 || stdout.Len() > 65536 {
		t.Fatalf("discovery manifest status=%d calls=%d bytes=%d", status, calls, stdout.Len())
	}
	out := validateEmitted(t, root, stdout.Bytes())
	found := false
	for _, o := range out.Omissions {
		if o.Section == "/files" && o.Count != nil && *o.Count == 400 {
			found = true
		}
	}
	if !found || out.Complete || len(out.CheckedFiles) != 0 || len(out.Interruptions) == 0 {
		t.Fatalf("inaccurate refusal: %+v", out)
	}
}
