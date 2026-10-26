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

func TestRuleContextHeadingOutlineAuthorityAndControls(t *testing.T) {
	c := headingContext(t, "Intro", parser.DefaultLimits(), context.Background())
	c.options = Options{"headings": []string{"# A", "*"}}
	facts, err := c.HeadingOutline(context.Background())
	if err != nil || len(facts) != 2 || facts[0].Level != 1 || facts[0].Text != "A" || !facts[1].Star {
		t.Fatal(facts, err)
	}
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false },
		func(c *RuleContext) { c.operation = nil },
		func(c *RuleContext) { c.document = nil },
		func(c *RuleContext) { c.budget = nil },
		func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("Intro"))) },
		func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
		func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte("Intro"), nil, DocumentContext{})
		},
		func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) },
	} {
		bad := c
		invalidate(&bad)
		if got, err := bad.HeadingOutline(context.Background()); err == nil || got != nil {
			t.Fatal(got, err)
		}
	}
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, origin := range []bool{false, true} {
			original, cancel := context.WithCancel(context.Background())
			c := headingContext(t, "Intro", parser.DefaultLimits(), original)
			c.options = Options{"headings": []string{}}
			caller := context.Background()
			if origin {
				c.operation = &mdTerminalStopped{Context: original, reason: reason}
			} else {
				caller = &mdTerminalStopped{Context: caller, reason: reason}
			}
			got, err := c.HeadingOutline(caller)
			cancel()
			if got != nil || !errors.Is(err, reason) {
				t.Fatal(got, err)
			}
		}
	}
	synctest.Test(t, func(t *testing.T) {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			limits := parser.DefaultLimits()
			if kind == "elapsed" {
				limits.OperationTime = time.Millisecond * 20
			}
			c := headingContext(t, "Intro", limits, context.Background())
			c.options = Options{"headings": []string{}}
			switch kind {
			case "work":
				_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
			case "nodes":
				_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes)
			case "elapsed":
				time.Sleep(time.Millisecond * 25)
			}
			for range 2 {
				got, err := c.HeadingOutline(context.Background())
				var limit *parser.LimitExceeded
				if got != nil || !errors.As(err, &limit) {
					t.Fatal(kind, got, err)
				}
			}
		}
	})
}

func TestRuleContextHeadingOutlineOwnedStorageAndWithholding(t *testing.T) {
	c := headingContext(t, "# A", parser.DefaultLimits(), context.Background())
	c.options = Options{"headings": []string{"# A", "*"}}
	got, err := c.HeadingOutline(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got[0].Text = "changed"
	got[1].Star = false
	again, err := c.HeadingOutline(context.Background())
	if err != nil || again[0].Text != "A" || !again[1].Star {
		t.Fatal(again, err)
	}
	c.options = Options{"headings": []string{"# " + strings.Repeat("a", 4000)}}
	counter := &terminalCounter{Context: context.Background()}
	if got, err := c.HeadingOutline(counter); err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	for _, stop := range []int{1, 4, counter.calls / 2, counter.calls - 1, counter.calls} {
		got, err := c.HeadingOutline(&terminalCounter{Context: context.Background(), stop: stop})
		if got != nil || !errors.Is(err, context.Canceled) {
			t.Fatal(stop, got, err)
		}
	}
}

func TestRuleContextHeadingOutlineReservesBeforeListCopy(t *testing.T) {
	for _, kind := range []string{"nodes", "work"} {
		c := headingContext(t, "# A", parser.DefaultLimits(), context.Background())
		values := make([]string, 10000)
		for i := range values {
			values[i] = "# A"
		}
		c.options = Options{"headings": values}
		if kind == "nodes" {
			_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes - 100)
		} else {
			_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits - 100)
		}
		got, err := c.HeadingOutline(context.Background())
		var limit *parser.LimitExceeded
		if got != nil || !errors.As(err, &limit) {
			t.Fatal(kind, got, err)
		}
	}
}
