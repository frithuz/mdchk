package rules

import (
	"context"
	"errors"
	"fmt"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"math"
	"testing"
	"testing/synctest"
	"time"
)

// Source-derived phase totals, including both publication boundaries.
var md081PhaseCases = []struct {
	name, source  string
	want, minimum int
	pwork, pnodes int64
	pfinal        int
	cwork, cnodes int64
	cfinal        int
}{
	{"empty", "", 0, 1, 47, 3, 17, 10, 0, 3},
	{"clean", "clean", 0, 1, 3249, 15, 68, 47, 0, 8},
	{"protected", "```\nx\n```", 0, 1, 84, 7, 33, 10, 0, 3},
	{"code-only", "`ab`", 0, 1, 141, 15, 54, 47, 0, 8},
	{"violation", "**ab** c", 1, 1, 3348, 27, 101, 50, 2, 11},
	{"minskip", "**ab** c", 0, 4, 3348, 27, 101, 47, 0, 8},
	{"equality", "**ab** cd", 0, 1, 3862, 27, 103, 47, 0, 8},
	{"overlap", "***ab*** c", 1, 1, 3383, 29, 116, 50, 2, 11},
	{"growth", "**a**\n\n**b**", 2, 1, 2410, 34, 132, 91, 5, 19},
}

func md081BoundarySettings(minimum int) engine.Settings {
	s := md081Settings()
	on := true
	s["MD081"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"minimum_characters": minimum}}
	return s
}

func TestMD081ComparisonAndWholeStaticBoundaries(t *testing.T) {
	for _, tc := range md081PhaseCases {
		for _, whole := range []bool{false, true} {
			for _, kind := range []string{"work", "nodes"} {
				exact := tc.cwork
				if kind == "nodes" {
					exact = tc.cnodes
				}
				if whole {
					if kind == "work" {
						exact += tc.pwork + 39
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
							r := registry(t, []engine.Registration{{ID: "MD081", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
								var items []parser.ParagraphEmphasisFact
								if !whole {
									var e error
									items, e = c.ParagraphEmphasis(ctx)
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
										return excessiveParagraphEmphasis(ctx, c)
									}
									return compareParagraphEmphasis(ctx, c, items, .5, tc.minimum)
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
							analyzeHeadings(t, r, tc.source, md081BoundarySettings(tc.minimum), engine.DocumentContext{}, engine.RunOptions{Budget: b})
						}
						run(true)
						run(false)
					})
				}
			}
		}
	}
}

func TestMD081ComparisonAndWholeFinalCallerAndOrigin(t *testing.T) {
	for _, tc := range md081PhaseCases {
		for _, whole := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%s/whole-%t/origin-%t/%s", tc.name, whole, originating, reason), func(t *testing.T) {
						origin := &md018LateContext{Context: context.Background(), stop: 1 << 30, reason: reason}
						r := registry(t, []engine.Registration{{ID: "MD081", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							var items []parser.ParagraphEmphasisFact
							if !whole {
								var e error
								items, e = c.ParagraphEmphasis(ctx)
								if e != nil {
									t.Fatal(items, e)
								}
							}
							final := tc.cfinal
							if whole {
								final += tc.pfinal + 9
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
								out = excessiveParagraphEmphasis(caller, c)
							} else {
								out = compareParagraphEmphasis(caller, c, items, .5, tc.minimum)
							}
							if !errors.Is(out.Err, reason) || out.Findings != nil {
								t.Fatal(out)
							}
							if !originating && stop.calls != final {
								t.Fatal(stop.calls, final)
							}
							return out
						}}})
						in, e := engine.NewInput("spacing.md", []byte(tc.source), md081BoundarySettings(tc.minimum), engine.DocumentContext{})
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

func TestMD081CallbackStickyControls(t *testing.T) {
	for _, tc := range md081PhaseCases {
		for _, whole := range []bool{false, true} {
			for _, kind := range []string{"work", "nodes", "elapsed"} {
				t.Run(fmt.Sprintf("%s/whole-%t/%s", tc.name, whole, kind), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						limits := parser.DefaultLimits()
						limits.OperationTime = 20 * time.Millisecond
						b, _ := parser.NewOperationBudget(limits)
						r := registry(t, []engine.Registration{{ID: "MD081", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							var items []parser.ParagraphEmphasisFact
							if !whole {
								var e error
								items, e = c.ParagraphEmphasis(ctx)
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
									out = excessiveParagraphEmphasis(ctx, c)
								} else {
									out = compareParagraphEmphasis(ctx, c, items, .5, tc.minimum)
								}
								var e *parser.LimitExceeded
								if !errors.As(out.Err, &e) || out.Findings != nil {
									t.Fatal(out)
								}
							}
							return out
						}}})
						out := analyzeHeadings(t, r, tc.source, md081BoundarySettings(tc.minimum), engine.DocumentContext{}, engine.RunOptions{Budget: b})
						if out.Complete || len(out.Diagnostics) != 0 {
							t.Fatal(out)
						}
					})
				})
			}
		}
	}
}

func TestMD081ComparisonRejectsMalformedLateFactsAtomically(t *testing.T) {
	for _, kind := range []string{"absent", "foreign", "unsorted", "negative", "overcount", "utf8-interior", "crlf-interior", "invalid-ratio", "invalid-minimum"} {
		phase := "late-fact/"
		if kind == "invalid-ratio" || kind == "invalid-minimum" {
			phase = "admission/" // These reject before any finding is constructed.
		}
		t.Run(phase+kind, func(t *testing.T) {
			r := registry(t, []engine.Registration{{ID: "MD081", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				facts, e := c.ParagraphEmphasis(ctx)
				if e != nil || len(facts) != 2 {
					t.Fatal(facts, e)
				}
				// Independent literal precondition: the first valid fact MUST report
				// at .5 before a later malformed fact can withdraw its output.
				first := facts[0]
				if first.Total != 3 || first.Emphasized != 2 || !first.Content.Present() || first.Content.Start() != 0 || first.Content.End() != 8 || float64(first.Emphasized)/float64(first.Total) <= .5 {
					t.Fatal("first paragraph does not establish earlier output", first)
				}
				if c.Source().Len() != 24 || facts[1].Content.Start() != 12 || facts[1].Content.End() != 24 {
					t.Fatal("independent second paragraph geometry", facts[1])
				}
				ratio, minimum := .5, 1
				switch kind {
				case "absent":
					facts[1].Content = parser.Span{}
				case "foreign":
					facts[1].Content, _ = parser.NewSource([]byte("foreign")).Span(0, 7)
				case "unsorted":
					facts[1].Content = facts[0].Content
				case "negative":
					facts[1].Total = -1
				case "overcount":
					facts[1].Emphasized = facts[1].Total + 1
				case "utf8-interior":
					facts[1].Content, _ = c.Source().Span(13, 24)
				case "crlf-interior":
					facts[1].Content, _ = c.Source().Span(9, 24)
				case "invalid-ratio":
					ratio = math.NaN()
				case "invalid-minimum":
					minimum = 0
				}
				if (kind == "utf8-interior" || kind == "crlf-interior") && facts[1].Content.Start() < first.Content.End() {
					t.Fatal("endpoint mutation would fail ordering before endpoint checks")
				}
				out := compareParagraphEmphasis(ctx, c, facts, ratio, minimum)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				return out
			}}})
			out := analyzeHeadings(t, r, "**ab** c\r\n\r\n雪 **cd** e", md081BoundarySettings(1), engine.DocumentContext{}, engine.RunOptions{})
			if out.Complete || len(out.Diagnostics) != 0 {
				t.Fatal(out)
			}
		})
	}
}
