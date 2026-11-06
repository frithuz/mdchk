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

func TestMD075FrozenInvalidConfigurations(t *testing.T) {
	b, e := os.ReadFile("../rules/testdata/md075.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "ab1f0cdd6c5e51fea5e3d7a9ea02bc4417c9d1ca264b2a5e26cbed3afa12b497" {
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
	r := registryFor(t, "MD075")
	required := map[string]bool{"MD075: {style: x}": false, "MD075: {unknown: 1}": false, "MD075: {enabled: \"yes\"}": false, "MD075: {allow_directives: \"false\"}": false, "MD075: {enabled: true, enabled: true}": false}
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
	for _, text := range []string{"version: 1\nrules: {md075: {}}\n", "version: 1\nrules:\n  MD075: {}\n  MD075: {}\n", "version: 1\nrules: {MD075: {enabled: false, style: x}}\n", "version: 1\nrules: {MD075: {allow_directives: 1}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(text)
		}
	}
	for _, text := range []string{"version: 1\nrules: {MD075: {}}\n", "version: 1\nrules: {MD075: {enabled: false}}\n", "version: 1\nflavor: gfm\nrules: {MD075: {enabled: true, allow_directives: false}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e != nil {
			t.Fatal("valid configuration rejected", text, e)
		}
	}
}
