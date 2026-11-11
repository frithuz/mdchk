package parser

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBareURLsOriginalAuthority(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, fault := range []string{"original", "detached", "wrong-source", "wrong-budget", "wrong-options", "incomplete", "nil-document", "nil-context"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, fault), func(t *testing.T) {
				d, b, i, o := md011Parse(t, "https://example.org", gfm)
				got, e := d.BareURLs(context.Background(), b, i, o)
				if e != nil || got.Availability != Available || len(got.Spans) != 1 || got.Spans[0].start != 0 || got.Spans[0].end != 19 {
					t.Fatal("positive original prerequisite", got, e)
				}
				ctx := context.Background()
				switch fault {
				case "detached":
					d, _ = NewDocument(d.Root(), d.References())
				case "wrong-source":
					i = NewLineIndex(NewSource([]byte("https://example.org")))
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
				got, e = d.BareURLs(ctx, b, i, o)
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
func TestBareURLsGFMOriginsAndDetachedConcurrentResults(t *testing.T) {
	const source = "https://example.org <https://example.org> ftp://example.org www.example.org user@example.org"
	for _, gfm := range []bool{false, true} {
		d, b, i, o := md011Parse(t, source, gfm)
		if gfm {
			origins := map[string]int{}
			for _, n := range md070Nodes(d, Link) {
				origins[n.spec.Properties.Link.Origin]++
			}
			if origins["bare-gfm"] != 3 || origins["autolink"] != 1 {
				t.Fatal(origins)
			}
		}
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				got, e := d.BareURLs(context.Background(), b, i, o)
				if e != nil || got.Availability != Available || len(got.Spans) != 2 || got.Spans[0].start != 0 || got.Spans[0].end != 19 || got.Spans[1].start != 42 || got.Spans[1].end != 59 {
					t.Error(got, e)
					return
				}
				got.Spans[0] = Span{}
			})
		}
		wg.Wait()
		got, e := d.BareURLs(context.Background(), b, i, o)
		if e != nil || len(got.Spans) != 2 || got.Spans[0].start != 0 || got.Spans[0].SourceToken() != i.source.Token() {
			t.Fatal(got, e)
		}
	}
}
func TestBareURLsUnknownRawAndLateProvenance(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, tc := range []struct {
			source    string
			available Availability
			count     int
		}{
			{"https://example.org\n\n<div>\n<x title=\"", Available, 1},
			{"https://example.org\n\n<div>\n<x title=\"\n\nftp://later.example.org", Unavailable, 0},
			{"https://example.org\n\n<div>\n<x title=\"\n\nhttps://later.example.org", Unavailable, 0},
			{"![<pre>](image.png) https://example.org", Available, 1},
		} {
			d, b, i, o := md011Parse(t, tc.source, gfm)
			got, e := d.BareURLs(context.Background(), b, i, o)
			if e != nil || got.Availability != tc.available || len(got.Spans) != tc.count {
				t.Fatal(gfm, tc.source, got, e)
			}
		}
	}
	for _, fault := range []string{"literal", "mapping", "semantic", "source", "foreign", "range", "exact"} {
		t.Run(fault, func(t *testing.T) {
			d, b, i, o := md011Parse(t, "ftp://example.org\n\nftp://later.example.org", false)
			nodes := md070Nodes(d, Text)
			if len(nodes) != 2 {
				t.Fatal(nodes)
			}
			n := nodes[1]
			switch fault {
			case "literal":
				n.spec.Properties.Literal.Present = false
			case "mapping":
				n.spec.Content[0].Transformation = MappingUnavailable
			case "semantic":
				n.spec.Content[0].Semantic.Present = false
			case "source":
				n.spec.Content[0].Source = Span{}
			case "foreign":
				n.spec.Content[0].Source.source = NewSource(i.source.Bytes())
			case "range":
				n.spec.Content[0].Source.end = i.source.Len() + 1
			case "exact":
				n.spec.Content[0].Semantic.Value = "wrong"
			}
			got, e := d.BareURLs(context.Background(), b, i, o)
			if fault == "foreign" || fault == "range" || fault == "exact" {
				if !errors.Is(e, ErrModel) || got.Spans != nil {
					t.Fatal(got, e)
				}
			} else if e != nil || got.Availability != Unavailable || got.Spans != nil {
				t.Fatal(got, e)
			}
		})
	}
}
func TestBareURLsBoundedRepeatedCandidates(t *testing.T) {
	// Failed scheme probes and suffix cleanup remain linear rather than
	// restarting a scan at every overlapping prefix in a lexical candidate.
	for _, gfm := range []bool{false, true} {
		text := strings.Repeat("ftp://", 2000) + "example.org" + strings.Repeat(")?!", 2000)
		limits := DefaultLimits()
		limits.WorkUnits = 1024*int64(len(text)) + 65536
		source := NewSource([]byte(text))
		budget, e := NewOperationBudget(limits)
		if e != nil {
			t.Fatal(e)
		}
		options := ParseOptions{GFM: gfm}
		parsed := ParseWithOptions(context.Background(), source, budget, options)
		if parsed.Status != Complete {
			t.Fatal(parsed)
		}
		got, e := parsed.Document.BareURLs(context.Background(), budget, NewLineIndex(source), options)
		if e != nil || got.Availability != Available || len(got.Spans) != 1 || got.Spans[0].end != 12011 {
			t.Fatal(got, e)
		}
	}
}

func TestBareURLsStickyExhaustedBudget(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, kind := range []string{"work", "nodes"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, kind), func(t *testing.T) {
				d, b, i, o := md011Parse(t, "https://example.org", gfm)
				before, e := d.BareURLs(context.Background(), b, i, o)
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
					got, e := d.BareURLs(context.Background(), b, i, o)
					if !errors.As(e, &limit) || got.Spans != nil || got.Availability != "" {
						t.Fatal(got, e)
					}
				}
			})
		}
	}
}

func TestBareURLsCleanProseStillValidatesMapping(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		d, b, i, o := md011Parse(t, "plain prose without a URL", gfm)
		before, e := d.BareURLs(context.Background(), b, i, o)
		if e != nil || before.Availability != Available || len(before.Spans) != 0 {
			t.Fatal(before, e)
		}
		text := md070Nodes(d, Text)
		if len(text) != 1 {
			t.Fatal(text)
		}
		text[0].spec.Content[0].Transformation = MappingUnavailable
		got, e := d.BareURLs(context.Background(), b, i, o)
		if e != nil || got.Availability != Unavailable || got.Spans != nil {
			t.Fatal(got, e)
		}
	}
}

func BenchmarkBareURLsProse(b *testing.B) {
	for _, n := range []int{16384, 65536, 262144, 600000} {
		for _, shape := range []string{"single", "wrapped"} {
			b.Run(fmt.Sprintf("%s/%d", shape, n), func(b *testing.B) {
				text := strings.Repeat("plain prose ", n/12)
				if shape == "wrapped" {
					text = strings.Repeat(strings.Repeat("plain prose ", 6)+"\n", n/73)
				}
				source := NewSource([]byte(text))
				// The observation owns the same cumulative budget every time.
				// Raised measurement quotas prevent iteration count from becoming
				// the measured limit; default-budget behavior has regressions above.
				limits := DefaultLimits()
				limits.TotalNodes = 1 << 40
				limits.WorkUnits = 1 << 60
				limits.OperationTime = time.Hour
				budget, e := NewOperationBudget(limits)
				if e != nil {
					b.Fatal(e)
				}
				options := ParseOptions{}
				parsed := ParseWithOptions(context.Background(), source, budget, options)
				if parsed.Status != Complete {
					b.Fatal(parsed)
				}
				index := NewLineIndex(source)
				b.SetBytes(int64(source.Len()))
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					got, e := parsed.Document.BareURLs(context.Background(), budget, index, options)
					if e != nil || got.Availability != Available || len(got.Spans) != 0 {
						b.Fatal(got, e)
					}
				}
			})
		}
	}
}
