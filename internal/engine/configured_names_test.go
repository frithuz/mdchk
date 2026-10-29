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

func configuredNamesContext(t *testing.T, source, flavor string) RuleContext {
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
	return RuleContext{input: in, index: parser.NewLineIndex(in.source), document: p.Document, parsed: p, budget: b, operation: context.Background(), syntaxReliable: true, options: Options{"names": []string{"GitHub"}, "code_blocks": false}}
}

func TestRuleContextConfiguredNamesAuthority(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			c := configuredNamesContext(t, "github", flavor)
			got, err := c.ConfiguredNames(context.Background())
			if err != nil || len(got) != 1 {
				t.Fatal(got, err)
			}
			for i, invalidate := range []func(*RuleContext){
				func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("github")) }, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.parsed.Document = nil },
				func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("github"))) },
				func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) {
					c.input, _ = NewInput("foreign.md", []byte("github"), nil, DocumentContext{Flavor: flavor})
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
					got, err := bad.ConfiguredNames(context.Background())
					if !errors.Is(err, parser.ErrModel) || got != nil {
						t.Fatal(got, err)
					}
				})
			}
			//nolint:staticcheck // Deliberately absent caller.
			if got, err := c.ConfiguredNames(nil); !errors.Is(err, parser.ErrModel) || got != nil {
				t.Fatal(got, err)
			}
		})
	}
}

func TestRuleContextConfiguredNamesFinalCallerAndOrigin(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
		names        []string
	}{
		{"no-names", "github", 8, nil}, {"empty", "", 22, []string{"GitHub"}}, {"clean", "GitHub", 77, []string{"GitHub"}},
		{"finding", "github", 79, []string{"GitHub"}}, {"protected", "~~~\ngithub\n~~~", 31, []string{"GitHub"}},
		{"longest", "github actions", 132, []string{"GitHub", "GitHub Actions"}}, {"exact-consumed", "GitHub Actions", 118, []string{"GitHub Actions", "Actions"}},
		{"fold-tie", "GITHUB", 99, []string{"GitHub", "Github"}}, {"growth", "github-github-github", 151, []string{"GitHub"}},
	} {
		for _, origin := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(fmt.Sprintf("%s/origin-%t", tc.name+reason.Error(), origin), func(t *testing.T) {
					c := configuredNamesContext(t, tc.source, "commonmark")
					c.options["names"] = tc.names
					stop := &proseFacadeStop{Context: context.Background(), stop: tc.final, reason: reason}
					caller := context.Context(stop)
					if origin {
						c.operation = stop
						caller = context.Background()
					}
					got, err := c.ConfiguredNames(caller)
					if !errors.Is(err, reason) || got != nil || stop.calls != tc.final {
						t.Fatal(got, err, stop.calls, tc.final)
					}
				})
			}
		}
	}
}

func TestRuleContextConfiguredNamesStickyControls(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := configuredNamesContext(t, "github", "gfm")
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
					got, err := c.ConfiguredNames(context.Background())
					var limit *parser.LimitExceeded
					if !errors.As(err, &limit) || got != nil {
						t.Fatal(got, err)
					}
				}
			})
		})
	}
}
