package md042_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

type md042Literal struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Frontmatter  string
	Findings     []struct {
		Bytes, Start, End [2]int
		Expected, Message string
	}
}

func md042Literals(t *testing.T) []md042Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md042.json")
	if e != nil {
		t.Fatal(e)
	}
	var v struct{ Cases []md042Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) == 0 {
		t.Fatal(e)
	}
	return v.Cases
}
func md042Settings() engine.Settings {
	s := ruletest.Settings("MD047")
	for id := range s {
		off := false
		s[id] = engine.RuleSetting{Enabled: &off}
	}
	on := true
	s["MD042"] = engine.RuleSetting{Enabled: &on}
	return s
}
func TestMD042AnalysisFixtures(t *testing.T) {
	for _, tc := range md042Literals(t) {
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
					t.Fatal("source identity")
				}
				dc := engine.DocumentContext{Flavor: flavor}
				if tc.Frontmatter != "" {
					var e error
					dc.Frontmatter, e = parser.NewFrontmatterOptions(true, []string{tc.Frontmatter})
					if e != nil {
						t.Fatal(e)
					}
				}
				selection, e := ruletest.Registry(t, rules.Registrations()).SelectRules(md042Settings(), engine.ValidationProviders{})
				if e != nil {
					t.Fatal(e)
				}
				plan, e := selection.Plan(dc, nil)
				if e != nil {
					t.Fatal(e)
				}
				original := []byte(tc.Source)
				input, e := plan.NewInput("literal.md", original)
				if e != nil {
					t.Fatal(e)
				}
				var previous []result.Diagnostic
				for pass := 0; pass < 2; pass++ {
					out, trace, e := plan.Inspect(context.Background(), input, engine.RunOptions{}, engine.InspectionOptions{})
					if e != nil || !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes)+len(out.SuppressedDiagnostics) != 0 || len(out.Diagnostics) != len(tc.Findings) || string(original) != tc.Source || string(input.Source().Bytes()) != tc.Source {
						t.Fatal(out, e, tc.Findings)
					}
					if len(out.Files) != 1 || out.Files[0].WriteStatus != "not-requested" {
						t.Fatal(out.Files)
					}
					if pass == 1 && !reflect.DeepEqual(previous, out.Diagnostics) {
						t.Fatal("non-deterministic findings")
					}
					previous = out.Diagnostics
					for i, d := range out.Diagnostics {
						w := tc.Findings[i]
						if d.Rule != "MD042" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range != (result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}) || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}) || tc.Source[w.Bytes[0]:w.Bytes[1]] != w.Expected {
							t.Fatal(d, w)
						}
					}
					found := false
					for _, ev := range trace.Evaluations() {
						if ev.Rule == "MD042" {
							found = true
							if ev.Active != "complete" {
								t.Fatal(ev)
							}
						}
					}
					if !found {
						t.Fatal("evaluation missing")
					}
				}
			})
		}
	}
}
func TestMD042AvailabilityDefaultsAndOptions(t *testing.T) {
	r := ruletest.Registry(t, rules.Registrations())
	s, e := r.ResolveSettings(nil, engine.ValidationProviders{})
	if e != nil || !s["MD042"].Available || !s["MD042"].Enabled || len(s["MD042"].Options) != 0 {
		t.Fatal(s, e)
	}
	for _, meta := range r.Metadata() {
		if meta.ID == "MD042" && (meta.Fix != engine.NoFix || !meta.RequiresSyntax || len(meta.Options) != 0) {
			t.Fatal(meta)
		}
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, empty := range []bool{false, true} {
			settings := md042Settings()
			delete(settings, "MD042")
			if empty {
				settings["MD042"] = engine.RuleSetting{Options: engine.Options{}}
			}
			out := ruletest.AnalyzeDocument(t, r, "[x](#)", settings, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != 1 || len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(out)
			}
		}
	}
	for _, enabled := range []bool{false, true} {
		settings := md042Settings()
		settings["MD042"] = engine.RuleSetting{Enabled: &enabled, Options: engine.Options{"style": "auto"}}
		if _, e = r.ResolveSettings(settings, engine.ValidationProviders{}); e == nil {
			t.Fatal("unknown option accepted")
		}
	}
}
func TestMD042DirectivesAndInactiveObservation(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"suppressed", "denied", "inactive", "enable", "enable-denied", "protected"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				s := md042Settings()
				enabled := mode != "inactive" && mode != "enable" && mode != "enable-denied"
				allow := mode != "denied" && mode != "enable-denied"
				s["MD042"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow}
				source := "<!-- mdchk:disable MD042 -->\n[x](#)"
				if mode == "inactive" {
					source = "[x](#)"
				}
				if mode == "enable" || mode == "enable-denied" {
					source = "<!-- mdchk:enable MD042 -->\n[x](#)"
				}
				if mode == "protected" {
					source = "```md\n<!-- mdchk:disable MD042 -->\n[x](#)\n```\n\n[x](#)"
				}
				selection, e := ruletest.Registry(t, rules.Registrations()).SelectRules(s, engine.ValidationProviders{})
				if e != nil {
					t.Fatal(e)
				}
				plan, e := selection.Plan(engine.DocumentContext{Flavor: flavor}, nil)
				if e != nil {
					t.Fatal(e)
				}
				in, e := plan.NewInput("literal.md", []byte(source))
				if e != nil {
					t.Fatal(e)
				}
				out, trace, e := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{ObserveInactive: mode == "inactive"})
				wantVisible := 0
				if mode == "denied" || mode == "enable" || mode == "protected" {
					wantVisible = 1
				}
				if e != nil || !out.Complete || len(out.Diagnostics) != wantVisible || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents) != 0 {
					t.Fatal(out, e)
				}
				wantHidden := 0
				if mode == "suppressed" {
					wantHidden = 1
				}
				if len(out.SuppressedDiagnostics) != wantHidden {
					t.Fatal(out)
				}
				if mode == "inactive" {
					found := 0
					for _, v := range trace.Suppressed() {
						if v.Diagnostic.Rule == "MD042" {
							found++
							if v.Diagnostic.ByteRange != (result.ByteRange{Start: 4, End: 5}) {
								t.Fatal(v)
							}
						}
					}
					if found != 1 {
						t.Fatal(trace.Suppressed())
					}
				}
				if mode == "protected" && len(trace.Directives().Candidates()) != 0 {
					t.Fatal("code directive executed")
				}
			})
		}
	}
}
func TestMD042UnknownContextWithholdsAllFindings(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		source := "[x](#)\n\n<div>\n<x title=\"\n\n[later](#)"
		out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), source, md042Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
		if out.Complete || len(out.Diagnostics)+len(out.Errors) != 0 || len(out.Coverage) != 1 || out.Coverage[0].Reason != result.ContextUnavailable {
			t.Fatal(out)
		}
	}
}

func TestMD042LongComponentsWithinDefaultBudget(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, destination := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/destination-%v", flavor, destination), func(t *testing.T) {
				body := strings.Repeat("x", 200000)
				source := "[" + body + "](#)"
				want := 1
				if destination {
					source = "[x](" + body + ")"
					want = 0
				}
				out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), source, md042Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Diagnostics) != want || len(out.Errors)+len(out.Coverage)+len(out.Interruptions) != 0 {
					t.Fatal(out)
				}
			})
		}
	}
}

func TestMD042MissingDestinationSuppressionAudit(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, row := range []struct {
			name, source, reason   string
			visible, hidden, audit int
		}{
			{"after", "<!-- mdchk:disable MD042 -->\n[x]()", "suppression-used", 0, 1, 0},
			{"before", "[x]()\n<!-- mdchk:disable MD042 -->", "suppression-unused", 1, 0, 1},
			{"re-enabled", "<!-- mdchk:disable MD042 -->\n[x]()\n<!-- mdchk:enable MD042 -->\n[y]()", "suppression-used", 1, 1, 0},
		} {
			t.Run(flavor+"/"+row.name, func(t *testing.T) {
				r := ruletest.Registry(t, rules.Registrations())
				s := md042Settings()
				on := true
				s["MD087"] = engine.RuleSetting{Enabled: &on}
				in, e := engine.NewInput("point.md", []byte(row.source), s, engine.DocumentContext{Flavor: flavor})
				if e != nil {
					t.Fatal(e)
				}
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				out, e := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Budget: budget})
				if e != nil || !out.Complete || len(out.Diagnostics) != row.visible+row.audit || len(out.SuppressedDiagnostics) != row.hidden || len(out.Errors)+len(out.Coverage)+len(out.Changes) != 0 {
					t.Fatal(out, e)
				}
				visible, audit := 0, 0
				for _, d := range out.Diagnostics {
					if d.Rule == "MD042" {
						visible++
						if d.ByteRange.Start != d.ByteRange.End {
							t.Fatal(d)
						}
					} else if d.Rule == "MD087" {
						audit++
					} else {
						t.Fatal(d)
					}
				}
				if visible != row.visible || audit != row.audit {
					t.Fatal(out.Diagnostics)
				}
				decisions := 0
				for _, record := range out.AnalysisRecords() {
					if a, ok := record.(engine.DirectiveAnalysis); ok {
						ds, e := a.SuppressionDecisions(context.Background(), budget)
						if e != nil || len(ds) != 1 || ds[0].Rule != "MD042" || ds[0].Reason != row.reason {
							t.Fatal(ds, e)
						}
						decisions++
					}
				}
				if decisions != 1 {
					t.Fatal("audit record missing", decisions)
				}
			})
		}
	}
}
