package engine_test

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

// These independent contract fixtures are transcribed from docs/rules, rather
// than collected from Catalog. Rules without an entry have no options.
func documentedOptions() map[string]engine.OptionSchema {
	b := func(v bool) engine.OptionSpec { return engine.OptionSpec{Kind: engine.Boolean, Default: v} }
	n := func(v, min int) engine.OptionSpec {
		return engine.OptionSpec{Kind: engine.Integer, Default: v, Minimum: &min}
	}
	level := func(v int) engine.OptionSpec { s := n(v, 1); max := 6; s.Maximum = &max; return s }
	str := func(v string) engine.OptionSpec { return engine.OptionSpec{Kind: engine.String, Default: v} }
	choice := func(v ...string) engine.OptionSpec {
		return engine.OptionSpec{Kind: engine.String, Default: v[0], Choices: v}
	}
	list := func(v ...string) engine.OptionSpec {
		return engine.OptionSpec{Kind: engine.Strings, Default: append([]string{}, v...)}
	}
	marker := func(v string) engine.OptionSpec { s := str(v); s.Domain = engine.HTMLComment; return s }
	domain := func(d engine.OptionDomain) engine.OptionSpec { s := list(); s.Domain = d; return s }
	min, max := 0, 1
	styles := []string{"inline", "full_reference", "collapsed_reference", "shortcut_reference", "autolink"}
	return map[string]engine.OptionSchema{
		"MD001": {"frontmatter_title": b(false)},
		"MD003": {"style": choice("consistent", "atx", "atx_closed", "setext")},
		"MD004": {"marker": choice("consistent", "asterisk", "plus", "dash")},
		"MD007": {"indent": n(2, 1), "start_indented": b(false)},
		"MD009": {"hard_break_spaces": n(2, 2), "strict": b(false), "code_blocks": b(false)},
		"MD010": {"code_blocks": b(false), "spaces_per_tab": n(4, 1)},
		"MD012": {"maximum": n(1, 0)},
		"MD013": {"length": n(80, 1), "code_blocks": b(false), "tables": b(false), "headings": b(true)},
		"MD014": {"languages": list("sh", "shell", "bash", "zsh", "console")},
		"MD022": {"before": n(1, 0), "after": n(1, 0)},
		"MD024": {"siblings_only": b(false)},
		"MD025": {"level": level(1), "frontmatter_title": b(false)},
		"MD026": {"punctuation": str(".,:;!?")},
		"MD029": {"style": choice("ordered", "one")},
		"MD030": {"spaces": n(1, 1)},
		"MD033": {"allowed_tags": list()},
		"MD035": {"style": choice("consistent", "dash", "asterisk", "underscore")},
		"MD040": {"allowed_languages": list()},
		"MD041": {"level": level(1), "frontmatter_title": b(false)},
		"MD043": {"headings": list()},
		"MD044": {"names": list(), "code_blocks": b(false)},
		"MD045": {"allow_decorative": b(false)},
		"MD046": {"style": choice("consistent", "fenced", "indented")},
		"MD048": {"style": choice("consistent", "backtick", "tilde")},
		"MD049": {"style": choice("consistent", "asterisk", "underscore")},
		"MD050": {"style": choice("consistent", "asterisk", "underscore")},
		"MD051": {"cross_file": b(false)},
		"MD053": {"ignored_labels": list()},
		"MD054": {"allowed_styles": {Kind: engine.Strings, Default: styles, Choices: styles, MinItems: 1}},
		"MD055": {"style": choice("consistent", "leading_and_trailing", "none")},
		"MD057": {"check_images": b(true)},
		"MD059": {"phrases": list("click here", "here", "more", "read more")},
		"MD060": {"style": choice("aligned", "compact", "tight")},
		"MD061": {"terms": list(), "case_sensitive": b(false)},
		"MD063": {"style": choice("sentence", "title"), "proper_names": list()},
		"MD064": {"maximum": n(1, 1)},
		"MD072": {"recursive": b(false), "case_sensitive": b(true)},
		"MD073": {"start_marker": marker("<!-- toc -->"), "end_marker": marker("<!-- /toc -->"), "min_level": level(1), "max_level": level(6)},
		"MD074": {"config_path": str("mkdocs.yml"), "check_omitted": b(false), "check_absolute": b(false)},
		"MD076": {"style": choice("consistent", "tight", "loose")},
		"MD078": {"require_unique": b(true)},
		"MD081": {"maximum_ratio": {Kind: engine.Number, Default: 0.5, Minimum: &min, Maximum: &max}, "minimum_characters": n(20, 1)},
		"MD082": {"allow_final_section": b(false)},
		"MD083": {"sequences": list("Ã", "Â", "â€")},
		"MD084": {"allowed_codepoints": domain(engine.UnicodeScalarHex)},
		"MD088": {"style": choice("ascii")},
		"MD090": {"allow_slides": b(true)},
		"MD093": {"allowed_constructs": {Kind: engine.Strings, Default: []string{}, Choices: []string{"emphasis", "strong", "code", "link", "image"}}},
		"MD095": {"allowed_hosts": domain(engine.DNSHostname)},
	}
}

func TestCatalogDocumentedContracts(t *testing.T) {
	disabled := strings.Fields("MD013 MD026 MD033 MD036 MD040 MD043 MD044 MD054 MD060 MD061 MD063 MD070 MD072 MD073 MD074 MD078 MD079 MD080 MD081 MD082 MD083 MD084 MD085 MD087 MD088 MD089 MD090 MD091 MD093 MD095")
	planned := strings.Fields("MD003 MD004 MD005 MD007 MD009 MD010 MD011 MD012 MD014 MD019 MD021 MD022 MD023 MD027 MD029 MD030 MD031 MD032 MD034 MD035 MD038 MD044 MD046 MD047 MD048 MD049 MD050 MD053 MD054 MD055 MD058 MD060 MD063 MD064 MD065 MD067 MD070 MD071 MD072 MD073 MD076 MD077 MD085 MD088 MD089")
	reserved := []int{2, 6, 8, 15, 16, 17}
	var wantIDs []string
	for i := 1; i <= 95; i++ {
		if !slices.Contains(reserved, i) {
			wantIDs = append(wantIDs, fmt.Sprintf("MD%03d", i))
		}
	}
	root, err := os.OpenRoot("../../docs/rules")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	docs, err := fs.Glob(root.FS(), "*/md*.md")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, path := range docs {
		content, err := fs.ReadFile(root.FS(), path)
		if err != nil {
			t.Fatal(err)
		}
		title, _, _ := strings.Cut(string(content), "\n")
		id, name, ok := strings.Cut(strings.TrimPrefix(title, "# "), ": ")
		if !ok {
			t.Fatalf("missing rule title in %s", path)
		}
		if _, exists := names[id]; exists {
			t.Fatalf("duplicate documented rule %s", id)
		}
		names[id] = name
	}
	catalog := engine.Catalog()
	var gotIDs []string
	enabled := 0
	schemas := documentedOptions()
	for _, m := range catalog {
		gotIDs = append(gotIDs, m.ID)
		if m.Name != names[m.ID] {
			t.Errorf("%s name = %q, documented %q", m.ID, m.Name, names[m.ID])
		}
		if m.DefaultEnabled {
			enabled++
		}
		if m.DefaultEnabled == slices.Contains(disabled, m.ID) {
			t.Errorf("%s wrong default enablement", m.ID)
		}
		fix := engine.NoFix
		if slices.Contains(planned, m.ID) {
			fix = engine.PlannedFix
		}
		if m.Fix != fix {
			t.Errorf("%s fix = %s, want %s", m.ID, m.Fix, fix)
		}
		want := schemas[m.ID]
		if want == nil {
			want = engine.OptionSchema{}
		}
		if !reflect.DeepEqual(m.Options, want) {
			t.Errorf("%s schema = %#v, want %#v", m.ID, m.Options, want)
		}
		if m.RequiresSyntax != (m.ID != "MD094") {
			t.Errorf("%s wrong syntax prerequisite", m.ID)
		}
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Errorf("catalog IDs = %v, want %v", gotIDs, wantIDs)
	}
	if len(names) != 89 || len(catalog) != 89 || enabled != 59 || len(disabled) != 30 {
		t.Fatalf("counts: docs=%d catalog=%d enabled=%d disabled=%d", len(names), len(catalog), enabled, len(disabled))
	}
}

func TestCatalogExecutableSubsetsPreserveMetadata(t *testing.T) {
	check := func(context.Context, engine.RuleContext) engine.RuleOutcome { return engine.RuleOutcome{} }
	for _, ids := range [][]string{nil, {"MD001"}, {"MD013", "MD088", "MD095"}} {
		var regs []engine.Registration
		for _, id := range ids {
			regs = append(regs, engine.Registration{ID: id, Check: check})
		}
		r, err := engine.NewCatalogRegistry(regs)
		if err != nil {
			t.Fatal(err)
		}
		if !catalogMetadataEqual(r.Metadata(), engine.Catalog()) {
			t.Fatalf("subset %v changed metadata", ids)
		}
		states, err := r.ResolveSettings(nil, engine.ValidationProviders{})
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range r.Metadata() {
			if states[m.ID].Available != (m.ID == "MD094" || slices.Contains(ids, m.ID)) {
				t.Errorf("subset %v wrong availability for %s", ids, m.ID)
			}
		}
	}
	for _, id := range []string{"md001", "MD01", "MD000", "MD002", "MD006", "MD008", "MD015", "MD016", "MD017", "MD096", "MD999", "MD094"} {
		if _, err := engine.NewCatalogRegistry([]engine.Registration{{ID: id, Check: check}}); err == nil {
			t.Errorf("accepted registration %q", id)
		}
	}
	if _, err := engine.NewCatalogRegistry([]engine.Registration{{ID: "MD001", Check: check}, {ID: "MD001", Check: check}}); err == nil {
		t.Error("accepted duplicate implementation")
	}
	if _, err := engine.NewCatalogRegistry([]engine.Registration{{ID: "MD001"}}); err == nil {
		t.Error("accepted nil implementation")
	}
}

func TestCatalogSettingsPresenceAndPreflight(t *testing.T) {
	r, err := engine.NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := engine.SettingFromMapping(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"MD001", "MD013"} {
		states, err := r.ResolveSettings(engine.Settings{id: empty}, engine.ValidationProviders{})
		if err != nil {
			t.Fatal(err)
		}
		state := states[id]
		if !state.Explicit || state.Enabled != (id == "MD001") || !state.AllowDirectives || state.Available {
			t.Errorf("empty %s mapping resolved to %+v", id, state)
		}
		in := input(t, "test.md", []byte("text\n"), engine.Settings{id: empty})
		_, err = r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
		if (err != nil) != (id == "MD001") {
			t.Errorf("explicit %s admission error = %v", id, err)
		}
	}
	for _, mapping := range []map[string]any{{"length": 100}, {"allow_directives": false}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err != nil {
			t.Fatal(err)
		}
		states, err := r.ResolveSettings(engine.Settings{"MD013": setting}, engine.ValidationProviders{})
		if err != nil {
			t.Fatal(err)
		}
		state := states["MD013"]
		if !state.Explicit || state.Enabled {
			t.Errorf("presence lost: %+v", state)
		}
		if _, ok := mapping["allow_directives"]; ok && state.AllowDirectives {
			t.Error("permission override lost")
		}
	}
	for _, mapping := range []map[string]any{nil, {"enabled": nil}, {"enabled": "false"}, {"allow_directives": 0}, {"allow_directives": nil}} {
		if _, err := engine.SettingFromMapping(mapping); err == nil {
			t.Errorf("accepted invalid shared setting %#v", mapping)
		}
	}
	calls := 0
	check := func(context.Context, engine.RuleContext) engine.RuleOutcome { calls++; return engine.RuleOutcome{} }
	r, err = engine.NewCatalogRegistry([]engine.Registration{{ID: "MD001", Check: check}})
	if err != nil {
		t.Fatal(err)
	}
	for _, settings := range []engine.Settings{
		{"MD013": {Enabled: boolptr(false), Options: engine.Options{"length": 0}}},
		{"MD088": {Enabled: boolptr(false), Options: engine.Options{"replacements": []string{}}}},
		{"MD002": {}},
	} {
		inputs := []engine.Input{input(t, "a.md", []byte("# Valid\n"), nil), input(t, "b.md", []byte("# Valid\n"), settings)}
		if _, err := r.Lint(context.Background(), inputs, engine.RunOptions{}); err == nil {
			t.Errorf("accepted invalid disabled/unknown setting %#v", settings)
		}
		if calls != 0 {
			t.Fatalf("preflight ran %d callbacks", calls)
		}
	}
}

func TestCatalogDefensiveCopies(t *testing.T) {
	catalog := engine.Catalog()
	r, err := engine.NewRegistry(catalog, nil)
	if err != nil {
		t.Fatal(err)
	}
	mutate := func(ms []engine.Metadata) {
		for i := range ms {
			for k, s := range ms[i].Options {
				if s.Minimum != nil {
					*s.Minimum = -999
				}
				if s.Maximum != nil {
					*s.Maximum = -999
				}
				if len(s.Choices) > 0 {
					s.Choices[0] = "corrupted"
				}
				if v, ok := s.Default.([]string); ok && len(v) > 0 {
					v[0] = "corrupted"
				}
				s.Default = nil
				ms[i].Options[k] = s
			}
			ms[i].Name = "corrupted"
		}
	}
	mutate(catalog)
	mutate(r.Metadata())
	if !catalogMetadataEqual(r.Metadata(), engine.Catalog()) {
		t.Fatal("metadata shares mutable input/output storage")
	}
	phrases := []string{"custom"}
	states, err := r.ResolveSettings(engine.Settings{"MD059": {Options: engine.Options{"phrases": phrases}}}, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	phrases[0] = "mutated"
	if got := states["MD059"].Options["phrases"].([]string)[0]; got != "custom" {
		t.Fatalf("resolved override aliases input: %q", got)
	}
	states["MD014"].Options["languages"].([]string)[0] = "mutated"
	states["MD059"].Options["phrases"].([]string)[0] = "mutated"
	fresh, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	if fresh["MD014"].Options["languages"].([]string)[0] != "sh" || fresh["MD059"].Options["phrases"].([]string)[0] != "click here" {
		t.Fatal("resolved settings share catalog defaults")
	}
}
