package parser

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestParagraphIndentationRejectsForeignAuthority(t *testing.T) {
	d, b, index := sectionParse(t, "a\n  b")
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
		{d, b, NewLineIndex(NewSource([]byte("a\n  b"))), ParseOptions{}},
		{d, b, index, ParseOptions{GFM: true}},
	} {
		if got, e := tc.d.ParagraphIndentation(context.Background(), tc.b, tc.index, tc.options); !errors.Is(e, ErrModel) || got != nil {
			t.Fatal(got, e)
		}
	}
	//nolint:staticcheck // Missing caller authority is deliberately rejected.
	if got, e := d.ParagraphIndentation(nil, b, index, ParseOptions{}); !errors.Is(e, ErrModel) || got != nil {
		t.Fatal(got, e)
	}
	d.parsedComplete = false
	if got, e := d.ParagraphIndentation(context.Background(), b, index, ParseOptions{}); !errors.Is(e, ErrModel) || got != nil {
		t.Fatal(got, e)
	}
}

// These equations were enumerated before execution in the engineering static
// accounting record; no observed counter supplies an expected reservation.
func TestParagraphIndentationExactReservationsAndPublication(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		facts        int
		work, nodes  int64
		calls        int
	}{
		{"empty", "", 0, 6, 4, 7}, {"clean", "a", 0, 43, 15, 26}, {"protected", "```\nx\n```", 0, 18, 11, 15},
		{"violation", "a\n  b", 1, 90, 30, 48}, {"growth", "a\n  b\n  c\n  d\n  e\n  f", 5, 279, 84, 132},
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
					got, e := d.ParagraphIndentation(context.Background(), b, index, ParseOptions{})
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
					got, e := d.ParagraphIndentation(blockquoteOperation{caller, owner}, b, index, ParseOptions{})
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
			got, e := d.ParagraphIndentation(counter, b, index, ParseOptions{})
			if e != nil || len(got) != tc.facts || b.work-w != tc.work || b.nodes-n != tc.nodes || counter.calls != tc.calls {
				t.Fatal("static equation", got, e, b.work-w, b.nodes-n, counter.calls, tc)
			}
		})
	}
}
func TestParagraphIndentationStickyQuota(t *testing.T) {
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
				got, e := d.ParagraphIndentation(context.Background(), b, index, ParseOptions{})
				var limit *LimitExceeded
				if !errors.As(e, &limit) || got != nil {
					t.Fatal(got, e)
				}
			}
		}
	}
}
func TestParagraphIndentationDetachedConcurrent(t *testing.T) {
	d, b, index := sectionParse(t, "a\n  b\n  c\n  d\n  e\n  f")
	got, e := d.ParagraphIndentation(context.Background(), b, index, ParseOptions{})
	if e != nil || len(got) != 5 {
		t.Fatal(got, e)
	}
	first := got[0]
	got[0] = Span{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 8 {
				again, e := d.ParagraphIndentation(context.Background(), b, index, ParseOptions{})
				if e != nil || len(again) != 5 || again[0] != first {
					t.Error(again, e)
				}
				again[0] = Span{}
			}
		})
	}
	wg.Wait()
}

func TestParagraphIndentationOriginalElapsedAndIndependentOperations(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, source := range []string{"", "a", "```\nx\n```", "a\n  b"} {
			d, b, index := sectionParse(t, source)
			b.limits.OperationTime = time.Millisecond
			time.Sleep(2 * time.Millisecond)
			for range 2 {
				got, err := d.ParagraphIndentation(context.Background(), b, index, ParseOptions{})
				var limit *LimitExceeded
				if !errors.As(err, &limit) || got != nil {
					t.Fatal("direct elapsed Err+nil", got, err)
				}
				if limit.Name != "operation-time" {
					t.Fatal(limit)
				}
			}
		}
	})
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 8 {
				d, b, index := sectionParse(t, "a\n  b")
				got, err := d.ParagraphIndentation(context.Background(), b, index, ParseOptions{})
				if err != nil || len(got) != 1 || got[0].start != 2 || got[0].end != 4 {
					t.Error(got, err)
				}
			}
		})
	}
	wg.Wait()
}
