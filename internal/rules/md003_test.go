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

func md003Settings(style string) engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	option := engine.Options{}
	if style != "" {
		option["style"] = style
	}
	s["MD003"] = engine.RuleSetting{Enabled: &on, Options: option}
	return s
}

func TestMD003AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md003.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source          string
		Recognize, Incomplete bool
		Style                 string
		Expected              []struct {
			Range  []int
			Source string
			Style  string
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	r := registry(t, Registrations())
	for _, f := range fixtures {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(f.Name+"/"+flavor, func(t *testing.T) {
				fm, err := parser.NewFrontmatterOptions(f.Recognize, nil)
				if err != nil {
					t.Fatal(err)
				}
				dc := engine.DocumentContext{Flavor: flavor, Frontmatter: fm}
				out := analyzeHeadings(t, r, f.Source, md003Settings(f.Style), dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, md003Settings(f.Style), dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) {
					t.Fatal("nondeterministic analysis")
				}
				if out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(f.Expected) {
					t.Fatalf("unexpected outcome: %+v", out)
				}
				if f.Incomplete {
					found := false
					for _, coverage := range out.Coverage {
						if coverage.Reason == result.ParserUncertain {
							found = true
						}
					}
					if !found {
						t.Fatal(out.Coverage)
					}
				}
				for i, want := range f.Expected {
					d := out.Diagnostics[i]
					got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if !reflect.DeepEqual(got, want.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != want.Source || d.Rule != "MD003" || d.Message != "Heading syntax must be "+want.Style+"." {
						t.Fatalf("got %v %+v, want %+v", got, d, want)
					}
				}
			})
		}
	}
}

func TestMD003DirectivesPreserveFirstHeading(t *testing.T) {
	for _, tc := range []struct {
		name, source          string
		enabled, allowed      bool
		active, hidden, owner int
	}{
		{"disabled first", "<!-- mdchk:disable MD003 -->\n# First #\n<!-- mdchk:enable MD003 -->\n## Open\n", true, true, 1, 0, 0},
		{"configured off", "# First #\n<!-- mdchk:enable MD003 -->\n## Open\n", false, true, 1, 0, 0},
		{"suppressed mismatch retains baseline", "# First\n<!-- mdchk:disable MD003 -->\n## Wrong #\n<!-- mdchk:enable MD003 -->\n### Fine\n", true, true, 0, 1, 0},
		{"last disable owns", "# First\n<!-- mdchk:disable MD003 -->\n<!-- mdchk:disable MD003 -->\n## Wrong #\n<!-- mdchk:enable MD003 -->\n### Fine\n", true, true, 0, 1, 1},
		{"permission blocks enable", "# First #\n<!-- mdchk:enable MD003 -->\n## Open\n", false, false, 0, 0, 0},
		{"permission blocks disable", "# First\n<!-- mdchk:disable MD003 -->\n## Wrong #\n", true, false, 1, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := md003Settings("")
			s["MD003"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allowed}
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, s, engine.DocumentContext{}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
				t.Fatal(out)
			}
			if tc.hidden > 0 {
				trace := out.AnalysisRecords()[0].(engine.DirectiveAnalysis)
				if trace.Suppressed()[0].Owner != tc.owner || trace.Suppressed()[0].Diagnostic.Rule != "MD003" {
					t.Fatal(trace.Suppressed())
				}
			}
		})
	}
}

func TestMD003CatalogAndOptions(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD003") || !r.Available("MD001") || !r.Available("MD047") || r.Available("MD005") || len(r.Metadata()) != 87 {
		t.Fatal("incorrect catalog availability")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD003" && (!m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.PlannedFix || m.Options["style"].Default != "consistent") {
			t.Fatal(m)
		}
	}
	for _, mapping := range []map[string]any{{"style": nil}, {"style": true}, {"style": 1}, {"style": "ATX"}, {"style": "setext_with_atx"}, {"enabled": false, "style": "wrong"}, {"enabled": false, "unknown": true}, {"enabled": "false"}, {"allow_directives": nil}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD003": setting}, engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal("invalid options accepted", mapping)
		}
	}
	for _, style := range []string{"consistent", "atx", "atx_closed", "setext"} {
		for _, enabled := range []bool{false, true} {
			_, err := r.ResolveSettings(engine.Settings{"MD003": {Enabled: &enabled, Options: engine.Options{"style": style}}}, engine.ValidationProviders{})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestMD003AddedWorkInterruptions(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		var observed error
		r := registry(t, []engine.Registration{{ID: "MD003", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			out := headingStyle(&traversalContext{Context: ctx, err: reason}, c)
			observed = out.Err
			return out
		}}})
		out := analyzeHeadings(t, r, "# A\n## B #\n", md003Settings(""), engine.DocumentContext{}, engine.RunOptions{})
		if !errors.Is(observed, reason) || out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "analysis" || len(out.Errors) != 0 {
			t.Fatal(observed, out)
		}
	}
	for _, kind := range []string{"work", "nodes"} {
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		r := registry(t, []engine.Registration{{ID: "MD003", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			if kind == "work" {
				_ = budget.ChargeWork(budget.Limits().WorkUnits)
			} else {
				_ = budget.ChargeNodes(budget.Limits().TotalNodes)
			}
			return headingStyle(ctx, c)
		}}})
		out := analyzeHeadings(t, r, "# A\n## B #\n", md003Settings(""), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
		if out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "analysis" || out.Interruptions[0].Limit == nil || len(out.Errors) != 0 {
			t.Fatal(out)
		}
	}
}
