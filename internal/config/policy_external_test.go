package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
)

func TestPolicyMatchersRemainOwnedAcrossBranches(t *testing.T) {
	registry, err := engine.NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	apply := func(parent config.PolicyState, path, body string) config.PolicyState {
		t.Helper()
		definition, err := config.DecodeYAML(path, []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		next, _, err := parent.Apply(definition, registry, engine.ValidationProviders{}, config.Capabilities{})
		if err != nil {
			t.Fatal(err)
		}
		return next
	}
	root := apply(config.PolicyState{}, "root/.mdchk.yaml", "version: 1\nexclude: [private/**]\n")
	child := apply(root, "root/child/.mdchk.yaml", "version: 1\nexclude: [child-private/**]\n")
	sibling := apply(root, "root/sibling/.mdchk.yaml", "version: 1\nexclude: [sibling-private/**]\n")
	// A caller can overwrite an exported pointer's whole value even when all
	// fields and matching methods otherwise preserve internal immutability.
	for _, state := range []config.PolicyState{root, child, sibling} {
		for _, scope := range state.Exclusions() {
			*scope.Patterns = config.Patterns{}
		}
	}
	laterChild := apply(root, "root/later/.mdchk.yaml", "version: 1\n")
	grandchild := apply(child, "root/child/grand/.mdchk.yaml", "version: 1\n")
	for _, tc := range []struct {
		name  string
		state config.PolicyState
		names []string
	}{
		{"original", root, []string{"private/a.md"}},
		{"child", child, []string{"private/a.md", "child-private/a.md"}},
		{"sibling", sibling, []string{"private/a.md", "sibling-private/a.md"}},
		{"later child", laterChild, []string{"private/a.md"}},
		{"grandchild", grandchild, []string{"private/a.md", "child-private/a.md"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scopes := tc.state.Exclusions()
			if len(scopes) != len(tc.names) {
				t.Fatalf("scopes=%d want %d", len(scopes), len(tc.names))
			}
			for i, name := range tc.names {
				matched, err := scopes[i].Patterns.Match(name, false, true)
				if err != nil || !matched {
					t.Fatalf("retained exclusion %d no longer matches %q: %v", i, name, err)
				}
			}
		})
	}
}

func TestSelectionAndExecutionMatchersRemainOwned(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	access, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := access.Close(); err != nil {
			t.Error(err)
		}
	}()
	body := []byte("version: 1\nflavor: commonmark\ndirectives: {enabled: false}\nexclude: [private/**]\ndocuments: {slides: [slides/**]}\n")
	if err := access.WriteFile(config.Filename, body, 0600); err != nil {
		t.Fatal(err)
	}
	registry, err := engine.NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	caps := config.Capabilities{CommonMark: true, SlideAnnotation: true}
	selected, err := config.ResolveValidated(config.SelectionOptions{WorkingDirectory: base, BoundaryDirectory: base}, registry, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	admit := func() *config.Resolution {
		t.Helper()
		r, err := selected.Admit(caps)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	matches := func(r *config.Resolution) {
		t.Helper()
		for _, tc := range []struct {
			patterns *config.Patterns
			name     string
		}{{r.Exclude, "private/a.md"}, {r.Slides, "slides/a.md"}} {
			matched, err := tc.patterns.Match(tc.name, false, true)
			if err != nil || !matched {
				t.Fatalf("matcher no longer matches %q: %v", tc.name, err)
			}
		}
	}
	overwrite := func(r *config.Resolution) { t.Helper(); *r.Exclude = config.Patterns{}; *r.Slides = config.Patterns{} }
	first := admit()
	sibling := admit()
	firstPlan := first.ExecutionPlan()
	siblingPlan := sibling.ExecutionPlan()
	firstView := firstPlan.Configuration()
	secondView := firstPlan.Configuration()
	overwrite(firstView)
	matches(first)
	matches(sibling)
	matches(secondView)
	matches(firstPlan.Configuration())
	firstView = firstPlan.Configuration()
	overwrite(first)
	matches(sibling)
	matches(firstView)
	matches(secondView)
	matches(admit())
	overwrite(firstView)
	matches(secondView)
	matches(firstPlan.Configuration())
	matches(siblingPlan.Configuration())
	matches(admit())
	overwrite(secondView)
	overwrite(sibling)
	matches(firstPlan.Configuration())
	matches(siblingPlan.Configuration())
	matches(admit())
	overwrite(siblingPlan.Configuration())
	matches(firstPlan.Configuration())
	matches(siblingPlan.Configuration())
	matches(admit())
	// A separate selection branch over the same source also owns its matchers.
	other, err := config.ResolveValidated(config.SelectionOptions{WorkingDirectory: base, BoundaryDirectory: base}, registry, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	r, err := other.Admit(caps)
	if err != nil {
		t.Fatal(err)
	}
	matches(r)
	overwrite(r)
	matches(admit())
}
