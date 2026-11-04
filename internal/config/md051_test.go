package config

import (
	"github.com/frithuz/mdchk/internal/engine"
	"testing"
)

func TestMD051SharedConfigurationAdmission(t *testing.T) {
	r := registryFor(t, "MD051")
	for _, value := range []string{"{}", "{cross_file: true}", "{cross_file: false}", "{enabled: false, cross_file: true}", "{enabled: false}", "{allow_directives: false}", "{enabled: true, allow_directives: true}"} {
		t.Run(value, func(t *testing.T) {
			d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nrules: {MD051: "+value+"}\n"))
			if e == nil {
				_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if e != nil {
				t.Fatal(e)
			}
		})
	}
	for _, value := range []string{"null", "true", "[]", "{enabled: null}", "{enabled: 'false'}", "{allow_directives: null}", "{allow_directives: 1}", "{enabled: false, anchor_style: github}", "{enabled: false, options: {}}", "{cross_file: null}", "{cross_file: yes please}", "{cross_file: 1}"} {
		t.Run("invalid/"+value, func(t *testing.T) {
			d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nrules: {MD051: "+value+"}\n"))
			if e == nil {
				_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if e == nil {
				t.Fatal("invalid configuration admitted", value)
			}
		})
	}
}
