package parser

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const md011Example = "(Guide)[https://example.org]"

func md011Parse(t *testing.T, text string, gfm bool) (*Document, *OperationBudget, LineIndex, ParseOptions) {
	t.Helper()
	source := NewSource([]byte(text))
	budget, err := NewOperationBudget(DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	options := ParseOptions{GFM: gfm}
	parsed := ParseWithOptions(context.Background(), source, budget, options)
	if parsed.Status != Complete || parsed.Document == nil {
		t.Fatal(parsed)
	}
	return parsed.Document, budget, NewLineIndex(source), options
}

func md011Positive(t *testing.T, gfm bool) {
	t.Helper()
	d, b, index, options := md011Parse(t, md011Example, gfm)
	got, err := d.SwappedLinks(context.Background(), b, index, options)
	if err != nil || got.Availability != Available || len(got.Spans) != 1 || got.Spans[0].start != 0 || got.Spans[0].end != 28 || got.Spans[0].SourceToken() != index.source.Token() {
		t.Fatal("actual original positive prerequisite", got, err)
	}
}

func TestSwappedLinksLateTextProvenanceAndDetached(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, fault := range []string{"unchanged", "literal", "content", "source", "semantic", "unavailable", "foreign", "range", "exact-mismatch", "transformation", "normalized", "partial-tab", "synthetic"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, fault), func(t *testing.T) {
				d, b, index, o := md011Parse(t, md011Example+"\n\n"+md011Example, gfm)
				pre, e := d.SwappedLinks(context.Background(), b, index, o)
				if e != nil || pre.Availability != Available || len(pre.Spans) != 2 || pre.Spans[0].start != 0 || pre.Spans[1].start != 30 {
					t.Fatal("actual two-positive prerequisite", pre, e)
				}
				pre.Spans[0] = Span{}
				nodes := md070Nodes(d, Text)
				if len(nodes) != 2 || len(nodes[1].spec.Content) == 0 {
					t.Fatal("original Text ownership", nodes)
				}
				n := nodes[1]
				switch fault {
				case "literal":
					n.spec.Properties.Literal.Present = false
				case "content":
					n.spec.Content = nil
				case "source":
					n.spec.Content[0].Source = Span{}
				case "semantic":
					n.spec.Content[0].Semantic.Present = false
				case "unavailable":
					n.spec.Content[0].Transformation = MappingUnavailable
				case "foreign":
					n.spec.Content[0].Source.source = NewSource(index.source.Bytes())
				case "range":
					n.spec.Content[0].Source.end = index.source.Len() + 1
				case "exact-mismatch":
					n.spec.Content[0].Semantic.Value = "wrong"
				case "transformation":
					n.spec.Content[0].Transformation = "other"
				case "normalized":
					n.spec.Content[0].Transformation = Normalized
				case "partial-tab":
					n.spec.Content[0].Transformation = PartialTab
				case "synthetic":
					n.spec.Content[0].Transformation = Synthetic
				}
				got, e := d.SwappedLinks(context.Background(), b, index, o)
				switch fault {
				case "literal", "content", "source", "semantic", "unavailable":
					if e != nil || got.Availability != Unavailable || got.Spans != nil {
						t.Fatal(got, e)
					}
				case "foreign", "range", "exact-mismatch", "transformation":
					if !errors.Is(e, ErrModel) || got.Spans != nil || got.Availability != "" {
						t.Fatal(got, e)
					}
				default:
					want := 1
					if fault == "unchanged" {
						want = 2
					}
					if e != nil || got.Availability != Available || len(got.Spans) != want || got.Spans[0].start != 0 || got.Spans[0].end != 28 {
						t.Fatal(got, e)
					}
				}
			})
		}
	}
}

func TestSwappedLinksUnknownRawCutoff(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, tc := range []struct {
			name, source string
			known        bool
			count        int
		}{
			{"unknown-after-last", md011Example + "\n\n<div>\n<x title=\"", true, 1},
			{"unknown-before-later", md011Example + "\n\n<div>\n<x title=\"\n\n" + md011Example, false, 0},
			{"unknown-before-invalid-url", md011Example + "\n\n<div>\n<x title=\"\n\n(x)[relative]", false, 0},
			{"unknown-before-unclosed", md011Example + "\n\n<div>\n<x title=\"\n\n(x)[", false, 0},
			{"opaque-image-raw", "![<pre>](image.png) " + md011Example, true, 1},
		} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, tc.name), func(t *testing.T) {
				md011Positive(t, gfm)
				d, b, i, o := md011Parse(t, tc.source, gfm)
				got, e := d.SwappedLinks(context.Background(), b, i, o)
				want := Unavailable
				if tc.known {
					want = Available
				}
				if e != nil || got.Availability != want || len(got.Spans) != tc.count {
					t.Fatal(got, e)
				}
			})
		}
	}
}

func TestSwappedLinksHostGuardIndependentOfGODEBUG(t *testing.T) {
	for _, setting := range []string{"0", "1"} {
		t.Run("urlstrictcolons="+setting, func(t *testing.T) {
			t.Setenv("GODEBUG", "urlstrictcolons="+setting)
			for _, gfm := range []bool{false, true} {
				md011Positive(t, gfm)
				for _, tc := range []struct {
					source string
					count  int
				}{
					{"(Guide)[https://example.org:80:90/path]", 0},
					{"(Guide)[https://&#91;2001:db8::1&#93;/path]", 1},
					{"(Guide)[https://example.org:8080/path]", 1},
				} {
					d, b, i, o := md011Parse(t, tc.source, gfm)
					got, e := d.SwappedLinks(context.Background(), b, i, o)
					if e != nil || got.Availability != Available || len(got.Spans) != tc.count {
						t.Fatal(setting, gfm, tc.source, got, e)
					}
				}
			}
		})
	}
}

func TestSwappedLinksRepeatedConcurrentDetachedObservation(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		d, b, i, o := md011Parse(t, md011Example, gfm)
		before, e := d.SwappedLinks(context.Background(), b, i, o)
		if e != nil || len(before.Spans) != 1 {
			t.Fatal(before, e)
		}
		root, token := d.root, d.Source().Token()
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				got, e := d.SwappedLinks(context.Background(), b, i, o)
				if e != nil || !reflect.DeepEqual(got, before) {
					t.Error(got, e)
				}
				if len(got.Spans) > 0 {
					got.Spans[0] = Span{}
				}
			})
		}
		wg.Wait()
		if d.root != root || d.Source().Token() != token || d.Source().content != md011Example {
			t.Fatal("original source/tree changed")
		}
	}
}

// This helper inventory deliberately excludes parse/traversal cost. The actual
// producer prerequisite above prevents private accounting from replacing a real
// production observation; constants were derived from source before any runtime.
func md011Units(value string) []swappedLinkUnit {
	var units []swappedLinkUnit
	for i := range len(value) {
		c := value[i]
		u := swappedLinkUnit{start: i, end: i + 1, value: value[i : i+1], nonblank: c != ' '}
		if strings.ContainsRune("()[]!", rune(c)) {
			u.syntax = c
		}
		units = append(units, u)
	}
	return units
}

func TestSwappedLinksStaticURLAndFlushReservations(t *testing.T) {
	md011Positive(t, false)
	for _, stage := range []string{"url", "flush"} {
		for _, resource := range []string{"work", "nodes"} {
			for _, delta := range []int64{-1, 0} {
				t.Run(fmt.Sprintf("%s/%s/delta-%d", stage, resource, delta), func(t *testing.T) {
					// URL: 2*9 +64*9+256*9+512=3410W; 8*9+16=88N.
					// Flush: URL +112 group +16 cursor/terminal +12 adjacency
					// +1 label +4 first-span growth =3555W, and span adds2N.
					work, nodes := int64(3410), int64(88)
					if stage == "flush" {
						work, nodes = 3555, 90
					}
					limits := DefaultLimits()
					if resource == "work" {
						limits.WorkUnits = work + delta
					} else {
						limits.TotalNodes = nodes + delta
						limits.DocumentNodes = limits.TotalNodes
					}
					b, e := NewOperationBudget(limits)
					if e != nil {
						t.Fatal(e)
					}
					s := swappedLinkScanner{ctx: context.Background(), budget: b, source: NewSource([]byte("(x)[https://x]"))}
					var valid bool
					if stage == "url" {
						valid, e = s.validURL(md011Units("https://x"))
					} else {
						s.units = md011Units("(x)[https://x]")
						e = s.flush()
						valid = len(s.spans) == 1
					}
					if delta == 0 {
						if e != nil || !valid {
							t.Fatal(valid, e)
						}
						return
					}
					var stop *LimitExceeded
					// Private helpers may retain provisional spans before a terminal
					// charge fails; the public producer owns atomic publication.
					if !errors.As(e, &stop) {
						t.Fatal(valid, e)
					}
					if e = s.charge(0, 0); !errors.As(e, &stop) {
						t.Fatal("budget not sticky", e)
					}
				})
			}
		}
	}
}

func TestSwappedLinksManyCandidatesAndInvalidPrefixesWithinSourceBound(t *testing.T) {
	const count = 6000
	for _, gfm := range []bool{false, true} {
		for _, invalid := range []bool{false, true} {
			t.Run(fmt.Sprintf("gfm-%v/invalid-%v", gfm, invalid), func(t *testing.T) {
				md011Positive(t, gfm)
				unit := "(x)[https://x]\n\n"
				if invalid {
					unit = "(x)[relative]\n\n"
				}
				source := NewSource([]byte(strings.Repeat(unit, count)))
				// This shallow source has <=16 bytes,2 lines,2 nodes per unit.
				// Original parsing/index:128B+256L+256(nodes+1)+256 <=4096/unit.
				// Text semantics: <=17*256 +24+16+17=4409; geometric unit
				// storage adds <9*16=144W. URL <=2*9+64*9+256*9+512=3410.
				// Traversal (4 visits), groups/flush/raw/final and geometric frames
				// fit2048/unit. Detached sorted spans are already monotonic: typed
				// pdqsort increasing hint <=count+11 comparisons, each4W. 16384/unit
				// plus65536 fixed overhead therefore bounds the entire original operation.
				// A restart at every prefix charging even8W/node would exceed this ceiling
				// at6000 units:8*sum(1..count). No used/remaining budget counters are read.
				ceiling := int64(16384*count + 65536)
				if ceiling >= int64(8*count*(count+1)/2) {
					t.Fatal("ceiling no longer excludes quadratic prefix scans")
				}
				limits := DefaultLimits()
				limits.WorkUnits = ceiling
				limits.TotalNodes = 1024*count + 65536
				limits.DocumentNodes = limits.TotalNodes
				limits.OperationTime = 5 * time.Minute
				b, e := NewOperationBudget(limits)
				if e != nil {
					t.Fatal(e)
				}
				o := ParseOptions{GFM: gfm}
				p := ParseWithOptions(context.Background(), source, b, o)
				if p.Status != Complete || p.Document == nil {
					t.Fatal(p)
				}
				if len(p.Document.root.spec.Children) != count {
					t.Fatal("original independent paragraphs", len(p.Document.root.spec.Children))
				}
				var index LineIndex
				e = nestedExampleLocked(context.Background(), b, LineIndex{}, func(w *parserContext, _ LineIndex) error { index = newLineIndex(w, source); return nil })
				if e != nil {
					t.Fatal(e)
				}
				got, e := p.Document.SwappedLinks(context.Background(), b, index, o)
				want := count
				if invalid {
					want = 0
				}
				if e != nil || got.Availability != Available || len(got.Spans) != want {
					t.Fatal(got.Availability, len(got.Spans), e)
				}
				for j, s := range got.Spans {
					if s.start != j*len(unit) || s.end != j*len(unit)+14 || s.SourceToken() != source.Token() {
						t.Fatal(j, s)
					}
				}
			})
		}
	}
}

func TestSwappedLinksStaticEmptyOriginalReservations(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, delta := range []int64{-1, 0} {
			t.Run(fmt.Sprintf("gfm-%v/delta-%d", gfm, delta), func(t *testing.T) {
				s := NewSource(nil)
				b, _ := NewOperationBudget(DefaultLimits())
				options := ParseOptions{GFM: gfm}
				p := ParseWithOptions(context.Background(), s, b, options)
				if p.Status != Complete {
					t.Fatal(p)
				}
				setup := int64(25)
				if gfm {
					setup = 30
				}
				// Independently frozen: original empty parse 25/30W, producer 3W.
				if err := b.ChargeWork(b.Limits().WorkUnits - setup - 3 - delta); err != nil {
					t.Fatal(err)
				}
				got, err := p.Document.SwappedLinks(context.Background(), b, NewLineIndex(s), options)
				if delta == 0 {
					if err != nil || got.Availability != Available || len(got.Spans) != 0 {
						t.Fatal(got, err)
					}
					return
				}
				for range 2 {
					var stop *LimitExceeded
					if !errors.As(err, &stop) || got.Spans != nil {
						t.Fatal(got, err)
					}
					got, err = p.Document.SwappedLinks(context.Background(), b, NewLineIndex(s), options)
				}
			})
		}
	}
}

func TestSwappedLinksOriginalPrivateAuthority(t *testing.T) {
	d, b, index := md014Parse(t, "(Guide)[https://example.org]")
	pre, err := d.SwappedLinks(context.Background(), b, index, ParseOptions{})
	if err != nil || len(pre.Spans) != 1 || pre.Spans[0].start != 0 || pre.Spans[0].end != 28 {
		t.Fatal("actual positive prerequisite", pre, err)
	}
	public, _ := NewDocument(d.Root(), d.References())
	foreign, _ := NewOperationBudget(DefaultLimits())
	for _, tc := range []struct {
		d *Document
		b *OperationBudget
		i LineIndex
		o ParseOptions
	}{
		{nil, b, index, ParseOptions{}}, {public, b, index, ParseOptions{}}, {d, nil, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}}, {d, b, NewLineIndex(NewSource([]byte("(Guide)[https://example.org]"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}},
	} {
		got, err := tc.d.SwappedLinks(context.Background(), tc.b, tc.i, tc.o)
		if !errors.Is(err, ErrModel) || got.Spans != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Deliberately absent caller.
	if got, err := d.SwappedLinks(nil, b, index, ParseOptions{}); !errors.Is(err, ErrModel) || got.Spans != nil {
		t.Fatal(got, err)
	}
	for _, profile := range []parserProfile{tablesOnlyProfile, tasksOnlyProfile, strikeOnlyProfile, bareOnlyProfile, filterOnlyProfile} {
		for _, fullFlags := range []bool{false, true} {
			t.Run(fmt.Sprintf("profile-%d/full-flags-%v", profile, fullFlags), func(t *testing.T) {
				s := NewSource([]byte("(Guide)[https://example.org]"))
				budget, _ := NewOperationBudget(DefaultLimits())
				p := parseWithProfile(context.Background(), s, budget, profile)
				if p.Status != Complete {
					t.Fatal(p)
				}
				if fullFlags {
					p.Document.parsedOptions.GFM = true
				}
				got, err := p.Document.SwappedLinks(context.Background(), budget, NewLineIndex(s), p.Document.parsedOptions)
				if !errors.Is(err, ErrModel) || got.Spans != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
	d.parsedComplete = false
	if got, err := d.SwappedLinks(context.Background(), b, index, ParseOptions{}); !errors.Is(err, ErrModel) || got.Spans != nil {
		t.Fatal(got, err)
	}
	source := NewSource([]byte("(Guide)[https://example.org]"))
	budget, _ := NewOperationBudget(DefaultLimits())
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	partial := ParseWithOptions(cancelled, source, budget, ParseOptions{})
	if partial.Status == Complete {
		t.Fatal("genuine interrupted parse completed")
	}
	if got, err := partial.Document.SwappedLinks(context.Background(), budget, NewLineIndex(source), ParseOptions{}); !errors.Is(err, ErrModel) || got.Spans != nil {
		t.Fatal(got, err)
	}
}

func TestSwappedLinksStaticTextReservation(t *testing.T) {
	md011Positive(t, false)
	for _, resource := range []string{"work", "nodes"} {
		for _, delta := range []int64{-1, 0} {
			t.Run(fmt.Sprintf("%s/delta-%d", resource, delta), func(t *testing.T) {
				// Construct exactly one source-backed Exact segment for private accounting.
				// This does not assume the original parser uses one segment per Text.
				// 16+24+15+256*15+(2*14+3*16+4-1)+1 =3975W;14+2*16-1=45N.
				d, _, index, _ := md011Parse(t, "(x)[https://x]", false)
				nodes := md070Nodes(d, Text)
				if len(nodes) != 1 {
					t.Fatal(nodes)
				}
				node := *nodes[0]
				part := node.spec.Content[0]
				part.Source, _ = index.source.Span(0, 14)
				part.Semantic = Optional[string]{Present: true, Value: "(x)[https://x]"}
				part.Transformation = Exact
				node.spec.Content = []Segment{part}
				limits := DefaultLimits()
				if resource == "work" {
					limits.WorkUnits = 3975 + delta
				} else {
					limits.TotalNodes = 45 + delta
					limits.DocumentNodes = limits.TotalNodes
				}
				b, e := NewOperationBudget(limits)
				if e != nil {
					t.Fatal(e)
				}
				s := swappedLinkScanner{ctx: context.Background(), budget: b, source: index.source}
				known, e := s.text(&node)
				if delta == 0 {
					if e != nil || !known || len(s.units) != 14 {
						t.Fatal(known, e, len(s.units))
					}
					return
				}
				var stop *LimitExceeded
				// text can finish semantic recognition before its terminal charge
				// fails. Operational error, not the provisional boolean, governs.
				if !errors.As(e, &stop) {
					t.Fatal(known, e)
				}
				if e = s.charge(0, 0); !errors.As(e, &stop) {
					t.Fatal("budget not sticky", e)
				}
			})
		}
	}
}

func TestSwappedLinksUnmatchedPrefixWithinSourceBound(t *testing.T) {
	const count = 6000
	for _, gfm := range []bool{false, true} {
		t.Run(fmt.Sprintf("gfm-%v", gfm), func(t *testing.T) {
			md011Positive(t, gfm)
			text := strings.Repeat("(", count) + "x)[relative]"
			source := NewSource([]byte(text))
			limits := DefaultLimits()
			// <=128B original parse/index +256(B+1) Text scalar scan +9B
			// geometric units +8(B+1) group walk; 1024B+65536 covers all fixed
			// ownership/segment/terminal work for this single shallow paragraph.
			// The same group cannot restart at each unmatched opener: the bound is
			// below8*sum(1..count) even before counting any URL or parse cost.
			limits.WorkUnits = 1024*int64(len(text)) + 65536
			if limits.WorkUnits >= 8*count*(count+1)/2 {
				t.Fatal("nonlinear allowance")
			}
			b, e := NewOperationBudget(limits)
			if e != nil {
				t.Fatal(e)
			}
			o := ParseOptions{GFM: gfm}
			p := ParseWithOptions(context.Background(), source, b, o)
			if p.Status != Complete || p.Document == nil {
				t.Fatal(p)
			}
			var index LineIndex
			e = nestedExampleLocked(context.Background(), b, LineIndex{}, func(w *parserContext, _ LineIndex) error { index = newLineIndex(w, source); return nil })
			if e != nil {
				t.Fatal(e)
			}
			got, e := p.Document.SwappedLinks(context.Background(), b, index, o)
			if e != nil || got.Availability != Available || len(got.Spans) != 0 {
				t.Fatal(got, e)
			}
		})
	}
}
