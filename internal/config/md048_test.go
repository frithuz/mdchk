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

func TestMD048FrozenInvalidConfigurations(t *testing.T) {
	b, e := os.ReadFile("../rules/testdata/md048.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "afef57dcdf5974d81ddbe9c85e688f1ac0f42b6d4befcd68b056a28b4b254807" {
		t.Fatal(e)
	}
	var v struct {
		Invalid []struct {
			Name     string
			Value    json.RawMessage `json:"md048_value"`
			Rejected bool            `json:"expected_rejected"`
		} `json:"invalid_configurations"`
	}
	if e = json.Unmarshal(b, &v); e != nil || len(v.Invalid) != 17 {
		t.Fatal(e)
	}
	r := registryFor(t, "MD048")
	for _, row := range v.Invalid {
		t.Run(row.Name, func(t *testing.T) {
			var value bytes.Buffer
			if e := json.Compact(&value, row.Value); e != nil {
				t.Fatal(e)
			}
			d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nrules: {MD048: "+value.String()+"}\n"))
			if e == nil {
				_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if e == nil || !row.Rejected {
				t.Fatal("invalid admitted", row.Name)
			}
		})
	}
	for _, text := range []string{"version: 1\nrules: {md048: {}}\n", "version: 1\nrules: {MD048: {style: backtick, style: tilde}}\n", "version: 1\nrules:\n  MD048: {}\n  MD048: {}\n", "version: 1\nrules: {MD048: {enabled: false, style: auto}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(text)
		}
	}
}

func TestMD048StyleDomain(t *testing.T) {
	r := registryFor(t, "MD048")
	for _, enabled := range []bool{false, true} {
		for _, style := range []string{"consistent", "backtick", "tilde"} {
			d, e := DecodeYAML("fixture.yaml", []byte(fmt.Sprintf("version: 1\nrules: {MD048: {enabled: %v, style: %s}}\n", enabled, style)))
			if e == nil {
				_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if e != nil {
				t.Fatal(enabled, style, e)
			}
		}
	}
}
