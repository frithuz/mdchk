package parser

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"testing/synctest"
	"time"
)

func md092Parse(t *testing.T, source string) (*Document, *OperationBudget, LineIndex) {
	t.Helper()
	s := NewSource([]byte(source))
	b, _ := NewOperationBudget(DefaultLimits())
	p := ParseWithOptions(context.Background(), s, b, ParseOptions{})
	if p.Status != Complete {
		t.Fatal(p)
	}
	return p.Document, b, NewLineIndex(s)
}

func TestConflictMarkersOriginalPrivateAuthority(t *testing.T) {
	d, b, index := md092Parse(t, "<<<<<<<")
	public, _ := NewDocument(d.Root(), d.References())
	foreign, _ := NewOperationBudget(DefaultLimits())
	for _, tc := range []struct {
		d *Document
		b *OperationBudget
		i LineIndex
		o ParseOptions
	}{
		{nil, b, index, ParseOptions{}}, {public, b, index, ParseOptions{}}, {d, nil, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}}, {d, b, NewLineIndex(NewSource([]byte("<<<<<<<"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}},
	} {
		got, err := tc.d.ConflictMarkers(context.Background(), tc.b, tc.i, tc.o)
		if !errors.Is(err, ErrModel) || got.Markers != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Deliberately absent caller.
	if got, err := d.ConflictMarkers(nil, b, index, ParseOptions{}); !errors.Is(err, ErrModel) || got.Markers != nil {
		t.Fatal(got, err)
	}
	for _, profile := range []parserProfile{tablesOnlyProfile, tasksOnlyProfile, strikeOnlyProfile, bareOnlyProfile, filterOnlyProfile} {
		for _, fullFlags := range []bool{false, true} {
			t.Run(fmt.Sprintf("profile-%d/full-flags-%v", profile, fullFlags), func(t *testing.T) {
				s := NewSource([]byte("<<<<<<<"))
				budget, _ := NewOperationBudget(DefaultLimits())
				p := parseWithProfile(context.Background(), s, budget, profile)
				if p.Status != Complete {
					t.Fatal(p)
				}
				if fullFlags {
					p.Document.parsedOptions.GFM = true
				}
				got, err := p.Document.ConflictMarkers(context.Background(), budget, NewLineIndex(s), p.Document.parsedOptions)
				if !errors.Is(err, ErrModel) || got.Markers != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
	d.parsedComplete = false
	if got, err := d.ConflictMarkers(context.Background(), b, index, ParseOptions{}); !errors.Is(err, ErrModel) || got.Markers != nil {
		t.Fatal(got, err)
	}
	source := NewSource([]byte("<<<<<<<"))
	budget, _ := NewOperationBudget(DefaultLimits())
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	partial := ParseWithOptions(cancelled, source, budget, ParseOptions{})
	if partial.Status == Complete {
		t.Fatal("genuine interrupted parse completed")
	}
	if got, err := partial.Document.ConflictMarkers(context.Background(), budget, NewLineIndex(source), ParseOptions{}); !errors.Is(err, ErrModel) || got.Markers != nil {
		t.Fatal(got, err)
	}
}

// Empty original parsing costs 25W/2N (CM), 30W/2N (GFM). The frozen
// independent ledger and root comparison establish residual projection33W/2N.
func TestConflictMarkersStaticEmptyOriginalReservations(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, nodes := range []bool{false, true} {
			for _, delta := range []int64{-1, 0} {
				t.Run(fmt.Sprintf("gfm-%v/nodes-%v/delta-%d", gfm, nodes, delta), func(t *testing.T) {
					s := NewSource(nil)
					b, _ := NewOperationBudget(DefaultLimits())
					o := ParseOptions{GFM: gfm}
					p := ParseWithOptions(context.Background(), s, b, o)
					if p.Status != Complete {
						t.Fatal(p)
					}
					charge, limit, setup, residual := b.ChargeWork, b.Limits().WorkUnits, int64(25), int64(33)
					if gfm {
						setup = 30
					}
					if nodes {
						charge, limit, setup, residual = b.ChargeNodes, b.Limits().TotalNodes, 2, 2
					}
					if err := charge(limit - setup - residual - delta); err != nil {
						t.Fatal(err)
					}
					got, err := p.Document.ConflictMarkers(context.Background(), b, NewLineIndex(s), o)
					if delta == 0 {
						if err != nil || got.Availability != Available || len(got.Markers) != 0 {
							t.Fatal(got, err)
						}
					} else {
						for range 2 {
							var stop *LimitExceeded
							if !errors.As(err, &stop) || got.Markers != nil {
								t.Fatal(got, err)
							}
							got, err = p.Document.ConflictMarkers(context.Background(), b, NewLineIndex(s), o)
						}
					}
				})
			}
		}
	}
}

func TestConflictMarkersStickyControlsAndDetachedFacts(t *testing.T) {
	for _, source := range []string{"", "clean", "```\n<<<<<<<\n```", "<<<<<<<", "<<<<<<< before\n\n<div>\n<x title=\""} {
		for _, kind := range []string{"work", "nodes", "elapsed", "cancelled", "deadline"} {
			t.Run(source+"/"+kind, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					d, b, index := md092Parse(t, source)
					ctx := context.Background()
					switch kind {
					case "work":
						_ = b.ChargeWork(b.Limits().WorkUnits)
					case "nodes":
						_ = b.ChargeNodes(b.Limits().TotalNodes)
					case "elapsed":
						time.Sleep(61 * time.Second)
					case "cancelled":
						c, cancel := context.WithCancel(ctx)
						cancel()
						ctx = c
					case "deadline":
						c, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
						defer cancel()
						ctx = c
					}
					for range 2 {
						got, err := d.ConflictMarkers(ctx, b, index, ParseOptions{})
						if err == nil || got.Markers != nil {
							t.Fatal(got, err)
						}
					}
				})
			})
		}
	}
	d, b, index := md092Parse(t, "<<<<<<<\n=======\n>>>>>>>\n")
	first, err := d.ConflictMarkers(context.Background(), b, index, ParseOptions{})
	if err != nil || len(first.Markers) != 3 {
		t.Fatal(first, err)
	}
	saved := append([]Span(nil), first.Markers...)
	first.Markers[0] = Span{}
	next, err := d.ConflictMarkers(context.Background(), b, index, ParseOptions{})
	if err != nil || !reflect.DeepEqual(next.Markers, saved) {
		t.Fatal(next, err)
	}
}

// These are lexical-helper quota tests, not production parse authority. The
// explicitly built owner supplies only this private helper's mapped spelling.
// Public fabricated Documents are separately rejected above; no bridge exports
// this owner, context or helper to production callers.
func TestConflictMarkersHTMLStaticHelperReservations(t *testing.T) {
	for _, row := range []struct {
		text      string
		form      int
		incoming  string
		w, n      int64
		available bool
	}{
		{"<div>\n", 6, "", 335, 48, true},
		{"<script>\n", 1, "", 278, 68, true},
		{"<script>\n</style>\n", 1, "", 514, 132, true},
		{"<div>\n</script>\n", 6, "script", 426, 120, true},
		{"<div>\ntext\n", 6, "script", 295, 83, true},
		{"<div>\n</style>\n", 6, "script", 403, 111, true},
		{"<div><x a=\"", 6, "", 487, 84, false},
		{"<div><!--", 6, "", 427, 69, true},
	} {
		for _, nodes := range []bool{false, true} {
			for _, delta := range []int64{-1, 0} {
				t.Run(fmt.Sprintf("%q/nodes-%v/delta-%d", row.text, nodes, delta), func(t *testing.T) {
					s := NewSource([]byte(row.text))
					span, _ := s.Span(0, len(row.text))
					n, err := NewNode(NodeSpec{Kind: HTMLBlock, Envelope: span, Content: []Segment{{Source: span, Transformation: Exact, Semantic: Some(row.text)}}, Properties: Properties{HTML: &HTMLProperties{BlockForm: Some(row.form)}}})
					if err != nil {
						t.Fatal(err)
					}
					b, _ := NewOperationBudget(DefaultLimits())
					charge, limit, phase := b.ChargeWork, b.Limits().WorkUnits, row.w
					if nodes {
						charge, limit, phase = b.ChargeNodes, b.Limits().TotalNodes, row.n
					}
					if err := charge(limit - phase - delta); err != nil {
						t.Fatal(err)
					}
					work := &parserContext{Context: context.Background(), budget: &parseBudget{operation: b}}
					state, closed, available, err := conflictHTMLRaw(work, s, n, conflictRawState{name: row.incoming})
					if delta == 0 {
						if err != nil || available != row.available {
							t.Fatal(state, closed, available, err)
						}
					} else {
						var stop *LimitExceeded
						if !errors.As(err, &stop) || available || closed != nil {
							t.Fatal(state, closed, available, err)
						}
						_, again, a, e := conflictHTMLRaw(work, s, n, conflictRawState{name: row.incoming})
						if e == nil || a || again != nil {
							t.Fatal(again, a, e)
						}
					}
				})
			}
		}
	}
}
