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

const md055FacadeExample = "A | B\n--- | ---\n| 1 | 2 |\n"

func md055ShapeContext(t *testing.T, source string, ctx context.Context) RuleContext {
	c := md011ShapeContext(t, "gfm", source, ctx)
	c.options = Options{"style": "consistent"}
	return c
}

func md055FacadePositive(t *testing.T, got parser.TableOuterPipesInventory, err error) {
	t.Helper()
	if err != nil || got.Availability != parser.Available || len(got.Rows) != 3 || got.Rows[2].Role != "body" || got.Rows[2].Start != 16 || got.Rows[2].End != 25 || got.Rows[2].LeadingPipe.Start() != 16 || got.Rows[2].TrailingPipe.Start() != 24 {
		t.Fatal("actual positive", got, err)
	}
}

func TestRuleContextTableOuterPipesStyles(t *testing.T) {
	for _, style := range []string{"consistent", "leading_and_trailing", "none"} {
		c := md055ShapeContext(t, md055FacadeExample, context.Background())
		c.options = Options{"style": style}
		got, err := c.TableOuterPipes(context.Background())
		md055FacadePositive(t, got, err)
	}
	c := md011ShapeContext(t, "commonmark", md055FacadeExample, context.Background())
	c.options = Options{"style": "consistent"}
	if got, err := c.TableOuterPipes(context.Background()); err != nil || got.Availability != parser.Available || got.Rows != nil {
		t.Fatal("CommonMark has no tables", got, err)
	}
}

func TestRuleContextTableOuterPipesOriginalAuthority(t *testing.T) {
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) {
			c.parsed.Source = parser.NewSource([]byte(md055FacadeExample))
		}, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) {
			c.index = parser.NewLineIndex(parser.NewSource([]byte(md055FacadeExample)))
		}, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte(md055FacadeExample), nil, DocumentContext{})
		}, func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		}, func(c *RuleContext) { c.input.documentContext.Flavor = "commonmark" },
		func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) },
		// Option shape: missing, extra, renamed, wrongly typed or unknown style.
		func(c *RuleContext) { c.options = nil }, func(c *RuleContext) { c.options = Options{} },
		func(c *RuleContext) {
			c.options = Options{"style": "none", "unknown": false}
		}, func(c *RuleContext) { c.options = Options{"styles": "none"} },
		func(c *RuleContext) { c.options = Options{"style": 1} }, func(c *RuleContext) { c.options = Options{"style": []string{"none"}} },
		func(c *RuleContext) { c.options = Options{"style": nil} }, func(c *RuleContext) { c.options = Options{"style": "both"} },
		func(c *RuleContext) { c.options = Options{"style": "Consistent"} },
	} {
		c := md055ShapeContext(t, md055FacadeExample, context.Background())
		pre, err := c.TableOuterPipes(context.Background())
		md055FacadePositive(t, pre, err)
		invalidate(&c)
		got, err := c.TableOuterPipes(context.Background())
		if !errors.Is(err, parser.ErrModel) || got.Rows != nil || got.Availability != "" {
			t.Fatal(got, err)
		}
	}
	c := md055ShapeContext(t, md055FacadeExample, context.Background())
	//nolint:staticcheck // Deliberately missing caller authority.
	if got, err := c.TableOuterPipes(nil); !errors.Is(err, parser.ErrModel) || got.Rows != nil {
		t.Fatal(got, err)
	}
}

func TestRuleContextTableOuterPipesCallerAndOriginControls(t *testing.T) {
	for _, source := range []string{"", "clean", "```\na | b\n--- | ---\n```\n", md055FacadeExample, "> | a |\n> | - |\n"} {
		for _, originating := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/origin-%v", source, originating), func(t *testing.T) {
				origin, cancelOrigin := context.WithCancel(context.Background())
				defer cancelOrigin()
				c := md055ShapeContext(t, source, origin)
				caller, cancelCaller := context.WithCancel(context.Background())
				defer cancelCaller()
				if originating {
					cancelOrigin()
				} else {
					cancelCaller()
				}
				got, err := c.TableOuterPipes(caller)
				if !errors.Is(err, context.Canceled) || got.Rows != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
}

func TestRuleContextTableOuterPipesProducerStagesAndFinalAfterPositive(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).TableOuterPipes.func1", ".RuleContext.TableOuterPipes.func1", "/parser.(*Document).TableOuterPipes.func2"} {
		for _, originating := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(fmt.Sprintf("%s/origin-%v/%v", gate, originating, reason), func(t *testing.T) {
					origin := &md014ProducerFinalStop{Context: context.Background(), reason: reason, gate: gate}
					c := md055ShapeContext(t, md055FacadeExample, origin)
					before, err := c.TableOuterPipes(context.Background())
					md055FacadePositive(t, before, err)
					caller := &md014ProducerFinalStop{Context: context.Background(), armed: true, reason: reason, gate: gate}
					var ctx context.Context = caller
					if originating {
						origin.armed = true
						ctx = context.Background()
					}
					got, err := c.TableOuterPipes(ctx)
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

func TestRuleContextTableOuterPipesActualCallerAndOriginDeadlines(t *testing.T) {
	for _, source := range []string{"", "clean", md055FacadeExample} {
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
					c := md055ShapeContext(t, source, origin)
					if !originating {
						d, cancel := context.WithDeadline(caller, time.Now().Add(time.Second))
						defer cancel()
						caller = d
					}
					time.Sleep(2 * time.Second)
					out, err := c.TableOuterPipes(caller)
					if !errors.Is(err, context.DeadlineExceeded) || out.Rows != nil {
						t.Fatal(out, err)
					}
				})
			})
		}
	}
}

func TestRuleContextTableOuterPipesProducerAndFacadeFinalStickyDiscard(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).TableOuterPipes.func1", ".RuleContext.TableOuterPipes.func1"} {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/origin-%v", gate, kind, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						origin := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind}
						c := md055ShapeContext(t, md055FacadeExample, origin)
						origin.budget = c.budget
						pre, e := c.TableOuterPipes(context.Background())
						md055FacadePositive(t, pre, e)
						caller := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind, budget: c.budget, armed: true}
						var target context.Context = caller
						if originating {
							origin.armed = true
							target = context.Background()
						}
						for range 2 {
							got, e := c.TableOuterPipes(target)
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

func TestRuleContextTableOuterPipesExhaustedBudget(t *testing.T) {
	for _, kind := range []string{"work", "nodes"} {
		t.Run(kind, func(t *testing.T) {
			c := md055ShapeContext(t, md055FacadeExample, context.Background())
			pre, err := c.TableOuterPipes(context.Background())
			md055FacadePositive(t, pre, err)
			if kind == "work" {
				_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
			} else {
				_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes)
			}
			for range 2 {
				got, err := c.TableOuterPipes(context.Background())
				var stop *parser.LimitExceeded
				if !errors.As(err, &stop) || got.Rows != nil || got.Availability != "" {
					t.Fatal(got, err)
				}
			}
		})
	}
}
