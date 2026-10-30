package config

import (
	"github.com/frithuz/mdchk/internal/engine"
	"testing"
)

func TestMD081YAMLAndJSONNumberAdmission(t *testing.T) {
	for _, format := range []string{"yaml", "json"} {
		for _, tc := range []struct {
			ratio, minimum string
			valid          bool
		}{
			{"0", "1", true}, {"1", "20", true}, {"-0.0", "1", true}, {"0.33333333333333326", "1", true}, {"0.3333333333333333", "1", true}, {"0.33333333333333337", "1", true}, {".5", "2147483647", true},
			{"0.5", "20.0", false}, {"0.5", "2e1", false}, {"0.5", "0", false}, {"0.5", "-1", false}, {"1.01", "1", false}, {"-0.01", "1", false}, {"1e999", "1", false}, {"null", "1", false}, {"true", "1", false}, {"\"0.5\"", "1", false},
		} {
			t.Run(format+"/"+tc.ratio+"/"+tc.minimum, func(t *testing.T) {
				ratio := tc.ratio
				if format == "json" && ratio == ".5" {
					ratio = "0.5"
				}
				var d *Definition
				var err error
				if format == "yaml" {
					d, err = DecodeYAML("typed.yaml", []byte("version: 1\nrules:\n  MD081: {enabled: false, maximum_ratio: "+ratio+", minimum_characters: "+tc.minimum+"}\n"))
				} else {
					d, err = DecodeJSON([]byte(`{"version":1,"rules":{"MD081":{"enabled":false,"maximum_ratio":` + ratio + `,"minimum_characters":` + tc.minimum + `}}}`))
				}
				if err == nil {
					_, err = ValidateDefinition(d, registryFor(t, "MD081"), engine.ValidationProviders{})
				}
				if (err == nil) != tc.valid {
					t.Fatal(tc, err)
				}
			})
		}
	}
}
func TestMD081SharedAndUnknownConfiguration(t *testing.T) {
	for _, source := range []string{
		"version: 1\nrules:\n  MD081: null\n", "version: 1\nrules:\n  MD081: {enabled: null}\n", "version: 1\nrules:\n  MD081: {enabled: false, unknown: 1}\n", "version: 1\nrules:\n  MD081: {allow_directives: 'false'}\n", "version: 1\nrules:\n  md081: {}\n", "version: 1\nrules:\n  MD081: {}\n  MD081: {}\n", "version: 1\nrules:\n  MD081: {maximum_ratio: 0, maximum_ratio: 1}\n",
	} {
		d, e := DecodeYAML("strict.yaml", []byte(source))
		if e == nil {
			_, e = ValidateDefinition(d, registryFor(t, "MD081"), engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(source)
		}
	}
	for _, source := range []string{`{"version":1,"rules":{"MD081":null}}`, `{"version":1,"rules":{"MD081":{"enabled":null}}}`, `{"version":1,"rules":{"MD081":{"enabled":false,"unknown":1}}}`, `{"version":1,"rules":{"md081":{}}}`, `{"version":1,"rules":{"MD081":{},"MD081":{}}}`} {
		d, e := DecodeJSON([]byte(source))
		if e == nil {
			_, e = ValidateDefinition(d, registryFor(t, "MD081"), engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(source)
		}
	}
}
