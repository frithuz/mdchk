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

func md086Settings() engine.Settings {
	s := engine.Settings{}
	off, on := false, true
	for _, m := range engine.Catalog() {
		s[m.ID] = engine.RuleSetting{Enabled: &off}
	}
	s["MD086"] = engine.RuleSetting{Enabled: &on}
	return s
}
func TestMD086FrozenAnalysisFixtures(t *testing.T) {
	data, e := os.ReadFile("testdata/md086.json")
	if e != nil {
		t.Fatal(e)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != "040049a12181feead5852bd61b889b0a68ded6da44b45b6c6c7a8941eda3391f" {
		t.Fatal("root oracle identity")
	}
	var oracle struct {
		Cases []struct {
			ID, Source           string
			SHA                  string `json:"source_sha256"`
			Bytes                int    `json:"source_bytes"`
			Flavors              []string
			Frontmatter, Enabled *bool
			Allow                *bool `json:"allow_directives"`
			Directives           *bool `json:"directives_enabled"`
			Complete             bool  `json:"analysis_complete"`
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
				s := md086Settings()
				setting := s["MD086"]
				setting.Enabled = f.Enabled
				setting.AllowDirectives = f.Allow
				s["MD086"] = setting
				if f.ID == "default-omitted" {
					delete(s, "MD086")
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
					denied["MD086"] = true
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
					if d.Rule != "MD086" || d.Message != "HTML comment is missing a closing delimiter." || d.Message != w.Message || [2]int{d.ByteRange.Start, d.ByteRange.End} != w.Bytes || d.Range.Start != (result.Position{Line: w.Start[0], Column: w.Start[1]}) || d.Range.End != (result.Position{Line: w.End[0], Column: w.End[1]}) || f.Source[w.Bytes[0]:w.Bytes[1]] != w.Text || d.Snapshot.SHA256 != f.SHA || d.Snapshot.SizeBytes != f.Bytes || d.Snapshot.View != "original" || d.FixAvailable {
						t.Fatal(d, w)
					}
				}
			})
		}
	}
	if len(oracle.Cases) != 101 || len(sources) != 97 || candidates != 45 || ranges != 43 || executions != 201 {
		t.Fatal(len(oracle.Cases), len(sources), candidates, ranges, executions)
	}
}

func TestMD086AvailabilityAndStrictOptions(t *testing.T) {
	r := registry(t, Registrations())
	states, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	m := states["MD086"]
	if !r.Available("MD086") || !m.Enabled || !m.Available || len(m.Options) != 0 {
		t.Fatal(m)
	}
	on, off := 0, 0
	for _, metadata := range r.Metadata() {
		if metadata.DefaultEnabled {
			on++
		} else {
			off++
		}
		if metadata.ID == "MD086" && (!metadata.DefaultEnabled || !metadata.RequiresSyntax || metadata.UsesFrontmatter || metadata.Fix != engine.NoFix || len(metadata.Options) != 0) {
			t.Fatal(metadata)
		}
	}
	if on != 59 || off != 28 {
		t.Fatal(on, off)
	}
	disabled := false
	for _, key := range []string{"unknown", "allowed_tags", "options", "style"} {
		s := md086Settings()
		s["MD086"] = engine.RuleSetting{Enabled: &disabled, Options: engine.Options{key: nil}}
		if _, err := r.ResolveSettings(s, engine.ValidationProviders{}); err == nil {
			t.Fatal(key)
		}
	}
}

func TestMD086ComparisonRejectsLateFactsAtomically(t *testing.T) {
	for _, kind := range []string{"absent", "foreign", "endpoint", "spelling", "order"} {
		t.Run(kind, func(t *testing.T) {
			observed := false
			r := registry(t, []engine.Registration{{ID: "MD086", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				items, err := c.UnclosedHTMLComments(ctx)
				if err != nil || len(items.Comments) != 2 {
					t.Fatal(items, err)
				}
				prefix := compareUnclosedHTMLComments(ctx, c, items.Comments[:1])
				if prefix.Err != nil || len(prefix.Findings) != 1 || prefix.Findings[0].Span != items.Comments[0].Open || prefix.Findings[0].Span.Start() != 2 || prefix.Findings[0].Span.End() != 6 || prefix.Findings[0].Message != "HTML comment is missing a closing delimiter." {
					t.Fatal("earlier finding precondition", prefix)
				}
				bad := items.Comments[1]
				switch kind {
				case "absent":
					bad.Open = parser.Span{}
				case "foreign":
					bad.Open, _ = parser.NewSource([]byte("> <!-- one\n\n<!-- two")).Span(12, 16)
				case "endpoint":
					bad.Open, _ = c.Source().Span(12, 15)
				case "spelling":
					bad.Open, _ = c.Source().Span(13, 17)
				case "order":
					bad.Open = items.Comments[0].Open
				}
				items.Comments[1] = bad
				out := compareUnclosedHTMLComments(ctx, c, items.Comments)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				observed = true
				return out
			}}})
			analyzeHeadings(t, r, "> <!-- one\n\n<!-- two", md086Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if !observed {
				t.Fatal("callback not exercised")
			}
		})
	}
}

func TestMD086ComparisonAndOuterFinalCallerAndOrigin(t *testing.T) {
	for _, tc := range []struct {
		name, source      string
		comparison, whole int
	}{{"empty", "", 2, 11}, {"clean", "clean", 2, 13}, {"code", "`ab`", 2, 13}, {"closed", "<!--x-->", 2, 13}, {"finding", "<!--", 6, 16}, {"unavailable", "<div\n", 0, 12}} {
		for _, whole := range []bool{false, true} {
			if !whole && tc.comparison == 0 {
				continue
			}
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%s/whole-%v/origin-%v/%v", tc.name, whole, originating, reason), func(t *testing.T) {
						origin := &md033StopAt{Context: context.Background(), stop: 1 << 30, reason: reason}
						observed := false
						r := registry(t, []engine.Registration{{ID: "MD086", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							var items parser.UnclosedHTMLComments
							if !whole {
								var err error
								items, err = c.UnclosedHTMLComments(ctx)
								if err != nil {
									t.Fatal(err)
								}
							}
							final := tc.comparison
							if whole {
								final = tc.whole
							}
							caller := &md033StopAt{Context: context.Background(), stop: final, reason: reason}
							var target context.Context = caller
							if originating {
								origin.stop = origin.calls + final
								target = context.Background()
							}
							var out engine.RuleOutcome
							if whole {
								out = unclosedHTMLComments(target, c)
							} else {
								out = compareUnclosedHTMLComments(target, c, items.Comments)
							}
							if !errors.Is(out.Err, reason) || out.Findings != nil || out.IncompleteReason != "" {
								t.Fatal(out)
							}
							observed = true
							return out
						}}})
						in, err := engine.NewInput("literal.md", []byte(tc.source), md086Settings(), engine.DocumentContext{})
						if err != nil {
							t.Fatal(err)
						}
						_, err = r.Lint(origin, []engine.Input{in}, engine.RunOptions{})
						if err != nil || !observed {
							t.Fatal(err, observed)
						}
					})
				}
			}
		}
	}
}

func TestMD086CallbackStickyControls(t *testing.T) {
	for _, source := range []string{"", "clean", "<!--x-->", "`ab`", "<!--", "<div\n"} {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			t.Run(source+"/"+kind, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					b, _ := parser.NewOperationBudget(parser.DefaultLimits())
					observed := false
					r := registry(t, []engine.Registration{{ID: "MD086", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
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
							out = unclosedHTMLComments(ctx, c)
							var stop *parser.LimitExceeded
							if !errors.As(out.Err, &stop) || out.Findings != nil || out.IncompleteReason != "" {
								t.Fatal(out)
							}
						}
						observed = true
						return out
					}}})
					analyzeHeadings(t, r, source, md086Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: b})
					if !observed {
						t.Fatal("callback not exercised")
					}
				})
			})
		}
	}
}
