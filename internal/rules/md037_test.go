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

func md037Settings() engine.Settings {
	s := engine.Settings{}
	off := false
	for _, m := range engine.Catalog() {
		s[m.ID] = engine.RuleSetting{Enabled: &off}
	}
	delete(s, "MD037")
	return s
}

func TestMD037FrozenAnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md037.json")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != "5dec9f990fdae4a01bf1b685793936d828be03bbe2abe3ec0939f8803dd9295b" {
		t.Fatal("frozen oracle identity")
	}
	var fixtures struct {
		Cases []struct {
			Name, Source string
			SHA          string `json:"source_sha256"`
			Ranges       [][2]int
			Text         []string `json:"range_text"`
			Positions    []struct{ Start, End [2]int }
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures.Cases) != 42 {
		t.Fatal(len(fixtures.Cases))
	}
	for _, f := range fixtures.Cases {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(f.Name+"/"+flavor, func(t *testing.T) {
				if fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source))) != f.SHA {
					t.Fatal("literal identity")
				}
				out := analyzeHeadings(t, registry(t, Registrations()), f.Source, md037Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(f.Ranges) || len(out.Changes) != 0 {
					t.Fatal(out)
				}
				for i, d := range out.Diagnostics {
					if [2]int{d.ByteRange.Start, d.ByteRange.End} != f.Ranges[i] || [2]int{d.Range.Start.Line, d.Range.Start.Column} != f.Positions[i].Start || [2]int{d.Range.End.Line, d.Range.End.Column} != f.Positions[i].End || f.Source[d.ByteRange.Start:d.ByteRange.End] != f.Text[i] || d.Rule != "MD037" || d.Message != "Whitespace within emphasis delimiters." || d.FixAvailable {
						t.Fatal(d, f)
					}
				}
			})
		}
	}
}

func TestMD037PolicyBoundaries(t *testing.T) {
	// Source-authored literals before their first execution. Expected offsets
	// select original spaces, never parser-produced or observed diagnostic ranges.
	for _, tc := range []struct {
		name, source string
		ranges       [][2]int
		gfm          bool
	}{
		{"raw-entity-before", "a&#x301;* text *", [][2]int{{9, 10}, {14, 15}}, false},
		{"raw-entity-after", "A * text *&#x61;", [][2]int{{3, 4}, {8, 9}}, false},
		{"neutral-underscore", "A * foo_bar *", [][2]int{{3, 4}, {11, 12}}, false},
		{"long-poison-before-neutral", "A * a___b *", nil, false},
		{"neutral-star-payload", "A * a*b *", [][2]int{{3, 4}, {7, 8}}, false},
		{"ls-semantic", "A * \u2028 *", [][2]int{{3, 4}, {7, 8}}, false},
		{"vt-barrier", "A * \vword *", nil, false},
		{"nel-barrier", "A * \u0085word *", nil, false},
		{"nul-barrier", "A * \x00word *", nil, false},
		{"replacement-valid", "A * \ufffd *", [][2]int{{3, 4}, {7, 8}}, false},
		{"generated-blank-edge", "A *&#32;x *", [][2]int{{9, 10}}, false},
		{"link-label", "[A * x *](u)", [][2]int{{4, 5}, {6, 7}}, false},
		{"image-alt", "![A * x *](u)", [][2]int{{5, 6}, {7, 8}}, false},
		{"image-raw-isolated", "![<script>](u) A * x *", [][2]int{{18, 19}, {20, 21}}, false},
		{"raw-cross-owner", "A <script>\n\nA * x *\n\n</script>\n\nA * y *", [][2]int{{35, 36}, {37, 38}}, false},
		{"setext", "A * x *\n===", [][2]int{{3, 4}, {5, 6}}, false},
		{"table-cell", "| a | b |\n| - | - |\n| A * x * | a\\|b |", [][2]int{{25, 26}, {27, 28}}, true},
		{"table-escaped-payload", "| a | b |\n| - | - |\n| A * a\\|b * | c |", [][2]int{{25, 26}, {30, 31}}, true},
		{"raw-tail-new-owner", "<script>\nx\n</script><style>\n\nA * x *\n\n</style>\n\nA * y *", [][2]int{{51, 52}, {53, 54}}, false},
		{"punctuation", "A «* x *»", [][2]int{{5, 6}, {7, 8}}, false},
		{"mark-outside", "A * x *\u0301", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flavor := "commonmark"
			if tc.gfm {
				flavor = "gfm"
			}
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, md037Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			var got [][2]int
			for _, d := range out.Diagnostics {
				got = append(got, [2]int{d.ByteRange.Start, d.ByteRange.End})
			}
			if !out.Complete || !reflect.DeepEqual(got, tc.ranges) {
				t.Fatal(got, tc.ranges, out)
			}
		})
	}
}

func TestMD037DefaultsSettingsAndAvailability(t *testing.T) {
	r := registry(t, Registrations())
	on, off := 0, 0
	for _, m := range r.Metadata() {
		if m.DefaultEnabled {
			on++
		} else {
			off++
		}
		if m.ID == "MD037" && (!m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.NoFix || len(m.Options) != 0) {
			t.Fatal(m)
		}
	}
	if on != 59 || off != 28 || !r.Available("MD037") {
		t.Fatal(on, off)
	}
	for _, enabled := range []bool{true, false} {
		if _, err := r.ResolveSettings(engine.Settings{"MD037": {Enabled: &enabled, Options: engine.Options{"unknown": true}}}, engine.ValidationProviders{}); err == nil {
			t.Fatal("unknown accepted")
		}
	}
	for _, m := range []map[string]any{nil, {"enabled": nil}, {"enabled": "false"}, {"allow_directives": 0}} {
		if _, err := engine.SettingFromMapping(m); err == nil {
			t.Fatal(m)
		}
	}
	for _, setting := range []engine.RuleSetting{{}, {Enabled: func() *bool { b := false; return &b }()}} {
		s := md037Settings()
		s["MD037"] = setting
		out := analyzeHeadings(t, r, "A * x *", s, engine.DocumentContext{}, engine.RunOptions{})
		expected := 2
		if setting.Enabled != nil {
			expected = 0
		}
		if !out.Complete || len(out.Diagnostics) != expected {
			t.Fatal(out)
		}
	}
	in, _ := engine.NewInput("implicit.md", []byte("A * x *"), nil, engine.DocumentContext{})
	out, err := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil || out.Complete {
		t.Fatal(out, err)
	}
	unrelated, found := false, false
	for _, c := range out.Coverage {
		if c.Rule == "MD037" {
			t.Fatal(c)
		}
		unrelated = unrelated || c.Reason == result.RuleUnavailable
	}
	for _, d := range out.Diagnostics {
		found = found || d.Rule == "MD037"
	}
	if !unrelated || !found {
		t.Fatal(out)
	}
}

func TestMD037OpaqueHeadersAndDirectives(t *testing.T) {
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	for _, source := range []string{"\ufeff---\nx: '* x *'\n---\nA * x *", "+++\nx = '* x *'\n+++\nA * x *"} {
		out := analyzeHeadings(t, registry(t, Registrations()), source, md037Settings(), engine.DocumentContext{Frontmatter: fm}, engine.RunOptions{})
		if !out.Complete || len(out.Diagnostics) != 2 {
			t.Fatal(out)
		}
	}
	out := analyzeHeadings(t, registry(t, Registrations()), "---\nA * x *", md037Settings(), engine.DocumentContext{Frontmatter: fm}, engine.RunOptions{})
	if out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
	for _, tc := range []struct {
		name, source string
		denied, off  bool
		want         int
	}{
		{"disable-enable", "<!-- mdchk:disable MD037 -->\nA * x *\n<!-- mdchk:enable MD037 -->\nA * y *", false, false, 2},
		{"denied", "<!-- mdchk:disable MD037 -->\nA * x *", true, false, 2},
		{"off-then-enable", "A * x *\n<!-- mdchk:enable MD037 -->\nA * y *", false, true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := md037Settings()
			enabled, allow := !tc.off, !tc.denied
			s["MD037"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow}
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, s, engine.DocumentContext{}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.want {
				t.Fatal(out)
			}
		})
	}
}

func TestMD037DisabledAndUnselectedHeaderOwnership(t *testing.T) {
	source := "---\nA * x *\n---\nA * y *"
	for _, tc := range []struct {
		name string
		fm   parser.FrontmatterOptions
		want int
	}{
		{"disabled", parser.FrontmatterOptions{}, 4},
		{"unselected", func() parser.FrontmatterOptions {
			f, _ := parser.NewFrontmatterOptions(true, []string{"toml"})
			return f
		}(), 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := analyzeHeadings(t, registry(t, Registrations()), source, md037Settings(), engine.DocumentContext{Frontmatter: tc.fm}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.want {
				t.Fatal(out)
			}
		})
	}
}
