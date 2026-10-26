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

func capitalizationContext(t *testing.T) RuleContext {
	t.Helper()
	c := headingContext(t, "# lower", parser.DefaultLimits(), context.Background())
	c.options = Options{"style": "sentence", "proper_names": []string{"iPhone", "van Gogh"}}
	return c
}

func TestRuleContextHeadingCapitalizationAuthority(t *testing.T) {
	c := capitalizationContext(t)
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil },
		func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("# lower"))) },
		func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
		func(c *RuleContext) { c.input, _ = NewInput("foreign.md", []byte("# lower"), nil, DocumentContext{}) },
		func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) },
	} {
		bad := c
		invalidate(&bad)
		if _, names, err := bad.HeadingCapitalizationOptions(context.Background()); err == nil || names != nil {
			t.Fatal(names, err)
		}
		if got, err := bad.HeadingScalars(context.Background()); err == nil || got != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Verify nil context cannot grant operation authority.
	if _, _, err := c.HeadingCapitalizationOptions(nil); err == nil {
		t.Fatal("nil context")
	}
}

func TestRuleContextHeadingCapitalizationOwnedOptionsAndFinalWithholding(t *testing.T) {
	c := capitalizationContext(t)
	style, names, err := c.HeadingCapitalizationOptions(context.Background())
	if err != nil || style != "sentence" || len(names) != 2 {
		t.Fatal(style, names, err)
	}
	names[0] = "changed"
	_, again, err := c.HeadingCapitalizationOptions(context.Background())
	if err != nil || again[0] != "iPhone" {
		t.Fatal(again, err)
	}
	for _, scalars := range []bool{false, true} {
		counter := &terminalCounter{Context: context.Background()}
		if scalars {
			if _, err := c.HeadingScalars(counter); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, _, err := c.HeadingCapitalizationOptions(counter); err != nil {
				t.Fatal(err)
			}
		}
		for _, stop := range []int{1, counter.calls / 2, counter.calls} {
			ctx := &terminalCounter{Context: context.Background(), stop: stop}
			if scalars {
				if got, err := c.HeadingScalars(ctx); !errors.Is(err, context.Canceled) || got != nil {
					t.Fatal(stop, got, err)
				}
			} else {
				if style, got, err := c.HeadingCapitalizationOptions(ctx); !errors.Is(err, context.Canceled) || got != nil || style != "" {
					t.Fatal(stop, style, got, err)
				}
			}
		}
	}
}

func TestRuleContextHeadingCapitalizationOriginalControls(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, origin := range []bool{false, true} {
			c := capitalizationContext(t)
			caller := context.Background()
			if origin {
				c.operation = &mdTerminalStopped{Context: c.operation, reason: reason}
			} else {
				caller = &mdTerminalStopped{Context: caller, reason: reason}
			}
			if _, names, err := c.HeadingCapitalizationOptions(caller); !errors.Is(err, reason) || names != nil {
				t.Fatal(names, err)
			}
			if got, err := c.HeadingScalars(caller); !errors.Is(err, reason) || got != nil {
				t.Fatal(got, err)
			}
		}
	}
	synctest.Test(t, func(t *testing.T) {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			limits := parser.DefaultLimits()
			limits.OperationTime = 20 * time.Millisecond
			c := headingContext(t, "# lower", limits, context.Background())
			c.options = Options{"style": "sentence", "proper_names": []string{}}
			switch kind {
			case "work":
				_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
			case "nodes":
				_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes)
			case "elapsed":
				time.Sleep(25 * time.Millisecond)
			}
			for range 2 {
				var limit *parser.LimitExceeded
				if _, names, err := c.HeadingCapitalizationOptions(context.Background()); !errors.As(err, &limit) || names != nil {
					t.Fatal(kind, names, err)
				}
				if got, err := c.HeadingScalars(context.Background()); !errors.As(err, &limit) || got != nil {
					t.Fatal(kind, got, err)
				}
			}
		}
	})
}

func TestRuleContextHeadingCapitalizationReservesBeforeNamesCopy(t *testing.T) {
	for _, kind := range []string{"work", "nodes"} {
		c := capitalizationContext(t)
		values := make([]string, 10000)
		for i := range values {
			values[i] = "name"
		}
		c.options["proper_names"] = values
		if kind == "work" {
			_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits - 100)
		} else {
			_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes - 100)
		}
		var limit *parser.LimitExceeded
		if _, got, err := c.HeadingCapitalizationOptions(context.Background()); !errors.As(err, &limit) || got != nil {
			t.Fatal(kind, got, err)
		}
	}
}

func TestRuleContextHeadingScalarsReserveOriginalRecords(t *testing.T) {
	limits := parser.DefaultLimits()
	limits.TotalNodes = 1000
	limits.DocumentNodes = 1000
	c := headingContext(t, "# "+strings.Repeat("a", 2000), limits, context.Background())
	var limit *parser.LimitExceeded
	if got, err := c.HeadingScalars(context.Background()); !errors.As(err, &limit) || got != nil || limit.Name != "total-nodes" {
		t.Fatal(got, err)
	}
}
