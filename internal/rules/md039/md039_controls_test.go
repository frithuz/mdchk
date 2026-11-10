package md039_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules/md039"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

const md039Example = "[ x](y)"

func TestMD039ComparisonRejectsLateInvalidPoints(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, fault := range []string{"absent", "foreign", "zero-width-length", "reversed", "utf8", "crlf"} {
				t.Run(fault, func(t *testing.T) {
					called := false
					r := ruletest.Registry(t, []engine.Registration{{ID: "MD039", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						items, err := c.LinkLabelWhitespace(ctx)
						if err != nil || len(items.Spans) != 2 {
							t.Fatal(items, err)
						}
						pre := md039.CompareLinkLabelWhitespace(ctx, c, items.Spans)
						if pre.Err != nil || len(pre.Findings) != 2 || pre.Findings[0].Span.Start() != 1 {
							t.Fatal("positive", pre)
						}
						var bad parser.Span
						switch fault {
						case "foreign":
							s := parser.NewSource(c.Source().Bytes())
							bad, _ = s.Span(14, 15)
						case "zero-width-length":
							bad, _ = c.Source().Span(14, 14)
						case "reversed":
							bad, _ = c.Source().Span(1, 2)
						case "utf8":
							bad, _ = c.Source().Span(10, 11)
						case "crlf":
							bad, _ = c.Source().Span(12, 13)
						}
						items.Spans[1] = bad
						out := md039.CompareLinkLabelWhitespace(ctx, c, items.Spans)
						if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
							t.Fatal(out)
						}
						called = true
						return out
					}}})
					ruletest.AnalyzeDocument(t, r, md039Example+"\n\né\r\n"+md039Example, md039Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
					if !called {
						t.Fatal("callback missing")
					}
				})
			}

		})
	}
}

func TestMD039ComparisonAndDistinctOuterFinalControls(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, source := range []string{"", "clean", "`code`", "```sh\n$ pwd\nout\n```\n", "[ x](y)", "[ x](y)\n<div>\n<x a=\"\n\n[ x](y)"} {
				for _, whole := range []bool{false, true} {
					for _, originating := range []bool{false, true} {
						for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
							t.Run(fmt.Sprintf("%q/whole-%v/origin-%v/%v", source, whole, originating, reason), func(t *testing.T) {
								origin := &ruletest.GateStopContext{Context: context.Background(), Reason: reason}
								called := false
								r := ruletest.Registry(t, []engine.Registration{{ID: "MD039", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
									items, e := c.LinkLabelWhitespace(ctx)
									if e != nil {
										t.Fatal(e)
									}
									if source == "[ x](y)" {
										pre := md039.CompareLinkLabelWhitespace(ctx, c, items.Spans)
										if pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 1 || pre.Findings[0].Span.End() != 2 {
											t.Fatal("actual positive precondition", pre)
										}
									}
									gate := ".compareLinkLabelWhitespace.func1"
									if whole {
										gate = ".linkLabelWhitespace.func1"
									}
									caller := &ruletest.GateStopContext{Context: context.Background(), Gate: gate, Reason: reason}
									var target context.Context = caller
									if originating {
										origin.Gate = gate
										target = context.Background()
									}
									var out engine.RuleOutcome
									if whole {
										out = md039.LinkLabelWhitespace(target, c)
									} else {
										out = md039.CompareLinkLabelWhitespace(target, c, items.Spans)
									}
									reached := caller.Reached
									if originating {
										reached = origin.Reached
									}
									if !reached || !errors.Is(out.Err, reason) || out.Findings != nil || out.IncompleteReason != "" {
										t.Fatal(out, reached)
									}
									called = true
									return out
								}}})
								in, e := engine.NewInput("literal.md", []byte(source), md039Settings(), engine.DocumentContext{Flavor: flavor})
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

func TestMD039ComparisonAndOuterFinalStickyDiscard(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, kind := range []string{"work", "nodes", "elapsed"} {
				for _, outer := range []bool{false, true} {
					for _, originating := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/outer-%v/origin-%v", kind, outer, originating), func(t *testing.T) {
							synctest.Test(t, func(t *testing.T) {
								budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
								gate := ".compareLinkLabelWhitespace.func1"
								if outer {
									gate = ".linkLabelWhitespace.func1"
								}
								origin := &ruletest.GateQuotaContext{Context: context.Background(), Budget: budget, Kind: kind, Gate: gate}
								called := false
								r := ruletest.Registry(t, []engine.Registration{{ID: "MD039", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
									items, e := c.LinkLabelWhitespace(ctx)
									pre := md039.CompareLinkLabelWhitespace(ctx, c, items.Spans)
									if e != nil || pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 1 {
										t.Fatal("positive precondition", pre, e)
									}
									caller := &ruletest.GateQuotaContext{Context: context.Background(), Budget: budget, Kind: kind, Gate: gate, Armed: true}
									var target context.Context = caller
									if originating {
										origin.Armed = true
										target = context.Background()
									}
									var out engine.RuleOutcome
									if outer {
										out = md039.LinkLabelWhitespace(target, c)
									} else {
										out = md039.CompareLinkLabelWhitespace(target, c, items.Spans)
									}
									var stop *parser.LimitExceeded
									want := "work-units"
									if kind == "nodes" {
										want = "total-nodes"
									}
									if kind == "elapsed" {
										want = "operation-time"
									}
									if !errors.As(out.Err, &stop) || stop.Name != want || out.Findings != nil || !caller.Reached && !origin.Reached {
										t.Fatal(out, caller.Reached, origin.Reached)
									}
									called = true
									return out
								}}})
								in, e := engine.NewInput("literal.md", []byte("[ x](y)"), md039Settings(), engine.DocumentContext{Flavor: flavor})
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
