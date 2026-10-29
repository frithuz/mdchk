package rules

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func md027Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD027"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{}}
	return s
}
func TestMD027AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md027.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor  string
		Recognize, Incomplete bool
		SHA256                string
		SizeBytes             int

		Expected []struct {
			Range   []int
			Source  string
			Message string
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	r := registry(t, Registrations())
	for _, f := range fixtures {
		for _, flavor := range []string{"commonmark", "gfm"} {
			if f.Flavor != "" && f.Flavor != flavor {
				continue
			}
			t.Run(f.Name+"/"+flavor, func(t *testing.T) {
				fm, _ := parser.NewFrontmatterOptions(f.Recognize, nil)
				dc := engine.DocumentContext{Flavor: flavor, Frontmatter: fm}
				setting := md027Settings()
				on := true
				setting["MD027"] = engine.RuleSetting{Enabled: &on}
				out := analyzeHeadings(t, r, f.Source, setting, dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, setting, dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) || out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(f.Expected) {
					t.Fatal("outcome", out.Complete, out.Diagnostics, out.Errors, out.Interruptions, "expected", f.Expected)
				}
				snapshot := out.Files[0].InputSnapshot
				if snapshot == nil || snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source))) || snapshot.SizeBytes != f.SizeBytes || snapshot.SHA256 != f.SHA256 {
					t.Fatal(out.Files)
				}
				uncertain := false
				for _, coverage := range out.Coverage {
					uncertain = uncertain || coverage.Reason == result.ParserUncertain
				}
				if uncertain != f.Incomplete || (!f.Incomplete && len(out.Coverage) != 0) {
					t.Fatal(out.Coverage)
				}
				for i, w := range f.Expected {
					d := out.Diagnostics[i]
					got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if !reflect.DeepEqual(got, w.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source || d.Rule != "MD027" || d.FixAvailable || d.Message != w.Message {
						t.Fatal(got, d, w)
					}
				}
			})
		}
	}
}
func TestMD027FinalPublicationWithholdsAllFindings(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 1}, {"clean", "a\nb", 1}, {"protected", "```\nx\n```", 1}, {"violation", ">  a\n>  a\n>  a\n>  a\n>  a\n", 15}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+reason.Error(), func(t *testing.T) {
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD027", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, err := c.BlockquoteSpaces(ctx)
					if err != nil {
						t.Fatal(err)
					}
					// Fixed independent counts: one check per item, optional
					// finding growth/emission, then the final publication check.
					late := &md018LateContext{Context: ctx, reason: reason, stop: tc.final}
					observed = compareBlockquoteSpaces(late, c, items)
					return observed
				}}})
				out := analyzeHeadings(t, r, tc.source, md027Settings(), engine.DocumentContext{}, engine.RunOptions{})
				if !errors.Is(observed.Err, reason) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
					t.Fatal(observed, out)
				}
			})
		}
	}
}

func TestMD027CallbackQuotaAndCopyBoundaries(t *testing.T) {
	for _, kind := range []string{"work", "nodes"} {
		for _, remaining := range []int64{0, 20, 21} {
			if kind == "nodes" && remaining == 20 {
				remaining = 16
			}
			if kind == "nodes" && remaining == 21 {
				remaining = 17
			}
			t.Run(fmt.Sprintf("%s/%d", kind, remaining), func(t *testing.T) {
				var used int64
				run := func(probe bool) (result.Result, engine.RuleOutcome) {
					budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
					var observed engine.RuleOutcome
					r := registry(t, []engine.Registration{{ID: "MD027", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						items, e := c.BlockquoteSpaces(ctx)
						if e != nil || len(items) != 5 {
							t.Fatal(items, e)
						}
						charge, limit := budget.ChargeWork, budget.Limits().WorkUnits
						if kind == "nodes" {
							charge, limit = budget.ChargeNodes, budget.Limits().TotalNodes
						}
						if probe {
							var stopped *parser.LimitExceeded
							if !errors.As(charge(limit), &stopped) {
								t.Fatal("probe must exhaust")
							}
							used = stopped.Observed - limit
							return engine.RuleOutcome{Err: stopped}
						}
						if e := charge(limit - used - remaining); e != nil {
							t.Fatal(e)
						}
						observed = compareBlockquoteSpaces(ctx, c, items)
						return observed
					}}})
					return analyzeHeadings(t, r, ">  a\n>  a\n>  a\n>  a\n>  a\n", md027Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget}), observed
				}
				run(true)
				out, observed := run(false)
				success := remaining == 21 || remaining == 17
				if success {
					if observed.Err != nil || len(observed.Findings) != 5 {
						t.Fatal(out, observed)
					}
				} else {
					var limit *parser.LimitExceeded
					if !errors.As(observed.Err, &limit) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 {
						t.Fatal("direct callback quota omitted", out, observed)
					}
				}
			})
		}
	}
}
func TestMD027CatalogStrictSettingsAndDefaults(t *testing.T) {
	r := registry(t, Registrations())
	enabled := 0
	for _, m := range r.Metadata() {
		if m.DefaultEnabled {
			enabled++
		}
		if m.ID == "MD027" && (!m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.PlannedFix || len(m.Options) != 0) {
			t.Fatal(m)
		}
	}
	if len(r.Metadata()) != 87 || enabled != 59 || !r.Available("MD027") || r.Available("MD028") {
		t.Fatal("catalog metadata")
	}
	for _, mapping := range []map[string]any{{}, {"enabled": true}, {"enabled": false}, {"allow_directives": true}, {"allow_directives": false}} {
		s, e := engine.SettingFromMapping(mapping)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = r.ResolveSettings(engine.Settings{"MD027": s}, engine.ValidationProviders{}); e != nil {
			t.Fatal(e)
		}
	}
	for _, mapping := range []map[string]any{nil, {"enabled": nil}, {"enabled": "true"}, {"allow_directives": 0}, {"allow_directives": nil}} {
		if _, e := engine.SettingFromMapping(mapping); e == nil {
			t.Fatal(mapping)
		}
	}
	for _, option := range []string{"unknown", "spaces", "spaces_per_tab", "prose", "code_blocks"} {
		off := false
		if _, e := r.ResolveSettings(engine.Settings{"MD027": {Enabled: &off, Options: engine.Options{option: true}}}, engine.ValidationProviders{}); e == nil {
			t.Fatal(option)
		}
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, explicit := range []bool{false, true} {
			s := md027Settings()
			if explicit {
				s["MD027"] = engine.RuleSetting{}
			} else {
				delete(s, "MD027")
			}
			out := analyzeHeadings(t, r, ">  text\n", s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != 1 {
				t.Fatal(out)
			}
		}
	}
}
func TestMD027DirectiveStartAndPermissions(t *testing.T) {
	r := registry(t, Registrations())
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source          string
			enabled, allow        bool
			active, hidden, start int
		}{
			{"late", ">  text <!-- mdchk:disable MD027 -->\n", true, true, 1, 0, 2},
			{"before", "<!-- mdchk:disable MD027 -->\n>  text\n", true, true, 0, 1, 31},
			{"enable", "<!-- mdchk:enable MD027 -->\n>  text\n", false, true, 1, 0, 30},
			{"denied", "<!-- mdchk:disable MD027 -->\n>  text\n", true, false, 1, 0, 31},
			{"denied-enable", "<!-- mdchk:enable MD027 -->\n>  text\n", false, false, 0, 0, 30},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				s := md027Settings()
				s["MD027"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allow}
				out := analyzeHeadings(t, r, tc.source, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
					t.Fatal(out)
				}
				if tc.active > 0 && (out.Diagnostics[0].ByteRange.Start != tc.start || out.Diagnostics[0].ByteRange.End != tc.start+1) {
					t.Fatal(out.Diagnostics)
				}
				if tc.hidden > 0 {
					var d result.SuppressedDiagnostic
					if e := json.Unmarshal(out.SuppressedDiagnostics[0], &d); e != nil {
						t.Fatal(e)
					}
					if d.Diagnostic.ByteRange.Start != tc.start {
						t.Fatal(d)
					}
				}
			})
		}
	}
}
