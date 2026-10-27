package parser

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMaximumLineWidthRejectsForeignAuthority(t *testing.T) {
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
		if got, e := tc.d.MaximumLineWidth(context.Background(), tc.b, tc.index, tc.options, 1, false, false, true); e == nil || got != nil {
			t.Fatal(got, e)
		}
	}
	//nolint:staticcheck // Missing caller authority is deliberately rejected.
	if got, e := d.MaximumLineWidth(nil, b, index, ParseOptions{}, 1, false, false, true); e == nil || got != nil {
		t.Fatal(got, e)
	}
	d.parsedComplete = false
	if got, e := d.MaximumLineWidth(context.Background(), b, index, ParseOptions{}, 1, false, false, true); e == nil || got != nil {
		t.Fatal(got, e)
	}
}

func TestMaximumLineWidthDirectQuota(t *testing.T) {
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
				got, e := d.MaximumLineWidth(context.Background(), b, index, ParseOptions{}, 1, false, false, true)
				var limit *LimitExceeded
				// Direct error and atomic-output oracle precedes any accounting assertions.
				if !errors.As(e, &limit) || got != nil {
					t.Fatal("direct quota omitted", kind, source, got, e)
				}
			}
		}
	}
}

func TestMaximumLineWidthDetachedAndConcurrent(t *testing.T) {
	d, b, index := sectionParse(t, "a\t\nb\t\nc\t\nd\t\ne\t\n")
	got, e := d.MaximumLineWidth(context.Background(), b, index, ParseOptions{}, 1, false, false, true)
	if e != nil || len(got) != 5 {
		t.Fatal(got, e)
	}
	first := got[0]
	got[0] = Span{}
	again, e := d.MaximumLineWidth(context.Background(), b, index, ParseOptions{}, 1, false, false, true)
	if e != nil || again[0] != first {
		t.Fatal("aliased facts", again, e)
	}
	for range 4 {
		t.Run("independent", func(t *testing.T) {
			t.Parallel()
			for range 8 {
				d, b, index := sectionParse(t, "a\t\n")
				got, e := d.MaximumLineWidth(context.Background(), b, index, ParseOptions{}, 1, false, false, true)
				if e != nil || len(got) != 1 {
					t.Fatal(got, e)
				}
			}
		})
	}
}

// These assertions establish actual retained ownership separately from MD013.
func TestMaximumLineWidthRetainedOwnershipPremises(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		t.Run(map[bool]string{false: "commonmark", true: "gfm"}[gfm], func(t *testing.T) {
			source := NewSource([]byte("<!-- c --><script>\nlongbody\n</script> tail\n"))
			b, _ := NewOperationBudget(DefaultLimits())
			p := ParseWithOptions(context.Background(), source, b, ParseOptions{GFM: gfm})
			if p.Status != Complete || len(p.Document.root.spec.Children) != 2 {
				t.Fatal(p)
			}
			block := p.Document.root.spec.Children[0]
			paragraph := p.Document.root.spec.Children[1]
			if block.Kind() != HTMLBlock || block.spec.Envelope.End() != 19 || len(block.htmlTail) != 1 || block.htmlTail[0].rawTag != "script" || block.htmlTail[0].rawClosing {
				t.Fatal(block)
			}
			if paragraph.Kind() != Paragraph {
				t.Fatal(paragraph)
			}
			found := false
			for _, n := range paragraph.spec.Children {
				if n.rawTag == "script" && n.rawClosing && n.spec.Envelope.End() == 37 {
					found = true
				}
			}
			if !found {
				t.Fatal("actual raw closer ownership missing")
			}
			source = NewSource([]byte("\tb"))
			b, _ = NewOperationBudget(DefaultLimits())
			p = ParseWithOptions(context.Background(), source, b, ParseOptions{GFM: gfm})
			if p.Status != Complete || len(p.Document.root.spec.Children) != 1 || p.Document.root.spec.Children[0].Kind() != CodeBlock {
				t.Fatal("leading TAB is intrinsic code", p)
			}
		})
	}
}

// Reservation equation (independent of observed counters): entry W1/N1;
// line-state W(L+1)/N(L), initialization WL; root W1/N2;
// each popped node W1, each child frame W1/N1; doubled capacity C costs
// W(old length+1)/NC; every literal segment costs visit W1 + mark W1 +
// capacity + record W1/N1; each sorted interval list costs
// 8*K*(bits.Len(K)+1)+1 work; each line W1; each decoded scalar W8;
// each visited interval W1; each finding costs capacity plus W1/N1.
// Final publication performs a zero charge and a caller check.
func TestMaximumLineWidthExactReservationsAndFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		facts        int
		work, nodes  int64
		calls        int
	}{
		{"empty", "", 0, 6, 3, 7}, {"clean", "a\n", 0, 21, 6, 14},
		{"protected", "<!-- x -->\n", 0, 112, 7, 26},
		{"growth", "aa\naa\naa\naa\naa\n", 5, 156, 68, 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, kind := range []string{"work", "nodes"} {
				d, b, index := sectionParse(t, tc.source)
				if kind == "work" {
					b.limits.WorkUnits = b.work + tc.work - 1
				} else {
					b.limits.TotalNodes = b.nodes + tc.nodes - 1
					b.limits.DocumentNodes = b.limits.TotalNodes
				}
				got, e := d.MaximumLineWidth(context.Background(), b, index, ParseOptions{}, 1, false, false, true)
				var limit *LimitExceeded
				if !errors.As(e, &limit) || got != nil {
					t.Fatal("direct exact quota omitted", kind, got, e)
				}
				d, b, index = sectionParse(t, tc.source)
				if kind == "work" {
					b.limits.WorkUnits = b.work + tc.work
				} else {
					b.limits.TotalNodes = b.nodes + tc.nodes
					b.limits.DocumentNodes = b.limits.TotalNodes
				}
				got, e = d.MaximumLineWidth(context.Background(), b, index, ParseOptions{}, 1, false, false, true)
				if e != nil || len(got) != tc.facts {
					t.Fatal("exact finite allowance", kind, got, e)
				}
			}
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				d, b, index := sectionParse(t, tc.source)
				stop := &widthProjectionStop{Context: context.Background(), stop: tc.calls, reason: reason}
				got, e := d.MaximumLineWidth(stop, b, index, ParseOptions{}, 1, false, false, true)
				if !errors.Is(e, reason) || got != nil {
					t.Fatal("direct final Err oracle", got, e)
				}
				if stop.calls != tc.calls {
					t.Fatal("final calls", stop.calls, tc.calls)
				}
			}
			d, b, index := sectionParse(t, tc.source)
			work, nodes := b.work, b.nodes
			counter := &widthProjectionStop{Context: context.Background(), stop: 1000000}
			got, e := d.MaximumLineWidth(counter, b, index, ParseOptions{}, 1, false, false, true)
			if e != nil || len(got) != tc.facts || b.work-work != tc.work || b.nodes-nodes != tc.nodes || counter.calls != tc.calls {
				t.Fatal("fixed equation", got, e, b.work-work, b.nodes-nodes, counter.calls, tc)
			}
		})
	}
}

type widthProjectionStop struct {
	context.Context
	calls, stop int
	reason      error
}

func (c *widthProjectionStop) Err() error {
	c.calls++
	if c.calls >= c.stop {
		return c.reason
	}
	return nil
}

func TestMaximumLineWidthSyntheticUsesDoNotRescanDestinations(t *testing.T) {
	// Actual reference uses only have a spanless synthetic destination. One
	// definition is inspected once; doubling uses cannot multiply destination work.
	for _, gfm := range []bool{false, true} {
		t.Run(map[bool]string{false: "commonmark", true: "gfm"}[gfm], func(t *testing.T) {
			source := NewSource([]byte(strings.Repeat("[x] ", 128) + "\n\n[x]: " + strings.Repeat("d", 4096) + "\n"))
			b, _ := NewOperationBudget(DefaultLimits())
			options := ParseOptions{GFM: gfm}
			p := ParseWithOptions(context.Background(), source, b, options)
			if p.Status != Complete {
				t.Fatal(p)
			}
			work := b.work
			b.limits.WorkUnits = work + 80000
			got, e := p.Document.MaximumLineWidth(context.Background(), b, NewLineIndex(source), options, 1, false, false, true)
			if e != nil || len(got) != 2 {
				t.Fatal("bounded actual synthetic uses", got, e)
			}
			if b.work-work >= 80000 {
				t.Fatal("unbounded semantic rescans")
			}
		})
	}
}

func TestMaximumLineWidthContributionOwnershipPremises(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		t.Run(map[bool]string{false: "commonmark", true: "gfm"}[gfm], func(t *testing.T) {
			for _, tc := range []struct {
				source  string
				kind    Kind
				content [][2]int
				partial bool
			}{
				{"- a\n  p `x\n\tq`\tout\n", CodeSpan, [][2]int{{9, 11}, {12, 13}}, false},
				{"- a\n  p <span title=\"x\n\tq\">\tout\n", InlineHTML, [][2]int{{8, 23}, {24, 27}}, false},
				{"- <div>\n\tpayload\n\n", HTMLBlock, [][2]int{{2, 8}, {8, 9}, {9, 17}}, true},
				{"- ```\n\tx\n  ```\n", CodeBlock, [][2]int{{6, 7}, {7, 8}, {8, 9}}, true},
			} {
				source := NewSource([]byte(tc.source))
				b, _ := NewOperationBudget(DefaultLimits())
				p := ParseWithOptions(context.Background(), source, b, ParseOptions{GFM: gfm})
				if p.Status != Complete {
					t.Fatal(p)
				}
				var owner *Node
				stack := []*Node{p.Document.root}
				for len(stack) > 0 {
					n := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					if n.Kind() == tc.kind {
						owner = n
					}
					stack = append(stack, n.spec.Children...)
				}
				if owner == nil || len(owner.spec.Content) != len(tc.content) {
					t.Fatal(tc.kind, owner)
				}
				partial := false
				for i, s := range owner.spec.Content {
					if s.Source.start != tc.content[i][0] || s.Source.end != tc.content[i][1] {
						t.Fatal("actual contribution", tc.kind, i, s.Source, tc.content)
					}
					partial = partial || s.Transformation == PartialTab
				}
				if partial != tc.partial {
					t.Fatal("actual atomic TAB premise", tc.kind, partial)
				}
			}
		})
	}
}
