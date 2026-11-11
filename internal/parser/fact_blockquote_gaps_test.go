package parser

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBlockquoteGapsRejectsForeignAuthority(t *testing.T) {
	d, b, index := sectionParse(t, "> A\n\n> B\n")
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
		{constructed, b, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}}, {d, b, NewLineIndex(NewSource([]byte("> A\n\n> B\n"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}},
	} {
		if got, e := tc.d.BlockquoteGaps(context.Background(), tc.b, tc.index, tc.options); e == nil || got != nil {
			t.Fatal(got, e)
		}
	}
	//nolint:staticcheck // Missing caller authority is deliberately rejected.
	if got, e := d.BlockquoteGaps(nil, b, index, ParseOptions{}); e == nil || got != nil {
		t.Fatal(got, e)
	}
	d.parsedComplete = false
	if got, e := d.BlockquoteGaps(context.Background(), b, index, ParseOptions{}); e == nil || got != nil {
		t.Fatal(got, e)
	}
}

func TestBlockquoteGapsDirectQuota(t *testing.T) {
	for _, source := range []string{"", "clean\n", "> A\n\n> B\n> A\n\n> B\n> A\n\n> B\n> A\n\n> B\n> A\n\n> B\n", "<!-- protected --> \n", strings.Repeat("> A\n\n> B\n", 256)} {
		for _, kind := range []string{"work", "nodes"} {
			d, b, index := sectionParse(t, source)
			if kind == "work" {
				b.limits.WorkUnits = b.work + 1
			} else {
				b.limits.TotalNodes = b.nodes
				b.limits.DocumentNodes = b.limits.TotalNodes
			}
			for range 2 {
				got, e := d.BlockquoteGaps(context.Background(), b, index, ParseOptions{})
				var limit *LimitExceeded
				// Direct error and atomic-output oracle precedes any accounting assertions.
				if !errors.As(e, &limit) || got != nil {
					t.Fatal("direct quota omitted", kind, source, got, e)
				}
			}
		}
	}
}

func TestBlockquoteGapsDetachedAndConcurrent(t *testing.T) {
	d, b, index := sectionParse(t, "> A\n\n> B\n> A\n\n> B\n> A\n\n> B\n> A\n\n> B\n> A\n\n> B\n")
	got, e := d.BlockquoteGaps(context.Background(), b, index, ParseOptions{})
	if e != nil || len(got) != 5 {
		t.Fatal(got, e)
	}
	first := got[0]
	got[0] = Span{}
	again, e := d.BlockquoteGaps(context.Background(), b, index, ParseOptions{})
	if e != nil || again[0] != first {
		t.Fatal("aliased facts", again, e)
	}
	for range 4 {
		t.Run("independent", func(t *testing.T) {
			t.Parallel()
			for range 8 {
				d, b, index := sectionParse(t, "> A\n\n> B\n")
				got, e := d.BlockquoteGaps(context.Background(), b, index, ParseOptions{})
				if e != nil || len(got) != 1 {
					t.Fatal(got, e)
				}
			}
		})
	}
}

// Independent static equations are recorded before execution in the issue log.
func TestBlockquoteGapsStaticReservations(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		facts        int
		work, nodes  int64
	}{
		{"empty", "", 0, 6, 4}, {"clean", "a\n", 0, 14, 7}, {"protected", "```\nx\n```", 0, 36, 8},
		{"nested", "> > A\n>\t\n> > B", 1, 73, 21},
		{"growth", "> A\n\n> B\n\n> C\n\n> D\n\n> E\n\n> F", 5, 216, 73},
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
					got, e := d.BlockquoteGaps(context.Background(), b, index, ParseOptions{})
					if delta < 0 {
						var limit *LimitExceeded
						if !errors.As(e, &limit) || got != nil {
							t.Fatal("direct Err+nil", kind, got, e)
						}
					} else if e != nil || len(got) != tc.facts {
						t.Fatal("exact static quota", kind, got, e)
					}
				}
			}
			d, b, index := sectionParse(t, tc.source)
			w, n := b.work, b.nodes
			got, e := d.BlockquoteGaps(context.Background(), b, index, ParseOptions{})
			if e != nil || len(got) != tc.facts || b.work-w != tc.work || b.nodes-n != tc.nodes {
				t.Fatal("static equation", got, e, b.work-w, b.nodes-n, tc)
			}
		})
	}
}

// Both constituents are observed at the final publication boundary.
type gapOperation struct {
	context.Context
	origin context.Context
}

func (c gapOperation) Err() error { return errors.Join(c.Context.Err(), c.origin.Err()) }
func TestBlockquoteGapsCallerAndOriginFinal(t *testing.T) {
	for _, tc := range []struct {
		source string
		calls  int
	}{{"", 7}, {"a\n", 14}, {"```\nx\n```", 26}, {"> A\n\n> B\n\n> C\n\n> D\n\n> E\n\n> F", 128}} {
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
				got, e := d.BlockquoteGaps(gapOperation{caller, owner}, b, index, ParseOptions{})
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

func TestBlockquoteGapsOriginalElapsedWithholds(t *testing.T) {
	for _, source := range []string{"", "a\n", "```\nx\n```", "> A\n\n> B"} {
		d, b, index := sectionParse(t, source)
		b.started = time.Now().Add(-2 * b.limits.OperationTime)
		for range 2 {
			got, e := d.BlockquoteGaps(context.Background(), b, index, ParseOptions{})
			var limit *LimitExceeded
			if !errors.As(e, &limit) || got != nil || limit.Name != "operation-time" {
				t.Fatal("direct elapsed Err+nil", got, e)
			}
		}
	}
}
