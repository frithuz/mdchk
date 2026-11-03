package config

import (
	"github.com/frithuz/mdchk/internal/engine"
	"testing"
)

func TestMD039StrictSharedConfiguration(t *testing.T) {
	r := registryFor(t, "MD039")
	for _, value := range []string{"null", "{unknown: true}", "{enabled: false, unknown: true}", "{enabled: null}", "{enabled: 1}", "{enabled: yes}", "{allow_directives: null}", "{allow_directives: 1}", "{style: auto}", "{enabled: true, enabled: false}"} {
		text := "version: 1\nrules: {MD039: " + value + "}\n"
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal("invalid admitted", text)
		}
	}
	for _, value := range []string{"{}", "{enabled: false}", "{enabled: true, allow_directives: false}"} {
		d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nrules: {MD039: "+value+"}\n"))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e != nil {
			t.Fatal(value, e)
		}
	}
}
