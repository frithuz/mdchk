package config

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

func TestFrontmatterSettingsIndependentOfFlavor(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name    string
			setting map[string]any
			enabled bool
			formats []string
		}{
			{"omitted", nil, false, []string{"yaml", "toml"}},
			{"empty", map[string]any{}, false, []string{"yaml", "toml"}},
			{"disabled", map[string]any{"enabled": false}, false, []string{"yaml", "toml"}},
			{"enabled", map[string]any{"enabled": true}, true, []string{"yaml", "toml"}},
			{"yaml", map[string]any{"enabled": true, "formats": []string{"yaml"}}, true, []string{"yaml"}},
			{"toml", map[string]any{"enabled": true, "formats": []string{"toml"}}, true, []string{"toml"}},
			{"disabled-subset", map[string]any{"enabled": false, "formats": []string{"toml"}}, false, []string{"toml"}},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				object := map[string]any{"version": 1, "flavor": flavor}
				if tc.setting != nil {
					object["frontmatter"] = tc.setting
				}
				raw, err := json.Marshal(object)
				if err != nil {
					t.Fatal(err)
				}
				// JSON is a YAML subset; the same object exercises all three
				// supported configuration acquisition boundaries.
				for _, decode := range []struct {
					name string
					call func() (*Definition, error)
				}{
					{"yaml", func() (*Definition, error) { return DecodeYAML("frontmatter.yaml", raw) }},
					{"json", func() (*Definition, error) { return DecodeJSON(raw) }},
					{"object", func() (*Definition, error) { return DecodeObject(object) }},
				} {
					t.Run(decode.name, func(t *testing.T) {
						definition, err := decode.call()
						if err != nil {
							t.Fatal(err)
						}
						validated, err := ValidateDefinition(definition, registryFor(t), engine.ValidationProviders{})
						if err != nil {
							t.Fatal(err)
						}
						caps := baseCaps()
						caps.FrontmatterYAML, caps.FrontmatterTOML = true, true
						admitted, err := validated.Admit(caps, Permissions{})
						if err != nil {
							t.Fatal(err)
						}
						for _, settings := range []Builtins{admitted.Settings, validated.Configuration(caps).Settings} {
							if settings.Flavor != flavor || settings.FrontmatterEnabled != tc.enabled || !reflect.DeepEqual(settings.FrontmatterFormats, tc.formats) {
								t.Fatalf("frontmatter changed with flavor/acquisition: %+v", settings)
							}
						}
						_, err = validated.Admit(baseCaps(), Permissions{})
						if tc.enabled {
							assertPath(t, err, "frontmatter.enabled")
						} else if err != nil {
							t.Fatalf("disabled recognition requires a capability: %v", err)
						}
					})
				}
			})
		}
	}
}
