package parser

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestConfiguredNamesOriginalAuthority(t *testing.T) {
	d, b, index := sectionParse(t, "github")
	fresh, _ := NewOperationBudget(DefaultLimits())
	detached, _ := NewDocument(d.Root(), d.References())
	for _, tc := range []struct {
		name string
		d    *Document
		b    *OperationBudget
		i    LineIndex
		o    ParseOptions
	}{
		{"foreign-index", d, b, NewLineIndex(NewSource([]byte("github"))), ParseOptions{}}, {"fresh-budget", d, fresh, index, ParseOptions{}}, {"constructed", detached, b, index, ParseOptions{}}, {"nil-document", nil, b, index, ParseOptions{}}, {"nil-budget", d, nil, index, ParseOptions{}}, {"foreign-options", d, b, index, ParseOptions{GFM: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.d.ConfiguredNames(context.Background(), tc.b, tc.i, tc.o, []string{"GitHub"}, false)
			if !errors.Is(err, ErrModel) || got != nil {
				t.Fatal(got, err)
			}
		})
	}
	//nolint:staticcheck // Deliberately absent caller authority.
	if got, err := d.ConfiguredNames(nil, b, index, ParseOptions{}, nil, false); !errors.Is(err, ErrModel) || got != nil {
		t.Fatal(got, err)
	}
	for _, profile := range []parserProfile{strikeOnlyProfile, tablesOnlyProfile} {
		d.parsedProfile = profile
		if got, err := d.ConfiguredNames(context.Background(), b, index, ParseOptions{}, nil, false); !errors.Is(err, ErrModel) || got != nil {
			t.Fatal(got, err)
		}
	}
	d.parsedProfile = coreProfile
	d.parsedComplete = false
	if got, err := d.ConfiguredNames(context.Background(), b, index, ParseOptions{}, nil, false); !errors.Is(err, ErrModel) || got != nil {
		t.Fatal(got, err)
	}
}

func TestConfiguredNamesStaticReservationBoundaries(t *testing.T) {
	// Source-derived, independently reviewed before first quota execution.
	for _, tc := range []struct {
		name, source string
		names        []string
		work, nodes  int64
		want, final  int
	}{
		{"no-names", "github", nil, 3, 1, 0, 4}, {"empty", "", []string{"GitHub"}, 91, 24, 0, 17},
		{"clean", "GitHub", []string{"GitHub"}, 1118, 55, 0, 72}, {"finding", "github", []string{"GitHub"}, 1120, 57, 1, 74},
		{"protected", "~~~\ngithub\n~~~", []string{"GitHub"}, 122, 28, 0, 26},
		{"longest", "github actions", []string{"GitHub", "GitHub Actions"}, 2359, 125, 1, 126},
		{"exact-consumed", "GitHub Actions", []string{"GitHub Actions", "Actions"}, 2336, 126, 0, 112},
		{"fold-tie", "GITHUB", []string{"GitHub", "Github"}, 1206, 77, 1, 93},
		{"growth", "github-github-github", []string{"GitHub"}, 3055, 127, 3, 146},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, kind := range []string{"work", "nodes"} {
				for _, delta := range []int64{-1, 0} {
					t.Run(fmt.Sprintf("%s/%d", kind, delta), func(t *testing.T) {
						d, b, index := sectionParse(t, tc.source)
						if kind == "work" {
							b.limits.WorkUnits = b.work + tc.work + delta
						} else {
							b.limits.TotalNodes = b.nodes + tc.nodes + delta
							b.limits.DocumentNodes = b.limits.TotalNodes
						}
						got, err := d.ConfiguredNames(context.Background(), b, index, ParseOptions{}, tc.names, false)
						if delta == 0 {
							if err != nil || len(got) != tc.want {
								t.Fatal(got, err)
							}
							return
						}
						var limit *LimitExceeded
						if !errors.As(err, &limit) || got != nil {
							t.Fatal("first failure", got, err)
						}
						got, err = d.ConfiguredNames(context.Background(), b, index, ParseOptions{}, tc.names, false)
						if !errors.As(err, &limit) || got != nil {
							t.Fatal("sticky", got, err)
						}
					})
				}
			}
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(reason.Error(), func(t *testing.T) {
					d, b, index := sectionParse(t, tc.source)
					caller := &thematicStyleStop{Context: context.Background(), stop: tc.final, reason: reason}
					got, err := d.ConfiguredNames(caller, b, index, ParseOptions{}, tc.names, false)
					if !errors.Is(err, reason) || got != nil || caller.calls != tc.final {
						t.Fatal(got, err, caller.calls, tc.final)
					}
				})
			}
		})
	}
}

func TestConfiguredNamesStickyElapsedAndDetached(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d, b, index := sectionParse(t, "github")
		b.limits.OperationTime = time.Millisecond
		time.Sleep(2 * time.Millisecond)
		for range 2 {
			got, err := d.ConfiguredNames(context.Background(), b, index, ParseOptions{}, []string{"GitHub"}, false)
			var limit *LimitExceeded
			if !errors.As(err, &limit) || limit.Name != "operation-time" || got != nil {
				t.Fatal(got, err)
			}
		}
	})
	d, b, index := sectionParse(t, "github-github")
	got, err := d.ConfiguredNames(context.Background(), b, index, ParseOptions{}, []string{"GitHub"}, false)
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	got[0] = Span{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			again, err := d.ConfiguredNames(context.Background(), b, index, ParseOptions{}, []string{"GitHub"}, false)
			if err != nil || len(again) != 2 || again[0].start != 0 || again[1].start != 7 {
				t.Error(again, err)
			}
		})
	}
	wg.Wait()
}
