package parser

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBlankBodyLinesRejectsForeignAuthority(t *testing.T) {
	d, b, index := sectionParse(t, "a\n\n\n")
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
		{constructed, b, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}}, {d, b, NewLineIndex(NewSource([]byte("a\n\n\n"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}},
	} {
		if got, e := tc.d.BlankBodyLines(context.Background(), tc.b, tc.index, tc.options, 1); e == nil || got != nil {
			t.Fatal(got, e)
		}
	}
	//nolint:staticcheck // Missing caller authority is deliberately rejected.
	if got, e := d.BlankBodyLines(nil, b, index, ParseOptions{}, 1); e == nil || got != nil {
		t.Fatal(got, e)
	}
	d.parsedComplete = false
	if got, e := d.BlankBodyLines(context.Background(), b, index, ParseOptions{}, 1); e == nil || got != nil {
		t.Fatal(got, e)
	}
}

func TestBlankBodyLinesDirectQuota(t *testing.T) {
	for _, source := range []string{"", "clean\n", "a\n\n\nb\n\n\nc\n\n\nd\n\n\ne\n\n\n", "<!-- protected --> \n", strings.Repeat("a\n\n\n", 256)} {
		for _, kind := range []string{"work", "nodes"} {
			d, b, index := sectionParse(t, source)
			if kind == "work" {
				b.limits.WorkUnits = b.work + 1
			} else {
				b.limits.TotalNodes = b.nodes
				b.limits.DocumentNodes = b.limits.TotalNodes
			}
			for range 2 {
				got, e := d.BlankBodyLines(context.Background(), b, index, ParseOptions{}, 1)
				var limit *LimitExceeded
				// Direct error and atomic-output oracle precedes any accounting assertions.
				if !errors.As(e, &limit) || got != nil {
					t.Fatal("direct quota omitted", kind, source, got, e)
				}
			}
		}
	}
}

func TestBlankBodyLinesDetachedAndConcurrent(t *testing.T) {
	d, b, index := sectionParse(t, "a\n\n\nb\n\n\nc\n\n\nd\n\n\ne\n\n\n")
	got, e := d.BlankBodyLines(context.Background(), b, index, ParseOptions{}, 1)
	if e != nil || len(got) != 5 {
		t.Fatal(got, e)
	}
	first := got[0]
	got[0] = Span{}
	again, e := d.BlankBodyLines(context.Background(), b, index, ParseOptions{}, 1)
	if e != nil || again[0] != first {
		t.Fatal("aliased facts", again, e)
	}
	for range 4 {
		t.Run("independent", func(t *testing.T) {
			t.Parallel()
			for range 8 {
				d, b, index := sectionParse(t, "a\n\n\n")
				got, e := d.BlankBodyLines(context.Background(), b, index, ParseOptions{}, 1)
				if e != nil || len(got) != 1 {
					t.Fatal(got, e)
				}
			}
		})
	}
}

// Reservations: entry(1w,1n), line table(L+1w,Ln), initialization(Lw),
// suffix bytes including failed probes, root frame(1w,2n), each node/edge/
// marker/association(1w), each child(1n), lookup(bits.Len(L)+1w), doubling
// growth(length+1w,newCapacity n), emitted span(1w,1n), final(0w,0n).
// Totals are derived from physical line initialization/scans, actual final tree
// edges/markers, explicit doubling capacities and copied prefixes. They are
// checked only AFTER the direct error oracle, so a missing charge fails as a
// semantic quota omission rather than a later incidental metric mismatch.
func TestBlankBodyLinesExactReservationsAndFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		source      string
		facts       int
		work, nodes int64
		final       int
	}{
		{"", 0, 4, 3, 5}, {"clean\n", 0, 12, 6, 12}, {"<!-- protected -->\n", 0, 16, 5, 14}, {"a\n\n\na\n\n\na\n\n\na\n\n\na\n\n\n", 5, 100, 62, 72},
	} {
		d, b, index := sectionParse(t, tc.source)
		work, nodes := b.work, b.nodes
		counter := &headingConstructStop{Context: context.Background(), stop: 1000000}
		got, e := d.BlankBodyLines(counter, b, index, ParseOptions{}, 1)
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
			got, e = d.BlankBodyLines(context.Background(), b, index, ParseOptions{}, 1)
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
			got, e = d.BlankBodyLines(context.Background(), b, index, ParseOptions{}, 1)
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
			got, e = d.BlankBodyLines(stop, b, index, ParseOptions{}, 1)
			if !errors.Is(e, context.Canceled) || got != nil || stop.calls != tc.final {
				t.Fatal("final producer escaped", got, e, stop.calls)
			}
		}
	}
}

// Premises assert actual retained owners independently of the blank projection.
func TestBlankBodyLinesActualOwnershipPremises(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, tc := range []struct {
			source     string
			kind       Kind
			start, end int
			raw        bool
		}{
			{"    a\n\n    b\n\n\nend\n", CodeBlock, 0, 13, false},
			{">     a\n>\n>     \n>     b\n>\n> \n> text\n", CodeBlock, 2, 25, false},
			{"```\n\n\n```\n\n\nend\n", CodeBlock, 0, 10, false},
			{"x <script>\n\n\n</script>\n", InlineHTML, 2, 10, true},
			{"a `x\n y` z\n", CodeSpan, 2, 8, false},
		} {
			s := NewSource([]byte(tc.source))
			b, _ := NewOperationBudget(DefaultLimits())
			p := ParseWithOptions(context.Background(), s, b, ParseOptions{GFM: gfm})
			if p.Status != Complete {
				t.Fatal(p)
			}
			found := false
			var walk func(*Node)
			walk = func(n *Node) {
				if n.Kind() == tc.kind && n.spec.Envelope.Start() == tc.start && n.spec.Envelope.End() == tc.end {
					if tc.raw && n.rawTag != "script" {
						t.Fatal("missing actual raw opener")
					}
					found = true
				}
				for _, child := range n.spec.Children {
					walk(child)
				}
				for _, child := range n.htmlTail {
					walk(child)
				}
			}
			walk(p.Document.root)
			if !found {
				t.Fatal("missing literal owner", tc)
			}
		}
	}
}

type blankLineStop struct {
	context.Context
	calls, stop int
	reason      error
}

func (c *blankLineStop) Err() error {
	c.calls++
	if c.calls >= c.stop {
		return c.reason
	}
	return nil
}
func TestBlankBodyLinesCallerDeadlineAndFinalAtomicity(t *testing.T) {
	for _, tc := range []struct {
		source string
		final  int
	}{{"", 5}, {"clean\n", 12}, {"<!-- protected -->\n", 14}, {"a\n\n\na\n\n\na\n\n\na\n\n\na\n\n\n", 72}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			for _, stop := range []int{1, tc.final} {
				d, b, index := sectionParse(t, tc.source)
				ctx := &blankLineStop{Context: context.Background(), stop: stop, reason: reason}
				got, err := d.BlankBodyLines(ctx, b, index, ParseOptions{}, 1)
				if !errors.Is(err, reason) || got != nil {
					t.Fatal("direct caller stop escaped", got, err)
				}
				if ctx.calls != stop {
					t.Fatal(ctx.calls, stop)
				}
			}
		}
	}
}
