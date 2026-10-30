package parser

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestUnclosedHTMLCommentsFrozenGeometry(t *testing.T) {
	data, e := os.ReadFile("../rules/testdata/md086.json")
	if e != nil {
		t.Fatal(e)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != "040049a12181feead5852bd61b889b0a68ded6da44b45b6c6c7a8941eda3391f" {
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
				Text              string
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
				got, e := p.Document.UnclosedHTMLComments(context.Background(), b, index, o)
				if p.Status != Complete {
					if f.Complete || !errors.Is(e, ErrModel) || got.Comments != nil {
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
				if got.Availability != want || len(got.Comments) != len(f.Candidates) {
					t.Fatal(got, f.Candidates)
				}
				for i, w := range f.Candidates {
					g := got.Comments[i]
					text, te := s.Text(g.Open)
					a, ae := index.Position(g.Open.Start())
					z, ze := index.Position(g.Open.End())
					if te != nil || ae != nil || ze != nil || text != w.Text || [2]int{g.Open.Start(), g.Open.End()} != w.Bytes || a != (Position{w.Start[0], w.Start[1]}) || z != (Position{w.End[0], w.End[1]}) {
						t.Fatal(g, w, te, ae, ze)
					}
				}
				if string(s.Bytes()) != f.Source {
					t.Fatal("source changed")
				}
			})
		}
	}
}

func unclosedHTMLCommentsParse(t *testing.T, source string) (*Document, *OperationBudget, LineIndex) {
	t.Helper()
	s := NewSource([]byte(source))
	b, _ := NewOperationBudget(DefaultLimits())
	p := ParseWithOptions(context.Background(), s, b, ParseOptions{})
	if p.Status != Complete {
		t.Fatal(p)
	}
	return p.Document, b, NewLineIndex(s)
}

func TestUnclosedHTMLCommentsOriginalAuthority(t *testing.T) {
	d, b, index := unclosedHTMLCommentsParse(t, "<!-- note")
	constructed, _ := NewDocument(d.Root(), d.References())
	foreign, _ := NewOperationBudget(DefaultLimits())
	for _, tc := range []struct {
		d *Document
		b *OperationBudget
		i LineIndex
		o ParseOptions
	}{{nil, b, index, ParseOptions{}}, {constructed, b, index, ParseOptions{}}, {d, nil, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}}, {d, b, NewLineIndex(NewSource([]byte("<!-- note"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}}} {
		got, e := tc.d.UnclosedHTMLComments(context.Background(), tc.b, tc.i, tc.o)
		if !errors.Is(e, ErrModel) || got.Comments != nil {
			t.Fatal(got, e)
		}
	}
	//nolint:staticcheck // Deliberately missing caller authority.
	if got, e := d.UnclosedHTMLComments(nil, b, index, ParseOptions{}); !errors.Is(e, ErrModel) || got.Comments != nil {
		t.Fatal(got, e)
	}
	// Exercise real privately parsed documents for every partial profile, rather
	// than only mutating the public result or forging the stored profile field.
	for _, profile := range []parserProfile{tablesOnlyProfile, tasksOnlyProfile, strikeOnlyProfile, bareOnlyProfile, filterOnlyProfile} {
		s := NewSource([]byte("<!-- note"))
		budget, _ := NewOperationBudget(DefaultLimits())
		p := parseWithProfile(context.Background(), s, budget, profile)
		if p.Status != Complete {
			t.Fatal(p)
		}
		got, e := p.Document.UnclosedHTMLComments(context.Background(), budget, NewLineIndex(s), p.Document.parsedOptions)
		if !errors.Is(e, ErrModel) || got.Comments != nil {
			t.Fatal(profile, got, e)
		}
	}
	d.parsedComplete = false
	if got, e := d.UnclosedHTMLComments(context.Background(), b, index, ParseOptions{}); !errors.Is(e, ErrModel) || got.Comments != nil {
		t.Fatal(got, e)
	}
}

func TestUnclosedHTMLCommentsLateAssociations(t *testing.T) {
	for _, kind := range []string{"absent", "foreign", "semantic", "transformation", "order"} {
		t.Run(kind, func(t *testing.T) {
			d, b, index := unclosedHTMLCommentsParse(t, "> <!-- first\n\n<!-- second")
			before, e := d.UnclosedHTMLComments(context.Background(), b, index, ParseOptions{})
			if e != nil || len(before.Comments) != 2 || before.Comments[0].Open.Start() != 2 {
				t.Fatal(before, e)
			}
			n := d.root.spec.Children[1]
			switch kind {
			case "absent":
				n.spec.Content[0].Source = Span{}
			case "foreign":
				n.spec.Content[0].Source, _ = NewSource([]byte("<!-- second")).Span(0, 11)
			case "semantic":
				n.spec.Content[0].Semantic = Some("<!---->")
			case "transformation":
				n.spec.Content[0].Transformation = Synthetic
			case "order":
				n.spec.Content = append(n.spec.Content, n.spec.Content[0])
			}
			got, e := d.UnclosedHTMLComments(context.Background(), b, index, ParseOptions{})
			if !errors.Is(e, ErrModel) || got.Comments != nil {
				t.Fatal(got, e)
			}
		})
	}
}

func TestUnclosedHTMLCommentsStickyDetachedConcurrency(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				d, b, index := unclosedHTMLCommentsParse(t, "<!-- note")
				switch kind {
				case "work":
					_ = b.ChargeWork(b.Limits().WorkUnits)
				case "nodes":
					_ = b.ChargeNodes(b.Limits().TotalNodes)
				case "elapsed":
					time.Sleep(61 * time.Second)
				}
				for range 2 {
					got, e := d.UnclosedHTMLComments(context.Background(), b, index, ParseOptions{})
					var stop *LimitExceeded
					if !errors.As(e, &stop) || got.Comments != nil {
						t.Fatal(got, e)
					}
				}
			})
		})
	}
	d, b, index := unclosedHTMLCommentsParse(t, "<!-- note")
	got, e := d.UnclosedHTMLComments(context.Background(), b, index, ParseOptions{})
	if e != nil || len(got.Comments) != 1 {
		t.Fatal(got, e)
	}
	saved := append([]UnclosedHTMLComment(nil), got.Comments...)
	got.Comments[0] = UnclosedHTMLComment{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			got, e := d.UnclosedHTMLComments(context.Background(), b, index, ParseOptions{})
			if e != nil || !reflect.DeepEqual(got.Comments, saved) {
				t.Error(got, e)
			}
		})
	}
	wg.Wait()
}

// Source-derived setup and phase costs; neither budget counters nor failed
// sacrificial parses are used to fit an allowance.
func TestUnclosedHTMLCommentsStaticEmptyReservations(t *testing.T) {
	for _, nodes := range []bool{false, true} {
		for _, delta := range []int64{-1, 0} {
			d, b, index := unclosedHTMLCommentsParse(t, "")
			charge, limit, setup, phase := b.ChargeWork, b.Limits().WorkUnits, int64(25), int64(32)
			if nodes {
				charge, limit, setup, phase = b.ChargeNodes, b.Limits().TotalNodes, 2, 2
			}
			if err := charge(limit - setup - phase - delta); err != nil {
				t.Fatal(err)
			}
			got, err := d.UnclosedHTMLComments(context.Background(), b, index, ParseOptions{})
			if delta == 0 {
				if err != nil || got.Availability != Available || len(got.Comments) != 0 {
					t.Fatal(got, err)
				}
			} else {
				for range 2 {
					var stop *LimitExceeded
					if !errors.As(err, &stop) || got.Comments != nil {
						t.Fatal(got, err)
					}
					got, err = d.UnclosedHTMLComments(context.Background(), b, index, ParseOptions{})
				}
			}
		}
	}
}

func TestUnclosedHTMLCommentsFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		source string
		final  int
	}{{"", 5}, {"clean", 7}, {"`ab`", 7}, {"```\nx\n```", 6}, {"<!--x-->", 7}, {"<!--", 6}, {"<div\n", 8}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			d, b, index := unclosedHTMLCommentsParse(t, tc.source)
			caller := &paragraphEmphasisStop{Context: context.Background(), stop: tc.final, reason: reason}
			got, err := d.UnclosedHTMLComments(caller, b, index, ParseOptions{})
			if !errors.Is(err, reason) || got.Comments != nil {
				t.Fatal(tc.source, got, err)
			}
		}
	}
}

func TestUnclosedHTMLCommentsWideScratchReservation(t *testing.T) {
	for _, tc := range []struct{ logical, parts, work, records int64 }{{0, 0, 8, 3}, {4, 1, 41, 20}, {1 << 30, 1, 8*(1<<30) + 9, 4*(1<<30) + 4}, {math.MaxInt64, 1, math.MaxInt64, math.MaxInt64}, {1, math.MaxInt64, math.MaxInt64, math.MaxInt64}} {
		w, n := unclosedHTMLScratchCosts(tc.logical, tc.parts)
		if w != tc.work || n != tc.records {
			t.Fatal(tc, w, n)
		}
	}
}

func TestUnclosedHTMLCommentsGlobalRawOpaqueProof(t *testing.T) {
	for _, source := range []string{
		"x <script>\n\n<!-- </script> -->\n\n<!-- protected -->\n\n</script>\n\n<!-- eligible",
		"x <script> <a title='</script>'>\n\n<!-- protected -->\n\n</script>\n\n<!-- eligible",
		"x <script>\n\n<?p </script> ?>\n\n<!-- protected -->\n\n</script>\n\n<!-- eligible",
		"x <script>\n\n<![CDATA[</script>]]>\n\n<!-- protected -->\n\n</script>\n\n<!-- eligible",
	} {
		d, b, index := unclosedHTMLCommentsParse(t, source)
		got, err := d.UnclosedHTMLComments(context.Background(), b, index, ParseOptions{})
		if err != nil || got.Availability != Available || len(got.Comments) != 1 || got.Comments[0].Open.Start() != strings.LastIndex(source, "<!-- eligible") {
			t.Fatal(source, got, err)
		}
	}
}
