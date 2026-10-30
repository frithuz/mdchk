package config

import (
	"github.com/frithuz/mdchk/internal/engine"
	"testing"
)

func TestMD065StrictConfiguration(t *testing.T) {
	for _, enabled := range []string{"false", "true"} {
		for _, body := range []string{
			"before: 1", "after: 1", "minimum: 1", "style: consistent", "unknown: true",
			"allow_directives: null", "allow_directives: 'false'", "allow_directives: 1", "allow_directives: []", "allow_directives: {}",
		} {
			source := "version: 1\nrules:\n  MD065:\n    enabled: " + enabled + "\n    " + body + "\n"
			d, e := DecodeYAML("fixture.yaml", []byte(source))
			if e == nil {
				_, e = ValidateDefinition(d, registryFor(t, "MD065"), engine.ValidationProviders{})
			}
			if e == nil {
				t.Fatal(source)
			}
		}
	}
	for _, body := range []string{"null", "{enabled: null}", "{enabled: 'false'}", "{enabled: 1}", "{enabled: []}", "{enabled: {}}"} {
		source := "version: 1\nrules:\n  MD065: " + body + "\n"
		d, e := DecodeYAML("fixture.yaml", []byte(source))
		if e == nil {
			_, e = ValidateDefinition(d, registryFor(t, "MD065"), engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(source)
		}
	}
	for _, body := range []string{"{}", "{enabled: true, allow_directives: true}", "{enabled: false, allow_directives: false}"} {
		d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nrules:\n  MD065: "+body+"\n"))
		if e == nil {
			_, e = ValidateDefinition(d, registryFor(t, "MD065"), engine.ValidationProviders{})
		}
		if e != nil {
			t.Fatal(body, e)
		}
	}
	for _, source := range []string{
		"version: 1\nrules:\n  MD065: {}\n  MD065: {}\n",
		"version: 1\nrules:\n  MD065: {enabled: true, enabled: false}\n",
		"version: 1\nrules:\n  md065: {}\n",
		"version: 1\nrules:\n  horizontal-rule-spacing: {}\n",
	} {
		d, e := DecodeYAML("fixture.yaml", []byte(source))
		if e == nil {
			_, e = ValidateDefinition(d, registryFor(t, "MD065"), engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(source)
		}
	}
}
