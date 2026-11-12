package md088_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules/md088"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

// Source-derived phase totals, including both publication boundaries. The
// whole check adds the fix eligibility pass (ework, enodes) when there are
// findings: the correction fact, its reparses and the interior edit queries.
// The reparses check the caller and originating contexts a different number of
// times, so the caller sees efinal checks and the originating context eorigin.
var md088PhaseCases = []struct {
	name, source    string
	want            int
	pwork, pnodes   int64
	pfinal          int
	cwork, cnodes   int64
	cfinal          int
	ework, enodes   int64
	efinal, eorigin int
}{
	{"empty", "", 0, 33, 2, 12, 10, 0, 3, 0, 0, 0, 0}, {"clean", "clean", 0, 715, 4, 50, 10, 0, 3, 0, 0, 0, 0},
	{"protected", "```\nx\n```", 0, 62, 3, 23, 10, 0, 3, 0, 0, 0, 0}, {"code-only", "`ab`", 0, 91, 4, 34, 10, 0, 3, 0, 0, 0, 0},
	{"one-offender", "—", 1, 330, 6, 47, 110, 2, 8, 371, 8, 59, 59}, {"six-class", "‘’“”–—", 6, 860, 25, 80, 615, 19, 31, 1550, 77, 187, 146},
	{"nested", "***—***", 1, 388, 8, 69, 110, 2, 8, 1387, 92, 201, 188}, {"raw", "<script>\n—\n</script>", 0, 74, 3, 23, 10, 0, 3, 0, 0, 0, 0},
	{"mixed-transformed", "a&amp;—", 1, 560, 6, 54, 110, 2, 8, 1125, 49, 116, 105}, {"growth", "—\n\n—", 2, 630, 13, 83, 211, 5, 13, 696, 16, 100, 100},
}

func TestMD088ComparisonAndWholeStaticBoundaries(t *testing.T) {
	for _, tc := range md088PhaseCases {
		for _, whole := range []bool{false, true} {
			for _, kind := range []string{"work", "nodes"} {
				exact := tc.cwork
				if kind == "nodes" {
					exact = tc.cnodes
				}
				if whole {
					if kind == "work" {
						exact += tc.pwork + 39 + tc.ework
					} else {
						exact += tc.pnodes + tc.enodes
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
							r := ruletest.Registry(t, []engine.Registration{{ID: "MD088", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
								var items []parser.ASCIIPunctuationFact
								if !whole {
									var e error
									items, e = c.ASCIIPunctuation(ctx)
									if e != nil {
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
										return md088.Registration().Check(ctx, c)
									}
									return md088.CompareASCIIPunctuation(ctx, c, items, "ascii")
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
							ruletest.AnalyzeDocument(t, r, tc.source, md088Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: b})
						}
						run(true)
						run(false)
					})
				}
			}
		}
	}
}

func TestMD088ComparisonAndWholeFinalCallerAndOrigin(t *testing.T) {
	for _, tc := range md088PhaseCases {
		for _, whole := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%s/whole-%t/origin-%t/%s", tc.name, whole, originating, reason), func(t *testing.T) {
						origin := &ruletest.LateStopContext{Context: context.Background(), Stop: 1 << 30, Reason: reason}
						r := ruletest.Registry(t, []engine.Registration{{ID: "MD088", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							var items []parser.ASCIIPunctuationFact
							if !whole {
								var e error
								items, e = c.ASCIIPunctuation(ctx)
								if e != nil {
									t.Fatal(items, e)
								}
							}
							final := tc.cfinal
							if whole {
								final += tc.pfinal + 9 + tc.efinal
								if originating {
									final += tc.eorigin - tc.efinal
								}
							}
							stop := &ruletest.LateStopContext{Context: context.Background(), Stop: final, Reason: reason}
							var caller context.Context = stop
							// Preserve the same originating context and its monotonically increasing
							// count. Only the already-completed prefix is qualified at this boundary.
							if originating {
								origin.Stop = origin.Calls + final
								caller = context.Background()
							}
							var out engine.RuleOutcome
							if whole {
								out = md088.Registration().Check(caller, c)
							} else {
								out = md088.CompareASCIIPunctuation(caller, c, items, "ascii")
							}
							if !errors.Is(out.Err, reason) || out.Findings != nil {
								t.Fatal(out)
							}
							if !originating && stop.Calls != final {
								t.Fatal(stop.Calls, final)
							}
							return out
						}}})
						in, e := engine.NewInput("spacing.md", []byte(tc.source), md088Settings(), engine.DocumentContext{})
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

func TestMD088CallbackStickyControls(t *testing.T) {
	for _, tc := range md088PhaseCases {
		for _, whole := range []bool{false, true} {
			for _, kind := range []string{"work", "nodes", "elapsed"} {
				t.Run(fmt.Sprintf("%s/whole-%t/%s", tc.name, whole, kind), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						limits := parser.DefaultLimits()
						limits.OperationTime = 20 * time.Millisecond
						b, _ := parser.NewOperationBudget(limits)
						r := ruletest.Registry(t, []engine.Registration{{ID: "MD088", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							var items []parser.ASCIIPunctuationFact
							if !whole {
								var e error
								items, e = c.ASCIIPunctuation(ctx)
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
									out = md088.Registration().Check(ctx, c)
								} else {
									out = md088.CompareASCIIPunctuation(ctx, c, items, "ascii")
								}
								var e *parser.LimitExceeded
								if !errors.As(out.Err, &e) || out.Findings != nil {
									t.Fatal(out)
								}
							}
							return out
						}}})
						out := ruletest.AnalyzeDocument(t, r, tc.source, md088Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: b})
						if out.Complete || len(out.Diagnostics) != 0 {
							t.Fatal(out)
						}
					})
				})
			}
		}
	}
}

func TestMD088ComparisonRejectsMalformedLateFactsAtomically(t *testing.T) {
	for _, kind := range []string{"absent", "foreign", "unsorted", "width", "scalar", "utf8-interior", "ascii-source", "invalid-style"} {
		t.Run(kind, func(t *testing.T) {
			r := ruletest.Registry(t, []engine.Registration{{ID: "MD088", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				facts, e := c.ASCIIPunctuation(ctx)
				if e != nil || len(facts) != 2 {
					t.Fatal(facts, e)
				}
				first := md088.CompareASCIIPunctuation(ctx, c, facts[:1], "ascii")
				if first.Err != nil || len(first.Findings) != 1 || first.Findings[0].Span.Start() != 0 || first.Findings[0].Span.End() != 3 {
					t.Fatal("earlier actual finding", first)
				}
				style := "ascii"
				switch kind {
				case "absent":
					facts[1].Span = parser.Span{}
				case "foreign":
					facts[1].Span, _ = parser.NewSource([]byte("—")).Span(0, 3)
				case "unsorted":
					facts[1] = facts[0]
				case "width":
					facts[1].Span, _ = c.Source().Span(5, 7)
				case "scalar":
					facts[1].Scalar = '–'
				case "utf8-interior":
					facts[1].Span, _ = c.Source().Span(6, 9)
				case "ascii-source":
					facts[1].Span, _ = c.Source().Span(8, 11)
				case "invalid-style":
					style = "ASCII"
				}
				if (kind == "utf8-interior" || kind == "ascii-source") && facts[1].Span.Start() < facts[0].Span.End() {
					t.Fatal("mutation preempts intended source validation")
				}
				out := md088.CompareASCIIPunctuation(ctx, c, facts, style)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				return out
			}}})
			out := ruletest.AnalyzeDocument(t, r, "—\n\n—abc", md088Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if out.Complete || len(out.Diagnostics) != 0 {
				t.Fatal(out)
			}
		})
	}
}
