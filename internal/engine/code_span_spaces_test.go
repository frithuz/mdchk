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

func codeSpanSpacesContext(t *testing.T, source, flavor string) RuleContext {
	t.Helper()
	in, err := NewInput("delimiter.md", []byte(source), nil, DocumentContext{Flavor: flavor})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	p := parser.ParseWithOptions(context.Background(), in.source, b, in.documentContext.ParseOptions())
	if p.Status != parser.Complete {
		t.Fatal(p)
	}
	return RuleContext{input: in, index: parser.NewLineIndex(in.source), document: p.Document, parsed: p, budget: b, operation: context.Background(), syntaxReliable: true}
}

func TestRuleContextCodeSpanSpacesAuthority(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			c := codeSpanSpacesContext(t, "` x `", flavor)
			got, err := c.CodeSpanSpaces(context.Background())
			if err != nil || len(got) != 2 {
				t.Fatal(got, err)
			}
			for i, invalidate := range []func(*RuleContext){
				func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("` x `")) }, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.parsed.Document = nil },
				func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("` x `"))) },
				func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) {
					c.input, _ = NewInput("foreign.md", []byte("` x `"), nil, DocumentContext{Flavor: flavor})
				},
				func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) { c.options = Options{"other": true} },
				func(c *RuleContext) {
					if flavor == "commonmark" {
						c.input.documentContext.Flavor = "gfm"
					} else {
						c.input.documentContext.Flavor = "commonmark"
					}
				}, func(c *RuleContext) {
					fm, _ := parser.NewFrontmatterOptions(true, nil)
					c.input.documentContext.Frontmatter = fm
				},
			} {
				t.Run(fmt.Sprint(i), func(t *testing.T) {
					bad := c
					invalidate(&bad)
					got, err := bad.CodeSpanSpaces(context.Background())
					if !errors.Is(err, parser.ErrModel) || got != nil {
						t.Fatal(got, err)
					}
				})
			}
			//nolint:staticcheck // Deliberately absent caller.
			if got, err := c.CodeSpanSpaces(nil); !errors.Is(err, parser.ErrModel) || got != nil {
				t.Fatal(got, err)
			}
		})
	}
}

func TestRuleContextCodeSpanSpacesFinalCallerAndOrigin(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 10}, {"clean", "---", 14}, {"protected", "```\nx\n```", 14}, {"findings", "` x `", 38}, {"growth", "` x ` ` y ` ` z `", 95}} {
		for _, origin := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(fmt.Sprintf("%s/origin-%t", tc.name+reason.Error(), origin), func(t *testing.T) {
					c := codeSpanSpacesContext(t, tc.source, "commonmark")
					stop := &proseFacadeStop{Context: context.Background(), stop: tc.final, reason: reason}
					caller := context.Context(stop)
					if origin {
						c.operation = stop
						caller = context.Background()
					}
					got, err := c.CodeSpanSpaces(caller)
					if !errors.Is(err, reason) || got != nil || stop.calls != tc.final {
						t.Fatal(got, err, stop.calls, tc.final)
					}
				})
			}
		}
	}
}

func TestRuleContextCodeSpanSpacesStickyControls(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := codeSpanSpacesContext(t, "` x `", "gfm")
				limits := c.budget.Limits()
				switch kind {
				case "work":
					_ = c.budget.ChargeWork(limits.WorkUnits)
				case "nodes":
					_ = c.budget.ChargeNodes(limits.TotalNodes)
				case "elapsed":
					time.Sleep(limits.OperationTime + time.Millisecond)
				}
				for range 2 {
					got, err := c.CodeSpanSpaces(context.Background())
					var limit *parser.LimitExceeded
					if !errors.As(err, &limit) || got != nil {
						t.Fatal(got, err)
					}
				}
			})
		})
	}
}
