package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
	"testing/synctest"
	"time"
)

func TestRuleContextTrailingWhitespaceActualAuthority(t *testing.T) {
	c := listContext(t, "a \n", parser.DefaultLimits(), context.Background())
	c.options = Options{"hard_break_spaces": 2, "strict": false, "code_blocks": false}
	got, err := c.TrailingWhitespace(context.Background())
	if err != nil || len(got) != 1 || got[0].Start() != 1 {
		t.Fatal(got, err)
	}
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false },
		func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("a \n")) },
		func(c *RuleContext) { c.parsed.Status = parser.Incomplete },
		func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil },
		func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("a \n"))) },
		func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
		func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte("a \n"), nil, DocumentContext{})
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
		if got, err := bad.TrailingWhitespace(context.Background()); err == nil || got != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Explicitly reject absent caller authority.
	if _, err := c.TrailingWhitespace(nil); err == nil {
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
			if got, err := q.TrailingWhitespace(caller); !errors.Is(err, reason) || got != nil {
				t.Fatal(got, err)
			}
		}
	}
}
func TestRuleContextTrailingWhitespaceOriginalControlsAndFinalWithholding(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			limits := parser.DefaultLimits()
			limits.OperationTime = 20 * time.Millisecond
			q := listContext(t, "a \n", limits, context.Background())
			q.options = Options{"hard_break_spaces": 2, "strict": false, "code_blocks": false}
			switch kind {
			case "work":
				_ = q.budget.ChargeWork(limits.WorkUnits)
			case "nodes":
				_ = q.budget.ChargeNodes(limits.TotalNodes)
			case "elapsed":
				time.Sleep(25 * time.Millisecond)
			}
			for range 2 {
				got, err := q.TrailingWhitespace(context.Background())
				var limit *parser.LimitExceeded
				if !errors.As(err, &limit) || got != nil {
					t.Fatal(kind, got, err)
				}
			}
		}
	})
}

func TestRuleContextTrailingWhitespaceFinalPublication(t *testing.T) {
	// Producer counts + facade entry, scalar-option completion and publication.
	for _, tc := range []struct {
		source       string
		facts, final int
	}{{"", 0, 7}, {"clean\n", 0, 14}, {"a \nb \nc \nd \ne \n", 5, 60}} {
		for _, origin := range []bool{false, true} {
			c := listContext(t, tc.source, parser.DefaultLimits(), context.Background())
			c.options = Options{"hard_break_spaces": 2, "strict": false, "code_blocks": false}
			stop := &terminalCounter{Context: context.Background(), stop: tc.final}
			caller := context.Background()
			if origin {
				c.operation = stop
			} else {
				caller = stop
			}
			got, e := c.TrailingWhitespace(caller)
			if !errors.Is(e, context.Canceled) || got != nil || stop.calls != tc.final {
				t.Fatal("final facade escaped", tc, origin, got, e, stop.calls)
			}
		}
	}
}
