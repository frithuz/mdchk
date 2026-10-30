package config

import (
	"github.com/frithuz/mdchk/internal/engine"
	"testing"
)

func TestMD086YAMLAndJSONAdmission(t *testing.T) {
	for _, format := range []string{"yaml", "json"} {
		for _, tc := range []struct {
			row   string
			valid bool
		}{
			{`{}`, true}, {`{"enabled":true}`, true}, {`{"enabled":false,"allow_directives":false}`, true},
			{`null`, false}, {`[]`, false}, {`true`, false}, {`{"enabled":null}`, false},
			{`{"enabled":"false"}`, false}, {`{"allow_directives":0}`, false}, {`{"allow_directives":null}`, false},
			{`{"enabled":false,"unknown":[]}`, false}, {`{"enabled":false,"allowed_tags":[]}`, false},
			{`{"enabled":true,"enabled":false}`, false}, {`{"enabled":false,"options":{}}`, false},
		} {
			t.Run(format+"/"+tc.row, func(t *testing.T) {
				var d *Definition
				var err error
				if format == "yaml" {
					d, err = DecodeYAML("strict.yaml", []byte("version: 1\nrules:\n  MD086: "+tc.row+"\n"))
				} else {
					d, err = DecodeJSON([]byte(`{"version":1,"rules":{"MD086":` + tc.row + `}}`))
				}
				if err == nil {
					_, err = ValidateDefinition(d, registryFor(t, "MD086"), engine.ValidationProviders{})
				}
				if (err == nil) != tc.valid {
					t.Fatal(tc, err)
				}
			})
		}
	}
	for _, source := range []string{"version: 1\nrules:\n  md086: {}\n", "version: 1\nrules:\n  MD086: {}\n  MD086: {}\n"} {
		d, err := DecodeYAML("duplicate.yaml", []byte(source))
		if err == nil {
			_, err = ValidateDefinition(d, registryFor(t, "MD086"), engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal(source)
		}
	}
}
