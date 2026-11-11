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

func TestCJKSpacingRejectsForeignAuthority(t *testing.T) {
	d, b, index := sectionParse(t, "中A")
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
		{d, b, NewLineIndex(NewSource([]byte("中A"))), ParseOptions{}},
		{d, b, index, ParseOptions{GFM: true}},
	} {
		if got, e := tc.d.CJKSpacing(context.Background(), tc.b, tc.index, tc.options); !errors.Is(e, ErrModel) || got != nil {
			t.Fatal(got, e)
		}
	}
	//nolint:staticcheck // Missing caller authority is deliberately rejected.
	if got, e := d.CJKSpacing(nil, b, index, ParseOptions{}); !errors.Is(e, ErrModel) || got != nil {
		t.Fatal(got, e)
	}
	d.parsedComplete = false
	if got, e := d.CJKSpacing(context.Background(), b, index, ParseOptions{}); !errors.Is(e, ErrModel) || got != nil {
		t.Fatal(got, e)
	}
}

func TestCJKSpacingStickyQuota(t *testing.T) {
	for _, source := range []string{"", "clean", "```\nx\n```", strings.Repeat("中A ", 256)} {
		for _, kind := range []string{"work", "nodes"} {
			d, b, index := sectionParse(t, source)
			if kind == "work" {
				b.limits.WorkUnits = b.work + 1
			} else {
				b.limits.TotalNodes = b.nodes
				b.limits.DocumentNodes = b.nodes
			}
			for range 2 {
				got, e := d.CJKSpacing(context.Background(), b, index, ParseOptions{})
				var limit *LimitExceeded
				if !errors.As(e, &limit) || got != nil {
					t.Fatal(got, e)
				}
			}
		}
	}
}
func TestCJKSpacingDetachedConcurrent(t *testing.T) {
	d, b, index := sectionParse(t, "中A中A中A")
	got, e := d.CJKSpacing(context.Background(), b, index, ParseOptions{})
	if e != nil || len(got) != 5 {
		t.Fatal(got, e)
	}
	first := got[0]
	got[0] = Span{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 8 {
				again, e := d.CJKSpacing(context.Background(), b, index, ParseOptions{})
				if e != nil || len(again) != 5 || again[0] != first {
					t.Error(again, e)
				}
				again[0] = Span{}
			}
		})
	}
	wg.Wait()
}

func TestCJKSpacingOriginalElapsedAndIndependentOperations(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, source := range []string{"", "a", "```\nx\n```", "中A"} {
			d, b, index := sectionParse(t, source)
			b.limits.OperationTime = time.Millisecond
			time.Sleep(2 * time.Millisecond)
			for range 2 {
				got, err := d.CJKSpacing(context.Background(), b, index, ParseOptions{})
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
				d, b, index := sectionParse(t, "中A")
				got, err := d.CJKSpacing(context.Background(), b, index, ParseOptions{})
				if err != nil || len(got) != 1 || got[0].start != 3 || got[0].end != 3 {
					t.Error(got, err)
				}
			}
		})
	}
	wg.Wait()
}

// Literal reservations were written before boundary execution in
// Working/issue-70-engineer-static-accounting.md, from charged source sites.
func TestCJKSpacingExactReservationsAndPublication(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		facts        int
		work, nodes  int64
		calls        int
	}{
		{"empty", "", 0, 27, 5, 10}, {"clean", "a", 0, 600, 15, 31},
		{"protected", "```\nx\n```", 0, 55, 9, 18}, {"pair", "中A", 1, 861, 17, 34},
		{"supplementary", "𠀀A", 1, 862, 17, 34}, {"combining", "Á中", 0, 1117, 15, 33},
		{"growth", "中A中A中A", 5, 1907, 35, 45},
		{"nested-label", "[*中A*](x)", 1, 934, 29, 48},
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
					got, err := d.CJKSpacing(context.Background(), b, index, ParseOptions{})
					if delta < 0 {
						var limit *LimitExceeded
						if !errors.As(err, &limit) || got != nil {
							t.Fatal("direct Err+nil", kind, got, err)
						}
					} else if err != nil || len(got) != tc.facts {
						t.Fatal("exact quota", kind, got, err)
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
					got, err := d.CJKSpacing(blockquoteOperation{caller, owner}, b, index, ParseOptions{})
					if !errors.Is(err, reason) || got != nil || stop.calls != tc.calls {
						t.Fatal("final Err+nil", got, err, stop.calls, tc.calls)
					}
				}
			}
			d, b, index := sectionParse(t, tc.source)
			w, n := b.work, b.nodes
			counter := &widthProjectionStop{Context: context.Background(), stop: 1000000}
			got, err := d.CJKSpacing(counter, b, index, ParseOptions{})
			if err != nil || len(got) != tc.facts || b.work-w != tc.work || b.nodes-n != tc.nodes || counter.calls != tc.calls {
				t.Fatal("independent equation", got, err, b.work-w, b.nodes-n, counter.calls, tc)
			}
		})
	}
}

func TestCJKSpacingExactContributionAssociation(t *testing.T) {
	// Split only a private already-completed fixture's Text contributions. This
	// probes permitted contiguous association and malformed internal associations;
	// no public constructor acquires completion authority.
	for _, mode := range []string{"contiguous", "gap", "transformed", "foreign", "mismatch", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			d, b, index := sectionParse(t, "中A B")
			n := d.root.spec.Children[0].spec.Children[0]
			left := Segment{Source: index.span(0, 3), Transformation: Exact, Semantic: Some("中")}
			right := Segment{Source: index.span(3, 4), Transformation: Exact, Semantic: Some("A")}
			switch mode {
			case "gap":
				right.Source = index.span(5, 6)
				right.Semantic = Some("B")
			case "transformed":
				right.Transformation = Entity
			case "foreign":
				right.Source, _ = NewSource([]byte("中A B")).Span(3, 4)
			case "mismatch":
				right.Semantic = Some("B")
			case "unknown":
				right.Transformation = MappingUnavailable
			}
			n.spec.Content = []Segment{left, right}
			got, err := d.CJKSpacing(context.Background(), b, index, ParseOptions{})
			if mode == "foreign" || mode == "mismatch" || mode == "unknown" {
				if !errors.Is(err, ErrModel) || got != nil {
					t.Fatal(got, err)
				}
				return
			}
			want := 0
			if mode == "contiguous" {
				want = 1
			}
			if err != nil || len(got) != want {
				t.Fatal(got, err)
			}
		})
	}
}
