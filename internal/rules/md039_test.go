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

type md039Literal struct {
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

func md039Literals(t *testing.T) []md039Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md039.json")
	if e != nil {
		t.Fatal(e)
	}
	var v struct{ Cases []md039Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) == 0 {
		t.Fatal(e)
	}
	return v.Cases
}
func md039Settings() engine.Settings {
	s := settings()
	for id := range s {
		off := false
		s[id] = engine.RuleSetting{Enabled: &off}
	}
	on := true
	s["MD039"] = engine.RuleSetting{Enabled: &on}
	return s
}
func TestMD039AnalysisFixtures(t *testing.T) {
	for _, tc := range md039Literals(t) {
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
				selection, e := registry(t, Registrations()).SelectRules(md039Settings(), engine.ValidationProviders{})
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
						if d.Rule != "MD039" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range != (result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}) || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}) || tc.Source[w.Bytes[0]:w.Bytes[1]] != w.Expected {
							t.Fatal(d, w)
						}
					}
					found := false
					for _, ev := range trace.Evaluations() {
						if ev.Rule == "MD039" {
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
func TestMD039AvailabilityDefaultsAndOptions(t *testing.T) {
	r := registry(t, Registrations())
	s, e := r.ResolveSettings(nil, engine.ValidationProviders{})
	if e != nil || !s["MD039"].Available || !s["MD039"].Enabled || len(s["MD039"].Options) != 0 {
		t.Fatal(s, e)
	}
	for _, meta := range r.Metadata() {
		if meta.ID == "MD039" && (meta.Fix != engine.NoFix || !meta.RequiresSyntax || len(meta.Options) != 0) {
			t.Fatal(meta)
		}
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, empty := range []bool{false, true} {
			settings := md039Settings()
			delete(settings, "MD039")
			if empty {
				settings["MD039"] = engine.RuleSetting{Options: engine.Options{}}
			}
			out := analyzeHeadings(t, r, "[ x](y)", settings, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != 1 || len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(out)
			}
		}
	}
	for _, enabled := range []bool{false, true} {
		settings := md039Settings()
		settings["MD039"] = engine.RuleSetting{Enabled: &enabled, Options: engine.Options{"style": "auto"}}
		if _, e = r.ResolveSettings(settings, engine.ValidationProviders{}); e == nil {
			t.Fatal("unknown option accepted")
		}
	}
}
func TestMD039DirectivesAndInactiveObservation(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"suppressed", "denied", "inactive", "enable", "enable-denied", "protected"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				s := md039Settings()
				enabled := mode != "inactive" && mode != "enable" && mode != "enable-denied"
				allow := mode != "denied" && mode != "enable-denied"
				s["MD039"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow}
				source := "<!-- mdchk:disable MD039 -->\n[ x](y)"
				if mode == "inactive" {
					source = "[ x](y)"
				}
				if mode == "enable" || mode == "enable-denied" {
					source = "<!-- mdchk:enable MD039 -->\n[ x](y)"
				}
				if mode == "protected" {
					source = "```md\n<!-- mdchk:disable MD039 -->\n[ x](y)\n```\n\n[ x](y)"
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
				// Observing the inactive MD057 needs the link target to exist.
				resources := newNativeFragments(t, map[string]string{"docs/y": ""}, flavor, "literal.md", in.Source())
				out, trace, e := plan.Inspect(context.Background(), in, engine.RunOptions{Fragments: resources}, engine.InspectionOptions{ObserveInactive: mode == "inactive"})
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
						if v.Diagnostic.Rule == "MD039" {
							found++
							if v.Diagnostic.ByteRange != (result.ByteRange{Start: 1, End: 2}) {
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
func TestMD039UnknownContextWithholdsAllFindings(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		source := "[ x](y)\n\n<div>\n<x title=\"\n\n[ later](y)"
		out := analyzeHeadings(t, registry(t, Registrations()), source, md039Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
		if out.Complete || len(out.Diagnostics)+len(out.Errors) != 0 || len(out.Coverage) != 1 || out.Coverage[0].Reason != result.ContextUnavailable {
			t.Fatal(out)
		}
	}
}

func TestMD039LongLabelsWithinDefaultBudget(t *testing.T) {
	for _, tc := range []struct {
		flavor string
		count  int
	}{{"commonmark", 200000}, {"gfm", 20000}} {
		for _, allWhitespace := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/all-whitespace-%v", tc.flavor, allWhitespace), func(t *testing.T) {
				body := strings.Repeat("x", tc.count)
				if allWhitespace {
					body = strings.Repeat(" ", tc.count)
				}
				source := "[ " + body + " ](y)"
				out := analyzeHeadings(t, registry(t, Registrations()), source, md039Settings(), engine.DocumentContext{Flavor: tc.flavor}, engine.RunOptions{})
				want := 2
				if allWhitespace {
					want = 1
				}
				if !out.Complete || len(out.Diagnostics) != want || len(out.Errors)+len(out.Coverage)+len(out.Interruptions) != 0 {
					t.Fatal(out)
				}
				if allWhitespace && out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 1, End: tc.count + 3}) {
					t.Fatal(out.Diagnostics)
				}
			})
		}
	}
}
