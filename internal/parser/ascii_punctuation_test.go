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

func TestASCIIPunctuationFrozenGeometry(t *testing.T) {
	data, e := os.ReadFile("../rules/testdata/md088.json")
	if e != nil {
		t.Fatal(e)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != "6ef31b6c678d10d662ea1da66c3f65cad236223adf2c5787b37bddba5f533060" {
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
				Text, Message     string
			}
		}
	}
	if e = json.Unmarshal(data, &oracle); e != nil {
		t.Fatal(e)
	}
	for _, f := range oracle.Cases {
		for _, flavor := range f.Flavors {
			t.Run(f.ID+"/"+flavor, func(t *testing.T) {
				source := NewSource([]byte(f.Source))
				b, _ := NewOperationBudget(DefaultLimits())
				fm, _ := NewFrontmatterOptions(f.Frontmatter, nil)
				options := ParseOptions{GFM: flavor == "gfm", Frontmatter: fm}
				p := ParseWithOptions(context.Background(), source, b, options)
				index := NewLineIndex(source)
				if !f.Complete {
					if p.Status != Incomplete {
						t.Fatal(p)
					}
					got, e := p.Document.ASCIIPunctuation(context.Background(), b, index, options)
					if !errors.Is(e, ErrModel) || got != nil {
						t.Fatal(got, e)
					}
					return
				}
				if p.Status != Complete {
					t.Fatal(p)
				}
				got, e := p.Document.ASCIIPunctuation(context.Background(), b, index, options)
				if e != nil || len(got) != len(f.Candidates) {
					t.Fatal(got, e, f.Candidates)
				}
				for i, w := range f.Candidates {
					g := got[i]
					text, e := source.Text(g.Span)
					start, se := index.Position(g.Span.Start())
					end, ee := index.Position(g.Span.End())
					r := []rune(w.Text)
					if e != nil || se != nil || ee != nil || text != w.Text || [2]int{g.Span.Start(), g.Span.End()} != w.Bytes || start != (Position{w.Start[0], w.Start[1]}) || end != (Position{w.End[0], w.End[1]}) || len(r) != 1 || g.Scalar != r[0] {
						t.Fatal(g, w, e, se, ee)
					}
				}
				if string(source.Bytes()) != f.Source {
					t.Fatal("source changed")
				}
			})
		}
	}
}

// Static charge-site totals; no tested phase is measured to derive these rows.
var asciiPunctuationCosts = []struct {
	name, source string
	facts        int
	work, nodes  int64
	calls        int
}{
	{"empty", "", 0, 33, 2, 12},
	{"clean", "clean", 0, 715, 4, 50},
	{"protected", "```\nx\n```", 0, 62, 3, 23},
	{"code-only", "`ab`", 0, 91, 4, 34},
	{"one-offender", "—", 1, 330, 6, 47},
	{"six-class", "‘’“”–—", 6, 860, 25, 80},
	{"nested", "***—***", 1, 388, 8, 69},
	{"raw", "<script>\n—\n</script>", 0, 74, 3, 23},
	{"mixed-transformed", "a&amp;—", 1, 560, 6, 54},
	{"growth", "—\n\n—", 2, 630, 13, 83},
}

func asciiPunctuationParse(t *testing.T, source string, ctx context.Context) (*Document, *OperationBudget, LineIndex) {
	t.Helper()
	s := NewSource([]byte(source))
	b, _ := NewOperationBudget(DefaultLimits())
	p := ParseWithOptions(ctx, s, b, ParseOptions{})
	if p.Status != Complete {
		t.Fatal(p)
	}
	return p.Document, b, NewLineIndex(s)
}
func TestASCIIPunctuationStaticReservations(t *testing.T) {
	for _, tc := range asciiPunctuationCosts {
		for _, kind := range []string{"work", "nodes"} {
			for _, delta := range []int64{-1, 0} {
				t.Run(fmt.Sprintf("%s/%s/%d", tc.name, kind, delta), func(t *testing.T) {
					d, b, index := asciiPunctuationParse(t, tc.source, context.Background())
					if kind == "work" {
						b.limits.WorkUnits = b.work + tc.work + delta
					} else {
						b.limits.TotalNodes = b.nodes + tc.nodes + delta
						b.limits.DocumentNodes = b.limits.TotalNodes
					}
					got, e := d.ASCIIPunctuation(context.Background(), b, index, ParseOptions{})
					if delta == 0 {
						if e != nil || len(got) != tc.facts {
							t.Fatal(got, e)
						}
					} else {
						for range 2 {
							var stop *LimitExceeded
							if !errors.As(e, &stop) || got != nil {
								t.Fatal(got, e)
							}
							got, e = d.ASCIIPunctuation(context.Background(), b, index, ParseOptions{})
						}
					}
				})
			}
		}
	}
}
func TestASCIIPunctuationFinalPublication(t *testing.T) {
	for _, tc := range asciiPunctuationCosts {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%v/%v", tc.name, reason, originating), func(t *testing.T) {
					origin := &paragraphEmphasisStop{Context: context.Background(), stop: 1 << 30, reason: reason}
					d, b, index := asciiPunctuationParse(t, tc.source, origin)
					caller := &paragraphEmphasisStop{Context: context.Background(), stop: tc.calls, reason: reason}
					var ctx context.Context = caller
					if originating {
						origin.stop = origin.calls + tc.calls
						ctx = blockquoteOperation{Context: context.Background(), origin: origin}
					}
					got, e := d.ASCIIPunctuation(ctx, b, index, ParseOptions{})
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
func TestASCIIPunctuationAuthorityAndLateAssociations(t *testing.T) {
	d, b, index := asciiPunctuationParse(t, "—", context.Background())
	constructed, _ := NewDocument(d.Root(), d.References())
	foreign, _ := NewOperationBudget(DefaultLimits())
	for _, tc := range []struct {
		d *Document
		b *OperationBudget
		i LineIndex
		o ParseOptions
	}{{nil, b, index, ParseOptions{}}, {constructed, b, index, ParseOptions{}}, {d, nil, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}}, {d, b, NewLineIndex(NewSource([]byte("—"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}}} {
		got, e := tc.d.ASCIIPunctuation(context.Background(), tc.b, tc.i, tc.o)
		if !errors.Is(e, ErrModel) || got != nil {
			t.Fatal(got, e)
		}
	}
	//nolint:staticcheck // Deliberately missing caller.
	if got, e := d.ASCIIPunctuation(nil, b, index, ParseOptions{}); !errors.Is(e, ErrModel) || got != nil {
		t.Fatal(got, e)
	}
	for _, kind := range []string{"incomplete", "profile", "absent", "foreign", "semantic", "transformation", "order"} {
		t.Run(kind, func(t *testing.T) {
			d, b, index := asciiPunctuationParse(t, "—\n\n—", context.Background())
			before, e := d.ASCIIPunctuation(context.Background(), b, index, ParseOptions{})
			if e != nil || len(before) != 2 || before[0].Span.Start() != 0 || before[0].Span.End() != 3 || before[0].Scalar != '—' {
				t.Fatal("earlier literal finding precondition", before, e)
			}
			n := d.root.spec.Children[1].spec.Children[0]
			switch kind {
			case "incomplete":
				d.parsedComplete = false
			case "profile":
				d.parsedProfile = parserProfile(255)
			case "absent":
				n.spec.Content[0].Source = Span{}
			case "foreign":
				n.spec.Content[0].Source, _ = NewSource([]byte("—")).Span(0, 3)
			case "semantic":
				n.spec.Content[0].Semantic = Some("–")
			case "transformation":
				n.spec.Content[0].Transformation = Synthetic
			case "order":
				n.spec.Content = append(n.spec.Content, n.spec.Content[0])
			}
			got, e := d.ASCIIPunctuation(context.Background(), b, index, ParseOptions{})
			if !errors.Is(e, ErrModel) || got != nil {
				t.Fatal(got, e)
			}
		})
	}
}
func TestASCIIPunctuationStickyDetachedConcurrency(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				d, b, index := asciiPunctuationParse(t, "—", context.Background())
				switch kind {
				case "work":
					_ = b.ChargeWork(b.Limits().WorkUnits)
				case "nodes":
					_ = b.ChargeNodes(b.Limits().TotalNodes)
				case "elapsed":
					time.Sleep(61 * time.Second)
				}
				for range 2 {
					got, e := d.ASCIIPunctuation(context.Background(), b, index, ParseOptions{})
					var stop *LimitExceeded
					if !errors.As(e, &stop) || got != nil {
						t.Fatal(got, e)
					}
				}
			})
		})
	}
	d, b, index := asciiPunctuationParse(t, "—", context.Background())
	got, e := d.ASCIIPunctuation(context.Background(), b, index, ParseOptions{})
	if e != nil || len(got) != 1 {
		t.Fatal(got, e)
	}
	saved := append([]ASCIIPunctuationFact(nil), got...)
	got[0] = ASCIIPunctuationFact{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			got, e := d.ASCIIPunctuation(context.Background(), b, index, ParseOptions{})
			if e != nil || !reflect.DeepEqual(got, saved) {
				t.Error(got, e)
			}
		})
	}
	wg.Wait()
}
