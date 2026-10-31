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

func md031ShapeContext(t *testing.T, source string, origin context.Context) RuleContext {
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
	return RuleContext{options: Options{}, input: in, index: parser.NewLineIndex(in.source), document: p.Document, parsed: p, budget: b, operation: origin, syntaxReliable: true}
}

func TestRuleContextFencedSpacingOriginalAuthority(t *testing.T) {
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("x\n```\n```")) }, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("x\n```\n```"))) }, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte("x\n```\n```"), nil, DocumentContext{})
		}, func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		}, func(c *RuleContext) { c.input.documentContext.Flavor = "gfm" }, func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) }, func(c *RuleContext) { c.options = Options{"unknown": false} }, func(c *RuleContext) { c.options = Options{"allowed_tags": []string{}} },
		func(c *RuleContext) { c.options = Options{"languages": nil} }, func(c *RuleContext) { c.options = Options{"languages": "sh"} },
	} {
		c := md031ShapeContext(t, "x\n```\n```", context.Background())
		invalidate(&c)
		got, err := c.FencedSpacing(context.Background())
		if !errors.Is(err, parser.ErrModel) || got.Facts != nil {
			t.Fatal(got, err)
		}
	}
	c := md031ShapeContext(t, "x\n```\n```", context.Background())
	//nolint:staticcheck // Deliberately missing caller authority.
	if got, err := c.FencedSpacing(nil); !errors.Is(err, parser.ErrModel) || got.Facts != nil {
		t.Fatal(got, err)
	}
}

// Original empty parse25W2N, facade530W2N: held-source finite arithmetic.
func TestRuleContextFencedSpacingStaticEmptyReservations(t *testing.T) {
	for _, nodes := range []bool{false, true} {
		for _, delta := range []int64{-1, 0} {
			t.Run(fmt.Sprintf("nodes-%v/delta-%d", nodes, delta), func(t *testing.T) {
				c := md031ShapeContext(t, "", context.Background())
				charge, limit, setup, phase := c.budget.ChargeWork, c.budget.Limits().WorkUnits, int64(25), int64(530)
				if nodes {
					charge, limit, setup, phase = c.budget.ChargeNodes, c.budget.Limits().TotalNodes, 2, 2
				}
				if err := charge(limit - setup - phase - delta); err != nil {
					t.Fatal(err)
				}
				got, err := c.FencedSpacing(context.Background())
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
						got, err = c.FencedSpacing(context.Background())
					}
				}
			})
		}
	}
}

func TestRuleContextFencedSpacingCallerAndOriginControls(t *testing.T) {
	for _, source := range []string{"", "clean", "`code`", "<pre>**x**", "x\n```\n```", "<div>**x** <x a=\""} {
		for _, originating := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/origin-%v", source, originating), func(t *testing.T) {
				origin, cancelOrigin := context.WithCancel(context.Background())
				defer cancelOrigin()
				c := md031ShapeContext(t, source, origin)
				caller, cancelCaller := context.WithCancel(context.Background())
				defer cancelCaller()
				if originating {
					cancelOrigin()
				} else {
					cancelCaller()
				}
				got, err := c.FencedSpacing(caller)
				if !errors.Is(err, context.Canceled) || got.Facts != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
}

func TestRuleContextFencedSpacingProducerFinalAfterPositive(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).FencedSpacing.func1", ".RuleContext.FencedSpacing.func1"} {
		for _, originating := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(fmt.Sprintf("origin-%v/%v", originating, reason), func(t *testing.T) {
					origin := &md014ProducerFinalStop{Context: context.Background(), reason: reason, gate: gate}
					c := md031ShapeContext(t, "x\n```\n```", origin)
					before, err := c.FencedSpacing(context.Background())
					if err != nil || len(before.Facts) != 1 || before.Facts[0].Point.Start() != 2 || before.Facts[0].Point.End() != 2 {
						t.Fatal("actual earlier positive precondition", before, err)
					}
					caller := &md014ProducerFinalStop{Context: context.Background(), armed: true, reason: reason, gate: gate}
					var ctx context.Context = caller
					if originating {
						origin.armed = true
						ctx = context.Background()
					}
					got, err := c.FencedSpacing(ctx)
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

func TestRuleContextFencedSpacingActualCallerAndOriginDeadlines(t *testing.T) {
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
					c := md031ShapeContext(t, source, origin)
					if !originating {
						d, cancel := context.WithDeadline(caller, time.Now().Add(time.Second))
						defer cancel()
						caller = d
					}
					time.Sleep(2 * time.Second)
					out, err := c.FencedSpacing(caller)
					if !errors.Is(err, context.DeadlineExceeded) || out.Facts != nil {
						t.Fatal(out, err)
					}
				})
			})
		}
	}
}

func TestRuleContextFencedSpacingProducerAndFacadeFinalStickyDiscard(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).FencedSpacing.func1", ".RuleContext.FencedSpacing.func1"} {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/origin-%v", gate, kind, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						origin := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind}
						c := md031ShapeContext(t, "x\n```\n```", origin)
						origin.budget = c.budget
						pre, e := c.FencedSpacing(context.Background())
						if e != nil || len(pre.Facts) != 1 || pre.Facts[0].Point.Start() != 2 {
							t.Fatal("positive precondition", pre, e)
						}
						caller := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind, budget: c.budget, armed: true}
						var target context.Context = caller
						if originating {
							origin.armed = true
							target = context.Background()
						}
						for range 2 {
							got, e := c.FencedSpacing(target)
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
