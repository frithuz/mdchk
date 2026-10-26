package engine

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestRuleContextHeadingConstructsActualAuthority(t *testing.T) {
	c := constructContext(t, "# *H*", parser.DefaultLimits(), context.Background())
	got, err := c.DisallowedHeadingConstructs(context.Background())
	if err != nil || len(got) != 1 || got[0].Span.Start() != 2 {
		t.Fatal(got, err)
	}
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false },
		func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("# *H*")) },
		func(c *RuleContext) { c.parsed.Status = parser.Incomplete },
		func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil },
		func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("# *H*"))) },
		func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
		func(c *RuleContext) { c.input, _ = NewInput("foreign.md", []byte("# *H*"), nil, DocumentContext{}) },
		func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) },
		func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		},
		func(c *RuleContext) { c.input.documentContext.Flavor = "gfm" },
		func(c *RuleContext) { c.input.documentContext.Frontmatter = parser.FrontmatterOptions{} },
	} {
		bad := c
		invalidate(&bad)
		if got, err := bad.DisallowedHeadingConstructs(context.Background()); err == nil || got != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Explicitly reject absent caller authority.
	if _, err := c.DisallowedHeadingConstructs(nil); err == nil {
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
			if got, err := q.DisallowedHeadingConstructs(caller); !errors.Is(err, reason) || got != nil {
				t.Fatal(got, err)
			}
		}
	}
}
func TestRuleContextHeadingConstructsOriginalControlsAndFinalWithholding(t *testing.T) {
	c := constructContext(t, "# *H*", parser.DefaultLimits(), context.Background())
	count := &terminalCounter{Context: context.Background()}
	if _, err := c.DisallowedHeadingConstructs(count); err != nil {
		t.Fatal(err)
	}
	for _, stop := range []int{1, count.calls / 2, count.calls} {
		if got, err := c.DisallowedHeadingConstructs(&terminalCounter{Context: context.Background(), stop: stop}); !errors.Is(err, context.Canceled) || got != nil {
			t.Fatal(stop, got, err)
		}
	}
	synctest.Test(t, func(t *testing.T) {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			limits := parser.DefaultLimits()
			limits.OperationTime = 20 * time.Millisecond
			q := constructContext(t, "# *H*", limits, context.Background())
			switch kind {
			case "work":
				_ = q.budget.ChargeWork(limits.WorkUnits)
			case "nodes":
				_ = q.budget.ChargeNodes(limits.TotalNodes)
			case "elapsed":
				time.Sleep(25 * time.Millisecond)
			}
			for range 2 {
				got, err := q.DisallowedHeadingConstructs(context.Background())
				var limit *parser.LimitExceeded
				if !errors.As(err, &limit) || got != nil {
					t.Fatal(kind, got, err)
				}
			}
		}
	})
}

func constructContext(t *testing.T, text string, limits parser.Limits, operation context.Context) RuleContext {
	c := headingContext(t, text, limits, operation)
	c.options = Options{"allowed_constructs": []string{}}
	return c
}

func TestRuleContextHeadingConstructsFixedFinalCheck(t *testing.T) {
	for _, origin := range []bool{false, true} {
		c := constructContext(t, "", parser.DefaultLimits(), context.Background())
		// Empty syntax observes original/caller at entry, parser entry/root/final,
		// then the engine facade's fixed fifth publication observation.
		stop := &terminalCounter{Context: context.Background(), stop: 5}
		caller := context.Background()
		if origin {
			c.operation = stop
		} else {
			caller = stop
		}
		if got, err := c.DisallowedHeadingConstructs(caller); !errors.Is(err, context.Canceled) || got != nil {
			t.Fatal(origin, got, err, stop.calls)
		}
	}
}
func TestRuleContextHeadingConstructsOptionOwnershipAndWork(t *testing.T) {
	c := constructContext(t, "# [**x**](u)", parser.DefaultLimits(), context.Background())
	c.options = Options{"allowed_constructs": []string{"link", "link"}}
	copied := c.Options()
	copied["allowed_constructs"].([]string)[0] = "strong"
	got, err := c.DisallowedHeadingConstructs(context.Background())
	if err != nil || len(got) != 1 || got[0].Kind != parser.Strong {
		t.Fatal(got, err)
	}
	c.options = Options{"allowed_constructs": []string{"link", "wrong"}}
	if got, err := c.DisallowedHeadingConstructs(context.Background()); !errors.Is(err, parser.ErrModel) || got != nil {
		t.Fatal(got, err)
	}
	c = constructContext(t, "", parser.DefaultLimits(), context.Background())
	values := make([]string, 10000)
	for i := range values {
		values[i] = "code"
	}
	c.options = Options{"allowed_constructs": values}
	if err := c.budget.ChargeWork(parser.DefaultLimits().WorkUnits - 4096); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got, err := c.DisallowedHeadingConstructs(context.Background())
		var limit *parser.LimitExceeded
		if !errors.As(err, &limit) || limit.Name != "work-units" || got != nil {
			t.Fatal(got, err)
		}
	}
}
