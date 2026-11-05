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

const md055ControlSource = "| A | B |\r\n| --- | --- |\r\né | 2\r\n"

func TestMD055ComparisonRejectsLateInvalidRows(t *testing.T) {
	for _, fault := range []string{"foreign", "reversed", "empty", "out-of-range", "utf8", "crlf", "role", "pipe-offset", "unterminated", "style"} {
		t.Run(fault, func(t *testing.T) {
			called := false
			r := registry(t, []engine.Registration{{ID: "MD055", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				rows, err := c.TableOuterPipes(ctx)
				if err != nil || len(rows.Rows) != 3 {
					t.Fatal(rows, err)
				}
				pre := compareTableOuterPipes(ctx, c, rows.Rows, "consistent")
				if pre.Err != nil || len(pre.Findings) != 2 || pre.Findings[0].Span.Start() != 26 || pre.Findings[0].Span.End() != 26 || pre.Findings[1].Span.Start() != 32 || pre.Findings[1].Span.End() != 32 {
					t.Fatal("positive", pre)
				}
				style := "consistent"
				switch fault {
				case "foreign":
					rows.Rows[0].LeadingPipe, _ = parser.NewSource(c.Source().Bytes()).Span(0, 1)
				case "reversed":
					rows.Rows[2].Start = 5
				case "empty":
					rows.Rows[2].End = rows.Rows[2].Start
				case "out-of-range":
					rows.Rows[2].End = c.Source().Len() + 1
				case "utf8":
					rows.Rows[2].Start = 27
				case "crlf":
					rows.Rows[2].End = 33
				case "role":
					rows.Rows[1].Role = "body"
				case "pipe-offset":
					rows.Rows[0].LeadingPipe, _ = c.Source().Span(8, 9)
				case "unterminated":
					rows.Rows = rows.Rows[:1]
				case "style":
					style = "both"
				}
				out := compareTableOuterPipes(ctx, c, rows.Rows, style)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				called = true
				return out
			}}})
			analyzeHeadings(t, r, md055ControlSource, md055Settings(), engine.DocumentContext{Flavor: "gfm"}, engine.RunOptions{})
			if !called {
				t.Fatal("callback missing")
			}
		})
	}
}

func TestMD055ComparisonAndDistinctOuterFinalControls(t *testing.T) {
	for _, source := range []string{"", "clean", "```\n| A | B |\n--- | ---\n```\n", md055Example, md055ControlSource} {
		for _, whole := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%q/whole-%v/origin-%v/%v", source, whole, originating, reason), func(t *testing.T) {
						origin := &md014PublicationStop{Context: context.Background(), reason: reason}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD055", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							rows, e := c.TableOuterPipes(ctx)
							if e != nil {
								t.Fatal(e)
							}
							if source == md055Example {
								pre := compareTableOuterPipes(ctx, c, rows.Rows, "consistent")
								if pre.Err != nil || len(pre.Findings) != 2 || pre.Findings[0].Span.Start() != 16 || pre.Findings[1].Span.End() != 25 {
									t.Fatal("actual positive precondition", pre)
								}
							}
							gate := ".compareTableOuterPipes.func1"
							if whole {
								gate = ".outerTablePipes.func1"
							}
							caller := &md014PublicationStop{Context: context.Background(), gate: gate, reason: reason}
							var target context.Context = caller
							if originating {
								origin.gate = gate
								target = context.Background()
							}
							var out engine.RuleOutcome
							if whole {
								out = outerTablePipes(target, c)
							} else {
								out = compareTableOuterPipes(target, c, rows.Rows, "consistent")
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
						in, e := engine.NewInput("literal.md", []byte(source), md055Settings(), engine.DocumentContext{Flavor: "gfm"})
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
}

func TestMD055ComparisonAndOuterFinalStickyDiscard(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		for _, outer := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/outer-%v/origin-%v", kind, outer, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
						gate := ".compareTableOuterPipes.func1"
						if outer {
							gate = ".outerTablePipes.func1"
						}
						origin := &md014FinalQuota{Context: context.Background(), budget: budget, kind: kind, gate: gate}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD055", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							rows, e := c.TableOuterPipes(ctx)
							pre := compareTableOuterPipes(ctx, c, rows.Rows, "consistent")
							if e != nil || pre.Err != nil || len(pre.Findings) != 2 || pre.Findings[0].Span.Start() != 16 {
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
								out = outerTablePipes(target, c)
							} else {
								out = compareTableOuterPipes(target, c, rows.Rows, "consistent")
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
						in, e := engine.NewInput("literal.md", []byte(md055Example), md055Settings(), engine.DocumentContext{Flavor: "gfm"})
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
}

func TestMD055ExhaustedBudgetIsSticky(t *testing.T) {
	for _, kind := range []string{"work", "nodes"} {
		t.Run(kind, func(t *testing.T) {
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			called := false
			r := registry(t, []engine.Registration{{ID: "MD055", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				called = true
				if pre := outerTablePipes(ctx, c); pre.Err != nil || len(pre.Findings) != 2 {
					t.Fatal("positive precondition", pre)
				}
				if kind == "work" {
					_ = budget.ChargeWork(budget.Limits().WorkUnits)
				} else {
					_ = budget.ChargeNodes(budget.Limits().TotalNodes)
				}
				var out engine.RuleOutcome
				for range 2 {
					out = outerTablePipes(ctx, c)
					var stop *parser.LimitExceeded
					if !errors.As(out.Err, &stop) || out.Findings != nil || out.IncompleteReason != "" {
						t.Fatal(out)
					}
				}
				return out
			}}})
			analyzeHeadings(t, r, md055Example, md055Settings(), engine.DocumentContext{Flavor: "gfm"}, engine.RunOptions{Budget: budget})
			if !called {
				t.Fatal("actual callback not reached")
			}
		})
	}
}
