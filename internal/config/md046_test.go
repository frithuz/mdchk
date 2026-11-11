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

func TestMD046FrozenInvalidConfigurations(t *testing.T) {
	b, e := os.ReadFile("../rules/md046/testdata/md046.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "1fff51e531a8c0c7958ef64c617a93df77933d3bb32e62ef75f2a54d4c4788fb" {
		t.Fatal(e)
	}
	var v struct {
		Invalid []struct {
			Name     string
			Value    json.RawMessage `json:"md046_value"`
			Rejected bool            `json:"expected_rejected"`
		} `json:"invalid_configurations"`
	}
	if e = json.Unmarshal(b, &v); e != nil || len(v.Invalid) != 17 {
		t.Fatal(e)
	}
	r := registryFor(t, "MD046")
	for _, row := range v.Invalid {
		t.Run(row.Name, func(t *testing.T) {
			var value bytes.Buffer
			if e := json.Compact(&value, row.Value); e != nil {
				t.Fatal(e)
			}
			d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nrules: {MD046: "+value.String()+"}\n"))
			if e == nil {
				_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if e == nil || !row.Rejected {
				t.Fatal("invalid admitted", row.Name)
			}
		})
	}
	for _, text := range []string{"version: 1\nrules: {md046: {}}\n", "version: 1\nrules:\n  MD046: {}\n  MD046: {}\n", "version: 1\nrules: {MD046: {enabled: false, style: auto}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(text)
		}
	}
}

func TestMD046StyleDomain(t *testing.T) {
	r := registryFor(t, "MD046")
	for _, enabled := range []bool{false, true} {
		for _, style := range []string{"consistent", "fenced", "indented"} {
			d, e := DecodeYAML("fixture.yaml", []byte(fmt.Sprintf("version: 1\nrules: {MD046: {enabled: %v, style: %s}}\n", enabled, style)))
			if e == nil {
				_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if e != nil {
				t.Fatal(enabled, style, e)
			}
		}
	}
}
