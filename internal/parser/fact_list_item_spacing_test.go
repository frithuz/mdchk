package parser

import (
	"context"
	"errors"
	"testing"
)

func TestListItemSpacingProjectionRejectsForeignAuthority(t *testing.T) {
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
		{nil, b, index, ParseOptions{}}, {d, nil, index, ParseOptions{}}, {d, b, LineIndex{}, ParseOptions{}}, {constructed, b, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}}, {d, b, NewLineIndex(NewSource([]byte("Text\n- One"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}},
	} {
		if got, e := tc.d.ListItemSpacing(context.Background(), tc.b, tc.index, tc.options, "consistent"); e == nil || got != nil {
			t.Fatal(got, e)
		}
	}
	d.parsedComplete = false
	if got, e := d.ListItemSpacing(context.Background(), b, index, ParseOptions{}, "consistent"); e == nil || got != nil {
		t.Fatal(got, e)
	}
}

func TestListItemSpacingProjectionActualSourceOwnership(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, tc := range []struct {
			source                    string
			child                     Kind
			definitions, count, start int
		}{
			{"- ```\n  \n- B\n", CodeBlock, 0, 0, -1},
			{"- ```\n  \n  ```\n\n- B\n", CodeBlock, 0, 1, 15},
			{"- <script>\n  \n- B\n", HTMLBlock, 0, 0, -1},
			{"- > A\n  >\n- B\n", BlockQuote, 0, 0, -1},
			{"- [a]: /url\n\n- B\n", "", 1, 1, 12},
			{"- - A\n\n- B\n", List, 0, 1, 6},
			{"-\n\n\n- B\n", "", 0, 2, 2},
		} {
			source := NewSource([]byte(tc.source))
			b, _ := NewOperationBudget(DefaultLimits())
			options := ParseOptions{GFM: gfm}
			p := ParseWithOptions(context.Background(), source, b, options)
			if p.Status != Complete {
				t.Fatal(p)
			}
			root := p.Document.root.spec.Children
			if len(root) != 1 || root[0].Kind() != List || len(root[0].spec.Children) != 2 || len(p.Document.references.Definitions) != tc.definitions {
				t.Fatal("actual list/definition premise", tc, root)
			}
			children := root[0].spec.Children[0].spec.Children
			if tc.child != "" && (len(children) != 1 || children[0].Kind() != tc.child) {
				t.Fatal("protection/terminal child premise", tc, children)
			}
			got, e := p.Document.ListItemSpacing(context.Background(), b, NewLineIndex(source), options, "tight")
			n := 0
			if tc.count > 0 {
				n = 1
			}
			if e != nil || len(got) != n {
				t.Fatal(tc, got, e)
			}
			if n > 0 {
				want := got[0]
				if want.Actual != tc.count || want.Expected != 0 || want.Span.Start() != tc.start {
					t.Fatal(tc, want)
				}
				got[0] = ListItemSpacingFact{}
				again, e := p.Document.ListItemSpacing(context.Background(), b, NewLineIndex(source), options, "tight")
				if e != nil || len(again) != 1 || again[0] != want {
					t.Fatal("detached", again, e)
				}
			}
		}
	}
}

// Costs are fixed against the original traversal before the first freeze. The
// table covers root/line state, clean groups, prefix rollback, definition
// association and output growth/copy/logical records. Each quota fails directly
// in this producer; a later engine charge cannot mask a missing reservation.
func TestListItemSpacingProjectionOriginalReservations(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		facts        int
		nodes, work  int64
	}{
		{"empty", "", 0, 1, 14},
		{"no list", "plain\nplain", 0, 3, 30},
		{"clean group", "- A\n- B", 0, 15, 66},
		{"prefix and definitions", "> - [a]: /url\n>\n> - B\n> - C", 1, 40, 164},
		{"output growth copies records", "- A\n\n- B\n- C\n- D\n- E\n- F", 4, 39, 192},
	} {
		for _, kind := range []string{"nodes", "work"} {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				d, b, index := sectionParse(t, tc.source)
				original, e := d.ListItemSpacing(context.Background(), b, index, ParseOptions{}, "consistent")
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
					got, e := d.ListItemSpacing(context.Background(), b, index, ParseOptions{}, "consistent")
					var limit *LimitExceeded
					if !errors.As(e, &limit) || got != nil {
						t.Fatal("direct sticky producer failure", got, e)
					}
				}
			})
		}
	}
}

func TestListItemSpacingProjectionFixedFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		source string
		final  int
	}{{"", 5}, {"- A\n- B", 40}, {"- A\n\n- B\n- C", 60}} {
		d, b, index := sectionParse(t, tc.source)
		caller := &headingConstructStop{Context: context.Background(), stop: tc.final}
		got, e := d.ListItemSpacing(caller, b, index, ParseOptions{}, "consistent")
		if !errors.Is(e, context.Canceled) || got != nil || caller.calls != tc.final {
			t.Fatal("final inventory escaped", got, e, caller.calls)
		}
	}
}

func TestListItemSpacingProjectionGFMOwnershipPremises(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, tc := range []struct {
			source string
			table  bool
		}{
			{"- A | B\n  --- | ---\n  X | Y\n- C\n", true},
			{"- [ ] A\n\n- [x] B\n- [ ] C\n", false},
		} {
			source := NewSource([]byte(tc.source))
			b, _ := NewOperationBudget(DefaultLimits())
			options := ParseOptions{GFM: gfm}
			p := ParseWithOptions(context.Background(), source, b, options)
			if p.Status != Complete {
				t.Fatal(p)
			}
			list := p.Document.root.spec.Children[0]
			if list.Kind() != List || len(list.spec.Children) < 2 {
				t.Fatal("actual siblings", list)
			}
			first := list.spec.Children[0]
			child := first.spec.Children[0]
			if tc.table && (child.Kind() == Table) != gfm {
				t.Fatal("actual table ownership", gfm, child.Kind())
			}
			if !tc.table && (first.spec.Properties.Task != nil) != gfm {
				t.Fatal("actual task ownership", gfm, first.spec.Properties.List)
			}
			facts, e := p.Document.ListItemSpacing(context.Background(), b, NewLineIndex(source), options, "consistent")
			want := 0
			if !tc.table {
				want = 1
			}
			if e != nil || len(facts) != want {
				t.Fatal(facts, e)
			}
		}
	}
}
