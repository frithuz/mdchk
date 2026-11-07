package report

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
		r.Files = append(r.Files, result.File{Path: path, Analysis: "failed", WriteStatus: "not-requested", ReturnStatus: "not-requested", InputSnapshot: &v.Snapshot, ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}})
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

// A larger bound by one record's size keeps more, so the prefix is the largest.
func TestEncodingReductionKeepsLargestPrefix(t *testing.T) {
	r := encodingResult("lint", map[string]int{"a.md": 1000})
	kept := func(bound int64) int {
		reduced, err := reduceEncodingErrors(context.Background(), r, "json", bound, true)
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
	reduced, err := reduceEncodingErrors(context.Background(), r, "json", 65536, true)
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
	got, err := BuildReportCore(context.Background(), r, "json", map[string]int64{"response-bytes": 65536})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Out.Diagnostics) != 1 || len(got.Out.Coverage) != 1 {
		t.Fatal(got.Out.Diagnostics, got.Out.Coverage)
	}
	for _, o := range got.Out.Omissions {
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
	got, err := BuildReportCore(context.Background(), r, "json", map[string]int64{"response-bytes": 65536})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Out.Errors) != 1 || len(got.Out.Diagnostics) >= 1000 {
		t.Fatal(len(got.Out.Errors), len(got.Out.Diagnostics))
	}
	if count := encodingReductionOmission(t, got.Out); count != 999 {
		t.Fatal(count)
	}
}

// encodingManyFiles gives each of n files more invalid bytes than the file cap.
func encodingManyFiles(n int) map[string]int {
	sources := map[string]int{}
	for i := range n {
		sources[fmt.Sprintf("f%04d.md", i)] = 1100
	}
	return sources
}

// Whole file records are never dropped: a bound below them still fails.
func TestMinimalLintReportKeepsFileRecords(t *testing.T) {
	r := encodingResult("lint", encodingManyFiles(600))
	if _, err := BuildReport(context.Background(), r, "json", map[string]int64{"response-bytes": 65536}); !errors.Is(err, ErrReportBudget) {
		t.Fatal(err)
	}
}

func TestMinimalReportIsLintOnly(t *testing.T) {
	r := encodingResult("fix", encodingManyFiles(300))
	if _, err := BuildReport(context.Background(), r, "json", map[string]int64{"response-bytes": 66000}); !errors.Is(err, ErrReportBudget) {
		t.Fatal(err)
	}
}

// A report that fits keeps the omission for each file.
func TestEncodingRetentionOmissionsSurviveWhenTheyFit(t *testing.T) {
	r := encodingResult("lint", map[string]int{"a.md": 1100, "b.md": 1100})
	got, err := BuildReportCore(context.Background(), r, "json", map[string]int64{"response-bytes": 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Out.Omissions) != 2 || got.Out.Omissions[0].Path != "a.md" || got.Out.Omissions[1].Path != "b.md" {
		t.Fatal(got.Out.Omissions)
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
