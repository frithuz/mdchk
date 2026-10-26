package parser

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestListContinuationProjectionActualOwnership(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, tc := range []struct {
			source             string
			child              Kind
			definitions, count int
		}{
			{"- One\n\n   Text\n", Paragraph, 0, 1},
			{"- One\n\n Text\n", Paragraph, 0, 0},
			{"- One\n\n   <!-- x -->\n", HTMLBlock, 0, 0},
			{"- One\n\n      code\n", CodeBlock, 0, 0},
			{"- One\n\n  ```\n   ```x\n  ```\n", CodeBlock, 0, 0},
			{"- One\n\n   [x]: /u\n", "", 1, 1},
			{"- [x] One\n   Text\n", Paragraph, 0, 1},
			{"- One\n\n  ```\n  <!-- mdchk:disable MD077 -->\n  ```\n   Text\n", Paragraph, 0, 1},
			{"- One\n\n   | a |\n   | - |\n   | b |\n", Paragraph, 0, 3},
		} {
			source := NewSource([]byte(tc.source))
			b, _ := NewOperationBudget(DefaultLimits())
			options := ParseOptions{GFM: gfm}
			p := ParseWithOptions(context.Background(), source, b, options)
			if p.Status != Complete {
				t.Fatal(p)
			}
			first := p.Document.root.spec.Children[0]
			if first.Kind() != List || len(first.spec.Children) != 1 {
				t.Fatal("list premise", first)
			}
			item := first.spec.Children[0]
			if item.Kind() != ListItem || item.spec.Properties.List.ContentIndent != Some(2) {
				t.Fatal("item premise", item)
			}
			if len(p.Document.references.Definitions) != tc.definitions {
				t.Fatal("definitions premise", tc)
			}
			if tc.child != "" {
				want := tc.child
				if gfm && strings.Contains(tc.source, "| a |") {
					want = Table
				}
				if len(item.spec.Children) == 0 || item.spec.Children[len(item.spec.Children)-1].Kind() != want {
					t.Fatal("leaf premise", tc, item.spec.Children)
				}
			}
			if strings.Contains(tc.source, "mdchk:disable MD077") {
				if len(item.spec.Children) != 3 || item.spec.Children[1].Kind() != CodeBlock || item.spec.Children[1].spec.Properties.Literal != Some("<!-- mdchk:disable MD077 -->\n") || item.spec.Children[2].Kind() != Paragraph || item.spec.Children[2].spec.Envelope.Start() != 52 {
					t.Fatal("actual protected control and later owned paragraph", item.spec.Children)
				}
			}

			if strings.Contains(tc.source, "[x] One") && (item.spec.Properties.Task != nil) != gfm {
				t.Fatal("task premise", gfm)
			}
			if strings.Contains(tc.source, "\n Text") && (len(p.Document.root.spec.Children) != 2 || p.Document.root.spec.Children[1].Kind() != Paragraph) {
				t.Fatal("outside premise")
			}
			got, e := p.Document.ListContinuationIndentation(context.Background(), b, NewLineIndex(source), options)
			if e != nil || len(got) != tc.count {
				t.Fatal(tc, got, e)
			}
			if len(got) > 0 {
				want := got[0]
				got[0] = ListContinuationFact{}
				again, e := p.Document.ListContinuationIndentation(context.Background(), b, NewLineIndex(source), options)
				if e != nil || len(again) != tc.count || again[0] != want {
					t.Fatal("detached", again, e)
				}
			}
		}
	}
}

func TestListContinuationProjectionRejectsForeignAuthority(t *testing.T) {
	d, b, index := sectionParse(t, "- One\n Text\n")
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
		{constructed, b, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}},
		{d, b, NewLineIndex(NewSource([]byte("- One\n Text\n"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}},
	} {
		if got, e := tc.d.ListContinuationIndentation(context.Background(), tc.b, tc.index, tc.options); e == nil || got != nil {
			t.Fatal(got, e)
		}
	}
	//nolint:staticcheck // Absent caller authority is explicitly rejected.
	if got, e := d.ListContinuationIndentation(nil, b, index, ParseOptions{}); e == nil || got != nil {
		t.Fatal(got, e)
	}
	d.parsedComplete = false
	if got, e := d.ListContinuationIndentation(context.Background(), b, index, ParseOptions{}); e == nil || got != nil {
		t.Fatal(got, e)
	}
}

// Fixed charged-path totals: one root slot; exact per-line state; frame
// growth copies and logical pushes; confirmed-prefix replay; line selection;
// final source suffix replay; detached finding storage/copies/logical records.
// The original-path trace is preserved in Working/issue-60-prefreeze-parser-01.log.
func TestListContinuationProjectionOriginalReservations(t *testing.T) {
	for _, tc := range []struct {
		source      string
		facts       int
		nodes, work int64
		final       int
	}{
		{"", 0, 1, 3, 4}, {"plain\nplain", 0, 13, 34, 26},
		{"- One\n  Text\n", 0, 23, 57, 41}, {"- One\n Text\n", 1, 25, 54, 40},
		{"> - One\n>\n>\t Text\n", 1, 27, 97, 68},
		{"- A\n X\n Y\n Z\n W\n", 4, 43, 101, 78},
	} {
		for _, kind := range []string{"nodes", "work", "final"} {
			d, b, index := sectionParse(t, tc.source)
			nodes, work := b.nodes, b.work
			got, e := d.ListContinuationIndentation(context.Background(), b, index, ParseOptions{})
			if e != nil || len(got) != tc.facts {
				t.Fatal("fixed original total", tc, got, e, b.nodes-nodes, b.work-work)
			}
			actualNodes, actualWork := b.nodes-nodes, b.work-work
			d, b, index = sectionParse(t, tc.source)
			caller := context.Background()
			if kind == "nodes" {
				b.limits.TotalNodes = b.nodes + tc.nodes - 1
				b.limits.DocumentNodes = b.limits.TotalNodes
			}
			if kind == "work" {
				b.limits.WorkUnits = b.work + tc.work - 1
			}
			if kind == "final" {
				caller = &headingConstructStop{Context: caller, stop: tc.final}
			}
			got, e = d.ListContinuationIndentation(caller, b, index, ParseOptions{})
			if kind == "final" {
				if !errors.Is(e, context.Canceled) || got != nil {
					t.Fatal("final publication", tc, got, e)
				}
			} else {
				var limit *LimitExceeded
				if !errors.As(e, &limit) || got != nil {
					t.Fatal("direct quota omitted", kind, tc, got, e)
				}
				got, e = d.ListContinuationIndentation(context.Background(), b, index, ParseOptions{})
				if !errors.As(e, &limit) || got != nil {
					t.Fatal("sticky quota", got, e)
				}
			}
			if kind == "nodes" && actualNodes != tc.nodes || kind == "work" && actualWork != tc.work {
				t.Fatal("fixed original total", tc, actualNodes, actualWork)
			}
		}
	}
}

func TestListContinuationProjectionGrowthIsCharged(t *testing.T) {
	for _, source := range []string{strings.Repeat("plain\n", 256), "- A\n" + strings.Repeat(" Text\n", 256), strings.Repeat("- ", 64) + "A\nText"} {
		d, b, index := sectionParse(t, source)
		b.limits.WorkUnits = b.work + 64
		for range 2 {
			got, e := d.ListContinuationIndentation(context.Background(), b, index, ParseOptions{})
			var limit *LimitExceeded
			if !errors.As(e, &limit) || got != nil {
				t.Fatal("direct sticky failure", got, e)
			}
		}
	}
}

func TestListContinuationProjectionFinalInlineAndHeadingProtection(t *testing.T) {
	for _, tc := range []struct {
		source string
		kind   Kind
		inline Kind
	}{
		{"- One\n\n  Title\n   ---\n", Heading, ""},
		{"- One\n  `code\n      literal\n  end`\n", Paragraph, CodeSpan},
		{"- One\n  text <!-- comment\n      literal\n  --> tail\n", Paragraph, InlineHTML},
	} {
		for _, gfm := range []bool{false, true} {
			t.Run(string(tc.kind)+string(tc.inline), func(t *testing.T) {
				source := NewSource([]byte(tc.source))
				b, _ := NewOperationBudget(DefaultLimits())
				options := ParseOptions{GFM: gfm}
				p := ParseWithOptions(context.Background(), source, b, options)
				if p.Status != Complete {
					t.Fatal(p)
				}
				item := p.Document.root.spec.Children[0].spec.Children[0]
				leaf := item.spec.Children[len(item.spec.Children)-1]
				if leaf.Kind() != tc.kind {
					t.Fatal("final leaf premise", leaf.Kind(), tc)
				}
				if tc.inline != "" {
					found := false
					for _, n := range leaf.spec.Children {
						if n.Kind() == tc.inline {
							first, _ := NewLineIndex(source).Position(n.Span().Start())
							last, _ := NewLineIndex(source).Position(n.Span().End())
							found = last.Line > first.Line
						}
					}
					if !found {
						t.Fatal("actual multiline inline premise", tc, leaf.spec.Children)
					}
				}
				got, e := p.Document.ListContinuationIndentation(context.Background(), b, NewLineIndex(source), options)
				if e != nil || len(got) != 0 {
					t.Fatal("final protected source", tc, got, e)
				}
			})
		}
	}
}

func TestListContinuationProjectionSharedTabBetweenItems(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		source := NewSource([]byte("- > - One\n\t> Text\n"))
		b, _ := NewOperationBudget(DefaultLimits())
		options := ParseOptions{GFM: gfm}
		p := ParseWithOptions(context.Background(), source, b, options)
		if p.Status != Complete {
			t.Fatal(p)
		}
		outer := p.Document.root.spec.Children[0].spec.Children[0]
		quote := outer.spec.Children[0]
		inner := quote.spec.Children[0].spec.Children[0]
		if outer.Kind() != ListItem || quote.Kind() != BlockQuote || inner.Kind() != ListItem || outer.spec.Properties.List.ContentIndent != Some(2) || inner.spec.Properties.List.ContentIndent != Some(2) {
			t.Fatal("actual ancestry")
		}
		if len(quote.spec.Markers) != 2 || quote.spec.Markers[1].Source.Start() != 10 || quote.spec.Markers[1].Source.End() != 13 {
			t.Fatal("actual shared quote marker", quote.spec.Markers)
		}
		if len(outer.spec.Markers) != 3 || outer.spec.Markers[2].Source.Start() != 10 || outer.spec.Markers[2].Source.End() != 11 {
			t.Fatal("actual item partial tab", outer.spec.Markers)
		}
		index := NewLineIndex(source)
		c, _ := index.Indentation(2)
		_, _ = c.Consume(2)
		if c.offset != 10 || c.column != 2 || c.tabRemaining != 2 {
			t.Fatal("pending original item tab phase", c)
		}
		replay := blockParser{index: index}
		next, prefix, ok := replay.quotePrefix(c)
		if !ok || prefix != quote.spec.Markers[1].Source || next.offset != 13 || next.column != 6 || next.tabRemaining != 0 {
			t.Fatal("consumed quote columns versus byte extent", next, prefix, ok)
		}
		got, e := p.Document.ListContinuationIndentation(context.Background(), b, NewLineIndex(source), options)
		if e != nil || len(got) != 1 || got[0].Span.Start() != 13 || got[0].Span.End() != 13 || got[0].Actual != 2 || got[0].Expected != 4 {
			t.Fatal(got, e)
		}
		t.Log("actual outer item partial tab [10,11); quote [10,13) consumes four columns; nearest inner Text [13,17); Actual2/Expected4")
	}
}

func TestListContinuationProjectionFiniteChargedFamilies(t *testing.T) {
	families := []struct {
		name   string
		source func(int) string
		facts  func(int) int
	}{
		{"long line", func(n int) string { return "- A\n " + strings.Repeat("x", n) + "\n" }, func(int) int { return 1 }},
		{"many items", func(n int) string { return strings.Repeat("- A\n Text\n", n) }, func(n int) int { return n }},
		{"deep items", func(n int) string { return strings.Repeat("- ", n) + "A\nText\n" }, func(int) int { return 1 }},
	}
	for _, family := range families {
		t.Run(family.name, func(t *testing.T) {
			var previousWork, previousNodes int64
			for _, n := range []int{16, 32} {
				d, b, index := sectionParse(t, family.source(n))
				work, nodes := b.work, b.nodes
				got, e := d.ListContinuationIndentation(context.Background(), b, index, ParseOptions{})
				usedWork, usedNodes := b.work-work, b.nodes-nodes
				if e != nil || len(got) != family.facts(n) || usedWork <= 0 || usedNodes <= 0 {
					t.Fatal("actual charged family", n, got, e, usedWork, usedNodes)
				}
				if previousWork > 0 && (usedWork > 4*previousWork || usedNodes > 4*previousNodes) {
					t.Fatal("finite doubled work/storage bound", n, usedWork, usedNodes, previousWork, previousNodes)
				}
				previousWork, previousNodes = usedWork, usedNodes
			}
		})
	}
}
