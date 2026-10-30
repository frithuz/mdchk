package config

import (
	"github.com/frithuz/mdchk/internal/engine"
	"testing"
)

func TestMD092StrictSharedBooleanSettingsEvenDisabled(t *testing.T) {
	r := registryFor(t, "MD092")
	for _, enabled := range []string{"true", "false"} {
		for _, value := range []string{"true", "false", "null", "1", "'true'", "[]", "{}"} {
			source := "version: 1\nrules: {MD092: {enabled: " + enabled + ", allow_directives: " + value + "}}\n"
			d, e := DecodeYAML("fixture.yaml", []byte(source))
			if e == nil {
				_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if (e == nil) != (value == "true" || value == "false") {
				t.Fatal(enabled, value, e)
			}
		}
	}
	for _, value := range []string{"null", "1", "'false'", "[]", "{}"} {
		d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nrules: {MD092: {enabled: "+value+"}}\n"))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(value)
		}
	}
	for _, setting := range []string{"null", "[]", "true", "{enabled: false, unknown: true}", "{enabled: false, options: {}}"} {
		d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nrules: {MD092: "+setting+"}\n"))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(setting)
		}
	}
}
