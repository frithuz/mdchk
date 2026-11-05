package config

import (
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

func TestMD053IgnoredLabelsAdmission(t *testing.T) {
	for _, format := range []string{"yaml", "json"} {
		for _, tc := range []struct {
			row   string
			valid bool
		}{
			{`{}`, true}, {`{"ignored_labels":[]}`, true}, {`{"ignored_labels":["a"]}`, true},
			{`{"enabled":false,"ignored_labels":["Foo  Bar","","[a]","ẞ"]}`, true},
			{`{"ignored_labels":"foo"}`, false}, {`{"ignored_labels":[1]}`, false}, {`{"ignored_labels":["a",1]}`, false},
			{`{"ignored_labels":null}`, false}, {`{"ignored_labels":{"a":1}}`, false}, {`{"other":true}`, false},
			{`{"ignored_labels":[],"other":true}`, false}, {`{"enabled":false,"unknown":[]}`, false},
			{`{"ignored_labels":[],"ignored_labels":[]}`, false}, {`{"enabled":"yes"}`, false}, {`null`, false},
		} {
			t.Run(format+"/"+tc.row, func(t *testing.T) {
				var d *Definition
				var e error
				if format == "yaml" {
					d, e = DecodeYAML("strict.yaml", []byte("version: 1\nrules:\n  MD053: "+tc.row+"\n"))
				} else {
					d, e = DecodeJSON([]byte(`{"version":1,"rules":{"MD053":` + tc.row + `}}`))
				}
				if e == nil {
					_, e = ValidateDefinition(d, registryFor(t, "MD053"), engine.ValidationProviders{})
				}
				if (e == nil) != tc.valid {
					t.Fatal(tc, e)
				}
			})
		}
	}
	for _, source := range []string{"version: 1\nrules:\n  MD053: {ignored_labels: foo}\n", "version: 1\nrules:\n  md053: {}\n", "version: 1\nrules:\n  MD053: {}\n  MD053: {}\n"} {
		d, e := DecodeYAML("plain.yaml", []byte(source))
		if e == nil {
			_, e = ValidateDefinition(d, registryFor(t, "MD053"), engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(source)
		}
	}
}
