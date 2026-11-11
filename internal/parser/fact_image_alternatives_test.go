package parser

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestImageAlternativesOriginalAuthority(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, fault := range []string{"original", "detached", "wrong-source", "wrong-budget", "wrong-options", "incomplete", "nil-document", "nil-context"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, fault), func(t *testing.T) {
				d, b, i, o := md011Parse(t, "![](x)", gfm)
				got, e := d.ImageAlternatives(context.Background(), b, i, o, false)
				if e != nil || got.Availability != Available || len(got.Spans) != 1 || got.Spans[0].start != 2 || got.Spans[0].end != 2 {
					t.Fatal("positive prerequisite", got, e)
				}
				ctx := context.Background()
				switch fault {
				case "detached":
					d, _ = NewDocument(d.Root(), d.References())
				case "wrong-source":
					i = NewLineIndex(NewSource([]byte("![](x)")))
				case "wrong-budget":
					b, _ = NewOperationBudget(DefaultLimits())
				case "wrong-options":
					o.GFM = !o.GFM
				case "incomplete":
					d.parsedComplete = false
				case "nil-document":
					d = nil
				case "nil-context":
					ctx = nil
				}
				got, e = d.ImageAlternatives(ctx, b, i, o, false)
				if fault == "original" {
					if e != nil || len(got.Spans) != 1 {
						t.Fatal(got, e)
					}
				} else if !errors.Is(e, ErrModel) || got.Spans != nil {
					t.Fatal(got, e)
				}
			})
		}
	}
}
func TestImageAlternativesDetachedConcurrentResults(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		d, b, i, o := md011Parse(t, "![](x)", gfm)
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				got, e := d.ImageAlternatives(context.Background(), b, i, o, false)
				if e != nil || got.Availability != Available || len(got.Spans) != 1 || got.Spans[0].start != 2 || got.Spans[0].end != 2 {
					t.Error(got, e)
					return
				}
				got.Spans[0] = Span{}
			})
		}
		wg.Wait()
		got, e := d.ImageAlternatives(context.Background(), b, i, o, false)
		if e != nil || len(got.Spans) != 1 || got.Spans[0].start != 2 || got.Spans[0].SourceToken() != i.source.Token() {
			t.Fatal(got, e)
		}
	}
}
func TestImageAlternativesStickyExhaustedBudget(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, kind := range []string{"work", "nodes"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, kind), func(t *testing.T) {
				d, b, i, o := md011Parse(t, "![](x)", gfm)
				before, e := d.ImageAlternatives(context.Background(), b, i, o, false)
				if e != nil || len(before.Spans) != 1 {
					t.Fatal("positive precondition", before, e)
				}
				if kind == "work" {
					e = b.ChargeWork(b.Limits().WorkUnits + 1)
				} else {
					e = b.ChargeNodes(b.Limits().TotalNodes + 1)
				}
				var limit *LimitExceeded
				if !errors.As(e, &limit) {
					t.Fatal(e)
				}
				for range 2 {
					got, e := d.ImageAlternatives(context.Background(), b, i, o, false)
					if !errors.As(e, &limit) || got.Spans != nil || got.Availability != "" {
						t.Fatal(got, e)
					}
				}
			})
		}
	}
}

func TestImageAlternativesPartialResultsAreDetached(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		d, b, i, o := md011Parse(t, "![](a) <img alt=\"\" alt=x> <img> ![](b)", gfm)
		got, e := d.ImageAlternatives(context.Background(), b, i, o, false)
		if e != nil || got.Availability != Unavailable || len(got.Spans) != 3 || len(got.Uncertainty) != 1 {
			t.Fatal(got, e)
		}
		got.Spans[0], got.Uncertainty[0] = Span{}, Span{}
		fresh, e := d.ImageAlternatives(context.Background(), b, i, o, false)
		if e != nil || fresh.Availability != Unavailable || len(fresh.Spans) != 3 || len(fresh.Uncertainty) != 1 || fresh.Spans[0].start != 2 || !fresh.Uncertainty[0].present {
			t.Fatal(fresh, e)
		}
	}
}
