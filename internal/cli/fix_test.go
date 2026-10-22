package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func fixConfiguration() string {
	var b strings.Builder
	b.WriteString("version: 1\nflavor: commonmark\nrules:\n")
	for _, meta := range engine.Catalog() {
		fmt.Fprintf(&b, "  %s: {enabled: %t}\n", meta.ID, meta.ID == "MD047" || meta.ID == "MD094")
	}
	return b.String()
}
func TestFixProductionCorrectionAndArguments(t *testing.T) {
	root := project(t)
	put(t, filepath.Join(root, ".mdchk.yaml"), fixConfiguration())
	put(t, filepath.Join(root, "doc.md"), "hello")
	for _, args := range [][]string{{"fix", "--dry-run", "doc.md", "--format=json"}, {"fix", "doc.md", "--format", "json", "--dry-run"}, {"fix", "--dry-run", "--format=json", "--", "doc.md"}} {
		var stdout, stderr bytes.Buffer
		status := Run(args, &stdout, &stderr, "test")
		if status != 0 {
			t.Fatal(status, stdout.String())
		}
		r := validateEmitted(t, t.TempDir(), stdout.Bytes())
		if r.Operation != "fix" || r.DryRun == nil || !*r.DryRun || len(r.Changes) != 1 || len(r.Diagnostics) != 0 || r.Files[0].WriteStatus != "proposed" {
			t.Fatal(r)
		}
	}
	for _, args := range [][]string{{"fix", "--bad", "--format=json"}, {"fix", "--config", "--dry-run", "doc.md", "--format=json"}, {"fix", "--dry-run", "--limit=workers-per-operation=2", "--format=json"}} {
		var stdout, stderr bytes.Buffer
		if status := Run(args, &stdout, &stderr, "test"); status != 11 {
			t.Fatal(status, stdout.String())
		}
		r := validateEmitted(t, t.TempDir(), stdout.Bytes())
		if r.Operation != "fix" || r.Errors[0].Category != result.Usage {
			t.Fatal(r)
		}
	}
	for _, args := range [][]string{{"fix", "--help"}, {"--help", "fix"}, {"fix", "--version"}} {
		var b bytes.Buffer
		if status := Run(args, &b, &b, "test"); status != 0 {
			t.Fatal(status, b.String())
		}
	}
	//nolint:gosec // Read only the test-owned source fixture.
	content, _ := os.ReadFile(filepath.Join(root, "doc.md"))
	if string(content) != "hello" {
		t.Fatal("dry run wrote source")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	if status := RunContext(ctx, []string{"fix", "--dry-run", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 15 {
		t.Fatal(status, stdout.String())
	}
	validateEmitted(t, t.TempDir(), stdout.Bytes())
}
func TestFixDiffPhysicalHunksAndScaling(t *testing.T) {
	for _, tc := range []struct{ old, next string }{{"", "\n"}, {"a", "a\n"}, {"a\r\nb\rc\n", "\na\r\nb\rc\n"}, {"a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\n", "A\nb\nc\nd\ne\nf\ng\nh\ni\nj\nK\n"}, {"雪\n", ""}, {strings.Repeat("paragraph\n\n", 8000), "\n" + strings.Repeat("paragraph\n\n", 8000)}} {
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		diff, err := buildFixDiff(context.Background(), budget, parser.NewSource([]byte(tc.old)), parser.NewSource([]byte(tc.next)))
		if err != nil {
			t.Fatal(err)
		}
		if len(diff.Hunks) == 0 {
			t.Fatal("missing changed hunk")
		}
		oldLines := strings.ReplaceAll(strings.ReplaceAll(tc.old, "\r\n", "\n"), "\r", "\n")
		_ = oldLines
		for _, h := range diff.Hunks {
			a, b := 0, 0
			for _, line := range h.Lines {
				if strings.ContainsAny(line.Text, "\r\n") {
					t.Fatal(line)
				}
				if line.Kind != "add" {
					a++
				}
				if line.Kind != "remove" {
					b++
				}
			}
			if a != h.OldLines || b != h.NewLines {
				t.Fatal(h)
			}
		}
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	diff, err := buildFixDiff(context.Background(), budget, parser.NewSource([]byte("雪\r\n")), parser.NewSource([]byte("雪\r")))
	if err != nil || len(diff.Hunks) != 1 || !reflect.DeepEqual(diff.Hunks[0].Lines, []result.DiffLine{{Kind: "remove", Text: "雪", LineEnding: "crlf"}, {Kind: "add", Text: "雪", LineEnding: "cr"}}) {
		t.Fatal(diff, err)
	}
	budget, _ = parser.NewOperationBudget(parser.Limits{WorkUnits: 1})
	_, err = buildFixDiff(context.Background(), budget, parser.NewSource([]byte("a")), parser.NewSource([]byte("b")))
	if err == nil {
		t.Fatal("diff did not consume residual meter")
	}
}
func TestFixReporterBoundsDeadlineOwnership(t *testing.T) {
	r := result.NewFix(true)
	before := result.Snapshot{View: "original", SHA256: strings.Repeat("a", 64), SizeBytes: 1}
	next := before
	next.View = "candidate"
	next.SHA256 = strings.Repeat("b", 64)
	r.Files = []result.File{{Path: "x.md", Analysis: "complete", WriteStatus: "proposed", ReturnStatus: "not-requested", InputSnapshot: &before, ResultSnapshot: &next, ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{"change-1"}}}
	r.CheckedFiles = []string{"x.md"}
	r.Changes = []result.Change{{ID: "change-1", Path: "x.md", Disposition: "proposed", Before: before, Candidate: next, Rules: []string{"MD001"}, EditCount: 1, Diff: &result.Diff{Format: "line-hunks", ContextLines: 3, Hunks: []result.DiffHunk{{OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1, Lines: []result.DiffLine{{Kind: "remove", Text: strings.Repeat("<&\x1b", 30000), LineEnding: "none"}, {Kind: "add", Text: "new", LineEnding: "lf"}}}}}}}
	engine.FinalizeLintResult(&r)
	image, _ := json.Marshal(r)
	for _, format := range []string{"json", "text"} {
		for _, deadline := range []int64{1, 1000, 2000000000} {
			var b bytes.Buffer
			status := reportResult(r, format, &b, map[string]int64{"response-bytes": 65536, "reporting-time": deadline})
			if status != 15 || b.Len() > 65536 {
				t.Fatal(status, b.Len())
			}
			if format == "json" {
				out := validateEmitted(t, t.TempDir(), b.Bytes())
				if out.Operation != "fix" || out.DryRun == nil || !*out.DryRun || len(out.Files) != 1 || len(out.Changes) != 1 || out.Changes[0].Diff != nil {
					t.Fatal(out)
				}
			}
			if reportResult(r, format, inspectionShortWriter{}, nil) != 13 {
				t.Fatal("shortwrite")
			}
		}
	}
	after, _ := json.Marshal(r)
	if !bytes.Equal(image, after) {
		t.Fatal("mutated correction result")
	}
}

func TestFixReportingErrorPrecedenceAndExactFit(t *testing.T) {
	for _, category := range []result.ErrorCategory{result.IO, result.Internal} {
		r := result.NewFix(true)
		r.Errors = append(r.Errors, result.Error{Category: category, Code: "fixture", Scope: "operation", Message: "encountered failure"})
		engine.FinalizeLintResult(&r)
		want := 13
		if category == result.Internal {
			want = 14
		}
		for _, format := range []string{"json", "text"} {
			for _, deadline := range []int64{1, 1000} {
				var b bytes.Buffer
				if got := reportResult(r, format, &b, map[string]int64{"reporting-time": deadline}); got != want {
					t.Fatal(got, want, b.String())
				}
				if format == "json" {
					out := validateEmitted(t, t.TempDir(), b.Bytes())
					if len(out.Errors) != 1 || out.Errors[0].Category != category {
						t.Fatal(out)
					}
				}
				if got := reportResult(r, format, inspectionShortWriter{}, map[string]int64{"reporting-time": deadline}); got != want {
					t.Fatal(got, want)
				}
			}
		}
	}
	r := result.NewFix(true)
	r.Notes = []json.RawMessage{json.RawMessage(`{"id":"note","code":"fixture","message":"` + strings.Repeat("x", 68000) + `"}`)}
	engine.FinalizeLintResult(&r)
	first, err := buildReport(context.Background(), r, "json", nil)
	if err != nil {
		t.Fatal(err)
	}
	// The response-bytes limit's own decimal spelling is part of the encoding.
	bound := int64(len(first.data))
	for range 4 {
		first, err = buildReport(context.Background(), r, "json", map[string]int64{"response-bytes": bound})
		if err != nil {
			t.Fatal(err)
		}
		bound = int64(len(first.data))
	}
	exact, err := buildReport(context.Background(), r, "json", map[string]int64{"response-bytes": bound})
	if err != nil || !exact.out.Complete || int64(len(exact.data)) != bound {
		t.Fatal(err, len(exact.data), bound)
	}
	under, err := buildReport(context.Background(), r, "json", map[string]int64{"response-bytes": bound - 1})
	if err != nil || under.out.Complete || len(under.out.Notes) != 0 || len(under.out.Omissions) != 1 || under.out.Omissions[0].Count == nil || *under.out.Omissions[0].Count != 1 {
		t.Fatal(err, under.out)
	}
}

func TestFixSelectionParityAndResidualConfigurationBudget(t *testing.T) {
	root := project(t)
	put(t, filepath.Join(root, ".mdchk.yaml"), fixConfiguration()+"exclude: ['hidden/**']\n")
	put(t, filepath.Join(root, "good.md"), "text\n")
	put(t, filepath.Join(root, "hidden", "excluded.md"), "hidden\n")
	put(t, filepath.Join(root, "other.txt"), "text\n")
	for _, paths := range [][]string{nil, {"."}, {"good.md", "good.md", "hidden/excluded.md"}, {"other.txt"}} {
		var a, b, stderr bytes.Buffer
		lintArgs := append([]string{"lint", "--format=json"}, paths...)
		fixArgs := append([]string{"fix", "--dry-run", "--format=json"}, paths...)
		status := Run(lintArgs, &a, &stderr, "test")
		fixed := Run(fixArgs, &b, &stderr, "test")
		if status != fixed {
			t.Fatal(status, fixed, a.String(), b.String())
		}
		l := validateEmitted(t, t.TempDir(), a.Bytes())
		f := validateEmitted(t, t.TempDir(), b.Bytes())
		if !reflect.DeepEqual(l.CheckedFiles, f.CheckedFiles) || !reflect.DeepEqual(l.SkippedFiles, f.SkippedFiles) {
			t.Fatal(l, f)
		}
	}
	// Exhaust the original budget before configuration: the same meter must
	// reject discovery, not create a fresh acquisition/validation budget.
	opts, _, err := parseFix([]string{"--dry-run", "good.md"})
	if err != nil {
		t.Fatal(err)
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	if err := budget.ChargeWork(budget.Limits().WorkUnits); err != nil {
		t.Fatal(err)
	}
	registry, providers, caps, err := configuredProviders()
	if err != nil {
		t.Fatal(err)
	}
	validator, produce, err := configuredCorrections(registry)
	if err != nil {
		t.Fatal(err)
	}
	out := executeFix(context.Background(), opts, budget, registry, providers, caps, validator, produce)
	if lintExit(out) != 15 || len(out.CheckedFiles) != 0 || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "configuration" || out.Interruptions[0].Limit == nil || out.Interruptions[0].Limit.Name != "work-units" {
		t.Fatal(out)
	}
}

// Empty discovery must retain its own cause before requesting native authority.
func TestFixEmptyDiscoveryCausePrecedence(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprint(mixed), func(t *testing.T) {
			root := project(t)
			put(t, filepath.Join(root, ".mdchk.yaml"), fixConfiguration())
			for i := 0; i < 1100; i++ {
				put(t, filepath.Join(root, "z-directory", fmt.Sprintf("%04d.txt", i)), "untouched")
			}
			paths := []string{"z-directory"}
			if mixed {
				paths = append(paths, "a-missing.md")
			}
			for _, dry := range []bool{false, true} {
				opts, _, err := parseFix(paths)
				if err != nil {
					t.Fatal(err)
				}
				opts.dryRun = dry
				limits := parser.DefaultLimits()
				limits.TotalNodes = 1000
				limits.DocumentNodes = 1000
				budget, err := parser.NewOperationBudget(limits)
				if err != nil {
					t.Fatal(err)
				}
				registry, providers, caps, err := configuredProviders()
				if err != nil {
					t.Fatal(err)
				}
				validator, produce, err := configuredCorrections(registry)
				if err != nil {
					t.Fatal(err)
				}
				out := executeFix(context.Background(), opts, budget, registry, providers, caps, validator, produce)
				want := 15
				if mixed {
					want = 13
				}
				if lintExit(out) != want || out.Complete || len(out.Interruptions) == 0 || len(out.CheckedFiles) != 0 || len(out.Changes) != 0 {
					t.Fatalf("exit=%d result=%+v", lintExit(out), out)
				}
				for _, e := range out.Errors {
					if e.Code != "selection-failed" {
						t.Fatal(e)
					}
				}
				if (len(out.Errors) > 0) != mixed {
					t.Fatal(out.Errors)
				}
				var stdout bytes.Buffer
				if status := reportResult(out, "json", &stdout, nil); status != want {
					t.Fatal(status, stdout.String())
				}
				validateEmitted(t, t.TempDir(), stdout.Bytes())
			}
		})
	}
}
