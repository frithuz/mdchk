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

func TestMD054FrozenInvalidConfigurations(t *testing.T) {
	b, e := os.ReadFile("../rules/md054/testdata/md054.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "c064141c21b18bae99ebca4a7b252f8d5d78e976873a9ba0b9b8199227e698bf" {
		t.Fatal(e)
	}
	var v struct {
		Invalid []struct {
			Name string
			YAML string `json:"yaml"`
		} `json:"invalid_configurations"`
	}
	if e = json.Unmarshal(b, &v); e != nil || len(v.Invalid) != 14 {
		t.Fatal(e)
	}
	r := registryFor(t, "MD054")
	required := map[string]bool{"MD054: {allowed_styles: []}": false, "MD054: {allowed_styles: [reference]}": false, "MD054: {allowed_styles: [Inline]}": false, "MD054: {allowed_styles: inline}": false, "MD054: {style: inline}": false, "MD054: {enabled: \"yes\"}": false, "MD054: {allow_directives: \"false\"}": false}
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
	for _, text := range []string{"version: 1\nrules: {md054: {}}\n", "version: 1\nrules: {MD054: {allowed_styles: [inline], allowed_styles: [autolink]}}\n", "version: 1\nrules:\n  MD054: {}\n  MD054: {}\n", "version: 1\nrules: {MD054: {enabled: false, style: auto}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(text)
		}
	}
	for _, text := range []string{"version: 1\nrules: {MD054: {}}\n", "version: 1\nrules: {MD054: {enabled: true, allowed_styles: [full_reference]}}\n", "version: 1\nrules: {MD054: {allowed_styles: [inline, autolink], allow_directives: false}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e != nil {
			t.Fatal("valid configuration rejected", text, e)
		}
	}
}
