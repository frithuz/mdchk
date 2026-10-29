package config

import (
	"github.com/frithuz/mdchk/internal/engine"
	"testing"
)

func TestMD049StrictConfiguration(t *testing.T) {
	for _, enabled := range []string{"false", "true"} {
		for _, body := range []string{"style: null", "style: 1", "style: [asterisk]", "style: Asterisk", "style: dash", "style: ''", "unknown: true", "allow_directives: null", "enabled: null"} {
			source := "version: 1\nrules:\n  MD049:\n    enabled: " + enabled + "\n    " + body + "\n"
			if body == "enabled: null" {
				source = "version: 1\nrules:\n  MD049: {enabled: null}\n"
			}
			d, err := DecodeYAML("fixture.yaml", []byte(source))
			if err == nil {
				_, err = ValidateDefinition(d, registryFor(t, "MD049"), engine.ValidationProviders{})
			}
			if err == nil {
				t.Fatal(source)
			}
		}
	}
	for _, body := range []string{"{}", "{style: consistent}", "{style: asterisk}", "{style: underscore}"} {
		d, err := DecodeYAML("fixture.yaml", []byte("version: 1\nrules:\n  MD049: "+body+"\n"))
		if err == nil {
			_, err = ValidateDefinition(d, registryFor(t, "MD049"), engine.ValidationProviders{})
		}
		if err != nil {
			t.Fatal(body, err)
		}
	}
}
