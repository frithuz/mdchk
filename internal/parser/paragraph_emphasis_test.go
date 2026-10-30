package parser

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestParagraphEmphasisFrozenCountsAndGeometry(t *testing.T) {
	data, e := os.ReadFile("../rules/testdata/md081.json")
	if e != nil {
		t.Fatal(e)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != "501e217a9bb3657c605c47964b988ffdcb8cf50e916f5d721af8e9da58410ed5" {
		t.Fatal("oracle digest")
	}
	var oracle struct {
		Cases []struct {
			ID, Source  string
			Flavors     []string
			Frontmatter bool
			Witnesses   []struct {
				Content           string `json:"source_content"`
				Bytes, Start, End [2]int
				Total, Emphasized int
				Eligible          string `json:"eligible_scalar_witness"`
				Emphasis          string `json:"emphasized_scalar_witness"`
			} `json:"paragraph_witnesses"`
		}
	}
	if e = json.Unmarshal(data, &oracle); e != nil {
		t.Fatal(e)
	}
	witnesses := 0
	for _, f := range oracle.Cases {
		witnesses += len(f.Witnesses)
		for _, flavor := range f.Flavors {
			t.Run(f.ID+"/"+flavor, func(t *testing.T) {
				source := NewSource([]byte(f.Source))
				b, _ := NewOperationBudget(DefaultLimits())
				fm, _ := NewFrontmatterOptions(f.Frontmatter, nil)
				options := ParseOptions{GFM: flavor == "gfm", Frontmatter: fm}
				p := ParseWithOptions(context.Background(), source, b, options)
				if p.Status != Complete {
					t.Fatal(p)
				}
				index := NewLineIndex(source)
				got, e := p.Document.ParagraphEmphasis(context.Background(), b, index, options)
				if e != nil || len(got) != len(f.Witnesses) {
					t.Fatal(got, e, f.Witnesses)
				}
				for i, w := range f.Witnesses {
					g := got[i]
					text, e := source.Text(g.Content)
					start, se := index.Position(g.Content.Start())
					end, ee := index.Position(g.Content.End())
					if e != nil || se != nil || ee != nil || text != w.Content || [2]int{g.Content.Start(), g.Content.End()} != w.Bytes || start != (Position{w.Start[0], w.Start[1]}) || end != (Position{w.End[0], w.End[1]}) || g.Total != w.Total || g.Emphasized != w.Emphasized || len([]rune(w.Eligible)) != w.Total || len([]rune(w.Emphasis)) != w.Emphasized {
						t.Fatal(g, w, e, se, ee)
					}
				}
				if string(source.Bytes()) != f.Source {
					t.Fatal("source changed")
				}
			})
		}
	}
	if len(oracle.Cases) != 107 || witnesses != 108 {
		t.Fatal(len(oracle.Cases), witnesses)
	}
}

// All values are derived from paragraph_emphasis.go's static charge sites.
// No tested projection phase is measured to establish these expectations.
var paragraphEmphasisCosts = []struct {
	name, source string
	facts        int
	work, nodes  int64
	calls        int
}{
	{"empty", "", 0, 47, 3, 17},
	{"clean", "clean", 1, 3249, 15, 68},
	{"protected", "```\nx\n```", 0, 84, 7, 33},
	{"code-only", "`ab`", 1, 141, 15, 54},
	{"violation", "**ab** c", 1, 3348, 27, 101},
	{"equality", "**ab** cd", 1, 3862, 27, 103},
	{"overlap", "***ab*** c", 1, 3383, 29, 116},
	{"growth", "**a**\n\n**b**", 2, 2410, 34, 132},
}

func paragraphEmphasisParse(t *testing.T, source string, ctx context.Context) (*Document, *OperationBudget, LineIndex) {
	t.Helper()
	s := NewSource([]byte(source))
	b, _ := NewOperationBudget(DefaultLimits())
	p := ParseWithOptions(ctx, s, b, ParseOptions{})
	if p.Status != Complete {
		t.Fatal(p)
	}
	return p.Document, b, NewLineIndex(s)
}

func TestParagraphEmphasisStaticReservations(t *testing.T) {
	for _, tc := range paragraphEmphasisCosts {
		for _, kind := range []string{"work", "nodes"} {
			for _, delta := range []int64{-1, 0} {
				t.Run(fmt.Sprintf("%s/%s/%d", tc.name, kind, delta), func(t *testing.T) {
					d, b, index := paragraphEmphasisParse(t, tc.source, context.Background())
					if kind == "work" {
						b.limits.WorkUnits = b.work + tc.work + delta
					} else {
						b.limits.TotalNodes = b.nodes + tc.nodes + delta
						b.limits.DocumentNodes = b.limits.TotalNodes
					}
					got, e := d.ParagraphEmphasis(context.Background(), b, index, ParseOptions{})
					if delta == 0 {
						if e != nil || len(got) != tc.facts {
							t.Fatal(got, e)
						}
					} else {
						for range 2 {
							var limit *LimitExceeded
							if !errors.As(e, &limit) || got != nil {
								t.Fatal(got, e)
							}
							got, e = d.ParagraphEmphasis(context.Background(), b, index, ParseOptions{})
						}
					}
				})
			}
		}
	}
}

type paragraphEmphasisStop struct {
	context.Context
	calls, stop int
	reason      error
}

func (c *paragraphEmphasisStop) Err() error {
	c.calls++
	if c.calls >= c.stop {
		return c.reason
	}
	return nil
}
func TestParagraphEmphasisFinalPublication(t *testing.T) {
	for _, tc := range paragraphEmphasisCosts {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%v/%v", tc.name, reason, originating), func(t *testing.T) {
					origin := &paragraphEmphasisStop{Context: context.Background(), stop: 1 << 30, reason: reason}
					d, b, index := paragraphEmphasisParse(t, tc.source, origin)
					caller := &paragraphEmphasisStop{Context: context.Background(), stop: tc.calls, reason: reason}
					var ctx context.Context = caller
					if originating {
						origin.stop = origin.calls + tc.calls
						ctx = blockquoteOperation{Context: context.Background(), origin: origin}
					}
					got, e := d.ParagraphEmphasis(ctx, b, index, ParseOptions{})
					if !errors.Is(e, reason) || got != nil {
						t.Fatal(got, e)
					}
					if !originating && caller.calls != tc.calls {
						t.Fatal(caller.calls, tc.calls)
					}
				})
			}
		}
	}
}
func TestParagraphEmphasisAuthorityAndMalformedAssociations(t *testing.T) {
	d, b, index := paragraphEmphasisParse(t, "**ab** c", context.Background())
	constructed, _ := NewDocument(d.Root(), d.References())
	foreign, _ := NewOperationBudget(DefaultLimits())
	for _, tc := range []struct {
		d *Document
		b *OperationBudget
		i LineIndex
		o ParseOptions
	}{{nil, b, index, ParseOptions{}}, {constructed, b, index, ParseOptions{}}, {d, nil, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}}, {d, b, NewLineIndex(NewSource([]byte("**ab** c"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}}} {
		got, e := tc.d.ParagraphEmphasis(context.Background(), tc.b, tc.i, tc.o)
		if !errors.Is(e, ErrModel) || got != nil {
			t.Fatal(got, e)
		}
	}
	//nolint:staticcheck // Deliberate missing caller authority.
	if got, e := d.ParagraphEmphasis(nil, b, index, ParseOptions{}); !errors.Is(e, ErrModel) || got != nil {
		t.Fatal(got, e)
	}
	for _, breakModel := range []func(*Document){
		func(d *Document) { d.parsedComplete = false }, func(d *Document) { d.parsedProfile = parserProfile(255) },
		func(d *Document) { d.root.spec.Children[0].spec.Content[0].Source = Span{} },
		func(d *Document) {
			n := d.root.spec.Children[0].spec.Children[0].spec.Children[0]
			n.spec.Content[0].Semantic = Some("zz")
		},
		func(d *Document) {
			n := d.root.spec.Children[0].spec.Children[1]
			n.spec.Content[0].Transformation = Synthetic
		},
		func(d *Document) {
			n := d.root.spec.Children[0].spec.Children[1]
			n.spec.Content = append(n.spec.Content, n.spec.Content[0])
		},
	} {
		d, b, index = paragraphEmphasisParse(t, "**ab** c", context.Background())
		breakModel(d)
		got, e := d.ParagraphEmphasis(context.Background(), b, index, ParseOptions{})
		if !errors.Is(e, ErrModel) || got != nil {
			t.Fatal(got, e)
		}
	}
}
func TestParagraphEmphasisStickyAndDetachedConcurrency(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				d, b, index := paragraphEmphasisParse(t, "**ab** c", context.Background())
				switch kind {
				case "work":
					_ = b.ChargeWork(b.Limits().WorkUnits)
				case "nodes":
					_ = b.ChargeNodes(b.Limits().TotalNodes)
				case "elapsed":
					time.Sleep(61 * time.Second)
				}
				for range 2 {
					got, e := d.ParagraphEmphasis(context.Background(), b, index, ParseOptions{})
					var stop *LimitExceeded
					if !errors.As(e, &stop) || got != nil {
						t.Fatal(got, e)
					}
				}
			})
		})
	}
	d, b, index := paragraphEmphasisParse(t, "**ab** c", context.Background())
	want, e := d.ParagraphEmphasis(context.Background(), b, index, ParseOptions{})
	if e != nil {
		t.Fatal(e)
	}
	saved := append([]ParagraphEmphasisFact(nil), want...)
	want[0] = ParagraphEmphasisFact{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			got, e := d.ParagraphEmphasis(context.Background(), b, index, ParseOptions{})
			if e != nil || !reflect.DeepEqual(got, saved) {
				t.Error(got, e)
			}
		})
	}
	wg.Wait()
}

func TestParagraphEmphasisLateMalformedOutputAndEmptyTasks(t *testing.T) {
	d, b, index := paragraphEmphasisParse(t, "**ab** c\n\n**cd** e", context.Background())
	n := d.root.spec.Children[1].spec.Children[1]
	n.spec.Content[0].Semantic = Some("zz")
	got, e := d.ParagraphEmphasis(context.Background(), b, index, ParseOptions{})
	if !errors.Is(e, ErrModel) || got != nil {
		t.Fatal("prior paragraph must be withheld", got, e)
	}
	source := NewSource([]byte("- [x] "))
	b, _ = NewOperationBudget(DefaultLimits())
	p := ParseWithOptions(context.Background(), source, b, ParseOptions{GFM: true})
	if p.Status != Complete {
		t.Fatal(p)
	}
	got, e = p.Document.ParagraphEmphasis(context.Background(), b, NewLineIndex(source), ParseOptions{GFM: true})
	if e != nil || len(got) != 1 || got[0].Content.Present() || got[0].Total != 0 || got[0].Emphasized != 0 {
		t.Fatal(got, e)
	}
}
