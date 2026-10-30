package rules

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"os"
	"testing"
	"testing/synctest"
	"time"
)

func md033Settings() engine.Settings {
	s := engine.Settings{}
	off, on := false, true
	for _, m := range engine.Catalog() {
		s[m.ID] = engine.RuleSetting{Enabled: &off}
	}
	s["MD033"] = engine.RuleSetting{Enabled: &on}
	return s
}
func TestMD033FrozenAnalysisFixtures(t *testing.T) {
	data, e := os.ReadFile("testdata/md033.json")
	if e != nil {
		t.Fatal(e)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != "4cc912cbc98368c037ecb3dc8d307be76be21affef69d8897b026c038dc70e37" {
		t.Fatal("root oracle identity")
	}
	var oracle struct {
		Cases []struct {
			ID, Source           string
			SHA                  string `json:"source_sha256"`
			Bytes                int    `json:"source_bytes"`
			Flavors              []string
			Frontmatter, Enabled *bool
			Allow                *bool    `json:"allow_directives"`
			Directives           *bool    `json:"directives_enabled"`
			AllowedTags          []string `json:"allowed_tags"`
			Complete             bool     `json:"analysis_complete"`
			Shape                string   `json:"setting_shape"`
			Candidates           []struct {
				Bytes, Start, End [2]int
				Text, Message     string
				Active            bool `json:"active_at_range_start"`
			}
			Findings []struct {
				Bytes, Start, End [2]int
				Text, Message     string
			}
		}
	}
	if e = json.Unmarshal(data, &oracle); e != nil {
		t.Fatal(e)
	}
	sources := map[string]bool{}
	executions, ranges, candidates := 0, 0, 0
	for _, f := range oracle.Cases {
		sources[f.Source] = true
		ranges += len(f.Findings)
		candidates += len(f.Candidates)
		for _, flavor := range f.Flavors {
			executions++
			t.Run(f.ID+"/"+flavor, func(t *testing.T) {
				if len(f.Source) != f.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source))) != f.SHA {
					t.Fatal("literal identity")
				}
				s := md033Settings()
				setting := s["MD033"]
				setting.Enabled = f.Enabled
				setting.AllowDirectives = f.Allow
				setting.Options = engine.Options{"allowed_tags": f.AllowedTags}
				s["MD033"] = setting
				if f.ID == "default-omitted" {
					delete(s, "MD033")
				}
				dc := engine.DocumentContext{Flavor: flavor}
				if f.Frontmatter != nil {
					dc.Frontmatter, e = parser.NewFrontmatterOptions(*f.Frontmatter, nil)
					if e != nil {
						t.Fatal(e)
					}
				}
				r := registry(t, Registrations())
				sel, e := r.SelectRules(s, engine.ValidationProviders{})
				if e != nil {
					t.Fatal(e)
				}
				denied := map[string]bool{}
				if f.Directives != nil && !*f.Directives {
					denied["MD033"] = true
				}
				plan, e := sel.Plan(dc, denied)
				if e != nil {
					t.Fatal(e)
				}
				original := []byte(f.Source)
				in, e := plan.NewInput("literal.md", original)
				if e != nil {
					t.Fatal(e)
				}
				out, e := plan.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
				if e != nil || out.Complete != f.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(f.Findings) || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 || string(original) != f.Source || string(in.Source().Bytes()) != f.Source {
					t.Fatal(out, e)
				}
				if !f.Complete && len(out.Coverage) == 0 {
					t.Fatal("unterminated header lacks coverage")
				}
				for i, d := range out.Diagnostics {
					w := f.Findings[i]
					if d.Rule != "MD033" || d.Message != "Embedded HTML tag is not allowed." || d.Message != w.Message || [2]int{d.ByteRange.Start, d.ByteRange.End} != w.Bytes || d.Range.Start != (result.Position{Line: w.Start[0], Column: w.Start[1]}) || d.Range.End != (result.Position{Line: w.End[0], Column: w.End[1]}) || f.Source[w.Bytes[0]:w.Bytes[1]] != w.Text || d.Snapshot.SHA256 != f.SHA || d.Snapshot.SizeBytes != f.Bytes || d.Snapshot.View != "original" || d.FixAvailable {
						t.Fatal(d, w)
					}
				}
			})
		}
	}
	if len(oracle.Cases) != 98 || len(sources) != 94 || candidates != 151 || ranges != 134 || executions != 194 {
		t.Fatal(len(oracle.Cases), len(sources), candidates, ranges, executions)
	}
}

func TestMD033AvailabilityAndStrictOptions(t *testing.T) {
	r := registry(t, Registrations())
	states, e := r.ResolveSettings(nil, engine.ValidationProviders{})
	if e != nil {
		t.Fatal(e)
	}
	m := states["MD033"]
	if !r.Available("MD033") || m.Enabled || !m.Available || len(m.Options["allowed_tags"].([]string)) != 0 {
		t.Fatal(m)
	}
	on, off := 0, 0
	for _, metadata := range r.Metadata() {
		if metadata.DefaultEnabled {
			on++
		} else {
			off++
		}
		if metadata.ID == "MD033" && (metadata.DefaultEnabled || !metadata.RequiresSyntax || metadata.UsesFrontmatter || metadata.Fix != engine.NoFix) {
			t.Fatal(metadata)
		}
	}
	if on != 59 || off != 28 {
		t.Fatal(on, off)
	}
	disabled := false
	for _, v := range []any{nil, true, 1, "b", []any{"b", 1}} {
		s := md033Settings()
		s["MD033"] = engine.RuleSetting{Enabled: &disabled, Options: engine.Options{"allowed_tags": v}}
		if _, e := r.ResolveSettings(s, engine.ValidationProviders{}); e == nil {
			t.Fatal(v)
		}
	}
	for _, key := range []string{"unknown", "style"} {
		s := md033Settings()
		s["MD033"] = engine.RuleSetting{Enabled: &disabled, Options: engine.Options{key: "b"}}
		if _, e := r.ResolveSettings(s, engine.ValidationProviders{}); e == nil {
			t.Fatal(key)
		}
	}
	for _, list := range [][]string{{}, {"", " b ", "<b>", "/b", "b/", "ſ", "\x00", string([]byte{0xff})}, {"b", "B", "b"}, {"X-CARD"}} {
		s := md033Settings()
		s["MD033"] = engine.RuleSetting{Enabled: &disabled, Options: engine.Options{"allowed_tags": list}}
		got, e := r.ResolveSettings(s, engine.ValidationProviders{})
		if e != nil || len(got["MD033"].Options["allowed_tags"].([]string)) != len(list) {
			t.Fatal(got, e)
		}
	}
}

func TestMD033ComparisonRejectsLateFactsAtomically(t *testing.T) {
	for _, kind := range []string{"absent", "foreign", "endpoint", "name", "association"} {
		t.Run(kind, func(t *testing.T) {
			observed := false
			r := registry(t, []engine.Registration{{ID: "MD033", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				items, e := c.EmbeddedHTMLTags(ctx)
				if e != nil || len(items.Tags) != 2 {
					t.Fatal(items, e)
				}
				// Establish the real earlier-output path before introducing the late
				// mutant. This is ordinary semantic acceptance, not budget calibration.
				prefix := compareEmbeddedHTML(ctx, c, items.Tags[:1], nil)
				if prefix.Err != nil || len(prefix.Findings) != 1 || prefix.Findings[0].Span != items.Tags[0].Tag || prefix.Findings[0].Span.Start() != 2 || prefix.Findings[0].Span.End() != 5 || prefix.Findings[0].Message != "Embedded HTML tag is not allowed." {
					t.Fatal("earlier finding precondition", prefix)
				}
				// The mutant stays after it, so endpoint/name association checks,
				// rather than ordering, reject it. The name/association mutants test
				// bounds and starting association, not a distinct late grammar path.
				bad := items.Tags[1]
				switch kind {
				case "absent":
					bad.Name = parser.Span{}
				case "foreign":
					s := parser.NewSource([]byte("a <b>x</b> z"))
					bad.Tag, _ = s.Span(6, 10)
					bad.Name, _ = s.Span(8, 9)
				case "endpoint":
					bad.Tag, _ = c.Source().Span(6, 9)
				case "name":
					bad.Name, _ = c.Source().Span(7, 9)
				case "association":
					bad.Name, _ = c.Source().Span(8, 10)
				}
				items.Tags[1] = bad
				out := compareEmbeddedHTML(ctx, c, items.Tags, nil)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				observed = true
				return out
			}}})
			analyzeHeadings(t, r, "a <b>x</b> z", md033Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if !observed {
				t.Fatal("callback not exercised")
			}
		})
	}
}

var md033PhaseRows = []struct {
	name, source                         string
	allowed                              []string
	pwork, pnodes, cwork, cnodes         int64
	want, producerFinal, comparisonFinal int
	unavailable                          bool
}{
	{"empty", "", nil, 32, 2, 2, 0, 0, 5, 2, false},
	{"clean", "clean", nil, 82, 4, 2, 0, 0, 7, 2, false},
	{"code", "`ab`", nil, 82, 4, 2, 0, 0, 7, 2, false},
	{"comment", "<!--x-->", nil, 202, 4, 2, 0, 0, 7, 2, false},
	{"one", "<b>", nil, 406, 7, 114, 2, 1, 10, 9, false},
	{"two", "<b></b>", nil, 685, 15, 228, 5, 2, 15, 16, false},
	{"allowed", "<b>", []string{"B"}, 406, 7, 118, 0, 0, 10, 7, false},
	{"raw", "<pre><b>x</b></pre>", nil, 1071, 10, 232, 5, 2, 19, 16, false},
	{"unavailable", "<div\n", nil, 199, 5, 0, 0, 0, 8, 0, true},
}

func TestMD033ComparisonAndWholeStaticBoundaries(t *testing.T) {
	for _, tc := range md033PhaseRows {
		for _, whole := range []bool{false, true} {
			if tc.unavailable && !whole {
				continue
			}
			for _, nodes := range []bool{false, true} {
				exact := tc.cwork
				if whole {
					exact += tc.pwork + 41 + 2*int64(len(tc.allowed))
				}
				if nodes {
					exact = tc.cnodes
					if whole {
						exact += tc.pnodes + 2*int64(len(tc.allowed))
					}
				}
				for _, delta := range []int64{-1, 0} {
					if exact == 0 && delta < 0 {
						continue
					}
					t.Run(fmt.Sprintf("%s/whole-%v/nodes-%v/%d", tc.name, whole, nodes, delta), func(t *testing.T) {
						var used int64
						run := func(prefix bool) {
							b, _ := parser.NewOperationBudget(parser.DefaultLimits())
							observed := false
							r := registry(t, []engine.Registration{{ID: "MD033", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
								var items parser.EmbeddedHTML
								if !whole {
									var e error
									items, e = c.EmbeddedHTMLTags(ctx)
									if e != nil {
										t.Fatal(items, e)
									}
								}
								charge, limit := b.ChargeWork, b.Limits().WorkUnits
								if nodes {
									charge, limit = b.ChargeNodes, b.Limits().TotalNodes
								}
								if prefix {
									var stop *parser.LimitExceeded
									if !errors.As(charge(limit), &stop) {
										t.Fatal("prefix must exhaust")
									}
									used = stop.Observed - limit
									return engine.RuleOutcome{Err: stop}
								}
								if e := charge(limit - used - exact - delta); e != nil {
									t.Fatal(e)
								}
								call := func() engine.RuleOutcome {
									if whole {
										return embeddedHTML(ctx, c)
									}
									return compareEmbeddedHTML(ctx, c, items.Tags, tc.allowed)
								}
								got := call()
								if delta == 0 {
									if got.Err != nil || len(got.Findings) != tc.want || (got.IncompleteReason != "") != tc.unavailable {
										t.Fatal(got)
									}
								} else {
									for range 2 {
										var stop *parser.LimitExceeded
										if !errors.As(got.Err, &stop) || got.Findings != nil {
											t.Fatal(got)
										}
										got = call()
									}
								}
								observed = true
								return got
							}}})
							settings := md033Settings()
							setting := settings["MD033"]
							setting.Options = engine.Options{"allowed_tags": tc.allowed}
							settings["MD033"] = setting
							analyzeHeadings(t, r, tc.source, settings, engine.DocumentContext{}, engine.RunOptions{Budget: b})
							if !prefix && !observed {
								t.Fatal("target phase not exercised")
							}
						}
						run(true)
						run(false)
					})
				}
			}
		}
	}
}

func TestMD033ComparisonAndOuterFinalCallerAndOrigin(t *testing.T) {
	for _, tc := range md033PhaseRows {
		for _, whole := range []bool{false, true} {
			if tc.unavailable && !whole {
				continue
			}
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%s/whole-%v/origin-%v/%v", tc.name, whole, originating, reason), func(t *testing.T) {
						origin := &md033StopAt{Context: context.Background(), stop: 1 << 30, reason: reason}
						observed := false
						r := registry(t, []engine.Registration{{ID: "MD033", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							var items parser.EmbeddedHTML
							if !whole {
								var e error
								items, e = c.EmbeddedHTMLTags(ctx)
								if e != nil {
									t.Fatal(items, e)
								}
							}
							final := tc.comparisonFinal
							if whole {
								final += tc.producerFinal + 11
							}
							caller := &md033StopAt{Context: context.Background(), stop: final, reason: reason}
							var target context.Context = caller
							if originating {
								origin.stop = origin.calls + final
								target = context.Background()
							}
							var out engine.RuleOutcome
							if whole {
								out = embeddedHTML(target, c)
							} else {
								out = compareEmbeddedHTML(target, c, items.Tags, tc.allowed)
							}
							if !errors.Is(out.Err, reason) || out.Findings != nil || out.IncompleteReason != "" {
								t.Fatal(out)
							}
							observed = true
							return out
						}}})
						settings := md033Settings()
						setting := settings["MD033"]
						setting.Options = engine.Options{"allowed_tags": tc.allowed}
						settings["MD033"] = setting
						in, e := engine.NewInput("literal.md", []byte(tc.source), settings, engine.DocumentContext{})
						if e != nil {
							t.Fatal(e)
						}
						_, e = r.Lint(origin, []engine.Input{in}, engine.RunOptions{})
						if e != nil || !observed {
							t.Fatal(e, observed)
						}
					})
				}
			}
		}
	}
}

type md033StopAt struct {
	context.Context
	calls, stop int
	reason      error
}

func (c *md033StopAt) Err() error {
	c.calls++
	if c.calls >= c.stop {
		return c.reason
	}
	return c.Context.Err()
}

func TestMD033CallbackStickyControls(t *testing.T) {
	for _, source := range []string{"", "clean", "<!--x-->", "`<b>`", "<b>", "<div\n"} {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			t.Run(source+"/"+kind, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					b, _ := parser.NewOperationBudget(parser.DefaultLimits())
					observed := false
					r := registry(t, []engine.Registration{{ID: "MD033", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						switch kind {
						case "work":
							_ = b.ChargeWork(b.Limits().WorkUnits)
						case "nodes":
							_ = b.ChargeNodes(b.Limits().TotalNodes)
						case "elapsed":
							time.Sleep(61 * time.Second)
						}
						var out engine.RuleOutcome
						for range 2 {
							out = embeddedHTML(ctx, c)
							var stop *parser.LimitExceeded
							if !errors.As(out.Err, &stop) || out.Findings != nil || out.IncompleteReason != "" {
								t.Fatal(out)
							}
						}
						observed = true
						return out
					}}})
					analyzeHeadings(t, r, source, md033Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: b})
					if !observed {
						t.Fatal("callback not exercised")
					}
				})
			})
		}
	}
}
