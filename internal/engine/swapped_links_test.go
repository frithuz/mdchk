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

func md011ShapeContext(t *testing.T, flavor, source string, origin context.Context) RuleContext {
	t.Helper()
	in, err := NewInput("literal.md", []byte(source), nil, DocumentContext{Flavor: flavor})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	p := parser.ParseWithOptions(origin, in.source, b, in.documentContext.ParseOptions())
	if p.Status != parser.Complete {
		t.Fatal(p)
	}
	return RuleContext{options: nil, input: in, index: parser.NewLineIndex(in.source), document: p.Document, parsed: p, budget: b, operation: origin, syntaxReliable: true}
}

func TestRuleContextSwappedLinksStaticEmptyReservations(t *testing.T) {
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
					got, err := c.SwappedLinks(context.Background())
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
						got, err = c.SwappedLinks(context.Background())
					}
				})
			}

		})
	}
}

func TestRuleContextSwappedLinksOriginalAuthority(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, invalidate := range []func(*RuleContext){
				func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) {
					c.parsed.Source = parser.NewSource([]byte("(Guide)[https://example.org]"))
				}, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) {
					c.index = parser.NewLineIndex(parser.NewSource([]byte("(Guide)[https://example.org]")))
				}, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) {
					c.input, _ = NewInput("foreign.md", []byte("(Guide)[https://example.org]"), nil, DocumentContext{})
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
				c := md011ShapeContext(t, flavor, "(Guide)[https://example.org]", context.Background())
				pre, err := c.SwappedLinks(context.Background())
				if err != nil || len(pre.Spans) != 1 || pre.Spans[0].Start() != 0 || pre.Spans[0].End() != 28 {
					t.Fatal("actual positive before authority mutation", pre, err)
				}
				invalidate(&c)
				got, err := c.SwappedLinks(context.Background())
				if !errors.Is(err, parser.ErrModel) || got.Spans != nil {
					t.Fatal(got, err)
				}
			}
			c := md011ShapeContext(t, flavor, "(Guide)[https://example.org]", context.Background())
			//nolint:staticcheck // Deliberately missing caller authority.
			if got, err := c.SwappedLinks(nil); !errors.Is(err, parser.ErrModel) || got.Spans != nil {
				t.Fatal(got, err)
			}

		})
	}
}

func TestRuleContextSwappedLinksCallerAndOriginControls(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, source := range []string{"", "clean", "`code`", "<pre>**x**", "(Guide)[https://example.org]", "<div>**x** <x a=\""} {
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
						got, err := c.SwappedLinks(caller)
						if !errors.Is(err, context.Canceled) || got.Spans != nil {
							t.Fatal(got, err)
						}
					})
				}
			}

		})
	}
}

func TestRuleContextSwappedLinksProducerStagesAndFinalAfterPositive(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, gate := range []string{"/parser.(*Document).SwappedLinks.func1", ".RuleContext.SwappedLinks.func1", ".(*swappedLinkScanner).text", ".(*swappedLinkScanner).flush", ".(*swappedLinkScanner).validURL", ".(*swappedLinkScanner).group"} {
				for _, originating := range []bool{false, true} {
					for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
						t.Run(fmt.Sprintf("%s/origin-%v/%v", gate, originating, reason), func(t *testing.T) {
							origin := &md014ProducerFinalStop{Context: context.Background(), reason: reason, gate: gate}
							c := md011ShapeContext(t, flavor, "(Guide)[https://example.org]", origin)
							before, err := c.SwappedLinks(context.Background())
							if err != nil || len(before.Spans) != 1 || before.Spans[0].Start() != 0 || before.Spans[0].End() != 28 {
								t.Fatal("actual earlier positive precondition", before, err)
							}
							caller := &md014ProducerFinalStop{Context: context.Background(), armed: true, reason: reason, gate: gate}
							var ctx context.Context = caller
							if originating {
								origin.armed = true
								ctx = context.Background()
							}
							got, err := c.SwappedLinks(ctx)
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

func TestRuleContextSwappedLinksActualCallerAndOriginDeadlines(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, source := range []string{"", "clean", "```sh\n$ pwd\nout\n```\n", "(Guide)[https://example.org]", "(Guide)[https://example.org]\n<div>\n<x title=\"\n\n(Guide)[https://example.org]"} {
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
							out, err := c.SwappedLinks(caller)
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

func TestRuleContextSwappedLinksProducerAndFacadeFinalStickyDiscard(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, gate := range []string{"/parser.(*Document).SwappedLinks.func1", ".RuleContext.SwappedLinks.func1"} {
				for _, kind := range []string{"work", "nodes", "elapsed"} {
					for _, originating := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/%s/origin-%v", gate, kind, originating), func(t *testing.T) {
							synctest.Test(t, func(t *testing.T) {
								origin := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind}
								c := md011ShapeContext(t, flavor, "(Guide)[https://example.org]", origin)
								origin.budget = c.budget
								pre, e := c.SwappedLinks(context.Background())
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
									got, e := c.SwappedLinks(target)
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

func TestRuleContextSwappedLinksHomogeneousFinalControls(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, gate := range []string{"/parser.(*Document).SwappedLinks.func1", ".RuleContext.SwappedLinks.func1"} {
				for _, originating := range []bool{false, true} {
					for _, kind := range []string{"work", "nodes", "elapsed"} {
						t.Run(fmt.Sprintf("%s/%s/origin-%v", gate, kind, originating), func(t *testing.T) {
							synctest.Test(t, func(t *testing.T) {
								origin := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind}
								c := md011ShapeContext(t, flavor, "```\n```", origin)
								c.options = nil
								origin.budget = c.budget
								before, e := c.SwappedLinks(context.Background())
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
									got, e := c.SwappedLinks(ctx)
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
							before, e := c.SwappedLinks(context.Background())
							if e != nil || before.Availability != parser.Available || len(before.Spans) != 0 {
								t.Fatal(before, e)
							}
							caller := &md014ProducerFinalStop{Context: context.Background(), armed: true, reason: reason, gate: gate}
							var ctx context.Context = caller
							if originating {
								origin.armed = true
								ctx = context.Background()
							}
							got, e := c.SwappedLinks(ctx)
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
