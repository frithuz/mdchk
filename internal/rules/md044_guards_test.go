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

func TestMD044CallbackFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 1}, {"clean", "a", 1}, {"protected", "```\nx\n```", 1}, {"findings", "github", 4}, {"growth", "github-github-github", 10}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+reason.Error(), func(t *testing.T) {
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD044", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					inventory, err := c.ConfiguredNames(ctx)
					if err != nil {
						t.Fatal(err)
					}
					late := &md018LateContext{Context: ctx, stop: tc.final, reason: reason}
					observed = compareConfiguredNames(late, c, inventory)
					return observed
				}}})
				out := analyzeHeadings(t, r, tc.source, md044Settings([]string{"GitHub"}, false), engine.DocumentContext{}, engine.RunOptions{})
				if !errors.Is(observed.Err, reason) || observed.Findings != nil || len(out.Diagnostics) != 0 || out.Complete || len(out.Interruptions) != 1 {
					t.Fatal(observed, out)
				}
			})
		}
	}
}

func TestMD044CallbackOriginFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 1}, {"clean", "a", 1}, {"protected", "```\nx\n```", 1}, {"findings", "github", 4}, {"growth", "github-github-github", 10}} {
		t.Run(tc.name, func(t *testing.T) {
			origin := &md018LateContext{Context: context.Background(), stop: 1 << 20, reason: context.Canceled}
			var observed engine.RuleOutcome
			r := registry(t, []engine.Registration{{ID: "MD044", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				inventory, err := c.ConfiguredNames(ctx)
				if err != nil {
					t.Fatal(err)
				}
				origin.calls = 0
				origin.stop = tc.final
				observed = compareConfiguredNames(context.Background(), c, inventory)
				if !errors.Is(observed.Err, context.Canceled) || observed.Findings != nil || origin.calls != tc.final {
					t.Fatal(observed, origin.calls)
				}
				return observed
			}}})
			in, _ := engine.NewInput("doc.md", []byte(tc.source), md044Settings([]string{"GitHub"}, false), engine.DocumentContext{})
			out, err := r.Lint(origin, []engine.Input{in}, engine.RunOptions{})
			if err != nil || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
				t.Fatal(out, err)
			}
		})
	}
}

func TestMD044CallbackStickyOriginalControls(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				limits := parser.DefaultLimits()
				limits.OperationTime = 20 * time.Millisecond
				b, _ := parser.NewOperationBudget(limits)
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD044", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					inventory, err := c.ConfiguredNames(ctx)
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
						observed = compareConfiguredNames(ctx, c, inventory)
						var limit *parser.LimitExceeded
						if !errors.As(observed.Err, &limit) || observed.Findings != nil {
							t.Fatal(fmt.Sprint(kind), observed)
						}
					}
					return observed
				}}})
				out := analyzeHeadings(t, r, "github", md044Settings([]string{"GitHub"}, false), engine.DocumentContext{}, engine.RunOptions{Budget: b})
				if out.Complete || len(out.Diagnostics) != 0 {
					t.Fatal(out)
				}
			})
		})
	}
}

func TestMD044CallbackManualFindings(t *testing.T) {
	r := registry(t, []engine.Registration{{ID: "MD044", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
		out := configuredNameCapitalization(ctx, c)
		if out.Err != nil || len(out.Findings) != 1 {
			t.Fatal(out)
		}
		for _, f := range out.Findings {
			if f.FixEligible {
				t.Fatal("manual callback claimed correction eligibility", f)
			}
		}
		return out
	}}})
	out := analyzeHeadings(t, r, "github", md044Settings([]string{"GitHub"}, false), engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 1 || len(out.Changes)+len(out.DeferredFixes)+len(out.Documents) != 0 {
		t.Fatal(out)
	}
}
