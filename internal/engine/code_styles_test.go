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

func md046ShapeContext(t *testing.T, source string, origin context.Context) RuleContext {
	t.Helper()
	in, err := NewInput("literal.md", []byte(source), nil, DocumentContext{Flavor: "commonmark"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	p := parser.ParseWithOptions(origin, in.source, b, in.documentContext.ParseOptions())
	if p.Status != parser.Complete {
		t.Fatal(p)
	}
	return RuleContext{options: Options{"style": "indented"}, input: in, index: parser.NewLineIndex(in.source), document: p.Document, parsed: p, budget: b, operation: origin, syntaxReliable: true}
}

func TestRuleContextCodeStylesOriginalAuthority(t *testing.T) {
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("x\n```\n```")) }, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("x\n```\n```"))) }, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte("x\n```\n```"), nil, DocumentContext{})
		}, func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		}, func(c *RuleContext) { c.input.documentContext.Flavor = "gfm" }, func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) }, func(c *RuleContext) { c.options = Options{"unknown": false} }, func(c *RuleContext) { c.options = Options{"allowed_tags": []string{}} },
		func(c *RuleContext) { c.options = Options{"style": nil} }, func(c *RuleContext) { c.options = Options{"style": "invalid"} },
	} {
		c := md046ShapeContext(t, "x\n```\n```", context.Background())
		invalidate(&c)
		got, err := c.CodeStyles(context.Background())
		if !errors.Is(err, parser.ErrModel) || got.Facts != nil {
			t.Fatal(got, err)
		}
	}
	c := md046ShapeContext(t, "x\n```\n```", context.Background())
	//nolint:staticcheck // Deliberately missing caller authority.
	if got, err := c.CodeStyles(nil); !errors.Is(err, parser.ErrModel) || got.Facts != nil {
		t.Fatal(got, err)
	}
}

// Original empty parse25W2N, facade583W2N: held-source finite arithmetic.
func TestRuleContextCodeStylesStaticEmptyReservations(t *testing.T) {
	for _, nodes := range []bool{false, true} {
		for _, delta := range []int64{-1, 0} {
			t.Run(fmt.Sprintf("nodes-%v/delta-%d", nodes, delta), func(t *testing.T) {
				c := md046ShapeContext(t, "", context.Background())
				charge, limit, setup, phase := c.budget.ChargeWork, c.budget.Limits().WorkUnits, int64(25), int64(583)
				if nodes {
					charge, limit, setup, phase = c.budget.ChargeNodes, c.budget.Limits().TotalNodes, 2, 2
				}
				if err := charge(limit - setup - phase - delta); err != nil {
					t.Fatal(err)
				}
				got, err := c.CodeStyles(context.Background())
				if delta == 0 {
					if err != nil || got.Availability != parser.Available || len(got.Facts) != 0 {
						t.Fatal(got, err)
					}
				} else {
					for range 2 {
						var stop *parser.LimitExceeded
						if !errors.As(err, &stop) || got.Facts != nil {
							t.Fatal(got, err)
						}
						got, err = c.CodeStyles(context.Background())
					}
				}
			})
		}
	}
}

func TestRuleContextCodeStylesCallerAndOriginControls(t *testing.T) {
	for _, source := range []string{"", "clean", "`code`", "<pre>**x**", "x\n```\n```", "<div>**x** <x a=\""} {
		for _, originating := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/origin-%v", source, originating), func(t *testing.T) {
				origin, cancelOrigin := context.WithCancel(context.Background())
				defer cancelOrigin()
				c := md046ShapeContext(t, source, origin)
				caller, cancelCaller := context.WithCancel(context.Background())
				defer cancelCaller()
				if originating {
					cancelOrigin()
				} else {
					cancelCaller()
				}
				got, err := c.CodeStyles(caller)
				if !errors.Is(err, context.Canceled) || got.Facts != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
}

func TestRuleContextCodeStylesProducerFinalAfterPositive(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).CodeStyles.func1", ".RuleContext.CodeStyles.func1"} {
		for _, originating := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(fmt.Sprintf("origin-%v/%v", originating, reason), func(t *testing.T) {
					origin := &md014ProducerFinalStop{Context: context.Background(), reason: reason, gate: gate}
					c := md046ShapeContext(t, "x\n```\n```", origin)
					before, err := c.CodeStyles(context.Background())
					if err != nil || len(before.Facts) != 1 || before.Facts[0].Span.Start() != 2 || before.Facts[0].Span.End() != 5 {
						t.Fatal("actual earlier positive precondition", before, err)
					}
					caller := &md014ProducerFinalStop{Context: context.Background(), armed: true, reason: reason, gate: gate}
					var ctx context.Context = caller
					if originating {
						origin.armed = true
						ctx = context.Background()
					}
					got, err := c.CodeStyles(ctx)
					reached := caller.reached
					if originating {
						reached = origin.reached
					}
					if !reached || !errors.Is(err, reason) || got.Facts != nil {
						t.Fatal(got, err, reached)
					}
				})
			}
		}
	}

}

func TestRuleContextCodeStylesActualCallerAndOriginDeadlines(t *testing.T) {
	for _, source := range []string{"", "clean", "```sh\n$ pwd\nout\n```\n", "x\n```\n```", "x\n```\n```\n<div>\n<x title=\"\n\nx\n```\n```"} {
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
					c := md046ShapeContext(t, source, origin)
					if !originating {
						d, cancel := context.WithDeadline(caller, time.Now().Add(time.Second))
						defer cancel()
						caller = d
					}
					time.Sleep(2 * time.Second)
					out, err := c.CodeStyles(caller)
					if !errors.Is(err, context.DeadlineExceeded) || out.Facts != nil {
						t.Fatal(out, err)
					}
				})
			})
		}
	}
}

func TestRuleContextCodeStylesProducerAndFacadeFinalStickyDiscard(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).CodeStyles.func1", ".RuleContext.CodeStyles.func1"} {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/origin-%v", gate, kind, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						origin := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind}
						c := md046ShapeContext(t, "x\n```\n```", origin)
						origin.budget = c.budget
						pre, e := c.CodeStyles(context.Background())
						if e != nil || len(pre.Facts) != 1 || pre.Facts[0].Span.Start() != 2 {
							t.Fatal("positive precondition", pre, e)
						}
						caller := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind, budget: c.budget, armed: true}
						var target context.Context = caller
						if originating {
							origin.armed = true
							target = context.Background()
						}
						for range 2 {
							got, e := c.CodeStyles(target)
							var stop *parser.LimitExceeded
							if !errors.As(e, &stop) || got.Facts != nil || got.Availability != "" {
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

func TestRuleContextCodeStylesHomogeneousFinalControls(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).CodeStyles.func1", ".RuleContext.CodeStyles.func1"} {
		for _, originating := range []bool{false, true} {
			for _, kind := range []string{"work", "nodes", "elapsed"} {
				t.Run(fmt.Sprintf("%s/%s/origin-%v", gate, kind, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						origin := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind}
						c := md046ShapeContext(t, "```\n```", origin)
						c.options = Options{"style": "consistent"}
						origin.budget = c.budget
						before, e := c.CodeStyles(context.Background())
						if e != nil || before.Availability != parser.Available || len(before.Facts) != 0 {
							t.Fatal(before, e)
						}
						caller := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind, budget: c.budget, armed: true}
						var ctx context.Context = caller
						if originating {
							origin.armed = true
							ctx = context.Background()
						}
						for range 2 {
							got, e := c.CodeStyles(ctx)
							var stop *parser.LimitExceeded
							if !errors.As(e, &stop) || got.Facts != nil || got.Availability != "" || !origin.reached && !caller.reached {
								t.Fatal(got, e)
							}
						}
					})
				})
			}
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(fmt.Sprintf("%s/%v/origin-%v", gate, reason, originating), func(t *testing.T) {
					origin := &md014ProducerFinalStop{Context: context.Background(), reason: reason, gate: gate}
					c := md046ShapeContext(t, "```\n```", origin)
					c.options = Options{"style": "consistent"}
					before, e := c.CodeStyles(context.Background())
					if e != nil || before.Availability != parser.Available || len(before.Facts) != 0 {
						t.Fatal(before, e)
					}
					caller := &md014ProducerFinalStop{Context: context.Background(), armed: true, reason: reason, gate: gate}
					var ctx context.Context = caller
					if originating {
						origin.armed = true
						ctx = context.Background()
					}
					got, e := c.CodeStyles(ctx)
					if !errors.Is(e, reason) || got.Facts != nil || got.Availability != "" || !origin.reached && !caller.reached {
						t.Fatal(got, e)
					}
				})
			}
		}
	}
}
