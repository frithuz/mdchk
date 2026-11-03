package rules

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
)

type md034Literal struct {
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

func md034Literals(t *testing.T) []md034Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md034.json")
	if e != nil {
		t.Fatal(e)
	}
	var v struct{ Cases []md034Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) == 0 {
		t.Fatal(e)
	}
	return v.Cases
}
func md034Settings() engine.Settings {
	s := settings()
	for id := range s {
		off := false
		s[id] = engine.RuleSetting{Enabled: &off}
	}
	on := true
	s["MD034"] = engine.RuleSetting{Enabled: &on}
	return s
}
func TestMD034AnalysisFixtures(t *testing.T) {
	for _, tc := range md034Literals(t) {
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
				selection, e := registry(t, Registrations()).SelectRules(md034Settings(), engine.ValidationProviders{})
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
						if d.Rule != "MD034" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range != (result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}) || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}) || tc.Source[w.Bytes[0]:w.Bytes[1]] != w.Expected {
							t.Fatal(d, w)
						}
					}
					found := false
					for _, ev := range trace.Evaluations() {
						if ev.Rule == "MD034" {
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
func TestMD034AvailabilityDefaultsAndOptions(t *testing.T) {
	r := registry(t, Registrations())
	s, e := r.ResolveSettings(nil, engine.ValidationProviders{})
	if e != nil || !s["MD034"].Available || !s["MD034"].Enabled || len(s["MD034"].Options) != 0 {
		t.Fatal(s, e)
	}
	for _, meta := range r.Metadata() {
		if meta.ID == "MD034" && (meta.Fix != engine.PlannedFix || !meta.RequiresSyntax || len(meta.Options) != 0) {
			t.Fatal(meta)
		}
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, empty := range []bool{false, true} {
			settings := md034Settings()
			delete(settings, "MD034")
			if empty {
				settings["MD034"] = engine.RuleSetting{Options: engine.Options{}}
			}
			out := analyzeHeadings(t, r, "https://example.org", settings, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != 1 || len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(out)
			}
		}
	}
	for _, enabled := range []bool{false, true} {
		settings := md034Settings()
		settings["MD034"] = engine.RuleSetting{Enabled: &enabled, Options: engine.Options{"style": "auto"}}
		if _, e = r.ResolveSettings(settings, engine.ValidationProviders{}); e == nil {
			t.Fatal("unknown option accepted")
		}
	}
}
func TestMD034DirectivesAndInactiveObservation(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"suppressed", "denied", "inactive", "enable", "enable-denied", "protected"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				s := md034Settings()
				enabled := mode != "inactive" && mode != "enable" && mode != "enable-denied"
				allow := mode != "denied" && mode != "enable-denied"
				s["MD034"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow}
				source := "<!-- mdchk:disable MD034 -->\nhttps://example.org"
				if mode == "inactive" {
					source = "https://example.org"
				}
				if mode == "enable" || mode == "enable-denied" {
					source = "<!-- mdchk:enable MD034 -->\nhttps://example.org"
				}
				if mode == "protected" {
					source = "```md\n<!-- mdchk:disable MD034 -->\nhttps://example.org\n```\n\nhttps://example.org"
				}
				selection, e := registry(t, Registrations()).SelectRules(s, engine.ValidationProviders{})
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
						if v.Diagnostic.Rule == "MD034" {
							found++
							if v.Diagnostic.ByteRange != (result.ByteRange{Start: 0, End: 19}) {
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
func TestMD034UnknownContextWithholdsAllFindings(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		source := "https://example.org\n\n<div>\n<x title=\"\n\nhttps://later.example.org"
		out := analyzeHeadings(t, registry(t, Registrations()), source, md034Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
		if out.Complete || len(out.Diagnostics)+len(out.Errors) != 0 || len(out.Coverage) != 1 || out.Coverage[0].Reason != result.ContextUnavailable {
			t.Fatal(out)
		}
	}
}

func TestMD034LongProseWithinDefaultBudget(t *testing.T) {
	for _, tc := range []struct {
		flavor              string
		repeats, start, end int
	}{
		{"commonmark", 50000, 600001, 600020},
		// GFM's existing parser index uses one document record per byte;
		// keep this supplementary case within its default250k parse gate.
		{"gfm", 10000, 120001, 120020},
	} {
		flavor := tc.flavor
		prefix := strings.Repeat("plain prose ", tc.repeats)
		for _, violating := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/violating-%v", flavor, violating), func(t *testing.T) {
				source := prefix
				if violating {
					source += " https://example.org\n"
				}
				out := analyzeHeadings(t, registry(t, Registrations()), source, md034Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				want := 0
				if violating {
					want = 1
				}
				if !out.Complete || len(out.Diagnostics) != want || len(out.Errors)+len(out.Coverage)+len(out.Interruptions) != 0 {
					t.Fatal(out)
				}
				if violating && out.Diagnostics[0].ByteRange != (result.ByteRange{Start: tc.start, End: tc.end}) {
					t.Fatal(out.Diagnostics)
				}
			})
		}
	}
}
