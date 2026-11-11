package parser

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/synctest"
	"time"
)

func TestThematicSpacingOwnershipAndFloors(t *testing.T) {
	for _, tc := range []struct {
		source string
		points []int
	}{
		{"- one\n\n***", nil},
		{"- - one\n\n***", nil},
		{"-\n***", []int{2}},
		{"- [r]: /x\n***", []int{10}},
		{"- > quote\n  >\n***", []int{14}},
		{"- ```\n  \n***", []int{9}},
		{"- <script>\n  \n***", []int{14}},
		{"- ```\n  \n  ```\n***", []int{15}},
		{"- <script>\n  \n  </script>\n***", []int{26}},
		{"> Text\n>   ***\n> More", []int{9, 14}},
		{"> [r]: /x\n>\n> ***", nil},
		{"***\n[r]: /x\n\nMore", []int{3}},
	} {
		for _, gfm := range []bool{false, true} {
			t.Run(tc.source+map[bool]string{false: "/commonmark", true: "/gfm"}[gfm], func(t *testing.T) {
				source := NewSource([]byte(tc.source))
				b, _ := NewOperationBudget(DefaultLimits())
				options := ParseOptions{GFM: gfm}
				p := ParseWithOptions(context.Background(), source, b, options)
				if p.Status != Complete {
					t.Fatal(p)
				}
				got, err := p.Document.ThematicSpacing(context.Background(), b, NewLineIndex(source), options)
				if err != nil {
					t.Fatal(err)
				}
				var points []int
				for _, f := range got {
					if !f.Point.Present() || f.Point.Start() != f.Point.End() || f.Point.SourceToken() != source.Token() {
						t.Fatal(f)
					}
					points = append(points, f.Point.Start())
				}
				if !reflect.DeepEqual(points, tc.points) {
					t.Fatal(points, tc.points)
				}
			})
		}
	}
}

func TestThematicSpacingRejectsDetachedAndMalformedAuthority(t *testing.T) {
	d, b, index := sectionParse(t, "Text\n***\nMore")
	constructed, err := NewDocument(d.Root(), d.References())
	if err != nil {
		t.Fatal(err)
	}
	foreign, _ := NewOperationBudget(DefaultLimits())
	for _, tc := range []struct {
		d       *Document
		b       *OperationBudget
		index   LineIndex
		options ParseOptions
	}{
		{nil, b, index, ParseOptions{}}, {constructed, b, index, ParseOptions{}}, {d, nil, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}},
		{d, b, NewLineIndex(NewSource([]byte("Text\n***\nMore"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}},
	} {
		if got, e := tc.d.ThematicSpacing(context.Background(), tc.b, tc.index, tc.options); !errors.Is(e, ErrModel) || got != nil {
			t.Fatal(got, e)
		}
	}
	// A public incomplete/foreign profile cannot acquire projection authority.
	for _, invalidate := range []func(){func() { d.parsedComplete = false }, func() { d.parsedProfile = parserProfile(255) }} {
		d, b, index = sectionParse(t, "Text\n***\nMore")
		invalidate()
		if got, e := d.ThematicSpacing(context.Background(), b, index, ParseOptions{}); !errors.Is(e, ErrModel) || got != nil {
			t.Fatal(got, e)
		}
	}
	d, b, index = sectionParse(t, "[a]: /a\n***\n[b]: /b")
	d.references.Definitions[0], d.references.Definitions[1] = d.references.Definitions[1], d.references.Definitions[0]
	if got, e := d.ThematicSpacing(context.Background(), b, index, ParseOptions{}); !errors.Is(e, ErrModel) || got != nil {
		t.Fatal(got, e)
	}
	//nolint:staticcheck // Deliberate absent caller.
	if got, e := d.ThematicSpacing(nil, b, index, ParseOptions{}); !errors.Is(e, ErrModel) || got != nil {
		t.Fatal(got, e)
	}
}

// These totals are derived from the held source, before any producer execution.
// See Working/issue-77-static-cost-ledger.md for phase equations and checkpoints.
var thematicSpacingCosts = []struct {
	name, source string
	facts        int
	work, nodes  int64
	final        int
}{
	{"empty", "", 0, 16, 4, 15},
	{"clean", "clean", 0, 53, 15, 40},
	{"edge", "***", 0, 39, 11, 32},
	{"protected", "```\nx\n```", 0, 45, 11, 34},
	{"violation", "Text\n***\nMore", 2, 128, 38, 90},
	{"definitions", "[r]: /x\n***", 1, 61, 14, 43},
	{"rollback", "> Text\n> ***\n> More", 2, 211, 53, 119},
	{"growth", "***\n***\n***\n***", 6, 159, 62, 103},
}

func TestThematicSpacingStaticReservations(t *testing.T) {
	for _, tc := range thematicSpacingCosts {
		for _, kind := range []string{"work", "nodes"} {
			for _, delta := range []int64{-1, 0} {
				t.Run(tc.name+"/"+kind+map[int64]string{-1: "/short", 0: "/exact"}[delta], func(t *testing.T) {
					d, b, index := sectionParse(t, tc.source)
					if kind == "work" {
						b.limits.WorkUnits = b.work + tc.work + delta
					} else {
						b.limits.TotalNodes = b.nodes + tc.nodes + delta
						b.limits.DocumentNodes = b.limits.TotalNodes
					}
					got, e := d.ThematicSpacing(context.Background(), b, index, ParseOptions{})
					if delta == 0 {
						if e != nil || len(got) != tc.facts {
							t.Fatal(got, e)
						}
					} else {
						for range 2 {
							var limit *LimitExceeded
							if !errors.As(e, &limit) || got != nil {
								t.Fatal("direct producer withholding", got, e)
							}
							got, e = d.ThematicSpacing(context.Background(), b, index, ParseOptions{})
						}
					}
				})
			}
		}
	}
}

func TestThematicSpacingFinalPublication(t *testing.T) {
	for _, tc := range thematicSpacingCosts {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+reason.Error(), func(t *testing.T) {
				d, b, index := sectionParse(t, tc.source)
				caller := &thematicSpacingStop{Context: context.Background(), stop: tc.final, reason: reason}
				got, e := d.ThematicSpacing(caller, b, index, ParseOptions{})
				if !errors.Is(e, reason) || got != nil || caller.calls != tc.final {
					t.Fatal(got, e, caller.calls, tc.final)
				}
			})
		}
	}
}

type thematicSpacingStop struct {
	context.Context
	calls, stop int
	reason      error
}

func (c *thematicSpacingStop) Err() error {
	c.calls++
	if c.calls >= c.stop {
		return c.reason
	}
	return nil
}

func TestThematicSpacingOriginAndStickyControls(t *testing.T) {
	for _, tc := range thematicSpacingCosts {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+"/origin/"+reason.Error(), func(t *testing.T) {
				origin := &thematicSpacingStop{Context: context.Background(), stop: 1 << 30, reason: reason}
				source := NewSource([]byte(tc.source))
				b, _ := NewOperationBudget(DefaultLimits())
				p := ParseWithOptions(origin, source, b, ParseOptions{})
				if p.Status != Complete {
					t.Fatal(p)
				}
				origin.stop = origin.calls + tc.final
				d, index := p.Document, NewLineIndex(source)
				got, e := d.ThematicSpacing(blockquoteOperation{Context: context.Background(), origin: origin}, b, index, ParseOptions{})
				if !errors.Is(e, reason) || got != nil || origin.calls != origin.stop {
					t.Fatal(got, e, origin.calls)
				}
			})
		}
	}
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				d, b, index := sectionParse(t, "Text\n***\nMore")
				switch kind {
				case "work":
					_ = b.ChargeWork(b.Limits().WorkUnits)
				case "nodes":
					_ = b.ChargeNodes(b.Limits().TotalNodes)
				case "elapsed":
					time.Sleep(61 * time.Second)
				}
				for range 2 {
					got, e := d.ThematicSpacing(context.Background(), b, index, ParseOptions{})
					var stop *LimitExceeded
					if !errors.As(e, &stop) || got != nil {
						t.Fatal(got, e)
					}
				}
			})
		})
	}
}
