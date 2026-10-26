package config

import (
	"errors"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

func TestMD080SharedStyleAdmissionAndPlanPolicy(t *testing.T) {
	r := registryFor(t, "MD080")
	caps := baseCaps()
	caps.GitHubAnchors, caps.ExplicitAnchors = true, true
	for _, style := range []string{"github", "explicit"} {
		v, err := ValidateDefinition(yamlFor(t, "version: 1\nflavor: commonmark\nresources: {anchor_style: "+style+"}\nrules: {MD080: {enabled: true}}\n"), r, engine.ValidationProviders{})
		if err != nil {
			t.Fatal(err)
		}
		resolution, err := v.Admit(caps, Permissions{})
		if err != nil {
			t.Fatal(err)
		}
		if resolution.Settings.AnchorStyle != style || resolution.ExecutionPlan().engine.Context().AnchorStyle != style || resolution.ExecutionPlan().CandidatePlan().Context().AnchorStyle != style {
			t.Fatal(resolution)
		}
		resolution.Settings.AnchorStyle = "changed"
		if resolution.ExecutionPlan().engine.Context().AnchorStyle != style {
			t.Fatal("mutable resolution altered plan")
		}
	}
	for n, body := range []string{"resources: {anchor_style: other}", "resources: {anchor_style: null}", "resources: {anchor_style: 1}", "rules: {MD080: {enabled: false, anchor_style: github}}"} {
		definition, err := DecodeYAML("fixture.yaml", []byte("version: 1\n"+body+"\n"))
		if err == nil {
			_, err = ValidateDefinition(definition, r, engine.ValidationProviders{})
		}
		var located *ValidationError
		if err == nil || n == 0 && (!errors.As(err, &located) || located.Origin.PropertyPath != "resources.anchor_style") {
			t.Fatal(body, err)
		}
	}
	v, err := ValidateDefinition(yamlFor(t, "version: 1\nresources: {anchor_style: explicit, index_file: index.md}\n"), r, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = v.Admit(caps, Permissions{})
	assertPath(t, err, "resources.index_file")
}
