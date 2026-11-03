package config

import (
	"github.com/frithuz/mdchk/internal/engine"
	"testing"
)

func TestMD045SharedConfigurationAdmission(t *testing.T) {
	r := registryFor(t, "MD045")
	for _, value := range []string{"{}", "{enabled: false}", "{allow_directives: false}", "{enabled: true, allow_directives: true}", "{allow_decorative: true}", "{allow_decorative: false}"} {
		t.Run(value, func(t *testing.T) {
			d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nrules: {MD045: "+value+"}\n"))
			if e == nil {
				_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if e != nil {
				t.Fatal(e)
			}
		})
	}
	for _, value := range []string{"null", "true", "[]", "{enabled: null}", "{enabled: 'false'}", "{allow_directives: null}", "{allow_directives: 1}", "{enabled: false, style: auto}", "{enabled: false, options: {}}", "{allow_decorative: null}", "{allow_decorative: 1}", "{enabled: false, allow_decorative: 'true'}"} {
		t.Run("invalid/"+value, func(t *testing.T) {
			d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nrules: {MD045: "+value+"}\n"))
			if e == nil {
				_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if e == nil {
				t.Fatal("invalid configuration admitted", value)
			}
		})
	}
}
