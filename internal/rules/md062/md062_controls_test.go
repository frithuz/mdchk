package md062_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules/md062"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

func TestMD062ComparisonRejectsLateInvalidPoints(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, fault := range []string{"absent", "foreign", "zero-width-length", "reversed", "utf8", "crlf"} {
				t.Run(fault, func(t *testing.T) {
					called := false
					r := ruletest.Registry(t, []engine.Registration{{ID: "MD062", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						items, err := c.DestinationWhitespace(ctx)
						if err != nil || len(items.Spans) != 2 {
							t.Fatal(items, err)
						}
						pre := md062.CompareDestinationWhitespace(ctx, c, items.Spans)
						if pre.Err != nil || len(pre.Findings) != 2 || pre.Findings[0].Span.Start() != 0 {
							t.Fatal("positive", pre)
						}
						var bad parser.Span
						switch fault {
						case "foreign":
							s := parser.NewSource(c.Source().Bytes())
							bad, _ = s.Span(26, 46)
						case "zero-width-length":
							bad, _ = c.Source().Span(26, 26)
						case "reversed":
							bad, _ = c.Source().Span(0, 20)
						case "utf8":
							bad, _ = c.Source().Span(23, 24)
						case "crlf":
							bad, _ = c.Source().Span(25, 26)
						}
						items.Spans[1] = bad
						out := md062.CompareDestinationWhitespace(ctx, c, items.Spans)
						if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
							t.Fatal(out)
						}
						called = true
						return out
					}}})
					ruletest.AnalyzeDocument(t, r, md062Example+"\n\né\r\n"+md062Example, md062Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
					if !called {
						t.Fatal("callback missing")
					}
				})
			}

		})
	}
}

func TestMD062ComparisonAndDistinctOuterFinalControls(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, source := range []string{"", "clean", "`code`", "```sh\n$ pwd\nout\n```\n", "[Guide](my guide.md)", "[Guide](my guide.md)\n<div>\n<x a=\"\n\n[Guide](my guide.md)"} {
				for _, whole := range []bool{false, true} {
					for _, originating := range []bool{false, true} {
						for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
							t.Run(fmt.Sprintf("%q/whole-%v/origin-%v/%v", source, whole, originating, reason), func(t *testing.T) {
								origin := &ruletest.GateStopContext{Context: context.Background(), Reason: reason}
								called := false
								r := ruletest.Registry(t, []engine.Registration{{ID: "MD062", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
									items, e := c.DestinationWhitespace(ctx)
									if e != nil {
										t.Fatal(e)
									}
									if source == "[Guide](my guide.md)" {
										pre := md062.CompareDestinationWhitespace(ctx, c, items.Spans)
										if pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 0 || pre.Findings[0].Span.End() != 20 {
											t.Fatal("actual positive precondition", pre)
										}
									}
									gate := ".compareDestinationWhitespace.func1"
									if whole {
										gate = ".destinationWhitespace.func1"
									}
									caller := &ruletest.GateStopContext{Context: context.Background(), Gate: gate, Reason: reason}
									var target context.Context = caller
									if originating {
										origin.Gate = gate
										target = context.Background()
									}
									var out engine.RuleOutcome
									if whole {
										out = md062.DestinationWhitespace(target, c)
									} else {
										out = md062.CompareDestinationWhitespace(target, c, items.Spans)
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
								in, e := engine.NewInput("literal.md", []byte(source), md062Settings(), engine.DocumentContext{Flavor: flavor})
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

func TestMD062ComparisonAndOuterFinalStickyDiscard(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, kind := range []string{"work", "nodes", "elapsed"} {
				for _, outer := range []bool{false, true} {
					for _, originating := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/outer-%v/origin-%v", kind, outer, originating), func(t *testing.T) {
							synctest.Test(t, func(t *testing.T) {
								budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
								gate := ".compareDestinationWhitespace.func1"
								if outer {
									gate = ".destinationWhitespace.func1"
								}
								origin := &ruletest.GateQuotaContext{Context: context.Background(), Budget: budget, Kind: kind, Gate: gate}
								called := false
								r := ruletest.Registry(t, []engine.Registration{{ID: "MD062", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
									items, e := c.DestinationWhitespace(ctx)
									pre := md062.CompareDestinationWhitespace(ctx, c, items.Spans)
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
										out = md062.DestinationWhitespace(target, c)
									} else {
										out = md062.CompareDestinationWhitespace(target, c, items.Spans)
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
								in, e := engine.NewInput("literal.md", []byte("[Guide](my guide.md)"), md062Settings(), engine.DocumentContext{Flavor: flavor})
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

func TestMD062StaticEmptyCallbackReservation(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, delta := range []int64{-1, 0} {
			t.Run(fmt.Sprintf("%s/delta-%d", flavor, delta), func(t *testing.T) {
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				called := false
				r := ruletest.Registry(t, []engine.Registration{{ID: "MD062", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					called = true
					// Entire original setup is source-derived: acquisition1, original empty
					// parse25/30, directive catalog82+82+82, Comments root1, entryCheckpoint1.
					// No MD087 implementation/inspection/applicability is registered here.
					prefix := int64(274)
					if flavor == "gfm" {
						prefix = 279
					}
					// Facade6 +empty comparator2 +distinct outer final1 =callback9W/0N.
					if e := budget.ChargeWork(budget.Limits().WorkUnits - prefix - 9 - delta); e != nil {
						t.Fatal(e)
					}
					out := md062.DestinationWhitespace(ctx, c)
					if delta == 0 {
						if out.Err != nil || len(out.Findings) != 0 || out.IncompleteReason != "" {
							t.Fatal(out)
						}
						return out
					}
					for range 2 {
						var stop *parser.LimitExceeded
						if !errors.As(out.Err, &stop) || stop.Name != "work-units" || out.Findings != nil || out.IncompleteReason != "" {
							t.Fatal(out)
						}
						out = md062.DestinationWhitespace(ctx, c)
					}
					return out
				}}})
				ruletest.AnalyzeDocument(t, r, "", md062Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{Budget: budget})
				if !called {
					t.Fatal("actual callback not reached")
				}
			})
		}
	}
}

func TestMD062StaticPositiveComparatorReservation(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, resource := range []string{"work", "nodes"} {
			for _, boundary := range []string{"entry", "upper", "lower"} {
				t.Run(flavor+"/"+resource+"/"+boundary, func(t *testing.T) {
					// Source-derived prefix bounds include original14B parsing, engine entry,
					// one real producer inventory and one positive comparator. Buckets:
					// parse/index4096W; Text4409+144W; candidate/destination<=512W;
					// traversal/growth2048W; engine acquisition57+catalog261+Comments<=16
					// +entry1 and comparator42W. B_W65536 leaves fixed/root/filter reserve.
					// Node allocations (parser <=1024, directive82, units45, candidates and
					// vectors/sort<=256, comparator2)
					// fit B_N4096. No used or remaining budget counters are observed.
					per, bound := int64(42), int64(65536)
					if resource == "nodes" {
						per, bound = 2, 4096
					}
					count := bound + 2
					ceiling := bound
					if boundary == "upper" {
						ceiling = per*count + bound
					}
					if boundary == "lower" {
						ceiling = per*count - 1
					}
					limits := parser.DefaultLimits()
					limits.OperationTime = 5 * time.Minute
					if resource == "work" {
						limits.WorkUnits = ceiling
					} else {
						limits.TotalNodes = ceiling
						limits.DocumentNodes = ceiling
					}
					budget, e := parser.NewOperationBudget(limits)
					if e != nil {
						t.Fatal(e)
					}
					called, failed := false, false
					r := ruletest.Registry(t, []engine.Registration{{ID: "MD062", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						called = true
						items, e := c.DestinationWhitespace(ctx)
						if e != nil || items.Availability != parser.Available || len(items.Spans) != 1 || items.Spans[0].Start() != 0 || items.Spans[0].End() != 14 {
							t.Fatal("actual original positive prerequisite", items, e)
						}
						pre := md062.CompareDestinationWhitespace(ctx, c, items.Spans)
						if pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span != items.Spans[0] {
							t.Fatal("actual comparator prerequisite", pre)
						}
						if boundary == "entry" {
							return engine.RuleOutcome{}
						}
						for range count {
							out := md062.CompareDestinationWhitespace(ctx, c, items.Spans)
							if out.Err != nil {
								var stop *parser.LimitExceeded
								want := "work-units"
								if resource == "nodes" {
									want = "total-nodes"
								}
								if boundary != "lower" || !errors.As(out.Err, &stop) || stop.Name != want || out.Findings != nil {
									t.Fatal(out)
								}
								failed = true
								return out
							}
							if !reflect.DeepEqual(out, pre) {
								t.Fatal("positive comparator changed", out, pre)
							}
						}
						if boundary == "lower" {
							t.Fatal("one-short repeated comparator allowance admitted all calls")
						}
						return engine.RuleOutcome{}
					}}})
					// Inspect the callback's direct outcomes; later engine reporting is outside
					// the pinned comparator boundary. All repeated calls retain one source and
					// its original operation. With K>B+1 and E<=B, upper success/lower typed
					// failure excludes both C+1 and C-1 integer charges per deterministic call.
					ruletest.AnalyzeDocument(t, r, "[x](https:/ x)", md062Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{Budget: budget})
					if !called || boundary == "lower" && !failed {
						t.Fatal("comparator boundary was not reached", called, failed)
					}
				})
			}
		}
	}
}
