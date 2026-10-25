package rules

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func md018Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD018"] = engine.RuleSetting{Enabled: &on}
	return s
}

func TestMD018AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md018.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source          string
		Recognize, Incomplete bool
		Expected, GFMExpected [][]int
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	r := registry(t, Registrations())
	for _, f := range fixtures {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(f.Name+"/"+flavor, func(t *testing.T) {
				fm, _ := parser.NewFrontmatterOptions(f.Recognize, nil)
				dc := engine.DocumentContext{Flavor: flavor, Frontmatter: fm}
				out := analyzeHeadings(t, r, f.Source, md018Settings(), dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, md018Settings(), dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) {
					t.Fatal("nondeterministic analysis")
				}
				want := f.Expected
				if flavor == "gfm" && f.GFMExpected != nil {
					want = f.GFMExpected
				}
				if out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(want) {
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
				for i, point := range want {
					d := out.Diagnostics[i]
					if d.ByteRange != (result.ByteRange{Start: point[0], End: point[0]}) || d.Range.Start != (result.Position{Line: point[1], Column: point[2]}) || d.Range.End != d.Range.Start || d.Rule != "MD018" || d.Message != "Opening hashes must be followed by a space." {
						t.Fatalf("got %+v, want %v", d, point)
					}
				}
			})
		}
	}
}

func TestMD018Directives(t *testing.T) {
	for _, tc := range []struct {
		name, source          string
		enabled, allowed      bool
		active, hidden, owner int
	}{
		{"last disable", "<!-- mdchk:disable MD018 -->\n<!-- mdchk:disable MD018 -->\n##hidden\n<!-- mdchk:enable MD018 -->\n##active", true, true, 1, 1, 1},
		{"configured off", "##off\n<!-- mdchk:enable MD018 -->\n##active", false, true, 1, 1, -1},
		{"off throughout", "##off", false, true, 0, 0, 0},
		{"disable blocked", "<!-- mdchk:disable MD018 -->\n##active", true, false, 1, 0, 0},
		{"enable blocked", "<!-- mdchk:enable MD018 -->\n##off", false, false, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := md018Settings()
			s["MD018"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allowed}
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, s, engine.DocumentContext{}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
				t.Fatal(out)
			}
			if tc.hidden > 0 {
				point := 60
				if tc.owner == -1 {
					point = 2
				}
				trace := out.AnalysisRecords()[0].(engine.DirectiveAnalysis)
				if trace.Suppressed()[0].Owner != tc.owner || trace.Suppressed()[0].Diagnostic.Rule != "MD018" || trace.Suppressed()[0].Diagnostic.ByteRange != (result.ByteRange{Start: point, End: point}) {
					t.Fatal(trace.Suppressed())
				}
			}
		})
	}
}

func TestMD018CatalogAndOptions(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD018") || r.Available("MD022") || len(r.Metadata()) != 87 {
		t.Fatal("incorrect availability")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD018" && (!m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.NoFix || len(m.Options) != 0) {
			t.Fatal(m)
		}
	}
	for _, mapping := range []map[string]any{{"unknown": true}, {"enabled": false, "unknown": true}, {"enabled": "false"}, {"allow_directives": nil}, {"enabled": false, "allow_directives": 1}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD018": setting}, engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal("invalid options accepted", mapping)
		}
	}
	for _, mapping := range []map[string]any{{}, {"enabled": false}, {"enabled": true, "allow_directives": false}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.ResolveSettings(engine.Settings{"MD018": setting}, engine.ValidationProviders{}); err != nil {
			t.Fatal(err)
		}
	}
	// Omission uses the catalog default, independently of explicit admission.
	s := md018Settings()
	delete(s, "MD018")
	out := analyzeHeadings(t, r, "##default", s, engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 1 {
		t.Fatal(out)
	}
}

func TestMD018AddedWorkInterruptions(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		var observed error
		r := registry(t, []engine.Registration{{ID: "MD018", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			out := openingHashSpacing(&traversalContext{Context: ctx, err: reason}, c)
			observed = out.Err
			return out
		}}})
		out := analyzeHeadings(t, r, "##bad", md018Settings(), engine.DocumentContext{}, engine.RunOptions{})
		if !errors.Is(observed, reason) || out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "analysis" || len(out.Errors) != 0 {
			t.Fatal(observed, out)
		}
	}
	for _, kind := range []string{"work", "nodes"} {
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		r := registry(t, []engine.Registration{{ID: "MD018", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			if kind == "work" {
				_ = budget.ChargeWork(budget.Limits().WorkUnits)
			} else {
				_ = budget.ChargeNodes(budget.Limits().TotalNodes)
			}
			return openingHashSpacing(ctx, c)
		}}})
		out := analyzeHeadings(t, r, "##bad", md018Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
		if out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "analysis" || out.Interruptions[0].Limit == nil || len(out.Errors) != 0 {
			t.Fatal(out)
		}
	}
}

// Count the actual production callback's context checks so the stop follows a
// completed inventory and the first emission, without fixing a parser work count.
type md018LateContext struct {
	context.Context
	calls, stop int
	reason      error
}

func (c *md018LateContext) Err() error {
	c.calls++
	if c.stop > 0 && c.calls >= c.stop {
		return c.reason
	}
	return c.Context.Err()
}

func TestMD018LateInterruptionRetainsObservedFinding(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		err          error
	}{
		{"cancellation", "cancelled", context.Canceled},
		{"deadline", "deadline-exceeded", context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			for _, late := range []bool{false, true} {
				var observed engine.RuleOutcome
				var token parser.SourceToken
				r := registry(t, []engine.Registration{{ID: "MD018", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					check := &md018LateContext{Context: ctx, reason: tc.err}
					if late {
						check.stop = calls
					}
					token = c.Source().Token()
					observed = openingHashSpacing(check, c)
					if !late {
						calls = check.calls
					}
					return observed
				}}})
				out := analyzeHeadings(t, r, "##first\n##second", md018Settings(), engine.DocumentContext{}, engine.RunOptions{})
				if !late {
					if observed.Err != nil || len(observed.Findings) != 2 || !out.Complete || len(out.Diagnostics) != 2 || calls == 0 {
						t.Fatalf("baseline did not observe both candidates: callback=%+v result=%+v", observed, out)
					}
					continue
				}
				if !errors.Is(observed.Err, tc.err) || len(observed.Findings) != 1 || len(out.Diagnostics) != 1 {
					t.Fatalf("reliable first finding lost: callback=%+v diagnostics=%+v checks=%d", observed, out.Diagnostics, calls)
				}
				span := observed.Findings[0].Span
				if span.SourceToken() != token || span.Start() != 2 || span.End() != 2 {
					t.Fatal("prior finding lost original source point", span)
				}
				d := out.Diagnostics[0]
				if d.Rule != "MD018" || d.ByteRange != (result.ByteRange{Start: 2, End: 2}) || d.Range.Start != (result.Position{Line: 1, Column: 3}) || d.Range.End != d.Range.Start {
					t.Fatal("partial result changed original point", d)
				}
				if out.Complete || out.Status != "incomplete" || len(out.Errors) != 0 || len(out.Interruptions) != 1 || out.Interruptions[0].Reason != tc.reason || out.Interruptions[0].Phase != "analysis" || len(out.Coverage) == 0 {
					t.Fatalf("partial finding lost interruption/coverage: %+v", out)
				}
			}
		})
	}
}
