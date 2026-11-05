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

func TestRuleContextDestinationWhitespaceStaticEmptyReservations(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, delta := range []int64{-1, 0} {
				t.Run(fmt.Sprintf("delta-%d", delta), func(t *testing.T) {
					c := md011ShapeContext(t, flavor, "", context.Background())
					// Frozen independently: original empty parse25/30W, facade+producer6W.
					setup := int64(25)
					if flavor == "gfm" {
						setup = 30
					}
					if err := c.budget.ChargeWork(c.budget.Limits().WorkUnits - setup - 6 - delta); err != nil {
						t.Fatal(err)
					}
					got, err := c.DestinationWhitespace(context.Background())
					if delta == 0 {
						if err != nil || got.Availability != parser.Available || len(got.Spans) != 0 {
							t.Fatal(got, err)
						}
						return
					}
					for range 2 {
						var stop *parser.LimitExceeded
						if !errors.As(err, &stop) || got.Spans != nil {
							t.Fatal(got, err)
						}
						got, err = c.DestinationWhitespace(context.Background())
					}
				})
			}

		})
	}
}

func TestRuleContextDestinationWhitespaceOriginalAuthority(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, invalidate := range []func(*RuleContext){
				func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) {
					c.parsed.Source = parser.NewSource([]byte("[Guide](my guide.md)"))
				}, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) {
					c.index = parser.NewLineIndex(parser.NewSource([]byte("[Guide](my guide.md)")))
				}, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) {
					c.input, _ = NewInput("foreign.md", []byte("[Guide](my guide.md)"), nil, DocumentContext{})
				}, func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) {
					c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
				}, func(c *RuleContext) {
					if flavor == "commonmark" {
						c.input.documentContext.Flavor = "gfm"
					} else {
						c.input.documentContext.Flavor = "commonmark"
					}
				}, func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) }, func(c *RuleContext) { c.options = Options{"unknown": false} }, func(c *RuleContext) { c.options = Options{"allowed_tags": []string{}} },
				func(c *RuleContext) { c.options = Options{"style": nil} }, func(c *RuleContext) { c.options = Options{"style": "invalid"} },
			} {
				c := md011ShapeContext(t, flavor, "[Guide](my guide.md)", context.Background())
				pre, err := c.DestinationWhitespace(context.Background())
				if err != nil || len(pre.Spans) != 1 || pre.Spans[0].Start() != 0 || pre.Spans[0].End() != 20 {
					t.Fatal("actual positive before authority mutation", pre, err)
				}
				invalidate(&c)
				got, err := c.DestinationWhitespace(context.Background())
				if !errors.Is(err, parser.ErrModel) || got.Spans != nil {
					t.Fatal(got, err)
				}
			}
			c := md011ShapeContext(t, flavor, "[Guide](my guide.md)", context.Background())
			//nolint:staticcheck // Deliberately missing caller authority.
			if got, err := c.DestinationWhitespace(nil); !errors.Is(err, parser.ErrModel) || got.Spans != nil {
				t.Fatal(got, err)
			}

		})
	}
}

func TestRuleContextDestinationWhitespaceCallerAndOriginControls(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, source := range []string{"", "clean", "`code`", "<pre>**x**", "[Guide](my guide.md)", "<div>**x** <x a=\""} {
				for _, originating := range []bool{false, true} {
					t.Run(fmt.Sprintf("%q/origin-%v", source, originating), func(t *testing.T) {
						origin, cancelOrigin := context.WithCancel(context.Background())
						defer cancelOrigin()
						c := md011ShapeContext(t, flavor, source, origin)
						caller, cancelCaller := context.WithCancel(context.Background())
						defer cancelCaller()
						if originating {
							cancelOrigin()
						} else {
							cancelCaller()
						}
						got, err := c.DestinationWhitespace(caller)
						if !errors.Is(err, context.Canceled) || got.Spans != nil {
							t.Fatal(got, err)
						}
					})
				}
			}

		})
	}
}

func TestRuleContextDestinationWhitespaceProducerStagesAndFinalAfterPositive(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, gate := range []string{"/parser.(*Document).DestinationWhitespace.func1", ".RuleContext.DestinationWhitespace.func1", ".(*swappedLinkScanner).text", ".(*destinationWhitespaceScanner).flush", ".(*swappedLinkScanner).group", ".(*destinationWhitespaceScanner).destination"} {
				for _, originating := range []bool{false, true} {
					for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
						t.Run(fmt.Sprintf("%s/origin-%v/%v", gate, originating, reason), func(t *testing.T) {
							origin := &md014ProducerFinalStop{Context: context.Background(), reason: reason, gate: gate}
							c := md011ShapeContext(t, flavor, "[Guide](my guide.md)", origin)
							before, err := c.DestinationWhitespace(context.Background())
							if err != nil || len(before.Spans) != 1 || before.Spans[0].Start() != 0 || before.Spans[0].End() != 20 {
								t.Fatal("actual earlier positive precondition", before, err)
							}
							caller := &md014ProducerFinalStop{Context: context.Background(), armed: true, reason: reason, gate: gate}
							var ctx context.Context = caller
							if originating {
								origin.armed = true
								ctx = context.Background()
							}
							got, err := c.DestinationWhitespace(ctx)
							reached := caller.reached
							if originating {
								reached = origin.reached
							}
							if !reached || !errors.Is(err, reason) || got.Spans != nil {
								t.Fatal(got, err, reached)
							}
						})
					}
				}
			}

		})
	}
}

func TestRuleContextDestinationWhitespaceActualCallerAndOriginDeadlines(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, source := range []string{"", "clean", "```sh\n$ pwd\nout\n```\n", "[Guide](my guide.md)", "[Guide](my guide.md)\n<div>\n<x title=\"\n\n[Guide](my guide.md)"} {
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
							c := md011ShapeContext(t, flavor, source, origin)
							if !originating {
								d, cancel := context.WithDeadline(caller, time.Now().Add(time.Second))
								defer cancel()
								caller = d
							}
							time.Sleep(2 * time.Second)
							out, err := c.DestinationWhitespace(caller)
							if !errors.Is(err, context.DeadlineExceeded) || out.Spans != nil {
								t.Fatal(out, err)
							}
						})
					})
				}
			}

		})
	}
}

func TestRuleContextDestinationWhitespaceProducerAndFacadeFinalStickyDiscard(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, gate := range []string{"/parser.(*Document).DestinationWhitespace.func1", ".RuleContext.DestinationWhitespace.func1"} {
				for _, kind := range []string{"work", "nodes", "elapsed"} {
					for _, originating := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/%s/origin-%v", gate, kind, originating), func(t *testing.T) {
							synctest.Test(t, func(t *testing.T) {
								origin := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind}
								c := md011ShapeContext(t, flavor, "[Guide](my guide.md)", origin)
								origin.budget = c.budget
								pre, e := c.DestinationWhitespace(context.Background())
								if e != nil || len(pre.Spans) != 1 || pre.Spans[0].Start() != 0 {
									t.Fatal("positive precondition", pre, e)
								}
								caller := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind, budget: c.budget, armed: true}
								var target context.Context = caller
								if originating {
									origin.armed = true
									target = context.Background()
								}
								for range 2 {
									got, e := c.DestinationWhitespace(target)
									var stop *parser.LimitExceeded
									if !errors.As(e, &stop) || got.Spans != nil || got.Availability != "" {
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

func TestRuleContextDestinationWhitespaceHomogeneousFinalControls(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, gate := range []string{"/parser.(*Document).DestinationWhitespace.func1", ".RuleContext.DestinationWhitespace.func1"} {
				for _, originating := range []bool{false, true} {
					for _, kind := range []string{"work", "nodes", "elapsed"} {
						t.Run(fmt.Sprintf("%s/%s/origin-%v", gate, kind, originating), func(t *testing.T) {
							synctest.Test(t, func(t *testing.T) {
								origin := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind}
								c := md011ShapeContext(t, flavor, "```\n```", origin)
								c.options = nil
								origin.budget = c.budget
								before, e := c.DestinationWhitespace(context.Background())
								if e != nil || before.Availability != parser.Available || len(before.Spans) != 0 {
									t.Fatal(before, e)
								}
								caller := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind, budget: c.budget, armed: true}
								var ctx context.Context = caller
								if originating {
									origin.armed = true
									ctx = context.Background()
								}
								for range 2 {
									got, e := c.DestinationWhitespace(ctx)
									var stop *parser.LimitExceeded
									if !errors.As(e, &stop) || got.Spans != nil || got.Availability != "" || !origin.reached && !caller.reached {
										t.Fatal(got, e)
									}
								}
							})
						})
					}
					for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
						t.Run(fmt.Sprintf("%s/%v/origin-%v", gate, reason, originating), func(t *testing.T) {
							origin := &md014ProducerFinalStop{Context: context.Background(), reason: reason, gate: gate}
							c := md011ShapeContext(t, flavor, "```\n```", origin)
							c.options = nil
							before, e := c.DestinationWhitespace(context.Background())
							if e != nil || before.Availability != parser.Available || len(before.Spans) != 0 {
								t.Fatal(before, e)
							}
							caller := &md014ProducerFinalStop{Context: context.Background(), armed: true, reason: reason, gate: gate}
							var ctx context.Context = caller
							if originating {
								origin.armed = true
								ctx = context.Background()
							}
							got, e := c.DestinationWhitespace(ctx)
							if !errors.Is(e, reason) || got.Spans != nil || got.Availability != "" || !origin.reached && !caller.reached {
								t.Fatal(got, e)
							}
						})
					}
				}
			}

		})
	}
}
