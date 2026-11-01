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

func TestMD040FrozenInvalidConfigurations(t *testing.T) {
	b, e := os.ReadFile("../rules/testdata/md040.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "b8967e0a2f607a15ba75d0c78e0a770052ff9c27d1e3ee21ca6559df4fe9eaad" {
		t.Fatal(e)
	}
	var v struct {
		Invalid []struct {
			Name     string
			Value    json.RawMessage `json:"md040_value"`
			Rejected bool            `json:"expected_rejected"`
		} `json:"invalid_configurations"`
	}
	if e = json.Unmarshal(b, &v); e != nil || len(v.Invalid) != 15 {
		t.Fatal(e)
	}
	r := registryFor(t, "MD040")
	for _, row := range v.Invalid {
		t.Run(row.Name, func(t *testing.T) {
			var value bytes.Buffer
			if e := json.Compact(&value, row.Value); e != nil {
				t.Fatal(e)
			}
			d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nrules: {MD040: "+value.String()+"}\n"))
			if e == nil {
				_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if e == nil || !row.Rejected {
				t.Fatal("invalid admitted", row.Name)
			}
		})
	}
	for _, text := range []string{"version: 1\nrules: {md040: {}}\n", "version: 1\nrules:\n  MD040: {}\n  MD040: {}\n", "version: 1\nrules: {MD040: {enabled: false, allowed_languages: [go, false]}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(text)
		}
	}
}

func TestMD040PermissiveLiteralAllowlist(t *testing.T) {
	r := registryFor(t, "MD040")
	for _, enabled := range []bool{false, true} {
		for _, list := range [][]string{{}, {""}, {"go", "go"}, {" go ", "\t", "é", "go\nnext", "\x00", "{.go}"}} {
			b, e := json.Marshal(list)
			if e != nil {
				t.Fatal(e)
			}
			d, e := DecodeYAML("fixture.yaml", []byte(fmt.Sprintf("version: 1\nrules: {MD040: {enabled: %v, allowed_languages: %s}}\n", enabled, b)))
			if e == nil {
				_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if e != nil {
				t.Fatal(enabled, list, e)
			}
		}
	}
}
