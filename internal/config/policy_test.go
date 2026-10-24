package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
)

func policyDefinition(t *testing.T, path, body string) *Definition {
	t.Helper()
	d, err := DecodeYAML(path, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestEffectivePolicyConsumers(t *testing.T) {
	root := discoveryRoot(t)
	discoveryDir(t, root, ".git")
	a := discoveryFile(t, root, Filename, "version: 1\nflavor: commonmark\ndirectives: {enabled: false}\nallow_nested_excludes: false\nrules: {MD013: {allow_directives: false, length: 50}}\n")
	middle := discoveryDir(t, root, "middle")
	b := discoveryFile(t, middle, Filename, "version: 1\ndirectives: {enabled: false}\nallow_nested_excludes: false\nrules: {MD013: {allow_directives: false}}\n")
	leaf := discoveryDir(t, middle, "leaf")
	c := discoveryFile(t, leaf, Filename, "version: 1\nflavor: commonmark\ndirectives: {enabled: true}\nallow_nested_excludes: true\nrules: {MD013: {allow_directives: true, length: 100}}\n")
	before := map[string][]byte{}
	for _, p := range []string{a, b, c} {
		data, err := readPolicyFixture(root, p)
		if err != nil {
			t.Fatal(err)
		}
		before[p] = data
	}
	opts := SelectionOptions{WorkingDirectory: root, StartDirectory: leaf, ExplicitPath: c, BoundaryDirectory: root}
	selected, err := ResolveValidated(opts, registryFor(t), engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Resolution != nil {
		t.Fatal("pre-admission selection grants execution")
	}
	view := selected.Configuration(Capabilities{CommonMark: true})
	if view.Settings.DirectivesEnabled || view.Settings.AllowNestedExcludes || view.Rules["MD013"].AllowDirectives {
		t.Fatal("child true reopened inherited denials")
	}
	if got := view.Rules["MD013"].Options["length"]; got != 100 {
		t.Fatalf("ancestor options merged: %v", got)
	}
	if p := view.Provenance["directives.enabled"]; !p.Explicit || p.Value == nil || !*p.Value || p.Origin.Path != c {
		t.Fatalf("lost selected true: %+v", p)
	}
	decision := view.DirectivePermission("MD013")
	if decision.Allowed || decision.Err != nil || len(decision.Restrictions) != 4 {
		t.Fatalf("decision %+v", decision)
	}
	for i, o := range decision.Restrictions {
		if o.Path != []string{a, b, a, b}[i] || o.Position == nil {
			t.Fatalf("lost governing source: %+v", o)
		}
	}
	shared := view.Shared()
	if shared.Effective["directives"].(map[string]any)["enabled"] != false || !shared.Effective["selected_permissions"].(map[string]bool)["/directives/enabled"] {
		t.Fatal("shared selected/effective permissions collapsed")
	}
	count := 0
	for _, p := range shared.Provenance {
		if p.Inherited && p.Pointer == "/directives/enabled" {
			count++
			if p.SourceKind != "ancestor-config" || p.Range == nil {
				t.Fatal(p)
			}
		}
	}
	if count != 2 {
		t.Fatalf("inherited records %d", count)
	}
	validatePolicyShared(t, shared)
	var state PolicyState
	var scanned *Configuration
	for _, path := range []string{a, b, c} {
		state, scanned, err = state.Apply(policyDefinition(t, path, string(before[path])), registryFor(t), engine.ValidationProviders{}, Capabilities{CommonMark: true})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(scanned.DirectivePermission("MD013"), decision) || !reflect.DeepEqual(scanned.Shared(), shared) {
		t.Fatal("top-down and bounded selection adapters disagree")
	}
	againBytes, _ := json.Marshal(selected.Configuration(Capabilities{CommonMark: true}).Shared())
	sharedBytes, _ := json.Marshal(shared)
	if !bytes.Equal(againBytes, sharedBytes) {
		t.Fatal("shared projection unstable")
	}
	// Returned shared source points and values are isolated from future queries.
	for _, p := range shared.Provenance {
		if p.Inherited && p.Range != nil {
			p.Range.Start.Line = 999
		}
	}
	shared.Effective["directives"].(map[string]any)["enabled"] = true
	if selected.Configuration(Capabilities{}).DirectivePermission("MD013").Restrictions[0].Position.Line == 999 {
		t.Fatal("shared projection aliases authority")
	}

	admitted, err := Resolve(opts, registryFor(t), engine.ValidationProviders{}, Capabilities{CommonMark: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(admitted.Resolution.EffectivePermissions, view.EffectivePermissions) {
		t.Fatal("inspection/admission disagree")
	}
	// Mutations of every outward authority-bearing projection cannot reopen the
	// retained decision or subsequent admission; source-point pointers are owned.
	decision.Restrictions[0].Position.Line = 999
	view.EffectivePermissions.Directives = nil
	view.Settings.DirectivesEnabled = true
	r := view.Rules["MD013"]
	r.AllowDirectives = true
	view.Rules["MD013"] = r
	*view.Provenance["directives.enabled"].Value = false
	selected.SelectedPath = "invented"
	selected.AncestorPaths[0] = "invented"
	if view.DirectivePermission("MD013").Allowed || view.DirectivePermission("MD013").Restrictions[0].Position.Line == 999 {
		t.Fatal("passive mutation changed authority")
	}
	again, err := selected.Admit(Capabilities{CommonMark: true})
	if err != nil || again.Settings.DirectivesEnabled {
		t.Fatalf("admission authority changed: %v", err)
	}
	if selected.Configuration(Capabilities{}).Provenance["directives.enabled"].Value == nil || !*selected.Configuration(Capabilities{}).Provenance["directives.enabled"].Value {
		t.Fatal("selection ownership lost")
	}
	for p, want := range before {
		got, err := readPolicyFixture(root, p)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("source changed %s: %v", p, err)
		}
	}
}

func TestPolicyStateRejectsWholeInvalidChild(t *testing.T) {
	registry := registryFor(t)
	providers := engine.ValidationProviders{}
	apply := func(s PolicyState, path, body string) (PolicyState, *Configuration, error) {
		return s.Apply(policyDefinition(t, path, body), registry, providers, Capabilities{})
	}
	root, _, err := apply(PolicyState{}, "root/.mdchk.yaml", "version: 1\nexclude: [private/**]\nallow_nested_excludes: false\nrules: {MD013: {allow_directives: false}}\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		"version: 1\nexclude: [other/**]\ndirectives: {enabled: false}\n",
		"version: 1\ndirectives: {enabled: false}\nrules: {MD013: {length: -1}}\n",
	} {
		next, view, err := apply(root, "root/bad/.mdchk.yaml", body)
		if err == nil || view != nil || !reflect.DeepEqual(next.Permissions(), root.Permissions()) || len(next.Exclusions()) != 1 {
			t.Fatalf("invalid child activated state: %+v %v", next, err)
		}
		scopes := next.Exclusions()
		matched, e := scopes[0].Patterns.Match("private/file.md", false, true)
		if e != nil || !matched {
			t.Fatalf("inherited exclusion lost: %v %v", matched, e)
		}
		grand, view, err := apply(next, "root/bad/grand/.mdchk.yaml", "version: 1\nallow_nested_excludes: true\nrules: {MD013: {allow_directives: true}}\n")
		if err != nil || len(grand.Exclusions()) != 1 || view.DirectivePermission("MD013").Allowed || !view.DirectivePermission("MD001").Allowed || !view.Settings.DirectivesEnabled || view.Settings.AllowNestedExcludes {
			t.Fatalf("wrong valid inherited policy: %+v %v", view, err)
		}
		origins := grand.Permissions()
		origins.RuleDirectives["MD013"][0].Path = "mutated"
		scopes[0].Origin.Path = "mutated"
		if grand.Permissions().RuleDirectives["MD013"][0].Path != "root/.mdchk.yaml" || grand.Exclusions()[0].Origin.Path != "root/.mdchk.yaml" {
			t.Fatal("policy state aliases caller")
		}
	}
	// The definition that introduces a lock may introduce its own exclusions.
	if len(root.Exclusions()) != 1 {
		t.Fatal("own lock prohibited own excludes")
	}
}

func TestPreAdmissionBoundariesAndUnavailableSelection(t *testing.T) {
	root := discoveryRoot(t)
	discoveryFile(t, root, Filename, "version: broken\n")
	cap := discoveryDir(t, root, "project")
	start := discoveryDir(t, cap, "target")
	path := discoveryFile(t, start, Filename, "version: 1\nflavor: commonmark\nrules: {MD013: {enabled: true}}\n")
	s, err := ResolveValidated(SelectionOptions{WorkingDirectory: root, StartDirectory: start, BoundaryDirectory: cap}, registryFor(t), engine.ValidationProviders{})
	if err != nil || s.SelectedPath != path {
		t.Fatalf("bounded target inspection %v %v", s, err)
	}
	if !s.Configuration(Capabilities{}).Rules["MD013"].Enabled {
		t.Fatal("unavailable explicit request lost")
	}
	if r, e := s.Admit(Capabilities{CommonMark: true}); r != nil || e == nil {
		t.Fatal("unavailable selection admitted")
	} else {
		var source *SourceError
		if !errors.As(e, &source) || len(source.Sources()) != 1 || source.Sources()[0].Path != path {
			t.Fatal("lost same-read identity", e)
		}
	}
	discoveryFile(t, cap, Filename, "version: invalid\n")
	if s, e := ResolveValidated(SelectionOptions{StartDirectory: start, BoundaryDirectory: cap}, registryFor(t), engine.ValidationProviders{}); s != nil || e == nil {
		t.Fatal("invalid ancestor yielded partial inspection")
	}
	var unresolved *Configuration
	if unresolved.DirectivePermission("MD001").Allowed || (&Configuration{}).DirectivePermission("MD001").Allowed {
		t.Fatal("unresolved grant")
	}
	d := policyDefinition(t, "", "version: 1\nrules: {MD013: {allow_directives: false}}\n")
	v, e := ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
	if e != nil {
		t.Fatal(e)
	}
	standalone := v.Configuration(Capabilities{})
	if standalone.DirectivePermission("MD013").Allowed || !standalone.DirectivePermission("MD001").Allowed || standalone.DirectivePermission("UNKNOWN").Allowed {
		t.Fatal("standalone permissions")
	}
	if got := standalone.DirectivePermission("MD013").Restrictions[0]; got.Path != "" {
		t.Fatal("invented host ancestry")
	}
}

func validatePolicyShared(t *testing.T, shared any) {
	t.Helper()
	python := os.Getenv("MDCHK_RESULT_VALIDATOR_PYTHON")
	if python == "" {
		return
	}
	content, err := json.Marshal(shared)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := filepath.Abs("../../docs/contracts/results/result.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	script := `import json,sys
from jsonschema import Draft202012Validator
schema=json.load(open(sys.argv[1]))
Draft202012Validator({"$ref":"#/$defs/Configuration","$defs":schema["$defs"]}).validate(json.load(sys.stdin))`
	command := exec.CommandContext(ctx, python, "-B", "-c", script, schema) //nolint:gosec // Explicit test interpreter with fixed checked-in schema and validator code.
	command.Stdin = bytes.NewReader(content)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("shared inherited Configuration: %v: %s", err, output)
	}
}

func TestDirectivePermissionMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, body   string
		global, rule bool
		denials      int
	}{
		{"omitted", "version: 1\n", true, true, 0},
		{"global-only", "version: 1\ndirectives: {enabled: false}\n", false, false, 1},
		{"rule-only", "version: 1\nrules: {MD013: {allow_directives: false}}\n", true, false, 1},
		{"both", "version: 1\ndirectives: {enabled: false}\nrules: {MD013: {allow_directives: false}}\n", false, false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ValidateDefinition(policyDefinition(t, "supplied.yaml", tc.body), registryFor(t), engine.ValidationProviders{})
			if err != nil {
				t.Fatal(err)
			}
			view := v.Configuration(Capabilities{})
			got := view.DirectivePermission("MD013")
			if got.Allowed != tc.rule || len(got.Restrictions) != tc.denials || view.DirectivePermission("MD001").Allowed != tc.global {
				t.Fatalf("wrong effective decision: %+v", got)
			}
			for _, o := range got.Restrictions {
				if o.PropertyPath != "directives.enabled" && o.PropertyPath != "rules.MD013.allow_directives" {
					t.Fatal("invented denial field", o)
				}
			}
		})
	}
}

// Read only through the fixture root, mirroring the production acquisition bound.
func readPolicyFixture(base, path string) ([]byte, error) {
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer closeReadHandle(root)
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return nil, err
	}
	return root.ReadFile(rel)
}
