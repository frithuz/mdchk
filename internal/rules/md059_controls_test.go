package rules

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func md059ControlSettings() engine.Settings {
	s := md059Settings()
	s["MD059"] = engine.RuleSetting{Enabled: s["MD059"].Enabled, Options: engine.Options{"phrases": []string{"click here"}}}
	return s
}

func TestMD059ComparisonRejectsLateInvalidPoints(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, fault := range []string{"absent", "foreign", "reversed", "overlap", "utf8", "crlf", "unmatched-crlf"} {
				t.Run(fault, func(t *testing.T) {
					called := false
					r := registry(t, []engine.Registration{{ID: "MD059", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						items, err := c.LinkLabels(ctx)
						if err != nil || len(items.Items) != 2 {
							t.Fatal(items, err)
						}
						phrases := []string{"click here"}
						pre := compareLinkLabels(ctx, c, items.Items, phrases)
						if pre.Err != nil || len(pre.Findings) != 2 || pre.Findings[0].Span.Start() != 1 || pre.Findings[1].Span.Start() != 29 || pre.Findings[1].Span.End() != 39 {
							t.Fatal("positive", pre)
						}
						var bad parser.Span
						switch fault {
						case "foreign":
							s := parser.NewSource(c.Source().Bytes())
							bad, _ = s.Span(29, 39)
						case "reversed":
							items.Items[0].Span, _ = c.Source().Span(29, 39)
							bad, _ = c.Source().Span(1, 11)
						case "overlap":
							bad, _ = c.Source().Span(10, 39)
						case "utf8":
							bad, _ = c.Source().Span(25, 39)
						case "crlf", "unmatched-crlf":
							bad, _ = c.Source().Span(27, 39)
						}
						if fault == "unmatched-crlf" {
							items.Items[1].Text = "Guide"
						}
						items.Items[1].Span = bad
						out := compareLinkLabels(ctx, c, items.Items, phrases)
						if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
							t.Fatal(out)
						}
						called = true
						return out
					}}})
					analyzeHeadings(t, r, md059Example+"\n\né\r\n"+md059Example, md059ControlSettings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
					if !called {
						t.Fatal("callback missing")
					}
				})
			}
		})
	}
}

func TestMD059ComparisonAndDistinctOuterFinalControls(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, source := range []string{"", "clean", "`code`", "```sh\n$ pwd\nout\n```\n", md059Example, md059Example + "\n<div>\n<x a=\"\n\n" + md059Example} {
				for _, whole := range []bool{false, true} {
					for _, originating := range []bool{false, true} {
						for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
							t.Run(fmt.Sprintf("%q/whole-%v/origin-%v/%v", source, whole, originating, reason), func(t *testing.T) {
								origin := &md014PublicationStop{Context: context.Background(), reason: reason}
								called := false
								r := registry(t, []engine.Registration{{ID: "MD059", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
									items, e := c.LinkLabels(ctx)
									if e != nil {
										t.Fatal(e)
									}
									phrases := []string{"click here"}
									if source == md059Example {
										pre := compareLinkLabels(ctx, c, items.Items, phrases)
										if pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 1 || pre.Findings[0].Span.End() != 11 {
											t.Fatal("actual positive precondition", pre)
										}
									}
									gate := ".compareLinkLabels.func1"
									if whole {
										gate = ".uninformativeLinkLabels.func1"
									}
									caller := &md014PublicationStop{Context: context.Background(), gate: gate, reason: reason}
									var target context.Context = caller
									if originating {
										origin.gate = gate
										target = context.Background()
									}
									var out engine.RuleOutcome
									if whole {
										out = uninformativeLinkLabels(target, c)
									} else {
										out = compareLinkLabels(target, c, items.Items, phrases)
									}
									reached := caller.reached
									if originating {
										reached = origin.reached
									}
									if !reached || !errors.Is(out.Err, reason) || out.Findings != nil || out.IncompleteReason != "" {
										t.Fatal(out, reached)
									}
									called = true
									return out
								}}})
								in, e := engine.NewInput("literal.md", []byte(source), md059ControlSettings(), engine.DocumentContext{Flavor: flavor})
								if e != nil {
									t.Fatal(e)
								}
								_, e = r.Lint(origin, []engine.Input{in}, engine.RunOptions{})
								if e != nil || !called {
									t.Fatal(e, called)
								}
							})
						}
					}
				}
			}
		})
	}
}

func TestMD059ComparisonAndOuterFinalStickyDiscard(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, kind := range []string{"work", "nodes", "elapsed"} {
				for _, outer := range []bool{false, true} {
					for _, originating := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/outer-%v/origin-%v", kind, outer, originating), func(t *testing.T) {
							synctest.Test(t, func(t *testing.T) {
								budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
								gate := ".compareLinkLabels.func1"
								if outer {
									gate = ".uninformativeLinkLabels.func1"
								}
								origin := &md014FinalQuota{Context: context.Background(), budget: budget, kind: kind, gate: gate}
								called := false
								r := registry(t, []engine.Registration{{ID: "MD059", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
									phrases := []string{"click here"}
									items, e := c.LinkLabels(ctx)
									pre := compareLinkLabels(ctx, c, items.Items, phrases)
									if e != nil || pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 1 {
										t.Fatal("positive precondition", pre, e)
									}
									caller := &md014FinalQuota{Context: context.Background(), budget: budget, kind: kind, gate: gate, armed: true}
									var target context.Context = caller
									if originating {
										origin.armed = true
										target = context.Background()
									}
									var out engine.RuleOutcome
									if outer {
										out = uninformativeLinkLabels(target, c)
									} else {
										out = compareLinkLabels(target, c, items.Items, phrases)
									}
									var stop *parser.LimitExceeded
									want := "work-units"
									if kind == "nodes" {
										want = "total-nodes"
									}
									if kind == "elapsed" {
										want = "operation-time"
									}
									if !errors.As(out.Err, &stop) || stop.Name != want || out.Findings != nil || !caller.reached && !origin.reached {
										t.Fatal(out, caller.reached, origin.reached)
									}
									called = true
									return out
								}}})
								in, e := engine.NewInput("literal.md", []byte(md059Example), md059ControlSettings(), engine.DocumentContext{Flavor: flavor})
								if e != nil {
									t.Fatal(e)
								}
								out, e := r.Lint(origin, []engine.Input{in}, engine.RunOptions{Budget: budget})
								if e != nil || !called || out.Complete || len(out.Diagnostics) != 0 {
									t.Fatal(out, e, called)
								}
							})
						})
					}
				}
			}
		})
	}
}

func TestMD059ExhaustedBudgetIsSticky(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, kind := range []string{"work", "nodes"} {
			t.Run(flavor+"/"+kind, func(t *testing.T) {
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				called := false
				r := registry(t, []engine.Registration{{ID: "MD059", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					called = true
					if pre := uninformativeLinkLabels(ctx, c); pre.Err != nil || len(pre.Findings) != 1 {
						t.Fatal("positive precondition", pre)
					}
					if kind == "work" {
						_ = budget.ChargeWork(budget.Limits().WorkUnits)
					} else {
						_ = budget.ChargeNodes(budget.Limits().TotalNodes)
					}
					var out engine.RuleOutcome
					for range 2 {
						out = uninformativeLinkLabels(ctx, c)
						var stop *parser.LimitExceeded
						if !errors.As(out.Err, &stop) || out.Findings != nil || out.IncompleteReason != "" {
							t.Fatal(out)
						}
					}
					return out
				}}})
				analyzeHeadings(t, r, md059Example, md059ControlSettings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{Budget: budget})
				if !called {
					t.Fatal("actual callback not reached")
				}
			})
		}
	}
}
