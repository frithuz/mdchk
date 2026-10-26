package config

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

func TestMD043DescriptorLocationEvenDisabled(t *testing.T) {
	for _, enabled := range []string{"false", "true"} {
		source := "version: 1\nrules:\n  MD043:\n    enabled: " + enabled + "\n    headings: [\"# A\", \"bad\"]\n"
		root := discoveryRoot(t)
		path := discoveryFile(t, root, Filename, source)
		_, err := Resolve(SelectionOptions{WorkingDirectory: root, BoundaryDirectory: root}, registryFor(t, "MD043"), engine.ValidationProviders{}, baseCaps())
		var v *ValidationError
		var s *engine.SettingError
		if !errors.As(err, &v) || !errors.As(err, &s) || v.Origin.Path != path || v.Origin.PropertyPath != "rules.MD043.headings[1]" || v.Origin.Position == nil || v.Origin.Position.Line != 5 || v.Origin.Position.Column != 23 || s.Element == nil || *s.Element != 1 || s.Option != "headings" || s.RuleID != "MD043" {
			t.Fatal(err, v, s)
		}
		//nolint:gosec // Only the fixture's own temporary path is read.
		if b, e := os.ReadFile(path); e != nil || string(b) != source {
			t.Fatal(e, string(b))
		}
	}
	jsonSource := `{"version":1,"rules":{"MD043":{"enabled":false,"headings":["# A","bad"]}}}`
	d, err := DecodeJSON([]byte(jsonSource))
	if err != nil {
		t.Fatal(err)
	}
	_, err = ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
	assertPath(t, err, "rules.MD043.headings[1]")
}

func TestMD043DescriptorSnapshotsAndDefaults(t *testing.T) {
	input := []string{"# A", "*", "## B"}
	r := registryFor(t, "MD043")
	off := false
	got, err := r.ResolveSettings(engine.Settings{"MD043": {Enabled: &off, Options: engine.Options{"headings": input}}}, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	input[0] = "# changed"
	if !reflect.DeepEqual(got["MD043"].Options["headings"], []string{"# A", "*", "## B"}) {
		t.Fatal(got)
	}
	source := "version: 1\nrules:\n  MD043: {enabled: true, headings: ['# ', '*', '###### B']}\n"
	v, err := ValidateDefinition(yamlFor(t, source), r, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Admit(baseCaps(), Permissions{}); err != nil {
		t.Fatal(err)
	}
}
