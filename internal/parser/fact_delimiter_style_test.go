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

func TestDelimiterStyleOriginalAuthority(t *testing.T) {
	d, b, index := sectionParse(t, "A * x *")
	fresh, _ := NewOperationBudget(DefaultLimits())
	detached, _ := NewDocument(d.Root(), d.References())
	for _, tc := range []struct {
		name string
		d    *Document
		b    *OperationBudget
		i    LineIndex
		o    ParseOptions
	}{
		{"foreign-index", d, b, NewLineIndex(NewSource([]byte("A * x *"))), ParseOptions{}},
		{"fresh-budget", d, fresh, index, ParseOptions{}}, {"constructed", detached, b, index, ParseOptions{}},
		{"nil-document", nil, b, index, ParseOptions{}}, {"nil-budget", d, nil, index, ParseOptions{}}, {"foreign-options", d, b, index, ParseOptions{GFM: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.d.DelimiterStyle(context.Background(), tc.b, tc.i, tc.o)
			if !errors.Is(err, ErrModel) || got != nil {
				t.Fatal(got, err)
			}
		})
	}
	//nolint:staticcheck // Deliberately absent caller authority.
	if got, err := d.DelimiterStyle(nil, b, index, ParseOptions{}); !errors.Is(err, ErrModel) || got != nil {
		t.Fatal(got, err)
	}
	d.parsedProfile = strikeOnlyProfile
	if got, err := d.DelimiterStyle(context.Background(), b, index, ParseOptions{}); !errors.Is(err, ErrModel) || got != nil {
		t.Fatal(got, err)
	}
}

func TestDelimiterStyleStaticReservationBoundaries(t *testing.T) {
	// Independent equations frozen before execution in issue-72-static-reservations.
	for _, tc := range []struct {
		name, source string
		work, nodes  int64
		want         int
	}{
		{"empty", "", 37, 4, 0}, {"clean", "---", 66, 8, 0}, {"findings", "A * x *", 3745, 41, 2},
	} {
		for _, kind := range []string{"work", "nodes"} {
			exact := tc.work
			if kind == "nodes" {
				exact = tc.nodes
			}
			for _, delta := range []int64{-1, 0} {
				t.Run(fmt.Sprintf("%s/%s/%d", tc.name, kind, delta), func(t *testing.T) {
					d, b, index := sectionParse(t, tc.source)
					if kind == "work" {
						b.limits.WorkUnits = b.work + exact + delta
					} else {
						b.limits.TotalNodes = b.nodes + exact + delta
						b.limits.DocumentNodes = b.limits.TotalNodes
					}
					got, err := d.DelimiterStyle(context.Background(), b, index, ParseOptions{})
					if delta == 0 {
						if err != nil || got == nil || len(got.Whitespace) != tc.want {
							t.Fatal(got, err)
						}
						return
					}
					var limit *LimitExceeded
					if !errors.As(err, &limit) || got != nil {
						t.Fatal("typed error nil first", got, err)
					}
					got, err = d.DelimiterStyle(context.Background(), b, index, ParseOptions{})
					if !errors.As(err, &limit) || got != nil {
						t.Fatal("sticky", got, err)
					}
				})
			}
		}
	}
}

func TestDelimiterStyleFinalPublicationAndElapsed(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 13}, {"clean", "---", 22}, {"protected", "```\nx\n```", 22}, {"findings", "A * x *", 81}, {"growth", "A * x *\n\nA * x *\n\nA * x *", 204}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+reason.Error(), func(t *testing.T) {
				d, b, index := sectionParse(t, tc.source)
				caller := &thematicStyleStop{Context: context.Background(), stop: tc.final, reason: reason}
				got, err := d.DelimiterStyle(caller, b, index, ParseOptions{})
				if !errors.Is(err, reason) || got != nil || caller.calls != tc.final {
					t.Fatal(got, err, caller.calls, tc.final)
				}
			})
		}
	}
	synctest.Test(t, func(t *testing.T) {
		d, b, index := sectionParse(t, "A * x *")
		b.limits.OperationTime = time.Millisecond
		time.Sleep(2 * time.Millisecond)
		got, err := d.DelimiterStyle(context.Background(), b, index, ParseOptions{})
		var limit *LimitExceeded
		if !errors.As(err, &limit) || limit.Name != "operation-time" || got != nil {
			t.Fatal(got, err)
		}
	})
}

func TestDelimiterStyleValidFactsDetachedAndConcurrent(t *testing.T) {
	d, b, index := sectionParse(t, "A **one _two_**\n\nA * x *")
	got, err := d.DelimiterStyle(context.Background(), b, index, ParseOptions{})
	if err != nil || len(got.Valid) != 2 || len(got.Whitespace) != 2 {
		t.Fatal(got, err)
	}
	for i, w := range []struct {
		kind             Kind
		marker           byte
		width            int
		opening, closing [2]int
	}{{Strong, '*', 2, [2]int{2, 4}, [2]int{13, 15}}, {Emphasis, '_', 1, [2]int{8, 9}, [2]int{12, 13}}} {
		f := got.Valid[i]
		if f.Kind != w.kind || f.Marker != w.marker || f.Width != w.width || [2]int{f.Opening.start, f.Opening.end} != w.opening || [2]int{f.Closing.start, f.Closing.end} != w.closing || f.Envelope.start != w.opening[0] || f.Envelope.end != w.closing[1] {
			t.Fatal(f, w)
		}
	}
	got.Valid[0] = DelimiterFact{}
	got.Whitespace[0] = Span{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			again, err := d.DelimiterStyle(context.Background(), b, index, ParseOptions{})
			if err != nil || again.Valid[0].Width != 2 || !again.Whitespace[0].Present() {
				t.Error(again, err)
			}
			independent, bb, ii := sectionParse(t, "A _ x _")
			other, err := independent.DelimiterStyle(context.Background(), bb, ii, ParseOptions{})
			if err != nil || len(other.Whitespace) != 2 {
				t.Error(other, err)
			}
		})
	}
	wg.Wait()
}

func TestDelimiterStyleStartupQuotaAndProtectedControls(t *testing.T) {
	for _, source := range []string{"", "clean", "```\nx\n```", "A * " + strings.Repeat("*", 1000) + " x *"} {
		t.Run(fmt.Sprintf("bytes-%d", len(source)), func(t *testing.T) {
			d, b, index := sectionParse(t, source)
			b.limits.WorkUnits = b.work + 1
			got, err := d.DelimiterStyle(context.Background(), b, index, ParseOptions{})
			var limit *LimitExceeded
			if !errors.As(err, &limit) || got != nil {
				t.Fatal(got, err)
			}
		})
	}
}

func TestDelimiterStyleLongPoisonProbeReservation(t *testing.T) {
	source := "A " + strings.Repeat("*", 1000) + " x " + strings.Repeat("*", 1000)
	for _, remaining := range []int64{522690, 525713, 525714} {
		t.Run(fmt.Sprint(remaining), func(t *testing.T) {
			d, b, index := sectionParse(t, source)
			b.limits.WorkUnits = b.work + remaining
			got, err := d.DelimiterStyle(context.Background(), b, index, ParseOptions{})
			if remaining == 525714 {
				if err != nil || got == nil || len(got.Whitespace) != 0 {
					t.Fatal(got, err)
				}
				return
			}
			var limit *LimitExceeded
			if !errors.As(err, &limit) || got != nil {
				t.Fatal("long scan typed error and nil", got, err)
			}
		})
	}
}

func TestDelimiterStyleValidFactRoleReservation(t *testing.T) {
	// Independently source-derived before execution: valid-fact-reservations-before-repair.
	for _, kind := range []string{"work", "nodes"} {
		for _, delta := range []int64{-1, 0} {
			t.Run(fmt.Sprintf("%s/%d", kind, delta), func(t *testing.T) {
				d, b, index := sectionParse(t, "*x*")
				if kind == "work" {
					b.limits.WorkUnits = b.work + 685 + delta
				} else {
					b.limits.TotalNodes = b.nodes + 20 + delta
					b.limits.DocumentNodes = b.limits.TotalNodes
				}
				got, err := d.DelimiterStyle(context.Background(), b, index, ParseOptions{})
				if delta < 0 {
					var limit *LimitExceeded
					if !errors.As(err, &limit) || got != nil {
						t.Fatal("valid-fact reservation typed error plus nil first", got, err)
					}
					return
				}
				if err != nil || got == nil || len(got.Valid) != 1 || len(got.Whitespace) != 0 {
					t.Fatal(got, err)
				}
				f := got.Valid[0]
				if f.Kind != Emphasis || f.Marker != '*' || f.Width != 1 || f.Opening.start != 0 || f.Opening.end != 1 || f.Closing.start != 2 || f.Closing.end != 3 || f.Envelope.start != 0 || f.Envelope.end != 3 {
					t.Fatal(f)
				}
			})
		}
	}
}
