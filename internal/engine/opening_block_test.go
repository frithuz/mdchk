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

func TestRuleContextOpeningBlockAuthorityAndControls(t *testing.T) {
	c := headingContext(t, "Intro", parser.DefaultLimits(), context.Background())
	facts, err := c.OpeningBlock(context.Background())
	if err != nil || !facts.Point.Present() || facts.Point.Start() != 0 || facts.Point.End() != 0 || facts.Level != 0 {
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
		if got, err := bad.OpeningBlock(context.Background()); err == nil || got.Point.Present() {
			t.Fatal(got, err)
		}
	}
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, origin := range []bool{false, true} {
			original, cancel := context.WithCancel(context.Background())
			c := headingContext(t, "Intro", parser.DefaultLimits(), original)
			caller := context.Background()
			if origin {
				c.operation = &mdTerminalStopped{Context: original, reason: reason}
			} else {
				caller = &mdTerminalStopped{Context: caller, reason: reason}
			}
			got, err := c.OpeningBlock(caller)
			cancel()
			if got.Point.Present() || !errors.Is(err, reason) {
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
			switch kind {
			case "work":
				_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
			case "nodes":
				_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes)
			case "elapsed":
				time.Sleep(time.Millisecond * 25)
			}
			for range 2 {
				got, err := c.OpeningBlock(context.Background())
				var limit *parser.LimitExceeded
				if got.Point.Present() || !errors.As(err, &limit) {
					t.Fatal(kind, got, err)
				}
			}
		}
	})
}

func TestRuleContextOpeningBlockLongScanAndFinalWithholding(t *testing.T) {
	// A private caller stop sweeps checkpoints through construction and final
	// publication; any interrupted collection must withhold all retained facts.
	c := headingContext(t, "<!-- a -->"+strings.Repeat(" ", 4000)+"\nIntro", parser.DefaultLimits(), context.Background())
	counter := &terminalCounter{Context: context.Background()}
	if got, err := c.OpeningBlock(counter); err != nil || !got.Point.Present() {
		t.Fatal(got, err)
	}
	for _, stop := range []int{4, counter.calls / 2, counter.calls - 1, counter.calls} {
		got, err := c.OpeningBlock(&terminalCounter{Context: context.Background(), stop: stop})
		if got.Point.Present() || !errors.Is(err, context.Canceled) {
			t.Fatal(stop, got, err)
		}
	}
}
