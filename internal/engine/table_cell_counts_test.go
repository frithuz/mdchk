package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

const md056FacadeExample = "A | B\n--- | ---\n1 | 2 | 3\n"

func md056ShapeContext(t *testing.T, source string, ctx context.Context) RuleContext {
	return md011ShapeContext(t, "gfm", source, ctx)
}

func md056FacadePositive(t *testing.T, got parser.TableCellCountsInventory, err error) {
	t.Helper()
	if err != nil || got.Availability != parser.Available || len(got.Rows) != 1 || got.Rows[0] != (parser.TableCellCountRow{Start: 16, End: 25, RawCells: 3, Width: 2}) {
		t.Fatal("actual positive", got, err)
	}
}

func TestRuleContextTableCellCountsOptionless(t *testing.T) {
	for _, options := range []Options{nil, {}} {
		c := md056ShapeContext(t, md056FacadeExample, context.Background())
		c.options = options
		got, err := c.TableCellCounts(context.Background())
		md056FacadePositive(t, got, err)
	}
	c := md011ShapeContext(t, "commonmark", md056FacadeExample, context.Background())
	if got, err := c.TableCellCounts(context.Background()); err != nil || got.Availability != parser.Available || got.Rows != nil {
		t.Fatal("CommonMark has no tables", got, err)
	}
}

func TestRuleContextTableCellCountsOriginalAuthority(t *testing.T) {
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) {
			c.parsed.Source = parser.NewSource([]byte(md056FacadeExample))
		}, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) {
			c.index = parser.NewLineIndex(parser.NewSource([]byte(md056FacadeExample)))
		}, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte(md056FacadeExample), nil, DocumentContext{})
		}, func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		}, func(c *RuleContext) { c.input.documentContext.Flavor = "commonmark" },
		func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) },
		// MD056 admits no rule-specific option.
		func(c *RuleContext) { c.options = Options{"style": "consistent"} }, func(c *RuleContext) { c.options = Options{"unknown": false} },
		func(c *RuleContext) { c.options = Options{"enabled": true} },
	} {
		c := md056ShapeContext(t, md056FacadeExample, context.Background())
		pre, err := c.TableCellCounts(context.Background())
		md056FacadePositive(t, pre, err)
		invalidate(&c)
		got, err := c.TableCellCounts(context.Background())
		if !errors.Is(err, parser.ErrModel) || got.Rows != nil || got.Availability != "" {
			t.Fatal(got, err)
		}
	}
	c := md056ShapeContext(t, md056FacadeExample, context.Background())
	//nolint:staticcheck // Deliberately missing caller authority.
	if got, err := c.TableCellCounts(nil); !errors.Is(err, parser.ErrModel) || got.Rows != nil {
		t.Fatal(got, err)
	}
}

func TestRuleContextTableCellCountsCallerAndOriginControls(t *testing.T) {
	for _, source := range []string{"", "clean", "```\na | b\n--- | ---\n```\n", md056FacadeExample, "> | a |\n> | - |\n> | 1 | 2 |\n"} {
		for _, originating := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/origin-%v", source, originating), func(t *testing.T) {
				origin, cancelOrigin := context.WithCancel(context.Background())
				defer cancelOrigin()
				c := md056ShapeContext(t, source, origin)
				caller, cancelCaller := context.WithCancel(context.Background())
				defer cancelCaller()
				if originating {
					cancelOrigin()
				} else {
					cancelCaller()
				}
				got, err := c.TableCellCounts(caller)
				if !errors.Is(err, context.Canceled) || got.Rows != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
}

func TestRuleContextTableCellCountsProducerStagesAndFinalAfterPositive(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).TableCellCounts.func1", ".RuleContext.TableCellCounts.func1", "/parser.(*Document).TableCellCounts.func2"} {
		for _, originating := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(fmt.Sprintf("%s/origin-%v/%v", gate, originating, reason), func(t *testing.T) {
					origin := &md014ProducerFinalStop{Context: context.Background(), reason: reason, gate: gate}
					c := md056ShapeContext(t, md056FacadeExample, origin)
					before, err := c.TableCellCounts(context.Background())
					md056FacadePositive(t, before, err)
					caller := &md014ProducerFinalStop{Context: context.Background(), armed: true, reason: reason, gate: gate}
					var ctx context.Context = caller
					if originating {
						origin.armed = true
						ctx = context.Background()
					}
					got, err := c.TableCellCounts(ctx)
					reached := caller.reached
					if originating {
						reached = origin.reached
					}
					if !reached || !errors.Is(err, reason) || got.Rows != nil {
						t.Fatal(got, err, reached)
					}
				})
			}
		}
	}
}

func TestRuleContextTableCellCountsActualCallerAndOriginDeadlines(t *testing.T) {
	for _, source := range []string{"", "clean", md056FacadeExample} {
		for _, originating := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/origin-%v", source, originating), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					origin := context.Background()
					caller := context.Background()
					if originating {
						c, cancel := context.WithDeadline(origin, time.Now().Add(time.Second))
						defer cancel()
						origin = c
					}
					c := md056ShapeContext(t, source, origin)
					if !originating {
						d, cancel := context.WithDeadline(caller, time.Now().Add(time.Second))
						defer cancel()
						caller = d
					}
					time.Sleep(2 * time.Second)
					out, err := c.TableCellCounts(caller)
					if !errors.Is(err, context.DeadlineExceeded) || out.Rows != nil {
						t.Fatal(out, err)
					}
				})
			})
		}
	}
}

func TestRuleContextTableCellCountsProducerAndFacadeFinalStickyDiscard(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).TableCellCounts.func1", ".RuleContext.TableCellCounts.func1"} {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/origin-%v", gate, kind, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						origin := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind}
						c := md056ShapeContext(t, md056FacadeExample, origin)
						origin.budget = c.budget
						pre, e := c.TableCellCounts(context.Background())
						md056FacadePositive(t, pre, e)
						caller := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind, budget: c.budget, armed: true}
						var target context.Context = caller
						if originating {
							origin.armed = true
							target = context.Background()
						}
						for range 2 {
							got, e := c.TableCellCounts(target)
							var stop *parser.LimitExceeded
							if !errors.As(e, &stop) || got.Rows != nil || got.Availability != "" {
								t.Fatal(got, e)
							}
						}
						if !caller.reached && !origin.reached {
							t.Fatal("final gate not reached")
						}
					})
				})
			}
		}
	}
}

func TestRuleContextTableCellCountsExhaustedBudget(t *testing.T) {
	for _, kind := range []string{"work", "nodes"} {
		t.Run(kind, func(t *testing.T) {
			c := md056ShapeContext(t, md056FacadeExample, context.Background())
			pre, err := c.TableCellCounts(context.Background())
			md056FacadePositive(t, pre, err)
			if kind == "work" {
				_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
			} else {
				_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes)
			}
			for range 2 {
				got, err := c.TableCellCounts(context.Background())
				var stop *parser.LimitExceeded
				if !errors.As(err, &stop) || got.Rows != nil || got.Availability != "" {
					t.Fatal(got, err)
				}
			}
		})
	}
}
