package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

func TestJSONYAMLObjectParity(t *testing.T) {
	cases := []struct {
		name, yaml, raw string
		object          map[string]any
		path            string
	}{
		{"valid", "version: 1\nflavor: commonmark\ndirectives: {enabled: false}\nresources: {}\nrules: {MD013: {enabled: false, length: 80}}\n", `{"version":1,"flavor":"commonmark","directives":{"enabled":false},"resources":{},"rules":{"MD013":{"enabled":false,"length":80}}}`, map[string]any{"version": 1, "flavor": "commonmark", "directives": map[string]any{"enabled": false}, "resources": map[string]any{}, "rules": map[string]any{"MD013": map[string]any{"enabled": false, "length": 80}}}, ""},
		{"unknown", "version: 1\nother: true\n", `{"version":1,"other":true}`, map[string]any{"version": 1, "other": true}, "other"},
		{"nested", "version: 1\nresources: {other: true}\n", `{"version":1,"resources":{"other":true}}`, map[string]any{"version": 1, "resources": map[string]any{"other": true}}, "resources.other"},
		{"domain", "version: 1\nrules: {MD013: {enabled: false, length: -1}}\n", `{"version":1,"rules":{"MD013":{"enabled":false,"length":-1}}}`, map[string]any{"version": 1, "rules": map[string]any{"MD013": map[string]any{"enabled": false, "length": -1}}}, "rules.MD013.length"},
		{"removed", "version: 1\nrules: {MD057: {enabled: false, index_file: x}}\n", `{"version":1,"rules":{"MD057":{"enabled":false,"index_file":"x"}}}`, map[string]any{"version": 1, "rules": map[string]any{"MD057": map[string]any{"enabled": false, "index_file": "x"}}}, "rules.MD057.index_file"},
		{"canonical", "version: 1\nrules: {md001: {}}\n", `{"version":1,"rules":{"md001":{}}}`, map[string]any{"version": 1, "rules": map[string]any{"md001": map[string]any{}}}, "rules.md001"},
		{"reserved", "version: 1\nrules: {MD002: {}}\n", `{"version":1,"rules":{"MD002":{}}}`, map[string]any{"version": 1, "rules": map[string]any{"MD002": map[string]any{}}}, "rules.MD002"},
		{"list", "version: 1\nexclude: [true]\n", `{"version":1,"exclude":[true]}`, map[string]any{"version": 1, "exclude": []any{true}}, "exclude[0]"},
		{"null", "version: 1\nresources: null\n", `{"version":1,"resources":null}`, map[string]any{"version": 1, "resources": nil}, "resources"},
		{"number", "version: 1.0\n", `{"version":1.0}`, map[string]any{"version": float64(1)}, "version"},
	}
	r := registryFor(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			type outcome struct {
				definition *Definition
				err        error
			}
			yd, ye := DecodeYAML("parity.yaml", []byte(tc.yaml))
			jd, je := DecodeJSON([]byte(tc.raw))
			od, oe := DecodeObject(tc.object)
			var shared *result.ConfigurationData
			var reason string
			for _, o := range []outcome{{yd, ye}, {jd, je}, {od, oe}} {
				err := o.err
				var v *Validated
				if err == nil {
					v, err = ValidateDefinition(o.definition, r, engine.ValidationProviders{})
				}
				if tc.path != "" {
					path, message := errorProperty(t, err)
					if path != tc.path {
						t.Fatalf("path=%s want %s: %v", path, tc.path, err)
					}
					if reason == "" {
						reason = message
					} else if reason != message {
						t.Fatalf("different reasons %q / %q", reason, message)
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				got := v.Configuration(Capabilities{}).Shared()
				got.Sources = nil
				got.Provenance = nil
				if shared == nil {
					shared = &got
				} else if !reflect.DeepEqual(*shared, got) {
					t.Fatal("semantic projection differs")
				}
				if _, err = v.Admit(Capabilities{CommonMark: true}, Permissions{}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
func errorProperty(t *testing.T, err error) (string, string) {
	t.Helper()
	var d *DecodeError
	var v *ValidationError
	if errors.As(err, &d) {
		return d.Origin.PropertyPath, d.Message
	}
	if errors.As(err, &v) {
		return v.Origin.PropertyPath, v.Err.Error()
	}
	t.Fatalf("missing located error: %v", err)
	return "", ""
}
func TestJSONRejectsWithoutPartialDefinition(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `1`, `{"version":1} {}`, `{"version":1,"version":1}`, `{"version":1,"rules":{"MD013":{"length":null}}}`, `{"version":1e0}`, `{"version":"1"}`, `{"version":null}`, `{"version":1,"exclude":null}`, `{"version":1,"resources":{"index_file":"\ud800"}}`} {
		d, e := DecodeJSON([]byte(raw))
		if e == nil {
			if d == nil {
				t.Fatal("nil success")
			}
			_, e = ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
		}
		if e == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range [][]byte{nil, {0xff}, []byte(strings.Repeat(" ", configBytes+1))} {
		if d, e := DecodeJSON(raw); d != nil || e == nil {
			t.Fatal(d, e)
		}
	}
}
func TestPreAdmissionProvenanceAndOwnership(t *testing.T) {
	d := yamlFor(t, "version: 1\nflavor: commonmark\nresources: {index_file: index.md}\nrules: {MD001: {}, MD013: {enabled: true, length: 80}}\nfrontmatter: {formats: &formats [yaml, toml]}\nexclude: *formats\n")
	v, e := ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
	if e != nil {
		t.Fatal(e)
	}
	view := v.Configuration(Capabilities{CommonMark: true})
	for path, want := range map[string]bool{"version": true, "flavor": true, "resources.index_file": true, "resources.anchor_style": false, "rules.MD001": true, "rules.MD001.enabled": false, "rules.MD013.enabled": true, "rules.MD013.length": true, "rules.MD013.allow_directives": false} {
		if view.Provenance[path].Explicit != want {
			t.Errorf("%s explicit=%v", path, !want)
		}
	}
	if !view.Rules["MD013"].Enabled || view.Rules["MD013"].Available || !view.Rules["MD001"].Explicit {
		t.Fatal(view.Rules)
	}
	if _, e = v.Admit(Capabilities{CommonMark: true, DirectiveProcessing: true, ResourceIndex: true, RepresentIndexFile: func(string) error { return nil }}, Permissions{}); e == nil {
		t.Fatal("admitted unavailable explicit rule")
	} else {
		assertPath(t, e, "rules.MD001")
	}
	if view.Provenance["exclude[0]"].Origin.Position.Line != 6 {
		t.Fatal("lost alias use site")
	}
	shared := view.Shared()
	encoded, e := json.Marshal(shared)
	if e != nil || !json.Valid(encoded) {
		t.Fatal(e)
	}
	for _, p := range shared.Provenance {
		if strings.Contains(p.Pointer, ".") {
			t.Fatal(p.Pointer)
		}
		if p.SourceKind == "default" && (p.Range != nil || p.Path != "") {
			t.Fatal("invented default source")
		}
	}
	want := v.Configuration(Capabilities{CommonMark: true}).Shared()
	view.Settings.FrontmatterFormats[0] = "changed"
	view.Rules["MD054"].Options["allowed_styles"].([]string)[0] = "changed"
	provenance := view.Provenance["rules.MD013.enabled"]
	provenance.Available = true
	view.Provenance["rules.MD013.enabled"] = provenance
	shared.Effective["availability"].(map[string]bool)["/flavor"] = false
	view.Provenance["version"].Origin.Position.Line = 100
	rule := view.Rules["MD013"]
	rule.Options["length"] = 900
	view.Rules["MD013"] = rule
	shared.Effective["resources"].(map[string]any)["index_file"] = "changed"
	shared.Provenance[0].Pointer = "bad"
	d.Provenance["version"].Position.Line = 200
	if got := v.Configuration(Capabilities{CommonMark: true}).Shared(); !reflect.DeepEqual(got, want) {
		t.Fatal("view/input mutation changed validated snapshot")
	}
	if propertyPointer("rules.a~/b.enabled") != "/rules/a~0~1b/enabled" {
		t.Fatal("JSON Pointer escaping")
	}
}
func TestJSONRequestsAreIsolatedAndSupplied(t *testing.T) {
	for i := 0; i < 3; i++ {
		for _, raw := range []string{`{"version":1,"rules":{"MD013":{"enabled":true}}}`, `{"version":1,"bad":1}`, `{"version":1}`} {
			d, e := DecodeJSON([]byte(raw))
			if strings.Contains(raw, "bad") {
				if e == nil {
					t.Fatal("accepted bad")
				}
				continue
			}
			if e != nil {
				t.Fatal(e)
			}
			v, e := ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
			if e != nil {
				t.Fatal(e)
			}
			view := v.Configuration(Capabilities{})
			if view.Rules["MD013"].Enabled != strings.Contains(raw, "MD013") {
				t.Fatal("cross-call rule state")
			}
			for _, p := range view.Shared().Provenance {
				if p.Range != nil || p.Path != "" {
					t.Fatal("fabricated supplied source")
				}
			}
		}
	}
}

func TestJSONAndYAMLCapabilityParity(t *testing.T) {
	for _, tc := range []struct{ name, yaml, raw, path string }{
		{"explicit rule", "rules: {MD013: {enabled: true}}", `"rules":{"MD013":{"enabled":true}}`, "rules.MD013"},
		{"index", "resources: {index_file: index.md}", `"resources":{"index_file":"index.md"}`, "resources.index_file"},
		{"anchors", "resources: {anchor_style: github}", `"resources":{"anchor_style":"github"}`, "resources.anchor_style"},
		{"frontmatter", "frontmatter: {enabled: true}", `"frontmatter":{"enabled":true}`, "frontmatter.enabled"},
		{"quarto", "integrations: {quarto: {enabled: true}}", `"integrations":{"quarto":{"enabled":true}}`, "integrations.quarto.enabled"},
		{"slides", "documents: {slides: ['slides/**']}", `"documents":{"slides":["slides/**"]}`, "documents.slides"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			yd, ye := DecodeYAML("cap.yaml", []byte("version: 1\nflavor: commonmark\ndirectives: {enabled: false}\n"+tc.yaml+"\n"))
			jd, je := DecodeJSON([]byte(`{"version":1,"flavor":"commonmark","directives":{"enabled":false},` + tc.raw + `}`))
			if ye != nil || je != nil {
				t.Fatal(ye, je)
			}
			for _, d := range []*Definition{yd, jd} {
				v, e := ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
				if e != nil {
					t.Fatal(e)
				}
				if v.Configuration(Capabilities{CommonMark: true}).Provenance[tc.path].Available {
					t.Fatal("unavailable behavior marked available")
				}
				_, e = v.Admit(Capabilities{CommonMark: true}, Permissions{})
				assertPath(t, e, tc.path)
				_, reason := errorProperty(t, e)
				if !strings.Contains(reason, "unavailable") {
					t.Fatal(reason)
				}
			}
		})
	}
	// Corrected controls isolate the feature under test from unrelated defaults.
	for _, raw := range []string{`{}`, `{"resources":{}}`, `{"rules":{"MD013":{"enabled":false}}}`} {
		raw = `{"version":1,"flavor":"commonmark","directives":{"enabled":false},` + strings.TrimPrefix(raw, "{")
		raw = strings.ReplaceAll(raw, ",}", "}")
		d, e := DecodeJSON([]byte(raw))
		if e != nil {
			t.Fatal(e)
		}
		v, e := ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = v.Admit(Capabilities{CommonMark: true}, Permissions{}); e != nil {
			t.Fatal(e)
		}
	}
}

func TestSuppliedJSONIgnoresHostConfigurationAndOwnsInput(t *testing.T) {
	t.Chdir(t.TempDir())
	if e := os.WriteFile(Filename, []byte("version: invalid\n"), 0600); e != nil {
		t.Fatal(e)
	}
	raw := []byte(`{"version":1,"flavor":"commonmark","directives":{"enabled":false}}`)
	d, e := DecodeJSON(raw)
	if e != nil {
		t.Fatal(e)
	}
	for i := range raw {
		raw[i] = 'x'
	}
	v, e := ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = v.Admit(Capabilities{CommonMark: true}, Permissions{}); e != nil {
		t.Fatal(e)
	}
	view := v.Configuration(Capabilities{CommonMark: true})
	if view.Provenance["rules.MD013.enabled"].Explicit || view.Rules["MD013"].Enabled {
		t.Fatal("default-disabled MD013 changed")
	}
	if view.Selected.Path != "" || view.Selected.Position != nil {
		t.Fatal("invented supplied identity")
	}
}
func TestSharedConfigurationSchema(t *testing.T) {
	python := os.Getenv("MDCHK_RESULT_VALIDATOR_PYTHON")
	if python == "" {
		t.Skip("pinned schema validator not requested")
	}
	d, e := DecodeJSON([]byte(`{"version":1,"resources":{"index_file":"index.md"},"rules":{"MD013":{"enabled":true}}}`))
	if e != nil {
		t.Fatal(e)
	}
	v, e := ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
	if e != nil {
		t.Fatal(e)
	}
	got := v.Configuration(Capabilities{}).Shared()
	content, e := json.Marshal(got)
	if e != nil {
		t.Fatal(e)
	}
	again, e := json.Marshal(v.Configuration(Capabilities{}).Shared())
	if e != nil || !bytes.Equal(content, again) {
		t.Fatal("unstable shared serialization", e)
	}
	schema, e := filepath.Abs("../../docs/contracts/results/result.schema.json")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	script := `import json,sys
from jsonschema import Draft202012Validator
schema=json.load(open(sys.argv[1]))
Draft202012Validator({"$ref":"#/$defs/Configuration","$defs":schema["$defs"]}).validate(json.load(sys.stdin))`
	command := exec.CommandContext(ctx, python, "-B", "-c", script, schema) //nolint:gosec // Explicit test interpreter; fixed validator code and checked-in schema path.
	command.Stdin = bytes.NewReader(content)
	if output, e := command.CombinedOutput(); e != nil {
		t.Fatalf("shared Configuration schema: %v: %s", e, output)
	}
}

func TestMetadataOptionAvailabilityMatchesAdmission(t *testing.T) {
	ids := []string{"MD001", "MD025", "MD041"}
	registry := registryFor(t, ids...) // Registers real callbacks; inspection must not invoke them.
	for _, id := range ids {
		for _, enabled := range []bool{false, true} {
			for _, title := range []bool{false, true} {
				for _, metadata := range []bool{false, true} {
					name := fmt.Sprintf("%s/enabled=%t/title=%t/metadata=%t", id, enabled, title, metadata)
					t.Run(name, func(t *testing.T) {
						raw := fmt.Sprintf(`{"version":1,"flavor":"commonmark","directives":{"enabled":false},"rules":{"%s":{"enabled":%t,"frontmatter_title":%t}}}`, id, enabled, title)
						d, err := DecodeJSON([]byte(raw))
						if err != nil {
							t.Fatal(err)
						}
						v, err := ValidateDefinition(d, registry, engine.ValidationProviders{})
						if err != nil {
							t.Fatal(err)
						}
						caps := Capabilities{CommonMark: true, MetadataInterpretation: metadata}
						view := v.Configuration(caps)
						path := "rules." + id + ".frontmatter_title"
						want := !title || metadata
						if got := view.Provenance[path]; !got.Explicit || got.Available != want {
							t.Fatalf("option provenance=%+v want explicit availability %t", got, want)
						}
						if rule := view.Rules[id]; !rule.Available || rule.Enabled != enabled {
							t.Fatalf("option capability changed registration/enablement: %+v", rule)
						}
						shared := view.Shared()
						pointer := "/rules/" + id + "/frontmatter_title"
						if got := shared.Effective["availability"].(map[string]bool)[pointer]; got != want {
							t.Fatalf("shared option availability=%t want %t", got, want)
						}
						if got := shared.Effective["rules"].(map[string]any)[id].(map[string]any)["available"]; got != true {
							t.Fatalf("shared rule registration=%v", got)
						}
						found := false
						for _, origin := range shared.Provenance {
							if origin.Pointer == pointer {
								found = true
								if origin.SourceKind != "supplied-config" {
									t.Fatal(origin)
								}
							}
						}
						if !found {
							t.Fatal("missing option source")
						}
						_, err = v.Admit(caps, Permissions{})
						if enabled && !want {
							assertPath(t, err, path)
						} else if err != nil {
							t.Fatal(err)
						}
					})
				}
			}
		}
	}
	// Capability assertions alone cannot turn an absent rule implementation into
	// an available option, even when the inactive option's metadata support exists.
	for _, id := range ids {
		d, err := DecodeJSON([]byte(fmt.Sprintf(`{"version":1,"rules":{"%s":{"enabled":false,"frontmatter_title":true}}}`, id)))
		if err != nil {
			t.Fatal(err)
		}
		v, err := ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
		if err != nil {
			t.Fatal(err)
		}
		if v.Configuration(Capabilities{MetadataInterpretation: true}).Provenance["rules."+id+".frontmatter_title"].Available {
			t.Fatal("metadata fabricated executable option availability", id)
		}
	}
}
