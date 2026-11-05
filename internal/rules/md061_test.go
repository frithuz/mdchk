package rules

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func TestMD061OptionsAndAvailability(t *testing.T) {
	r := registry(t, Registrations())
	found := false
	for _, m := range r.Metadata() {
		if m.ID == "MD061" {
			found = true
			if m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.NoFix || len(m.Options) != 2 || m.Options["terms"].Domain != engine.ConfiguredNameUTF8 || m.Options["case_sensitive"].Default != false {
				t.Fatal(m)
			}
		}
	}
	if !found || !r.Available("MD061") {
		t.Fatal("missing executable MD061")
	}
	for _, enabled := range []bool{false, true} {
		for _, options := range []engine.Options{{"terms": nil}, {"terms": "blacklist"}, {"terms": []any{"blacklist", 1}}, {"terms": []string{string([]byte{255})}}, {"case_sensitive": nil}, {"case_sensitive": "true"}, {"unknown": true}} {
			if _, err := r.ResolveSettings(engine.Settings{"MD061": {Enabled: &enabled, Options: options}}, engine.ValidationProviders{}); err == nil {
				t.Fatal(enabled, options)
			}
		}
	}
	terms := []string{"", " ", "blacklist", "Blacklist", "blacklist"}
	resolved, err := r.ResolveSettings(engine.Settings{"MD061": {Options: engine.Options{"terms": terms, "case_sensitive": true}}}, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	expected := append([]string(nil), terms...)
	terms[0] = "changed"
	if !reflect.DeepEqual(resolved["MD061"].Options["terms"], expected) || resolved["MD061"].Options["case_sensitive"] != true {
		t.Fatal(resolved)
	}
	s := md061Settings([]string{"blacklist"}, false)
	off := false
	s["MD061"] = engine.RuleSetting{Enabled: &off, Options: engine.Options{"terms": []string{"blacklist"}}}
	out := analyzeHeadings(t, r, "blacklist", s, engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
	defaults, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil || defaults["MD061"].Enabled || !reflect.DeepEqual(defaults["MD061"].Options["terms"], []string{}) || defaults["MD061"].Options["case_sensitive"] != false {
		t.Fatal(defaults, err)
	}
}

func TestMD061ProtectedAndNestedSource(t *testing.T) {
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	for _, tc := range []struct {
		name, source string
		terms        []string
		ranges       [][2]int
	}{
		{"table-task", "| blacklist | x |\n| --- | --- |\n| blacklist | y |\n\n- [x] blacklist", nil, [][2]int{{2, 11}, {34, 43}, {57, 66}}},
		{"nested-code", "> - ~~~blacklist\n>   blacklist\n>   ~~~", nil, nil},
		{"unclosed-code", "~~~\nblacklist", nil, nil},
		{"quoted-list", "> 1. a\n>    - blacklist", nil, [][2]int{{14, 23}}},
		{"header-duplicate", "---\nx: blacklist\nx: blacklist\n---\nblacklist", nil, [][2]int{{34, 43}}},
		{"entity-nonword-neighbor", "&#32;blacklist&#32;", nil, [][2]int{{5, 14}}},
		{"entity-word-neighbor", "&#97;blacklist", nil, nil},
		{"tag-no-join", "black<a>list</a> blacklist", nil, [][2]int{{17, 26}}},
		{"link-no-join", "[black](x)list blacklist", nil, [][2]int{{15, 24}}},
		{"raw-cross-block", "a <script>\n\nblacklist\n\n</script>\n\nblacklist", nil, [][2]int{{34, 43}}},
		{"code-raw-does-not-leak", "~~~\n<script>blacklist\n~~~\n\nblacklist", nil, [][2]int{{27, 36}}},
		{"no-cross-line-term", "black\nlist", []string{"black\nlist"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			terms := tc.terms
			if terms == nil {
				terms = []string{"blacklist"}
			}
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, md061Settings(terms, false), engine.DocumentContext{Flavor: "gfm", Frontmatter: fm}, engine.RunOptions{})
			if !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(tc.ranges) {
				t.Fatal(out)
			}
			for i, d := range out.Diagnostics {
				w := tc.ranges[i]
				if [2]int{d.ByteRange.Start, d.ByteRange.End} != w || d.Range.Start != md038OriginalPosition(tc.source, w[0]) || d.Range.End != md038OriginalPosition(tc.source, w[1]) || d.Message != "Configured prohibited term." || d.FixAvailable {
					t.Fatal(d, w)
				}
			}
		})
	}
}

func TestMD061UnavailableSyntaxAndUTF8(t *testing.T) {
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	out := analyzeHeadings(t, registry(t, Registrations()), "---\nblacklist", md061Settings([]string{"blacklist"}, false), engine.DocumentContext{Frontmatter: fm}, engine.RunOptions{})
	if out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
	in, err := engine.NewInput("invalid.md", []byte{'b', 255}, md061Settings([]string{"blacklist"}, false), engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	out, err = registry(t, Registrations()).Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil || out.Complete || len(out.Diagnostics) != 0 || len(out.Errors) != 1 || out.Errors[0].Code != "invalid-utf8" {
		t.Fatal(out, err)
	}
	_, err = registry(t, Registrations()).ResolveSettings(engine.Settings{"MD061": {Options: engine.Options{"terms": []string{"ok", string([]byte{255})}}}}, engine.ValidationProviders{})
	var detail *engine.SettingError
	if !errors.As(err, &detail) || detail.RuleID != "MD061" || detail.Option != "terms" || detail.Element == nil || *detail.Element != 1 {
		t.Fatal(err)
	}
}
