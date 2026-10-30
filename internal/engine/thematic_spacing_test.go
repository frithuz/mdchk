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

func md065Context(t *testing.T, flavor string) RuleContext {
	t.Helper()
	in, err := NewInput("style.md", []byte("Text\n***\nMore"), nil, DocumentContext{Flavor: flavor})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithOptions(context.Background(), in.source, b, in.documentContext.ParseOptions())
	if parsed.Status != parser.Complete {
		t.Fatal(parsed)
	}
	return RuleContext{input: in, index: parser.NewLineIndex(in.source), document: parsed.Document, parsed: parsed, budget: b, operation: context.Background(), syntaxReliable: true, options: Options{}}
}

func TestRuleContextThematicSpacingActualAuthority(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			c := md065Context(t, flavor)
			got, err := c.ThematicSpacing(context.Background())
			if err != nil || len(got) != 2 || got[0].Point.Start() != 5 {
				t.Fatal(got, err)
			}
			for _, invalidate := range []func(*RuleContext){
				func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("Text\n***\nMore")) },
				func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil },
				func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("Text\n***\nMore"))) }, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
				func(c *RuleContext) {
					c.input, _ = NewInput("foreign.md", []byte("Text\n***\nMore"), nil, DocumentContext{Flavor: flavor})
				},
				func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) },
				func(c *RuleContext) {
					c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
				},
				func(c *RuleContext) {
					fm, _ := parser.NewFrontmatterOptions(true, nil)
					c.input.documentContext.Frontmatter = fm
				},
				func(c *RuleContext) {
					if flavor == "commonmark" {
						c.input.documentContext.Flavor = "gfm"
					} else {
						c.input.documentContext.Flavor = "commonmark"
					}
				},
				func(c *RuleContext) { c.options = Options{"before": true} }, func(c *RuleContext) { c.options = Options{"after": nil} }, func(c *RuleContext) { c.options = Options{"style": "dash"} },
			} {
				bad := c
				invalidate(&bad)
				got, err := bad.ThematicSpacing(context.Background())
				if !errors.Is(err, parser.ErrModel) || got != nil {
					t.Fatal(got, err)
				}
			}
			//nolint:staticcheck // Deliberate absent caller authority.
			if got, err := c.ThematicSpacing(nil); !errors.Is(err, parser.ErrModel) || got != nil {
				t.Fatal(got, err)
			}
		})
	}
}

var md065ProjectionCases = []struct {
	name, source string
	facts        int
	work, nodes  int64
	final        int
}{
	{"empty", "", 0, 16, 4, 15}, {"clean", "clean", 0, 53, 15, 40}, {"edge", "***", 0, 39, 11, 32},
	{"protected", "```\nx\n```", 0, 45, 11, 34}, {"violation", "Text\n***\nMore", 2, 128, 38, 90},
	{"definitions", "[r]: /x\n***", 1, 61, 14, 43}, {"rollback", "> Text\n> ***\n> More", 2, 211, 53, 119},
	{"growth", "***\n***\n***\n***", 6, 159, 62, 103},
}

func md065ShapeContext(t *testing.T, source string, origin context.Context) RuleContext {
	t.Helper()
	in, e := NewInput("spacing.md", []byte(source), nil, DocumentContext{Flavor: "commonmark"})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	p := parser.ParseWithOptions(origin, in.source, b, in.documentContext.ParseOptions())
	if p.Status != parser.Complete {
		t.Fatal(p)
	}
	return RuleContext{input: in, index: parser.NewLineIndex(in.source), document: p.Document, parsed: p, budget: b, operation: origin, syntaxReliable: true, options: Options{}}
}

func TestRuleContextThematicSpacingStaticReservations(t *testing.T) {
	for _, tc := range md065ProjectionCases {
		for _, kind := range []string{"work", "nodes"} {
			for _, delta := range []int64{-1, 0} {
				t.Run(tc.name+"/"+kind+map[int64]string{-1: "/short", 0: "/exact"}[delta], func(t *testing.T) {
					exact := tc.work + 2
					if kind == "nodes" {
						exact = tc.nodes
					}
					// A sacrificial original operation qualifies ONLY the parse/setup prefix,
					// before facade entry. The genuine operation keeps its own original budget.
					prefix := md065ShapeContext(t, tc.source, context.Background())
					charge, limit := prefix.budget.ChargeWork, prefix.budget.Limits().WorkUnits
					if kind == "nodes" {
						charge, limit = prefix.budget.ChargeNodes, prefix.budget.Limits().TotalNodes
					}
					var exhausted *parser.LimitExceeded
					if !errors.As(charge(limit), &exhausted) {
						t.Fatal("prefix must exhaust")
					}
					used := exhausted.Observed - limit
					c := md065ShapeContext(t, tc.source, context.Background())
					charge = c.budget.ChargeWork
					if kind == "nodes" {
						charge = c.budget.ChargeNodes
					}
					if e := charge(limit - used - exact - delta); e != nil {
						t.Fatal(e)
					}
					got, e := c.ThematicSpacing(context.Background())
					if delta == 0 {
						if e != nil || len(got) != tc.facts {
							t.Fatal(got, e)
						}
					} else {
						for range 2 {
							if !errors.As(e, &exhausted) || got != nil {
								t.Fatal(got, e)
							}
							got, e = c.ThematicSpacing(context.Background())
						}
					}
				})
			}
		}
	}
}

func TestRuleContextThematicSpacingFinalCallerAndOrigin(t *testing.T) {
	for _, tc := range md065ProjectionCases {
		for _, originating := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(tc.name+map[bool]string{false: "/caller", true: "/origin"}[originating]+reason.Error(), func(t *testing.T) {
					origin := &mdTerminalStopAt{Context: context.Background(), stop: 1 << 30, reason: reason}
					c := md065ShapeContext(t, tc.source, origin)
					caller := &mdTerminalStopAt{Context: context.Background(), stop: tc.final + 3, reason: reason}
					var ctx context.Context = caller
					if originating {
						origin.stop = origin.calls + tc.final + 3
						ctx = context.Background()
					}
					got, e := c.ThematicSpacing(ctx)
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

func TestRuleContextThematicSpacingStickyAndDetachedConcurrency(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := md065Context(t, "commonmark")
				switch kind {
				case "work":
					_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
				case "nodes":
					_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes)
				case "elapsed":
					time.Sleep(61 * time.Second)
				}
				for range 2 {
					got, e := c.ThematicSpacing(context.Background())
					var stop *parser.LimitExceeded
					if !errors.As(e, &stop) || got != nil {
						t.Fatal(got, e)
					}
				}
			})
		})
	}
	c := md065Context(t, "commonmark")
	got, e := c.ThematicSpacing(context.Background())
	if e != nil || len(got) != 2 {
		t.Fatal(got, e)
	}
	got[0] = parser.ThematicSpacingFact{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			facts, e := c.ThematicSpacing(context.Background())
			if e != nil || len(facts) != 2 || facts[0].Point.Start() != 5 || !facts[0].Before {
				t.Error(facts, e)
			}
		})
	}
	wg.Wait()
}
