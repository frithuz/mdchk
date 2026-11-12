package md049_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules/md049"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

// Source-derived phase costs in Working/issue-75-static-ledger-v1.md. A
// mismatch also pays for the reparse that decides its fix flag.
var md049PhaseCases = []struct {
	name, source, style string
	work, nodes         int64
	final, want         int
	// origin is the final call count on the originating context when it
	// differs from final: the fix flag reparse polls the two differently.
	origin int
}{
	{"empty", "", "consistent", 103, 0, 4, 0, 0},
	{"clean", "---", "consistent", 103, 0, 4, 0, 0},
	{"protected", "```\nx\n```", "consistent", 103, 0, 4, 0, 0},
	{"matching", "*x*", "consistent", 172, 0, 6, 0, 0},
	{"mismatch", "*x*", "underscore", 858, 74, 117, 1, 99},
	{"mixed", "*a* _b_", "consistent", 1354, 121, 171, 1, 150},
	{"nested", "*outer _inner_*", "consistent", 1559, 109, 163, 1, 143},
	{"growth", "_a_ _b_ _c_", "asterisk", 1900, 181, 259, 3, 211},
}

func TestMD049CallbackFinalCallerAndOrigin(t *testing.T) {
	for _, tc := range md049PhaseCases {
		for _, originating := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(fmt.Sprintf("%s/origin-%t/%s", tc.name, originating, reason), func(t *testing.T) {
					origin := &ruletest.LateStopContext{Context: context.Background(), Stop: 1 << 20, Reason: reason}
					var observed engine.RuleOutcome
					r := ruletest.Registry(t, []engine.Registration{{ID: "MD049", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						items, err := c.DelimiterStyle(ctx)
						if err != nil {
							t.Fatal(err)
						}
						final := tc.final
						stop := &ruletest.LateStopContext{Context: context.Background(), Stop: final, Reason: reason}
						caller := context.Context(stop)
						if originating {
							if tc.origin != 0 {
								final = tc.origin
							}
							origin.Calls = 0
							origin.Stop = final
							caller = context.Background()
							stop = origin
						}
						observed = md049.CompareItalicDelimiterStyle(caller, c, tc.style, items.Valid)
						if !errors.Is(observed.Err, reason) || observed.Findings != nil || stop.Calls != final {
							t.Fatal(observed, stop.Calls, final)
						}
						return observed
					}}})
					in, err := engine.NewInput("doc.md", []byte(tc.source), md049Settings(tc.style), engine.DocumentContext{})
					if err != nil {
						t.Fatal(err)
					}
					out, err := r.Lint(origin, []engine.Input{in}, engine.RunOptions{})
					if err != nil || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
						t.Fatal(out, err)
					}
				})
			}
		}
	}
}

func TestMD049CallbackStaticReservationBoundaries(t *testing.T) {
	for _, tc := range md049PhaseCases {
		for _, kind := range []string{"work", "nodes"} {
			exact := tc.work
			if kind == "nodes" {
				exact = tc.nodes
			}
			for _, delta := range []int64{-1, 0} {
				if exact == 0 && delta < 0 {
					continue
				}
				t.Run(fmt.Sprintf("%s/%s/%d", tc.name, kind, delta), func(t *testing.T) {
					var consumed int64
					var observed engine.RuleOutcome
					run := func(setup bool) {
						budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
						r := ruletest.Registry(t, []engine.Registration{{ID: "MD049", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							inventory, err := c.DelimiterStyle(ctx)
							if err != nil {
								t.Fatal(err)
							}
							charge, limit := budget.ChargeWork, budget.Limits().WorkUnits
							if kind == "nodes" {
								charge, limit = budget.ChargeNodes, budget.Limits().TotalNodes
							}
							if setup {
								var exhausted *parser.LimitExceeded
								if !errors.As(charge(limit), &exhausted) {
									t.Fatal("setup must exhaust")
								}
								consumed = exhausted.Observed - limit
								return engine.RuleOutcome{Err: exhausted}
							}
							if err := charge(limit - consumed - exact - delta); err != nil {
								t.Fatal(err)
							}
							observed = md049.CompareItalicDelimiterStyle(ctx, c, tc.style, inventory.Valid)
							if delta < 0 {
								var exhausted *parser.LimitExceeded
								if !errors.As(observed.Err, &exhausted) || observed.Findings != nil {
									t.Fatal(observed)
								}
								again := md049.CompareItalicDelimiterStyle(ctx, c, tc.style, inventory.Valid)
								if !errors.As(again.Err, &exhausted) || again.Findings != nil {
									t.Fatal("sticky", again)
								}
							} else if observed.Err != nil || len(observed.Findings) != tc.want {
								t.Fatal(observed)
							}
							// Later engine reporting may exhaust the deliberately exact allowance; the
							// phase outcome above is the assertion, not a claim of a complete lint result.
							return observed
						}}})
						ruletest.AnalyzeDocument(t, r, tc.source, md049Settings(tc.style), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
					}
					run(true)
					run(false)
				})
			}
		}
	}
}

func TestMD049CallbackStickyControlsAndAtomicOutcome(t *testing.T) {
	for _, tc := range md049PhaseCases {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					limits := parser.DefaultLimits()
					limits.OperationTime = 20 * time.Millisecond
					budget, _ := parser.NewOperationBudget(limits)
					r := ruletest.Registry(t, []engine.Registration{{ID: "MD049", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						inventory, err := c.DelimiterStyle(ctx)
						if err != nil {
							t.Fatal(err)
						}
						switch kind {
						case "work":
							_ = budget.ChargeWork(limits.WorkUnits)
						case "nodes":
							_ = budget.ChargeNodes(limits.TotalNodes)
						case "elapsed":
							time.Sleep(25 * time.Millisecond)
						}
						var out engine.RuleOutcome
						for range 2 {
							out = md049.CompareItalicDelimiterStyle(ctx, c, tc.style, inventory.Valid)
							var exhausted *parser.LimitExceeded
							if !errors.As(out.Err, &exhausted) || out.Findings != nil {
								t.Fatal(out)
							}
						}
						return out
					}}})
					out := ruletest.AnalyzeDocument(t, r, tc.source, md049Settings(tc.style), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
					if out.Complete || len(out.Diagnostics) != 0 {
						t.Fatal(out)
					}
				})
			})
		}
	}
}

func TestMD049WholeCallbackFinalWithdrawal(t *testing.T) {
	for _, tc := range []struct {
		source, style string
		final         int
	}{{"", "consistent", 27}, {"*x*", "consistent", 70}, {"*x*", "underscore", 181}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(fmt.Sprintf("%s/%s/%s", tc.source, tc.style, reason), func(t *testing.T) {
				r := ruletest.Registry(t, []engine.Registration{{ID: "MD049", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					stop := &ruletest.LateStopContext{Context: context.Background(), Stop: tc.final, Reason: reason}
					out := md049.Registration().Check(stop, c)
					if !errors.Is(out.Err, reason) || out.Findings != nil || stop.Calls != tc.final {
						t.Fatal(out, stop.Calls, tc.final)
					}
					return out
				}}})
				out := ruletest.AnalyzeDocument(t, r, tc.source, md049Settings(tc.style), engine.DocumentContext{}, engine.RunOptions{})
				if out.Complete || len(out.Diagnostics) != 0 {
					t.Fatal(out)
				}
			})
		}
	}
}

func TestMD049WholeCallbackStaticBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, source, style string
		work, nodes         int64
		want                int
	}{{"empty", "", "consistent", 224, 4, 0}, {"matching", "*x*", "consistent", 941, 20, 0}, {"mismatch", "*x*", "underscore", 1627, 94, 1}} {
		for _, kind := range []string{"work", "nodes"} {
			exact := tc.work
			if kind == "nodes" {
				exact = tc.nodes
			}
			for _, delta := range []int64{-1, 0} {
				t.Run(fmt.Sprintf("%s/%s/%d", tc.name, kind, delta), func(t *testing.T) {
					var consumed int64
					run := func(prefix bool) {
						budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
						r := ruletest.Registry(t, []engine.Registration{{ID: "MD049", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							// Sacrificial operation ends BEFORE getter, facade and callback. This
							// measures only parse/engine prefix, never the tested phase's expected cost.
							charge, limit := budget.ChargeWork, budget.Limits().WorkUnits
							if kind == "nodes" {
								charge, limit = budget.ChargeNodes, budget.Limits().TotalNodes
							}
							if prefix {
								var exhausted *parser.LimitExceeded
								if !errors.As(charge(limit), &exhausted) {
									t.Fatal("prefix must exhaust")
								}
								consumed = exhausted.Observed - limit
								return engine.RuleOutcome{Err: exhausted}
							}
							if err := charge(limit - consumed - exact - delta); err != nil {
								t.Fatal(err)
							}
							out := md049.Registration().Check(ctx, c)
							if delta < 0 {
								var exhausted *parser.LimitExceeded
								if !errors.As(out.Err, &exhausted) || out.Findings != nil {
									t.Fatal(out)
								}
							} else if out.Err != nil || len(out.Findings) != tc.want {
								t.Fatal(out)
							}
							return out
						}}})
						ruletest.AnalyzeDocument(t, r, tc.source, md049Settings(tc.style), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
					}
					run(true)
					run(false)
				})
			}
		}
	}
}
