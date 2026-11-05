package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

func TestMD052FrozenInvalidConfigurations(t *testing.T) {
	b, e := os.ReadFile("../rules/testdata/md052.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "b4d56f7f2b7f4485ad3a2b1b9ec3b4834e69674a7dc6db525c01e2a804470d2e" {
		t.Fatal(e)
	}
	var v struct {
		Invalid []struct {
			Name string
			YAML string `json:"yaml"`
		} `json:"invalid_configurations"`
	}
	if e = json.Unmarshal(b, &v); e != nil || len(v.Invalid) != 9 {
		t.Fatal(e)
	}
	r := registryFor(t, "MD052")
	for _, row := range v.Invalid {
		t.Run(row.Name, func(t *testing.T) {
			d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nrules:\n  "+strings.ReplaceAll(row.YAML, `\n`, "\n")+"\n"))
			if e == nil {
				_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if e == nil {
				t.Fatal("invalid admitted", row.Name)
			}
		})
	}
	for _, text := range []string{"version: 1\nrules: {md052: {}}\n", "version: 1\nrules: {MD052: {style: backtick, style: tilde}}\n", "version: 1\nrules:\n  MD052: {}\n  MD052: {}\n", "version: 1\nrules: {MD052: {enabled: false, style: auto}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(text)
		}
	}
}
