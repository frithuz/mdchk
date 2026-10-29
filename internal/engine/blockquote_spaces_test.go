package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
	"testing/synctest"
	"time"
)

func TestRuleContextBlockquoteSpacesActualAuthority(t *testing.T) {
	c := listContext(t, ">  a\n", parser.DefaultLimits(), context.Background())
	c.options = Options{}
	got, err := c.BlockquoteSpaces(context.Background())
	if err != nil || len(got) != 1 || got[0].Start() != 2 {
		t.Fatal(got, err)
	}
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false },
		func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte(">  a\n")) },
		func(c *RuleContext) { c.parsed.Status = parser.Incomplete },
		func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil },
		func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte(">  a\n"))) },
		func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
		func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte(">  a\n"), nil, DocumentContext{})
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
		if got, err := bad.BlockquoteSpaces(context.Background()); err == nil || got != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Explicitly reject absent caller authority.
	if _, err := c.BlockquoteSpaces(nil); err == nil {
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
			if got, err := q.BlockquoteSpaces(caller); !errors.Is(err, reason) || got != nil {
				t.Fatal(got, err)
			}
		}
	}
}
func TestRuleContextBlockquoteSpacesOriginalControlsAndFinalWithholding(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			limits := parser.DefaultLimits()
			limits.OperationTime = 20 * time.Millisecond
			q := listContext(t, ">  a\n", limits, context.Background())
			q.options = Options{}
			switch kind {
			case "work":
				_ = q.budget.ChargeWork(limits.WorkUnits)
			case "nodes":
				_ = q.budget.ChargeNodes(limits.TotalNodes)
			case "elapsed":
				time.Sleep(25 * time.Millisecond)
			}
			for range 2 {
				got, err := q.BlockquoteSpaces(context.Background())
				var limit *parser.LimitExceeded
				if !errors.As(err, &limit) || got != nil {
					t.Fatal(kind, got, err)
				}
			}
		}
	})
}

func TestRuleContextBlockquoteSpacesFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		source string
		final  int
	}{{"", 8}, {"a\n", 25}, {"```\nx\n```", 18}, {">  a\n>  a\n>  a\n>  a\n>  a\n", 148}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			for _, origin := range []bool{false, true} {
				c := listContext(t, tc.source, parser.DefaultLimits(), context.Background())
				c.options = Options{}
				stop := &blockquoteFacadeStop{Context: context.Background(), stop: tc.final, reason: reason}
				caller := context.Background()
				if origin {
					c.operation = stop
				} else {
					caller = stop
				}
				got, e := c.BlockquoteSpaces(caller)
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

type blockquoteFacadeStop struct {
	context.Context
	calls, stop int
	reason      error
}

func (c *blockquoteFacadeStop) Err() error {
	c.calls++
	if c.calls >= c.stop {
		return c.reason
	}
	return nil
}
