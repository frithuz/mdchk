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

const md060ControlSource = "| A | B |\r\n| --- | --- |\r\né | 2\r\n"

func TestMD060ComparisonRejectsLateInvalidRows(t *testing.T) {
	for _, fault := range []string{"reversed", "empty", "out-of-range", "utf8", "crlf", "role", "unterminated", "style", "no-cells", "field-start", "field-end", "text-order", "untrimmed-text", "split-scalar", "outer-pipe", "empty-edge-cell"} {
		t.Run(fault, func(t *testing.T) {
			called := false
			r := registry(t, []engine.Registration{{ID: "MD060", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				tables, err := c.TableLayout(ctx)
				if err != nil || len(tables.Tables) != 1 || len(tables.Tables[0].Rows) != 3 {
					t.Fatal(tables, err)
				}
				pre := compareTableLayout(ctx, c, tables.Tables, "aligned")
				if pre.Err != nil || len(pre.Findings) != 3 || pre.Findings[0].Span.Start() != 1 || pre.Findings[0].Span.End() != 4 || pre.Findings[1].Span.Start() != 5 || pre.Findings[2].Span.Start() != 26 || pre.Findings[2].Span.End() != 29 || pre.Findings[2].Message != "Cell padding does not match the aligned style; column width is 3." {
					t.Fatal("positive", pre)
				}
				rows := tables.Tables[0].Rows
				style := "aligned"
				switch fault {
				case "reversed":
					rows[2].Start = 5
				case "empty":
					rows[2].End = rows[2].Start
				case "out-of-range":
					rows[2].End = c.Source().Len() + 1
				case "utf8":
					rows[2].Start = 27
				case "crlf":
					rows[2].End = 33
				case "role":
					rows[1].Role = "body"
				case "unterminated":
					tables.Tables[0].Rows = rows[:1]
				case "style":
					style = "wide"
				case "no-cells":
					rows[1].Cells = nil
				case "field-start":
					rows[0].Cells[1].FieldStart++
				case "field-end":
					rows[2].Cells[1].FieldEnd--
				case "text-order":
					rows[0].Cells[0].TextStart = rows[0].Cells[0].TextEnd + 1
				case "untrimmed-text":
					rows[0].Cells[0].TextStart--
				case "split-scalar":
					rows[2].Cells[0].TextEnd--
				case "outer-pipe":
					rows[0].LeadingPipe = false
				case "empty-edge-cell":
					rows[2].Cells[1].TextStart = rows[2].Cells[1].FieldEnd
					rows[2].Cells[1].TextEnd = rows[2].Cells[1].FieldEnd
				}
				out := compareTableLayout(ctx, c, tables.Tables, style)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				called = true
				return out
			}}})
			analyzeHeadings(t, r, md060ControlSource, md060Settings(), engine.DocumentContext{Flavor: "gfm"}, engine.RunOptions{})
			if !called {
				t.Fatal("callback missing")
			}
		})
	}
}

func TestMD060ComparisonAndDistinctOuterFinalControls(t *testing.T) {
	for _, source := range []string{"", "clean", "```\n| A | B |\n--- | ---\n```\n", md060Example, md060ControlSource} {
		for _, whole := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%q/whole-%v/origin-%v/%v", source, whole, originating, reason), func(t *testing.T) {
						origin := &md014PublicationStop{Context: context.Background(), reason: reason}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD060", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							tables, e := c.TableLayout(ctx)
							if e != nil {
								t.Fatal(e)
							}
							if source == md060Example {
								pre := compareTableLayout(ctx, c, tables.Tables, "aligned")
								if pre.Err != nil || len(pre.Findings) != 6 || pre.Findings[0].Span.Start() != 1 || pre.Findings[5].Span.End() != 23 {
									t.Fatal("actual positive precondition", pre)
								}
							}
							gate := ".compareTableLayout.func1"
							if whole {
								gate = ".tableColumnLayout.func1"
							}
							caller := &md014PublicationStop{Context: context.Background(), gate: gate, reason: reason}
							var target context.Context = caller
							if originating {
								origin.gate = gate
								target = context.Background()
							}
							var out engine.RuleOutcome
							if whole {
								out = tableColumnLayout(target, c)
							} else {
								out = compareTableLayout(target, c, tables.Tables, "aligned")
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
						in, e := engine.NewInput("literal.md", []byte(source), md060Settings(), engine.DocumentContext{Flavor: "gfm"})
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

func TestMD060ComparisonAndOuterFinalStickyDiscard(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		for _, outer := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/outer-%v/origin-%v", kind, outer, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
						gate := ".compareTableLayout.func1"
						if outer {
							gate = ".tableColumnLayout.func1"
						}
						origin := &md014FinalQuota{Context: context.Background(), budget: budget, kind: kind, gate: gate}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD060", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							tables, e := c.TableLayout(ctx)
							pre := compareTableLayout(ctx, c, tables.Tables, "aligned")
							if e != nil || pre.Err != nil || len(pre.Findings) != 6 || pre.Findings[0].Span.Start() != 1 {
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
								out = tableColumnLayout(target, c)
							} else {
								out = compareTableLayout(target, c, tables.Tables, "aligned")
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
						in, e := engine.NewInput("literal.md", []byte(md060Example), md060Settings(), engine.DocumentContext{Flavor: "gfm"})
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

func TestMD060ExhaustedBudgetIsSticky(t *testing.T) {
	for _, kind := range []string{"work", "nodes"} {
		t.Run(kind, func(t *testing.T) {
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			called := false
			r := registry(t, []engine.Registration{{ID: "MD060", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				called = true
				if pre := tableColumnLayout(ctx, c); pre.Err != nil || len(pre.Findings) != 6 {
					t.Fatal("positive precondition", pre)
				}
				if kind == "work" {
					_ = budget.ChargeWork(budget.Limits().WorkUnits)
				} else {
					_ = budget.ChargeNodes(budget.Limits().TotalNodes)
				}
				var out engine.RuleOutcome
				for range 2 {
					out = tableColumnLayout(ctx, c)
					var stop *parser.LimitExceeded
					if !errors.As(out.Err, &stop) || out.Findings != nil || out.IncompleteReason != "" {
						t.Fatal(out)
					}
				}
				return out
			}}})
			analyzeHeadings(t, r, md060Example, md060Settings(), engine.DocumentContext{Flavor: "gfm"}, engine.RunOptions{Budget: budget})
			if !called {
				t.Fatal("actual callback not reached")
			}
		})
	}
}
