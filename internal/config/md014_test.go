package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

func TestMD014FrozenInvalidConfigurationsEvenDisabled(t *testing.T) {
	b, err := os.ReadFile("../rules/testdata/md014-v2.json")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "b06f76546386295e38b4dc71b871d21398fc25b5e647ae1093ccdb1e5e787431" {
		t.Fatal(err)
	}
	var oracle struct {
		Invalid []struct {
			Name       string
			Value      json.RawMessage `json:"md014_value"`
			Rejected   bool            `json:"expected_rejected"`
			Operations []string
		} `json:"invalid_configurations"`
	}
	if err = json.Unmarshal(b, &oracle); err != nil || len(oracle.Invalid) != 19 {
		t.Fatal(err, len(oracle.Invalid))
	}
	r := registryFor(t, "MD014")
	for _, row := range oracle.Invalid {
		t.Run(row.Name, func(t *testing.T) {
			if !row.Rejected || !reflect.DeepEqual(row.Operations, []string{"lint", "fix"}) {
				t.Fatal(row)
			}
			var text bytes.Buffer
			if err := json.Compact(&text, row.Value); err != nil {
				t.Fatal(err)
			}
			d, err := DecodeYAML("fixture.yaml", []byte("version: 1\nrules:\n  MD014: "+text.String()+"\n"))
			if err == nil {
				_, err = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if err == nil {
				t.Fatal("invalid option accepted", row.Name, text.String())
			}
		})
	}
}

func TestMD014PermissiveLiteralLanguageDomain(t *testing.T) {
	r := registryFor(t, "MD014")
	for _, enabled := range []bool{false, true} {
		for _, languages := range [][]string{{}, {""}, {"sh", "sh"}, {" sh ", "\t", "ésh", "sh\nnext", "\x00"}} {
			b, err := json.Marshal(languages)
			if err != nil {
				t.Fatal(err)
			}
			d, err := DecodeYAML("fixture.yaml", []byte(fmt.Sprintf("version: 1\nrules: {MD014: {enabled: %v, languages: %s}}\n", enabled, b)))
			if err == nil {
				_, err = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if err != nil {
				t.Fatal(enabled, languages, err)
			}
		}
	}
}
