package md044_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules/md044"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

func TestMD044CallbackFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 1}, {"clean", "a", 1}, {"protected", "```\nx\n```", 1}, {"findings", "github", 4}, {"growth", "github-github-github", 10}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+reason.Error(), func(t *testing.T) {
				var observed engine.RuleOutcome
				r := ruletest.Registry(t, []engine.Registration{{ID: "MD044", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					inventory, replacements, err := c.ConfiguredNameReplacements(ctx)
					if err != nil {
						t.Fatal(err)
					}
					late := &ruletest.LateStopContext{Context: ctx, Stop: tc.final, Reason: reason}
					observed = md044.CompareConfiguredNames(late, c, inventory, replacements)
					return observed
				}}})
				out := ruletest.AnalyzeDocument(t, r, tc.source, md044Settings([]string{"GitHub"}, false), engine.DocumentContext{}, engine.RunOptions{})
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
	}{{"empty", "", 4}, {"clean", "a", 6}, {"protected", "```\nx\n```", 5}, {"findings", "github", 10}, {"growth", "github-github-github", 18}} {
		t.Run(tc.name, func(t *testing.T) {
			origin := &ruletest.LateStopContext{Context: context.Background(), Stop: 1 << 20, Reason: context.Canceled}
			var observed engine.RuleOutcome
			r := ruletest.Registry(t, []engine.Registration{{ID: "MD044", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				inventory, replacements, err := c.ConfiguredNameReplacements(ctx)
				if err != nil {
					t.Fatal(err)
				}
				origin.Calls = 0
				origin.Stop = tc.final
				observed = md044.CompareConfiguredNames(context.Background(), c, inventory, replacements)
				if !errors.Is(observed.Err, context.Canceled) || observed.Findings != nil || origin.Calls != tc.final {
					t.Fatal(observed, origin.Calls)
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
				r := ruletest.Registry(t, []engine.Registration{{ID: "MD044", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					inventory, replacements, err := c.ConfiguredNameReplacements(ctx)
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
						observed = md044.CompareConfiguredNames(ctx, c, inventory, replacements)
						var limit *parser.LimitExceeded
						if !errors.As(observed.Err, &limit) || observed.Findings != nil {
							t.Fatal(fmt.Sprint(kind), observed)
						}
					}
					return observed
				}}})
				out := ruletest.AnalyzeDocument(t, r, "github", md044Settings([]string{"GitHub"}, false), engine.DocumentContext{}, engine.RunOptions{Budget: b})
				if out.Complete || len(out.Diagnostics) != 0 {
					t.Fatal(out)
				}
			})
		})
	}
}

func TestMD044CallbackEligibleFindings(t *testing.T) {
	r := ruletest.Registry(t, []engine.Registration{{ID: "MD044", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
		out := md044.Registration().Check(ctx, c)
		if out.Err != nil || len(out.Findings) != 1 {
			t.Fatal(out)
		}
		for _, f := range out.Findings {
			if !f.FixEligible {
				t.Fatal("unambiguous finding not correction-eligible", f)
			}
		}
		return out
	}}})
	out := ruletest.AnalyzeDocument(t, r, "github", md044Settings([]string{"GitHub"}, false), engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 1 || len(out.Changes)+len(out.DeferredFixes)+len(out.Documents) != 0 {
		t.Fatal(out)
	}
}
