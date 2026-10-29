package rules

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// Source-derived phase costs in Working/issue-76-static-ledger-v1.md.
var md050PhaseCases = []struct {
	name, source, style string
	work, nodes         int64
	final, want         int
}{
	{"empty", "", "consistent", 109, 0, 4, 0},
	{"clean", "---", "consistent", 109, 0, 4, 0},
	{"protected", "```\nx\n```", "consistent", 109, 0, 4, 0},
	{"matching", "**x**", "consistent", 184, 0, 6, 0},
	{"mismatch", "**x**", "underscore", 186, 2, 8, 1},
	{"mixed", "**a** __b__", "consistent", 261, 2, 10, 1},
	{"nested", "**outer __inner__**", "consistent", 261, 2, 10, 1},
	{"growth", "__a__ __b__ __c__", "asterisk", 343, 9, 16, 3},
}

func TestMD050CallbackFinalCallerAndOrigin(t *testing.T) {
	for _, tc := range md050PhaseCases {
		for _, originating := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(fmt.Sprintf("%s/origin-%t/%s", tc.name, originating, reason), func(t *testing.T) {
					origin := &md018LateContext{Context: context.Background(), stop: 1 << 20, reason: reason}
					var observed engine.RuleOutcome
					r := registry(t, []engine.Registration{{ID: "MD050", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						items, err := c.DelimiterStyle(ctx)
						if err != nil {
							t.Fatal(err)
						}
						stop := &md018LateContext{Context: context.Background(), stop: tc.final, reason: reason}
						caller := context.Context(stop)
						if originating {
							origin.calls = 0
							origin.stop = tc.final
							caller = context.Background()
							stop = origin
						}
						observed = compareBoldDelimiterStyle(caller, c, tc.style, items.Valid)
						if !errors.Is(observed.Err, reason) || observed.Findings != nil || stop.calls != tc.final {
							t.Fatal(observed, stop.calls, tc.final)
						}
						return observed
					}}})
					in, err := engine.NewInput("doc.md", []byte(tc.source), md050Settings(tc.style), engine.DocumentContext{})
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

func TestMD050CallbackStaticReservationBoundaries(t *testing.T) {
	for _, tc := range md050PhaseCases {
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
						r := registry(t, []engine.Registration{{ID: "MD050", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
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
							observed = compareBoldDelimiterStyle(ctx, c, tc.style, inventory.Valid)
							if delta < 0 {
								var exhausted *parser.LimitExceeded
								if !errors.As(observed.Err, &exhausted) || observed.Findings != nil {
									t.Fatal(observed)
								}
								again := compareBoldDelimiterStyle(ctx, c, tc.style, inventory.Valid)
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
						analyzeHeadings(t, r, tc.source, md050Settings(tc.style), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
					}
					run(true)
					run(false)
				})
			}
		}
	}
}

func TestMD050CallbackStickyControlsAndAtomicOutcome(t *testing.T) {
	for _, tc := range md050PhaseCases {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					limits := parser.DefaultLimits()
					limits.OperationTime = 20 * time.Millisecond
					budget, _ := parser.NewOperationBudget(limits)
					r := registry(t, []engine.Registration{{ID: "MD050", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
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
							out = compareBoldDelimiterStyle(ctx, c, tc.style, inventory.Valid)
							var exhausted *parser.LimitExceeded
							if !errors.As(out.Err, &exhausted) || out.Findings != nil {
								t.Fatal(out)
							}
						}
						return out
					}}})
					out := analyzeHeadings(t, r, tc.source, md050Settings(tc.style), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
					if out.Complete || len(out.Diagnostics) != 0 {
						t.Fatal(out)
					}
				})
			})
		}
	}
}

func TestMD050WholeCallbackFinalWithdrawal(t *testing.T) {
	for _, tc := range []struct {
		source, style string
		final         int
	}{{"", "consistent", 27}, {"**x**", "consistent", 70}, {"**x**", "underscore", 72}} {
		for _, originating := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(fmt.Sprintf("%s/%s/origin-%t/%s", tc.source, tc.style, originating, reason), func(t *testing.T) {
					origin := &md018LateContext{Context: context.Background(), stop: 1 << 20, reason: reason}
					r := registry(t, []engine.Registration{{ID: "MD050", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						stop := &md018LateContext{Context: context.Background(), stop: tc.final, reason: reason}
						caller := context.Context(stop)
						if originating {
							origin.calls, origin.stop = 0, tc.final
							caller, stop = context.Background(), origin
						}
						out := boldDelimiterStyle(caller, c)
						if !errors.Is(out.Err, reason) || out.Findings != nil || stop.calls != tc.final {
							t.Fatal(out, stop.calls, tc.final)
						}
						return out
					}}})
					in, err := engine.NewInput("doc.md", []byte(tc.source), md050Settings(tc.style), engine.DocumentContext{})
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

func TestMD050WholeCallbackStaticBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, source, style string
		work, nodes         int64
		want                int
	}{{"empty", "", "consistent", 230, 4, 0}, {"matching", "**x**", "consistent", 953, 20, 0}, {"mismatch", "**x**", "underscore", 955, 22, 1}} {
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
						r := registry(t, []engine.Registration{{ID: "MD050", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
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
							out := boldDelimiterStyle(ctx, c)
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
						analyzeHeadings(t, r, tc.source, md050Settings(tc.style), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
					}
					run(true)
					run(false)
				})
			}
		}
	}
}
