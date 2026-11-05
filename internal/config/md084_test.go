package config

import (
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
	"testing"
)

func TestMD084StrictConfiguration(t *testing.T) {
	for _, enabled := range []string{"false", "true"} {
		for _, body := range []string{"allowed_codepoints: null", "allowed_codepoints: 200b", "allowed_codepoints: [200b, 3]", "allowed_codepoints: [U+200B]", "allowed_codepoints: ['0x200b']", "allowed_codepoints: ['']", "allowed_codepoints: [D800]", "allowed_codepoints: ['110000']", "unknown: true", "allow_directives: null", "enabled: null"} {
			source := "version: 1\nrules:\n  MD084:\n    enabled: " + enabled + "\n    " + body + "\n"
			if body == "enabled: null" {
				source = "version: 1\nrules:\n  MD084:\n    enabled: null\n"
			}
			definition, err := DecodeYAML("fixture.yaml", []byte(source))
			if err == nil {
				_, err = ValidateDefinition(definition, registryFor(t, "MD084"), engine.ValidationProviders{})
			}
			if err == nil {
				t.Fatal(source)
			}
		}
	}
	for _, enabled := range []bool{false, true} {
		_, err := registryFor(t, "MD084").ResolveSettings(engine.Settings{"MD084": {Enabled: &enabled, Options: engine.Options{"allowed_codepoints": []string{"200b", "U+202E"}}}}, engine.ValidationProviders{})
		var setting *engine.SettingError
		if !errors.As(err, &setting) || setting.RuleID != "MD084" || setting.Option != "allowed_codepoints" || setting.Element == nil || *setting.Element != 1 {
			t.Fatal(err)
		}
	}
	source := "version: 1\nrules:\n  MD084: {enabled: true, allowed_codepoints: [200b, 200B, '00FEFF', 202e, 200d], allow_directives: false}\n"
	definition, err := DecodeYAML("fixture.yaml", []byte(source))
	if err == nil {
		_, err = ValidateDefinition(definition, registryFor(t, "MD084"), engine.ValidationProviders{})
	}
	if err != nil {
		t.Fatal(err)
	}
}
