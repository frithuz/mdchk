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

func md059ShapeContext(t *testing.T, flavor, source string, ctx context.Context) RuleContext {
	c := md011ShapeContext(t, flavor, source, ctx)
	c.options = Options{"phrases": []string{"guide"}}
	return c
}

func TestRuleContextLinkLabelsOriginalAuthority(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, invalidate := range []func(*RuleContext){
				func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) {
					c.parsed.Source = parser.NewSource([]byte("[Guide](guide.md)"))
				}, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) {
					c.index = parser.NewLineIndex(parser.NewSource([]byte("[Guide](guide.md)")))
				}, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) {
					c.input, _ = NewInput("foreign.md", []byte("[Guide](guide.md)"), nil, DocumentContext{})
				}, func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) {
					c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
				}, func(c *RuleContext) {
					if flavor == "commonmark" {
						c.input.documentContext.Flavor = "gfm"
					} else {
						c.input.documentContext.Flavor = "commonmark"
					}
				}, func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) },
				// Option shape: missing, extra, renamed or wrongly typed phrases.
				func(c *RuleContext) { c.options = nil }, func(c *RuleContext) { c.options = Options{} },
				func(c *RuleContext) {
					c.options = Options{"phrases": []string{"here"}, "unknown": false}
				}, func(c *RuleContext) { c.options = Options{"phrase": []string{"here"}} },
				func(c *RuleContext) { c.options = Options{"phrases": "here"} }, func(c *RuleContext) { c.options = Options{"phrases": []any{"here"}} },
				func(c *RuleContext) { c.options = Options{"phrases": nil} },
			} {
				c := md059ShapeContext(t, flavor, "[Guide](guide.md)", context.Background())
				pre, err := c.LinkLabels(context.Background())
				if err != nil || pre.Availability != parser.Available || len(pre.Items) != 1 || pre.Items[0].Span.Start() != 1 || pre.Items[0].Span.End() != 6 || pre.Items[0].Text != "Guide" {
					t.Fatal("actual positive before authority mutation", pre, err)
				}
				invalidate(&c)
				got, err := c.LinkLabels(context.Background())
				if !errors.Is(err, parser.ErrModel) || got.Items != nil || got.Availability != "" {
					t.Fatal(got, err)
				}
			}
			c := md059ShapeContext(t, flavor, "[Guide](guide.md)", context.Background())
			//nolint:staticcheck // Deliberately missing caller authority.
			if got, err := c.LinkLabels(nil); !errors.Is(err, parser.ErrModel) || got.Items != nil {
				t.Fatal(got, err)
			}
		})
	}
}

func TestRuleContextLinkLabelsCallerAndOriginControls(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, source := range []string{"", "clean", "`code`", "<pre>**x**", "[Guide](guide.md)", "<div>**x** <x a=\""} {
				for _, originating := range []bool{false, true} {
					t.Run(fmt.Sprintf("%q/origin-%v", source, originating), func(t *testing.T) {
						origin, cancelOrigin := context.WithCancel(context.Background())
						defer cancelOrigin()
						c := md059ShapeContext(t, flavor, source, origin)
						caller, cancelCaller := context.WithCancel(context.Background())
						defer cancelCaller()
						if originating {
							cancelOrigin()
						} else {
							cancelCaller()
						}
						got, err := c.LinkLabels(caller)
						if !errors.Is(err, context.Canceled) || got.Items != nil {
							t.Fatal(got, err)
						}
					})
				}
			}
		})
	}
}

func TestRuleContextLinkLabelsProducerStagesAndFinalAfterPositive(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, gate := range []string{"/parser.(*Document).LinkLabels.func1", ".RuleContext.LinkLabels.func1", "/parser.(*Document).LinkLabels.func2"} {
				for _, originating := range []bool{false, true} {
					for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
						t.Run(fmt.Sprintf("%s/origin-%v/%v", gate, originating, reason), func(t *testing.T) {
							origin := &md014ProducerFinalStop{Context: context.Background(), reason: reason, gate: gate}
							c := md059ShapeContext(t, flavor, "[Guide](guide.md)", origin)
							before, err := c.LinkLabels(context.Background())
							if err != nil || len(before.Items) != 1 || before.Items[0].Span.Start() != 1 || before.Items[0].Span.End() != 6 {
								t.Fatal("actual earlier positive precondition", before, err)
							}
							caller := &md014ProducerFinalStop{Context: context.Background(), armed: true, reason: reason, gate: gate}
							var ctx context.Context = caller
							if originating {
								origin.armed = true
								ctx = context.Background()
							}
							got, err := c.LinkLabels(ctx)
							reached := caller.reached
							if originating {
								reached = origin.reached
							}
							if !reached || !errors.Is(err, reason) || got.Items != nil {
								t.Fatal(got, err, reached)
							}
						})
					}
				}
			}
		})
	}
}

func TestRuleContextLinkLabelsActualCallerAndOriginDeadlines(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, source := range []string{"", "clean", "```sh\n$ pwd\nout\n```\n", "[Guide](guide.md)", "[Guide](guide.md)\n<div>\n<x title=\"\n\n[Guide](guide.md)"} {
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
							c := md059ShapeContext(t, flavor, source, origin)
							if !originating {
								d, cancel := context.WithDeadline(caller, time.Now().Add(time.Second))
								defer cancel()
								caller = d
							}
							time.Sleep(2 * time.Second)
							out, err := c.LinkLabels(caller)
							if !errors.Is(err, context.DeadlineExceeded) || out.Items != nil {
								t.Fatal(out, err)
							}
						})
					})
				}
			}
		})
	}
}

func TestRuleContextLinkLabelsProducerAndFacadeFinalStickyDiscard(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, gate := range []string{"/parser.(*Document).LinkLabels.func1", ".RuleContext.LinkLabels.func1"} {
				for _, kind := range []string{"work", "nodes", "elapsed"} {
					for _, originating := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/%s/origin-%v", gate, kind, originating), func(t *testing.T) {
							synctest.Test(t, func(t *testing.T) {
								origin := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind}
								c := md059ShapeContext(t, flavor, "[Guide](guide.md)", origin)
								origin.budget = c.budget
								pre, e := c.LinkLabels(context.Background())
								if e != nil || len(pre.Items) != 1 || pre.Items[0].Span.Start() != 1 {
									t.Fatal("positive precondition", pre, e)
								}
								caller := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind, budget: c.budget, armed: true}
								var target context.Context = caller
								if originating {
									origin.armed = true
									target = context.Background()
								}
								for range 2 {
									got, e := c.LinkLabels(target)
									var stop *parser.LimitExceeded
									if !errors.As(e, &stop) || got.Items != nil || got.Availability != "" {
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
		})
	}
}

func TestRuleContextLinkLabelsExhaustedBudget(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, kind := range []string{"work", "nodes"} {
			t.Run(flavor+"/"+kind, func(t *testing.T) {
				c := md059ShapeContext(t, flavor, "[Guide](guide.md)", context.Background())
				if pre, err := c.LinkLabels(context.Background()); err != nil || len(pre.Items) != 1 {
					t.Fatal("positive precondition", pre, err)
				}
				if kind == "work" {
					_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
				} else {
					_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes)
				}
				for range 2 {
					got, err := c.LinkLabels(context.Background())
					var stop *parser.LimitExceeded
					if !errors.As(err, &stop) || got.Items != nil || got.Availability != "" {
						t.Fatal(got, err)
					}
				}
			})
		}
	}
}
