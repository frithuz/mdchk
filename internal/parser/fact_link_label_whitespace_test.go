package parser

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestLinkLabelWhitespaceOriginalAuthority(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, fault := range []string{"original", "detached", "wrong-source", "wrong-budget", "wrong-options", "incomplete", "nil-document", "nil-context"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, fault), func(t *testing.T) {
				d, b, i, o := md011Parse(t, "[ x](y)", gfm)
				got, e := d.LinkLabelWhitespace(context.Background(), b, i, o)
				if e != nil || got.Availability != Available || len(got.Spans) != 1 || got.Spans[0].start != 1 || got.Spans[0].end != 2 {
					t.Fatal("positive prerequisite", got, e)
				}
				ctx := context.Background()
				switch fault {
				case "detached":
					d, _ = NewDocument(d.Root(), d.References())
				case "wrong-source":
					i = NewLineIndex(NewSource([]byte("[ x](y)")))
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
				got, e = d.LinkLabelWhitespace(ctx, b, i, o)
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
func TestLinkLabelWhitespaceDetachedConcurrentResults(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		d, b, i, o := md011Parse(t, "[ x](y)", gfm)
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				got, e := d.LinkLabelWhitespace(context.Background(), b, i, o)
				if e != nil || got.Availability != Available || len(got.Spans) != 1 || got.Spans[0].start != 1 || got.Spans[0].end != 2 {
					t.Error(got, e)
					return
				}
				got.Spans[0] = Span{}
			})
		}
		wg.Wait()
		got, e := d.LinkLabelWhitespace(context.Background(), b, i, o)
		if e != nil || len(got.Spans) != 1 || got.Spans[0].start != 1 || got.Spans[0].SourceToken() != i.source.Token() {
			t.Fatal(got, e)
		}
	}
}
func TestLinkLabelWhitespaceUnknownAndLateProvenance(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, tc := range []struct {
			source    string
			available Availability
			count     int
		}{
			{"[ x](y)\n\n<div>\n<x title=\"", Available, 1},
			{"[ x](y)\n\n<div>\n<x title=\"\n\n[ later](y)", Unavailable, 0},
			{"![<pre>](image) [ x](y)", Available, 1},
		} {
			d, b, i, o := md011Parse(t, tc.source, gfm)
			got, e := d.LinkLabelWhitespace(context.Background(), b, i, o)
			if e != nil || got.Availability != tc.available || len(got.Spans) != tc.count {
				t.Fatal(gfm, tc.source, got, e)
			}
		}
	}
	for _, fault := range []string{"label", "mapping", "semantic", "source", "foreign", "range", "exact"} {
		t.Run(fault, func(t *testing.T) {
			d, b, i, o := md011Parse(t, "[ x](y)\n\n[ later](y)", false)
			nodes := md070Nodes(d, Link)
			if len(nodes) != 2 {
				t.Fatal(nodes)
			}
			n := nodes[1]
			switch fault {
			case "label":
				n.spec.Properties.Link.Label.Semantic.Present = false
			case "mapping":
				n.spec.Properties.Link.Label.Segments[0].Transformation = MappingUnavailable
			case "semantic":
				n.spec.Properties.Link.Label.Segments[0].Semantic.Present = false
			case "source":
				n.spec.Properties.Link.Label.Segments[0].Source = Span{}
			case "foreign":
				n.spec.Properties.Link.Label.Segments[0].Source.source = NewSource(i.source.Bytes())
			case "exact":
				n.spec.Properties.Link.Label.Segments[0].Semantic.Value = "wrong"
			case "range":
				n.spec.Properties.Link.Label.Segments[0].Source.end = i.source.Len() + 1
			}
			got, e := d.LinkLabelWhitespace(context.Background(), b, i, o)
			if fault == "foreign" || fault == "range" || fault == "source" || fault == "exact" {
				if !errors.Is(e, ErrModel) || got.Spans != nil {
					t.Fatal(got, e)
				}
			} else if e != nil || got.Availability != Unavailable || got.Spans != nil {
				t.Fatal(got, e)
			}
		})
	}
}
func TestLinkLabelWhitespaceStickyExhaustedBudget(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, kind := range []string{"work", "nodes"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, kind), func(t *testing.T) {
				d, b, i, o := md011Parse(t, "[ x](y)", gfm)
				before, e := d.LinkLabelWhitespace(context.Background(), b, i, o)
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
					got, e := d.LinkLabelWhitespace(context.Background(), b, i, o)
					if !errors.As(e, &limit) || got.Spans != nil || got.Availability != "" {
						t.Fatal(got, e)
					}
				}
			})
		}
	}
}
