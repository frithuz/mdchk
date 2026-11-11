package parser

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBlockquoteSpacesRejectsForeignAuthority(t *testing.T) {
	d, b, index := sectionParse(t, ">  a\n")
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
		{constructed, b, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}}, {d, b, NewLineIndex(NewSource([]byte(">  a\n"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}},
	} {
		if got, e := tc.d.BlockquoteSpaces(context.Background(), tc.b, tc.index, tc.options); e == nil || got != nil {
			t.Fatal(got, e)
		}
	}
	//nolint:staticcheck // Missing caller authority is deliberately rejected.
	if got, e := d.BlockquoteSpaces(nil, b, index, ParseOptions{}); e == nil || got != nil {
		t.Fatal(got, e)
	}
	d.parsedComplete = false
	if got, e := d.BlockquoteSpaces(context.Background(), b, index, ParseOptions{}); e == nil || got != nil {
		t.Fatal(got, e)
	}
}

func TestBlockquoteSpacesDirectQuota(t *testing.T) {
	for _, source := range []string{"", "clean\n", ">  a\n>  a\n>  a\n>  a\n>  a\n", "<!-- protected --> \n", strings.Repeat(">  a\n", 256)} {
		for _, kind := range []string{"work", "nodes"} {
			d, b, index := sectionParse(t, source)
			if kind == "work" {
				b.limits.WorkUnits = b.work + 1
			} else {
				b.limits.TotalNodes = b.nodes
				b.limits.DocumentNodes = b.limits.TotalNodes
			}
			for range 2 {
				got, e := d.BlockquoteSpaces(context.Background(), b, index, ParseOptions{})
				var limit *LimitExceeded
				// Direct error and atomic-output oracle precedes any accounting assertions.
				if !errors.As(e, &limit) || got != nil {
					t.Fatal("direct quota omitted", kind, source, got, e)
				}
			}
		}
	}
}

func TestBlockquoteSpacesDetachedAndConcurrent(t *testing.T) {
	d, b, index := sectionParse(t, ">  a\n>  a\n>  a\n>  a\n>  a\n")
	got, e := d.BlockquoteSpaces(context.Background(), b, index, ParseOptions{})
	if e != nil || len(got) != 5 {
		t.Fatal(got, e)
	}
	first := got[0]
	got[0] = Span{}
	again, e := d.BlockquoteSpaces(context.Background(), b, index, ParseOptions{})
	if e != nil || again[0] != first {
		t.Fatal("aliased facts", again, e)
	}
	for range 4 {
		t.Run("independent", func(t *testing.T) {
			t.Parallel()
			for range 8 {
				d, b, index := sectionParse(t, ">  a\n")
				got, e := d.BlockquoteSpaces(context.Background(), b, index, ParseOptions{})
				if e != nil || len(got) != 1 {
					t.Fatal(got, e)
				}
			}
		})
	}
}

// Static reservations were enumerated before execution; context counts include
// the final publication check. These constants never come from observed quotas.
func TestBlockquoteSpacesExactReservationsAndFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		facts        int
		work, nodes  int64
		calls        int
	}{
		{"empty", "", 0, 4, 3, 5}, {"clean", "a\n", 0, 26, 6, 22}, {"protected", "```\nx\n```", 0, 17, 7, 15}, {"growth", ">  a\n>  a\n>  a\n>  a\n>  a\n", 5, 246, 69, 145},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, kind := range []string{"work", "nodes"} {
				for _, delta := range []int64{-1, 0} {
					d, b, index := sectionParse(t, tc.source)
					if kind == "work" {
						b.limits.WorkUnits = b.work + tc.work + delta
					} else {
						b.limits.TotalNodes = b.nodes + tc.nodes + delta
						b.limits.DocumentNodes = b.limits.TotalNodes
					}
					got, e := d.BlockquoteSpaces(context.Background(), b, index, ParseOptions{})
					if delta < 0 {
						var limit *LimitExceeded
						if !errors.As(e, &limit) || got != nil {
							t.Fatal("direct Err+nil", kind, got, e)
						}
					} else if e != nil || len(got) != tc.facts {
						t.Fatal("exact quota", kind, got, e)
					}
				}
			}
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				d, b, index := sectionParse(t, tc.source)
				stop := &widthProjectionStop{Context: context.Background(), stop: tc.calls, reason: reason}
				got, e := d.BlockquoteSpaces(stop, b, index, ParseOptions{})
				if !errors.Is(e, reason) || got != nil {
					t.Fatal("final Err+nil", got, e)
				}
				if stop.calls != tc.calls {
					t.Fatal(stop.calls, tc.calls)
				}
			}
			d, b, index := sectionParse(t, tc.source)
			w, n := b.work, b.nodes
			counter := &widthProjectionStop{Context: context.Background(), stop: 1000000}
			got, e := d.BlockquoteSpaces(counter, b, index, ParseOptions{})
			if e != nil || len(got) != tc.facts || b.work-w != tc.work || b.nodes-n != tc.nodes || counter.calls != tc.calls {
				t.Fatal("static equation", got, e, b.work-w, b.nodes-n, counter.calls, tc)
			}
		})
	}
}

// The producer receives the compound operation context from its consumer.
// Independently stop either constituent at its final publication observation.
type blockquoteOperation struct {
	context.Context
	origin context.Context
}

func (c blockquoteOperation) Err() error { return errors.Join(c.Context.Err(), c.origin.Err()) }
func TestBlockquoteSpacesProducerCallerAndOriginFinal(t *testing.T) {
	for _, tc := range []struct {
		source string
		calls  int
	}{{"", 5}, {"a\n", 22}, {"```\nx\n```", 15}, {">  a\n>  a\n>  a\n>  a\n>  a\n", 145}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			for _, origin := range []bool{false, true} {
				d, b, index := sectionParse(t, tc.source)
				stop := &widthProjectionStop{Context: context.Background(), stop: tc.calls, reason: reason}
				caller, owner := context.Background(), context.Background()
				if origin {
					owner = stop
				} else {
					caller = stop
				}
				got, e := d.BlockquoteSpaces(blockquoteOperation{caller, owner}, b, index, ParseOptions{})
				if !errors.Is(e, reason) || got != nil {
					t.Fatal("direct producer Err+nil", got, e)
				}
				if stop.calls != tc.calls {
					t.Fatal(stop.calls, tc.calls)
				}
			}
		}
	}
}
