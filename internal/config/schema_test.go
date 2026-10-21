package config

import (
	"reflect"
	"testing"
)

func pointer[T any](v T) *T { return &v }

func TestPresenceIsIndependentOfDefaults(t *testing.T) {
	definition := Definition{
		Resources: &Resources{IndexFile: pointer("index.md")},
		Rules: map[string]RuleSetting{
			"MD001": {},
			"MD013": {Enabled: pointer(false), AllowDirectives: pointer(true)},
		},
		Directives: &Directives{Enabled: pointer(false)},
	}
	defaults := DefaultSettings()
	if definition.Flavor != nil || defaults.Flavor != "gfm" {
		t.Fatal("default flavor must remain GFM without making it explicit")
	}
	if _, ok := definition.Rules["MD001"]; !ok {
		t.Fatal("empty rule mapping must remain explicit")
	}
	if _, ok := definition.Rules["MD004"]; ok {
		t.Fatal("unmentioned rule must remain absent")
	}
	if definition.Rules["MD001"].Enabled != nil || *definition.Rules["MD013"].Enabled {
		t.Fatal("omitted and explicit false enablement must remain distinct")
	}
	if definition.Rules["MD001"].AllowDirectives != nil || !*definition.Rules["MD013"].AllowDirectives || *definition.Directives.Enabled {
		t.Fatal("omitted, true and false permissions must remain distinct")
	}
	if definition.Resources.IndexFile == nil || *definition.Resources.IndexFile != defaults.IndexFile || definition.Resources.AnchorStyle != nil {
		t.Fatal("explicit resource default must remain distinct from omission")
	}
	// Empty mappings and sequences are valid syntax-level distinctions. Validation
	// is performed by the decoders; these passive types must not erase distinctions.
	definition.Frontmatter = &Frontmatter{}
	definition.Exclude = &[]string{}
	if definition.Frontmatter == nil || definition.Frontmatter.Enabled != nil || definition.Exclude == nil || len(*definition.Exclude) != 0 {
		t.Fatal("empty supplied collections were lost")
	}
}

func TestDefaultSettingsContractAndIsolation(t *testing.T) {
	first, second := DefaultSettings(), DefaultSettings()
	if first.Flavor != "gfm" || first.IndexFile != "index.md" || first.AnchorStyle != "github" || first.FrontmatterEnabled || first.QuartoEnabled || !first.DirectivesEnabled || !first.AllowDirectives || !first.AllowNestedExcludes {
		t.Fatalf("unexpected approved defaults: %+v", first)
	}
	if !reflect.DeepEqual(first.FrontmatterFormats, []string{"yaml", "toml"}) || len(first.Slides) != 0 || len(first.Exclude) != 0 {
		t.Fatalf("unexpected collection defaults: %+v", first)
	}
	first.FrontmatterFormats[0] = "changed"
	first.Slides = append(first.Slides, "slides/**")
	first.Exclude = append(first.Exclude, "vendor/**")
	if !reflect.DeepEqual(second, DefaultSettings()) {
		t.Fatal("one operation modified another operation's defaults")
	}
}
