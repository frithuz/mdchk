package rules

import (
	"context"
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

func md035Settings() engine.Settings {
	s := engine.Settings{}
	off := false
	for _, m := range engine.Catalog() {
		s[m.ID] = engine.RuleSetting{Enabled: &off}
	}
	delete(s, "MD035") // Actual default ON; no helper silently re-enables it.
	return s
}

func TestMD035AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md035.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Style, SHA256, Flavor string
		Size                                int `json:"size_bytes"`
		Expected                            []struct {
			Range  []int
			Source string
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		for _, flavor := range []string{"commonmark", "gfm"} {
			if f.Flavor != "" && f.Flavor != flavor {
				continue
			}
			t.Run(f.Name+"/"+flavor, func(t *testing.T) {
				if len(f.Source) != f.Size || fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source))) != f.SHA256 {
					t.Fatal("independent literal source identity")
				}
				s := md035Settings()
				s["MD035"] = engine.RuleSetting{Options: engine.Options{"style": f.Style}}
				out := analyzeHeadings(t, registry(t, Registrations()), f.Source, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(f.Expected) || len(out.Changes) != 0 {
					t.Fatal(out)
				}
				for i, w := range f.Expected {
					d := out.Diagnostics[i]
					got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if !reflect.DeepEqual(got, w.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source || d.Rule != "MD035" || d.Message != "Horizontal rule marker style does not match the expected style." || d.FixAvailable {
						t.Fatal(got, d, w)
					}
				}
			})
		}
	}
}

func TestMD035DefaultAvailabilityAndStrictSettings(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD035") || len(r.Metadata()) != 87 {
		t.Fatal("catalog")
	}
	on, off := 0, 0
	for _, m := range r.Metadata() {
		if m.DefaultEnabled {
			on++
		} else {
			off++
		}
		if m.ID == "MD035" && (!m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.PlannedFix || len(m.Options) != 1 || m.Options["style"].Default != "consistent") {
			t.Fatal(m)
		}
	}
	if on != 59 || off != 28 {
		t.Fatal(on, off)
	}
	for _, enabled := range []bool{false, true} {
		for _, style := range []string{"consistent", "dash", "asterisk", "underscore"} {
			if _, err := r.ResolveSettings(engine.Settings{"MD035": {Enabled: &enabled, Options: engine.Options{"style": style}}}, engine.ValidationProviders{}); err != nil {
				t.Fatal(err)
			}
		}
		for _, v := range []any{nil, "", "Dash", "dash ", "unknown", 1, true, []string{"dash"}, map[string]any{}} {
			if _, err := r.ResolveSettings(engine.Settings{"MD035": {Enabled: &enabled, Options: engine.Options{"style": v}}}, engine.ValidationProviders{}); err == nil {
				t.Fatal(v)
			}
		}
		if _, err := r.ResolveSettings(engine.Settings{"MD035": {Enabled: &enabled, Options: engine.Options{"markers": 3}}}, engine.ValidationProviders{}); err == nil {
			t.Fatal("unknown option")
		}
	}
	for _, m := range []map[string]any{nil, {"enabled": "false"}, {"allow_directives": 0}} {
		if _, err := engine.SettingFromMapping(m); err == nil {
			t.Fatal(m)
		}
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		// Preserve actual implicit-default coverage. Other unavailable ON checks
		// remain truthful; only MD035's unavailable deficit must disappear.
		in, err := engine.NewInput("implicit.md", []byte("---\n\n***\n"), nil, engine.DocumentContext{Flavor: flavor})
		if err != nil {
			t.Fatal(err)
		}
		out, err := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
		if err != nil || out.Complete {
			t.Fatal(out, err)
		}
		unrelated, found := false, false
		for _, c := range out.Coverage {
			if c.Rule == "MD035" {
				t.Fatal(c)
			}
			if c.Reason == result.RuleUnavailable {
				unrelated = true
			}
		}
		for _, d := range out.Diagnostics {
			if d.Rule == "MD035" {
				found = true
			}
		}
		if !unrelated || !found {
			t.Fatal(out)
		}
		// Omitted and empty mappings exercise actual enabled/default consistent.
		for _, empty := range []bool{false, true} {
			s := md035Settings()
			if empty {
				s["MD035"] = engine.RuleSetting{}
			}
			out := analyzeHeadings(t, r, "---\n\n***", s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != 1 {
				t.Fatal(out)
			}
		}
	}
}

func TestMD035RawContextAndProtectedSyntax(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source, style string
			want                [][2]int
		}{
			{"raw-global", "text<script>\n\n***\n\n</script>\n\n___", "consistent", nil},
			{"raw-family", "text<script>\n\n***\n\n</script>\n\n___", "dash", [][2]int{{30, 33}}},
			{"tail", "<script>\nx\n</script><style>\n\n***\n\n</style>\n\n___", "dash", [][2]int{{44, 47}}},
			{"image-open", "![<script>](x)\n\n***", "dash", [][2]int{{16, 19}}},
			{"image-close", "text<script>\n\n![</script>](x)\n\n***", "dash", nil},
			{"nested-global", "- item\n\n  * * *\n\n> ___\n\n---", "consistent", [][2]int{{19, 22}, {24, 27}}},
			{"ownership", "heading\n---\n\n- item\n\n    ***\n\n```\n___\n```\n\n** x\n\n---", "consistent", [][2]int{{49, 52}}},
			{"root-code", "    ***\n\n** x\n\n---", "consistent", nil},
			{"mixed", "---\r\n\r***\n\n___", "consistent", [][2]int{{6, 9}, {11, 14}}},
		} {
			t.Run(tc.name+"/"+flavor, func(t *testing.T) {
				s := md035Settings()
				s["MD035"] = engine.RuleSetting{Options: engine.Options{"style": tc.style}}
				out := analyzeHeadings(t, registry(t, Registrations()), tc.source, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Diagnostics) != len(tc.want) {
					t.Fatal(out)
				}
				for i, w := range tc.want {
					d := out.Diagnostics[i]
					if d.ByteRange.Start != w[0] || d.ByteRange.End != w[1] {
						t.Fatal(d, w)
					}
				}
			})
		}
	}
}

func TestMD035OpaqueHeadersAndIncompleteCoverage(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source string
			enabled      bool
			formats      []string
			incomplete   bool
			count        int
		}{
			{"yaml", "\ufeff---\nbad: [\n---\n\n***\n\n___", true, nil, false, 1},
			{"toml", "\ufeff+++\nbad = [\n+++\n\n***\n\n___", true, nil, false, 1},
			{"yaml-off", "---\n\n***", false, nil, false, 1},
			{"yaml-unselected", "---\n\n***", true, []string{"toml"}, false, 1},
			{"toml-unselected", "+++\n\n***\n\n___", true, []string{"yaml"}, false, 1},
			{"yaml-incomplete", "---\n***", true, nil, true, 0},
			{"toml-incomplete", "+++\n***", true, nil, true, 0},
		} {
			t.Run(tc.name+"/"+flavor, func(t *testing.T) {
				fm, err := parser.NewFrontmatterOptions(tc.enabled, tc.formats)
				if err != nil {
					t.Fatal(err)
				}
				out := analyzeHeadings(t, registry(t, Registrations()), tc.source, md035Settings(), engine.DocumentContext{Flavor: flavor, Frontmatter: fm}, engine.RunOptions{})
				if out.Complete == tc.incomplete || len(out.Diagnostics) != tc.count || len(out.Errors) != 0 {
					t.Fatal(out)
				}
				if tc.incomplete {
					found := false
					for _, c := range out.Coverage {
						if (c.Rule == "MD035" || c.Rule == "") && c.Reason == result.ParserUncertain {
							found = true
						}
					}
					if !found {
						t.Fatal(out)
					}
				}
			})
		}
	}
}
