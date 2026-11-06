package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func TestMandatoryEncodingGate(t *testing.T) {
	prefix := "<!-- markdownlint-disable -->\r\n雪\t"
	for _, suffix := range [][]byte{{0xff, 0xfe}, {0xe2, 0x82}, {0xf0, 0x28, 0x8c, 0xbc}} {
		raw := append([]byte(prefix), suffix...)
		for _, enabled := range []bool{false, true} {
			got := ValidateEncoding("bad.md", parser.NewSource(raw), enabled)
			if len(got.Errors) == 0 {
				t.Fatal("invalid encoding accepted")
			}
			e := got.Errors[0]
			wantRule := ""
			if enabled {
				wantRule = "MD094"
			}
			if e.Category != result.IO || e.Code != "invalid-utf8" || e.Path != "bad.md" || e.Rule != wantRule || e.ByteOffset == nil || *e.ByteOffset != len(prefix) || e.Range == nil || e.Range.Start != (parser.Position{Line: 2, Column: 3}) || e.Range.Start != e.Range.End {
				t.Fatalf("error=%+v", e)
			}
			if got.Snapshot.SizeBytes != len(raw) || got.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256(raw)) || e.Snapshot == nil || *e.Snapshot != got.Snapshot {
				t.Fatalf("snapshot=%+v", got.Snapshot)
			}
			if got.Index.EncodingFailure() == nil {
				t.Fatal("index lost decoding failure")
			}
		}
	}
	for _, text := range []string{"", "\ufeff雪\r\ntext\rfinal\n", "literal \ufffd"} {
		got := ValidateEncoding("ok.md", parser.NewSource([]byte(text)), true)
		if len(got.Errors) != 0 || got.Omission != nil || got.Index.EncodingFailure() != nil {
			t.Fatalf("valid source rejected: %q", text)
		}
	}
}

func TestEncodingErrorRecords(t *testing.T) {
	raw := []byte("a\xffb\xe2\x82c\xff\r\nd\n")
	for _, enabled := range []bool{false, true} {
		got := ValidateEncoding("bad.md", parser.NewSource(raw), enabled)
		if got.Omission != nil || len(got.Errors) != 3 {
			t.Fatalf("%+v", got)
		}
		rule := ""
		if enabled {
			rule = "MD094"
		}
		for i, want := range []struct {
			start, end, column int
			message            string
		}{
			{1, 2, 2, "Invalid UTF-8 byte sequence 0xFF at byte offset 1."},
			{3, 5, 4, "Invalid UTF-8 byte sequence 0xE2 0x82 at byte offset 3."},
			{6, 7, 6, "Invalid UTF-8 byte sequence 0xFF at byte offset 6."},
		} {
			e := got.Errors[i]
			point := parser.Position{Line: 1, Column: want.column}
			if e.Category != result.IO || e.Code != "invalid-utf8" || e.Scope != "operation" || e.Path != "bad.md" || e.Rule != rule || e.Message != want.message {
				t.Fatalf("%d: %+v", i, e)
			}
			if *e.ByteOffset != want.start || *e.ByteRange != (result.ByteRange{Start: want.start, End: want.end}) || e.Range.Start != point || e.Range.End != point || *e.Snapshot != got.Snapshot {
				t.Fatalf("%d: %+v", i, e)
			}
		}
	}
}

func TestEncodingFileCap(t *testing.T) {
	exact := ValidateEncoding("bad.md", parser.NewSource(bytes.Repeat([]byte{0xFF}, MaxFileEncodingErrors)), true)
	if len(exact.Errors) != MaxFileEncodingErrors || exact.Omission != nil {
		t.Fatal(len(exact.Errors), exact.Omission)
	}
	over := ValidateEncoding("bad.md", parser.NewSource(bytes.Repeat([]byte{0xFF}, MaxFileEncodingErrors+1)), true)
	o := over.Omission
	if len(over.Errors) != MaxFileEncodingErrors || o == nil || o.Section != "/errors" || o.Reason != "output-limit" || o.Path != "bad.md" || *o.Count != 1 || !o.EncodingRetention() {
		t.Fatal(len(over.Errors), o)
	}
}

// A budget's allowance outlives one Lint call, so a fix run that analyzes one
// target at a time retains the same records as one lint batch.
func TestEncodingOperationCapSharedAcrossAnalyses(t *testing.T) {
	registry, err := NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	source := bytes.Repeat([]byte{0xFF}, 1500)
	inputs := make([]Input, 12)
	for i := range inputs {
		inputs[i], err = NewInput(fmt.Sprintf("f%02d.md", i), source, nil, DocumentContext{Flavor: "commonmark"})
		if err != nil {
			t.Fatal(err)
		}
	}
	check := func(out []result.Result) {
		t.Helper()
		errorsByPath, omitted := map[string]int{}, map[string]int{}
		for _, r := range out {
			for _, e := range r.Errors {
				errorsByPath[e.Path]++
			}
			for _, o := range r.Omissions {
				omitted[o.Path] += *o.Count
			}
		}
		for i := range inputs {
			path, retained := fmt.Sprintf("f%02d.md", i), 1000
			if i >= 10 {
				retained = 1
			}
			if errorsByPath[path] != retained || omitted[path] != 1500-retained {
				t.Fatal(path, errorsByPath[path], omitted[path])
			}
		}
	}
	batch, err := registry.Lint(context.Background(), inputs, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Errors) != 10002 {
		t.Fatal(len(batch.Errors))
	}
	check([]result.Result{batch})
	budget, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	var separate []result.Result
	for _, in := range inputs {
		out, err := registry.Lint(context.Background(), []Input{in}, RunOptions{Budget: budget})
		if err != nil {
			t.Fatal(err)
		}
		separate = append(separate, out)
	}
	check(separate)
}

// A partial allowance retains exactly what is left, and later files keep one.
func TestEncodingOperationCapPartialAllowance(t *testing.T) {
	budget, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	budget.RetainEncodingErrors(MaxOperationEncodingErrors - 3)
	source := parser.NewSource(bytes.Repeat([]byte{0xFF}, 10))
	first, err := validateEncoding("a.md", source, true, snapshot(source), budget)
	if err != nil || len(first.Errors) != 3 || *first.Omission.Count != 7 {
		t.Fatal(err, len(first.Errors), first.Omission)
	}
	second, err := validateEncoding("b.md", source, true, snapshot(source), budget)
	if err != nil || len(second.Errors) != 1 || *second.Errors[0].ByteOffset != 0 || *second.Omission.Count != 9 {
		t.Fatal(err, len(second.Errors), second.Omission)
	}
	// Only records actually retained consume the allowance.
	few := parser.NewSource([]byte{0xFF, 'a', 0xFF})
	fresh, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateEncoding("c.md", few, true, snapshot(few), fresh); err != nil || fresh.EncodingErrorsRetained() != 2 {
		t.Fatal(err, fresh.EncodingErrorsRetained())
	}
}

func encodingLint(t *testing.T, raw []byte, workUnits int64) result.Result {
	t.Helper()
	registry, err := NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	in, err := NewInput("bad.md", raw, nil, DocumentContext{Flavor: "commonmark"})
	if err != nil {
		t.Fatal(err)
	}
	limits := parser.DefaultLimits()
	limits.WorkUnits = workUnits
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	out, err := registry.Lint(context.Background(), []Input{in}, RunOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestEncodingChargeExhaustsWork(t *testing.T) {
	raw := []byte("a\xffb\xff")
	// The existing reserve passes; the scan and records do not fit after it.
	out := encodingLint(t, raw, int64(len(raw))*4+1+int64(len(raw)))
	if len(out.Errors) != 0 || out.Complete || out.Status != "incomplete" || len(out.Omissions) != 0 {
		t.Fatalf("%+v", out)
	}
	if len(out.Interruptions) != 1 || out.Interruptions[0].Reason != "limit-exceeded" || out.Interruptions[0].Limit == nil || out.Interruptions[0].Limit.Name != "work-units" {
		t.Fatalf("%+v", out.Interruptions)
	}
	if len(out.Files) != 1 || out.Files[0].Analysis != "partial" {
		t.Fatalf("%+v", out.Files)
	}
}

// The smallest sufficient work allowance measures the whole gate's charge.
func TestEncodingWorkIsLinearInFileSize(t *testing.T) {
	charged := func(size int) int64 {
		raw := bytes.Repeat([]byte{0xFF}, size)
		lo, hi := int64(1), int64(64*size)
		for lo < hi {
			mid := lo + (hi-lo)/2
			if len(encodingLint(t, raw, mid).Errors) != 0 {
				hi = mid
			} else {
				lo = mid + 1
			}
		}
		return lo
	}
	half, whole := charged(512*1024), charged(1024*1024)
	if float64(whole) > 2.5*float64(half) {
		t.Fatal(half, whole)
	}
}
