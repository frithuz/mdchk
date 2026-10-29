package engine

import (
	"context"
	"errors"
	"fmt"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
	"testing/synctest"
	"time"
)

func TestRuleContextItalicStyleAuthorityAndOptionShapes(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, style := range []string{"consistent", "asterisk", "underscore"} {
			c := delimiterContext(t, "*a* _b_", flavor)
			c.options = Options{"style": style}
			got, err := c.DelimiterStyle(context.Background())
			if err != nil || got == nil || len(got.Valid) != 2 {
				t.Fatal(got, err)
			}
			value, err := c.DelimiterStyleOption(context.Background())
			if err != nil || value != style {
				t.Fatal(value, err)
			}
		}
		for _, options := range []Options{nil, {}, {"unknown": true}, {"style": nil}, {"style": true}, {"style": "Asterisk"}, {"style": ""}, {"style": "dash"}, {"style": "consistent", "extra": true}} {
			c := delimiterContext(t, "*a* _b_", flavor)
			c.options = options
			value, err := c.DelimiterStyleOption(context.Background())
			if !errors.Is(err, parser.ErrModel) || value != "" {
				t.Fatal(options, value, err)
			}
			if len(options) > 0 {
				got, err := c.DelimiterStyle(context.Background())
				if !errors.Is(err, parser.ErrModel) || got != nil {
					t.Fatal(options, got, err)
				}
			}
		}
		c := delimiterContext(t, "*a*", flavor)
		c.options = Options{"style": "consistent"}
		for _, invalidate := range []func(*RuleContext){
			func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("*a*")) }, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.parsed.Document = nil }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("*a*"))) }, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) {
				c.input, _ = NewInput("foreign.md", []byte("*a*"), nil, DocumentContext{Flavor: flavor})
			}, func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) {
				if flavor == "gfm" {
					c.input.documentContext.Flavor = "commonmark"
				} else {
					c.input.documentContext.Flavor = "gfm"
				}
			}, func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) },
		} {
			bad := c
			invalidate(&bad)
			got, err := bad.DelimiterStyle(context.Background())
			if !errors.Is(err, parser.ErrModel) || got != nil {
				t.Fatal(got, err)
			}
			style, err := bad.DelimiterStyleOption(context.Background())
			if !errors.Is(err, parser.ErrModel) || style != "" {
				t.Fatal(style, err)
			}
		}
		//nolint:staticcheck // Deliberately absent caller authority.
		got, err := c.DelimiterStyle(nil)
		if !errors.Is(err, parser.ErrModel) || got != nil {
			t.Fatal(got, err)
		}
	}
}

func TestRuleContextItalicStyleStaticReservations(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		work, nodes  int64
	}{{"empty", "", 80, 4}, {"actual", "*x*", 728, 20}} {
		for _, kind := range []string{"work", "nodes"} {
			exact := tc.work
			if kind == "nodes" {
				exact = tc.nodes
			}
			for _, delta := range []int64{-1, 0} {
				t.Run(fmt.Sprintf("%s/%s/%d", tc.name, kind, delta), func(t *testing.T) {
					// Sacrificial prefix accounting, BEFORE invoking the tested facade.
					prefix := delimiterContext(t, tc.source, "commonmark")
					prefix.options = Options{"style": "consistent"}
					charge, limit := prefix.budget.ChargeWork, prefix.budget.Limits().WorkUnits
					if kind == "nodes" {
						charge, limit = prefix.budget.ChargeNodes, prefix.budget.Limits().TotalNodes
					}
					var exhausted *parser.LimitExceeded
					if !errors.As(charge(limit), &exhausted) {
						t.Fatal("prefix must exhaust")
					}
					used := exhausted.Observed - limit
					c := delimiterContext(t, tc.source, "commonmark")
					c.options = Options{"style": "consistent"}
					charge = c.budget.ChargeWork
					if kind == "nodes" {
						charge = c.budget.ChargeNodes
					}
					if err := charge(limit - used - exact - delta); err != nil {
						t.Fatal(err)
					}
					got, err := c.DelimiterStyle(context.Background())
					if delta == 0 {
						if err != nil || got == nil {
							t.Fatal(got, err)
						}
					} else {
						if !errors.As(err, &exhausted) || got != nil {
							t.Fatal(got, err)
						}
						got, err = c.DelimiterStyle(context.Background())
						if !errors.As(err, &exhausted) || got != nil {
							t.Fatal("sticky", got, err)
						}
					}
				})
			}
		}
	}
}

func TestRuleContextItalicStyleFinalAndStickyControls(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 19}, {"clean", "---", 28}, {"protected", "```\nx\n```", 28}, {"actual", "*x*", 60}} {
		for _, origin := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(fmt.Sprintf("%s/origin-%t/%s", tc.name, origin, reason), func(t *testing.T) {
					c := delimiterContext(t, tc.source, "commonmark")
					c.options = Options{"style": "consistent"}
					stop := &proseFacadeStop{Context: context.Background(), stop: tc.final, reason: reason}
					caller := context.Context(stop)
					if origin {
						c.operation = stop
						caller = context.Background()
					}
					got, err := c.DelimiterStyle(caller)
					if !errors.Is(err, reason) || got != nil || stop.calls != tc.final {
						t.Fatal(got, err, stop.calls, tc.final)
					}
				})
			}
		}
	}
	for _, source := range []string{"", "---", "```\nx\n```", "*x*", "*a* _b_ _c_"} {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			t.Run(fmt.Sprintf("%s/%s", source, kind), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					c := delimiterContext(t, source, "gfm")
					c.options = Options{"style": "consistent"}
					limits := c.budget.Limits()
					switch kind {
					case "work":
						_ = c.budget.ChargeWork(limits.WorkUnits)
					case "nodes":
						_ = c.budget.ChargeNodes(limits.TotalNodes)
					case "elapsed":
						time.Sleep(limits.OperationTime + time.Millisecond)
					}
					for range 2 {
						got, err := c.DelimiterStyle(context.Background())
						var exhausted *parser.LimitExceeded
						if !errors.As(err, &exhausted) || got != nil {
							t.Fatal(got, err)
						}
					}
				})
			})
		}
	}
}

func TestRuleContextItalicStyleOptionFinalWithdrawal(t *testing.T) {
	for _, origin := range []bool{false, true} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(fmt.Sprintf("origin-%t/%s", origin, reason), func(t *testing.T) {
				c := delimiterContext(t, "*x*", "commonmark")
				c.options = Options{"style": "consistent"}
				stop := &proseFacadeStop{Context: context.Background(), stop: 3, reason: reason}
				caller := context.Context(stop)
				if origin {
					c.operation = stop
					caller = context.Background()
				}
				style, err := c.DelimiterStyleOption(caller)
				if !errors.Is(err, reason) || style != "" || stop.calls != 3 {
					t.Fatal(style, err, stop.calls)
				}
			})
		}
	}
}
