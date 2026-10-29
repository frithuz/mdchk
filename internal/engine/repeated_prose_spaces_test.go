package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
	"testing/synctest"
	"time"
)

func TestRuleContextRepeatedProseSpacesActualAuthority(t *testing.T) {
	c := listContext(t, "a  b", parser.DefaultLimits(), context.Background())
	c.options = Options{"maximum": 1}
	got, err := c.RepeatedProseSpaces(context.Background())
	if err != nil || len(got) != 1 || got[0].Start() != 2 {
		t.Fatal(got, err)
	}
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false },
		func(c *RuleContext) { c.options = Options{"maximum": 0} },
		func(c *RuleContext) { c.options = Options{"maximum": 1, "unknown": true} },
		func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("a  b")) },
		func(c *RuleContext) { c.parsed.Status = parser.Incomplete },
		func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil },
		func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("a  b"))) },
		func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
		func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte("a  b"), nil, DocumentContext{})
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
		if got, err := bad.RepeatedProseSpaces(context.Background()); err == nil || got != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Explicitly reject absent caller authority.
	if _, err := c.RepeatedProseSpaces(nil); err == nil {
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
			if got, err := q.RepeatedProseSpaces(caller); !errors.Is(err, reason) || got != nil {
				t.Fatal(got, err)
			}
		}
	}
}
func TestRuleContextRepeatedProseSpacesOriginalControlsAndFinalWithholding(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			limits := parser.DefaultLimits()
			limits.OperationTime = 20 * time.Millisecond
			q := listContext(t, "a  b", limits, context.Background())
			q.options = Options{"maximum": 1}
			switch kind {
			case "work":
				_ = q.budget.ChargeWork(limits.WorkUnits)
			case "nodes":
				_ = q.budget.ChargeNodes(limits.TotalNodes)
			case "elapsed":
				time.Sleep(25 * time.Millisecond)
			}
			for range 2 {
				got, err := q.RepeatedProseSpaces(context.Background())
				var limit *parser.LimitExceeded
				if !errors.As(err, &limit) || got != nil {
					t.Fatal(kind, got, err)
				}
			}
		}
	})
}
func TestRuleContextRepeatedProseSpacesFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		source string
		final  int
	}{{"", 11}, {"a", 26}, {"```\nx\n```", 17}, {"a  b  c  d  e  f", 65}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			for _, origin := range []bool{false, true} {
				c := listContext(t, tc.source, parser.DefaultLimits(), context.Background())
				c.options = Options{"maximum": 1}
				stop := &proseFacadeStop{Context: context.Background(), stop: tc.final, reason: reason}
				caller := context.Background()
				if origin {
					c.operation = stop
				} else {
					caller = stop
				}
				got, e := c.RepeatedProseSpaces(caller)
				if !errors.Is(e, reason) || got != nil {
					t.Fatal("direct facade Err+nil", got, e)
				}
				if stop.calls != tc.final {
					t.Fatal(stop.calls, tc.final)
				}
			}
		}
	}
}

type proseFacadeStop struct {
	context.Context
	calls, stop int
	reason      error
}

func (c *proseFacadeStop) Err() error {
	c.calls++
	if c.calls >= c.stop {
		return c.reason
	}
	return nil
}
