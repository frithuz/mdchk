package config

import (
	"github.com/frithuz/mdchk/internal/engine"
	"testing"
)

func TestMD088YAMLAndJSONAdmission(t *testing.T) {
	for _, format := range []string{"yaml", "json"} {
		for _, tc := range []struct {
			row   string
			valid bool
		}{
			{`{}`, true}, {`{"enabled":true,"style":"ascii"}`, true}, {`{"style":"ascii"}`, true}, {`{"enabled":false}`, true},
			{`null`, false}, {`{"style":null}`, false}, {`{"style":""}`, false}, {`{"style":"ASCII"}`, false}, {`{"style":1}`, false}, {`{"style":true}`, false},
			{`{"enabled":false,"replacements":{}}`, false}, {`{"enabled":null}`, false}, {`{"enabled":"false"}`, false}, {`{"allow_directives":0}`, false},
			{`{"style":"ascii","style":"ascii"}`, false},
		} {
			t.Run(format+"/"+tc.row, func(t *testing.T) {
				var d *Definition
				var e error
				if format == "yaml" {
					d, e = DecodeYAML("strict.yaml", []byte("version: 1\nrules:\n  MD088: "+tc.row+"\n"))
				} else {
					d, e = DecodeJSON([]byte(`{"version":1,"rules":{"MD088":` + tc.row + `}}`))
				}
				if e == nil {
					_, e = ValidateDefinition(d, registryFor(t, "MD088"), engine.ValidationProviders{})
				}
				if (e == nil) != tc.valid {
					t.Fatal(tc, e)
				}
			})
		}
	}
	for _, source := range []string{"version: 1\nrules:\n  md088: {}\n", "version: 1\nrules:\n  MD088: {}\n  MD088: {}\n"} {
		d, e := DecodeYAML("duplicate.yaml", []byte(source))
		if e == nil {
			_, e = ValidateDefinition(d, registryFor(t, "MD088"), engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(source)
		}
	}
}
