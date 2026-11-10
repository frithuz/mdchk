package md034_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules/md034"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

const md034Example = "https://example.org"

func TestMD034ComparisonRejectsLateInvalidPoints(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, fault := range []string{"absent", "foreign", "zero-width-length", "reversed", "utf8", "crlf"} {
				t.Run(fault, func(t *testing.T) {
					called := false
					r := ruletest.Registry(t, []engine.Registration{{ID: "MD034", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						items, err := c.BareURLs(ctx)
						if err != nil || len(items.Spans) != 2 {
							t.Fatal(items, err)
						}
						pre := md034.CompareBareURLs(ctx, c, items.Spans)
						if pre.Err != nil || len(pre.Findings) != 2 || pre.Findings[0].Span.Start() != 0 {
							t.Fatal("positive", pre)
						}
						var bad parser.Span
						switch fault {
						case "foreign":
							s := parser.NewSource(c.Source().Bytes())
							bad, _ = s.Span(25, 44)
						case "zero-width-length":
							bad, _ = c.Source().Span(25, 25)
						case "reversed":
							bad, _ = c.Source().Span(0, 19)
						case "utf8":
							bad, _ = c.Source().Span(22, 23)
						case "crlf":
							bad, _ = c.Source().Span(24, 25)
						}
						items.Spans[1] = bad
						out := md034.CompareBareURLs(ctx, c, items.Spans)
						if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
							t.Fatal(out)
						}
						called = true
						return out
					}}})
					ruletest.AnalyzeDocument(t, r, md034Example+"\n\né\r\n"+md034Example, md034Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
					if !called {
						t.Fatal("callback missing")
					}
				})
			}

		})
	}
}

func TestMD034ComparisonAndDistinctOuterFinalControls(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, source := range []string{"", "clean", "`code`", "```sh\n$ pwd\nout\n```\n", "https://example.org", "https://example.org\n<div>\n<x a=\"\n\nhttps://example.org"} {
				for _, whole := range []bool{false, true} {
					for _, originating := range []bool{false, true} {
						for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
							t.Run(fmt.Sprintf("%q/whole-%v/origin-%v/%v", source, whole, originating, reason), func(t *testing.T) {
								origin := &ruletest.GateStopContext{Context: context.Background(), Reason: reason}
								called := false
								r := ruletest.Registry(t, []engine.Registration{{ID: "MD034", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
									items, e := c.BareURLs(ctx)
									if e != nil {
										t.Fatal(e)
									}
									if source == "https://example.org" {
										pre := md034.CompareBareURLs(ctx, c, items.Spans)
										if pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 0 || pre.Findings[0].Span.End() != 19 {
											t.Fatal("actual positive precondition", pre)
										}
									}
									gate := ".compareBareURLs.func1"
									if whole {
										gate = ".bareURLs.func1"
									}
									caller := &ruletest.GateStopContext{Context: context.Background(), Gate: gate, Reason: reason}
									var target context.Context = caller
									if originating {
										origin.Gate = gate
										target = context.Background()
									}
									var out engine.RuleOutcome
									if whole {
										out = md034.BareURLs(target, c)
									} else {
										out = md034.CompareBareURLs(target, c, items.Spans)
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
								in, e := engine.NewInput("literal.md", []byte(source), md034Settings(), engine.DocumentContext{Flavor: flavor})
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

func TestMD034ComparisonAndOuterFinalStickyDiscard(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, kind := range []string{"work", "nodes", "elapsed"} {
				for _, outer := range []bool{false, true} {
					for _, originating := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/outer-%v/origin-%v", kind, outer, originating), func(t *testing.T) {
							synctest.Test(t, func(t *testing.T) {
								budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
								gate := ".compareBareURLs.func1"
								if outer {
									gate = ".bareURLs.func1"
								}
								origin := &ruletest.GateQuotaContext{Context: context.Background(), Budget: budget, Kind: kind, Gate: gate}
								called := false
								r := ruletest.Registry(t, []engine.Registration{{ID: "MD034", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
									items, e := c.BareURLs(ctx)
									pre := md034.CompareBareURLs(ctx, c, items.Spans)
									if e != nil || pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 0 {
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
										out = md034.BareURLs(target, c)
									} else {
										out = md034.CompareBareURLs(target, c, items.Spans)
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
								in, e := engine.NewInput("literal.md", []byte("https://example.org"), md034Settings(), engine.DocumentContext{Flavor: flavor})
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
