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

func TestRuleContextMaximumLineWidthActualAuthority(t *testing.T) {
	c := listContext(t, "aa\n", parser.DefaultLimits(), context.Background())
	c.options = Options{"length": 1, "code_blocks": false, "tables": false, "headings": true}
	got, err := c.MaximumLineWidth(context.Background())
	if err != nil || len(got) != 1 || got[0].Start() != 1 {
		t.Fatal(got, err)
	}
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false },
		func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("aa\n")) },
		func(c *RuleContext) { c.parsed.Status = parser.Incomplete },
		func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil },
		func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("aa\n"))) },
		func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
		func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte("aa\n"), nil, DocumentContext{})
		},
		func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) },
		func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		},
		func(c *RuleContext) { c.input.documentContext.Flavor = "gfm" },
		func(c *RuleContext) {
			c.input.documentContext.Frontmatter = parser.FrontmatterOptions{}
		},
	} {
		bad := c
		invalidate(&bad)
		if got, err := bad.MaximumLineWidth(context.Background()); err == nil || got != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Explicitly reject absent caller authority.
	if _, err := c.MaximumLineWidth(nil); err == nil {
		t.Fatal("nil context")
	}
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, origin := range []bool{false, true} {
			q := c
			caller := context.Background()
			if origin {
				q.operation = &mdTerminalStopped{Context: q.operation, reason: reason}
			} else {
				caller = &mdTerminalStopped{Context: caller, reason: reason}
			}
			if got, err := q.MaximumLineWidth(caller); !errors.Is(err, reason) || got != nil {
				t.Fatal(got, err)
			}
		}
	}
}
func TestRuleContextMaximumLineWidthOriginalControlsAndFinalWithholding(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			limits := parser.DefaultLimits()
			limits.OperationTime = 20 * time.Millisecond
			q := listContext(t, "aa\n", limits, context.Background())
			q.options = Options{"length": 1, "code_blocks": false, "tables": false, "headings": true}
			switch kind {
			case "work":
				_ = q.budget.ChargeWork(limits.WorkUnits)
			case "nodes":
				_ = q.budget.ChargeNodes(limits.TotalNodes)
			case "elapsed":
				time.Sleep(25 * time.Millisecond)
			}
			for range 2 {
				got, err := q.MaximumLineWidth(context.Background())
				var limit *parser.LimitExceeded
				if !errors.As(err, &limit) || got != nil {
					t.Fatal(kind, got, err)
				}
			}
		}
	})
}

// Producer caller counts are fixed by its reservation equation. Facade adds
// entry, admitted-options completion and final publication checks.
func TestRuleContextMaximumLineWidthFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 10}, {"clean", "a\n", 17}, {"protected", "<!-- x -->\n", 29}, {"growth", "aa\naa\naa\naa\naa\n", 63}} {
		for _, origin := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(fmt.Sprintf("%s/%t/%s", tc.name, origin, reason), func(t *testing.T) {
					c := listContext(t, tc.source, parser.DefaultLimits(), context.Background())
					c.options = Options{"length": 1, "code_blocks": false, "tables": false, "headings": true}
					stop := &blankFacadeStop{Context: context.Background(), stop: tc.final, reason: reason}
					caller := context.Background()
					if origin {
						c.operation = stop
					} else {
						caller = stop
					}
					got, e := c.MaximumLineWidth(caller)
					if !errors.Is(e, reason) || got != nil {
						t.Fatal("direct final Err oracle", got, e)
					}
					if stop.calls != tc.final {
						t.Fatal(stop.calls, tc.final)
					}
				})
			}
		}
	}
}

func TestRuleContextMaximumLineWidthRejectsUnadmittedOptions(t *testing.T) {
	for _, options := range []Options{nil, {}, {"length": 0, "code_blocks": false, "tables": false, "headings": true}, {"length": 1, "code_blocks": false, "tables": false}, {"length": 1.0, "code_blocks": false, "tables": false, "headings": true}, {"length": 1, "code_blocks": 0, "tables": false, "headings": true}} {
		c := listContext(t, "aa\n", parser.DefaultLimits(), context.Background())
		c.options = options
		if got, e := c.MaximumLineWidth(context.Background()); e == nil || got != nil {
			t.Fatal(options, got, e)
		}
	}
}
