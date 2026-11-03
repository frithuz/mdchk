package rules

import (
	"context"
	"errors"
	"fmt"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
	"testing/synctest"
)

func TestMD045FinalControlsDiscardFindingsAndProblems(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, partial := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%s/partial-%v/origin-%v/%v", flavor, partial, originating, reason), func(t *testing.T) {
						source := "![](x)"
						if partial {
							source += " <img alt=\"\" alt=x>"
						}
						origin := &md014PublicationStop{Context: context.Background(), reason: reason}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD045", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							pre := imageAlternatives(ctx, c)
							if pre.Err != nil || len(pre.Findings) != 1 || len(pre.ParsingProblems) != map[bool]int{false: 0, true: 1}[partial] {
								t.Fatal("positive precondition", pre)
							}
							gate := ".imageAlternatives.func1"
							caller := &md014PublicationStop{Context: context.Background(), gate: gate, reason: reason}
							var target context.Context = caller
							if originating {
								origin.gate = gate
								target = context.Background()
							}
							out := imageAlternatives(target, c)
							if !errors.Is(out.Err, reason) || out.Findings != nil || out.ParsingProblems != nil || out.IncompleteReason != "" || !caller.reached && !origin.reached {
								t.Fatal(out, caller.reached, origin.reached)
							}
							called = true
							return out
						}}})
						in, e := engine.NewInput("literal.md", []byte(source), md045Settings(), engine.DocumentContext{Flavor: flavor})
						if e != nil {
							t.Fatal(e)
						}
						out, e := r.Lint(origin, []engine.Input{in}, engine.RunOptions{})
						if e != nil || !called || out.Complete || len(out.Diagnostics)+len(out.Notes) != 0 {
							t.Fatal(out, e, called)
						}
					})
				}
			}
		}
	}
}

func TestMD045FinalStickyLimitsDiscardAllPublication(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/origin-%v", flavor, kind, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
						gate := ".imageAlternatives.func1"
						origin := &md014FinalQuota{Context: context.Background(), budget: budget, kind: kind, gate: gate}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD045", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							pre := imageAlternatives(ctx, c)
							if pre.Err != nil || len(pre.Findings) != 1 || len(pre.ParsingProblems) != 1 {
								t.Fatal("positive partial precondition", pre)
							}
							caller := &md014FinalQuota{Context: context.Background(), budget: budget, kind: kind, gate: gate, armed: true}
							var target context.Context = caller
							if originating {
								origin.armed = true
								target = context.Background()
							}
							out := imageAlternatives(target, c)
							var stop *parser.LimitExceeded
							if !errors.As(out.Err, &stop) || out.Findings != nil || out.ParsingProblems != nil || out.IncompleteReason != "" || !caller.reached && !origin.reached {
								t.Fatal(out, caller.reached, origin.reached)
							}
							called = true
							return out
						}}})
						in, e := engine.NewInput("literal.md", []byte("![](x) <img alt=\"\" alt=x>"), md045Settings(), engine.DocumentContext{Flavor: flavor})
						if e != nil {
							t.Fatal(e)
						}
						out, e := r.Lint(origin, []engine.Input{in}, engine.RunOptions{Budget: budget})
						if e != nil || !called || out.Complete || len(out.Diagnostics)+len(out.Notes) != 0 {
							t.Fatal(out, e, called)
						}
					})
				})
			}
		}
	}
}

func TestMD045PartialSuppressionAuditRetainsUnknownAbsence(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, positive := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/positive-%v", flavor, positive), func(t *testing.T) {
				source := "<!-- mdchk:disable MD045 -->\n"
				if positive {
					source += "![](x) "
				}
				source += "<img alt=\"\" alt=x>"
				s := md045Settings()
				on := true
				s["MD087"] = engine.RuleSetting{Enabled: &on}
				in, e := engine.NewInput("partial.md", []byte(source), s, engine.DocumentContext{Flavor: flavor})
				if e != nil {
					t.Fatal(e)
				}
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				out, e := registry(t, Registrations()).Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Budget: budget})
				hidden := 0
				if positive {
					hidden = 1
				}
				if e != nil || out.Complete || len(out.Diagnostics)+len(out.Errors) != 0 || len(out.SuppressedDiagnostics) != hidden || len(out.Notes) == 0 || len(out.Coverage) == 0 {
					t.Fatal(out, e)
				}
				seen := false
				for _, record := range out.AnalysisRecords() {
					if a, ok := record.(engine.DirectiveAnalysis); ok {
						decisions, e := a.SuppressionDecisions(context.Background(), budget)
						if e != nil || len(decisions) != 1 {
							t.Fatal(decisions, e)
						}
						want := "suppression-uncertain"
						if positive {
							want = "suppression-used"
						}
						if decisions[0].Rule != "MD045" || decisions[0].Reason != want {
							t.Fatal(decisions)
						}
						seen = true
					}
				}
				if !seen {
					t.Fatal("missing audit decision")
				}
			})
		}
	}
}

func TestMD045InactiveUncertaintyRequiresObservation(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, observe := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/observe-%v", flavor, observe), func(t *testing.T) {
				s := md045Settings()
				on, off := true, false
				s["MD045"] = engine.RuleSetting{Enabled: &off}
				s["MD087"] = engine.RuleSetting{Enabled: &on}
				selection, e := registry(t, Registrations()).SelectRules(s, engine.ValidationProviders{})
				if e != nil {
					t.Fatal(e)
				}
				plan, e := selection.Plan(engine.DocumentContext{Flavor: flavor}, nil)
				if e != nil {
					t.Fatal(e)
				}
				in, e := plan.NewInput("inactive.md", []byte("<!-- mdchk:disable MD045 -->\n<img alt=\"\" alt=x>"))
				if e != nil {
					t.Fatal(e)
				}
				out, trace, e := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{ObserveInactive: observe})
				count := 0
				if observe {
					count = 1
				}
				if e != nil || out.Complete == observe || len(out.Notes) != count || len(out.Coverage) != count || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD087" || len(out.Errors) != 0 {
					t.Fatal(out, e)
				}
				for _, ev := range trace.Evaluations() {
					if ev.Rule == "MD045" {
						want := "not-requested"
						if observe {
							want = "partial"
						}
						if ev.Observational != want {
							t.Fatal(ev)
						}
					}
				}
			})
		}
	}
}
