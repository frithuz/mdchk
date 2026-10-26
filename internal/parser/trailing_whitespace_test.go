package parser

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestTrailingWhitespaceActualPremises(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, tc := range []struct {
			source      string
			kind        Kind
			raw         string
			definitions int
			count       int
		}{
			{"```  \na  \n  \n```  \n", CodeBlock, "", 0, 2},
			{"    a  \n    \n    b  \n", CodeBlock, "", 0, 0},
			{"`x  \ny`  \nz", CodeSpan, "", 0, 0},
			{"x <a\n title='a  \nb'>  \ny", InlineHTML, "", 0, 0},
			{"a <script>x  \ny</script>  \nz\n", InlineHTML, "script", 0, 0},
			{"a <iframe>x \ny</iframe> \nz\n", InlineHTML, "", 0, 2},
			{"[id]: /u \n", "", "", 1, 1},
		} {
			s := NewSource([]byte(tc.source))
			b, _ := NewOperationBudget(DefaultLimits())
			options := ParseOptions{GFM: gfm}
			p := ParseWithOptions(context.Background(), s, b, options)
			if p.Status != Complete || len(p.Document.references.Definitions) != tc.definitions {
				t.Fatal("parse premise", p)
			}
			found := tc.kind == ""
			rawFound := tc.raw == ""
			var walk func(*Node)
			walk = func(n *Node) {
				if n.Kind() == tc.kind {
					found = true
				}
				if n.rawTag == tc.raw && tc.raw != "" {
					rawFound = true
				}
				if tc.raw == "" && n.rawTag != "" {
					t.Fatal("unexpected raw state", tc.source, n.rawTag)
				}
				for _, c := range n.spec.Children {
					walk(c)
				}
			}
			walk(p.Document.root)
			if !found || !rawFound {
				t.Fatal("actual owner premise", tc)
			}
			got, e := p.Document.TrailingWhitespace(context.Background(), b, NewLineIndex(s), options, 2, true, false)
			if e != nil || len(got) != tc.count {
				t.Fatal(tc, got, e)
			}
		}
	}
}

func TestTrailingWhitespaceRejectsForeignAuthority(t *testing.T) {
	d, b, index := sectionParse(t, "a \n")
	constructed, e := NewDocument(d.Root(), d.References())
	if e != nil {
		t.Fatal(e)
	}
	foreign, _ := NewOperationBudget(DefaultLimits())
	for _, tc := range []struct {
		d       *Document
		b       *OperationBudget
		index   LineIndex
		options ParseOptions
	}{
		{nil, b, index, ParseOptions{}}, {d, nil, index, ParseOptions{}}, {d, b, LineIndex{}, ParseOptions{}},
		{constructed, b, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}}, {d, b, NewLineIndex(NewSource([]byte("a \n"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}},
	} {
		if got, e := tc.d.TrailingWhitespace(context.Background(), tc.b, tc.index, tc.options, 2, false, false); e == nil || got != nil {
			t.Fatal(got, e)
		}
	}
	//nolint:staticcheck // Missing caller authority is deliberately rejected.
	if got, e := d.TrailingWhitespace(nil, b, index, ParseOptions{}, 2, false, false); e == nil || got != nil {
		t.Fatal(got, e)
	}
	d.parsedComplete = false
	if got, e := d.TrailingWhitespace(context.Background(), b, index, ParseOptions{}, 2, false, false); e == nil || got != nil {
		t.Fatal(got, e)
	}
}

func TestTrailingWhitespaceDirectQuota(t *testing.T) {
	for _, source := range []string{"", "clean\n", "a \nb \nc \nd \ne \n", "<!-- protected --> \n", strings.Repeat("a \n", 256)} {
		for _, kind := range []string{"work", "nodes"} {
			d, b, index := sectionParse(t, source)
			if kind == "work" {
				b.limits.WorkUnits = b.work + 1
			} else {
				b.limits.TotalNodes = b.nodes
				b.limits.DocumentNodes = b.limits.TotalNodes
			}
			for range 2 {
				got, e := d.TrailingWhitespace(context.Background(), b, index, ParseOptions{}, 2, false, false)
				var limit *LimitExceeded
				// Direct error and atomic-output oracle precedes any accounting assertions.
				if !errors.As(e, &limit) || got != nil {
					t.Fatal("direct quota omitted", kind, source, got, e)
				}
			}
		}
	}
}

func TestTrailingWhitespaceDetachedAndConcurrent(t *testing.T) {
	d, b, index := sectionParse(t, "a \nb \nc \nd \ne \n")
	got, e := d.TrailingWhitespace(context.Background(), b, index, ParseOptions{}, 2, false, false)
	if e != nil || len(got) != 5 {
		t.Fatal(got, e)
	}
	first := got[0]
	got[0] = Span{}
	again, e := d.TrailingWhitespace(context.Background(), b, index, ParseOptions{}, 2, false, false)
	if e != nil || again[0] != first {
		t.Fatal("aliased facts", again, e)
	}
	for range 4 {
		t.Run("independent", func(t *testing.T) {
			t.Parallel()
			for range 8 {
				d, b, index := sectionParse(t, "a \n")
				got, e := d.TrailingWhitespace(context.Background(), b, index, ParseOptions{}, 2, false, false)
				if e != nil || len(got) != 1 {
					t.Fatal(got, e)
				}
			}
		})
	}
}

// Totals are derived from physical line initialization/scans, actual final tree
// edges/markers, explicit doubling capacities and copied prefixes. They are
// checked only AFTER the direct error oracle, so a missing charge fails as a
// semantic quota omission rather than a later incidental metric mismatch.
func TestTrailingWhitespaceExactReservationsAndFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		source      string
		facts       int
		work, nodes int64
		final       int
	}{
		{"", 0, 3, 1, 4}, {"clean\n", 0, 11, 4, 11}, {"a \nb \nc \nd \ne \n", 5, 83, 66, 57},
	} {
		d, b, index := sectionParse(t, tc.source)
		work, nodes := b.work, b.nodes
		counter := &headingConstructStop{Context: context.Background(), stop: 1000000}
		got, e := d.TrailingWhitespace(counter, b, index, ParseOptions{}, 2, false, false)
		t.Logf("source=%q work=%d nodes=%d calls=%d", tc.source, b.work-work, b.nodes-nodes, counter.calls)
		if e != nil || len(got) != tc.facts {
			t.Fatal(got, e)
		}
		actualWork, actualNodes := b.work-work, b.nodes-nodes
		for _, kind := range []string{"work", "nodes"} {
			d, b, index = sectionParse(t, tc.source)
			if kind == "work" {
				b.limits.WorkUnits = b.work + tc.work - 1
			} else {
				b.limits.TotalNodes = b.nodes + tc.nodes - 1
				b.limits.DocumentNodes = b.limits.TotalNodes
			}
			got, e = d.TrailingWhitespace(context.Background(), b, index, ParseOptions{}, 2, false, false)
			var limit *LimitExceeded
			if !errors.As(e, &limit) || got != nil {
				t.Fatal("direct exact quota omitted", kind, tc, got, e)
			}
		}
		// Exact finite allowances still publish all detached results.
		for _, kind := range []string{"work", "nodes"} {
			d, b, index = sectionParse(t, tc.source)
			if kind == "work" {
				b.limits.WorkUnits = b.work + tc.work
			} else {
				b.limits.TotalNodes = b.nodes + tc.nodes
				b.limits.DocumentNodes = b.limits.TotalNodes
			}
			got, e = d.TrailingWhitespace(context.Background(), b, index, ParseOptions{}, 2, false, false)
			if e != nil || len(got) != tc.facts {
				t.Fatal("exact finite allowance", kind, got, e)
			}
		}
		if actualWork != tc.work || actualNodes != tc.nodes {
			t.Fatal("independent accounting total", tc, actualWork, actualNodes)
		}
		if tc.final > 0 {
			d, b, index = sectionParse(t, tc.source)
			stop := &headingConstructStop{Context: context.Background(), stop: tc.final}
			got, e = d.TrailingWhitespace(stop, b, index, ParseOptions{}, 2, false, false)
			if !errors.Is(e, context.Canceled) || got != nil || stop.calls != tc.final {
				t.Fatal("final producer escaped", got, e, stop.calls)
			}
		}
	}
}
