package rules

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func md073Settings(options engine.Options) engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD073"] = engine.RuleSetting{Enabled: &on, Options: options}
	return s
}

func md073Context(t *testing.T, flavor, anchorStyle string, frontmatter bool) engine.DocumentContext {
	t.Helper()
	fm, err := parser.NewFrontmatterOptions(frontmatter, nil)
	if err != nil {
		t.Fatal(err)
	}
	return engine.DocumentContext{Flavor: flavor, AnchorStyle: anchorStyle, Frontmatter: fm}
}

func md073Coverage(out result.Result) []result.Coverage {
	var found []result.Coverage
	for _, c := range out.Coverage {
		if c.Rule == "MD073" {
			found = append(found, c)
		}
	}
	return found
}

func TestMD073AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md073.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, AnchorStyle string
		Options                   map[string]any
		Frontmatter, Incomplete   bool
		Expected                  []struct {
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
				options := engine.Options{}
				for k, v := range f.Options {
					if n, ok := v.(float64); ok {
						v = int(n)
					}
					options[k] = v
				}
				s := md073Settings(options)
				r := registry(t, Registrations())
				dc := md073Context(t, flavor, f.AnchorStyle, f.Frontmatter)
				out := analyzeHeadings(t, r, f.Source, s, dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, s, dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) {
					t.Fatal("nondeterministic analysis")
				}
				coverage := md073Coverage(out)
				if out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(f.Expected) || len(coverage) != len(out.Coverage) {
					t.Fatalf("complete=%v diag=%+v coverage=%+v errors=%v", out.Complete, out.Diagnostics, out.Coverage, out.Errors)
				}
				if f.Incomplete && (len(coverage) != 1 || coverage[0].Reason != result.ContextUnavailable || coverage[0].Message == "") {
					t.Fatal(coverage)
				}
				for i, w := range f.Expected {
					d := out.Diagnostics[i]
					if d.Rule != "MD073" || d.Message != w.Message || d.FixAvailable || d.ByteRange.Start != w.Start || d.ByteRange.End != w.End || d.Range.Start.Line != w.Line || d.Range.End.Line != w.Line || d.Range.Start.Column != w.Column || d.Range.End.Column != w.EndColumn {
						t.Fatalf("got %+v want %+v", d, w)
					}
				}
			})
		}
	}
}

func TestMD073CatalogAndOptions(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD073") {
		t.Fatal("availability")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD073" && (m.DefaultEnabled || m.UsesFrontmatter || !m.RequiresSyntax || m.Fix != engine.PlannedFix || len(m.Options) != 4 || m.Options["start_marker"].Default != "<!-- toc -->" || m.Options["end_marker"].Default != "<!-- /toc -->" || m.Options["min_level"].Default != 1 || m.Options["max_level"].Default != 6) {
			t.Fatal(m)
		}
	}
	for _, enabled := range []bool{false, true} {
		for _, options := range []engine.Options{{"start_marker": "toc"}, {"end_marker": "<!-- toc -->"}, {"min_level": 0}, {"max_level": 7}, {"min_level": 3, "max_level": 2}, {"min_level": "1"}, {"marker": "<!-- toc -->"}} {
			if _, err := r.ResolveSettings(engine.Settings{"MD073": {Enabled: &enabled, Options: options}}, engine.ValidationProviders{}); err == nil {
				t.Fatal("invalid option", options)
			}
		}
	}
	for _, mapping := range []map[string]any{{}, {"enabled": true}, {"allow_directives": true}, {"allow_directives": false}, {"start_marker": "<!-- contents -->", "end_marker": "<!-- /contents -->", "min_level": 2, "max_level": 2}} {
		v, err := engine.SettingFromMapping(mapping)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.ResolveSettings(engine.Settings{"MD073": v}, engine.ValidationProviders{}); err != nil {
			t.Fatal(err)
		}
	}
	// Left at its default, MD073 does not compare tables of contents.
	s := md073Settings(nil)
	delete(s, "MD073")
	out := analyzeHeadings(t, r, "<!-- toc -->\n- [Usage](#usage)\n<!-- /toc -->\n\n## Install\n", s, md073Context(t, "commonmark", "", false), engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
}

func TestMD073Directives(t *testing.T) {
	source := "<!-- mdchk:disable MD073 -->\n<!-- toc -->\n- [Usage](#usage)\n<!-- /toc -->\n\n## Install\n"
	for _, allowed := range []bool{false, true} {
		on := true
		s := md073Settings(nil)
		s["MD073"] = engine.RuleSetting{Enabled: &on, AllowDirectives: &allowed}
		out := analyzeHeadings(t, registry(t, Registrations()), source, s, md073Context(t, "gfm", "", false), engine.RunOptions{})
		if allowed && (len(out.Diagnostics) != 0 || len(out.SuppressedDiagnostics) != 1) {
			t.Fatal(allowed, out)
		}
		if !allowed && (len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD073" || len(out.SuppressedDiagnostics) != 0) {
			t.Fatal(allowed, out)
		}
	}
}
