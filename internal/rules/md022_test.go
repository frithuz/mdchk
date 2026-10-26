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

func md022Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD022"] = engine.RuleSetting{Enabled: &on}
	return s
}

func TestMD022AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md022.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source          string
		Recognize, Incomplete bool
		Options               map[string]int
		Expected              []struct {
			Range  []int
			Source string
			Side   string
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
				settings := md022Settings()
				setting := settings["MD022"]
				setting.Options = engine.Options{}
				for key, value := range f.Options {
					setting.Options[key] = value
				}
				settings["MD022"] = setting
				out := analyzeHeadings(t, r, f.Source, settings, dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, settings, dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) {
					t.Fatal("nondeterministic analysis")
				}
				if out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(f.Expected) {
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
				for i, want := range f.Expected {
					d := out.Diagnostics[i]
					got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if !reflect.DeepEqual(got, want.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != want.Source || d.Rule != "MD022" || d.Message != "Heading requires more blank body lines "+want.Side+" it." {
						t.Fatalf("got %v %+v, want %+v", got, d, want)
					}
				}
			})
		}
	}
}

func TestMD022AddedWorkInterruptions(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		var observed error
		r := registry(t, []engine.Registration{{ID: "MD022", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			out := headingBlankLines(&traversalContext{Context: ctx, err: reason}, c)
			observed = out.Err
			return out
		}}})
		out := analyzeHeadings(t, r, "Text\n# H\nText", md022Settings(), engine.DocumentContext{}, engine.RunOptions{})
		if !errors.Is(observed, reason) || out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "analysis" || len(out.Errors) != 0 {
			t.Fatal(observed, out)
		}
	}
	for _, kind := range []string{"work", "nodes"} {
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		r := registry(t, []engine.Registration{{ID: "MD022", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			if kind == "work" {
				_ = budget.ChargeWork(budget.Limits().WorkUnits)
			} else {
				_ = budget.ChargeNodes(budget.Limits().TotalNodes)
			}
			return headingBlankLines(ctx, c)
		}}})
		out := analyzeHeadings(t, r, "Text\n# H\nText", md022Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
		if out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "analysis" || out.Interruptions[0].Limit == nil || len(out.Errors) != 0 {
			t.Fatal(out)
		}
	}
}

func TestMD022CatalogOptionsDefaults(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD022") || r.Available("MD036") || len(r.Metadata()) != 87 {
		t.Fatal("availability/catalog changed")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD022" && (!m.DefaultEnabled || !m.RequiresSyntax || m.Fix != engine.PlannedFix || len(m.Options) != 2) {
			t.Fatal(m)
		}
	}
	for _, key := range []string{"before", "after"} {
		for _, value := range []any{-1, 1.5, "1", true, nil, []int{1}} {
			for _, enabled := range []bool{false, true} {
				_, err := r.ResolveSettings(engine.Settings{"MD022": {Enabled: &enabled, Options: engine.Options{key: value}}}, engine.ValidationProviders{})
				if err == nil {
					t.Fatalf("invalid %s=%v admitted", key, value)
				}
			}
		}
	}
	off := false
	if _, err := r.ResolveSettings(engine.Settings{"MD022": {Enabled: &off, Options: engine.Options{"unknown": true}}}, engine.ValidationProviders{}); err == nil {
		t.Fatal("unknown admitted while disabled")
	}
	for _, options := range []engine.Options{nil, {}, {"before": 1, "after": 1}} {
		s := md022Settings()
		delete(s, "MD022")
		if options != nil {
			s["MD022"] = engine.RuleSetting{Options: options}
		}
		out := analyzeHeadings(t, r, "Text\n# H\nText", s, engine.DocumentContext{}, engine.RunOptions{})
		if !out.Complete || len(out.Diagnostics) != 2 {
			t.Fatal("omitted defaults", out.Diagnostics)
		}
	}
}

func TestMD022Directives(t *testing.T) {
	for _, tc := range []struct {
		name, source          string
		enabled, allowed      bool
		active, hidden, owner int
	}{
		{"last disable", "<!-- mdchk:disable MD022 -->\n<!-- mdchk:disable MD022 -->\n# H\nText", true, true, 0, 2, 1},
		{"configured off then enable", "Text\n# H\nText\n<!-- mdchk:enable MD022 -->\n# J\nText", false, true, 2, 2, -1},
		{"off throughout", "Text\n# H\nText", false, true, 0, 0, 0},
		{"disable denied", "<!-- mdchk:disable MD022 -->\n# H\nText", true, false, 2, 0, 0},
		{"enable denied", "<!-- mdchk:enable MD022 -->\n# H\nText", false, false, 0, 0, 0},
		{"inline between anchors", "Text\n# <!-- mdchk:disable MD022 -->\nText", true, true, 1, 1, 0},
		{"protected fake", "Text\n# `<!-- mdchk:disable MD022 -->`\nText", true, true, 2, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := md022Settings()
			s["MD022"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allowed}
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, s, engine.DocumentContext{}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
				t.Fatalf("active=%v suppressed=%v", out.Diagnostics, out.SuppressedDiagnostics)
			}
			if tc.hidden > 0 {
				trace := out.AnalysisRecords()[0].(engine.DirectiveAnalysis)
				if trace.Suppressed()[0].Owner != tc.owner {
					t.Fatal(trace.Suppressed())
				}
				if tc.name == "inline between anchors" && out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 5, End: 5}) {
					t.Fatal("before point followed later disable", out.Diagnostics)
				}
			}
		})
	}
}

func TestMD022LateInterruptionRetainsReliableSides(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, source := range []string{"Text\n# H\nText", "Text\n# H\nText\n# J\nText"} {
			calls := 0
			baseline := registry(t, []engine.Registration{{ID: "MD022", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				check := &md018LateContext{Context: ctx, reason: reason}
				out := headingBlankLines(check, c)
				calls = check.calls
				return out
			}}})
			full := analyzeHeadings(t, baseline, source, md022Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if !full.Complete || len(full.Diagnostics) < 2 {
				t.Fatal(full.Diagnostics)
			}
			// Discover stops from the deterministic caller checkpoint sequence and require
			// every possible retained prefix, including before-to-after on one heading.
			retained := map[int]bool{}
			for stop := 1; stop <= calls; stop++ {
				var observed engine.RuleOutcome
				var token parser.SourceToken
				r := registry(t, []engine.Registration{{ID: "MD022", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					token = c.Source().Token()
					observed = headingBlankLines(&md018LateContext{Context: ctx, reason: reason, stop: stop}, c)
					return observed
				}}})
				out := analyzeHeadings(t, r, source, md022Settings(), engine.DocumentContext{}, engine.RunOptions{})
				n := len(observed.Findings)
				if n == 0 {
					continue
				}
				if !errors.Is(observed.Err, reason) || out.Complete || len(out.Diagnostics) != n || len(out.Interruptions) != 1 || len(out.Coverage) == 0 || len(out.Errors) != 0 {
					t.Fatalf("stop %d: %+v %+v", stop, observed, out)
				}
				for i, f := range observed.Findings {
					if f.Span.SourceToken() != token || out.Diagnostics[i].ByteRange != full.Diagnostics[i].ByteRange || out.Diagnostics[i].Range != full.Diagnostics[i].Range {
						t.Fatal("prior point/source lost", f, out.Diagnostics)
					}
				}
				retained[n] = true
			}
			for n := 1; n < len(full.Diagnostics); n++ {
				if !retained[n] {
					t.Fatalf("never retained %d reliable sides", n)
				}
			}
		}
	}
}

func TestMD022MaximumIntegerAndLongBlankRuns(t *testing.T) {
	s := md022Settings()
	setting := s["MD022"]
	setting.Options = engine.Options{"before": int(^uint(0) >> 1), "after": int(^uint(0) >> 1)}
	s["MD022"] = setting
	source := "Text\n" + strings.Repeat("\n", 1024) + "# H\n" + strings.Repeat(" \t", 1024) + "\nText"
	out := analyzeHeadings(t, registry(t, Registrations()), source, s, engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 2 {
		t.Fatal("large minima overflowed or one-short accepted", out.Diagnostics)
	}
}
