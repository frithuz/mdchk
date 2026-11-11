package md072_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

func md072Settings(recursive, caseSensitive bool) engine.Settings {
	s := ruletest.Settings("MD072")
	on := true
	s["MD072"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"recursive": recursive, "case_sensitive": caseSensitive}}
	return s
}

func md072Context(t *testing.T, flavor string, recognize bool, formats []string) engine.DocumentContext {
	t.Helper()
	fm, err := parser.NewFrontmatterOptions(recognize, formats)
	if err != nil {
		t.Fatal(err)
	}
	return engine.DocumentContext{Flavor: flavor, Frontmatter: fm}
}

func md072Coverage(out result.Result) []result.Coverage {
	var found []result.Coverage
	for _, c := range out.Coverage {
		if c.Rule == "MD072" {
			found = append(found, c)
		}
	}
	return found
}

func TestMD072AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md072.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source                                           string
		Formats                                                []string
		Disabled, Recursive, Insensitive, Incomplete, Unparsed bool
		Expected                                               []struct {
			Start, End, Line, Column, EndColumn int
			Message                             string
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(f.Name+"/"+flavor, func(t *testing.T) {
				s := md072Settings(f.Recursive, !f.Insensitive)
				r := ruletest.Registry(t, rules.Registrations())
				dc := md072Context(t, flavor, !f.Disabled, f.Formats)
				out := ruletest.AnalyzeDocument(t, r, f.Source, s, dc, engine.RunOptions{})
				again := ruletest.AnalyzeDocument(t, r, f.Source, s, dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) {
					t.Fatal("nondeterministic analysis")
				}
				coverage := md072Coverage(out)
				if f.Unparsed {
					// An unterminated header leaves syntax unreliable, so the engine
					// withholds every syntax rule, MD072 included, at document level.
					coverage = out.Coverage
				}
				if out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(f.Expected) || len(coverage) != len(out.Coverage) {
					t.Fatalf("complete=%v diag=%+v coverage=%+v errors=%v", out.Complete, out.Diagnostics, out.Coverage, out.Errors)
				}
				if f.Incomplete && !f.Unparsed && (len(coverage) != 1 || coverage[0].Reason != result.ContextUnavailable) {
					t.Fatal(coverage)
				}
				for _, c := range coverage {
					if c.Message == "" {
						t.Fatal(coverage)
					}
				}
				for i, w := range f.Expected {
					d := out.Diagnostics[i]
					if d.Rule != "MD072" || d.Message != w.Message || d.FixAvailable || d.ByteRange.Start != w.Start || d.ByteRange.End != w.End || d.Range.Start.Line != w.Line || d.Range.End.Line != w.Line || d.Range.Start.Column != w.Column || d.Range.End.Column != w.EndColumn {
						t.Fatalf("got %+v want %+v", d, w)
					}
				}
			})
		}
	}
}

func TestMD072CatalogAndOptions(t *testing.T) {
	r := ruletest.Registry(t, rules.Registrations())
	if !r.Available("MD072") {
		t.Fatal("availability")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD072" && (m.DefaultEnabled || !m.UsesFrontmatter || !m.RequiresSyntax || m.Fix != engine.PlannedFix || len(m.Options) != 2 || m.Options["recursive"].Default != false || m.Options["case_sensitive"].Default != true) {
			t.Fatal(m)
		}
	}
	for _, enabled := range []bool{false, true} {
		for _, options := range []engine.Options{{"recursive": "yes"}, {"case_sensitive": 1}, {"recursive": nil}, {"order": "asc"}} {
			if _, err := r.ResolveSettings(engine.Settings{"MD072": {Enabled: &enabled, Options: options}}, engine.ValidationProviders{}); err == nil {
				t.Fatal("invalid option", options)
			}
		}
	}
	for _, mapping := range []map[string]any{{}, {"enabled": true}, {"allow_directives": true}, {"allow_directives": false}, {"recursive": true, "case_sensitive": false}} {
		v, err := engine.SettingFromMapping(mapping)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.ResolveSettings(engine.Settings{"MD072": v}, engine.ValidationProviders{}); err != nil {
			t.Fatal(err)
		}
	}
	// Left at its default, MD072 does not report out-of-order keys.
	s := md072Settings(false, true)
	delete(s, "MD072")
	out := ruletest.AnalyzeDocument(t, r, "---\nz: 1\na: 2\n---\n", s, md072Context(t, "commonmark", true, nil), engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
}

func TestMD072BodyDirectiveCannotReachHeader(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		on := true
		s := md072Settings(false, true)
		s["MD072"] = engine.RuleSetting{Enabled: &on, AllowDirectives: &allowed}
		out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), "---\nz: 1\na: 2\n---\n<!-- mdchk:disable MD072 -->\n", s, md072Context(t, "gfm", true, nil), engine.RunOptions{})
		if len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD072" || len(out.SuppressedDiagnostics) != 0 {
			t.Fatal(allowed, out)
		}
	}
}

func TestMD072MetadataCoverageExplainsTheFirstProblem(t *testing.T) {
	merge := "---\nbase: &b {y: 1, x: 2}\nz: 1\nm:\n  <<: *b\na: 2\n---\n"
	type location struct{ line, column, endLine, endColumn int }
	for _, tc := range []struct {
		name, source, message string
		recursive             bool
		at                    *location
	}{
		{"malformed", "---\na: [\n---\n", "Metadata is malformed; the affected key ordering checks were withheld.", false, &location{2, 1, 3, 1}},
		{"merge key", merge, "Metadata uses an unsupported structure such as a merge key, custom tag or cycle; the affected key ordering checks were withheld.", false, &location{5, 3, 5, 5}},
		{"custom tag", "---\nz: !foo 1\na: 2\n---\n", "Metadata uses an unsupported structure such as a merge key, custom tag or cycle; the affected key ordering checks were withheld.", false, &location{2, 4, 2, 8}},
		{"duplicate key", "---\nz: 1\nz: 2\na: 3\n---\n", "Metadata contains a duplicate key; the affected key ordering checks were withheld.", false, &location{3, 1, 3, 2}},
		{"first of several", "---\nb: !foo 1\na: 1\na: 2\n---\n", "Metadata uses an unsupported structure such as a merge key, custom tag or cycle; the affected key ordering checks were withheld.", false, &location{2, 4, 2, 8}},
		{"no authored key", "+++\n[z.x]\nk = 1\n[a]\nk = 2\n+++\n", "A metadata key has no authored source location; its key ordering check was withheld.", true, nil},
	} {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(tc.name+"/"+flavor, func(t *testing.T) {
				out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), tc.source, md072Settings(tc.recursive, true), md072Context(t, flavor, true, nil), engine.RunOptions{})
				coverage := md072Coverage(out)
				if len(coverage) != 1 || coverage[0].Message != tc.message || coverage[0].Reason != result.ContextUnavailable || out.Complete {
					t.Fatalf("%+v", coverage)
				}
				if tc.at == nil {
					if coverage[0].Range != nil || coverage[0].Snapshot != nil {
						t.Fatalf("%+v", coverage[0])
					}
					return
				}
				r := coverage[0].Range
				if r == nil || coverage[0].Snapshot == nil || *coverage[0].Snapshot != *out.Files[0].InputSnapshot {
					t.Fatalf("%+v", coverage[0])
				}
				if (location{r.Start.Line, r.Start.Column, r.End.Line, r.End.Column}) != *tc.at {
					t.Fatalf("%+v", r)
				}
			})
		}
	}
}

func TestMD072ReliableFindingsStandBesideExplainedCoverage(t *testing.T) {
	out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), "---\nb:\n  z: 1\n  a: 2\nc: !foo 1\n---\n", md072Settings(true, true), md072Context(t, "commonmark", true, nil), engine.RunOptions{})
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 16, End: 17}) || len(md072Coverage(out)) != 1 || md072Coverage(out)[0].Range == nil {
		t.Fatalf("%+v %+v", out.Diagnostics, out.Coverage)
	}
}

func TestMD072CoverageLocatesAProblemInsideAScalar(t *testing.T) {
	out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), "+++\n中A = [\n+++\n", md072Settings(false, true), md072Context(t, "commonmark", true, nil), engine.RunOptions{})
	coverage := md072Coverage(out)
	if len(coverage) != 1 || coverage[0].Message != "Metadata is malformed; the affected key ordering checks were withheld." || coverage[0].Range == nil || coverage[0].Range.Start.Line != 2 {
		t.Fatalf("%+v", coverage)
	}
}
