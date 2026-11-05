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

const md075ControlSource = "é | 2\r\n3 | 4\r\n\r\n| 5 |\r\n| 6 |\r\n"

func TestMD075ComparisonRejectsLateInvalidRows(t *testing.T) {
	for _, fault := range []string{"reversed", "empty", "out-of-range", "utf8", "crlf", "cells", "line", "paragraph-order", "paragraph-negative", "next-to-table"} {
		t.Run(fault, func(t *testing.T) {
			called := false
			r := registry(t, []engine.Registration{{ID: "MD075", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				rows, err := c.PipeRowCandidates(ctx)
				if err != nil || len(rows.Rows) != 4 {
					t.Fatal(rows, err)
				}
				pre := comparePipeRowRuns(ctx, c, rows.Rows)
				if pre.Err != nil || len(pre.Findings) != 2 || pre.Findings[0].Span.Start() != 0 || pre.Findings[0].Span.End() != 13 || pre.Findings[1].Span.Start() != 17 || pre.Findings[1].Span.End() != 29 || pre.Findings[1].Message != "Pipe rows are not part of a table." {
					t.Fatal("positive", pre)
				}
				switch fault {
				case "reversed":
					rows.Rows[1].Start = 3
				case "empty":
					rows.Rows[0].End = rows.Rows[0].Start
				case "out-of-range":
					rows.Rows[3].End = c.Source().Len() + 1
				case "utf8":
					rows.Rows[0].Start = 1
				case "crlf":
					rows.Rows[0].End = 7
				case "cells":
					rows.Rows[0].Cells = 0
				case "line":
					rows.Rows[1].Line = rows.Rows[0].Line
				case "paragraph-order":
					rows.Rows[3].Paragraph = 0
				case "paragraph-negative":
					rows.Rows[0].Paragraph = -1
				case "next-to-table":
					rows.Rows[1].NextToTable = true
				}
				out := comparePipeRowRuns(ctx, c, rows.Rows)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				called = true
				return out
			}}})
			analyzeHeadings(t, r, md075ControlSource, md075Settings(), engine.DocumentContext{Flavor: "gfm"}, engine.RunOptions{})
			if !called {
				t.Fatal("callback missing")
			}
		})
	}
}

func TestMD075ComparisonAndDistinctOuterFinalControls(t *testing.T) {
	for _, source := range []string{"", "clean", "```\nA | B\n1 | 2\n```\n", md075Example, md075ControlSource} {
		for _, whole := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%q/whole-%v/origin-%v/%v", source, whole, originating, reason), func(t *testing.T) {
						origin := &md014PublicationStop{Context: context.Background(), reason: reason}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD075", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							rows, e := c.PipeRowCandidates(ctx)
							if e != nil {
								t.Fatal(e)
							}
							if source == md075Example {
								pre := comparePipeRowRuns(ctx, c, rows.Rows)
								if pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 0 || pre.Findings[0].Span.End() != 11 {
									t.Fatal("actual positive precondition", pre)
								}
							}
							gate := ".comparePipeRowRuns.func1"
							if whole {
								gate = ".headerlessPipeRows.func1"
							}
							caller := &md014PublicationStop{Context: context.Background(), gate: gate, reason: reason}
							var target context.Context = caller
							if originating {
								origin.gate = gate
								target = context.Background()
							}
							var out engine.RuleOutcome
							if whole {
								out = headerlessPipeRows(target, c)
							} else {
								out = comparePipeRowRuns(target, c, rows.Rows)
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
						in, e := engine.NewInput("literal.md", []byte(source), md075Settings(), engine.DocumentContext{Flavor: "gfm"})
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

func TestMD075ComparisonAndOuterFinalStickyDiscard(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		for _, outer := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/outer-%v/origin-%v", kind, outer, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
						gate := ".comparePipeRowRuns.func1"
						if outer {
							gate = ".headerlessPipeRows.func1"
						}
						origin := &md014FinalQuota{Context: context.Background(), budget: budget, kind: kind, gate: gate}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD075", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							rows, e := c.PipeRowCandidates(ctx)
							pre := comparePipeRowRuns(ctx, c, rows.Rows)
							if e != nil || pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 0 {
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
								out = headerlessPipeRows(target, c)
							} else {
								out = comparePipeRowRuns(target, c, rows.Rows)
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
						in, e := engine.NewInput("literal.md", []byte(md075Example), md075Settings(), engine.DocumentContext{Flavor: "gfm"})
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

func TestMD075ExhaustedBudgetIsSticky(t *testing.T) {
	for _, kind := range []string{"work", "nodes"} {
		t.Run(kind, func(t *testing.T) {
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			called := false
			r := registry(t, []engine.Registration{{ID: "MD075", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				called = true
				if pre := headerlessPipeRows(ctx, c); pre.Err != nil || len(pre.Findings) != 1 {
					t.Fatal("positive precondition", pre)
				}
				if kind == "work" {
					_ = budget.ChargeWork(budget.Limits().WorkUnits)
				} else {
					_ = budget.ChargeNodes(budget.Limits().TotalNodes)
				}
				var out engine.RuleOutcome
				for range 2 {
					out = headerlessPipeRows(ctx, c)
					var stop *parser.LimitExceeded
					if !errors.As(out.Err, &stop) || out.Findings != nil || out.IncompleteReason != "" {
						t.Fatal(out)
					}
				}
				return out
			}}})
			analyzeHeadings(t, r, md075Example, md075Settings(), engine.DocumentContext{Flavor: "gfm"}, engine.RunOptions{Budget: budget})
			if !called {
				t.Fatal("actual callback not reached")
			}
		})
	}
}
