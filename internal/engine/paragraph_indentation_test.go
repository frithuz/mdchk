package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
	"testing/synctest"
	"time"
)

func TestRuleContextParagraphIndentationActualAuthority(t *testing.T) {
	c := listContext(t, "a\n  b", parser.DefaultLimits(), context.Background())
	c.options = Options{}
	got, err := c.ParagraphIndentation(context.Background())
	if err != nil || len(got) != 1 || got[0].Start() != 2 {
		t.Fatal(got, err)
	}
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false },
		func(c *RuleContext) { c.options = Options{"unknown": true} },
		func(c *RuleContext) { c.options = Options{"maximum": 1, "unknown": true} },
		func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("a\n  b")) },
		func(c *RuleContext) { c.parsed.Status = parser.Incomplete },
		func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil },
		func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("a\n  b"))) },
		func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
		func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte("a\n  b"), nil, DocumentContext{})
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
		if got, err := bad.ParagraphIndentation(context.Background()); err == nil || got != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Explicitly reject absent caller authority.
	if _, err := c.ParagraphIndentation(nil); err == nil {
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
			if got, err := q.ParagraphIndentation(caller); !errors.Is(err, reason) || got != nil {
				t.Fatal(got, err)
			}
		}
	}
}
func TestRuleContextParagraphIndentationOriginalControlsAndFinalWithholding(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			limits := parser.DefaultLimits()
			limits.OperationTime = 20 * time.Millisecond
			q := listContext(t, "a\n  b", limits, context.Background())
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
				got, err := q.ParagraphIndentation(context.Background())
				var limit *parser.LimitExceeded
				if !errors.As(err, &limit) || got != nil {
					t.Fatal(kind, got, err)
				}
			}
		}
	})
}
func TestRuleContextParagraphIndentationFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		source string
		final  int
	}{{"", 10}, {"a", 29}, {"```\nx\n```", 18}, {"a\n  b\n  c\n  d\n  e\n  f", 135}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			for _, origin := range []bool{false, true} {
				c := listContext(t, tc.source, parser.DefaultLimits(), context.Background())
				c.options = Options{}
				stop := &paragraphFacadeStop{Context: context.Background(), stop: tc.final, reason: reason}
				caller := context.Background()
				if origin {
					c.operation = stop
				} else {
					caller = stop
				}
				got, e := c.ParagraphIndentation(caller)
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

type paragraphFacadeStop struct {
	context.Context
	calls, stop int
	reason      error
}

func (c *paragraphFacadeStop) Err() error {
	c.calls++
	if c.calls >= c.stop {
		return c.reason
	}
	return nil
}

// Static producer equations plus the facade's one work reservation. The probe
// observes only previously consumed parse work, never a successful projection.
func TestRuleContextParagraphIndentationExactBoundaries(t *testing.T) {
	for _, tc := range []struct {
		source      string
		work, nodes int64
		facts       int
	}{
		{"", 7, 4, 0}, {"a", 44, 15, 0}, {"```\nx\n```", 19, 11, 0}, {"a\n  b", 91, 30, 1}, {"a\n  b\n  c\n  d\n  e\n  f", 280, 84, 5},
	} {
		for _, kind := range []string{"work", "nodes"} {
			for _, delta := range []int64{-1, 0} {
				probe := listContext(t, tc.source, parser.DefaultLimits(), context.Background())
				limit := probe.budget.Limits().WorkUnits
				charge := probe.budget.ChargeWork
				remaining := tc.work
				if kind == "nodes" {
					limit = probe.budget.Limits().TotalNodes
					charge = probe.budget.ChargeNodes
					remaining = tc.nodes
				}
				var exhausted *parser.LimitExceeded
				if !errors.As(charge(limit), &exhausted) {
					t.Fatal("parse consumption probe")
				}
				used := exhausted.Observed - limit
				c := listContext(t, tc.source, parser.DefaultLimits(), context.Background())
				c.options = Options{}
				charge = c.budget.ChargeWork
				if kind == "nodes" {
					charge = c.budget.ChargeNodes
				}
				if e := charge(limit - used - remaining - delta); e != nil {
					t.Fatal(e)
				}
				got, err := c.ParagraphIndentation(context.Background())
				if delta < 0 {
					var e *parser.LimitExceeded
					if !errors.As(err, &e) || got != nil {
						t.Fatal("direct facade Err+nil", got, err)
					}
				} else if err != nil || len(got) != tc.facts {
					t.Fatal("exact facade", got, err)
				}
			}
		}
	}
}
