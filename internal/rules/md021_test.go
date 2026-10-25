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

func md021Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD021"] = engine.RuleSetting{Enabled: &on}
	return s
}

func TestMD021AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md021.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source          string
		Recognize, Incomplete bool
		Expected              []struct {
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
				out := analyzeHeadings(t, r, f.Source, md021Settings(), dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, md021Settings(), dc, engine.RunOptions{})
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
					if !reflect.DeepEqual(got, want.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != want.Source || d.Rule != "MD021" || d.Message != "Closed heading delimiter gaps must contain exactly one ASCII space." {
						t.Fatalf("got %v %+v, want %+v", got, d, want)
					}
				}
			})
		}
	}
}

func TestMD021Directives(t *testing.T) {
	for _, tc := range []struct {
		name, source          string
		enabled, allowed      bool
		active, hidden, owner int
	}{
		{"last disable", "<!-- mdchk:disable MD021 -->\n<!-- mdchk:disable MD021 -->\n##  hidden #\n<!-- mdchk:enable MD021 -->\n##  active #", true, true, 1, 1, 1},
		{"configured off", "##  off #\n<!-- mdchk:enable MD021 -->\n##  active #", false, true, 1, 1, -1},
		{"off throughout", "##  off #", false, true, 0, 0, 0},
		{"disable blocked", "<!-- mdchk:disable MD021 -->\n##  active #", true, false, 1, 0, 0},
		{"enable blocked", "<!-- mdchk:enable MD021 -->\n##  off #", false, false, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := md021Settings()
			s["MD021"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allowed}
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, s, engine.DocumentContext{}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
				t.Fatal(out)
			}
			if tc.hidden > 0 {
				point := 61
				if tc.owner == -1 {
					point = 3
				}
				trace := out.AnalysisRecords()[0].(engine.DirectiveAnalysis)
				if trace.Suppressed()[0].Owner != tc.owner || trace.Suppressed()[0].Diagnostic.Rule != "MD021" || trace.Suppressed()[0].Diagnostic.ByteRange != (result.ByteRange{Start: point, End: point + 1}) {
					t.Fatal(trace.Suppressed())
				}
			}
		})
	}
}

func TestMD021CatalogAndOptions(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD021") || r.Available("MD023") || len(r.Metadata()) != 87 {
		t.Fatal("incorrect availability")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD021" && (!m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.PlannedFix || len(m.Options) != 0) {
			t.Fatal(m)
		}
	}
	for _, mapping := range []map[string]any{{"unknown": true}, {"enabled": false, "unknown": true}, {"enabled": "false"}, {"allow_directives": nil}, {"enabled": false, "allow_directives": 1}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD021": setting}, engine.ValidationProviders{})
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
		if _, err = r.ResolveSettings(engine.Settings{"MD021": setting}, engine.ValidationProviders{}); err != nil {
			t.Fatal(err)
		}
	}
	// Omission uses the catalog default, independently of explicit admission.
	s := md021Settings()
	delete(s, "MD021")
	out := analyzeHeadings(t, r, "##  default #", s, engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 1 {
		t.Fatal(out)
	}
}

func TestMD021AddedWorkInterruptions(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		var observed error
		r := registry(t, []engine.Registration{{ID: "MD021", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			out := closedHeadingSpaces(&traversalContext{Context: ctx, err: reason}, c)
			observed = out.Err
			return out
		}}})
		out := analyzeHeadings(t, r, "##  bad #", md021Settings(), engine.DocumentContext{}, engine.RunOptions{})
		if !errors.Is(observed, reason) || out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "analysis" || len(out.Errors) != 0 {
			t.Fatal(observed, out)
		}
	}
	for _, kind := range []string{"work", "nodes"} {
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		r := registry(t, []engine.Registration{{ID: "MD021", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			if kind == "work" {
				_ = budget.ChargeWork(budget.Limits().WorkUnits)
			} else {
				_ = budget.ChargeNodes(budget.Limits().TotalNodes)
			}
			return closedHeadingSpaces(ctx, c)
		}}})
		out := analyzeHeadings(t, r, "##  bad #", md021Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
		if out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "analysis" || out.Interruptions[0].Limit == nil || len(out.Errors) != 0 {
			t.Fatal(out)
		}
	}
}

func TestMD021LateInterruptionRetainsObservedFinding(t *testing.T) {
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
				r := registry(t, []engine.Registration{{ID: "MD021", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					check := &md018LateContext{Context: ctx, reason: tc.err}
					if late {
						check.stop = calls - 3
					}
					token = c.Source().Token()
					observed = closedHeadingSpaces(check, c)
					if !late {
						calls = check.calls
					}
					return observed
				}}})
				out := analyzeHeadings(t, r, "##  first #\n##  second #", md021Settings(), engine.DocumentContext{}, engine.RunOptions{})
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
				if span.SourceToken() != token || span.Start() != 3 || span.End() != 4 {
					t.Fatal("prior finding lost original source point", span)
				}
				d := out.Diagnostics[0]
				if d.Rule != "MD021" || d.ByteRange != (result.ByteRange{Start: 3, End: 4}) || d.Range.Start != (result.Position{Line: 1, Column: 4}) || d.Range.End != (result.Position{Line: 1, Column: 5}) {
					t.Fatal("partial result changed original point", d)
				}
				if out.Complete || out.Status != "incomplete" || len(out.Errors) != 0 || len(out.Interruptions) != 1 || out.Interruptions[0].Reason != tc.reason || out.Interruptions[0].Phase != "analysis" || len(out.Coverage) == 0 {
					t.Fatalf("partial finding lost interruption/coverage: %+v", out)
				}
			}
		})
	}
}

func TestMD021IndependentMD019Overlap(t *testing.T) {
	for _, source := range []string{"#  x #", "<!-- mdchk:disable MD019 -->\n#  x #", "<!-- mdchk:disable MD021 -->\n#  x #"} {
		for _, enabled := range []string{"both", "MD019", "MD021"} {
			s := md021Settings()
			on, off := true, false
			s["MD019"] = engine.RuleSetting{Enabled: &on}
			if enabled == "MD019" {
				s["MD021"] = engine.RuleSetting{Enabled: &off}
			} else if enabled == "MD021" {
				s["MD019"] = engine.RuleSetting{Enabled: &off}
			}
			out := analyzeHeadings(t, registry(t, Registrations()), source, s, engine.DocumentContext{}, engine.RunOptions{})
			counts := map[string]int{}
			for _, d := range out.Diagnostics {
				counts[d.Rule]++
			}
			for _, id := range []string{"MD019", "MD021"} {
				want := 1
				if enabled != "both" && enabled != id || strings.Contains(source, "disable "+id) {
					want = 0
				}
				if counts[id] != want || !out.Complete {
					t.Fatalf("independent overlap %s/%s: %+v", enabled, id, out)
				}
			}
		}
	}
}

func TestMD021SameHeadingLateStopRetainsOpening(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		calls := 0
		for _, late := range []bool{false, true} {
			var observed engine.RuleOutcome
			r := registry(t, []engine.Registration{{ID: "MD021", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				check := &md018LateContext{Context: ctx, reason: reason}
				if late {
					check.stop = calls
				}
				observed = closedHeadingSpaces(check, c)
				if !late {
					calls = check.calls
				}
				return observed
			}}})
			out := analyzeHeadings(t, r, "#  x  #", md021Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if !late {
				if len(observed.Findings) != 2 || observed.Err != nil {
					t.Fatal(observed)
				}
				continue
			}
			if !errors.Is(observed.Err, reason) || len(observed.Findings) != 1 || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 2, End: 3}) || out.Complete || len(out.Interruptions) != 1 || len(out.Coverage) == 0 {
				t.Fatalf("same-heading prior finding lost: %+v %+v", observed, out)
			}
		}
	}
}

func TestMD021LongGapCheckpoint(t *testing.T) {
	source := "# " + strings.Repeat("\t", 1024) + " x #"
	out := analyzeHeadings(t, registry(t, Registrations()), source, md021Settings(), engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 1026, End: 1027}) {
		t.Fatal(out)
	}
}

func TestMD021StartAnchorWithinHeading(t *testing.T) {
	for _, source := range []string{"#\t<!-- mdchk:disable MD021 -->\t#", "#  <!-- mdchk:disable MD021 -->  #"} {
		out := analyzeHeadings(t, registry(t, Registrations()), source, md021Settings(), engine.DocumentContext{}, engine.RunOptions{})
		if !out.Complete || len(out.Diagnostics) != 1 || len(out.SuppressedDiagnostics) != 1 {
			t.Fatal(out)
		}
		want := 1
		if source[1] == ' ' {
			want = 2
		}
		if out.Diagnostics[0].ByteRange.Start != want {
			t.Fatal("opening anchor followed later disable", out)
		}
		var hidden result.SuppressedDiagnostic
		if err := json.Unmarshal(out.SuppressedDiagnostics[0], &hidden); err != nil || hidden.Diagnostic.ByteRange.Start <= out.Diagnostics[0].ByteRange.Start {
			t.Fatal("closing anchor ignored disable", hidden, err)
		}
	}
	out := analyzeHeadings(t, registry(t, Registrations()), "# `<!-- mdchk:disable MD021 -->` #\n#  x #", md021Settings(), engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 1 || len(out.SuppressedDiagnostics) != 0 {
		t.Fatal("protected fake directive changed state", out)
	}
}
