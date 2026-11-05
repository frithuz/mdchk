package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

const md058FacadeExample = "# T\n| a |\n| - |\n# N"

func md058ShapeContext(t *testing.T, source string, origin context.Context) RuleContext {
	t.Helper()
	c := md011ShapeContext(t, "gfm", source, origin)
	c.options = Options{}
	return c
}

func md058FacadePositive(t *testing.T, got []parser.TableSpacingFact, err error) {
	t.Helper()
	if err != nil || len(got) != 2 || got[0].Point.Start() != 4 || !got[0].Before || got[1].Point.Start() != 15 || got[1].Before {
		t.Fatal("actual positive", got, err)
	}
}

func TestRuleContextTableSpacingActualAuthority(t *testing.T) {
	c := md058ShapeContext(t, md058FacadeExample, context.Background())
	got, err := c.TableSpacing(context.Background())
	md058FacadePositive(t, got, err)
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte(md058FacadeExample)) },
		func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil },
		func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte(md058FacadeExample))) }, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
		func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte(md058FacadeExample), nil, DocumentContext{Flavor: "gfm"})
		},
		func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) },
		func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		},
		func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) },
		func(c *RuleContext) { c.input.documentContext.Flavor = "commonmark" },
		// Any rule option is rejected: MD058 has none.
		func(c *RuleContext) { c.options = Options{"before": true} }, func(c *RuleContext) { c.options = Options{"after": nil} }, func(c *RuleContext) { c.options = Options{"style": "consistent"} },
	} {
		bad := c
		invalidate(&bad)
		got, err := bad.TableSpacing(context.Background())
		if !errors.Is(err, parser.ErrModel) || got != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Deliberate absent caller authority.
	if got, err := c.TableSpacing(nil); !errors.Is(err, parser.ErrModel) || got != nil {
		t.Fatal(got, err)
	}
	commonmark := md011ShapeContext(t, "commonmark", md058FacadeExample, context.Background())
	commonmark.options = Options{}
	if got, err := commonmark.TableSpacing(context.Background()); err != nil || got != nil {
		t.Fatal("CommonMark has no tables", got, err)
	}
}

// The facade adds three caller and origin checks around the parser fact's
// final count, pinned in the parser's table spacing tests.
var md058ProjectionCases = []struct {
	name, source string
	facts, final int
}{
	{"empty", "", 0, 15}, {"clean", "clean", 0, 40}, {"edge", "| a |\n| - |", 0, 81},
	{"protected", "```\n| a |\n| - |\n```", 0, 36}, {"violation", md058FacadeExample, 2, 137},
	{"rollback", "> # T\n> | a |\n> | - |\n> # N", 2, 170},
}

func TestRuleContextTableSpacingFinalCallerAndOrigin(t *testing.T) {
	for _, tc := range md058ProjectionCases {
		for _, originating := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(tc.name+map[bool]string{false: "/caller", true: "/origin"}[originating]+reason.Error(), func(t *testing.T) {
					origin := &mdTerminalStopAt{Context: context.Background(), stop: 1 << 30, reason: reason}
					c := md058ShapeContext(t, tc.source, origin)
					if got, e := c.TableSpacing(context.Background()); e != nil || len(got) != tc.facts {
						t.Fatal("actual positive", got, e)
					}
					caller := &mdTerminalStopAt{Context: context.Background(), stop: tc.final + 3, reason: reason}
					var ctx context.Context = caller
					if originating {
						origin.stop = origin.calls + tc.final + 3
						ctx = context.Background()
					}
					got, e := c.TableSpacing(ctx)
					if !errors.Is(e, reason) || got != nil {
						t.Fatal(got, e)
					}
					if !originating && caller.calls != tc.final+3 {
						t.Fatal(caller.calls, tc.final+3)
					}
				})
			}
		}
	}
}

func TestRuleContextTableSpacingStickyAndDetachedConcurrency(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := md058ShapeContext(t, md058FacadeExample, context.Background())
				switch kind {
				case "work":
					_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
				case "nodes":
					_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes)
				case "elapsed":
					time.Sleep(61 * time.Second)
				}
				for range 2 {
					got, e := c.TableSpacing(context.Background())
					var stop *parser.LimitExceeded
					if !errors.As(e, &stop) || got != nil {
						t.Fatal(got, e)
					}
				}
			})
		})
	}
	c := md058ShapeContext(t, md058FacadeExample, context.Background())
	got, e := c.TableSpacing(context.Background())
	md058FacadePositive(t, got, e)
	got[0] = parser.TableSpacingFact{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			facts, e := c.TableSpacing(context.Background())
			if e != nil || len(facts) != 2 || facts[0].Point.Start() != 4 || !facts[0].Before {
				t.Error(facts, e)
			}
		})
	}
	wg.Wait()
}
