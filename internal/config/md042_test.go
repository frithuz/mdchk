package config

import (
	"github.com/frithuz/mdchk/internal/engine"
	"testing"
)

func TestMD042SharedConfigurationAdmission(t *testing.T) {
	r := registryFor(t, "MD042")
	for _, value := range []string{"{}", "{enabled: false}", "{allow_directives: false}", "{enabled: true, allow_directives: true}"} {
		t.Run(value, func(t *testing.T) {
			d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nrules: {MD042: "+value+"}\n"))
			if e == nil {
				_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if e != nil {
				t.Fatal(e)
			}
		})
	}
	for _, value := range []string{"null", "true", "[]", "{enabled: null}", "{enabled: 'false'}", "{allow_directives: null}", "{allow_directives: 1}", "{enabled: false, style: auto}", "{enabled: false, options: {}}"} {
		t.Run("invalid/"+value, func(t *testing.T) {
			d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nrules: {MD042: "+value+"}\n"))
			if e == nil {
				_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if e == nil {
				t.Fatal("invalid configuration admitted", value)
			}
		})
	}
}
