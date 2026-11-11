package parser

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestTabCharactersRejectsForeignAuthority(t *testing.T) {
	d, b, index := sectionParse(t, "a\t\n")
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
		{constructed, b, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}}, {d, b, NewLineIndex(NewSource([]byte("a\t\n"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}},
	} {
		if got, e := tc.d.TabCharacters(context.Background(), tc.b, tc.index, tc.options, false); e == nil || got != nil {
			t.Fatal(got, e)
		}
	}
	//nolint:staticcheck // Missing caller authority is deliberately rejected.
	if got, e := d.TabCharacters(nil, b, index, ParseOptions{}, false); e == nil || got != nil {
		t.Fatal(got, e)
	}
	d.parsedComplete = false
	if got, e := d.TabCharacters(context.Background(), b, index, ParseOptions{}, false); e == nil || got != nil {
		t.Fatal(got, e)
	}
}

func TestTabCharactersDirectQuota(t *testing.T) {
	for _, source := range []string{"", "clean\n", "a\t\nb\t\nc\t\nd\t\ne\t\n", "<!-- protected --> \n", strings.Repeat("a\t\n", 256)} {
		for _, kind := range []string{"work", "nodes"} {
			d, b, index := sectionParse(t, source)
			if kind == "work" {
				b.limits.WorkUnits = b.work + 1
			} else {
				b.limits.TotalNodes = b.nodes
				b.limits.DocumentNodes = b.limits.TotalNodes
			}
			for range 2 {
				got, e := d.TabCharacters(context.Background(), b, index, ParseOptions{}, false)
				var limit *LimitExceeded
				// Direct error and atomic-output oracle precedes any accounting assertions.
				if !errors.As(e, &limit) || got != nil {
					t.Fatal("direct quota omitted", kind, source, got, e)
				}
			}
		}
	}
}

func TestTabCharactersDetachedAndConcurrent(t *testing.T) {
	d, b, index := sectionParse(t, "a\t\nb\t\nc\t\nd\t\ne\t\n")
	got, e := d.TabCharacters(context.Background(), b, index, ParseOptions{}, false)
	if e != nil || len(got) != 5 {
		t.Fatal(got, e)
	}
	first := got[0]
	got[0] = Span{}
	again, e := d.TabCharacters(context.Background(), b, index, ParseOptions{}, false)
	if e != nil || again[0] != first {
		t.Fatal("aliased facts", again, e)
	}
	for range 4 {
		t.Run("independent", func(t *testing.T) {
			t.Parallel()
			for range 8 {
				d, b, index := sectionParse(t, "a\t\n")
				got, e := d.TabCharacters(context.Background(), b, index, ParseOptions{}, false)
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
func TestTabCharactersExactReservationsAndFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		source      string
		facts       int
		work, nodes int64
		final       int
	}{
		{"", 0, 4, 3, 5}, {"clean\n", 0, 16, 6, 16}, {"a\tb\tc\td\te\t\n", 5, 58, 46, 44},
	} {
		d, b, index := sectionParse(t, tc.source)
		work, nodes := b.work, b.nodes
		counter := &headingConstructStop{Context: context.Background(), stop: 1000000}
		got, e := d.TabCharacters(counter, b, index, ParseOptions{}, false)
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
			got, e = d.TabCharacters(context.Background(), b, index, ParseOptions{}, false)
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
			got, e = d.TabCharacters(context.Background(), b, index, ParseOptions{}, false)
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
			got, e = d.TabCharacters(stop, b, index, ParseOptions{}, false)
			if !errors.Is(e, context.Canceled) || got != nil || stop.calls != tc.final {
				t.Fatal("final producer escaped", got, e, stop.calls)
			}
		}
	}
}

func TestTabCharactersActualOwnershipPremises(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, tc := range []struct {
			source                 string
			kind                   Kind
			indentStart, indentEnd int
			raw                    bool
			contentIndent          int
		}{
			{"-\t\tcode\n", CodeBlock, 1, 3, false, 2},
			{">\t\tcode\n", CodeBlock, 1, 3, false, 0},
			{"-   a\n\n\t\tcode\n", CodeBlock, 8, 9, false, 4},
			{"a <script>x\t\n", InlineHTML, 0, 0, true, 0},
			{"a <script>x\n\n-\t\n", ListItem, 0, 0, true, 2},
		} {
			s := NewSource([]byte(tc.source))
			b, _ := NewOperationBudget(DefaultLimits())
			opts := ParseOptions{GFM: gfm}
			p := ParseWithOptions(context.Background(), s, b, opts)
			if p.Status != Complete {
				t.Fatal(p)
			}
			kind, indent, raw, property := false, tc.indentEnd == 0, !tc.raw, tc.contentIndent == 0
			var walk func(*Node)
			walk = func(n *Node) {
				kind = kind || n.Kind() == tc.kind
				raw = raw || n.rawTag == "script"
				if n.Kind() == ListItem && n.spec.Properties.List.ContentIndent.Value == tc.contentIndent {
					property = true
				}
				for _, m := range n.spec.Markers {
					if m.Role == "indent-prefix" && m.Source.Start() == tc.indentStart && m.Source.End() == tc.indentEnd {
						indent = true
					}
				}
				if tc.source == "a <script>x\t\n" {
					for _, seg := range n.spec.Content {
						if seg.Source.Start() <= 11 && seg.Source.End() > 11 {
							t.Fatal("trimmed raw TAB unexpectedly retained", n.Kind(), seg)
						}
					}
				}
				if n.Kind() == ListItem && tc.source == "a <script>x\n\n-\t\n" {
					v := n.spec.Properties.List.Prefix
					if v == nil || v.Semantic.Value != "-   " {
						t.Fatal("actual empty prefix", v)
					}
				}
				for _, child := range n.spec.Children {
					walk(child)
				}
			}
			walk(p.Document.root)
			if !kind || !indent || !raw || !property {
				t.Fatal(tc, kind, indent, raw, property)
			}
		}
	}
}
