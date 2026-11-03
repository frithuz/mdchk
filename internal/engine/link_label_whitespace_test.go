package engine

import (
	"context"
	"errors"
	"fmt"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
	"testing/synctest"
	"time"
)

func TestRuleContextLinkLabelWhitespaceOriginalAuthority(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, invalidate := range []func(*RuleContext){
				func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) {
					c.parsed.Source = parser.NewSource([]byte("[ x](y)"))
				}, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) {
					c.index = parser.NewLineIndex(parser.NewSource([]byte("[ x](y)")))
				}, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) {
					c.input, _ = NewInput("foreign.md", []byte("[ x](y)"), nil, DocumentContext{})
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
				c := md011ShapeContext(t, flavor, "[ x](y)", context.Background())
				pre, err := c.LinkLabelWhitespace(context.Background())
				if err != nil || len(pre.Spans) != 1 || pre.Spans[0].Start() != 1 || pre.Spans[0].End() != 2 {
					t.Fatal("actual positive before authority mutation", pre, err)
				}
				invalidate(&c)
				got, err := c.LinkLabelWhitespace(context.Background())
				if !errors.Is(err, parser.ErrModel) || got.Spans != nil {
					t.Fatal(got, err)
				}
			}
			c := md011ShapeContext(t, flavor, "[ x](y)", context.Background())
			//nolint:staticcheck // Deliberately missing caller authority.
			if got, err := c.LinkLabelWhitespace(nil); !errors.Is(err, parser.ErrModel) || got.Spans != nil {
				t.Fatal(got, err)
			}

		})
	}
}

func TestRuleContextLinkLabelWhitespaceCallerAndOriginControls(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, source := range []string{"", "clean", "`code`", "<pre>**x**", "[ x](y)", "<div>**x** <x a=\""} {
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
						got, err := c.LinkLabelWhitespace(caller)
						if !errors.Is(err, context.Canceled) || got.Spans != nil {
							t.Fatal(got, err)
						}
					})
				}
			}

		})
	}
}

func TestRuleContextLinkLabelWhitespaceProducerStagesAndFinalAfterPositive(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, gate := range []string{"/parser.(*Document).LinkLabelWhitespace.func1", ".RuleContext.LinkLabelWhitespace.func1", "/parser.(*Document).LinkLabelWhitespace.func5"} {
				for _, originating := range []bool{false, true} {
					for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
						t.Run(fmt.Sprintf("%s/origin-%v/%v", gate, originating, reason), func(t *testing.T) {
							origin := &md014ProducerFinalStop{Context: context.Background(), reason: reason, gate: gate}
							c := md011ShapeContext(t, flavor, "[ x](y)", origin)
							before, err := c.LinkLabelWhitespace(context.Background())
							if err != nil || len(before.Spans) != 1 || before.Spans[0].Start() != 1 || before.Spans[0].End() != 2 {
								t.Fatal("actual earlier positive precondition", before, err)
							}
							caller := &md014ProducerFinalStop{Context: context.Background(), armed: true, reason: reason, gate: gate}
							var ctx context.Context = caller
							if originating {
								origin.armed = true
								ctx = context.Background()
							}
							got, err := c.LinkLabelWhitespace(ctx)
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

func TestRuleContextLinkLabelWhitespaceActualDeadlines(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, originating := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/origin-%v", flavor, originating), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					origin, cancelOrigin := context.WithTimeout(context.Background(), time.Second)
					defer cancelOrigin()
					c := md011ShapeContext(t, flavor, "[ x](y)", origin)
					caller, cancelCaller := context.WithTimeout(context.Background(), time.Second)
					defer cancelCaller()
					target := context.Background()
					if !originating {
						c = md011ShapeContext(t, flavor, "[ x](y)", context.Background())
						target = caller
					}
					time.Sleep(2 * time.Second)
					got, e := c.LinkLabelWhitespace(target)
					if !errors.Is(e, context.DeadlineExceeded) || got.Spans != nil {
						t.Fatal(got, e)
					}
				})
			})
		}
	}
}
