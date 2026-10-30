package config

import (
	"github.com/frithuz/mdchk/internal/engine"
	"testing"
)

func TestMD033YAMLAndJSONAdmission(t *testing.T) {
	for _, format := range []string{"yaml", "json"} {
		for _, tc := range []struct {
			row   string
			valid bool
		}{
			{`{}`, true}, {`{"enabled":true,"allowed_tags":[]}`, true},
			{`{"allowed_tags":["B","b",""," b ","<b>","/b","ſ","\u0000"]}`, true},
			{`null`, false}, {`{"allowed_tags":null}`, false}, {`{"enabled":false,"allowed_tags":"b"}`, false},
			{`{"enabled":false,"allowed_tags":["b",1]}`, false}, {`{"enabled":null}`, false},
			{`{"enabled":"false"}`, false}, {`{"allow_directives":0}`, false},
			{`{"allowed_tags":[],"allowed_tags":[]}`, false}, {`{"enabled":false,"unknown":[]}`, false},
		} {
			t.Run(format+"/"+tc.row, func(t *testing.T) {
				var d *Definition
				var e error
				if format == "yaml" {
					d, e = DecodeYAML("strict.yaml", []byte("version: 1\nrules:\n  MD033: "+tc.row+"\n"))
				} else {
					d, e = DecodeJSON([]byte(`{"version":1,"rules":{"MD033":` + tc.row + `}}`))
				}
				if e == nil {
					_, e = ValidateDefinition(d, registryFor(t, "MD033"), engine.ValidationProviders{})
				}
				if (e == nil) != tc.valid {
					t.Fatal(tc, e)
				}
			})
		}
	}
	for _, source := range []string{"version: 1\nrules:\n  md033: {}\n", "version: 1\nrules:\n  MD033: {}\n  MD033: {}\n"} {
		d, e := DecodeYAML("duplicate.yaml", []byte(source))
		if e == nil {
			_, e = ValidateDefinition(d, registryFor(t, "MD033"), engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(source)
		}
	}
}
