package rules

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func md036Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD036"] = engine.RuleSetting{Enabled: &on}
	return s
}

func TestMD036AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md036.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source          string
		Recognize, Incomplete bool
		Expected, GFMExpected []struct {
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
				settings := md036Settings()

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
					t.Fatalf("complete=%v diagnostics=%v errors=%v interruptions=%v", out.Complete, out.Diagnostics, out.Errors, out.Interruptions)
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
					if !reflect.DeepEqual(got, want.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != want.Source || d.Rule != "MD036" || d.Message != "Use a heading instead of an emphasized paragraph before body text." {
						t.Fatalf("got %v %+v, want %+v", got, d, want)
					}
				}
			})
		}
	}
}

func TestMD036AddedWorkInterruptions(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		var observed error
		r := registry(t, []engine.Registration{{ID: "MD036", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			out := emphasisUsedAsHeading(&traversalContext{Context: ctx, err: reason}, c)
			observed = out.Err
			return out
		}}})
		out := analyzeHeadings(t, r, "**A**\n\nBody\n\n**B**\n\nBody\n\n**C**\n\nBody", md036Settings(), engine.DocumentContext{}, engine.RunOptions{})
		if !errors.Is(observed, reason) || out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "analysis" || len(out.Errors) != 0 {
			t.Fatal(observed, out)
		}
	}
	for _, kind := range []string{"work", "nodes"} {
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		r := registry(t, []engine.Registration{{ID: "MD036", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			if kind == "work" {
				_ = budget.ChargeWork(budget.Limits().WorkUnits)
			} else {
				_ = budget.ChargeNodes(budget.Limits().TotalNodes)
			}
			return emphasisUsedAsHeading(ctx, c)
		}}})
		out := analyzeHeadings(t, r, "**A**\n\nBody\n\n**B**\n\nBody\n\n**C**\n\nBody", md036Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
		if out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "analysis" || out.Interruptions[0].Limit == nil || len(out.Errors) != 0 {
			t.Fatal(out)
		}
	}
}

func TestMD036LateInterruptionRetainsReliableMarkers(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, source := range []string{"**A**\n\nBody\n\n**B**\n\nBody\n\n**C**\n\nBody", "**A**\n\nBody\n\n**B**\n\nBody\n\n**C**\n\nBody\n\n**D**\n\nBody"} {
			calls := 0
			baseline := registry(t, []engine.Registration{{ID: "MD036", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				check := &md018LateContext{Context: ctx, reason: reason}
				out := emphasisUsedAsHeading(check, c)
				calls = check.calls
				return out
			}}})
			full := analyzeHeadings(t, baseline, source, md036Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if !full.Complete || len(full.Diagnostics) < 2 {
				t.Fatal(full.Diagnostics)
			}
			// Discover stops from the deterministic caller checkpoint sequence and require
			// every possible retained prefix, including the prefix before the final heading.
			retained := map[int]bool{}
			for stop := 1; stop <= calls; stop++ {
				var observed engine.RuleOutcome
				var token parser.SourceToken
				r := registry(t, []engine.Registration{{ID: "MD036", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					token = c.Source().Token()
					observed = emphasisUsedAsHeading(&md018LateContext{Context: ctx, reason: reason, stop: stop}, c)
					return observed
				}}})
				out := analyzeHeadings(t, r, source, md036Settings(), engine.DocumentContext{}, engine.RunOptions{})
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

func TestMD036CatalogAndOptions(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD036") || r.Available("MD043") || len(r.Metadata()) != 87 {
		t.Fatal("catalog availability")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD036" && (m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.NoFix || len(m.Options) != 0) {
			t.Fatal(m)
		}
	}
	for _, enabled := range []bool{false, true} {
		if _, err := r.ResolveSettings(engine.Settings{"MD036": {Enabled: &enabled, Options: engine.Options{"unknown": true}}}, engine.ValidationProviders{}); err == nil {
			t.Fatal("unknown option")
		}
	}
	s := md036Settings()
	delete(s, "MD036")
	out := analyzeHeadings(t, r, "**A**\n\nBody", s, engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
}

func TestMD036Directives(t *testing.T) {
	for _, tc := range []struct {
		name, source          string
		enabled, allowed      bool
		active, hidden, owner int
	}{
		{"last disable", "<!-- mdchk:disable MD036 -->\n<!-- mdchk:disable MD036 -->\n**A**\n\nBody", true, true, 0, 1, 1},
		{"configured enable", "<!-- mdchk:enable MD036 -->\n**A**\n\nBody", false, true, 1, 0, 0},
		{"off throughout", "**A**\n\nBody", false, true, 0, 0, 0},
		{"deny disable", "<!-- mdchk:disable MD036 -->\n**A**\n\nBody", true, false, 1, 0, 0},
		{"deny enable", "<!-- mdchk:enable MD036 -->\n**A**\n\nBody", false, false, 0, 0, 0},
		{"inline breaks exact wrapper", "**A** <!-- mdchk:disable MD036 -->\n\nBody", true, true, 0, 0, 0},
		{"comment gap", "**A**\n\n<!-- mdchk:disable MD036 -->\n\nBody", true, true, 0, 0, 0},
		{"protected fake", "`<!-- mdchk:disable MD036 -->`\n\n**A**\n\nBody", true, true, 1, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := md036Settings()
			s["MD036"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allowed}
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, s, engine.DocumentContext{}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
				t.Fatal(out)
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
