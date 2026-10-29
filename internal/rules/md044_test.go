package rules

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func TestMD044OptionsAndAvailability(t *testing.T) {
	r := registry(t, Registrations())
	found := false
	for _, m := range r.Metadata() {
		if m.ID == "MD044" {
			found = true
			if m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.PlannedFix || len(m.Options) != 2 || m.Options["names"].Domain != engine.ConfiguredNameUTF8 || m.Options["code_blocks"].Default != false {
				t.Fatal(m)
			}
		}
	}
	if !found || !r.Available("MD044") {
		t.Fatal("missing executable MD044")
	}
	for _, enabled := range []bool{false, true} {
		for _, options := range []engine.Options{{"names": nil}, {"names": "GitHub"}, {"names": []any{"GitHub", 1}}, {"names": []string{string([]byte{255})}}, {"code_blocks": nil}, {"code_blocks": "true"}, {"unknown": true}} {
			if _, err := r.ResolveSettings(engine.Settings{"MD044": {Enabled: &enabled, Options: options}}, engine.ValidationProviders{}); err == nil {
				t.Fatal(enabled, options)
			}
		}
	}
	names := []string{"", " ", "C++", "x\x00Y", "new\nyork", " GitHub ", "GitHub", "Github", "GitHub"}
	resolved, err := r.ResolveSettings(engine.Settings{"MD044": {Options: engine.Options{"names": names}}}, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	expected := append([]string(nil), names...)
	names[0] = "changed"
	if !reflect.DeepEqual(resolved["MD044"].Options["names"], expected) {
		t.Fatal(resolved)
	}
	s := md044Settings([]string{"GitHub"}, false)
	off := false
	s["MD044"] = engine.RuleSetting{Enabled: &off, Options: engine.Options{"names": []string{"GitHub"}}}
	out := analyzeHeadings(t, r, "github", s, engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
	defaults, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil || defaults["MD044"].Enabled || !reflect.DeepEqual(defaults["MD044"].Options["names"], []string{}) {
		t.Fatal(defaults, err)
	}
	// Other string-list domains must retain their previous values, even invalid UTF8.
	if _, err = r.ResolveSettings(engine.Settings{"MD063": {Options: engine.Options{"proper_names": []string{string([]byte{255})}}}}, engine.ValidationProviders{}); err != nil {
		t.Fatal("unrelated domain narrowed", err)
	}
}

func TestMD044ProtectedAndNestedSource(t *testing.T) {
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	for _, tc := range []struct {
		name, source string
		code         bool
		names        []string
		ranges       [][2]int
	}{
		{"table-task", "| github | x |\n| --- | --- |\n| github | github |\n\n- [x] github", false, nil, [][2]int{{2, 8}, {31, 37}, {40, 46}, {56, 62}}},
		{"nested-code", "> - ~~~github\n>   github\n>   ~~~", true, nil, [][2]int{{18, 24}}},
		{"unclosed-code", "~~~\ngithub", true, nil, [][2]int{{4, 10}}},
		{"header-duplicate", "---\nx: github\nx: github\n---\ngithub", false, nil, [][2]int{{28, 34}}},
		{"header-invalid", "+++\nx = [github\n+++\ngithub", false, nil, [][2]int{{20, 26}}},
		{"normalized-nul", "git\x00hub github", false, nil, [][2]int{{8, 14}}},
		{"literal-replacement", "�", false, []string{"�"}, nil},
		{"entity-nonword-neighbor", "&#32;github&#32;", false, nil, [][2]int{{5, 11}}},
		{"entity-last-mark", "&NotEqualTilde;github", false, nil, nil},
		{"entity-many-scalars", "github&NotEqualTilde;", false, nil, [][2]int{{0, 6}}},
		{"escape-nonword", "github\\!", false, nil, [][2]int{{0, 6}}},
		{"tag-no-join", "git<a>hub</a> github", false, nil, [][2]int{{14, 20}}},
		{"link-no-join", "[git](x)hub github", false, nil, [][2]int{{12, 18}}},
		{"image-no-join", "git![hub](x) github", false, nil, [][2]int{{13, 19}}},
		{"raw-cross-block", "a <script>\n\ngithub\n\n</script>\n\ngithub", false, nil, [][2]int{{31, 37}}},
		{"raw-tail", "<script>\nx\n</script><style>\n\ngithub\n\n</style>\n\ngithub", false, nil, [][2]int{{47, 53}}},
		{"code-raw-does-not-leak", "~~~\n<script>github\n~~~\n\ngithub", true, nil, [][2]int{{12, 18}, {24, 30}}},
		{"literal-leading-space", " github ", false, []string{" GitHub "}, nil},
		{"literal-control-name", "a\x01b", false, []string{"A\x01B"}, [][2]int{{0, 3}}},
		{"no-cross-line-name", "new\nyork", false, []string{"New\nYork"}, nil},
		{"long-negative", strings.Repeat("a", 4096), false, []string{strings.Repeat("A", 4095) + "B"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			names := tc.names
			if names == nil {
				names = []string{"GitHub"}
			}
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, md044Settings(names, tc.code), engine.DocumentContext{Flavor: "gfm", Frontmatter: fm}, engine.RunOptions{})
			if !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(tc.ranges) {
				t.Fatal(out)
			}
			for i, d := range out.Diagnostics {
				w := tc.ranges[i]
				if [2]int{d.ByteRange.Start, d.ByteRange.End} != w || d.Range.Start != md038OriginalPosition(tc.source, w[0]) || d.Range.End != md038OriginalPosition(tc.source, w[1]) {
					t.Fatal(d, w)
				}
			}
		})
	}
}

func TestMD044UnavailableSyntaxAndUTF8(t *testing.T) {
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	out := analyzeHeadings(t, registry(t, Registrations()), "---\ngithub", md044Settings([]string{"GitHub"}, false), engine.DocumentContext{Frontmatter: fm}, engine.RunOptions{})
	if out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
	in, err := engine.NewInput("invalid.md", []byte{'g', 255}, md044Settings([]string{"GitHub"}, false), engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	out, err = registry(t, Registrations()).Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil || out.Complete || len(out.Diagnostics) != 0 || len(out.Errors) != 1 || out.Errors[0].Code != "invalid-utf8" {
		t.Fatal(out, err)
	}
	_, err = registry(t, Registrations()).ResolveSettings(engine.Settings{"MD044": {Options: engine.Options{"names": []string{"ok", string([]byte{255})}}}}, engine.ValidationProviders{})
	var detail *engine.SettingError
	if !errors.As(err, &detail) || detail.RuleID != "MD044" || detail.Option != "names" || detail.Element == nil || *detail.Element != 1 {
		t.Fatal(err)
	}
}
