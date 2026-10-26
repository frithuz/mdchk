package engine

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestRuleContextSectionsActualAuthority(t *testing.T) {
	c := sectionContext(t, "# A\n# A", parser.DefaultLimits(), context.Background())
	got, a, err := c.EmptySections(context.Background())
	if err != nil || a || len(got) != 2 || got[1].Label.Start() != 6 {
		t.Fatal(got, a, err)
	}
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false },
		func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("# A\n# A")) },
		func(c *RuleContext) { c.parsed.Status = parser.Incomplete },
		func(c *RuleContext) { c.options = Options{"allow_final_section": "true"} }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil },
		func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("# A\n# A"))) },
		func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
		func(c *RuleContext) { c.input, _ = NewInput("foreign.md", []byte("# A\n# A"), nil, DocumentContext{}) },
		func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) },
		func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		},
		func(c *RuleContext) { c.input.documentContext.Flavor = "gfm" },
		func(c *RuleContext) { c.input.documentContext.Frontmatter = parser.FrontmatterOptions{} },
	} {
		bad := c
		invalidate(&bad)
		if got, a, err := bad.EmptySections(context.Background()); err == nil || got != nil || a {
			t.Fatal(got, a, err)
		}
	}
	//nolint:staticcheck // Explicitly reject absent caller authority.
	if _, _, err := c.EmptySections(nil); err == nil {
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
			if got, a, err := q.EmptySections(caller); !errors.Is(err, reason) || got != nil || a {
				t.Fatal(got, a, err)
			}
		}
	}
}
func TestRuleContextSectionsOriginalControlsAndFinalWithholding(t *testing.T) {
	c := sectionContext(t, "# A\n# A", parser.DefaultLimits(), context.Background())
	count := &terminalCounter{Context: context.Background()}
	if _, _, err := c.EmptySections(count); err != nil {
		t.Fatal(err)
	}
	for _, stop := range []int{1, count.calls / 2, count.calls} {
		if got, a, err := c.EmptySections(&terminalCounter{Context: context.Background(), stop: stop}); !errors.Is(err, context.Canceled) || got != nil || a {
			t.Fatal(stop, got, a, err)
		}
	}
	synctest.Test(t, func(t *testing.T) {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			limits := parser.DefaultLimits()
			limits.OperationTime = 20 * time.Millisecond
			q := sectionContext(t, "# A\n# A", limits, context.Background())
			switch kind {
			case "work":
				_ = q.budget.ChargeWork(limits.WorkUnits)
			case "nodes":
				_ = q.budget.ChargeNodes(limits.TotalNodes)
			case "elapsed":
				time.Sleep(25 * time.Millisecond)
			}
			for range 2 {
				got, a, err := q.EmptySections(context.Background())
				var limit *parser.LimitExceeded
				if !errors.As(err, &limit) || got != nil || a {
					t.Fatal(kind, got, a, err)
				}
			}
		}
	})
}

func sectionContext(t *testing.T, text string, limits parser.Limits, operation context.Context) RuleContext {
	c := headingContext(t, text, limits, operation)
	c.options = Options{"allow_final_section": false}
	return c
}
