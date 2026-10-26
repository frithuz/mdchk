package parser

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestListSpacingProjectionSourceOwnership(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, tc := range []struct {
			source              string
			root                []Kind
			definitions, points int
		}{
			{"Text\n- One", []Kind{Paragraph, List}, 0, 1},
			{"- One\nTail", []Kind{List}, 0, 0},
			{"- One\n# H", []Kind{List, Heading}, 0, 1},
			{"- One\n\n# H", []Kind{List, Heading}, 0, 0},
			{"[a]: /url\n- One", []Kind{List}, 1, 1},
			{"- [a]: /url\n  - One", []Kind{List}, 1, 1},
			{"- [a]: /url\n# H", []Kind{List, Heading}, 1, 1},
			{"> [a]: /url\n- One", []Kind{BlockQuote, List}, 1, 1},
			{"- [a]: /url\n  > - One", []Kind{List}, 1, 0},
			{"-\n# H", []Kind{List, Heading}, 0, 1},
			{"- -\n# H", []Kind{List, Heading}, 0, 1},
			{"- > Quote\n  >\n# H", []Kind{List, Heading}, 0, 1},
			{"- ```\n  \n# H", []Kind{List, Heading}, 0, 1},
			{"- <script>\n  \n# H", []Kind{List, Heading}, 0, 1},
			{"- ```\n  \n  ```\n# H", []Kind{List, Heading}, 0, 1},
			{"- <script>\n  \n  </script>\n# H", []Kind{List, Heading}, 0, 1},
			{"> - One\n>\n> # H", []Kind{BlockQuote}, 0, 0},
			{"- One\n+ Two\n1. Three\n2) Four", []Kind{List, List, List, List}, 0, 0},
		} {
			t.Run(tc.source+map[bool]string{false: "/commonmark", true: "/gfm"}[gfm], func(t *testing.T) {
				source := NewSource([]byte(tc.source))
				b, _ := NewOperationBudget(DefaultLimits())
				options := ParseOptions{GFM: gfm}
				p := ParseWithOptions(context.Background(), source, b, options)
				if p.Status != Complete {
					t.Fatal(p)
				}
				d := p.Document
				var kinds []Kind
				for _, n := range d.root.spec.Children {
					kinds = append(kinds, n.Kind())
				}
				if !reflect.DeepEqual(kinds, tc.root) || len(d.references.Definitions) != tc.definitions {
					t.Fatal("source membership", kinds, d.references)
				}
				index := NewLineIndex(source)
				got, err := d.ListSpacing(context.Background(), b, index, options)
				if err != nil || len(got) != tc.points {
					t.Fatal(got, err)
				}
				for _, fact := range got {
					if !fact.Point.Present() || fact.Point.SourceToken() != source.Token() || fact.Point.Start() != fact.Point.End() {
						t.Fatal(fact)
					}
				}
				if len(got) > 0 {
					want := got[0]
					got[0] = ListSpacingFact{}
					again, err := d.ListSpacing(context.Background(), b, index, options)
					if err != nil || again[0] != want {
						t.Fatal(again, err)
					}
				}
				if tc.source == "- One\n\n# H" {
					list := d.root.spec.Children[0]
					if list.spec.Envelope.End() != 7 || d.root.spec.Children[1].spec.Envelope.Start() != 7 {
						t.Fatal("owned zero-gap premise")
					}
					found := false
					for _, m := range list.spec.Children[0].spec.Markers {
						found = found || m.Role == "item-blank" && m.Source.Start() == 6
					}
					if !found {
						t.Fatal("owned blank missing")
					}
				}
			})
		}
	}
}

func TestListSpacingProjectionRejectsForeignAuthority(t *testing.T) {
	d, b, index := sectionParse(t, "Text\n- One")
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
		{constructed, b, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}}, {d, b, NewLineIndex(NewSource([]byte("Text\n- One"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}},
	} {
		if got, e := tc.d.ListSpacing(context.Background(), tc.b, tc.index, tc.options); e == nil || got != nil {
			t.Fatal(got, e)
		}
	}
	d.parsedComplete = false
	if got, e := d.ListSpacing(context.Background(), b, index, ParseOptions{}); e == nil || got != nil {
		t.Fatal(got, e)
	}
}

// Fixed original costs, independently recorded before the first candidate freeze.
// Each shape proves actual membership before imposing a quota. A missing storage,
// copy, logical-record, association or visit charge can make the helper succeed;
// failure must come directly from this producer, never a later engine consumer.
func TestListSpacingProjectionOriginalReservations(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		facts        int
		nodes, work  int64
	}{
		{"empty", "", 0, 1, 3},
		{"no list line state", "plain\nplain", 0, 3, 19},
		{"clean traversal", "- one", 0, 12, 32},
		{"prefix and definitions", "> [a]: /url\n> - one", 1, 24, 81},
		{"output capacity copies records", "Text\n- a\n# H\n- b\n# J\n- c\n# K", 6, 45, 172},
	} {
		for _, kind := range []string{"nodes", "work"} {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				d, b, index := sectionParse(t, tc.source)
				original, e := d.ListSpacing(context.Background(), b, index, ParseOptions{})
				if e != nil || len(original) != tc.facts {
					t.Fatal("actual premise", original, e)
				}
				d, b, index = sectionParse(t, tc.source)
				if kind == "nodes" {
					b.limits.TotalNodes = b.nodes + tc.nodes - 1
					b.limits.DocumentNodes = b.limits.TotalNodes
				} else {
					b.limits.WorkUnits = b.work + tc.work - 1
				}
				for range 2 {
					got, e := d.ListSpacing(context.Background(), b, index, ParseOptions{})
					var limit *LimitExceeded
					if !errors.As(e, &limit) || got != nil {
						t.Fatal("producer must withhold", got, e)
					}
				}
			})
		}
	}
}

func TestListSpacingProjectionFixedFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		source string
		final  int
	}{{"", 4}, {"- one", 24}, {"Text\n- one\n# H", 40}} {
		d, b, index := sectionParse(t, tc.source)
		caller := &headingConstructStop{Context: context.Background(), stop: tc.final}
		got, e := d.ListSpacing(caller, b, index, ParseOptions{})
		if !errors.Is(e, context.Canceled) || got != nil || caller.calls != tc.final {
			t.Fatal("final inventory escaped", got, e, caller.calls, tc.final)
		}
	}
}
