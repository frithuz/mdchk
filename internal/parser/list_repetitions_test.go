package parser

import (
	"context"
	"encoding/json"
	"errors"
	"math/bits"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestListRepetitionsProjectionRejectsForeignAuthority(t *testing.T) {
	d, b, index := sectionParse(t, "- - One")
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
		{constructed, b, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}}, {d, b, NewLineIndex(NewSource([]byte("- - One"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}},
	} {
		if got, e := tc.d.ListRepetitions(context.Background(), tc.b, tc.index, tc.options); e == nil || got != nil {
			t.Fatal(got, e)
		}
	}
	d.parsedComplete = false
	if got, e := d.ListRepetitions(context.Background(), b, index, ParseOptions{}); e == nil || got != nil {
		t.Fatal(got, e)
	}
}

func TestListRepetitionsProjectionActualMembership(t *testing.T) {
	data, e := os.ReadFile("../rules/testdata/md069.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixtures []struct {
		Name, Source          string
		Recognize, Incomplete bool
		Expected              []struct {
			Range  []int
			Source string
		}
	}
	if e = json.Unmarshal(data, &fixtures); e != nil {
		t.Fatal(e)
	}
	for _, gfm := range []bool{false, true} {
		for _, f := range fixtures {
			t.Run(f.Name+map[bool]string{false: "/cm", true: "/gfm"}[gfm], func(t *testing.T) {
				fm, _ := NewFrontmatterOptions(f.Recognize, nil)
				options := ParseOptions{GFM: gfm, Frontmatter: fm}
				source := NewSource([]byte(f.Source))
				b, _ := NewOperationBudget(DefaultLimits())
				p := ParseWithOptions(context.Background(), source, b, options)
				if f.Incomplete {
					if p.Status == Complete {
						t.Fatal("uncertain premise", p)
					}
					return
				}
				if p.Status != Complete {
					t.Fatal(p)
				}
				items, e := p.Document.ListItems(context.Background(), b, NewLineIndex(source), options)
				if e != nil {
					t.Fatal(e)
				}
				// Assert protected ownership directly so negative fixtures cannot pass
				// merely because an assumed opaque block was actually ordinary prose.
				var owned Kind
				switch f.Name {
				case "raw protected", "comment protected":
					owned = HTMLBlock
				case "fence", "indented":
					owned = CodeBlock
				case "thematic dash", "thematic star":
					owned = ThematicBreak
				}
				if owned != "" && (len(p.Document.root.spec.Children) != 1 || p.Document.root.spec.Children[0].Kind() != owned) {
					t.Fatal("protected block ownership premise", f.Name, p.Document.root.spec.Children)
				}
				// Positive expectations must consist of real item markers, including EOF,
				// definitions and tasks whose rendered content can be empty.
				for _, w := range f.Expected {
					first, last := false, false
					for _, it := range items {
						first = first || it.Marker.Start() == w.Range[0]
						last = last || it.Marker.End() == w.Range[1]
					}
					if !first || !last {
						t.Fatal("actual marker premise", items, w)
					}
				}
				if (f.Name == "residual code" || f.Name == "two tabs code" || f.Name == "payload" || f.Name == "nonlexical") && len(items) != 1 {
					t.Fatal("one item premise", items)
				}
				if (f.Name == "raw protected" || f.Name == "comment protected" || f.Name == "fence" || f.Name == "indented" || strings.HasPrefix(f.Name, "thematic")) && len(items) != 0 {
					t.Fatal("protected ownership premise", items)
				}
				got, e := p.Document.ListRepetitions(context.Background(), b, NewLineIndex(source), options)
				if e != nil || len(got) != len(f.Expected) {
					t.Fatal(got, e, f.Expected)
				}
				for i, w := range f.Expected {
					if got[i].Start() != w.Range[0] || got[i].End() != w.Range[1] || got[i].SourceToken() != source.Token() {
						t.Fatal(got, w)
					}
				}
				if len(got) > 0 {
					want := append([]Span(nil), got...)
					got[0] = Span{}
					again, e := p.Document.ListRepetitions(context.Background(), b, NewLineIndex(source), options)
					if e != nil || !reflect.DeepEqual(again, want) {
						t.Fatal(again, e)
					}
				}
			})
		}
	}
}

// Calibrate only the unchanged ListItems cost. Projection costs follow its
// explicit reservations: 3*n+2*r+v nodes, n*(lookupBits+6)+3*r+v+gap+2 work.
// A one-unit deficit exposes removal of any positive producer charge directly,
// including clean state, lookup, gap, run records and output capacity/records.
func TestListRepetitionsProjectionOriginalReservations(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		n, r, v, gap int64
	}{
		{"empty", "", 0, 0, 0, 0}, {"clean state", "- one\n- two", 2, 2, 0, 0}, {"gap and maximal run", "- - - Text", 3, 1, 1, 2}, {"many outputs", "- - a\n\n+ + b\n\n* * c\n\n- - d", 8, 4, 4, 4},
	} {
		for _, kind := range []string{"work", "nodes"} {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				d, b, index := sectionParse(t, tc.source)
				w, n := b.work, b.nodes
				items, e := d.ListItems(context.Background(), b, index, ParseOptions{})
				if e != nil || int64(len(items)) != tc.n {
					t.Fatal(items, e)
				}
				inventoryWork, inventoryNodes := b.work-w, b.nodes-n
				d, b, index = sectionParse(t, tc.source)
				got, e := d.ListRepetitions(context.Background(), b, index, ParseOptions{})
				if e != nil || int64(len(got)) != tc.v {
					t.Fatal("actual membership", got, e)
				}
				d, b, index = sectionParse(t, tc.source)
				if kind == "work" {
					projection := tc.n*(int64(bits.Len(uint(len(index.lines))))) + 6*tc.n + 3*tc.r + tc.v + tc.gap + 2
					b.limits.WorkUnits = b.work + inventoryWork + projection - 1
				} else {
					b.limits.TotalNodes = b.nodes + inventoryNodes + 3*tc.n + 2*tc.r + tc.v - 1
					b.limits.DocumentNodes = b.limits.TotalNodes
				}
				for range 2 {
					got, e = d.ListRepetitions(context.Background(), b, index, ParseOptions{})
					var limit *LimitExceeded
					if !errors.As(e, &limit) || got != nil {
						t.Fatal("producer must withhold", got, e)
					}
				}
			})
		}
	}
}

func TestListRepetitionsDifferentLineBeforeGap(t *testing.T) {
	source := "- " + strings.Repeat("a", 20000) + "\n  - next"
	d, b, index := sectionParse(t, source)
	w := b.work
	_, e := d.ListItems(context.Background(), b, index, ParseOptions{})
	if e != nil {
		t.Fatal(e)
	}
	inventory := b.work - w
	d, b, index = sectionParse(t, source)
	b.limits.WorkUnits = b.work + inventory + 100
	got, e := d.ListRepetitions(context.Background(), b, index, ParseOptions{})
	if e != nil || len(got) != 0 {
		t.Fatal("cross-line gap scanned", got, e)
	}
}

// Original fixed counts: ListItems (3/21/31 checkpoints) plus projection
// (3/11/16). Inventory counts include entry/final, traversal, item markers,
// growth and records. Projection counts are 3+2*n+2*gapScans+2*runs+outputs.
// The facade adds entry and publication checks separately.
func TestListRepetitionsProjectionFixedFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		source string
		final  int
	}{{"", 6}, {"- one\n- two", 32}, {"- - - Text", 47}} {
		d, b, index := sectionParse(t, tc.source)
		caller := &headingConstructStop{Context: context.Background(), stop: tc.final}
		got, e := d.ListRepetitions(caller, b, index, ParseOptions{})
		if !errors.Is(e, context.Canceled) || got != nil || caller.calls != tc.final {
			t.Fatal("final inventory escaped", got, e, caller.calls, tc.final)
		}
	}
}

func TestListRepetitionsDeepAndWide(t *testing.T) {
	for _, tc := range []struct {
		source     string
		count, end int
	}{{strings.Repeat("- ", 40) + "Text", 1, 79}, {strings.Repeat("- one\n", 256), 0, 0}} {
		d, b, index := sectionParse(t, tc.source)
		got, e := d.ListRepetitions(context.Background(), b, index, ParseOptions{})
		if e != nil || len(got) != tc.count {
			t.Fatal(got, e)
		}
		if len(got) > 0 && (got[0].Start() != 0 || got[0].End() != tc.end) {
			t.Fatal(got)
		}
	}
}
