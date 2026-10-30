package engine

import (
	"context"
	"errors"
	"fmt"
	"github.com/frithuz/mdchk/internal/parser"
	"math"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func md081ShapeContext(t *testing.T, source string, origin context.Context) RuleContext {
	t.Helper()
	in, e := NewInput("ratio.md", []byte(source), nil, DocumentContext{Flavor: "commonmark"})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	p := parser.ParseWithOptions(origin, in.source, b, in.documentContext.ParseOptions())
	if p.Status != parser.Complete {
		t.Fatal(p)
	}
	return RuleContext{input: in, index: parser.NewLineIndex(in.source), document: p.Document, parsed: p, budget: b, operation: origin, syntaxReliable: true, options: Options{"maximum_ratio": .5, "minimum_characters": 1}}
}

func TestRuleContextParagraphEmphasisAuthority(t *testing.T) {
	c := md081ShapeContext(t, "**ab** c", context.Background())
	got, e := c.ParagraphEmphasis(context.Background())
	if e != nil || len(got) != 1 || got[0].Total != 3 || got[0].Emphasized != 2 {
		t.Fatal(got, e)
	}
	for _, invalidate := range []func(*RuleContext){func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("**ab** c")) }, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("**ab** c"))) }, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) { c.input, _ = NewInput("other.md", []byte("**ab** c"), nil, DocumentContext{}) }, func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) {
		c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
	}, func(c *RuleContext) { c.input.documentContext.Flavor = "gfm" }, func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) }, func(c *RuleContext) { c.options = Options{} }, func(c *RuleContext) { c.options = Options{"maximum_ratio": math.NaN(), "minimum_characters": 1} }, func(c *RuleContext) { c.options = Options{"maximum_ratio": 0, "minimum_characters": 20.0} }, func(c *RuleContext) {
		c.options = Options{"maximum_ratio": 0, "minimum_characters": 1, "style": "consistent"}
	}} {
		bad := c
		invalidate(&bad)
		got, e := bad.ParagraphEmphasis(context.Background())
		if !errors.Is(e, parser.ErrModel) || got != nil {
			t.Fatal(got, e)
		}
	}
	//nolint:staticcheck // Missing caller deliberately rejected.
	if got, e := c.ParagraphEmphasis(nil); !errors.Is(e, parser.ErrModel) || got != nil {
		t.Fatal(got, e)
	}
	for _, ratio := range []any{0, 1, 0.0, 1.0, math.Copysign(0, -1)} {
		c.options = Options{"maximum_ratio": ratio, "minimum_characters": 1}
		if _, _, e := c.ParagraphEmphasisOptions(context.Background()); e != nil {
			t.Fatal(ratio, e)
		}
	}
}

var md081ProjectionRows = []struct {
	name, source string
	facts        int
	work, nodes  int64
	calls        int
}{
	{"empty", "", 0, 47, 3, 17}, {"clean", "clean", 1, 3249, 15, 68}, {"protected", "```\nx\n```", 0, 84, 7, 33}, {"code-only", "`ab`", 1, 141, 15, 54}, {"violation", "**ab** c", 1, 3348, 27, 101},
	{"equality", "**ab** cd", 1, 3862, 27, 103}, {"overlap", "***ab*** c", 1, 3383, 29, 116}, {"growth", "**a**\n\n**b**", 2, 2410, 34, 132},
}

func TestRuleContextParagraphEmphasisStaticBoundaries(t *testing.T) {
	for _, tc := range md081ProjectionRows {
		for _, getter := range []bool{false, true} {
			for _, kind := range []string{"work", "nodes"} {
				exact := tc.work + 20
				if getter {
					exact = 18
				}
				if kind == "nodes" {
					exact = tc.nodes
					if getter {
						exact = 0
					}
				}
				for _, delta := range []int64{-1, 0} {
					if exact == 0 && delta < 0 {
						continue
					}
					t.Run(fmt.Sprintf("%s/getter-%t/%s/%d", tc.name, getter, kind, delta), func(t *testing.T) {
						prefix := md081ShapeContext(t, tc.source, context.Background())
						charge, limit := prefix.budget.ChargeWork, prefix.budget.Limits().WorkUnits
						if kind == "nodes" {
							charge, limit = prefix.budget.ChargeNodes, prefix.budget.Limits().TotalNodes
						}
						var stop *parser.LimitExceeded
						if !errors.As(charge(limit), &stop) {
							t.Fatal("prefix must exhaust")
						}
						used := stop.Observed - limit
						c := md081ShapeContext(t, tc.source, context.Background())
						charge = c.budget.ChargeWork
						if kind == "nodes" {
							charge = c.budget.ChargeNodes
						}
						if e := charge(limit - used - exact - delta); e != nil {
							t.Fatal(e)
						}
						call := func() ([]parser.ParagraphEmphasisFact, error) {
							if getter {
								ratio, min, e := c.ParagraphEmphasisOptions(context.Background())
								if e == nil && (ratio != .5 || min != 1) {
									t.Fatal(ratio, min)
								}
								return nil, e
							}
							return c.ParagraphEmphasis(context.Background())
						}
						got, e := call()
						if delta == 0 {
							if e != nil || !getter && len(got) != tc.facts {
								t.Fatal(got, e)
							}
						} else {
							for range 2 {
								if !errors.As(e, &stop) || got != nil {
									t.Fatal(got, e)
								}
								got, e = call()
							}
						}
					})
				}
			}
		}
	}
}
func TestRuleContextParagraphEmphasisFinalCallerAndOrigin(t *testing.T) {
	for _, tc := range md081ProjectionRows {
		for _, getter := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%s/getter-%t/origin-%t/%s", tc.name, getter, originating, reason), func(t *testing.T) {
						origin := &mdTerminalStopAt{Context: context.Background(), stop: 1 << 30, reason: reason}
						c := md081ShapeContext(t, tc.source, origin)
						final := tc.calls + 5
						if getter {
							final = 3
						}
						caller := &mdTerminalStopAt{Context: context.Background(), stop: final, reason: reason}
						var ctx context.Context = caller
						if originating {
							origin.stop = origin.calls + final
							ctx = context.Background()
						}
						if getter {
							ratio, min, e := c.ParagraphEmphasisOptions(ctx)
							if !errors.Is(e, reason) || ratio != 0 || min != 0 {
								t.Fatal(ratio, min, e)
							}
						} else {
							got, e := c.ParagraphEmphasis(ctx)
							if !errors.Is(e, reason) || got != nil {
								t.Fatal(got, e)
							}
						}
						if !originating && caller.calls != final {
							t.Fatal(caller.calls, final)
						}
					})
				}
			}
		}
	}
}
func TestRuleContextParagraphEmphasisStickyDetachedConcurrency(t *testing.T) {
	for _, getter := range []bool{false, true} {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			t.Run(fmt.Sprintf("getter-%t/%s", getter, kind), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					c := md081ShapeContext(t, "**ab** c", context.Background())
					switch kind {
					case "work":
						_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
					case "nodes":
						_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes)
					case "elapsed":
						time.Sleep(61 * time.Second)
					}
					for range 2 {
						var stop *parser.LimitExceeded
						if getter {
							ratio, min, e := c.ParagraphEmphasisOptions(context.Background())
							if !errors.As(e, &stop) || ratio != 0 || min != 0 {
								t.Fatal(ratio, min, e)
							}
						} else {
							got, e := c.ParagraphEmphasis(context.Background())
							if !errors.As(e, &stop) || got != nil {
								t.Fatal(got, e)
							}
						}
					}
				})
			})
		}
	}
	c := md081ShapeContext(t, "**ab** c", context.Background())
	facts, e := c.ParagraphEmphasis(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	facts[0] = parser.ParagraphEmphasisFact{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			got, e := c.ParagraphEmphasis(context.Background())
			if e != nil || len(got) != 1 || got[0].Total != 3 || got[0].Emphasized != 2 {
				t.Error(got, e)
			}
		})
	}
	wg.Wait()
}
