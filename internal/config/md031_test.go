package config

import (
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

func TestMD031ConfigurationBoundaries(t *testing.T) {
	r := registryFor(t, "MD031")
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"empty", "{}", true}, {"enabled", "{enabled: true}", true}, {"disabled", "{enabled: false}", true}, {"denied", "{allow_directives: false}", true},
		{"null", "null", false}, {"scalar", "true", false}, {"list", "[]", false}, {"enabled-string", "{enabled: 'true'}", false}, {"allow-number", "{allow_directives: 1}", false}, {"unknown", "{list_items: false}", false}, {"disabled-unknown", "{enabled: false, list_items: false}", false}, {"null-enabled", "{enabled: null}", false}, {"null-allow", "{allow_directives: null}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nrules:\n  MD031: "+tc.value+"\n"))
			if e == nil {
				_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if (e == nil) != tc.valid {
				t.Fatal(e, tc)
			}
		})
	}
	for _, text := range []string{"version: 1\nrules: {md031: {}}\n", "version: 1\nrules:\n  MD031: {}\n  MD031: {}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal("invalid identity accepted", text)
		}
	}
}
