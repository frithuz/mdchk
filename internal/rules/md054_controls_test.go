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

func md054ControlSettings() engine.Settings {
	s := md054Settings()
	s["MD054"] = engine.RuleSetting{Enabled: s["MD054"].Enabled, Options: engine.Options{"allowed_styles": []string{"full_reference"}}}
	return s
}

func TestMD054ComparisonRejectsLateInvalidPoints(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, fault := range []string{"absent", "foreign", "zero-width", "reversed", "utf8", "crlf", "style", "allowed-crlf"} {
				t.Run(fault, func(t *testing.T) {
					called := false
					r := registry(t, []engine.Registration{{ID: "MD054", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						items, err := c.LinkRepresentations(ctx)
						if err != nil || len(items.Items) != 2 {
							t.Fatal(items, err)
						}
						allowed := []string{"full_reference"}
						pre := compareLinkRepresentations(ctx, c, items.Items, allowed)
						if pre.Err != nil || len(pre.Findings) != 2 || pre.Findings[0].Span.Start() != 0 || pre.Findings[1].Span.Start() != 23 {
							t.Fatal("positive", pre)
						}
						var bad parser.Span
						switch fault {
						case "foreign":
							s := parser.NewSource(c.Source().Bytes())
							bad, _ = s.Span(23, 40)
						case "zero-width":
							bad, _ = c.Source().Span(23, 23)
						case "reversed":
							items.Items[0].Span, _ = c.Source().Span(23, 40)
							bad, _ = c.Source().Span(0, 17)
						case "utf8":
							bad, _ = c.Source().Span(20, 40)
						case "crlf", "allowed-crlf":
							bad, _ = c.Source().Span(22, 40)
						case "style":
							bad = items.Items[1].Span
							items.Items[1].Style = "reference"
						}
						if fault == "allowed-crlf" {
							allowed = []string{"inline"}
						}
						items.Items[1].Span = bad
						out := compareLinkRepresentations(ctx, c, items.Items, allowed)
						if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
							t.Fatal(out)
						}
						called = true
						return out
					}}})
					analyzeHeadings(t, r, md054Example+"\n\né\r\n"+md054Example, md054ControlSettings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
					if !called {
						t.Fatal("callback missing")
					}
				})
			}
		})
	}
}

func TestMD054ComparisonAndDistinctOuterFinalControls(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, source := range []string{"", "clean", "`code`", "```sh\n$ pwd\nout\n```\n", md054Example, md054Example + "\n<div>\n<x a=\"\n\n" + md054Example} {
				for _, whole := range []bool{false, true} {
					for _, originating := range []bool{false, true} {
						for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
							t.Run(fmt.Sprintf("%q/whole-%v/origin-%v/%v", source, whole, originating, reason), func(t *testing.T) {
								origin := &gateStopContext{Context: context.Background(), reason: reason}
								called := false
								r := registry(t, []engine.Registration{{ID: "MD054", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
									items, e := c.LinkRepresentations(ctx)
									if e != nil {
										t.Fatal(e)
									}
									allowed := []string{"full_reference"}
									if source == md054Example {
										pre := compareLinkRepresentations(ctx, c, items.Items, allowed)
										if pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 0 || pre.Findings[0].Span.End() != 17 {
											t.Fatal("actual positive precondition", pre)
										}
									}
									gate := ".compareLinkRepresentations.func1"
									if whole {
										gate = ".linkRepresentationStyle.func1"
									}
									caller := &gateStopContext{Context: context.Background(), gate: gate, reason: reason}
									var target context.Context = caller
									if originating {
										origin.gate = gate
										target = context.Background()
									}
									var out engine.RuleOutcome
									if whole {
										out = linkRepresentationStyle(target, c)
									} else {
										out = compareLinkRepresentations(target, c, items.Items, allowed)
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
								in, e := engine.NewInput("literal.md", []byte(source), md054ControlSettings(), engine.DocumentContext{Flavor: flavor})
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

func TestMD054ComparisonAndOuterFinalStickyDiscard(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, kind := range []string{"work", "nodes", "elapsed"} {
				for _, outer := range []bool{false, true} {
					for _, originating := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/outer-%v/origin-%v", kind, outer, originating), func(t *testing.T) {
							synctest.Test(t, func(t *testing.T) {
								budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
								gate := ".compareLinkRepresentations.func1"
								if outer {
									gate = ".linkRepresentationStyle.func1"
								}
								origin := &gateQuotaContext{Context: context.Background(), budget: budget, kind: kind, gate: gate}
								called := false
								r := registry(t, []engine.Registration{{ID: "MD054", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
									allowed := []string{"full_reference"}
									items, e := c.LinkRepresentations(ctx)
									pre := compareLinkRepresentations(ctx, c, items.Items, allowed)
									if e != nil || pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 0 {
										t.Fatal("positive precondition", pre, e)
									}
									caller := &gateQuotaContext{Context: context.Background(), budget: budget, kind: kind, gate: gate, armed: true}
									var target context.Context = caller
									if originating {
										origin.armed = true
										target = context.Background()
									}
									var out engine.RuleOutcome
									if outer {
										out = linkRepresentationStyle(target, c)
									} else {
										out = compareLinkRepresentations(target, c, items.Items, allowed)
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
								in, e := engine.NewInput("literal.md", []byte(md054Example), md054ControlSettings(), engine.DocumentContext{Flavor: flavor})
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

func TestMD054ExhaustedBudgetIsSticky(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, kind := range []string{"work", "nodes"} {
			t.Run(flavor+"/"+kind, func(t *testing.T) {
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				called := false
				r := registry(t, []engine.Registration{{ID: "MD054", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					called = true
					if pre := linkRepresentationStyle(ctx, c); pre.Err != nil || len(pre.Findings) != 1 {
						t.Fatal("positive precondition", pre)
					}
					if kind == "work" {
						_ = budget.ChargeWork(budget.Limits().WorkUnits)
					} else {
						_ = budget.ChargeNodes(budget.Limits().TotalNodes)
					}
					var out engine.RuleOutcome
					for range 2 {
						out = linkRepresentationStyle(ctx, c)
						var stop *parser.LimitExceeded
						if !errors.As(out.Err, &stop) || out.Findings != nil || out.IncompleteReason != "" {
							t.Fatal(out)
						}
					}
					return out
				}}})
				analyzeHeadings(t, r, md054Example, md054ControlSettings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{Budget: budget})
				if !called {
					t.Fatal("actual callback not reached")
				}
			})
		}
	}
}
