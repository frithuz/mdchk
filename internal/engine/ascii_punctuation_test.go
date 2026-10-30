package engine

import (
	"context"
	"errors"
	"fmt"
	"github.com/frithuz/mdchk/internal/parser"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func md088ShapeContext(t *testing.T, source string, origin context.Context) RuleContext {
	t.Helper()
	in, e := NewInput("ascii.md", []byte(source), nil, DocumentContext{Flavor: "commonmark"})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	p := parser.ParseWithOptions(origin, in.source, b, in.documentContext.ParseOptions())
	if p.Status != parser.Complete {
		t.Fatal(p)
	}
	return RuleContext{input: in, index: parser.NewLineIndex(in.source), document: p.Document, parsed: p, budget: b, operation: origin, syntaxReliable: true, options: Options{"style": "ascii"}}
}

func TestRuleContextASCIIPunctuationAuthority(t *testing.T) {
	c := md088ShapeContext(t, "—", context.Background())
	got, e := c.ASCIIPunctuation(context.Background())
	if e != nil || len(got) != 1 || got[0].Scalar != '—' {
		t.Fatal(got, e)
	}
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("—")) }, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("—"))) }, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) { c.input, _ = NewInput("other.md", []byte("—"), nil, DocumentContext{}) }, func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		}, func(c *RuleContext) { c.input.documentContext.Flavor = "gfm" }, func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) }, func(c *RuleContext) { c.options = Options{} }, func(c *RuleContext) { c.options = Options{"style": nil} }, func(c *RuleContext) { c.options = Options{"style": "ASCII"} }, func(c *RuleContext) { c.options = Options{"style": "ascii", "replacements": "x"} },
	} {
		bad := c
		invalidate(&bad)
		got, e := bad.ASCIIPunctuation(context.Background())
		if !errors.Is(e, parser.ErrModel) || got != nil {
			t.Fatal(got, e)
		}
	}
	//nolint:staticcheck // Deliberately missing caller.
	if got, e := c.ASCIIPunctuation(nil); !errors.Is(e, parser.ErrModel) || got != nil {
		t.Fatal(got, e)
	}
}

var md088ProjectionRows = []struct {
	name, source string
	facts        int
	work, nodes  int64
	calls        int
}{
	{"empty", "", 0, 33, 2, 12}, {"clean", "clean", 0, 715, 4, 50},
	{"protected", "```\nx\n```", 0, 62, 3, 23}, {"code-only", "`ab`", 0, 91, 4, 34},
	{"one-offender", "—", 1, 330, 6, 47}, {"six-class", "‘’“”–—", 6, 860, 25, 80},
	{"nested", "***—***", 1, 388, 8, 69}, {"raw", "<script>\n—\n</script>", 0, 74, 3, 23},
	{"mixed-transformed", "a&amp;—", 1, 560, 6, 54}, {"growth", "—\n\n—", 2, 630, 13, 83},
}

func TestRuleContextASCIIPunctuationStaticBoundaries(t *testing.T) {
	for _, tc := range md088ProjectionRows {
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
						prefix := md088ShapeContext(t, tc.source, context.Background())
						charge, limit := prefix.budget.ChargeWork, prefix.budget.Limits().WorkUnits
						if kind == "nodes" {
							charge, limit = prefix.budget.ChargeNodes, prefix.budget.Limits().TotalNodes
						}
						var stop *parser.LimitExceeded
						if !errors.As(charge(limit), &stop) {
							t.Fatal("prefix must exhaust")
						}
						used := stop.Observed - limit
						c := md088ShapeContext(t, tc.source, context.Background())
						charge = c.budget.ChargeWork
						if kind == "nodes" {
							charge = c.budget.ChargeNodes
						}
						if e := charge(limit - used - exact - delta); e != nil {
							t.Fatal(e)
						}
						call := func() ([]parser.ASCIIPunctuationFact, error) {
							if getter {
								style, e := c.ASCIIPunctuationStyle(context.Background())
								if e == nil && (style != "ascii") {
									t.Fatal(style)
								}
								return nil, e
							}
							return c.ASCIIPunctuation(context.Background())
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
func TestRuleContextASCIIPunctuationFinalCallerAndOrigin(t *testing.T) {
	for _, tc := range md088ProjectionRows {
		for _, getter := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%s/getter-%t/origin-%t/%s", tc.name, getter, originating, reason), func(t *testing.T) {
						origin := &mdTerminalStopAt{Context: context.Background(), stop: 1 << 30, reason: reason}
						c := md088ShapeContext(t, tc.source, origin)
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
							style, e := c.ASCIIPunctuationStyle(ctx)
							if !errors.Is(e, reason) || style != "" {
								t.Fatal(style, e)
							}
						} else {
							got, e := c.ASCIIPunctuation(ctx)
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
func TestRuleContextASCIIPunctuationStickyDetachedConcurrency(t *testing.T) {
	for _, getter := range []bool{false, true} {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			t.Run(fmt.Sprintf("getter-%t/%s", getter, kind), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					c := md088ShapeContext(t, "—", context.Background())
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
							style, e := c.ASCIIPunctuationStyle(context.Background())
							if !errors.As(e, &stop) || style != "" {
								t.Fatal(style, e)
							}
						} else {
							got, e := c.ASCIIPunctuation(context.Background())
							if !errors.As(e, &stop) || got != nil {
								t.Fatal(got, e)
							}
						}
					}
				})
			})
		}
	}
	c := md088ShapeContext(t, "—", context.Background())
	facts, e := c.ASCIIPunctuation(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	facts[0] = parser.ASCIIPunctuationFact{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			got, e := c.ASCIIPunctuation(context.Background())
			if e != nil || len(got) != 1 || got[0].Scalar != '—' {
				t.Error(got, e)
			}
		})
	}
	wg.Wait()
}
