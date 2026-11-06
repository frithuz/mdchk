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

const md056ControlSource = "| A | B |\r\n| --- | --- |\r\né | 2 | 3\r\n4\r\n"

func TestMD056ComparisonRejectsLateInvalidRows(t *testing.T) {
	for _, fault := range []string{"reversed", "empty", "out-of-range", "utf8", "crlf", "cells", "width"} {
		t.Run(fault, func(t *testing.T) {
			called := false
			r := registry(t, []engine.Registration{{ID: "MD056", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				rows, err := c.TableCellCounts(ctx)
				if err != nil || len(rows.Rows) != 2 {
					t.Fatal(rows, err)
				}
				pre := compareTableCellCounts(ctx, c, rows.Rows)
				if pre.Err != nil || len(pre.Findings) != 2 || pre.Findings[0].Span.Start() != 26 || pre.Findings[0].Span.End() != 36 || pre.Findings[1].Span.Start() != 38 || pre.Findings[1].Span.End() != 39 || pre.Findings[1].Message != "Row has 1 cell; table has 2 columns." {
					t.Fatal("positive", pre)
				}
				switch fault {
				case "reversed":
					rows.Rows[1].Start = 30
				case "empty":
					rows.Rows[0].End = rows.Rows[0].Start
				case "out-of-range":
					rows.Rows[1].End = c.Source().Len() + 1
				case "utf8":
					rows.Rows[0].Start = 27
				case "crlf":
					rows.Rows[0].End = 37
				case "cells":
					rows.Rows[0].RawCells = 0
				case "width":
					rows.Rows[1].Width = 0
				}
				out := compareTableCellCounts(ctx, c, rows.Rows)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				called = true
				return out
			}}})
			analyzeHeadings(t, r, md056ControlSource, onlyMD056Settings(), engine.DocumentContext{Flavor: "gfm"}, engine.RunOptions{})
			if !called {
				t.Fatal("callback missing")
			}
		})
	}
}

func TestMD056ComparisonAndDistinctOuterFinalControls(t *testing.T) {
	for _, source := range []string{"", "clean", "```\n| A | B |\n--- | ---\n```\n", md056Example, md056ControlSource} {
		for _, whole := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%q/whole-%v/origin-%v/%v", source, whole, originating, reason), func(t *testing.T) {
						origin := &gateStopContext{Context: context.Background(), reason: reason}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD056", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							rows, e := c.TableCellCounts(ctx)
							if e != nil {
								t.Fatal(e)
							}
							if source == md056Example {
								pre := compareTableCellCounts(ctx, c, rows.Rows)
								if pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 16 || pre.Findings[0].Span.End() != 25 {
									t.Fatal("actual positive precondition", pre)
								}
							}
							gate := ".compareTableCellCounts.func1"
							if whole {
								gate = ".mismatchedTableCells.func1"
							}
							caller := &gateStopContext{Context: context.Background(), gate: gate, reason: reason}
							var target context.Context = caller
							if originating {
								origin.gate = gate
								target = context.Background()
							}
							var out engine.RuleOutcome
							if whole {
								out = mismatchedTableCells(target, c)
							} else {
								out = compareTableCellCounts(target, c, rows.Rows)
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
						in, e := engine.NewInput("literal.md", []byte(source), onlyMD056Settings(), engine.DocumentContext{Flavor: "gfm"})
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

func TestMD056ComparisonAndOuterFinalStickyDiscard(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		for _, outer := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/outer-%v/origin-%v", kind, outer, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
						gate := ".compareTableCellCounts.func1"
						if outer {
							gate = ".mismatchedTableCells.func1"
						}
						origin := &gateQuotaContext{Context: context.Background(), budget: budget, kind: kind, gate: gate}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD056", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							rows, e := c.TableCellCounts(ctx)
							pre := compareTableCellCounts(ctx, c, rows.Rows)
							if e != nil || pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 16 {
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
								out = mismatchedTableCells(target, c)
							} else {
								out = compareTableCellCounts(target, c, rows.Rows)
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
						in, e := engine.NewInput("literal.md", []byte(md056Example), onlyMD056Settings(), engine.DocumentContext{Flavor: "gfm"})
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

func TestMD056ExhaustedBudgetIsSticky(t *testing.T) {
	for _, kind := range []string{"work", "nodes"} {
		t.Run(kind, func(t *testing.T) {
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			called := false
			r := registry(t, []engine.Registration{{ID: "MD056", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				called = true
				if pre := mismatchedTableCells(ctx, c); pre.Err != nil || len(pre.Findings) != 1 {
					t.Fatal("positive precondition", pre)
				}
				if kind == "work" {
					_ = budget.ChargeWork(budget.Limits().WorkUnits)
				} else {
					_ = budget.ChargeNodes(budget.Limits().TotalNodes)
				}
				var out engine.RuleOutcome
				for range 2 {
					out = mismatchedTableCells(ctx, c)
					var stop *parser.LimitExceeded
					if !errors.As(out.Err, &stop) || out.Findings != nil || out.IncompleteReason != "" {
						t.Fatal(out)
					}
				}
				return out
			}}})
			analyzeHeadings(t, r, md056Example, onlyMD056Settings(), engine.DocumentContext{Flavor: "gfm"}, engine.RunOptions{Budget: budget})
			if !called {
				t.Fatal("actual callback not reached")
			}
		})
	}
}
