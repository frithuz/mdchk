package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestRuleContextListsActualAuthority(t *testing.T) {
	c := listContext(t, "- H", parser.DefaultLimits(), context.Background())
	got, err := c.ListItems(context.Background())
	if err != nil || len(got) != 1 || got[0].Marker.Start() != 0 {
		t.Fatal(got, err)
	}
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false },
		func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("- H")) },
		func(c *RuleContext) { c.parsed.Status = parser.Incomplete },
		func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil },
		func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("- H"))) },
		func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
		func(c *RuleContext) { c.input, _ = NewInput("foreign.md", []byte("- H"), nil, DocumentContext{}) },
		func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) },
		func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		},
		func(c *RuleContext) { c.input.documentContext.Flavor = "gfm" },
		func(c *RuleContext) { c.input.documentContext.Frontmatter = parser.FrontmatterOptions{} },
	} {
		bad := c
		invalidate(&bad)
		if got, err := bad.ListItems(context.Background()); err == nil || got != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Explicitly reject absent caller authority.
	if _, err := c.ListItems(nil); err == nil {
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
			if got, err := q.ListItems(caller); !errors.Is(err, reason) || got != nil {
				t.Fatal(got, err)
			}
		}
	}
}
func TestRuleContextListsOriginalControlsAndFinalWithholding(t *testing.T) {
	c := listContext(t, "- H", parser.DefaultLimits(), context.Background())
	count := &terminalCounter{Context: context.Background()}
	if _, err := c.ListItems(count); err != nil {
		t.Fatal(err)
	}
	for _, stop := range []int{1, count.calls / 2, count.calls} {
		if got, err := c.ListItems(&terminalCounter{Context: context.Background(), stop: stop}); !errors.Is(err, context.Canceled) || got != nil {
			t.Fatal(stop, got, err)
		}
	}
	synctest.Test(t, func(t *testing.T) {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			limits := parser.DefaultLimits()
			limits.OperationTime = 20 * time.Millisecond
			q := listContext(t, "- H", limits, context.Background())
			switch kind {
			case "work":
				_ = q.budget.ChargeWork(limits.WorkUnits)
			case "nodes":
				_ = q.budget.ChargeNodes(limits.TotalNodes)
			case "elapsed":
				time.Sleep(25 * time.Millisecond)
			}
			for range 2 {
				got, err := q.ListItems(context.Background())
				var limit *parser.LimitExceeded
				if !errors.As(err, &limit) || got != nil {
					t.Fatal(kind, got, err)
				}
			}
		}
	})
}

func listContext(t *testing.T, text string, limits parser.Limits, operation context.Context) RuleContext {
	c := headingContext(t, text, limits, operation)
	c.options = Options{"marker": "consistent"}
	return c
}

func TestRuleContextListsFixedFinalCheck(t *testing.T) {
	for _, origin := range []bool{false, true} {
		c := listContext(t, "", parser.DefaultLimits(), context.Background())
		// Empty syntax observes original/caller at entry, parser entry/root/final,
		// then the engine facade's fixed fifth publication observation.
		stop := &terminalCounter{Context: context.Background(), stop: 5}
		caller := context.Background()
		if origin {
			c.operation = stop
		} else {
			caller = stop
		}
		if got, err := c.ListItems(caller); !errors.Is(err, context.Canceled) || got != nil {
			t.Fatal(origin, got, err, stop.calls)
		}
	}
}

func TestRuleContextListsMarkerOptionOwnershipAndWork(t *testing.T) {
	c := listContext(t, "- H", parser.DefaultLimits(), context.Background())
	c.options = Options{"marker": "plus"}
	copied := c.Options()
	copied["marker"] = "dash"
	if got, err := c.BulletMarker(context.Background()); err != nil || got != '+' {
		t.Fatal(got, err)
	}
	for _, v := range []any{nil, true, "wrong"} {
		c.options = Options{"marker": v}
		if _, err := c.BulletMarker(context.Background()); !errors.Is(err, parser.ErrModel) {
			t.Fatal(v, err)
		}
	}
	c = listContext(t, "", parser.DefaultLimits(), context.Background())
	c.options = Options{"marker": strings.Repeat("x", 10000)}
	if err := c.budget.ChargeWork(parser.DefaultLimits().WorkUnits - 64); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		_, err := c.BulletMarker(context.Background())
		var limit *parser.LimitExceeded
		if !errors.As(err, &limit) || limit.Name != "work-units" {
			t.Fatal(err)
		}
	}
}

func TestRuleContextBulletIndentationOriginalAuthority(t *testing.T) {
	for _, origin := range []bool{false, true} {
		for _, stop := range []int{1, 2} {
			c := listContext(t, "", parser.DefaultLimits(), context.Background())
			c.options = Options{"indent": int(^uint(0) >> 1), "start_indented": true}
			caller := context.Background()
			counter := &terminalCounter{Context: context.Background(), stop: stop}
			if origin {
				c.operation = counter
			} else {
				caller = counter
			}
			indent, start, err := c.BulletIndentation(caller)
			if !errors.Is(err, context.Canceled) || indent != 0 || start {
				t.Fatal(indent, start, err)
			}
		}
	}
	for _, kind := range []string{"fresh budget", "foreign input", "absent context", "invalid option", "work", "nodes", "elapsed"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				limits := parser.DefaultLimits()
				limits.OperationTime = 20 * time.Millisecond
				c := listContext(t, "", limits, context.Background())
				c.options = Options{"indent": 2, "start_indented": false}
				caller := context.Background()
				switch kind {
				case "fresh budget":
					c.budget, _ = parser.NewOperationBudget(limits)
				case "foreign input":
					c.input, _ = NewInput("other", nil, nil, DocumentContext{})
				case "absent context":
					caller = nil
				case "invalid option":
					c.options["indent"] = 0
				case "work":
					_ = c.budget.ChargeWork(limits.WorkUnits)
				case "nodes":
					_ = c.budget.ChargeNodes(limits.TotalNodes + 1)
				case "elapsed":
					time.Sleep(25 * time.Millisecond)
				}
				indent, start, err := c.BulletIndentation(caller)
				if err == nil || indent != 0 || start {
					t.Fatal(kind, indent, start, err)
				}
			})
		})
	}
	c := listContext(t, "", parser.DefaultLimits(), context.Background())
	c.options = Options{"indent": int(^uint(0) >> 1), "start_indented": true}
	copied := c.Options()
	copied["indent"] = 1
	if indent, start, err := c.BulletIndentation(context.Background()); err != nil || indent != int(^uint(0)>>1) || !start {
		t.Fatal(indent, start, err)
	}
}
