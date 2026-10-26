package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
	"testing/synctest"
	"time"
)

func TestRuleContextListItemSpacingActualAuthority(t *testing.T) {
	c := listContext(t, "- A\n\n- B\n- C", parser.DefaultLimits(), context.Background())
	c.options = Options{"style": "consistent"}
	got, err := c.ListItemSpacing(context.Background())
	if err != nil || len(got) != 1 || got[0].Span.Start() != 9 {
		t.Fatal(got, err)
	}
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false },
		func(c *RuleContext) { c.options = Options{"style": "Loose"} },
		func(c *RuleContext) { c.options = nil },
		func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("- A\n\n- B\n- C")) },
		func(c *RuleContext) { c.parsed.Status = parser.Incomplete },
		func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil },
		func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("- A\n\n- B\n- C"))) },
		func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
		func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte("- A\n\n- B\n- C"), nil, DocumentContext{})
		},
		func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) },
		func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		},
		func(c *RuleContext) { c.input.documentContext.Flavor = "gfm" },
		func(c *RuleContext) { c.input.documentContext.Frontmatter = parser.FrontmatterOptions{} },
	} {
		bad := c
		invalidate(&bad)
		if got, err := bad.ListItemSpacing(context.Background()); err == nil || got != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Explicitly reject absent caller authority.
	if _, err := c.ListItemSpacing(nil); err == nil {
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
			if got, err := q.ListItemSpacing(caller); !errors.Is(err, reason) || got != nil {
				t.Fatal(got, err)
			}
		}
	}
}
func TestRuleContextListItemSpacingOriginalControlsAndFinalWithholding(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			limits := parser.DefaultLimits()
			limits.OperationTime = 20 * time.Millisecond
			q := listContext(t, "- A\n\n- B\n- C", limits, context.Background())
			q.options = Options{"style": "consistent"}
			switch kind {
			case "work":
				_ = q.budget.ChargeWork(limits.WorkUnits)
			case "nodes":
				_ = q.budget.ChargeNodes(limits.TotalNodes)
			case "elapsed":
				time.Sleep(25 * time.Millisecond)
			}
			for range 2 {
				got, err := q.ListItemSpacing(context.Background())
				var limit *parser.LimitExceeded
				if !errors.As(err, &limit) || got != nil {
					t.Fatal(kind, got, err)
				}
			}
		}
	})
}

// Counts include facade entry, immutable style admission, the complete producer,
// and final facade publication. They are fixed before the first freeze.
func TestRuleContextListItemSpacingFixedFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		source       string
		facts, final int
	}{{"", 0, 8}, {"- A\n- B", 0, 43}, {"- A\n\n- B\n- C", 1, 63}} {
		for _, origin := range []bool{false, true} {
			c := listContext(t, tc.source, parser.DefaultLimits(), context.Background())
			c.options = Options{"style": "consistent"}
			got, e := c.ListItemSpacing(context.Background())
			if e != nil || len(got) != tc.facts {
				t.Fatal(got, e)
			}
			stop := &terminalCounter{Context: context.Background(), stop: tc.final}
			caller := context.Background()
			if origin {
				c.operation = stop
			} else {
				caller = stop
			}
			got, e = c.ListItemSpacing(caller)
			if !errors.Is(e, context.Canceled) || got != nil || stop.calls != tc.final {
				t.Fatal("final facade escaped", got, e, stop.calls)
			}
		}
	}
}
