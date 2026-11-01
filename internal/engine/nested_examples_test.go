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

func md070ShapeContext(t *testing.T, source string, origin context.Context) RuleContext {
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
	return RuleContext{options: nil, input: in, index: parser.NewLineIndex(in.source), document: p.Document, parsed: p, budget: b, operation: origin, syntaxReliable: true}
}

func TestRuleContextNestedExamplesStaticEmptyReservations(t *testing.T) {
	for _, delta := range []int64{-1, 0} {
		t.Run(fmt.Sprintf("delta-%d", delta), func(t *testing.T) {
			c := md070ShapeContext(t, "", context.Background())
			// Frozen independently: original empty parse25W, facade+producer6W.
			if err := c.budget.ChargeWork(c.budget.Limits().WorkUnits - 25 - 6 - delta); err != nil {
				t.Fatal(err)
			}
			got, err := c.NestedExamples(context.Background())
			if delta == 0 {
				if err != nil || got.Availability != parser.Available || len(got.Facts) != 0 {
					t.Fatal(got, err)
				}
				return
			}
			for range 2 {
				var stop *parser.LimitExceeded
				if !errors.As(err, &stop) || got.Facts != nil {
					t.Fatal(got, err)
				}
				got, err = c.NestedExamples(context.Background())
			}
		})
	}
}

func TestRuleContextNestedExamplesOriginalAuthority(t *testing.T) {
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) {
			c.parsed.Source = parser.NewSource([]byte("<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->"))
		}, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) {
			c.index = parser.NewLineIndex(parser.NewSource([]byte("<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->")))
		}, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte("<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->"), nil, DocumentContext{})
		}, func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		}, func(c *RuleContext) { c.input.documentContext.Flavor = "gfm" }, func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) }, func(c *RuleContext) { c.options = Options{"unknown": false} }, func(c *RuleContext) { c.options = Options{"allowed_tags": []string{}} },
		func(c *RuleContext) { c.options = Options{"style": nil} }, func(c *RuleContext) { c.options = Options{"style": "invalid"} },
	} {
		c := md070ShapeContext(t, "<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->", context.Background())
		invalidate(&c)
		got, err := c.NestedExamples(context.Background())
		if !errors.Is(err, parser.ErrModel) || got.Facts != nil {
			t.Fatal(got, err)
		}
	}
	c := md070ShapeContext(t, "<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->", context.Background())
	//nolint:staticcheck // Deliberately missing caller authority.
	if got, err := c.NestedExamples(nil); !errors.Is(err, parser.ErrModel) || got.Facts != nil {
		t.Fatal(got, err)
	}
}

func TestRuleContextNestedExamplesCallerAndOriginControls(t *testing.T) {
	for _, source := range []string{"", "clean", "`code`", "<pre>**x**", "<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->", "<div>**x** <x a=\""} {
		for _, originating := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/origin-%v", source, originating), func(t *testing.T) {
				origin, cancelOrigin := context.WithCancel(context.Background())
				defer cancelOrigin()
				c := md070ShapeContext(t, source, origin)
				caller, cancelCaller := context.WithCancel(context.Background())
				defer cancelCaller()
				if originating {
					cancelOrigin()
				} else {
					cancelCaller()
				}
				got, err := c.NestedExamples(caller)
				if !errors.Is(err, context.Canceled) || got.Facts != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
}

func TestRuleContextNestedExamplesProducerStagesAndFinalAfterPositive(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).NestedExamples.func1", ".RuleContext.NestedExamples.func1", ".(*nestedExampleModel).prepare", ".(*nestedExampleModel).pairs", ".nestedExamplePayload.mappedMarker", ".parseConfigured"} {
		for _, originating := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(fmt.Sprintf("%s/origin-%v/%v", gate, originating, reason), func(t *testing.T) {
					origin := &md014ProducerFinalStop{Context: context.Background(), reason: reason, gate: gate}
					c := md070ShapeContext(t, "<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->", origin)
					before, err := c.NestedExamples(context.Background())
					if err != nil || len(before.Facts) != 1 || before.Facts[0].Span.Start() != 23 || before.Facts[0].Span.End() != 26 {
						t.Fatal("actual earlier positive precondition", before, err)
					}
					caller := &md014ProducerFinalStop{Context: context.Background(), armed: true, reason: reason, gate: gate}
					var ctx context.Context = caller
					if originating {
						origin.armed = true
						ctx = context.Background()
					}
					got, err := c.NestedExamples(ctx)
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

func TestRuleContextNestedExamplesActualCallerAndOriginDeadlines(t *testing.T) {
	for _, source := range []string{"", "clean", "```sh\n$ pwd\nout\n```\n", "<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->", "<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->\n<div>\n<x title=\"\n\n<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->"} {
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
					c := md070ShapeContext(t, source, origin)
					if !originating {
						d, cancel := context.WithDeadline(caller, time.Now().Add(time.Second))
						defer cancel()
						caller = d
					}
					time.Sleep(2 * time.Second)
					out, err := c.NestedExamples(caller)
					if !errors.Is(err, context.DeadlineExceeded) || out.Facts != nil {
						t.Fatal(out, err)
					}
				})
			})
		}
	}
}

func TestRuleContextNestedExamplesProducerAndFacadeFinalStickyDiscard(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).NestedExamples.func1", ".RuleContext.NestedExamples.func1"} {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/origin-%v", gate, kind, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						origin := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind}
						c := md070ShapeContext(t, "<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->", origin)
						origin.budget = c.budget
						pre, e := c.NestedExamples(context.Background())
						if e != nil || len(pre.Facts) != 1 || pre.Facts[0].Span.Start() != 23 {
							t.Fatal("positive precondition", pre, e)
						}
						caller := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind, budget: c.budget, armed: true}
						var target context.Context = caller
						if originating {
							origin.armed = true
							target = context.Background()
						}
						for range 2 {
							got, e := c.NestedExamples(target)
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

func TestRuleContextNestedExamplesHomogeneousFinalControls(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).NestedExamples.func1", ".RuleContext.NestedExamples.func1"} {
		for _, originating := range []bool{false, true} {
			for _, kind := range []string{"work", "nodes", "elapsed"} {
				t.Run(fmt.Sprintf("%s/%s/origin-%v", gate, kind, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						origin := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind}
						c := md070ShapeContext(t, "```\n```", origin)
						c.options = nil
						origin.budget = c.budget
						before, e := c.NestedExamples(context.Background())
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
							got, e := c.NestedExamples(ctx)
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
					c := md070ShapeContext(t, "```\n```", origin)
					c.options = nil
					before, e := c.NestedExamples(context.Background())
					if e != nil || before.Availability != parser.Available || len(before.Facts) != 0 {
						t.Fatal(before, e)
					}
					caller := &md014ProducerFinalStop{Context: context.Background(), armed: true, reason: reason, gate: gate}
					var ctx context.Context = caller
					if originating {
						origin.armed = true
						ctx = context.Background()
					}
					got, e := c.NestedExamples(ctx)
					if !errors.Is(e, reason) || got.Facts != nil || got.Availability != "" || !origin.reached && !caller.reached {
						t.Fatal(got, e)
					}
				})
			}
		}
	}
}
