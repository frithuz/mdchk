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

const md075FacadeExample = "A | B\n1 | 2\n"

func md075ShapeContext(t *testing.T, source string, ctx context.Context) RuleContext {
	return md011ShapeContext(t, "gfm", source, ctx)
}

func md075FacadePositive(t *testing.T, got parser.PipeRowCandidatesInventory, err error) {
	t.Helper()
	if err != nil || got.Availability != parser.Available || len(got.Rows) != 2 || got.Rows[1] != (parser.PipeRowCandidate{Start: 6, End: 11, Line: 2, Cells: 2}) {
		t.Fatal("actual positive", got, err)
	}
}

func TestRuleContextPipeRowCandidatesOptionless(t *testing.T) {
	for _, options := range []Options{nil, {}} {
		c := md075ShapeContext(t, md075FacadeExample, context.Background())
		c.options = options
		got, err := c.PipeRowCandidates(context.Background())
		md075FacadePositive(t, got, err)
	}
	c := md011ShapeContext(t, "commonmark", md075FacadeExample, context.Background())
	// The fact classifies Paragraph lines in every flavor; MD075 applies only to GFM.
	if got, err := c.PipeRowCandidates(context.Background()); err != nil || got.Availability != parser.Available || len(got.Rows) != 2 {
		t.Fatal("CommonMark paragraph lines", got, err)
	}
}

func TestRuleContextPipeRowCandidatesOriginalAuthority(t *testing.T) {
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) {
			c.parsed.Source = parser.NewSource([]byte(md075FacadeExample))
		}, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) {
			c.index = parser.NewLineIndex(parser.NewSource([]byte(md075FacadeExample)))
		}, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte(md075FacadeExample), nil, DocumentContext{})
		}, func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		}, func(c *RuleContext) { c.input.documentContext.Flavor = "commonmark" },
		func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) },
		// MD075 admits no rule-specific option.
		func(c *RuleContext) { c.options = Options{"style": "consistent"} }, func(c *RuleContext) { c.options = Options{"unknown": false} },
		func(c *RuleContext) { c.options = Options{"enabled": true} },
	} {
		c := md075ShapeContext(t, md075FacadeExample, context.Background())
		pre, err := c.PipeRowCandidates(context.Background())
		md075FacadePositive(t, pre, err)
		invalidate(&c)
		got, err := c.PipeRowCandidates(context.Background())
		if !errors.Is(err, parser.ErrModel) || got.Rows != nil || got.Availability != "" {
			t.Fatal(got, err)
		}
	}
	c := md075ShapeContext(t, md075FacadeExample, context.Background())
	//nolint:staticcheck // Deliberately missing caller authority.
	if got, err := c.PipeRowCandidates(nil); !errors.Is(err, parser.ErrModel) || got.Rows != nil {
		t.Fatal(got, err)
	}
}

func TestRuleContextPipeRowCandidatesCallerAndOriginControls(t *testing.T) {
	for _, source := range []string{"", "clean", "```\na | b\n--- | ---\n```\n", md075FacadeExample, "> a | b\n> 1 | 2\n"} {
		for _, originating := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/origin-%v", source, originating), func(t *testing.T) {
				origin, cancelOrigin := context.WithCancel(context.Background())
				defer cancelOrigin()
				c := md075ShapeContext(t, source, origin)
				caller, cancelCaller := context.WithCancel(context.Background())
				defer cancelCaller()
				if originating {
					cancelOrigin()
				} else {
					cancelCaller()
				}
				got, err := c.PipeRowCandidates(caller)
				if !errors.Is(err, context.Canceled) || got.Rows != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
}

func TestRuleContextPipeRowCandidatesProducerStagesAndFinalAfterPositive(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).PipeRowCandidates.func1", ".RuleContext.PipeRowCandidates.func1", "/parser.(*Document).PipeRowCandidates.func2"} {
		for _, originating := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(fmt.Sprintf("%s/origin-%v/%v", gate, originating, reason), func(t *testing.T) {
					origin := &md014ProducerFinalStop{Context: context.Background(), reason: reason, gate: gate}
					c := md075ShapeContext(t, md075FacadeExample, origin)
					before, err := c.PipeRowCandidates(context.Background())
					md075FacadePositive(t, before, err)
					caller := &md014ProducerFinalStop{Context: context.Background(), armed: true, reason: reason, gate: gate}
					var ctx context.Context = caller
					if originating {
						origin.armed = true
						ctx = context.Background()
					}
					got, err := c.PipeRowCandidates(ctx)
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

func TestRuleContextPipeRowCandidatesActualCallerAndOriginDeadlines(t *testing.T) {
	for _, source := range []string{"", "clean", md075FacadeExample} {
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
					c := md075ShapeContext(t, source, origin)
					if !originating {
						d, cancel := context.WithDeadline(caller, time.Now().Add(time.Second))
						defer cancel()
						caller = d
					}
					time.Sleep(2 * time.Second)
					out, err := c.PipeRowCandidates(caller)
					if !errors.Is(err, context.DeadlineExceeded) || out.Rows != nil {
						t.Fatal(out, err)
					}
				})
			})
		}
	}
}

func TestRuleContextPipeRowCandidatesProducerAndFacadeFinalStickyDiscard(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).PipeRowCandidates.func1", ".RuleContext.PipeRowCandidates.func1"} {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/origin-%v", gate, kind, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						origin := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind}
						c := md075ShapeContext(t, md075FacadeExample, origin)
						origin.budget = c.budget
						pre, e := c.PipeRowCandidates(context.Background())
						md075FacadePositive(t, pre, e)
						caller := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind, budget: c.budget, armed: true}
						var target context.Context = caller
						if originating {
							origin.armed = true
							target = context.Background()
						}
						for range 2 {
							got, e := c.PipeRowCandidates(target)
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

func TestRuleContextPipeRowCandidatesExhaustedBudget(t *testing.T) {
	for _, kind := range []string{"work", "nodes"} {
		t.Run(kind, func(t *testing.T) {
			c := md075ShapeContext(t, md075FacadeExample, context.Background())
			pre, err := c.PipeRowCandidates(context.Background())
			md075FacadePositive(t, pre, err)
			if kind == "work" {
				_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
			} else {
				_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes)
			}
			for range 2 {
				got, err := c.PipeRowCandidates(context.Background())
				var stop *parser.LimitExceeded
				if !errors.As(err, &stop) || got.Rows != nil || got.Availability != "" {
					t.Fatal(got, err)
				}
			}
		})
	}
}
