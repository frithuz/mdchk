package config

import (
	"github.com/frithuz/mdchk/internal/engine"
	"testing"
)

func TestMD050StrictConfiguration(t *testing.T) {
	for _, enabled := range []string{"false", "true"} {
		for _, body := range []string{"style: null", "style: 1", "style: [asterisk]", "style: Asterisk", "style: dash", "style: ''", "unknown: true", "allow_directives: null", "enabled: null"} {
			source := "version: 1\nrules:\n  MD050:\n    enabled: " + enabled + "\n    " + body + "\n"
			if body == "enabled: null" {
				source = "version: 1\nrules:\n  MD050: {enabled: null}\n"
			}
			d, err := DecodeYAML("fixture.yaml", []byte(source))
			if err == nil {
				_, err = ValidateDefinition(d, registryFor(t, "MD050"), engine.ValidationProviders{})
			}
			if err == nil {
				t.Fatal(source)
			}
		}
	}
	for _, body := range []string{"{}", "{style: consistent}", "{style: asterisk}", "{style: underscore}"} {
		d, err := DecodeYAML("fixture.yaml", []byte("version: 1\nrules:\n  MD050: "+body+"\n"))
		if err == nil {
			_, err = ValidateDefinition(d, registryFor(t, "MD050"), engine.ValidationProviders{})
		}
		if err != nil {
			t.Fatal(body, err)
		}
	}
}

func TestMD050DuplicateAndAliasConfiguration(t *testing.T) {
	for _, source := range []string{
		"version: 1\nrules:\n  MD050: {style: asterisk, style: underscore}\n",
		"version: 1\nrules:\n  MD050: {}\n  MD050: {}\n",
		"version: 1\nrules:\n  md050: {}\n",
		"version: 1\nflavor: pandoc\nrules:\n  MD050: {}\n",
		"version: 1\nflavor: quarto\nrules:\n  MD050: {}\n",
	} {
		d, err := DecodeYAML("fixture.yaml", []byte(source))
		if err == nil {
			_, err = ValidateDefinition(d, registryFor(t, "MD050"), engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal(source)
		}
	}
}
