package config

import (
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
	"testing"
)

func TestMD083StrictConfiguration(t *testing.T) {
	for _, enabled := range []string{"false", "true"} {
		for _, body := range []string{"sequences: null", "sequences: Ã", "sequences: [Ã, 3]", "unknown: true", "allow_directives: null", "enabled: null"} {
			source := "version: 1\nrules:\n  MD083:\n    enabled: " + enabled + "\n    " + body + "\n"
			if body == "enabled: null" {
				source = "version: 1\nrules:\n  MD083:\n    enabled: null\n"
			}
			definition, err := DecodeYAML("fixture.yaml", []byte(source))
			if err == nil {
				_, err = ValidateDefinition(definition, registryFor(t, "MD083"), engine.ValidationProviders{})
			}
			if err == nil {
				t.Fatal(source)
			}
		}
	}
	for _, enabled := range []bool{false, true} {
		_, err := registryFor(t, "MD083").ResolveSettings(engine.Settings{"MD083": {Enabled: &enabled, Options: engine.Options{"sequences": []string{"Ã", string([]byte{255})}}}}, engine.ValidationProviders{})
		var setting *engine.SettingError
		if !errors.As(err, &setting) || setting.RuleID != "MD083" || setting.Option != "sequences" || setting.Element == nil || *setting.Element != 1 {
			t.Fatal(err)
		}
	}
	source := "version: 1\nrules:\n  MD083: {enabled: true, sequences: ['', Ã, Ã, â€], allow_directives: false}\n"
	definition, err := DecodeYAML("fixture.yaml", []byte(source))
	if err == nil {
		_, err = ValidateDefinition(definition, registryFor(t, "MD083"), engine.ValidationProviders{})
	}
	if err != nil {
		t.Fatal(err)
	}
}
