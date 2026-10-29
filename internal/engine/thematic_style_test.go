package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

func md035Context(t *testing.T, flavor string) RuleContext {
	t.Helper()
	in, err := NewInput("style.md", []byte("---\n\n***"), nil, DocumentContext{Flavor: flavor})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithOptions(context.Background(), in.source, b, in.documentContext.ParseOptions())
	if parsed.Status != parser.Complete {
		t.Fatal(parsed)
	}
	return RuleContext{input: in, index: parser.NewLineIndex(in.source), document: parsed.Document, parsed: parsed, budget: b, operation: context.Background(), syntaxReliable: true, options: Options{"style": "consistent"}}
}

func TestRuleContextThematicStyleActualAuthority(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			c := md035Context(t, flavor)
			got, err := c.ThematicStyle(context.Background())
			if err != nil || len(got) != 1 || got[0].Start() != 5 {
				t.Fatal(got, err)
			}
			for _, invalidate := range []func(*RuleContext){
				func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("---\n\n***")) },
				func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil },
				func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("---\n\n***"))) }, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
				func(c *RuleContext) {
					c.input, _ = NewInput("foreign.md", []byte("---\n\n***"), nil, DocumentContext{Flavor: flavor})
				},
				func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) },
				func(c *RuleContext) {
					c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
				},
				func(c *RuleContext) {
					fm, _ := parser.NewFrontmatterOptions(true, nil)
					c.input.documentContext.Frontmatter = fm
				},
				func(c *RuleContext) {
					if flavor == "commonmark" {
						c.input.documentContext.Flavor = "gfm"
					} else {
						c.input.documentContext.Flavor = "commonmark"
					}
				},
				func(c *RuleContext) { c.options = Options{} }, func(c *RuleContext) { c.options = Options{"style": true} }, func(c *RuleContext) { c.options = Options{"style": "dash", "other": true} },
			} {
				bad := c
				invalidate(&bad)
				got, err := bad.ThematicStyle(context.Background())
				if !errors.Is(err, parser.ErrModel) || got != nil {
					t.Fatal(got, err)
				}
			}
			//nolint:staticcheck // Deliberate absent caller authority.
			if got, err := c.ThematicStyle(nil); !errors.Is(err, parser.ErrModel) || got != nil {
				t.Fatal(got, err)
			}
		})
	}
}

func TestRuleContextThematicStyleFinalCallerAndOrigin(t *testing.T) {
	// Fixed independent source-derived totals. The producer accounts for
	// 8/16/14/28/72 visits; facade entry/option/final add exactly three.
	for _, tc := range []struct {
		name, source string
		final        int
	}{
		{"empty", "", 11}, {"clean", "---", 19}, {"protected", "```\n***\n```", 17},
		{"violation", "---\n\n***", 31},
		{"growth", "---\n\n***\n\n***\n\n***\n\n***\n\n***", 75},
	} {
		for _, flavor := range []string{"commonmark", "gfm"} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				for _, origin := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/origin-%t", tc.name, flavor, reason.Error(), origin), func(t *testing.T) {
						in, e := NewInput("style.md", []byte(tc.source), nil, DocumentContext{Flavor: flavor})
						if e != nil {
							t.Fatal(e)
						}
						b, _ := parser.NewOperationBudget(parser.DefaultLimits())
						p := parser.ParseWithOptions(context.Background(), in.source, b, in.documentContext.ParseOptions())
						if p.Status != parser.Complete {
							t.Fatal(p)
						}
						c := RuleContext{input: in, index: parser.NewLineIndex(in.source), document: p.Document, parsed: p, budget: b, operation: context.Background(), syntaxReliable: true, options: Options{"style": "consistent"}}
						late := &mdTerminalStopAt{Context: context.Background(), stop: tc.final, reason: reason}
						caller := context.Context(late)
						if origin {
							c.operation = late
							caller = context.Background()
						}
						got, err := c.ThematicStyle(caller)
						if !errors.Is(err, reason) || got != nil {
							t.Fatal("direct facade final Err and nil first", got, err)
						}
						if late.calls != tc.final {
							t.Fatal("fixed final seam", late.calls, tc.final)
						}
					})
				}
			}
		}
	}
}

type mdTerminalStopAt struct {
	context.Context
	calls, stop int
	reason      error
}

func (c *mdTerminalStopAt) Err() error {
	c.calls++
	if c.calls >= c.stop {
		return c.reason
	}
	return nil
}

func TestRuleContextThematicStyleStickyControlsAndDetachedConcurrency(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		synctest.Test(t, func(t *testing.T) {
			c := md035Context(t, "gfm")
			switch kind {
			case "work":
				_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
			case "nodes":
				_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes)
			case "elapsed":
				time.Sleep(61 * time.Second)
			}
			for range 2 {
				got, err := c.ThematicStyle(context.Background())
				var limit *parser.LimitExceeded
				if !errors.As(err, &limit) || got != nil {
					t.Fatal(kind, got, err)
				}
			}
		})
	}
	c := md035Context(t, "commonmark")
	got, err := c.ThematicStyle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got[0] = parser.Span{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			got, err := c.ThematicStyle(context.Background())
			if err != nil || len(got) != 1 || got[0].Start() != 5 {
				t.Error(got, err)
			}
		})
	}
	wg.Wait()
}

func TestRuleContextThematicStyleStaticReservationBoundaries(t *testing.T) {
	// The facade adds exactly two work units and zero records to the independently
	// authored producer equation (78 work, 19 records) for these literal bytes.
	// A separate doomed context only establishes already-consumed parse/setup
	// units so the second original context can be left with a chosen allowance.
	// Neither expected cost nor the one-short boundary is runtime calibrated.
	for _, kind := range []string{"work", "nodes"} {
		exact := int64(80)
		if kind == "nodes" {
			exact = 19
		}
		for _, remaining := range []int64{exact - 1, exact} {
			t.Run(fmt.Sprintf("%s/%d", kind, remaining), func(t *testing.T) {
				probe := md035Context(t, "commonmark")
				charge, limit := probe.budget.ChargeWork, probe.budget.Limits().WorkUnits
				if kind == "nodes" {
					charge, limit = probe.budget.ChargeNodes, probe.budget.Limits().TotalNodes
				}
				var exhausted *parser.LimitExceeded
				if !errors.As(charge(limit), &exhausted) {
					t.Fatal("setup accounting probe must exhaust")
				}
				used := exhausted.Observed - limit
				c := md035Context(t, "commonmark")
				charge = c.budget.ChargeWork
				if kind == "nodes" {
					charge = c.budget.ChargeNodes
				}
				if err := charge(limit - used - remaining); err != nil {
					t.Fatal(err)
				}
				got, err := c.ThematicStyle(context.Background())
				if remaining == exact {
					if err != nil || len(got) != 1 || got[0].Start() != 5 {
						t.Fatal(got, err)
					}
				} else {
					var e *parser.LimitExceeded
					if !errors.As(err, &e) || got != nil {
						t.Fatal(got, err)
					}
				}
			})
		}
	}
}
