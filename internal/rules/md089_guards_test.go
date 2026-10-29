package rules

import (
	"context"
	"errors"
	"fmt"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"testing"
	"testing/synctest"
	"time"
)

func TestMD089FinalPublicationWithholdsAllFindings(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 1}, {"clean", "a\nb", 1}, {"protected", "```\nx\n```", 1}, {"violation", "中A中A中A", 15}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+reason.Error(), func(t *testing.T) {
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD089", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, err := c.CJKSpacing(ctx)
					if err != nil {
						t.Fatal(err)
					}
					// Fixed independent counts: one check per item, optional
					// finding growth/emission, then the final publication check.
					late := &md018LateContext{Context: ctx, reason: reason, stop: tc.final}
					observed = compareCJKSpacing(late, c, items)
					return observed
				}}})
				out := analyzeHeadings(t, r, tc.source, md089Settings(), engine.DocumentContext{}, engine.RunOptions{})
				if !errors.Is(observed.Err, reason) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
					t.Fatal(observed, out)
				}
			})
		}
	}
}

func TestMD089CallbackQuotaAndCopyBoundaries(t *testing.T) {
	for _, kind := range []string{"work", "nodes"} {
		for _, remaining := range []int64{0, 20, 21} {
			if kind == "nodes" && remaining == 20 {
				remaining = 16
			}
			if kind == "nodes" && remaining == 21 {
				remaining = 17
			}
			t.Run(fmt.Sprintf("%s/%d", kind, remaining), func(t *testing.T) {
				var used int64
				run := func(probe bool) (result.Result, engine.RuleOutcome) {
					budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
					var observed engine.RuleOutcome
					r := registry(t, []engine.Registration{{ID: "MD089", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						items, e := c.CJKSpacing(ctx)
						if e != nil || len(items) != 5 {
							t.Fatal(items, e)
						}
						charge, limit := budget.ChargeWork, budget.Limits().WorkUnits
						if kind == "nodes" {
							charge, limit = budget.ChargeNodes, budget.Limits().TotalNodes
						}
						if probe {
							var stopped *parser.LimitExceeded
							if !errors.As(charge(limit), &stopped) {
								t.Fatal("probe must exhaust")
							}
							used = stopped.Observed - limit
							return engine.RuleOutcome{Err: stopped}
						}
						if e := charge(limit - used - remaining); e != nil {
							t.Fatal(e)
						}
						observed = compareCJKSpacing(ctx, c, items)
						return observed
					}}})
					return analyzeHeadings(t, r, "中A中A中A", md089Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget}), observed
				}
				run(true)
				out, observed := run(false)
				success := remaining == 21 || remaining == 17
				if success {
					if observed.Err != nil || len(observed.Findings) != 5 {
						t.Fatal(out, observed)
					}
				} else {
					var limit *parser.LimitExceeded
					if !errors.As(observed.Err, &limit) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 {
						t.Fatal("direct callback quota omitted", out, observed)
					}
				}
			})
		}
	}
}

func TestMD089CallbackOriginalElapsedWithholds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, source := range []string{"", "a\n", "```\nx\n```", "中A"} {
			limits := parser.DefaultLimits()
			limits.OperationTime = 20 * time.Millisecond
			b, _ := parser.NewOperationBudget(limits)
			var observed engine.RuleOutcome
			r := registry(t, []engine.Registration{{ID: "MD089", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				items, e := c.CJKSpacing(ctx)
				if e != nil {
					t.Fatal(e)
				}
				time.Sleep(25 * time.Millisecond)
				observed = compareCJKSpacing(ctx, c, items)
				var limit *parser.LimitExceeded
				if !errors.As(observed.Err, &limit) || observed.Findings != nil {
					t.Fatal("direct callback elapsed Err+nil", observed)
				}
				return observed
			}}})
			out := analyzeHeadings(t, r, source, md089Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: b})
			if out.Complete || len(out.Diagnostics) != 0 {
				t.Fatal(observed, out)
			}
		}
	})
}
