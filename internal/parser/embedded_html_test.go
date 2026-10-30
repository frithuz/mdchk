package parser

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestEmbeddedHTMLFrozenGeometry(t *testing.T) {
	data, e := os.ReadFile("../rules/testdata/md033.json")
	if e != nil {
		t.Fatal(e)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != "4cc912cbc98368c037ecb3dc8d307be76be21affef69d8897b026c038dc70e37" {
		t.Fatal("oracle identity")
	}
	var oracle struct {
		Cases []struct {
			ID, Source  string
			Flavors     []string
			Frontmatter bool
			Complete    bool `json:"analysis_complete"`
			Candidates  []struct {
				Bytes, Start, End [2]int
				Text, Name        string
			}
		}
	}
	if e = json.Unmarshal(data, &oracle); e != nil {
		t.Fatal(e)
	}
	for _, f := range oracle.Cases {
		for _, flavor := range f.Flavors {
			t.Run(f.ID+"/"+flavor, func(t *testing.T) {
				s := NewSource([]byte(f.Source))
				b, _ := NewOperationBudget(DefaultLimits())
				fm, _ := NewFrontmatterOptions(f.Frontmatter, nil)
				o := ParseOptions{GFM: flavor == "gfm", Frontmatter: fm}
				p := ParseWithOptions(context.Background(), s, b, o)
				index := NewLineIndex(s)
				got, e := p.Document.EmbeddedHTMLTags(context.Background(), b, index, o)
				if p.Status != Complete {
					if f.Complete || !errors.Is(e, ErrModel) || got.Tags != nil {
						t.Fatal(p, got, e)
					}
					return
				}
				if e != nil {
					t.Fatal(got, e)
				}
				want := Available
				if !f.Complete {
					want = Unavailable
				}
				if got.Availability != want || len(got.Tags) != len(f.Candidates) {
					t.Fatal(got, f.Candidates)
				}
				for i, w := range f.Candidates {
					g := got.Tags[i]
					text, te := s.Text(g.Tag)
					name, ne := s.Text(g.Name)
					a, ae := index.Position(g.Tag.Start())
					z, ze := index.Position(g.Tag.End())
					if te != nil || ne != nil || ae != nil || ze != nil || text != w.Text || name != w.Name || [2]int{g.Tag.Start(), g.Tag.End()} != w.Bytes || a != (Position{w.Start[0], w.Start[1]}) || z != (Position{w.End[0], w.End[1]}) {
						t.Fatal(g, w, te, ne, ae, ze)
					}
				}
				if string(s.Bytes()) != f.Source {
					t.Fatal("source changed")
				}
			})
		}
	}
}

func embeddedHTMLParse(t *testing.T, source string) (*Document, *OperationBudget, LineIndex) {
	t.Helper()
	s := NewSource([]byte(source))
	b, _ := NewOperationBudget(DefaultLimits())
	p := ParseWithOptions(context.Background(), s, b, ParseOptions{})
	if p.Status != Complete {
		t.Fatal(p)
	}
	return p.Document, b, NewLineIndex(s)
}

func TestEmbeddedHTMLOriginalAuthority(t *testing.T) {
	d, b, index := embeddedHTMLParse(t, "a <b>x</b> z")
	constructed, _ := NewDocument(d.Root(), d.References())
	foreign, _ := NewOperationBudget(DefaultLimits())
	for _, tc := range []struct {
		d *Document
		b *OperationBudget
		i LineIndex
		o ParseOptions
	}{{nil, b, index, ParseOptions{}}, {constructed, b, index, ParseOptions{}}, {d, nil, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}}, {d, b, NewLineIndex(NewSource([]byte("a <b>x</b> z"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}}} {
		got, e := tc.d.EmbeddedHTMLTags(context.Background(), tc.b, tc.i, tc.o)
		if !errors.Is(e, ErrModel) || got.Tags != nil {
			t.Fatal(got, e)
		}
	}
	//nolint:staticcheck // Deliberately missing caller authority.
	if got, e := d.EmbeddedHTMLTags(nil, b, index, ParseOptions{}); !errors.Is(e, ErrModel) || got.Tags != nil {
		t.Fatal(got, e)
	}
	// Exercise real privately parsed documents for every partial profile, rather
	// than only mutating the public result or forging the stored profile field.
	for _, profile := range []parserProfile{tablesOnlyProfile, tasksOnlyProfile, strikeOnlyProfile, bareOnlyProfile, filterOnlyProfile} {
		s := NewSource([]byte("a <b>x</b> z"))
		budget, _ := NewOperationBudget(DefaultLimits())
		p := parseWithProfile(context.Background(), s, budget, profile)
		if p.Status != Complete {
			t.Fatal(p)
		}
		got, e := p.Document.EmbeddedHTMLTags(context.Background(), budget, NewLineIndex(s), p.Document.parsedOptions)
		if !errors.Is(e, ErrModel) || got.Tags != nil {
			t.Fatal(profile, got, e)
		}
	}
	d.parsedComplete = false
	if got, e := d.EmbeddedHTMLTags(context.Background(), b, index, ParseOptions{}); !errors.Is(e, ErrModel) || got.Tags != nil {
		t.Fatal(got, e)
	}
}

func TestEmbeddedHTMLLateAssociations(t *testing.T) {
	for _, kind := range []string{"absent", "foreign", "semantic", "transformation", "order"} {
		t.Run(kind, func(t *testing.T) {
			d, b, index := embeddedHTMLParse(t, "<b>\n\n<i>")
			before, e := d.EmbeddedHTMLTags(context.Background(), b, index, ParseOptions{})
			if e != nil || len(before.Tags) != 2 || before.Tags[0].Tag.Start() != 0 {
				t.Fatal(before, e)
			}
			n := d.root.spec.Children[1]
			switch kind {
			case "absent":
				n.spec.Content[0].Source = Span{}
			case "foreign":
				n.spec.Content[0].Source, _ = NewSource([]byte("<i>")).Span(0, 3)
			case "semantic":
				n.spec.Content[0].Semantic = Some("<b>")
			case "transformation":
				n.spec.Content[0].Transformation = Synthetic
			case "order":
				n.spec.Content = append(n.spec.Content, n.spec.Content[0])
			}
			got, e := d.EmbeddedHTMLTags(context.Background(), b, index, ParseOptions{})
			if !errors.Is(e, ErrModel) || got.Tags != nil {
				t.Fatal(got, e)
			}
		})
	}
}

func TestEmbeddedHTMLStickyDetachedConcurrency(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				d, b, index := embeddedHTMLParse(t, "<b>")
				switch kind {
				case "work":
					_ = b.ChargeWork(b.Limits().WorkUnits)
				case "nodes":
					_ = b.ChargeNodes(b.Limits().TotalNodes)
				case "elapsed":
					time.Sleep(61 * time.Second)
				}
				for range 2 {
					got, e := d.EmbeddedHTMLTags(context.Background(), b, index, ParseOptions{})
					var stop *LimitExceeded
					if !errors.As(e, &stop) || got.Tags != nil {
						t.Fatal(got, e)
					}
				}
			})
		})
	}
	d, b, index := embeddedHTMLParse(t, "a <b>x</b> z")
	got, e := d.EmbeddedHTMLTags(context.Background(), b, index, ParseOptions{})
	if e != nil || len(got.Tags) != 2 {
		t.Fatal(got, e)
	}
	saved := append([]EmbeddedHTMLTag(nil), got.Tags...)
	got.Tags[0] = EmbeddedHTMLTag{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			got, e := d.EmbeddedHTMLTags(context.Background(), b, index, ParseOptions{})
			if e != nil || !reflect.DeepEqual(got.Tags, saved) {
				t.Error(got, e)
			}
		})
	}
	wg.Wait()
}

func TestEmbeddedHTMLGlobalRawOpaqueProof(t *testing.T) {
	for _, source := range []string{
		"a <script>\n\n<!-- </script> -->\n\n<b>\n\n</script><i>",
		"a <script>\n\n<?p </script> ?>\n\n<b>\n\n</script><i>",
		"a <script>\n\n<![CDATA[</script>]]>\n\n<b>\n\n</script><i>",
		"a <script> <x title='</script>'> <b> </script><i>",
	} {
		t.Run(source, func(t *testing.T) {
			d, b, index := embeddedHTMLParse(t, source)
			got, e := d.EmbeddedHTMLTags(context.Background(), b, index, ParseOptions{})
			if e != nil || got.Availability != Available || len(got.Tags) != 3 {
				t.Fatal(got, e)
			}
			for i, want := range []string{"<script>", "</script>", "<i>"} {
				text, e := d.Source().Text(got.Tags[i].Tag)
				if e != nil || text != want {
					t.Fatal(got, e)
				}
			}
		})
	}
}

func TestEmbeddedHTMLFailedRawCandidateFamily(t *testing.T) {
	for _, count := range []int{1, 8, 64, 512} {
		source := "a <script>\n\n<div x='" + strings.Repeat("<x a='", count) + "unfinished"
		d, b, index := embeddedHTMLParse(t, source)
		got, e := d.EmbeddedHTMLTags(context.Background(), b, index, ParseOptions{})
		if e != nil || got.Availability != Available || len(got.Tags) != 1 {
			t.Fatal(count, got, e)
		}
		text, _ := d.Source().Text(got.Tags[0].Tag)
		if text != "<script>" {
			t.Fatal(text)
		}
	}
}

// Frozen source-derived residuals; never measured from the tested phase.
var embeddedHTMLRows = []struct {
	name, source string
	work, nodes  int64
	facts, final int
	available    Availability
}{
	{"empty", "", 32, 2, 0, 5, Available},
	{"clean", "clean", 82, 4, 0, 7, Available},
	{"code", "`ab`", 82, 4, 0, 7, Available},
	{"fenced", "```\nx\n```", 57, 3, 0, 6, Available},
	{"comment", "<!--x-->", 202, 4, 0, 7, Available},
	{"one", "<b>", 406, 7, 1, 10, Available},
	{"two", "<b></b>", 685, 15, 2, 15, Available},
	{"raw", "<pre><b>x</b></pre>", 1071, 10, 2, 19, Available},
	{"unavailable", "<div\n", 199, 5, 0, 8, Unavailable},
}

func TestEmbeddedHTMLStaticReservations(t *testing.T) {
	for _, tc := range embeddedHTMLRows {
		for _, nodes := range []bool{false, true} {
			for _, delta := range []int64{-1, 0} {
				t.Run(fmt.Sprintf("%s/nodes-%v/%d", tc.name, nodes, delta), func(t *testing.T) {
					// Sacrifice only completed parsing setup, before the tested projection.
					_, prefix, _ := embeddedHTMLParse(t, tc.source)
					charge, limit := prefix.ChargeWork, prefix.Limits().WorkUnits
					exact := tc.work
					if nodes {
						charge, limit = prefix.ChargeNodes, prefix.Limits().TotalNodes
						exact = tc.nodes
					}
					var stop *LimitExceeded
					if !errors.As(charge(limit), &stop) {
						t.Fatal("setup qualification did not exhaust")
					}
					used := stop.Observed - limit
					d, b, index := embeddedHTMLParse(t, tc.source)
					charge = b.ChargeWork
					if nodes {
						charge = b.ChargeNodes
					}
					if e := charge(limit - used - exact - delta); e != nil {
						t.Fatal(e)
					}
					got, e := d.EmbeddedHTMLTags(context.Background(), b, index, ParseOptions{})
					if delta == 0 {
						if e != nil || len(got.Tags) != tc.facts || got.Availability != tc.available {
							t.Fatal(got, e)
						}
					} else {
						for range 2 {
							if !errors.As(e, &stop) || got.Tags != nil {
								t.Fatal(got, e)
							}
							got, e = d.EmbeddedHTMLTags(context.Background(), b, index, ParseOptions{})
						}
					}
				})
			}
		}
	}
}

func TestEmbeddedHTMLFinalPublication(t *testing.T) {
	for _, tc := range embeddedHTMLRows {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(fmt.Sprintf("%s/%v", tc.name, reason), func(t *testing.T) {
				d, b, index := embeddedHTMLParse(t, tc.source)
				caller := &paragraphEmphasisStop{Context: context.Background(), stop: tc.final, reason: reason}
				got, e := d.EmbeddedHTMLTags(caller, b, index, ParseOptions{})
				if !errors.Is(e, reason) || got.Tags != nil {
					t.Fatal(got, e)
				}
			})
		}
	}
}
