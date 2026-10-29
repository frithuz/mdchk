package config

import (
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
	"testing"
)

func TestMD044StrictConfiguration(t *testing.T) {
	for _, enabled := range []string{"false", "true"} {
		for _, body := range []string{"names: null", "names: GitHub", "names: [GitHub, 3]", "code_blocks: null", "code_blocks: 1", "unknown: true", "allow_directives: null", "enabled: null"} {
			source := "version: 1\nrules:\n  MD044:\n    enabled: " + enabled + "\n    " + body + "\n"
			if body == "enabled: null" {
				source = "version: 1\nrules:\n  MD044:\n    enabled: null\n"
			}
			definition, err := DecodeYAML("fixture.yaml", []byte(source))
			if err == nil {
				_, err = ValidateDefinition(definition, registryFor(t, "MD044"), engine.ValidationProviders{})
			}
			if err == nil {
				t.Fatal(source)
			}
		}
	}
	off := false
	_, err := registryFor(t, "MD044").ResolveSettings(engine.Settings{"MD044": {Enabled: &off, Options: engine.Options{"names": []string{"GitHub", string([]byte{255})}}}}, engine.ValidationProviders{})
	var setting *engine.SettingError
	if !errors.As(err, &setting) || setting.RuleID != "MD044" || setting.Option != "names" || setting.Element == nil || *setting.Element != 1 {
		t.Fatal(err)
	}
	source := "version: 1\nrules:\n  MD044: {enabled: true, names: ['', ' ', GitHub, Github], code_blocks: true, allow_directives: false}\n"
	definition, err := DecodeYAML("fixture.yaml", []byte(source))
	if err == nil {
		_, err = ValidateDefinition(definition, registryFor(t, "MD044"), engine.ValidationProviders{})
	}
	if err != nil {
		t.Fatal(err)
	}
}
