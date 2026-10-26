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

func TestRuleContextEmphasisHeadingAuthorityAndControls(t *testing.T) {
	c := headingContext(t, "**A**\n\nBody", parser.DefaultLimits(), context.Background())
	facts, err := c.EmphasisHeadings(context.Background())
	if err != nil || len(facts) != 1 || facts[0].Start() != 0 || facts[0].End() != 5 {
		t.Fatal(facts, err)
	}
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false },
		func(c *RuleContext) { c.operation = nil },
		func(c *RuleContext) { c.document = nil },
		func(c *RuleContext) { c.budget = nil },
		func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("**A**\n\nBody"))) },
		func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
		func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte("**A**\n\nBody"), nil, DocumentContext{})
		},
		func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) },
	} {
		bad := c
		invalidate(&bad)
		if got, err := bad.EmphasisHeadings(context.Background()); err == nil || got != nil {
			t.Fatal(got, err)
		}
	}
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, origin := range []bool{false, true} {
			original, cancel := context.WithCancel(context.Background())
			c := headingContext(t, "**A**\n\nBody", parser.DefaultLimits(), original)
			caller := context.Background()
			if origin {
				c.operation = &mdTerminalStopped{Context: original, reason: reason}
			} else {
				caller = &mdTerminalStopped{Context: caller, reason: reason}
			}
			got, err := c.EmphasisHeadings(caller)
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
			c := headingContext(t, "**A**\n\nBody", limits, context.Background())
			switch kind {
			case "work":
				_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
			case "nodes":
				_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes)
			case "elapsed":
				time.Sleep(time.Millisecond * 25)
			}
			for range 2 {
				got, err := c.EmphasisHeadings(context.Background())
				var limit *parser.LimitExceeded
				if got != nil || !errors.As(err, &limit) {
					t.Fatal(kind, got, err)
				}
			}
		}
	})
}

func TestRuleContextEmphasisHeadingLongScanAndFinalWithholding(t *testing.T) {
	// A private caller stop sweeps checkpoints through construction and final
	// publication; any interrupted collection must withhold all retained facts.
	c := headingContext(t, "**"+strings.Repeat("&nbsp;", 3000)+"A**\n\nBody\n\n**B**\n\nBody", parser.DefaultLimits(), context.Background())
	counter := &terminalCounter{Context: context.Background()}
	if got, err := c.EmphasisHeadings(counter); err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	for _, stop := range []int{4, 100, 1000, counter.calls - 1, counter.calls} {
		got, err := c.EmphasisHeadings(&terminalCounter{Context: context.Background(), stop: stop})
		if got != nil || !errors.Is(err, context.Canceled) {
			t.Fatal(stop, got, err)
		}
	}
}
