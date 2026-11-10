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

func TestMD059FrozenInvalidConfigurations(t *testing.T) {
	b, e := os.ReadFile("../rules/md059/testdata/md059.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "3cbd8724ba09a6778ce53d5545e66d2dc391bea5389ad6baf60aad9156f411fd" {
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
	r := registryFor(t, "MD059")
	required := map[string]bool{"MD059: {phrase: [here]}": false, "MD059: {phrases: here}": false, "MD059: {phrases: [1]}": false, "MD059: {enabled: \"yes\"}": false, "MD059: {allow_directives: \"false\"}": false}
	for _, row := range v.Invalid {
		if _, ok := required[row.YAML]; ok {
			required[row.YAML] = true
		}
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
	for yaml, seen := range required {
		if !seen {
			t.Fatal("required invalid configuration missing from fixture", yaml)
		}
	}
	for _, text := range []string{"version: 1\nrules: {md059: {}}\n", "version: 1\nrules: {MD059: {phrases: [here], phrases: [more]}}\n", "version: 1\nrules:\n  MD059: {}\n  MD059: {}\n", "version: 1\nrules: {MD059: {enabled: false, labels: [here]}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(text)
		}
	}
	for _, text := range []string{"version: 1\nrules: {MD059: {}}\n", "version: 1\nrules: {MD059: {phrases: []}}\n", "version: 1\nrules: {MD059: {enabled: true, phrases: [link, \"  Click   HERE \", \"\"]}}\n", "version: 1\nrules: {MD059: {enabled: false, allow_directives: false}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e != nil {
			t.Fatal("valid configuration rejected", text, e)
		}
	}
}
