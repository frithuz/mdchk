package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestRuleContextHeadingKeyAuthorityAndControls(t *testing.T) {
	c := headingContext(t, "# Straße\n", parser.DefaultLimits(), context.Background())
	hs, err := c.Headings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	key, err := c.HeadingKey(context.Background(), hs[0])
	if err != nil || key != "strasse" {
		t.Fatal(key, err)
	}
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false },
		func(c *RuleContext) { c.operation = nil },
		func(c *RuleContext) { c.document = nil },
		func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
		func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte("# Straße\n"), nil, DocumentContext{})
		},
	} {
		bad := c
		invalidate(&bad)
		if key, err := bad.HeadingKey(context.Background(), hs[0]); err == nil || key != "" {
			t.Fatal("unowned key", key, err)
		}
	}
	foreign := headingContext(t, "# Straße\n", parser.DefaultLimits(), context.Background())
	if _, err := foreign.HeadingKey(context.Background(), hs[0]); err == nil {
		t.Fatal("foreign label accepted")
	}
	for _, origin := range []bool{false, true} {
		operation, cancel := context.WithCancel(context.Background())
		c := headingContext(t, "# Straße\n", parser.DefaultLimits(), operation)
		hs, err := c.Headings(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		caller := context.Background()
		if origin {
			cancel()
		} else {
			var stop context.CancelFunc
			caller, stop = context.WithCancel(caller)
			stop()
			defer cancel()
		}
		if key, err := c.HeadingKey(caller, hs[0]); key != "" || !errors.Is(err, context.Canceled) {
			t.Fatal(origin, key, err)
		}
	}
	operation, cancel := context.WithCancel(context.Background())
	c = headingContext(t, "# Straße\n", parser.DefaultLimits(), operation)
	hs, err = c.Headings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	caller, stop := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer stop()
	cancel()
	_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
	_, err = c.HeadingKey(caller, hs[0])
	var limit *parser.LimitExceeded
	if !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &limit) {
		t.Fatal(err)
	}
}

func TestRuleContextHeadingKeyResidualAllowance(t *testing.T) {
	for _, kind := range []string{"work", "nodes"} {
		c := headingContext(t, "# Straße\n", parser.DefaultLimits(), context.Background())
		hs, err := c.Headings(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if kind == "work" {
			_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
		} else {
			_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes)
		}
		for range 2 {
			key, err := c.HeadingKey(context.Background(), hs[0])
			var limit *parser.LimitExceeded
			if key != "" || !errors.As(err, &limit) {
				t.Fatal("new key allowance", kind, key, err)
			}
		}
	}
}

func TestRuleContextHeadingKeyOriginalElapsedAllowance(t *testing.T) {
	limits := parser.DefaultLimits()
	limits.OperationTime = time.Second
	c := headingContext(t, "# Straße\n", limits, context.Background())
	hs, err := c.Headings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(limits.OperationTime + time.Millisecond)
	key, err := c.HeadingKey(context.Background(), hs[0])
	var limit *parser.LimitExceeded
	if key != "" || !errors.As(err, &limit) || limit.Name != "operation-time" {
		t.Fatal("reset elapsed key allowance", key, err)
	}
}
