package rules

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func md038Settings() engine.Settings {
	s := engine.Settings{}
	off := false
	for _, m := range engine.Catalog() {
		s[m.ID] = engine.RuleSetting{Enabled: &off}
	}
	delete(s, "MD038")
	return s
}

func TestMD038DefaultsSettingsAndAvailability(t *testing.T) {
	r := registry(t, Registrations())
	executable := false
	for _, registration := range Registrations() {
		executable = executable || registration.ID == "MD038" && registration.Check != nil
	}
	if !executable {
		t.Fatal("MD038 executable registration missing")
	}
	on, off := 0, 0
	for _, m := range r.Metadata() {
		if m.DefaultEnabled {
			on++
		} else {
			off++
		}
		if m.ID == "MD038" && (!m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.PlannedFix || len(m.Options) != 0) {
			t.Fatal(m)
		}
	}
	if on != 59 || off != 28 || !r.Available("MD038") {
		t.Fatal(on, off)
	}
	for _, enabled := range []bool{true, false} {
		if _, err := r.ResolveSettings(engine.Settings{"MD038": {Enabled: &enabled, Options: engine.Options{"unknown": true}}}, engine.ValidationProviders{}); err == nil {
			t.Fatal("unknown accepted")
		}
	}
	for _, m := range []map[string]any{nil, {"enabled": nil}, {"enabled": "false"}, {"allow_directives": 0}} {
		if _, err := engine.SettingFromMapping(m); err == nil {
			t.Fatal(m)
		}
	}
	for _, setting := range []engine.RuleSetting{{}, {Enabled: func() *bool { b := false; return &b }()}} {
		s := md038Settings()
		s["MD038"] = setting
		out := analyzeHeadings(t, r, "` x `", s, engine.DocumentContext{}, engine.RunOptions{})
		expected := 2
		if setting.Enabled != nil {
			expected = 0
		}
		if !out.Complete || len(out.Diagnostics) != expected {
			t.Fatal(out)
		}
	}
	in, _ := engine.NewInput("implicit.md", []byte("` x `"), nil, engine.DocumentContext{})
	out, err := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil || out.Complete {
		t.Fatal(out, err)
	}
	unrelated, found := false, false
	for _, c := range out.Coverage {
		if c.Rule == "MD038" {
			t.Fatal(c)
		}
		unrelated = unrelated || c.Reason == result.RuleUnavailable
	}
	for _, d := range out.Diagnostics {
		found = found || d.Rule == "MD038"
	}
	if !unrelated || !found {
		t.Fatal(out)
	}
}

func TestMD038OpaqueHeadersAndDirectives(t *testing.T) {
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	for _, source := range []string{"\ufeff---\nx: '` x `'\n---\n` x `", "+++\nx = '` x `'\n+++\n` x `"} {
		out := analyzeHeadings(t, registry(t, Registrations()), source, md038Settings(), engine.DocumentContext{Frontmatter: fm}, engine.RunOptions{})
		if !out.Complete || len(out.Diagnostics) != 2 {
			t.Fatal(out)
		}
	}
	out := analyzeHeadings(t, registry(t, Registrations()), "---\n` x `", md038Settings(), engine.DocumentContext{Frontmatter: fm}, engine.RunOptions{})
	if out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
	for _, tc := range []struct {
		name, source string
		denied, off  bool
		want         int
	}{
		{"disable-enable", "<!-- mdchk:disable MD038 -->\n` x `\n<!-- mdchk:enable MD038 -->\n` y `", false, false, 2},
		{"denied", "<!-- mdchk:disable MD038 -->\n` x `", true, false, 2},
		{"off-then-enable", "` x `\n<!-- mdchk:enable MD038 -->\n` y `", false, true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := md038Settings()
			enabled, allow := !tc.off, !tc.denied
			s["MD038"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow}
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, s, engine.DocumentContext{}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.want {
				t.Fatal(out)
			}
		})
	}
}

func TestMD038DisabledAndUnselectedHeaderOwnership(t *testing.T) {
	source := "---\n` x `\n---\n` y `"
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
			out := analyzeHeadings(t, registry(t, Registrations()), source, md038Settings(), engine.DocumentContext{Frontmatter: tc.fm}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.want {
				t.Fatal(out)
			}
		})
	}
}

func TestMD038RawLifetimesAndOriginalEdges(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		ranges       [][2]int
	}{
		{"raw-cross-owner", "A <script>\n\n` x `\n\n</script>\n\n` y `", [][2]int{{31, 32}, {33, 34}}},
		{"raw-tail-new-owner", "<script>\nx\n</script><style>\n\n` x `\n\n</style>\n\n` y `", [][2]int{{47, 48}, {49, 50}}},
		{"image-local-raw", "![<script>` x `](pic) ` y `", [][2]int{{23, 24}, {25, 26}}},
		{"partial-tab-prefix", ">\t` x `", [][2]int{{3, 4}, {5, 6}}},
		{"html-block-splits-span", "` x\n<!-- mdchk:disable MD038 -->\ny `", nil},
		{"inline-directive-content", "` x\nz <!-- mdchk:disable MD038 -->\ny `", [][2]int{{1, 2}, {36, 37}}},
	} {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(tc.name+"/"+flavor, func(t *testing.T) {
				out := analyzeHeadings(t, registry(t, Registrations()), tc.source, md038Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Diagnostics) != len(tc.ranges) || len(out.SuppressedDiagnostics) != 0 {
					t.Fatal(out)
				}
				for i, d := range out.Diagnostics {
					if [2]int{d.ByteRange.Start, d.ByteRange.End} != tc.ranges[i] {
						t.Fatal(d, tc.ranges)
					}
				}
			})
		}
	}
}
