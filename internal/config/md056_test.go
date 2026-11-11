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

func TestMD056FrozenInvalidConfigurations(t *testing.T) {
	b, e := os.ReadFile("../rules/md056/testdata/md056.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "99f108a6a78b9078e645d3c73b2697dec3c812958b3c124680b3b8dc4ff55dae" {
		t.Fatal(e)
	}
	var v struct {
		Invalid []struct {
			Name string
			YAML string `json:"yaml"`
		} `json:"invalid_configurations"`
	}
	if e = json.Unmarshal(b, &v); e != nil || len(v.Invalid) != 8 {
		t.Fatal(e)
	}
	r := registryFor(t, "MD056")
	required := map[string]bool{"MD056: {style: x}": false, "MD056: {unknown: 1}": false, "MD056: {enabled: \"yes\"}": false, "MD056: {allow_directives: \"false\"}": false, "MD056: {enabled: true, enabled: true}": false}
	for _, row := range v.Invalid {
		if _, ok := required[row.YAML]; ok {
			required[row.YAML] = true
		}
		t.Run(row.Name, func(t *testing.T) {
			d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nflavor: gfm\nrules:\n  "+strings.ReplaceAll(row.YAML, `\n`, "\n")+"\n"))
			if e == nil {
				_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			if e == nil {
				t.Fatal("invalid admitted", row.Name)
			}
		})
	}
	for yaml, seen := range required {
		if !seen {
			t.Fatal("required invalid configuration missing from fixture", yaml)
		}
	}
	for _, text := range []string{"version: 1\nrules: {md056: {}}\n", "version: 1\nrules:\n  MD056: {}\n  MD056: {}\n", "version: 1\nrules: {MD056: {enabled: false, style: x}}\n", "version: 1\nrules: {MD056: {allow_directives: 1}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(text)
		}
	}
	for _, text := range []string{"version: 1\nrules: {MD056: {}}\n", "version: 1\nrules: {MD056: {enabled: false}}\n", "version: 1\nflavor: gfm\nrules: {MD056: {enabled: true, allow_directives: false}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e != nil {
			t.Fatal("valid configuration rejected", text, e)
		}
	}
}
