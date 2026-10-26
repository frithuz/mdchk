package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
	"testing/synctest"
	"time"
)

func TestRuleContextListSeparatorsActualAuthority(t *testing.T) {
	c := listContext(t, "- H", parser.DefaultLimits(), context.Background())
	got, err := c.ListSeparators(context.Background())
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
		if got, err := bad.ListSeparators(context.Background()); err == nil || got != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Explicitly reject absent caller authority.
	if _, err := c.ListSeparators(nil); err == nil {
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
			if got, err := q.ListSeparators(caller); !errors.Is(err, reason) || got != nil {
				t.Fatal(got, err)
			}
		}
	}
}
func TestRuleContextListSeparatorsOriginalControlsAndFinalWithholding(t *testing.T) {
	c := listContext(t, "- H", parser.DefaultLimits(), context.Background())
	count := &terminalCounter{Context: context.Background()}
	if _, err := c.ListSeparators(count); err != nil {
		t.Fatal(err)
	}
	for _, stop := range []int{1, count.calls / 2, count.calls} {
		if got, err := c.ListSeparators(&terminalCounter{Context: context.Background(), stop: stop}); !errors.Is(err, context.Canceled) || got != nil {
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
				got, err := q.ListSeparators(context.Background())
				var limit *parser.LimitExceeded
				if !errors.As(err, &limit) || got != nil {
					t.Fatal(kind, got, err)
				}
			}
		}
	})
}

func TestRuleContextListMarkerSpacesOriginalAuthority(t *testing.T) {
	for _, origin := range []bool{false, true} {
		for _, stop := range []int{1, 2} {
			c := listContext(t, "", parser.DefaultLimits(), context.Background())
			c.options = Options{"spaces": int(^uint(0) >> 1)}
			caller := context.Background()
			counter := &terminalCounter{Context: context.Background(), stop: stop}
			if origin {
				c.operation = counter
			} else {
				caller = counter
			}
			spaces, err := c.ListMarkerSpaces(caller)
			if !errors.Is(err, context.Canceled) || spaces != 0 {
				t.Fatal(spaces, err)
			}
		}
	}
	for _, kind := range []string{"fresh budget", "foreign input", "absent context", "invalid option", "work", "nodes", "elapsed"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				limits := parser.DefaultLimits()
				limits.OperationTime = 20 * time.Millisecond
				c := listContext(t, "", limits, context.Background())
				c.options = Options{"spaces": 1}
				caller := context.Background()
				switch kind {
				case "fresh budget":
					c.budget, _ = parser.NewOperationBudget(limits)
				case "foreign input":
					c.input, _ = NewInput("other", nil, nil, DocumentContext{})
				case "absent context":
					caller = nil
				case "invalid option":
					c.options["spaces"] = nil
				case "work":
					_ = c.budget.ChargeWork(limits.WorkUnits)
				case "nodes":
					_ = c.budget.ChargeNodes(limits.TotalNodes + 1)
				case "elapsed":
					time.Sleep(25 * time.Millisecond)
				}
				spaces, err := c.ListMarkerSpaces(caller)
				if err == nil || spaces != 0 {
					t.Fatal(kind, spaces, err)
				}
			})
		})
	}
	c := listContext(t, "", parser.DefaultLimits(), context.Background())
	c.options = Options{"spaces": int(^uint(0) >> 1)}
	copied := c.Options()
	copied["spaces"] = 1
	if spaces, err := c.ListMarkerSpaces(context.Background()); err != nil || spaces != int(^uint(0)>>1) {
		t.Fatal(spaces, err)
	}
}

// Counts are fixed from the original production path: facade entry, complete
// ListItems, separator reservation/lookup/records, parser final, facade final.
// Recalibrating inside the test would hide a removed final observation.
func TestRuleContextListSeparatorsFinalPublicationAllShapes(t *testing.T) {
	for _, tc := range []struct {
		source       string
		facts, final int
	}{
		{"", 0, 7}, {"- one\n- two", 2, 29}, {"-  one\n-  two", 2, 29},
	} {
		for _, origin := range []bool{false, true} {
			c := listContext(t, tc.source, parser.DefaultLimits(), context.Background())
			got, err := c.ListSeparators(context.Background())
			if err != nil || len(got) != tc.facts {
				t.Fatal("original inventory", got, err)
			}
			stop := &terminalCounter{Context: context.Background(), stop: tc.final}
			caller := context.Background()
			if origin {
				c.operation = stop
			} else {
				caller = stop
			}
			got, err = c.ListSeparators(caller)
			if !errors.Is(err, context.Canceled) || got != nil || stop.calls != tc.final {
				t.Fatal(tc.source, origin, got, err, stop.calls)
			}
		}
	}
}
