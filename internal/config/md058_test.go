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

func TestMD058FrozenInvalidConfigurations(t *testing.T) {
	b, e := os.ReadFile("../rules/testdata/md058.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "0329e5fe41d8e42d369e911602fe2085b3e9b002e7cd2beba0edb6ab45a4e095" {
		t.Fatal(e)
	}
	var v struct {
		Invalid []struct {
			Name string
			YAML string `json:"yaml"`
		} `json:"invalid_configurations"`
	}
	if e = json.Unmarshal(b, &v); e != nil || len(v.Invalid) != 6 {
		t.Fatal(e)
	}
	r := registryFor(t, "MD058")
	required := map[string]bool{"MD058: {unknown: true}": false, "MD058: {enabled: \"yes\"}": false, "MD058: {allow_directives: \"false\"}": false, "MD058: {enabled: false, unknown: true}": false}
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
	for _, text := range []string{"version: 1\nrules: {md058: {}}\n", "version: 1\nrules:\n  MD058: {}\n  MD058: {}\n", "version: 1\nflavor: commonmark\nrules: {MD058: {unknown: true}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(text)
		}
	}
	for _, text := range []string{"version: 1\nrules: {MD058: {}}\n", "version: 1\nrules: {MD058: {enabled: false}}\n", "version: 1\nflavor: commonmark\nrules: {MD058: {enabled: true}}\n", "version: 1\nflavor: gfm\nrules: {MD058: {enabled: true, allow_directives: false}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e != nil {
			t.Fatal("valid configuration rejected", text, e)
		}
	}
}
