package rules

import (
	"context"
	"errors"
	"fmt"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
	"testing/synctest"
	"time"
)

// Production totals are source-derived before execution; comparison totals
// include the terminal probe, five predicates, two boundary bytes, two source
// associations, one emission and each actual growth/copy.
var md065PhaseCases = []struct {
	name, source  string
	want          int
	pwork, pnodes int64
	pfinal        int
	cwork, cnodes int64
	cfinal        int
}{
	{"empty", "", 0, 16, 4, 15, 1, 0, 2},
	{"clean", "clean", 0, 53, 15, 40, 1, 0, 2},
	{"edge", "***", 0, 39, 11, 32, 1, 0, 2},
	{"protected", "```\nx\n```", 0, 45, 11, 34, 1, 0, 2},
	{"violation", "Text\n***\nMore", 2, 128, 38, 90, 26, 5, 10},
	{"definitions", "[r]: /x\n***", 1, 61, 14, 43, 13, 2, 6},
	{"rollback", "> Text\n> ***\n> More", 2, 211, 53, 119, 26, 5, 10},
	{"growth", "***\n***\n***\n***", 6, 159, 62, 103, 78, 19, 24},
}

func TestMD065ComparisonAndWholeStaticBoundaries(t *testing.T) {
	for _, tc := range md065PhaseCases {
		for _, whole := range []bool{false, true} {
			for _, kind := range []string{"work", "nodes"} {
				exact := tc.cwork
				if kind == "nodes" {
					exact = tc.cnodes
				}
				if whole {
					if kind == "work" {
						exact += tc.pwork + 2
					} else {
						exact += tc.pnodes
					}
				}
				for _, delta := range []int64{-1, 0} {
					if exact == 0 && delta < 0 {
						continue
					}
					t.Run(fmt.Sprintf("%s/whole-%t/%s/%d", tc.name, whole, kind, delta), func(t *testing.T) {
						var used int64
						run := func(prefix bool) {
							b, _ := parser.NewOperationBudget(parser.DefaultLimits())
							r := registry(t, []engine.Registration{{ID: "MD065", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
								var items []parser.ThematicSpacingFact
								if !whole {
									var e error
									items, e = c.ThematicSpacing(ctx)
									if e != nil || len(items) != tc.want {
										t.Fatal(items, e)
									}
								}
								charge, limit := b.ChargeWork, b.Limits().WorkUnits
								if kind == "nodes" {
									charge, limit = b.ChargeNodes, b.Limits().TotalNodes
								}
								if prefix {
									var e *parser.LimitExceeded
									if !errors.As(charge(limit), &e) {
										t.Fatal("prefix must exhaust")
									}
									used = e.Observed - limit
									return engine.RuleOutcome{Err: e}
								}
								if e := charge(limit - used - exact - delta); e != nil {
									t.Fatal(e)
								}
								call := func() engine.RuleOutcome {
									if whole {
										return horizontalRuleBlankLines(ctx, c)
									}
									return compareHorizontalRuleBlankLines(ctx, c, items)
								}
								out := call()
								if delta == 0 {
									if out.Err != nil || len(out.Findings) != tc.want {
										t.Fatal(out)
									}
								} else {
									for range 2 {
										var e *parser.LimitExceeded
										if !errors.As(out.Err, &e) || out.Findings != nil {
											t.Fatal(out)
										}
										out = call()
									}
								}
								return out
							}}})
							// The tested direct outcome precedes later reporting, which may consume
							// the deliberately exact remaining allowance.
							analyzeHeadings(t, r, tc.source, md065Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: b})
						}
						run(true)
						run(false)
					})
				}
			}
		}
	}
}

func TestMD065ComparisonAndWholeFinalCallerAndOrigin(t *testing.T) {
	for _, tc := range md065PhaseCases {
		for _, whole := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%s/whole-%t/origin-%t/%s", tc.name, whole, originating, reason), func(t *testing.T) {
						origin := &md018LateContext{Context: context.Background(), stop: 1 << 30, reason: reason}
						r := registry(t, []engine.Registration{{ID: "MD065", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							var items []parser.ThematicSpacingFact
							if !whole {
								var e error
								items, e = c.ThematicSpacing(ctx)
								if e != nil || len(items) != tc.want {
									t.Fatal(items, e)
								}
							}
							final := tc.cfinal
							if whole {
								final += tc.pfinal + 4
							}
							stop := &md018LateContext{Context: context.Background(), stop: final, reason: reason}
							var caller context.Context = stop
							// Preserve the same originating context and its monotonically increasing
							// count. Only the already-completed prefix is qualified at this boundary.
							if originating {
								origin.stop = origin.calls + final
								caller = context.Background()
							}
							var out engine.RuleOutcome
							if whole {
								out = horizontalRuleBlankLines(caller, c)
							} else {
								out = compareHorizontalRuleBlankLines(caller, c, items)
							}
							if !errors.Is(out.Err, reason) || out.Findings != nil {
								t.Fatal(out)
							}
							if !originating && stop.calls != final {
								t.Fatal(stop.calls, final)
							}
							return out
						}}})
						in, e := engine.NewInput("spacing.md", []byte(tc.source), md065Settings(), engine.DocumentContext{})
						if e != nil {
							t.Fatal(e)
						}
						out, e := r.Lint(origin, []engine.Input{in}, engine.RunOptions{})
						if e != nil || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
							t.Fatal(out, e)
						}
					})
				}
			}
		}
	}
}

func TestMD065CallbackStickyControls(t *testing.T) {
	for _, tc := range md065PhaseCases {
		for _, whole := range []bool{false, true} {
			for _, kind := range []string{"work", "nodes", "elapsed"} {
				t.Run(fmt.Sprintf("%s/whole-%t/%s", tc.name, whole, kind), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						limits := parser.DefaultLimits()
						limits.OperationTime = 20 * time.Millisecond
						b, _ := parser.NewOperationBudget(limits)
						r := registry(t, []engine.Registration{{ID: "MD065", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							var items []parser.ThematicSpacingFact
							if !whole {
								var e error
								items, e = c.ThematicSpacing(ctx)
								if e != nil {
									t.Fatal(e)
								}
							}
							switch kind {
							case "work":
								_ = b.ChargeWork(limits.WorkUnits)
							case "nodes":
								_ = b.ChargeNodes(limits.TotalNodes)
							case "elapsed":
								time.Sleep(25 * time.Millisecond)
							}
							var out engine.RuleOutcome
							for range 2 {
								if whole {
									out = horizontalRuleBlankLines(ctx, c)
								} else {
									out = compareHorizontalRuleBlankLines(ctx, c, items)
								}
								var e *parser.LimitExceeded
								if !errors.As(out.Err, &e) || out.Findings != nil {
									t.Fatal(out)
								}
							}
							return out
						}}})
						out := analyzeHeadings(t, r, tc.source, md065Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: b})
						if out.Complete || len(out.Diagnostics) != 0 {
							t.Fatal(out)
						}
					})
				})
			}
		}
	}
}

func TestMD065ComparisonRejectsMalformedPointsAtomically(t *testing.T) {
	for _, kind := range []string{"absent", "foreign", "nonzero", "utf8-interior", "crlf-interior"} {
		t.Run(kind, func(t *testing.T) {
			r := registry(t, []engine.Registration{{ID: "MD065", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				facts, e := c.ThematicSpacing(ctx)
				if e != nil || len(facts) != 2 {
					t.Fatal(facts, e)
				}
				switch kind {
				case "absent":
					facts[1].Point = parser.Span{}
				case "foreign":
					facts[1].Point, _ = parser.NewSource([]byte("雪\r\n***\r\nMore")).Span(8, 8)
				case "nonzero":
					facts[1].Point, _ = c.Source().Span(5, 8)
				case "utf8-interior":
					facts[1].Point, _ = c.Source().Span(1, 1)
				case "crlf-interior":
					facts[1].Point, _ = c.Source().Span(4, 4)
				}
				out := compareHorizontalRuleBlankLines(ctx, c, facts)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				return out
			}}})
			out := analyzeHeadings(t, r, "雪\r\n***\r\nMore", md065Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if out.Complete || len(out.Diagnostics) != 0 {
				t.Fatal(out)
			}
		})
	}
}
