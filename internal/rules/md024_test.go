package rules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func md024Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD024"] = engine.RuleSetting{Enabled: &on}
	return s
}

func TestMD024AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md024.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source                    string
		Recognize, Incomplete, Siblings bool
		Expected, GFMExpected           []struct {
			Range  []int
			Source string
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	r := registry(t, Registrations())
	for _, f := range fixtures {
		if f.Name == "nul" && !strings.ContainsRune(f.Source, 0) {
			t.Fatal("fixture did not decode original NUL byte")
		}
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(f.Name+"/"+flavor, func(t *testing.T) {
				fm, _ := parser.NewFrontmatterOptions(f.Recognize, nil)
				dc := engine.DocumentContext{Flavor: flavor, Frontmatter: fm}
				settings := md024Settings()
				settings["MD024"] = engine.RuleSetting{Options: engine.Options{"siblings_only": f.Siblings}}
				expected := f.Expected
				if flavor == "gfm" && f.GFMExpected != nil {
					expected = f.GFMExpected
				}
				out := analyzeHeadings(t, r, f.Source, settings, dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, settings, dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) {
					t.Fatal("nondeterministic analysis")
				}
				if out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(expected) {
					t.Fatalf("unexpected outcome: %+v", out)
				}
				if f.Incomplete {
					found := false
					for _, c := range out.Coverage {
						found = found || c.Reason == result.ParserUncertain
					}
					if !found {
						t.Fatal(out.Coverage)
					}
				}
				for i, want := range expected {
					d := out.Diagnostics[i]
					got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if !reflect.DeepEqual(got, want.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != want.Source || d.Rule != "MD024" || d.Message != "Heading text must not repeat." {
						t.Fatalf("got %v %+v, want %+v", got, d, want)
					}
				}
			})
		}
	}
}

func TestMD024AddedWorkInterruptions(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		var observed error
		r := registry(t, []engine.Registration{{ID: "MD024", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			out := repeatedHeadingText(&traversalContext{Context: ctx, err: reason}, c)
			observed = out.Err
			return out
		}}})
		out := analyzeHeadings(t, r, " # H\n\n # H\n\n # H", md024Settings(), engine.DocumentContext{}, engine.RunOptions{})
		if !errors.Is(observed, reason) || out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "analysis" || len(out.Errors) != 0 {
			t.Fatal(observed, out)
		}
	}
	for _, kind := range []string{"work", "nodes"} {
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		r := registry(t, []engine.Registration{{ID: "MD024", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			if kind == "work" {
				_ = budget.ChargeWork(budget.Limits().WorkUnits)
			} else {
				_ = budget.ChargeNodes(budget.Limits().TotalNodes)
			}
			return repeatedHeadingText(ctx, c)
		}}})
		out := analyzeHeadings(t, r, " # H\n\n # H\n\n # H", md024Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
		if out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "analysis" || out.Interruptions[0].Limit == nil || len(out.Errors) != 0 {
			t.Fatal(out)
		}
	}
}

func TestMD024LateInterruptionRetainsReliableMarkers(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, source := range []string{" # H\n\n # H\n\n # H", " # H\n\n # H\n\n # H\n\n # H"} {
			calls := 0
			baseline := registry(t, []engine.Registration{{ID: "MD024", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				check := &md018LateContext{Context: ctx, reason: reason}
				out := repeatedHeadingText(check, c)
				calls = check.calls
				return out
			}}})
			full := analyzeHeadings(t, baseline, source, md024Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if !full.Complete || len(full.Diagnostics) < 2 {
				t.Fatal(full.Diagnostics)
			}
			// Discover stops from the deterministic caller checkpoint sequence and require
			// every possible retained prefix, including the prefix before the final heading.
			retained := map[int]bool{}
			for stop := 1; stop <= calls; stop++ {
				var observed engine.RuleOutcome
				var token parser.SourceToken
				r := registry(t, []engine.Registration{{ID: "MD024", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					token = c.Source().Token()
					observed = repeatedHeadingText(&md018LateContext{Context: ctx, reason: reason, stop: stop}, c)
					return observed
				}}})
				out := analyzeHeadings(t, r, source, md024Settings(), engine.DocumentContext{}, engine.RunOptions{})
				n := len(observed.Findings)
				if n == 0 {
					continue
				}
				if !errors.Is(observed.Err, reason) || out.Complete || len(out.Diagnostics) != n || len(out.Interruptions) != 1 || len(out.Coverage) == 0 || len(out.Errors) != 0 {
					t.Fatalf("stop %d: %+v %+v", stop, observed, out)
				}
				for i, f := range observed.Findings {
					if f.Span.SourceToken() != token || out.Diagnostics[i].ByteRange != full.Diagnostics[i].ByteRange || out.Diagnostics[i].Range != full.Diagnostics[i].Range {
						t.Fatal("prior marker/source lost", f, out.Diagnostics)
					}
				}
				retained[n] = true
			}
			for n := 1; n < len(full.Diagnostics); n++ {
				if !retained[n] {
					t.Fatalf("never retained %d reliable markers", n)
				}
			}
		}
	}
}

func TestMD024CatalogAndOptions(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD024") || r.Available("MD026") || len(r.Metadata()) != 87 {
		t.Fatal("incorrect availability")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD024" && (!m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.NoFix || len(m.Options) != 1) {
			t.Fatal(m)
		}
	}
	for _, value := range []any{nil, 1, "false", []any{}, map[string]any{}} {
		for _, enabled := range []bool{true, false} {
			_, err := r.ResolveSettings(engine.Settings{"MD024": {Enabled: &enabled, Options: engine.Options{"siblings_only": value}}}, engine.ValidationProviders{})
			if err == nil {
				t.Fatal("invalid siblings_only accepted", value, enabled)
			}
		}
	}
	for _, mapping := range []map[string]any{{"unknown": true}, {"enabled": false, "unknown": true}, {"enabled": "false"}, {"enabled": nil}, {"enabled": 1}, {"allow_directives": "false"}, {"allow_directives": nil}, {"enabled": false, "allow_directives": 1}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD024": setting}, engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal("invalid options accepted", mapping)
		}
	}
	for _, mapping := range []map[string]any{{}, {"enabled": false}, {"enabled": true, "allow_directives": false}, {"siblings_only": true}, {"siblings_only": false}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.ResolveSettings(engine.Settings{"MD024": setting}, engine.ValidationProviders{}); err != nil {
			t.Fatal(err)
		}
	}
	// Omission uses the catalog default, independently of explicit admission.
	s := md024Settings()
	delete(s, "MD024")
	out := analyzeHeadings(t, r, "## default\n## DEFAULT", s, engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 1 {
		t.Fatal(out)
	}
}

func TestMD024Directives(t *testing.T) {
	for _, tc := range []struct {
		name, source          string
		enabled, allowed      bool
		active, hidden, owner int
	}{
		{"last disable", "# A\n<!-- mdchk:disable MD024 -->\n<!-- mdchk:disable MD024 -->\n# a", true, true, 0, 1, 1},
		{"configured off then enable", "# A\n<!-- mdchk:enable MD024 -->\n# a", false, true, 1, 0, 0},
		{"hidden first keeps context", "<!-- mdchk:disable MD024 -->\n# A\n<!-- mdchk:enable MD024 -->\n# a", true, true, 1, 0, 0},
		{"off throughout", "# A\n# a", false, true, 0, 0, 0},
		{"disable denied", "# A\n<!-- mdchk:disable MD024 -->\n# a", true, false, 1, 0, 0},
		{"enable denied", "# A\n<!-- mdchk:enable MD024 -->\n# a", false, false, 0, 0, 0},
		{"label starts before inline disable", "# A\n# a <!-- mdchk:disable MD024 -->", true, true, 1, 0, 0},
		{"label starts with inline disable", "# A\n# <!-- mdchk:disable MD024 -->a", true, true, 1, 0, 0},
		{"Setext label starts before disable", "# A B\n\na\nb <!-- mdchk:disable MD024 -->\n===", true, true, 1, 0, 0},
		{"protected fake", "# A\n`<!-- mdchk:disable MD024 -->`\n\n# a", true, true, 1, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := md024Settings()
			s["MD024"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allowed}
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, s, engine.DocumentContext{}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
				t.Fatalf("active=%v suppressed=%v", out.Diagnostics, out.SuppressedDiagnostics)
			}
			if tc.hidden > 0 {
				trace := out.AnalysisRecords()[0].(engine.DirectiveAnalysis)
				if trace.Suppressed()[0].Owner != tc.owner {
					t.Fatal(trace.Suppressed())
				}
			}
		})
	}
}

func TestMD024LongAndManyKeysShareOriginalAllowance(t *testing.T) {
	for _, source := range []string{"# " + strings.Repeat("Straße ", 1000) + "\n# " + strings.Repeat("STRASSE ", 1000), md024ManyKeys()} {
		r := registry(t, Registrations())
		full := analyzeHeadings(t, r, source, md024Settings(), engine.DocumentContext{}, engine.RunOptions{})
		if !full.Complete || len(full.Diagnostics) == 0 {
			t.Fatal("full long/many analysis", full)
		}
		// Exhaust only after real parsing and heading projection, proving that key
		// processing cannot create its own operation allowance.
		for _, kind := range []string{"work", "nodes"} {
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			r := registry(t, []engine.Registration{{ID: "MD024", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				if _, err := c.Headings(ctx); err != nil {
					return engine.RuleOutcome{Err: err}
				}
				if kind == "work" {
					_ = budget.ChargeWork(budget.Limits().WorkUnits)
				} else {
					_ = budget.ChargeNodes(budget.Limits().TotalNodes)
				}
				return repeatedHeadingText(ctx, c)
			}}})
			out := analyzeHeadings(t, r, source, md024Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
			if out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Limit == nil || len(out.Errors) != 0 {
				t.Fatal("reset original key allowance", kind, out)
			}
		}
	}
}

func TestMD024SuppressedParentIdentity(t *testing.T) {
	source := "# P\n## Child\n<!-- mdchk:disable MD024 -->\n# p\n<!-- mdchk:enable MD024 -->\n## child\n## CHILD"
	s := md024Settings()
	s["MD024"] = engine.RuleSetting{Options: engine.Options{"siblings_only": true}}
	out := analyzeHeadings(t, registry(t, Registrations()), source, s, engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 1 || len(out.SuppressedDiagnostics) != 1 || out.Diagnostics[0].Range.Start.Line != 7 {
		t.Fatal("suppressed actual parent identity lost", out)
	}
}

func md024ManyKeys() string {
	var out strings.Builder
	for i := range 2000 {
		fmt.Fprintf(&out, "# Unique %04d\n", i)
	}
	out.WriteString("# unique 0000")
	return out.String()
}
