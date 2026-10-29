package rules

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func md028Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD028"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{}}
	return s
}
func TestMD028AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md028.json")
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
				setting := md028Settings()
				on := true
				setting["MD028"] = engine.RuleSetting{Enabled: &on}
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
					if !reflect.DeepEqual(got, w.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source || d.Rule != "MD028" || d.FixAvailable || d.Message != w.Message {
						t.Fatal(got, d, w)
					}
				}
			})
		}
	}
}
func TestMD028CatalogStrictSettingsAndDefaults(t *testing.T) {
	r := registry(t, Registrations())
	enabled := 0
	for _, m := range r.Metadata() {
		if m.DefaultEnabled {
			enabled++
		}
		if m.ID == "MD028" && (!m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.NoFix || len(m.Options) != 0) {
			t.Fatal(m)
		}
	}
	if len(r.Metadata()) != 87 || enabled != 59 || !r.Available("MD028") || r.Available("MD031") {
		t.Fatal("catalog metadata")
	}
	for _, mapping := range []map[string]any{{}, {"enabled": true}, {"enabled": false}, {"allow_directives": true}, {"allow_directives": false}} {
		s, e := engine.SettingFromMapping(mapping)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = r.ResolveSettings(engine.Settings{"MD028": s}, engine.ValidationProviders{}); e != nil {
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
		if _, e := r.ResolveSettings(engine.Settings{"MD028": {Enabled: &off, Options: engine.Options{option: true}}}, engine.ValidationProviders{}); e == nil {
			t.Fatal(option)
		}
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, explicit := range []bool{false, true} {
			s := md028Settings()
			if explicit {
				s["MD028"] = engine.RuleSetting{}
			} else {
				delete(s, "MD028")
			}
			out := analyzeHeadings(t, r, "> A\n\n> B\n", s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != 1 {
				t.Fatal(out)
			}
		}
	}
}
func TestMD028DirectiveStartAndPermissions(t *testing.T) {
	r := registry(t, Registrations())
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source          string
			enabled, allow        bool
			active, hidden, start int
		}{
			{"late", "> A\n\n> B <!-- mdchk:disable MD028 -->\n", true, true, 1, 0, 4},
			{"before", "<!-- mdchk:disable MD028 -->\n> A\n\n> B\n", true, true, 0, 1, 33},
			{"enable", "<!-- mdchk:enable MD028 -->\n> A\n\n> B\n", false, true, 1, 0, 32},
			{"denied", "<!-- mdchk:disable MD028 -->\n> A\n\n> B\n", true, false, 1, 0, 33},
			{"denied-enable", "<!-- mdchk:enable MD028 -->\n> A\n\n> B\n", false, false, 0, 0, 32},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				s := md028Settings()
				s["MD028"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allow}
				out := analyzeHeadings(t, r, tc.source, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
					t.Fatal(out)
				}
				if tc.active > 0 && (out.Diagnostics[0].ByteRange.Start != tc.start || out.Diagnostics[0].ByteRange.End != tc.start) {
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
