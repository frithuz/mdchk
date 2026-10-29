package parser

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestThematicStyleStaticReservationBoundaries(t *testing.T) {
	// Independent pre-execution equations: Working/issue-71-static-reservations.md.
	for _, kind := range []string{"work", "nodes"} {
		exact := int64(78)
		if kind == "nodes" {
			exact = 19
		}
		for _, remaining := range []int64{exact - 1, exact} {
			t.Run(fmt.Sprintf("%s/%d", kind, remaining), func(t *testing.T) {
				d, b, index := sectionParse(t, "---\n\n***")
				if kind == "work" {
					b.limits.WorkUnits = b.work + remaining
				} else {
					b.limits.TotalNodes = b.nodes + remaining
					b.limits.DocumentNodes = b.limits.TotalNodes
				}
				got, err := d.ThematicStyle(context.Background(), b, index, ParseOptions{}, "consistent")
				if remaining == exact {
					if err != nil || len(got) != 1 || got[0].start != 5 || got[0].end != 8 {
						t.Fatal(got, err)
					}
					return
				}
				var limit *LimitExceeded
				if !errors.As(err, &limit) || got != nil {
					t.Fatal("typed error and nil before downstream", got, err)
				}
				got, err = d.ThematicStyle(context.Background(), b, index, ParseOptions{}, "consistent")
				if !errors.As(err, &limit) || got != nil {
					t.Fatal("sticky", got, err)
				}
			})
		}
	}
}

func TestThematicStyleOriginalAuthority(t *testing.T) {
	d, b, index := sectionParse(t, "---\n\n***")
	foreign := NewLineIndex(NewSource([]byte("---\n\n***")))
	fresh, _ := NewOperationBudget(DefaultLimits())
	detached, _ := NewDocument(d.Root(), d.References())
	for _, tc := range []struct {
		name string
		d    *Document
		b    *OperationBudget
		i    LineIndex
		o    ParseOptions
	}{
		{"foreign-index", d, b, foreign, ParseOptions{}}, {"fresh-budget", d, fresh, index, ParseOptions{}}, {"detached", detached, b, index, ParseOptions{}},
		{"nil-document", nil, b, index, ParseOptions{}}, {"nil-budget", d, nil, index, ParseOptions{}}, {"foreign-options", d, b, index, ParseOptions{GFM: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.d.ThematicStyle(context.Background(), tc.b, tc.i, tc.o, "consistent")
			if !errors.Is(err, ErrModel) || got != nil {
				t.Fatal(got, err)
			}
		})
	}
	//nolint:staticcheck // Deliberate absent caller authority.
	if got, err := d.ThematicStyle(nil, b, index, ParseOptions{}, "consistent"); !errors.Is(err, ErrModel) || got != nil {
		t.Fatal(got, err)
	}
	for _, style := range []string{"", "Dash", "unknown"} {
		got, err := d.ThematicStyle(context.Background(), b, index, ParseOptions{}, style)
		if !errors.Is(err, ErrModel) || got != nil {
			t.Fatal(got, err)
		}
	}
}

func TestThematicStyleDetachedGrowthAndConcurrency(t *testing.T) {
	for _, count := range []int{1, 5, 20} {
		d, b, index := sectionParse(t, "---\n\n"+strings.Repeat("***\n\n", count))
		before := b.nodes
		got, err := d.ThematicStyle(context.Background(), b, index, ParseOptions{}, "consistent")
		if err != nil || len(got) != count || b.nodes-before <= int64(count) {
			t.Fatal(got, err)
		}
		got[0] = Span{}
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				spans, err := d.ThematicStyle(context.Background(), b, index, ParseOptions{}, "consistent")
				if err != nil || len(spans) != count || spans[0].start != 5 {
					t.Error(spans, err)
				}
				independent := NewSource([]byte("***\n\n---"))
				fresh, _ := NewOperationBudget(DefaultLimits())
				parsed := ParseWithBudget(context.Background(), independent, fresh)
				spans, err = parsed.Document.ThematicStyle(context.Background(), fresh, NewLineIndex(independent), ParseOptions{}, "consistent")
				if err != nil || len(spans) != 1 || spans[0].start != 5 {
					t.Error(spans, err)
				}
			})
		}
		wg.Wait()
	}
}

func TestThematicStyleLongComparisonAndProtectedWork(t *testing.T) {
	for _, last := range []string{"*", "_"} {
		// Both are valid actual breaks; a same-length late mismatch or clean full
		// equality must inspect original pairs, rather than use family normalization.
		pattern := strings.Repeat("*", 1000) + " " + strings.Repeat("*", 1000)
		second := pattern
		if last == "_" {
			second = strings.Repeat("*", 1001) + " " + strings.Repeat("*", 999)
		}
		d, b, index := sectionParse(t, pattern+"\n\n"+second)
		b.limits.WorkUnits = b.work + 1000
		got, err := d.ThematicStyle(context.Background(), b, index, ParseOptions{}, "consistent")
		var limit *LimitExceeded
		if !errors.As(err, &limit) || got != nil {
			t.Fatal(got, err)
		}
	}
	for _, source := range []string{"", "---", "```\n***\n```", "text<script>\n\n***"} {
		d, b, index := sectionParse(t, source)
		b.limits.WorkUnits = b.work
		got, err := d.ThematicStyle(context.Background(), b, index, ParseOptions{}, "consistent")
		var limit *LimitExceeded
		if !errors.As(err, &limit) || got != nil {
			t.Fatal(source, got, err)
		}
	}
}

func TestThematicStyleFinalPublicationAndElapsed(t *testing.T) {
	// Fixed source-derived Err visits authored before execution in
	// Working/issue-71-final-seam-static.md. Never learn the final stop at runtime.
	for _, tc := range []struct {
		name, source string
		final        int
	}{
		{"empty", "", 8}, {"clean", "---", 16}, {"protected", "```\n***\n```", 14},
		{"violation", "---\n\n***", 28},
		{"growth", "---\n\n***\n\n***\n\n***\n\n***\n\n***", 72},
	} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+"/"+reason.Error(), func(t *testing.T) {
				d, b, index := sectionParse(t, tc.source)
				caller := &thematicStyleStop{Context: context.Background(), stop: tc.final, reason: reason}
				got, err := d.ThematicStyle(caller, b, index, ParseOptions{}, "consistent")
				if !errors.Is(err, reason) || got != nil {
					t.Fatal("direct producer final Err and nil first", got, err)
				}
				if caller.calls != tc.final {
					t.Fatal("fixed final seam", caller.calls, tc.final)
				}
			})
		}
	}
	synctest.Test(t, func(t *testing.T) {
		d, b, index := sectionParse(t, "---\n\n***")
		b.limits.OperationTime = time.Millisecond
		time.Sleep(2 * time.Millisecond)
		for range 2 {
			got, err := d.ThematicStyle(context.Background(), b, index, ParseOptions{}, "consistent")
			var limit *LimitExceeded
			if !errors.As(err, &limit) || limit.Name != "operation-time" || got != nil {
				t.Fatal(got, err)
			}
		}
	})
}

type thematicStyleStop struct {
	context.Context
	calls, stop int
	reason      error
}

func (c *thematicStyleStop) Err() error {
	c.calls++
	if c.stop > 0 && c.calls >= c.stop {
		return c.reason
	}
	return c.Context.Err()
}
