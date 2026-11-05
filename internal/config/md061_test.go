package config

import (
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
	"testing"
)

func TestMD061StrictConfiguration(t *testing.T) {
	for _, enabled := range []string{"false", "true"} {
		for _, body := range []string{"terms: null", "terms: blacklist", "terms: [blacklist, 3]", "case_sensitive: null", "case_sensitive: 1", "unknown: true", "allow_directives: null", "enabled: null"} {
			source := "version: 1\nrules:\n  MD061:\n    enabled: " + enabled + "\n    " + body + "\n"
			if body == "enabled: null" {
				source = "version: 1\nrules:\n  MD061:\n    enabled: null\n"
			}
			definition, err := DecodeYAML("fixture.yaml", []byte(source))
			if err == nil {
				_, err = ValidateDefinition(definition, registryFor(t, "MD061"), engine.ValidationProviders{})
			}
			if err == nil {
				t.Fatal(source)
			}
		}
	}
	for _, enabled := range []bool{false, true} {
		_, err := registryFor(t, "MD061").ResolveSettings(engine.Settings{"MD061": {Enabled: &enabled, Options: engine.Options{"terms": []string{"blacklist", string([]byte{255})}}}}, engine.ValidationProviders{})
		var setting *engine.SettingError
		if !errors.As(err, &setting) || setting.RuleID != "MD061" || setting.Option != "terms" || setting.Element == nil || *setting.Element != 1 {
			t.Fatal(err)
		}
	}
	source := "version: 1\nrules:\n  MD061: {enabled: true, terms: ['', ' ', blacklist, Blacklist], case_sensitive: true, allow_directives: false}\n"
	definition, err := DecodeYAML("fixture.yaml", []byte(source))
	if err == nil {
		_, err = ValidateDefinition(definition, registryFor(t, "MD061"), engine.ValidationProviders{})
	}
	if err != nil {
		t.Fatal(err)
	}
}
