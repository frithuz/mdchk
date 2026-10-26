package rules

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func md004Settings(marker string) engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	if marker == "" {
		marker = "consistent"
	}
	s["MD004"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"marker": marker}}
	return s
}
func TestMD004AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md004.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor  string
		Marker                string
		Recognize, Incomplete bool
		Expected              []struct {
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
				out := analyzeHeadings(t, r, f.Source, md004Settings(f.Marker), dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, md004Settings(f.Marker), dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) || out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(f.Expected) {
					t.Fatal("outcome", out.Complete, out.Diagnostics, out.Errors, out.Interruptions, "expected", f.Expected)
				}
				for i, w := range f.Expected {
					d := out.Diagnostics[i]
					got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if !reflect.DeepEqual(got, w.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source || d.Rule != "MD004" || d.FixAvailable || d.Message != w.Message {
						t.Fatal(got, d, w)
					}
				}
			})
		}
	}
}

func TestMD004CatalogOptionsAndDefaultActivation(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD004") || r.Available("MD007") || len(r.Metadata()) != 87 {
		t.Fatal("availability")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD004" && (!m.DefaultEnabled || m.Fix != engine.PlannedFix || !m.RequiresSyntax || m.UsesFrontmatter || len(m.Options) != 1) {
			t.Fatal(m)
		}
	}
	for _, enabled := range []bool{false, true} {
		for _, o := range []engine.Options{{"marker": nil}, {"marker": true}, {"marker": 1}, {"marker": []string{"dash"}}, {"marker": map[string]any{}}, {"marker": "Dash"}, {"marker": "wrong"}, {"unknown": false}} {
			if _, err := r.ResolveSettings(engine.Settings{"MD004": {Enabled: &enabled, Options: o}}, engine.ValidationProviders{}); err == nil {
				t.Fatal(o)
			}
		}
	}
	s := md004Settings("")
	delete(s, "MD004")
	if out := analyzeHeadings(t, r, "- one\n* two", s, engine.DocumentContext{}, engine.RunOptions{}); !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD004" {
		t.Fatal(out)
	}
}
func TestMD004DirectivesPreserveFirstBullet(t *testing.T) {
	for _, tc := range []struct {
		source           string
		enabled, allowed bool
		active, hidden   int
	}{
		{"<!-- mdchk:disable MD004 -->\n- first\n<!-- mdchk:enable MD004 -->\n* later", true, true, 1, 0},
		{"- first\n<!-- mdchk:enable MD004 -->\n* later", false, true, 1, 0},
		{"- first\n<!-- mdchk:disable MD004 -->\n* hidden\n<!-- mdchk:enable MD004 -->\n+ later", true, true, 1, 1},
		{"<!-- mdchk:disable MD004 -->\n- first\n* later", true, false, 1, 0},
		{"- first\n<!-- mdchk:enable MD004 -->\n* later", false, false, 0, 0},
	} {
		s := md004Settings("")
		s["MD004"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allowed}
		out := analyzeHeadings(t, registry(t, Registrations()), tc.source, s, engine.DocumentContext{}, engine.RunOptions{})
		if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
			t.Fatal(tc, out)
		}
	}
}
func TestMD004BaselineResetsAcrossInputs(t *testing.T) {
	r := registry(t, Registrations())
	a, _ := engine.NewInput("a.md", []byte("- one\n- two"), md004Settings(""), engine.DocumentContext{})
	b, _ := engine.NewInput("b.md", []byte("+ one\n+ two"), md004Settings(""), engine.DocumentContext{})
	for _, inputs := range [][]engine.Input{{a, b}, {b, a}} {
		out, err := r.Lint(context.Background(), inputs, engine.RunOptions{})
		if err != nil || !out.Complete || len(out.Diagnostics) != 0 {
			t.Fatal(out, err)
		}
	}
}
