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

func TestMD055FrozenInvalidConfigurations(t *testing.T) {
	b, e := os.ReadFile("../rules/md055/testdata/md055.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "5439144ca04395f31749913a620651b8c6c82ca67d33b4f729533df89b1dbefa" {
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
	r := registryFor(t, "MD055")
	required := map[string]bool{"MD055: {style: both}": false, "MD055: {style: 1}": false, "MD055: {styles: none}": false, "MD055: {enabled: \"yes\"}": false, "MD055: {allow_directives: \"false\"}": false}
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
	for _, text := range []string{"version: 1\nrules: {md055: {}}\n", "version: 1\nrules:\n  MD055: {}\n  MD055: {}\n", "version: 1\nrules: {MD055: {enabled: false, style: both}}\n", "version: 1\nrules: {MD055: {style: Consistent}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(text)
		}
	}
	for _, text := range []string{"version: 1\nrules: {MD055: {}}\n", "version: 1\nrules: {MD055: {style: none}}\n", "version: 1\nrules: {MD055: {enabled: false}}\n", "version: 1\nflavor: gfm\nrules: {MD055: {style: consistent}}\n", "version: 1\nflavor: gfm\nrules: {MD055: {enabled: true, style: leading_and_trailing, allow_directives: false}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e != nil {
			t.Fatal("valid configuration rejected", text, e)
		}
	}
}
