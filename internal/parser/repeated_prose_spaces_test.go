package parser

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestRepeatedProseSpacesRejectsForeignAuthority(t *testing.T) {
	d, b, index := sectionParse(t, "a  b")
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
		maximum int
	}{
		{nil, b, index, ParseOptions{}, 1}, {d, nil, index, ParseOptions{}, 1}, {d, b, LineIndex{}, ParseOptions{}, 1},
		{constructed, b, index, ParseOptions{}, 1}, {d, foreign, index, ParseOptions{}, 1}, {d, b, NewLineIndex(NewSource([]byte("a  b"))), ParseOptions{}, 1}, {d, b, index, ParseOptions{GFM: true}, 1}, {d, b, index, ParseOptions{}, 0},
	} {
		if got, e := tc.d.RepeatedProseSpaces(context.Background(), tc.b, tc.index, tc.options, tc.maximum); !errors.Is(e, ErrModel) || got != nil {
			t.Fatal(got, e)
		}
	}
	//nolint:staticcheck // Missing caller authority is deliberately rejected.
	if got, e := d.RepeatedProseSpaces(nil, b, index, ParseOptions{}, 1); !errors.Is(e, ErrModel) || got != nil {
		t.Fatal(got, e)
	}
	d.parsedComplete = false
	if got, e := d.RepeatedProseSpaces(context.Background(), b, index, ParseOptions{}, 1); !errors.Is(e, ErrModel) || got != nil {
		t.Fatal(got, e)
	}
}

// These equations were enumerated before execution in the engineering static
// accounting record; no observed counter supplies an expected reservation.
func TestRepeatedProseSpacesExactReservationsAndPublication(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		facts        int
		work, nodes  int64
		calls        int
	}{
		{"empty", "", 0, 7, 4, 8}, {"clean", "a", 0, 36, 14, 23}, {"protected", "```\nx\n```", 0, 14, 8, 14},
		{"violation", "a  b", 1, 78, 16, 31}, {"growth", "a  b  c  d  e  f", 5, 252, 34, 62}, {"nested", "**a**  **b**", 1, 101, 32, 50},
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
					got, e := d.RepeatedProseSpaces(context.Background(), b, index, ParseOptions{}, 1)
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
				for _, origin := range []bool{false, true} {
					d, b, index := sectionParse(t, tc.source)
					stop := &widthProjectionStop{Context: context.Background(), stop: tc.calls, reason: reason}
					caller, owner := context.Background(), context.Background()
					if origin {
						owner = stop
					} else {
						caller = stop
					}
					got, e := d.RepeatedProseSpaces(blockquoteOperation{caller, owner}, b, index, ParseOptions{}, 1)
					if !errors.Is(e, reason) || got != nil {
						t.Fatal("final Err+nil", got, e)
					}
					if stop.calls != tc.calls {
						t.Fatal(stop.calls, tc.calls)
					}
				}
			}
			d, b, index := sectionParse(t, tc.source)
			w, n := b.work, b.nodes
			counter := &widthProjectionStop{Context: context.Background(), stop: 1000000}
			got, e := d.RepeatedProseSpaces(counter, b, index, ParseOptions{}, 1)
			if e != nil || len(got) != tc.facts || b.work-w != tc.work || b.nodes-n != tc.nodes || counter.calls != tc.calls {
				t.Fatal("static equation", got, e, b.work-w, b.nodes-n, counter.calls, tc)
			}
		})
	}
}
func TestRepeatedProseSpacesStickyQuota(t *testing.T) {
	for _, source := range []string{"", "clean", "```\nx\n```", strings.Repeat("one  two ", 256)} {
		for _, kind := range []string{"work", "nodes"} {
			d, b, index := sectionParse(t, source)
			if kind == "work" {
				b.limits.WorkUnits = b.work + 1
			} else {
				b.limits.TotalNodes = b.nodes
				b.limits.DocumentNodes = b.nodes
			}
			for range 2 {
				got, e := d.RepeatedProseSpaces(context.Background(), b, index, ParseOptions{}, 1)
				var limit *LimitExceeded
				if !errors.As(e, &limit) || got != nil {
					t.Fatal(got, e)
				}
			}
		}
	}
}
func TestRepeatedProseSpacesDetachedConcurrent(t *testing.T) {
	d, b, index := sectionParse(t, "a  b  c  d  e  f")
	got, e := d.RepeatedProseSpaces(context.Background(), b, index, ParseOptions{}, 1)
	if e != nil || len(got) != 5 {
		t.Fatal(got, e)
	}
	first := got[0]
	got[0] = Span{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 8 {
				again, e := d.RepeatedProseSpaces(context.Background(), b, index, ParseOptions{}, 1)
				if e != nil || len(again) != 5 || again[0] != first {
					t.Error(again, e)
				}
				again[0] = Span{}
			}
		})
	}
	wg.Wait()
}
