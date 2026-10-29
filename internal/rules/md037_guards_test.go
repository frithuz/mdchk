package rules

import (
	"context"
	"errors"
	"fmt"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
	"testing/synctest"
	"time"
)

func TestMD037CallbackFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 1}, {"clean", "a", 1}, {"protected", "```\nx\n```", 1}, {"findings", "A * x *", 7}, {"growth", "A * x *\n\nA * x *\n\nA * x *", 17}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+reason.Error(), func(t *testing.T) {
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD037", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					inventory, err := c.DelimiterStyle(ctx)
					if err != nil {
						t.Fatal(err)
					}
					late := &md018LateContext{Context: ctx, stop: tc.final, reason: reason}
					observed = compareDelimiterStyle(late, c, inventory.Whitespace)
					return observed
				}}})
				out := analyzeHeadings(t, r, tc.source, md037Settings(), engine.DocumentContext{}, engine.RunOptions{})
				if !errors.Is(observed.Err, reason) || observed.Findings != nil || len(out.Diagnostics) != 0 || out.Complete || len(out.Interruptions) != 1 {
					t.Fatal(observed, out)
				}
			})
		}
	}
}

func TestMD037CallbackOriginFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 1}, {"clean", "a", 1}, {"protected", "```\nx\n```", 1}, {"findings", "A * x *", 7}, {"growth", "A * x *\n\nA * x *\n\nA * x *", 17}} {
		t.Run(tc.name, func(t *testing.T) {
			origin := &md018LateContext{Context: context.Background(), stop: 1 << 20, reason: context.Canceled}
			var observed engine.RuleOutcome
			r := registry(t, []engine.Registration{{ID: "MD037", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				inventory, err := c.DelimiterStyle(ctx)
				if err != nil {
					t.Fatal(err)
				}
				origin.calls = 0
				origin.stop = tc.final
				observed = compareDelimiterStyle(context.Background(), c, inventory.Whitespace)
				if !errors.Is(observed.Err, context.Canceled) || observed.Findings != nil || origin.calls != tc.final {
					t.Fatal(observed, origin.calls)
				}
				return observed
			}}})
			in, _ := engine.NewInput("doc.md", []byte(tc.source), md037Settings(), engine.DocumentContext{})
			out, err := r.Lint(origin, []engine.Input{in}, engine.RunOptions{})
			if err != nil || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
				t.Fatal(out, err)
			}
		})
	}
}

func TestMD037CallbackStickyOriginalControls(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				limits := parser.DefaultLimits()
				limits.OperationTime = 20 * time.Millisecond
				b, _ := parser.NewOperationBudget(limits)
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD037", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					inventory, err := c.DelimiterStyle(ctx)
					if err != nil {
						t.Fatal(err)
					}
					switch kind {
					case "work":
						_ = b.ChargeWork(limits.WorkUnits)
					case "nodes":
						_ = b.ChargeNodes(limits.TotalNodes)
					case "elapsed":
						time.Sleep(25 * time.Millisecond)
					}
					for range 2 {
						observed = compareDelimiterStyle(ctx, c, inventory.Whitespace)
						var limit *parser.LimitExceeded
						if !errors.As(observed.Err, &limit) || observed.Findings != nil {
							t.Fatal(fmt.Sprint(kind), observed)
						}
					}
					return observed
				}}})
				out := analyzeHeadings(t, r, "A * x *", md037Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: b})
				if out.Complete || len(out.Diagnostics) != 0 {
					t.Fatal(out)
				}
			})
		})
	}
}
