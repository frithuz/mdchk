package config

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/idna"
)

func TestMD095FrozenInvalidConfigurations(t *testing.T) {
	b, e := os.ReadFile("../rules/testdata/md095.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "20677564569fb578707dfca8c2d4b09a5ad5654acf42ef516892f5180b8d540f" {
		t.Fatal(e)
	}
	var v struct {
		Invalid []struct {
			Name, Path string
			YAML       string `json:"yaml"`
		} `json:"invalid_configurations"`
	}
	if e = json.Unmarshal(b, &v); e != nil || len(v.Invalid) != 16 {
		t.Fatal(e)
	}
	providers := engine.ValidationProviders{ValidateHostname: idna.ValidateHostname}
	for _, row := range v.Invalid {
		t.Run(row.Name, func(t *testing.T) {
			d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nrules:\n  "+row.YAML+"\n"))
			if e == nil {
				_, e = ValidateDefinition(d, registryFor(t, "MD095"), providers)
			}
			// Every rejection is located on the rule's line and names the setting.
			if e == nil || !strings.Contains(e.Error(), "fixture.yaml:3:") || !strings.Contains(e.Error(), "("+row.Path+")") {
				t.Fatal(e)
			}
		})
	}
}

func TestMD095AllowedHostsConfiguration(t *testing.T) {
	providers := engine.ValidationProviders{ValidateHostname: idna.ValidateHostname}
	hosts := []string{"example.org", "exаmple.org", "xn--exmple-4nf.org", "EXАMPLE.ORG", "example.org.", "Faß.de"}
	for _, enabled := range []string{"false", "true"} {
		source := "version: 1\nrules:\n  MD095:\n    enabled: " + enabled + "\n    allowed_hosts: [example.org, \"exаmple.org\", xn--exmple-4nf.org, \"EXАMPLE.ORG\", example.org., \"Faß.de\"]\n"
		v, err := ValidateDefinition(yamlFor(t, source), registryFor(t, "MD095"), providers)
		if err != nil {
			t.Fatal(err)
		}
		// Entries keep the spelling written; they are neither trimmed nor canonicalized.
		if got := v.rules["MD095"].Options["allowed_hosts"]; !slices.Equal(got.([]string), hosts) {
			t.Fatal(got)
		}
	}
	for i, host := range []string{"https://example.org", " example.org", "example.org\t", "*.example.org", "example.org:443", "user@example.org", "[::1]"} {
		_, err := registryFor(t, "MD095").ResolveSettings(engine.Settings{"MD095": {Options: engine.Options{"allowed_hosts": []string{"example.org", host}}}}, providers)
		var setting *engine.SettingError
		if !errors.As(err, &setting) || setting.RuleID != "MD095" || setting.Option != "allowed_hosts" || setting.Element == nil || *setting.Element != 1 {
			t.Fatal(i, host, err)
		}
	}
}
