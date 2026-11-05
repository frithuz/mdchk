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

func TestMD062FrozenInvalidConfigurations(t *testing.T) {
	b, e := os.ReadFile("../rules/testdata/md062.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "bca8967f53ceb7739dddc48e6387506a860c5aff3d83999e86a96218fde66b61" {
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
	r := registryFor(t, "MD062")
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
	for _, text := range []string{"version: 1\nrules: {md062: {}}\n", "version: 1\nrules: {MD062: {style: backtick, style: tilde}}\n", "version: 1\nrules:\n  MD062: {}\n  MD062: {}\n", "version: 1\nrules: {MD062: {enabled: false, style: auto}}\n"} {
		d, e := DecodeYAML("fixture.yaml", []byte(text))
		if e == nil {
			_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
		}
		if e == nil {
			t.Fatal(text)
		}
	}
}
