package config

import (
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

func TestMD057ConfigurationAdmission(t *testing.T) {
	r := registryFor(t, "MD057")
	for _, row := range []struct {
		value                  string
		enabled, allow, images bool
	}{
		{"{}", true, true, true},
		{"{check_images: true}", true, true, true},
		{"{check_images: false}", true, true, false},
		{"{enabled: false}", false, true, true},
		{"{enabled: false, check_images: false}", false, true, false},
		{"{allow_directives: false}", true, false, true},
		{"{enabled: true, allow_directives: true}", true, true, true},
	} {
		t.Run(row.value, func(t *testing.T) {
			d, e := DecodeYAML("fixture.yaml", []byte("version: 1\ndirectives: {enabled: true}\nrules: {MD057: "+row.value+"}\n"))
			if e != nil {
				t.Fatal(e)
			}
			v, e := ValidateDefinition(d, r, engine.ValidationProviders{})
			if e != nil {
				t.Fatal(e)
			}
			resolution, e := v.Admit(baseCaps(), Permissions{})
			if e != nil {
				t.Fatal(e)
			}
			rule := resolution.ExecutionPlan().Rules()["MD057"]
			if !rule.Available || rule.Enabled != row.enabled || rule.AllowDirectives != row.allow || rule.Options["check_images"] != row.images || len(rule.Options) != 1 {
				t.Fatalf("%+v", rule)
			}
		})
	}
	for _, row := range []struct{ value, path string }{
		{"{check_images: \"yes\"}", "rules.MD057.check_images"},
		{"{check_images: 1}", "rules.MD057.check_images"},
		{"{check_images: null}", "rules.MD057.check_images"},
		{"{check_images: [true]}", "rules.MD057.check_images[0]"},
		{"{index_file: index.md}", "rules.MD057.index_file"},
		{"{enabled: false, index_file: index.md}", "rules.MD057.index_file"},
		{"{images: true}", "rules.MD057.images"},
		{"{enabled: 'false'}", "rules.MD057.enabled"},
		{"{allow_directives: 1}", "rules.MD057.allow_directives"},
	} {
		t.Run("invalid/"+row.value, func(t *testing.T) {
			d, e := DecodeYAML("fixture.yaml", []byte("version: 1\nrules: {MD057: "+row.value+"}\n"))
			if e == nil {
				_, e = ValidateDefinition(d, r, engine.ValidationProviders{})
			}
			// Decode and validation errors both name the offending property path.
			if e == nil || !strings.Contains(e.Error(), "("+row.path+")") {
				t.Fatal(e, row.path)
			}
		})
	}
}
