package md083_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

func TestMD083OptionsAndAvailability(t *testing.T) {
	r := ruletest.Registry(t, rules.Registrations())
	found := false
	for _, m := range r.Metadata() {
		if m.ID == "MD083" {
			found = true
			if m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.NoFix || len(m.Options) != 1 || m.Options["sequences"].Domain != engine.ConfiguredNameUTF8 || !reflect.DeepEqual(m.Options["sequences"].Default, []string{"Ã", "Â", "â€"}) {
				t.Fatal(m)
			}
		}
	}
	if !found || !r.Available("MD083") {
		t.Fatal("missing executable MD083")
	}
	for _, enabled := range []bool{false, true} {
		for _, options := range []engine.Options{{"sequences": nil}, {"sequences": "Ã"}, {"sequences": []any{"Ã", 3}}, {"sequences": []string{string([]byte{255})}}, {"unknown": true}} {
			if _, err := r.ResolveSettings(engine.Settings{"MD083": {Enabled: &enabled, Options: options}}, engine.ValidationProviders{}); err == nil {
				t.Fatal(enabled, options)
			}
		}
	}
	sequences := []string{"", "Ã", "Ã", "â€"}
	resolved, err := r.ResolveSettings(engine.Settings{"MD083": {Options: engine.Options{"sequences": sequences}}}, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	expected := append([]string(nil), sequences...)
	sequences[0] = "changed"
	if !reflect.DeepEqual(resolved["MD083"].Options["sequences"], expected) {
		t.Fatal(resolved)
	}
	defaults, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil || defaults["MD083"].Enabled || !reflect.DeepEqual(defaults["MD083"].Options["sequences"], []string{"Ã", "Â", "â€"}) {
		t.Fatal(defaults, err)
	}
	s := md083Settings([]string{"Ã"})
	off := false
	s["MD083"] = engine.RuleSetting{Enabled: &off}
	out := ruletest.AnalyzeDocument(t, r, "cafÃ©", s, engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
	in, err := engine.NewInput("default.md", []byte("cafÃ©\n"), nil, engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	out, err = r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil || !out.Complete {
		t.Fatal(out, err)
	}
	for _, d := range out.Diagnostics {
		if d.Rule == "MD083" {
			t.Fatal(d)
		}
	}
	for _, c := range out.Coverage {
		if c.Rule == "MD083" {
			t.Fatal(c)
		}
	}
}

func TestMD083ProtectedAndNestedSource(t *testing.T) {
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	for _, tc := range []struct {
		name, source string
		sequences    []string
		ranges       [][2]int
	}{
		{"documented-violating", "cafÃ©", nil, [][2]int{{3, 5}}},
		{"documented-accepted", "café", nil, nil},
		{"each-default", "Â© donâ€™t cafÃ©", nil, [][2]int{{0, 2}, {8, 13}, {21, 23}}},
		{"lowercase", "cafã©", nil, nil},
		{"custom-list", "cafÃ© Â©", []string{"Ã", "Ã©"}, [][2]int{{3, 7}}},
		{"custom-empty", "cafÃ©", []string{""}, nil},
		{"custom-none", "cafÃ©", []string{}, nil},
		{"heading-and-label", "# cafÃ©\n\n[Ã](x) ![Ã](y.png)", nil, [][2]int{{5, 7}, {12, 14}, {21, 23}}},
		{"table-cell", "| Ã | b |\n| --- | --- |\n| cafÃ© | x |", nil, [][2]int{{2, 4}, {30, 32}}},
		{"nested-containers", "> - > 1. cafÃ©\n>   >    Ã", nil, [][2]int{{12, 14}, {26, 28}}},
		{"code", "`Ã`\n\n~~~\nÃ\n~~~\n\n    Ã", nil, nil},
		{"html", "<div>\nÃ\n</div>\n\na <b title=\"Ã\">x</b>", nil, nil},
		{"links", "<https://e.com/Ã> https://e.com/Ã [x](https://e.com/Ã \"Ã\")", nil, nil},
		{"definition", "[x]\n\n[x]: /Ã \"Ã\"", nil, nil},
		{"frontmatter", "---\nx: Ã\n---\nÃ", nil, [][2]int{{14, 16}}},
		{"entity", "&Atilde;&#195;", nil, nil},
		{"lf", "First.\nThe cafÃ©.", nil, [][2]int{{14, 16}}},
		{"crlf", "First.\r\nThe cafÃ©.", nil, [][2]int{{15, 17}}},
		{"multibyte-columns", "π Ω\nüÃ", nil, [][2]int{{8, 10}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sequences := tc.sequences
			if sequences == nil {
				sequences = []string{"Ã", "Â", "â€"}
			}
			out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), tc.source, md083Settings(sequences), engine.DocumentContext{Flavor: "gfm", Frontmatter: fm}, engine.RunOptions{})
			if !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(tc.ranges) || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
				t.Fatal(out)
			}
			for i, d := range out.Diagnostics {
				w := tc.ranges[i]
				if [2]int{d.ByteRange.Start, d.ByteRange.End} != w || d.Range.Start != ruletest.OriginalPosition(tc.source, w[0]) || d.Range.End != ruletest.OriginalPosition(tc.source, w[1]) || d.Message != "Text may be corrupted by an encoding mismatch." || d.FixAvailable {
					t.Fatal(d, w)
				}
			}
		})
	}
}

func TestMD083LineEndingsAndDirectives(t *testing.T) {
	r := ruletest.Registry(t, rules.Registrations())
	lf := ruletest.AnalyzeDocument(t, r, "a\nbÃ", md083Settings([]string{"Ã"}), engine.DocumentContext{}, engine.RunOptions{})
	crlf := ruletest.AnalyzeDocument(t, r, "a\r\nbÃ", md083Settings([]string{"Ã"}), engine.DocumentContext{}, engine.RunOptions{})
	if len(lf.Diagnostics) != 1 || len(crlf.Diagnostics) != 1 || lf.Diagnostics[0].Range != crlf.Diagnostics[0].Range || lf.Diagnostics[0].Range.Start.Line != 2 || lf.Diagnostics[0].Range.Start.Column != 2 || lf.Diagnostics[0].ByteRange.Start != 3 || crlf.Diagnostics[0].ByteRange.Start != 4 {
		t.Fatal(lf, crlf)
	}
	source := "Ã\n\n<!-- mdchk:disable MD083 -->\nÃ\n\n<!-- mdchk:enable MD083 -->\nÃ"
	for _, allow := range []bool{true, false} {
		s := md083Settings([]string{"Ã"})
		rule := s["MD083"]
		rule.AllowDirectives = &allow
		s["MD083"] = rule
		out := ruletest.AnalyzeDocument(t, r, source, s, engine.DocumentContext{}, engine.RunOptions{})
		want := 3
		if allow {
			want = 2
		}
		if !out.Complete || len(out.Diagnostics) != want || allow && len(out.SuppressedDiagnostics) != 1 {
			t.Fatal(allow, out)
		}
	}
}

func TestMD083UnavailableSyntaxAndUTF8(t *testing.T) {
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), "---\nÃ", md083Settings([]string{"Ã"}), engine.DocumentContext{Frontmatter: fm}, engine.RunOptions{})
	if out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
	in, err := engine.NewInput("invalid.md", []byte{0xc3, 0x83, 255}, md083Settings([]string{"Ã"}), engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	out, err = ruletest.Registry(t, rules.Registrations()).Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil || out.Complete || len(out.Diagnostics) != 0 || len(out.Errors) != 1 || out.Errors[0].Code != "invalid-utf8" {
		t.Fatal(out, err)
	}
	_, err = ruletest.Registry(t, rules.Registrations()).ResolveSettings(engine.Settings{"MD083": {Options: engine.Options{"sequences": []string{"Ã", string([]byte{255})}}}}, engine.ValidationProviders{})
	var detail *engine.SettingError
	if !errors.As(err, &detail) || detail.RuleID != "MD083" || detail.Option != "sequences" || detail.Element == nil || *detail.Element != 1 {
		t.Fatal(err)
	}
}
