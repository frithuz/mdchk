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

const md060FacadeExample = "| A| B |\n|---|---|\n|1|2|\n"

func md060ShapeContext(t *testing.T, source string, ctx context.Context) RuleContext {
	c := md011ShapeContext(t, "gfm", source, ctx)
	c.options = Options{"style": "aligned"}
	return c
}

func md060FacadePositive(t *testing.T, got parser.TableLayoutInventory, err error) {
	t.Helper()
	if err != nil || got.Availability != parser.Available || len(got.Tables) != 1 || len(got.Tables[0].Rows) != 3 || got.Tables[0].Rows[2].Role != "body" || got.Tables[0].Rows[2].Start != 19 || got.Tables[0].Rows[2].End != 24 || len(got.Tables[0].Rows[2].Cells) != 2 || got.Tables[0].Rows[0].Cells[0] != (parser.TableLayoutCell{FieldStart: 1, FieldEnd: 3, TextStart: 2, TextEnd: 3}) {
		t.Fatal("actual positive", got, err)
	}
}

func TestRuleContextTableLayoutStyles(t *testing.T) {
	for _, style := range []string{"aligned", "compact", "tight"} {
		c := md060ShapeContext(t, md060FacadeExample, context.Background())
		c.options = Options{"style": style}
		got, err := c.TableLayout(context.Background())
		md060FacadePositive(t, got, err)
	}
	c := md011ShapeContext(t, "commonmark", md060FacadeExample, context.Background())
	c.options = Options{"style": "aligned"}
	if got, err := c.TableLayout(context.Background()); err != nil || got.Availability != parser.Available || got.Tables != nil {
		t.Fatal("CommonMark has no tables", got, err)
	}
}

func TestRuleContextTableLayoutOriginalAuthority(t *testing.T) {
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) {
			c.parsed.Source = parser.NewSource([]byte(md060FacadeExample))
		}, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) {
			c.index = parser.NewLineIndex(parser.NewSource([]byte(md060FacadeExample)))
		}, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte(md060FacadeExample), nil, DocumentContext{})
		}, func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		}, func(c *RuleContext) { c.input.documentContext.Flavor = "commonmark" },
		func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) },
		// Option shape: missing, extra, renamed, wrongly typed or unknown style.
		func(c *RuleContext) { c.options = nil }, func(c *RuleContext) { c.options = Options{} },
		func(c *RuleContext) {
			c.options = Options{"style": "tight", "unknown": false}
		}, func(c *RuleContext) { c.options = Options{"styles": "tight"} },
		func(c *RuleContext) { c.options = Options{"style": 1} }, func(c *RuleContext) { c.options = Options{"style": []string{"tight"}} },
		func(c *RuleContext) { c.options = Options{"style": nil} }, func(c *RuleContext) { c.options = Options{"style": "wide"} },
		func(c *RuleContext) { c.options = Options{"style": "Aligned"} },
	} {
		c := md060ShapeContext(t, md060FacadeExample, context.Background())
		pre, err := c.TableLayout(context.Background())
		md060FacadePositive(t, pre, err)
		invalidate(&c)
		got, err := c.TableLayout(context.Background())
		if !errors.Is(err, parser.ErrModel) || got.Tables != nil || got.Availability != "" {
			t.Fatal(got, err)
		}
	}
	c := md060ShapeContext(t, md060FacadeExample, context.Background())
	//nolint:staticcheck // Deliberately missing caller authority.
	if got, err := c.TableLayout(nil); !errors.Is(err, parser.ErrModel) || got.Tables != nil {
		t.Fatal(got, err)
	}
}

func TestRuleContextTableLayoutCallerAndOriginControls(t *testing.T) {
	for _, source := range []string{"", "clean", "```\na | b\n--- | ---\n```\n", md060FacadeExample, "> | a |\n> | - |\n"} {
		for _, originating := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/origin-%v", source, originating), func(t *testing.T) {
				origin, cancelOrigin := context.WithCancel(context.Background())
				defer cancelOrigin()
				c := md060ShapeContext(t, source, origin)
				caller, cancelCaller := context.WithCancel(context.Background())
				defer cancelCaller()
				if originating {
					cancelOrigin()
				} else {
					cancelCaller()
				}
				got, err := c.TableLayout(caller)
				if !errors.Is(err, context.Canceled) || got.Tables != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
}

func TestRuleContextTableLayoutProducerStagesAndFinalAfterPositive(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).TableLayout.func1", ".RuleContext.TableLayout.func1", "/parser.(*Document).TableLayout.func2"} {
		for _, originating := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(fmt.Sprintf("%s/origin-%v/%v", gate, originating, reason), func(t *testing.T) {
					origin := &md014ProducerFinalStop{Context: context.Background(), reason: reason, gate: gate}
					c := md060ShapeContext(t, md060FacadeExample, origin)
					before, err := c.TableLayout(context.Background())
					md060FacadePositive(t, before, err)
					caller := &md014ProducerFinalStop{Context: context.Background(), armed: true, reason: reason, gate: gate}
					var ctx context.Context = caller
					if originating {
						origin.armed = true
						ctx = context.Background()
					}
					got, err := c.TableLayout(ctx)
					reached := caller.reached
					if originating {
						reached = origin.reached
					}
					if !reached || !errors.Is(err, reason) || got.Tables != nil {
						t.Fatal(got, err, reached)
					}
				})
			}
		}
	}
}

func TestRuleContextTableLayoutActualCallerAndOriginDeadlines(t *testing.T) {
	for _, source := range []string{"", "clean", md060FacadeExample} {
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
					c := md060ShapeContext(t, source, origin)
					if !originating {
						d, cancel := context.WithDeadline(caller, time.Now().Add(time.Second))
						defer cancel()
						caller = d
					}
					time.Sleep(2 * time.Second)
					out, err := c.TableLayout(caller)
					if !errors.Is(err, context.DeadlineExceeded) || out.Tables != nil {
						t.Fatal(out, err)
					}
				})
			})
		}
	}
}

func TestRuleContextTableLayoutProducerAndFacadeFinalStickyDiscard(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).TableLayout.func1", ".RuleContext.TableLayout.func1"} {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/origin-%v", gate, kind, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						origin := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind}
						c := md060ShapeContext(t, md060FacadeExample, origin)
						origin.budget = c.budget
						pre, e := c.TableLayout(context.Background())
						md060FacadePositive(t, pre, e)
						caller := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind, budget: c.budget, armed: true}
						var target context.Context = caller
						if originating {
							origin.armed = true
							target = context.Background()
						}
						for range 2 {
							got, e := c.TableLayout(target)
							var stop *parser.LimitExceeded
							if !errors.As(e, &stop) || got.Tables != nil || got.Availability != "" {
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

func TestRuleContextTableLayoutExhaustedBudget(t *testing.T) {
	for _, kind := range []string{"work", "nodes"} {
		t.Run(kind, func(t *testing.T) {
			c := md060ShapeContext(t, md060FacadeExample, context.Background())
			pre, err := c.TableLayout(context.Background())
			md060FacadePositive(t, pre, err)
			if kind == "work" {
				_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
			} else {
				_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes)
			}
			for range 2 {
				got, err := c.TableLayout(context.Background())
				var stop *parser.LimitExceeded
				if !errors.As(err, &stop) || got.Tables != nil || got.Availability != "" {
					t.Fatal(got, err)
				}
			}
		})
	}
}
