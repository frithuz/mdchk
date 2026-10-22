package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/configscan"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func documentedMatcher(t *testing.T) *regexp.Regexp {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(executionPackageDir, "../../docs/output-formats.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range strings.Split(string(raw), "```json\n")[1:] {
		text, _, _ := strings.Cut(block, "```")
		var task struct {
			Tasks []struct {
				ProblemMatcher struct {
					Pattern struct {
						Regexp                                      string
						File, Line, Column, Severity, Code, Message int
					}
				}
			}
		}
		if json.Unmarshal([]byte(text), &task) != nil || len(task.Tasks) == 0 {
			continue
		}
		p := task.Tasks[0].ProblemMatcher.Pattern
		if p.Regexp == "" {
			continue
		}
		if got := []int{p.File, p.Line, p.Column, p.Severity, p.Code, p.Message}; !reflect.DeepEqual(got, []int{1, 2, 3, 4, 5, 6}) {
			t.Fatal(got)
		}
		return regexp.MustCompile(p.Regexp)
	}
	t.Fatal("documented task matcher absent")
	return nil
}

func TestVSCodeDocumentedMatcherAndHostileFields(t *testing.T) {
	matcher := documentedMatcher(t)
	paths := []string{"docs/a.md", "../space 雪/😀é.md", "C:/docs/a.md", `C:\docs\a.md`, "docs/name:part.md", "fake:99:2: error: MD002: replacement.md", "x\r\nforged:1:2: error: MD002: z\x00\x1b\t\x7f\u0085\u2028\u2029\u202e\u2066.md"}
	message := "hostile:99:2: error: MD002: replacement\r\nforged\x00\x1b\t\x7f\u0085\u2028\u2029\u202e\u2066"
	expectedMessage := `hostile\x3a99\x3a2\x3a error\x3a MD002\x3a replacement\r\nforged\x00\x1b\t\x7f\u0085\u2028\u2029\u202e\u2066`
	for _, path := range paths {
		r := result.NewLint("cli")
		r.Diagnostics = append(r.Diagnostics, result.Diagnostic{Path: path, Rule: "MD047", Message: message, Range: result.Range{Start: result.Position{Line: 2, Column: 5}}, FixAvailable: true})
		r.Notes = []json.RawMessage{json.RawMessage(`{"message":"not a finding"}`)}
		// Rendering intentionally never inspects notes, diffs or suppressed data.
		original, _ := json.Marshal(r)
		var stdout, stderr bytes.Buffer
		if got := reportResultStreams(r, "vscode", &stdout, &stderr, nil); got != 10 {
			t.Fatal(got, stdout.String(), stderr.String())
		}
		if stderr.Len() != 0 || strings.Count(stdout.String(), "\n") != 1 {
			t.Fatal(stdout.String(), stderr.String())
		}
		captures := matcher.FindStringSubmatch(strings.TrimSuffix(stdout.String(), "\n"))
		want := []string{textField(path), "2", "5", "error", "MD047", expectedMessage}
		if len(captures) != 7 || !reflect.DeepEqual(captures[1:], want) {
			t.Fatalf("captures=%q want=%q", captures, want)
		}
		after, _ := json.Marshal(r)
		if !bytes.Equal(original, after) {
			t.Fatal("canonical input mutated")
		}
		for _, c := range strings.TrimSuffix(stdout.String(), "\n") {
			if terminalControl(c) {
				t.Fatalf("raw control %U", c)
			}
		}
	}
}

func TestVSCodeStreamsCategoriesAndNoDecorations(t *testing.T) {
	r := result.NewConfigValidation()
	at := result.Range{Start: result.Position{Line: 4, Column: 7}}
	r.Errors = []result.Error{{Category: result.Configuration, Code: "config-definition", Path: ".mdchk.yaml", Range: &at, Message: "invalid: value"}, {Category: result.IO, Code: "read-failed", Path: "missing.md", Range: &at, Message: "permission denied"}}
	r.Coverage = []result.Coverage{{Path: "doc.md", Range: &at, Message: "unknown"}, {Rule: "MD001", Reason: "rule-unavailable", Message: "not implemented"}}
	r.Interruptions = []result.Interruption{{Phase: "checking", Reason: "cancelled", Message: "cancelled"}}
	count := 2
	r.Omissions = []result.Omission{{Section: "/notes", Reason: "output-limit", Count: &count, Message: "omitted whole records"}}
	engine.FinalizeLintResult(&r)
	var stdout, stderr bytes.Buffer
	if status := reportResultStreams(r, "vscode", &stdout, &stderr, nil); status != 13 {
		t.Fatal(status)
	}
	if stdout.String() != ".mdchk.yaml:4:7: error: config-definition: invalid\\x3a value\ndoc.md:4:7: error: coverage: unknown\n" {
		t.Fatal(stdout.String())
	}
	expected := "error missing.md read-failed io permission denied\ncoverage  MD001 rule-unavailable not implemented\ninterrupted checking  cancelled cancelled\nomitted 2 /notes  output-limit omitted whole records\nincomplete\n"
	if stderr.String() != expected {
		t.Fatalf("stderr=%q want=%q", stderr.String(), expected)
	}
	for _, operation := range []string{"lint", "fix", "config validate"} {
		clean := result.NewLint("cli")
		if operation == "fix" {
			clean = result.NewFix(true)
		}
		if operation == "config validate" {
			clean = result.NewConfigValidation()
		}
		stdout.Reset()
		stderr.Reset()
		if status := reportResultStreams(clean, "vscode", &stdout, &stderr, nil); status != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
			t.Fatal(operation, status, stdout.String(), stderr.String())
		}
	}
}

type vscodeFailWriter struct {
	short bool
	calls int
}

func (w *vscodeFailWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.short {
		return len(p) - 1, nil
	}
	return 0, errors.New("stream unavailable")
}
func TestVSCodeBothWritersAndPrecedence(t *testing.T) {
	for _, internal := range []bool{false, true} {
		for _, short := range []bool{false, true} {
			for _, which := range []string{"stdout", "stderr", "both"} {
				r := result.NewLint("cli")
				r.Diagnostics = []result.Diagnostic{{Path: "a.md", Rule: "MD047", Message: "finding", Range: result.Range{Start: result.Position{Line: 1, Column: 2}}}}
				category := result.IO
				if internal {
					category = result.Internal
				}
				r.Errors = []result.Error{{Category: category, Code: "failed", Message: "failed"}}
				engine.FinalizeLintResult(&r)
				a, b := &vscodeFailWriter{short: short}, &vscodeFailWriter{short: short}
				stdout, stderr := io.Discard, io.Discard
				if which != "stderr" {
					stdout = a
				}
				if which != "stdout" {
					stderr = b
				}
				want := 13
				if internal {
					want = 14
				}
				if got := reportResultStreams(r, "vscode", stdout, stderr, nil); got != want {
					t.Fatal(which, short, internal, got)
				}
				if which == "both" && (a.calls != 1 || b.calls != 1) {
					t.Fatal("both streams must be attempted", a, b)
				}
			}
		}
	}
}

func TestVSCodeCombinedBoundsCancellationAndDeadline(t *testing.T) {
	r := result.NewLint("cli")
	r.Diagnostics = []result.Diagnostic{{Path: "a.md", Rule: "MD047", Message: strings.Repeat("\u202e:", 500), Range: result.Range{Start: result.Position{Line: 1, Column: 2}}}}
	r.Coverage = []result.Coverage{{Rule: "MD001", Reason: "rule-unavailable", Message: "missing"}}
	engine.FinalizeLintResult(&r)
	full, err := encodeVSCode(context.Background(), r, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	size := int64(len(full.data) + len(full.stderr))
	n, err := reportSize(context.Background(), r, "vscode")
	if err != nil || n != size {
		t.Fatal(n, size, err)
	}
	exact, err := buildReport(context.Background(), r, "vscode", map[string]int64{"response-bytes": size})
	if err != nil || !bytes.Equal(exact.data, full.data) || !bytes.Equal(exact.stderr, full.stderr) {
		t.Fatal(err)
	}
	bounded, err := buildReport(context.Background(), r, "vscode", map[string]int64{"response-bytes": size - 1})
	if err != nil || len(bounded.data) != 0 || int64(len(bounded.stderr)) > size-1 || bounded.out.Complete || !bytes.Contains(bounded.stderr, []byte("omitted 1 /diagnostics")) {
		t.Fatal(err, string(bounded.data), string(bounded.stderr))
	}
	large := r
	large.Diagnostics = []result.Diagnostic{{Path: "a.md", Rule: "MD047", Message: strings.Repeat("\u202e", 1<<18)}}
	ctx := &reportCheckpointContext{Context: context.Background(), stop: 10}
	if _, err := buildReport(ctx, large, "vscode", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("large scalar cancellation", err)
	}
	measure := &reportCheckpointContext{Context: context.Background(), stop: 1000000}
	if _, err := encodeVSCode(measure, r, 1<<20); err != nil {
		t.Fatal(err)
	}
	last := &reportCheckpointContext{Context: context.Background(), stop: measure.checks - 1}
	if _, err := encodeVSCode(last, r, 1<<20); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("final checkpoint", err)
	}
	for _, operation := range []string{"lint", "fix", "config validate"} {
		for _, timeout := range []int64{1, 1000} {
			current := r
			current.Operation = operation
			if operation == "fix" {
				dry := true
				current.DryRun = &dry
				current.Files = []result.File{{Path: "a.md", WriteStatus: "failed", Leftovers: []string{".owned-stage"}}}
				current.Errors = []result.Error{{Category: result.IO, Code: "publication-failed", Message: "failed"}}
			}
			if operation == "config validate" {
				current = result.NewConfigValidation()
				current.Errors = []result.Error{{Category: result.Configuration, Code: "config-definition", Message: strings.Repeat("invalid:", 500)}}
			}
			engine.FinalizeLintResult(&current)
			var stdout, stderr bytes.Buffer
			status := reportResultStreams(current, "vscode", &stdout, &stderr, map[string]int64{"reporting-time": timeout})
			want := 15
			if operation == "fix" {
				want = 13
			}
			if operation == "config validate" {
				want = 12
			}
			if status != want {
				t.Fatal(operation, timeout, status, stdout.String(), stderr.String())
			}
			for _, line := range strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n") {
				if line != "" && documentedMatcher(t).FindStringSubmatch(line) == nil {
					t.Fatal("decorated fallback", line)
				}
			}
			if (operation != "config validate" || strings.Contains(stderr.String(), "interrupted reporting")) && !strings.Contains(stderr.String(), "incomplete") {
				t.Fatal("lost incomplete", stderr.String())
			}
			if operation == "fix" && !strings.Contains(stderr.String(), ".owned-stage") {
				t.Fatal("lost recovery evidence", stderr.String())
			}
		}
	}
	// Preview diffs are intentionally outside this presentation, not omissions.
	clean := result.NewFix(true)
	clean.Changes = []result.Change{{Diff: &result.Diff{Hunks: []result.DiffHunk{{Lines: []result.DiffLine{{Text: strings.Repeat("x", 1<<20)}}}}}}}
	var stdout, stderr bytes.Buffer
	if got := reportResultStreams(clean, "vscode", &stdout, &stderr, nil); got != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatal(got, stdout.String(), stderr.String())
	}
}

func TestVSCodeActualProductionLintFixAndConfiguration(t *testing.T) {
	root := project(t)
	put(t, filepath.Join(root, config.Filename), fixConfiguration())
	raw := "\t😀é雪"
	put(t, filepath.Join(root, "doc.md"), raw)
	registry, providers, caps, err := configuredProviders()
	if err != nil {
		t.Fatal(err)
	}
	inv, err := prepareLintWith(lintOptions{paths: []string{"doc.md"}}, registry, providers, caps)
	if err != nil {
		t.Fatal(err)
	}
	if got := acquireLintContext(context.Background(), inv, io.Discard); got != 10 {
		t.Fatal(got)
	}
	var stdout, stderr bytes.Buffer
	if got := reportResultStreams(*inv.result, "vscode", &stdout, &stderr, nil); got != 10 {
		t.Fatal(got)
	}
	// Tab, astral, combining and CJK each count one scalar, plus EOF column.
	want := "doc.md:1:6: error: MD047: "
	if !strings.HasPrefix(stdout.String(), want) || stderr.Len() != 0 {
		t.Fatal(stdout.String(), stderr.String())
	}
	validator, produce, err := configuredCorrections(registry)
	if err != nil {
		t.Fatal(err)
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	raw = "hello"
	put(t, filepath.Join(root, "doc.md"), raw)
	before, err := os.Stat(filepath.Join(root, "doc.md"))
	if err != nil {
		t.Fatal(err)
	}
	fixed := executeFix(context.Background(), lintOptions{paths: []string{"doc.md"}, dryRun: true}, budget, registry, providers, caps, validator, produce)
	if len(fixed.Changes) != 1 || len(fixed.Diagnostics) != 0 {
		t.Fatal(fixed)
	}
	stdout.Reset()
	stderr.Reset()
	if got := reportResultStreams(fixed, "vscode", &stdout, &stderr, nil); got != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatal(got, stdout.String(), stderr.String())
	}
	// Actual protected EOF remains manual in the canonical retained candidate.
	put(t, filepath.Join(root, "protected.md"), "\n<!-- 😀x")
	budget, _ = parser.NewOperationBudget(parser.DefaultLimits())
	manual := executeFix(context.Background(), lintOptions{paths: []string{"protected.md"}, dryRun: true}, budget, registry, providers, caps, validator, produce)
	if len(manual.Diagnostics) != 1 || manual.Diagnostics[0].Snapshot.View != "candidate" {
		t.Fatal(manual)
	}
	stdout.Reset()
	stderr.Reset()
	if got := reportResultStreams(manual, "vscode", &stdout, &stderr, nil); got != 10 || !strings.HasPrefix(stdout.String(), "protected.md:2:8: error: MD047: ") || stderr.Len() != 0 {
		t.Fatal(got, stdout.String(), stderr.String())
	}
	// Reporter never reopens the target and never changes inode, mode or mtime.
	after, err := os.Stat(filepath.Join(root, "doc.md"))
	if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("reporter changed source", err)
	}
	//nolint:gosec // Inspect only test-owned fixture contents.
	actual, err := os.ReadFile(filepath.Join(root, "doc.md"))
	if err != nil || string(actual) != raw {
		t.Fatal(string(actual), err)
	}
	put(t, filepath.Join(root, config.Filename), "version: 1\nflavor: commonmark\nrules:\n  MD013: {length: -1}\n")
	_, err = prepareLintWith(lintOptions{}, registry, providers, caps)
	if err == nil {
		t.Fatal("missing configuration error")
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "absent-gitconfig"))
	configuration := validationResult(configscan.Validate(context.Background(), configscan.Options{Root: root}, registry, providers, caps))
	if configuration.ConfigValidation.DefinitionsChecked != 1 || len(configuration.Errors) == 0 {
		t.Fatal(configuration)
	}
	stdout.Reset()
	stderr.Reset()
	if got := reportResultStreams(configuration, "vscode", &stdout, &stderr, nil); got != 12 || !strings.HasPrefix(stdout.String(), ".mdchk.yaml:4:") || !strings.Contains(stdout.String(), ": error: config-definition: ") || stderr.Len() != 0 {
		t.Fatal(got, stdout.String(), stderr.String())
	}
	captures := documentedMatcher(t).FindStringSubmatch(strings.TrimSuffix(stdout.String(), "\n"))
	if len(captures) != 7 {
		t.Fatal(captures)
	}
	if _, err := strconv.Atoi(captures[3]); err != nil {
		t.Fatal(err)
	}
}

func TestVSCodeTinyCleanDeadlineAndWholeOrderedPrefix(t *testing.T) {
	for _, operation := range []string{"lint", "fix", "config validate"} {
		for _, timeout := range []int64{1, 1000} {
			clean := result.NewLint("cli")
			if operation == "fix" {
				clean = result.NewFix(true)
			}
			if operation == "config validate" {
				clean = result.NewConfigValidation()
			}
			var stdout, stderr bytes.Buffer
			status := reportResultStreams(clean, "vscode", &stdout, &stderr, map[string]int64{"reporting-time": timeout})
			if stdout.Len() != 0 {
				t.Fatal(stdout.String())
			}
			switch status {
			case 0:
				if stderr.Len() != 0 {
					t.Fatal("complete noisy", stderr.String())
				}
			case 15:
				if !strings.Contains(stderr.String(), "reporting") || !strings.Contains(stderr.String(), "incomplete") {
					t.Fatal(stderr.String())
				}
			default:
				t.Fatal(status, stderr.String())
			}
		}
	}
	r := result.NewLint("cli")
	for i := 0; i < 20; i++ {
		r.Diagnostics = append(r.Diagnostics, result.Diagnostic{Path: "a.md", Rule: "MD047", Message: strings.Repeat("finding", 20), Range: result.Range{Start: result.Position{Line: i + 1, Column: 2}}})
	}
	engine.FinalizeLintResult(&r)
	bounded, err := buildReport(context.Background(), r, "vscode", map[string]int64{"response-bytes": 1000})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(bounded.data), "\n"), "\n")
	if len(lines) == 0 || len(lines) >= 20 || len(bounded.data)+len(bounded.stderr) > 1000 {
		t.Fatal(string(bounded.data), string(bounded.stderr))
	}
	for i, line := range lines {
		want := "a.md:" + strconv.Itoa(i+1) + ":2: error: MD047: "
		if !strings.HasPrefix(line, want) {
			t.Fatal(i, line)
		}
	}
	if len(bounded.out.Omissions) != 1 || *bounded.out.Omissions[0].Count != 20-len(lines) {
		t.Fatal(bounded.out.Omissions)
	}
	// The next whole finding cannot fit; no partial physical line is emitted.
	next := len(lines)
	if len(bounded.data)+len(bounded.stderr)+len("a.md:"+strconv.Itoa(next+1)+":2: error: MD047: ")+140+1 <= 1000 {
		t.Fatal("prefix was not maximal")
	}
}

func TestVSCodeZeroBound(t *testing.T) {
	clean := result.NewLint("cli")
	if report, err := encodeVSCode(context.Background(), clean, 0); err != nil || len(report.data)+len(report.stderr) != 0 {
		t.Fatal(err)
	}
	clean.Diagnostics = []result.Diagnostic{{Path: "a", Rule: "MD047", Message: "finding"}}
	for _, bound := range []int64{0, -1} {
		if _, err := encodeVSCode(context.Background(), clean, bound); !errors.Is(err, errReportBudget) {
			t.Fatal(bound, err)
		}
	}
}

func TestVSCodeDeadlineFallbackKeepsCallerContext(t *testing.T) {
	r := result.NewLint("cli")
	ctx := &reportCheckpointContext{Context: context.Background(), stop: 2}
	if _, err := deadlineReport(ctx, r, "vscode", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("deadline fallback lost caller cancellation", err)
	}
}

func TestVSCodeAllBidiControlsIndependentOracle(t *testing.T) {
	// Explicit Unicode Bidi_Control set and literal expected spellings are the
	// oracle: neither depends on the renderer's production classification helper.
	controls := "\u061c\u200e\u200f\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069"
	escaped := `\u061c\u200e\u200f\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069`
	r := result.NewFix(false)
	r.Diagnostics = []result.Diagnostic{{Path: "a" + controls + ".md", Rule: "MD047", Message: "before" + controls + "after", Range: result.Range{Start: result.Position{Line: 2, Column: 3}}}}
	r.Errors = []result.Error{{Category: result.IO, Code: "failure" + controls, Path: "operational" + controls, Message: "before" + controls + "after"}}
	r.Files = []result.File{{Path: "owned" + controls, WriteStatus: "unknown", Leftovers: []string{"stage" + controls}}}
	engine.FinalizeLintResult(&r)
	before, _ := json.Marshal(r)
	var stdout, stderr bytes.Buffer
	if status := reportResultStreams(r, "vscode", &stdout, &stderr, nil); status != 13 {
		t.Fatal(status)
	}
	wantOut := "a" + escaped + ".md:2:3: error: MD047: before" + escaped + "after\n"
	wantErr := "error operational" + escaped + " failure" + escaped + " io before" + escaped + "after\noutcome owned" + escaped + " unknown\nowned staging leftover owned" + escaped + " stage" + escaped + "\n"
	if stdout.String() != wantOut || stderr.String() != wantErr {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	for _, control := range []rune{0x061c, 0x200e, 0x200f, 0x202a, 0x202b, 0x202c, 0x202d, 0x202e, 0x2066, 0x2067, 0x2068, 0x2069} {
		if strings.ContainsRune(stdout.String()+stderr.String(), control) {
			t.Fatalf("raw bidi %U", control)
		}
	}
	size := int64(len(wantOut) + len(wantErr))
	count, err := reportSize(context.Background(), r, "vscode")
	if err != nil || count != size {
		t.Fatal(count, size, err)
	}
	exact, err := encodeVSCode(context.Background(), r, size)
	if err != nil || string(exact.data) != wantOut || string(exact.stderr) != wantErr {
		t.Fatal(err)
	}
	if _, err := encodeVSCode(context.Background(), r, size-1); !errors.Is(err, errReportBudget) {
		t.Fatal("combined escaped bound", err)
	}
	reserve, err := emergencyVSCodeReport(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.ContainsRune(reserve.stderr, 0x061c) || !bytes.Contains(reserve.stderr, []byte("stage"+escaped)) {
		t.Fatalf("reserve=%q", reserve.stderr)
	}
	measured, err := reportSize(context.Background(), reserve.out, "vscode")
	if err != nil || measured != int64(len(reserve.data)+len(reserve.stderr)) {
		t.Fatal(measured, err)
	}
	after, _ := json.Marshal(r)
	if !bytes.Equal(before, after) {
		t.Fatal("canonical strings changed")
	}
}

func TestVSCodeActualConfigurationInterruptionReserve(t *testing.T) {
	root := validationProject(t)
	put(t, filepath.Join(root, config.Filename), fixConfiguration())
	put(t, filepath.Join(root, "first.md"), "first\n")
	put(t, filepath.Join(root, "second.md"), "second\n")
	registry, providers, caps, err := configuredProviders()
	if err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"cancelled", "deadline-exceeded", "limit-exceeded"} {
		t.Run(reason, func(t *testing.T) {
			ctx := context.Background()
			limits := configscan.DefaultLimits()
			switch reason {
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "deadline-exceeded":
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Unix(1, 0))
				defer cancel()
			case "limit-exceeded":
				limits.Entries = 1
			}
			original := validationResult(configscan.Validate(ctx, configscan.Options{Root: root, Limits: limits}, registry, providers, caps))
			if len(original.Interruptions) != 1 || original.Interruptions[0].Phase != "configuration" || original.Interruptions[0].Reason != reason {
				t.Fatal(original)
			}
			if reason == "limit-exceeded" {
				at := original.Interruptions[0].Limit
				if at == nil || at.Name != "directory-entries" || at.Observed != 2 || at.Allowed != 1 || at.Unit != "observed entries" {
					t.Fatal(at)
				}
			}
			for _, category := range []result.ErrorCategory{"", result.Configuration, result.IO, result.Internal} {
				current := cloneReport(original)
				want := 15
				if category != "" {
					current.Errors = append(current.Errors, result.Error{Category: category, Code: "encountered", Message: "encountered failure"})
					switch category {
					case result.Usage:
						want = 11
					case result.Configuration:
						want = 12
					case result.IO:
						want = 13
					case result.Internal:
						want = 14
					}
					engine.FinalizeLintResult(&current)
				}
				before, _ := json.Marshal(current)
				reserve, err := emergencyConfigReport(current, "vscode", map[string]int64{"response-bytes": 4096})
				if err != nil {
					t.Fatal(err)
				}
				if len(reserve.data) != 0 || len(reserve.stderr) > 4096 || lintExit(reserve.out) != want || len(reserve.out.Interruptions) != 2 {
					t.Fatal(reserve.out, err)
				}
				operation, reporting := reserve.out.Interruptions[0], reserve.out.Interruptions[1]
				if operation.Phase != "configuration" || operation.Reason != reason || !reflect.DeepEqual(operation.Limit, current.Interruptions[0].Limit) || reporting.Phase != "reporting" || reporting.Reason != "deadline-exceeded" {
					t.Fatal(reserve.out.Interruptions)
				}
				if !bytes.Contains(reserve.stderr, []byte("interrupted configuration  "+reason)) || !bytes.Contains(reserve.stderr, []byte("interrupted reporting  deadline-exceeded")) {
					t.Fatalf("lost independent cause: %s", reserve.stderr)
				}
				if reason == "limit-exceeded" && !bytes.Contains(reserve.stderr, []byte("limit directory-entries observed 2 allowed 1 observed entries")) {
					t.Fatal(string(reserve.stderr))
				}
				counted, err := reportSize(context.Background(), reserve.out, "vscode")
				if err != nil || counted != int64(len(reserve.stderr)) {
					t.Fatal(counted, err)
				}
				for _, deadline := range []int64{1, 1000, int64(time.Second)} {
					var stdout, stderr bytes.Buffer
					status := reportResultStreams(current, "vscode", &stdout, &stderr, map[string]int64{"reporting-time": deadline, "response-bytes": 4096})
					if status != want || stdout.Len() != 0 || stderr.Len() > 4096 || !strings.Contains(stderr.String(), "interrupted configuration  "+reason) {
						t.Fatal(reason, category, deadline, status, stdout.String(), stderr.String())
					}
					if reason == "limit-exceeded" && !strings.Contains(stderr.String(), "limit directory-entries observed 2 allowed 1 observed entries") {
						t.Fatal(stderr.String())
					}
				}
				after, _ := json.Marshal(current)
				if !bytes.Equal(before, after) {
					t.Fatal("reserve mutated canonical operation interruption")
				}
			}
		})
	}
}
