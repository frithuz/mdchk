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

func TestCodeSpanSpacesOriginalAuthority(t *testing.T) {
	d, b, index := sectionParse(t, "` x `")
	fresh, _ := NewOperationBudget(DefaultLimits())
	detached, _ := NewDocument(d.Root(), d.References())
	for _, tc := range []struct {
		name string
		d    *Document
		b    *OperationBudget
		i    LineIndex
		o    ParseOptions
	}{
		{"foreign-index", d, b, NewLineIndex(NewSource([]byte("` x `"))), ParseOptions{}},
		{"fresh-budget", d, fresh, index, ParseOptions{}}, {"constructed", detached, b, index, ParseOptions{}},
		{"nil-document", nil, b, index, ParseOptions{}}, {"nil-budget", d, nil, index, ParseOptions{}}, {"foreign-options", d, b, index, ParseOptions{GFM: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.d.CodeSpanSpaces(context.Background(), tc.b, tc.i, tc.o)
			if !errors.Is(err, ErrModel) || got != nil {
				t.Fatal(got, err)
			}
		})
	}
	//nolint:staticcheck // Deliberately absent caller authority.
	if got, err := d.CodeSpanSpaces(nil, b, index, ParseOptions{}); !errors.Is(err, ErrModel) || got != nil {
		t.Fatal(got, err)
	}
	for _, profile := range []parserProfile{strikeOnlyProfile, tablesOnlyProfile} {
		d.parsedProfile = profile
		if got, err := d.CodeSpanSpaces(context.Background(), b, index, ParseOptions{}); !errors.Is(err, ErrModel) || got != nil {
			t.Fatal(got, err)
		}
	}
	d.parsedProfile = coreProfile
	d.parsedComplete = false
	if got, err := d.CodeSpanSpaces(context.Background(), b, index, ParseOptions{}); !errors.Is(err, ErrModel) || got != nil {
		t.Fatal(got, err)
	}
}

func TestCodeSpanSpacesStaticReservationBoundaries(t *testing.T) {
	// Source-authored equations frozen before execution in issue-73-static-reservations.md.
	for _, tc := range []struct {
		name, source string
		work, nodes  int64
		want         int
	}{
		{"empty", "", 24, 3, 0}, {"clean", "---", 40, 4, 0}, {"protected", "```\nx\n```", 40, 4, 0},
		{"findings", "` x `", 139, 10, 2}, {"growth", "` x ` ` y ` ` z `", 381, 44, 6},
		{"long-failed-probe", "`" + strings.Repeat(" ", 1000) + "x" + strings.Repeat(" ", 1000) + "`", 8131, 10, 2},
	} {
		for _, kind := range []string{"work", "nodes"} {
			for _, delta := range []int64{-1, 0} {
				t.Run(fmt.Sprintf("%s/%s/%d", tc.name, kind, delta), func(t *testing.T) {
					d, b, index := sectionParse(t, tc.source)
					if kind == "work" {
						b.limits.WorkUnits = b.work + tc.work + delta
					} else {
						b.limits.TotalNodes = b.nodes + tc.nodes + delta
						b.limits.DocumentNodes = b.limits.TotalNodes
					}
					got, err := d.CodeSpanSpaces(context.Background(), b, index, ParseOptions{})
					if delta == 0 {
						if err != nil || len(got) != tc.want {
							t.Fatal(got, err)
						}
						return
					}
					var limit *LimitExceeded
					if !errors.As(err, &limit) || got != nil {
						t.Fatal("typed error nil first", got, err)
					}
					got, err = d.CodeSpanSpaces(context.Background(), b, index, ParseOptions{})
					if !errors.As(err, &limit) || got != nil {
						t.Fatal("sticky", got, err)
					}
				})
			}
		}
	}
}

func TestCodeSpanSpacesFinalPublicationAndElapsed(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{
		{"empty", "", 7}, {"clean", "---", 11}, {"protected", "```\nx\n```", 11}, {"findings", "` x `", 35}, {"growth", "` x ` ` y ` ` z `", 92}, {"long-failed-probe", "`" + strings.Repeat(" ", 1000) + "x" + strings.Repeat(" ", 1000) + "`", 3032},
	} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+reason.Error(), func(t *testing.T) {
				d, b, index := sectionParse(t, tc.source)
				caller := &thematicStyleStop{Context: context.Background(), stop: tc.final, reason: reason}
				got, err := d.CodeSpanSpaces(caller, b, index, ParseOptions{})
				if !errors.Is(err, reason) || got != nil || caller.calls != tc.final {
					t.Fatal(got, err, caller.calls, tc.final)
				}
			})
		}
	}
	synctest.Test(t, func(t *testing.T) {
		d, b, index := sectionParse(t, "` x `")
		b.limits.OperationTime = time.Millisecond
		time.Sleep(2 * time.Millisecond)
		got, err := d.CodeSpanSpaces(context.Background(), b, index, ParseOptions{})
		var limit *LimitExceeded
		if !errors.As(err, &limit) || limit.Name != "operation-time" || got != nil {
			t.Fatal(got, err)
		}
	})
}

func TestCodeSpanSpacesDetachedAndConcurrent(t *testing.T) {
	d, b, index := sectionParse(t, "` x `")
	got, err := d.CodeSpanSpaces(context.Background(), b, index, ParseOptions{})
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	got[0] = Span{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			again, err := d.CodeSpanSpaces(context.Background(), b, index, ParseOptions{})
			if err != nil || len(again) != 2 || again[0].start != 1 || again[1].start != 3 {
				t.Error(again, err)
			}
		})
	}
	wg.Wait()
}
