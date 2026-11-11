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

func TestMD060FrozenInvalidConfigurations(t *testing.T) {
	b, e := os.ReadFile("../rules/md060/testdata/md060.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "4dc4a37c8333402b1a9c5fe63b32e0b8caed5217b56909aa29ff433d08bd5837" {
		t.Fatal(e)
	}
	var v struct {
		Invalid []struct {
			Name string
			YAML string `json:"yaml"`
		} `json:"invalid_configurations"`
	}
	if e = json.Unmarshal(b, &v); e != nil || len(v.Invalid) != 10 {
		t.Fatal(e)
	}
	r := registryFor(t, "MD060")
	required := map[string]bool{"MD060: {style: wide}": false, "MD060: {style: 1}": false, "MD060: {styles: aligned}": false, "MD060: {enabled: \"yes\"}": false, "MD060: {allow_directives: \"false\"}": false}
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
	for _, text := range []string{"version: 1\nrules: {md060: {}}\n", "version: 1\nrules:\n  MD060: {}\n  MD060: {}\n", "version: 1\nrules: {MD060: {enabled: false, style: wide}}\n", "version: 1\nrules: {MD060: {style: Aligned}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(text)
		}
	}
	for _, text := range []string{"version: 1\nrules: {MD060: {}}\n", "version: 1\nrules: {MD060: {style: tight}}\n", "version: 1\nrules: {MD060: {enabled: false}}\n", "version: 1\nflavor: gfm\nrules: {MD060: {style: compact}}\n", "version: 1\nflavor: gfm\nrules: {MD060: {enabled: true, style: aligned, allow_directives: false}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e != nil {
			t.Fatal("valid configuration rejected", text, e)
		}
	}
}
