package config

import (
	"github.com/frithuz/mdchk/internal/engine"
	"testing"
)

func TestMD082BooleanPolicyEvenDisabled(t *testing.T) {
	r := registryFor(t, "MD082")
	for _, enabled := range []string{"true", "false"} {
		for _, value := range []string{"true", "false", "null", "1", "\"true\"", "[]", "{}"} {
			definition, err := DecodeYAML("fixture.yaml", []byte("version: 1\nrules: {MD082: {enabled: "+enabled+", allow_final_section: "+value+"}}\n"))
			if err == nil {
				_, err = ValidateDefinition(definition, r, engine.ValidationProviders{})
			}
			if (err == nil) != (value == "true" || value == "false") {
				t.Fatal(enabled, value, err)
			}
		}
	}
}
