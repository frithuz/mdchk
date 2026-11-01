package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

func TestMD070FrozenInvalidConfigurations(t *testing.T) {
	b, e := os.ReadFile("../rules/testdata/md070.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "9e3ca955c4cca090d3f80175d6195cf2779e24578520f64c3a2c79bab365f4be" {
		t.Fatal(e)
	}
	var v struct {
		Invalid []struct {
			Name     string
			Value    json.RawMessage `json:"md070_value"`
			Rejected bool            `json:"expected_rejected"`
		} `json:"invalid_configurations"`
	}
	if e = json.Unmarshal(b, &v); e != nil || len(v.Invalid) != 13 {
		t.Fatal(e)
	}
	r := registryFor(t, "MD070")
	for _, row := range v.Invalid {
		t.Run(row.Name, func(t *testing.T) {
			var value bytes.Buffer
			if e := json.Compact(&value, row.Value); e != nil {
				t.Fatal(e)
			}
			d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nrules: {MD070: "+value.String()+"}\n"))
			if e == nil {
				_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if e == nil || !row.Rejected {
				t.Fatal("invalid admitted", row.Name)
			}
		})
	}
	for _, text := range []string{"version: 1\nrules: {md070: {}}\n", "version: 1\nrules: {MD070: {style: backtick, style: tilde}}\n", "version: 1\nrules:\n  MD070: {}\n  MD070: {}\n", "version: 1\nrules: {MD070: {enabled: false, style: auto}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(text)
		}
	}
}
