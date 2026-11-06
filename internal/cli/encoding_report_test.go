package cli

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// encodingResult reproduces what the gate publishes for the given invalid files.
func encodingResult(operation string, sources map[string]int) result.Result {
	r := result.NewLint("cli")
	if operation == "fix" {
		r = result.NewFix(true)
	}
	paths := []string{}
	for path := range sources {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	for _, path := range paths {
		v := engine.ValidateEncoding(path, parser.NewSource(bytes.Repeat([]byte{0xFF}, sources[path])), true)
		r.Errors = append(r.Errors, v.Errors...)
		if v.Omission != nil {
			r.Omissions = append(r.Omissions, *v.Omission)
		}
		r.Coverage = append(r.Coverage, result.Coverage{Path: path, Reason: "decoding-failed", Message: "Markdown validation is unavailable."})
		r.CheckedFiles = append(r.CheckedFiles, path)
		r.Files = append(r.Files, result.File{Path: path, Analysis: "failed", WriteStatus: "not-requested", ReturnStatus: "not-requested", ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}})
	}
	engine.FinalizeLintResult(&r)
	return r
}

func encodingReductionOmission(t *testing.T, r result.Result) int {
	t.Helper()
	for _, o := range r.Omissions {
		if o.Section == "/errors" && o.Path == "" {
			return *o.Count
		}
	}
	t.Fatal("no path-less /errors omission", r.Omissions)
	return 0
}

func checkEncodingLinks(t *testing.T, r result.Result) {
	t.Helper()
	paths := map[string]string{}
	for _, e := range r.Errors {
		paths[e.ID] = e.Path
	}
	for _, c := range r.Coverage {
		if c.Reason != "decoding-failed" {
			continue
		}
		want := []string{}
		for id, path := range paths {
			if path == c.Path {
				want = append(want, id)
			}
		}
		got := slices.Clone(c.CauseIDs)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Fatalf("%s: causes %v, want %v", c.Path, got, want)
		}
	}
}

func TestEncodingReductionKeepsFirstOfEachFileAndLinks(t *testing.T) {
	limits := map[string]int64{"response-bytes": 65536}
	for _, operation := range []string{"lint", "fix"} {
		for _, format := range []string{"json", "text"} {
			r := encodingResult(operation, map[string]int{"a.md": 1000, "b.md": 1000, "c.md": 1})
			got, err := buildReportCore(context.Background(), r, format, limits)
			if err != nil {
				t.Fatal(operation, format, err)
			}
			if int64(len(got.data)) > limits["response-bytes"] {
				t.Fatal(operation, format, len(got.data))
			}
			if format == "json" {
				validateEmitted(t, t.TempDir(), got.data)
			}
			out := got.out
			seen := map[string]int{}
			for _, e := range out.Errors {
				if seen[e.Path] == 0 && *e.ByteOffset != 0 {
					t.Fatal("first record lost", e)
				}
				seen[e.Path]++
			}
			if seen["a.md"] == 0 || seen["b.md"] != 1 || seen["c.md"] != 1 {
				t.Fatal(operation, format, seen)
			}
			if count := encodingReductionOmission(t, out); count != 2001-len(out.Errors) {
				t.Fatal(operation, format, count, len(out.Errors))
			}
			if len(out.Coverage) != 3 {
				t.Fatal("coverage reduced", out.Coverage)
			}
			checkEncodingLinks(t, out)
			if lintExit(out) != 13 || out.Complete {
				t.Fatal(lintExit(out), out.Complete)
			}
		}
	}
}

// A larger bound by one record's size keeps more, so the prefix is the largest.
func TestEncodingReductionKeepsLargestPrefix(t *testing.T) {
	r := encodingResult("lint", map[string]int{"a.md": 1000})
	kept := func(bound int64) int {
		reduced, err := reduceEncodingErrors(context.Background(), r, "json", bound)
		if err != nil {
			t.Fatal(err)
		}
		return len(reduced.Errors)
	}
	base := kept(65536)
	if base <= 1 || base >= 1000 || kept(65536+1024) <= base {
		t.Fatal(base, kept(65536+1024))
	}
}

// Dropping a.md's records renumbers b.md's. The old IDs must not alias them.
func TestEncodingReductionLeavesNoStaleCause(t *testing.T) {
	r := encodingResult("lint", map[string]int{"a.md": 500, "b.md": 500})
	reduced, err := reduceEncodingErrors(context.Background(), r, "json", 65536)
	if err != nil {
		t.Fatal(err)
	}
	checkEncodingLinks(t, reduced)
	for _, f := range reduced.Files {
		if len(f.ErrorIDs) == 0 {
			t.Fatal(f)
		}
	}
}

func TestEncodingReductionComesBeforeOptionalFamilies(t *testing.T) {
	r := encodingResult("lint", map[string]int{"a.md": 1000})
	r.Diagnostics = append(r.Diagnostics, result.Diagnostic{Path: "ok.md", Rule: "MD047", Severity: "error", Message: "finding"})
	got, err := buildReportCore(context.Background(), r, "json", map[string]int64{"response-bytes": 65536})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.out.Diagnostics) != 1 || len(got.out.Coverage) != 1 {
		t.Fatal(got.out.Diagnostics, got.out.Coverage)
	}
	for _, o := range got.out.Omissions {
		if o.Section != "/errors" {
			t.Fatal(o)
		}
	}
}

func TestEncodingReductionLaterReductionsStillApply(t *testing.T) {
	r := encodingResult("lint", map[string]int{"a.md": 1000})
	for range 1000 {
		r.Diagnostics = append(r.Diagnostics, result.Diagnostic{Path: "ok.md", Rule: "MD047", Severity: "error", Message: strings.Repeat("x", 200)})
	}
	got, err := buildReportCore(context.Background(), r, "json", map[string]int64{"response-bytes": 65536})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.out.Errors) != 1 || len(got.out.Diagnostics) >= 1000 {
		t.Fatal(len(got.out.Errors), len(got.out.Diagnostics))
	}
	if count := encodingReductionOmission(t, got.out); count != 999 {
		t.Fatal(count)
	}
}

func TestEncodingReductionInDeadlineReport(t *testing.T) {
	limits := map[string]int64{"response-bytes": 65536}
	for _, format := range []string{"json", "text"} {
		got, err := deadlineReport(context.Background(), encodingResult("lint", map[string]int{"a.md": 1000}), format, limits)
		if err != nil {
			t.Fatal(format, err)
		}
		if int64(len(got.data)) > limits["response-bytes"] || len(got.out.Errors) == 0 || len(got.out.Errors) == 1000 {
			t.Fatal(format, len(got.data), len(got.out.Errors))
		}
		if count := encodingReductionOmission(t, got.out); count != 1000-len(got.out.Errors) {
			t.Fatal(format, count)
		}
		if format == "json" {
			validateEmitted(t, t.TempDir(), got.data)
		}
	}
}

func TestConfigManifestReductionClearsByteRange(t *testing.T) {
	offset := 1
	r := result.NewConfigValidation()
	r.Errors = append(r.Errors, result.Error{Category: result.Configuration, Code: "config-definition", Scope: "operation", Message: "bad", ByteOffset: &offset, ByteRange: &result.ByteRange{Start: 1, End: 2}})
	engine.FinalizeLintResult(&r)
	if err := reduceConfigManifest(context.Background(), &r, "json", 1); err != nil {
		t.Fatal(err)
	}
	if len(r.Errors) != 1 || r.Errors[0].ByteOffset != nil || r.Errors[0].ByteRange != nil {
		t.Fatalf("%+v", r.Errors)
	}
}

func TestVSCodeLocatesOnlyEncodingIOErrors(t *testing.T) {
	r := encodingResult("lint", map[string]int{"bad.md": 2})
	location := *r.Errors[0].Range
	r.Errors = append(r.Errors, result.Error{Category: result.IO, Code: "read-failed", Scope: "operation", Path: "other.md", Message: "failed", Range: &location})
	var stdout, stderr bytes.Buffer
	if err := renderVSCode(context.Background(), &stdout, &stderr, r); err != nil {
		t.Fatal(err)
	}
	want := "bad.md:1:1: error: invalid-utf8: Invalid UTF-8 byte sequence 0xFF at byte offset 0.\nbad.md:1:2: error: invalid-utf8: Invalid UTF-8 byte sequence 0xFF at byte offset 1.\n"
	if stdout.String() != want || !strings.Contains(stderr.String(), "read-failed") || strings.Contains(stderr.String(), "invalid-utf8") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestInspectionOutcomeIgnoresEncodingRetention(t *testing.T) {
	count := 5
	capped := result.Omission{Section: "/errors", Reason: "output-limit", Path: "bad.md", Count: &count, Message: "capped"}
	for _, tc := range []struct {
		name     string
		mutate   func(*result.Result)
		status   string
		exit     int
		complete bool
	}{
		{"capped", func(r *result.Result) { r.Omissions = []result.Omission{capped} }, "inspected", 0, false},
		{"path-less reduction", func(r *result.Result) {
			r.Omissions = []result.Omission{{Section: "/errors", Reason: "output-limit", Count: &count, Message: "reduced"}}
		}, "incomplete", exitIncomplete, false},
		{"capped and reduced", func(r *result.Result) {
			r.Omissions = []result.Omission{capped, {Section: "/notes", Reason: "output-limit", Count: &count, Message: "reduced"}}
		}, "incomplete", exitIncomplete, false},
		{"capped and interrupted", func(r *result.Result) {
			r.Omissions = []result.Omission{capped}
			r.Interruptions = []result.Interruption{{ID: "stop", Reason: "limit-exceeded", Phase: "reporting", Message: "stopped"}}
		}, "incomplete", exitIncomplete, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newInspectResult()
			tc.mutate(&r)
			inspectionOutcome(&r)
			if r.Status != tc.status || r.Complete != tc.complete || inspectionExit(r) != tc.exit {
				t.Fatal(r.Status, r.Complete, inspectionExit(r))
			}
		})
	}
}

func TestMergeFixAnalysisCopiesOnlyEncodingRetention(t *testing.T) {
	count := 1
	in := result.NewFix(true)
	in.Omissions = []result.Omission{
		{Section: "/errors", Reason: "output-limit", Path: "bad.md", Count: &count, Message: "capped"},
		{Section: "/notes", Reason: "output-limit", Path: "bad.md", Count: &count, Message: "other"},
		{Section: "/errors", Reason: "output-limit", Count: &count, Message: "reduced"},
	}
	out := result.NewFix(true)
	mergeFixAnalysis(&out, in)
	if len(out.Omissions) != 1 || out.Omissions[0].Message != "capped" {
		t.Fatal(out.Omissions)
	}
}
