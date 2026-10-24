package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

// Canonicalize the fixture root because macOS's temporary directory itself may
// contain a symlink; these tests introduce config symlinks explicitly below.
func discoveryRoot(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func discoveryDir(t *testing.T, root, rel string) string {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(p, 0700); err != nil {
		t.Fatal(err)
	}
	return p
}
func discoveryFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := root.WriteFile(name, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func discoveryResolve(t *testing.T, opts SelectionOptions, caps Capabilities) *Selection {
	t.Helper()
	got, err := Resolve(opts, registryFor(t), engine.ValidationProviders{}, caps)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Resolution == nil {
		t.Fatalf("missing usable selection: %#v", got)
	}
	return got
}
func discoveryReject(t *testing.T, opts SelectionOptions) error {
	t.Helper()
	got, err := Resolve(opts, registryFor(t), engine.ValidationProviders{}, baseCaps())
	if err == nil || got != nil {
		t.Fatalf("expected failure without partial result, got %#v, %v", got, err)
	}
	return err
}

func TestDiscoveryNearestAndDefaultSelection(t *testing.T) {
	root := discoveryRoot(t)
	discoveryDir(t, root, ".git")
	rootConfig := discoveryFile(t, root, Filename, "version: 1\nflavor: commonmark\n")
	near := discoveryDir(t, root, "near")
	nearConfig := discoveryFile(t, near, Filename, "version: 1\nrules: {MD013: {length: 88}}\n")
	start := discoveryDir(t, near, "deep")
	got := discoveryResolve(t, SelectionOptions{WorkingDirectory: start}, baseCaps())
	if got.SelectedPath != nearConfig || !reflect.DeepEqual(got.AncestorPaths, []string{rootConfig}) {
		t.Fatalf("selected %q ancestors %v", got.SelectedPath, got.AncestorPaths)
	}
	if got.Boundary.StartDirectory != start || got.Boundary.Directory != root || got.Boundary.RepositoryRoot != root {
		t.Fatalf("wrong repository boundary: %#v", got.Boundary)
	}
	if got.Resolution.Settings.Flavor != "gfm" {
		t.Fatal("ancestor flavor was inherited")
	}
	if err := os.Remove(nearConfig); err != nil {
		t.Fatal(err)
	}
	got = discoveryResolve(t, SelectionOptions{WorkingDirectory: start}, baseCaps())
	if got.SelectedPath != rootConfig || len(got.AncestorPaths) != 0 {
		t.Fatalf("root config not selected: %#v", got)
	}
	if err := os.Remove(rootConfig); err != nil {
		t.Fatal(err)
	}
	got = discoveryResolve(t, SelectionOptions{WorkingDirectory: start}, baseCaps())
	if got.SelectedPath != "" || len(got.AncestorPaths) != 0 || !reflect.DeepEqual(got.Resolution.Settings, DefaultSettings()) {
		t.Fatalf("missing config did not retain builtins: %#v", got)
	}
	_, err := Resolve(SelectionOptions{WorkingDirectory: start}, registryFor(t), engine.ValidationProviders{}, Capabilities{CommonMark: true, DirectiveProcessing: true})
	if err == nil {
		t.Fatal("missing config silently fell back from unavailable default GFM")
	}
}

func TestDiscoveryExplicitPathUsesWorkingDirectoryAndSelectedAncestors(t *testing.T) {
	root := discoveryRoot(t)
	discoveryDir(t, root, ".git")
	parent := discoveryFile(t, root, Filename, "version: 1\nallow_nested_excludes: false\n")
	cwd := discoveryDir(t, root, "left")
	// This unrelated branch must not be validated or contribute permissions.
	discoveryFile(t, cwd, Filename, "not: valid\n")
	selectedDir := discoveryDir(t, root, "right")
	// A custom selected file replaces the recognized name in its own directory.
	discoveryFile(t, selectedDir, Filename, "not: valid\n")
	selected := discoveryFile(t, selectedDir, "custom.yaml", "version: 1\nallow_nested_excludes: true\n")
	got := discoveryResolve(t, SelectionOptions{WorkingDirectory: cwd, StartDirectory: cwd, ExplicitPath: "../right/custom.yaml"}, baseCaps())
	if got.SelectedPath != selected || !reflect.DeepEqual(got.AncestorPaths, []string{parent}) {
		t.Fatalf("explicit selection used wrong branch: %#v", got)
	}
	if got.Resolution.Settings.AllowNestedExcludes || len(got.Resolution.EffectivePermissions.NestedExcludes) != 1 {
		t.Fatal("explicit sibling selection escaped ancestor policy")
	}
	// Start controls discovery; relative explicit paths continue to use cwd.
	got = discoveryResolve(t, SelectionOptions{WorkingDirectory: cwd, StartDirectory: selectedDir, ExplicitPath: "../right/custom.yaml"}, baseCaps())
	if got.SelectedPath != selected {
		t.Fatalf("explicit path resolved from start: %q", got.SelectedPath)
	}
}

func TestDiscoveryRepositoryAndCallerBoundaries(t *testing.T) {
	for _, worktree := range []bool{false, true} {
		name := "directory"
		if worktree {
			name = "worktree-file"
		}
		t.Run(name, func(t *testing.T) {
			outside := discoveryRoot(t)
			discoveryFile(t, outside, Filename, "not: valid\n")
			repo := discoveryDir(t, outside, "repo")
			if worktree {
				discoveryFile(t, repo, ".git", "gitdir: /nonexistent/metadata\n")
			} else {
				discoveryDir(t, repo, ".git")
			}
			rootConfig := discoveryFile(t, repo, Filename, "version: 1\n")
			start := discoveryDir(t, repo, "sub/deep")
			got := discoveryResolve(t, SelectionOptions{WorkingDirectory: start}, baseCaps())
			if got.SelectedPath != rootConfig || got.Boundary.Directory != repo || got.Boundary.RepositoryRoot != repo {
				t.Fatalf("repository marker ignored: %#v", got)
			}
			bound := filepath.Dir(start)
			got = discoveryResolve(t, SelectionOptions{WorkingDirectory: start, BoundaryDirectory: bound}, baseCaps())
			if got.SelectedPath != "" || got.Boundary.Directory != bound {
				t.Fatalf("caller bound widened: %#v", got)
			}
			_ = discoveryReject(t, SelectionOptions{WorkingDirectory: start, BoundaryDirectory: bound, ExplicitPath: rootConfig})
			_ = discoveryReject(t, SelectionOptions{WorkingDirectory: start, ExplicitPath: filepath.Join(outside, Filename)})
			_ = discoveryReject(t, SelectionOptions{WorkingDirectory: start, BoundaryDirectory: discoveryDir(t, outside, "unrelated")})
		})
	}
}

func TestDiscoveryNestedRepositoryScope(t *testing.T) {
	root := discoveryRoot(t)
	discoveryDir(t, root, ".git")
	outer := discoveryFile(t, root, Filename, "version: 1\nallow_nested_excludes: false\n")
	nested := discoveryDir(t, root, "nested")
	discoveryFile(t, nested, ".git", "gitdir: elsewhere\n")
	inner := discoveryFile(t, nested, Filename, "version: 1\nexclude: [generated]\n")
	start := discoveryDir(t, nested, "deep")
	got := discoveryResolve(t, SelectionOptions{WorkingDirectory: start}, baseCaps())
	if got.SelectedPath != inner || len(got.AncestorPaths) != 0 || got.Boundary.Directory != nested {
		t.Fatalf("outer repository leaked across nested root: %#v", got)
	}
	_ = discoveryReject(t, SelectionOptions{WorkingDirectory: start, ExplicitPath: outer})
	_ = discoveryReject(t, SelectionOptions{WorkingDirectory: root, ExplicitPath: inner})
}

func TestDiscoveryInvalidNearestAndAncestorsNeverFallback(t *testing.T) {
	for _, body := range []string{
		"version: 1\nversion: 1\n",
		"version: 2\n",
		"version: 1\nrules: {MD013: {enabled: false, length: -1}}\n",
		"version: 1\nexclude: ['[']\n",
	} {
		t.Run(body, func(t *testing.T) {
			root := discoveryRoot(t)
			discoveryDir(t, root, ".git")
			discoveryFile(t, root, Filename, "version: 1\n")
			child := discoveryDir(t, root, "child")
			bad := discoveryFile(t, child, Filename, body)
			err := discoveryReject(t, SelectionOptions{WorkingDirectory: child})
			if err.Error() == "" {
				t.Fatal("empty diagnostic")
			}
			// Turning the invalid nearest file into an ancestor must still fail.
			leaf := discoveryDir(t, child, "leaf")
			discoveryFile(t, leaf, Filename, "version: 1\n")
			err = discoveryReject(t, SelectionOptions{WorkingDirectory: leaf})
			var located *ValidationError
			// Syntax errors have their own structured type; semantic errors preserve
			// the ancestor origin rather than identifying the selected definition.
			if errors.As(err, &located) && located.Origin.Path != bad {
				t.Fatalf("wrong ancestor error origin: %#v", located.Origin)
			}
		})
	}
}

func TestDiscoveryAncestorSchemasWithoutExecutionAdmission(t *testing.T) {
	root := discoveryRoot(t)
	discoveryDir(t, root, ".git")
	parent := discoveryFile(t, root, Filename, "version: 1\nflavor: commonmark\nresources: {index_file: custom.md, anchor_style: explicit}\nfrontmatter: {enabled: true}\nrules: {MD001: {enabled: true}, MD013: {length: 80}}\nexclude: [parent-only]\n")
	child := discoveryDir(t, root, "child")
	discoveryFile(t, child, Filename, "version: 1\nrules: {MD013: {length: 99}}\n")
	got := discoveryResolve(t, SelectionOptions{WorkingDirectory: child}, baseCaps())
	if got.Resolution.Settings.Flavor != "gfm" || got.Resolution.Settings.IndexFile != "index.md" || got.Resolution.Settings.FrontmatterEnabled || len(got.Resolution.Settings.Exclude) != 0 {
		t.Fatalf("ancestor settings leaked: %#v", got.Resolution.Settings)
	}
	if !reflect.DeepEqual(got.AncestorPaths, []string{parent}) {
		t.Fatal(got.AncestorPaths)
	}
	// A domain/provider requirement is required even for a policy-only ancestor.
	discoveryFile(t, root, Filename, "version: 1\nrules: {MD095: {enabled: false, allowed_hosts: [example.com]}}\n")
	_ = discoveryReject(t, SelectionOptions{WorkingDirectory: child})
	// The same unavailable execution request is an error when selected.
	discoveryFile(t, root, Filename, "version: 1\n")
	discoveryFile(t, child, Filename, "version: 1\nfrontmatter: {enabled: true}\n")
	err := discoveryReject(t, SelectionOptions{WorkingDirectory: child})
	var at *ValidationError
	if !errors.As(err, &at) || at.Origin.Path != filepath.Join(child, Filename) || at.Origin.PropertyPath != "frontmatter.enabled" {
		t.Fatalf("wrong selected capability origin: %v", err)
	}
}

func TestDiscoveryNestedExclusionPolicy(t *testing.T) {
	for _, selected := range []string{"version: 1\n", "version: 1\nexclude: []\nallow_nested_excludes: true\n"} {
		root := discoveryRoot(t)
		discoveryDir(t, root, ".git")
		parent := discoveryFile(t, root, Filename, "version: 1\nallow_nested_excludes: false\nexclude: [own-exclusion]\n")
		child := discoveryDir(t, root, "child")
		childPath := discoveryFile(t, child, Filename, selected)
		got := discoveryResolve(t, SelectionOptions{WorkingDirectory: child}, baseCaps())
		if got.Resolution.Settings.AllowNestedExcludes || len(got.Resolution.Settings.Exclude) != 0 {
			t.Fatal("parent permission or settings changed")
		}
		discoveryFile(t, child, Filename, "version: 1\nexclude: [blocked]\n")
		for _, opts := range []SelectionOptions{{WorkingDirectory: child}, {WorkingDirectory: child, ExplicitPath: childPath}} {
			err := discoveryReject(t, opts)
			var policy *PolicyError
			if !errors.As(err, &policy) || policy.Origin.Path != childPath || policy.Origin.PropertyPath != "exclude" || policy.Origin.Position == nil || policy.Origin.Position.Line != 2 {
				t.Fatalf("unlocated exclusion error: %v", err)
			}
			if len(policy.Restrictions) != 1 || policy.Restrictions[0].Path != parent || policy.Restrictions[0].PropertyPath != "allow_nested_excludes" {
				t.Fatalf("missing governing parent: %v", err)
			}
		}
		// A prohibited intermediate definition cannot silently contribute a new
		// restriction while its exclusions are ignored.
		leaf := discoveryDir(t, child, "leaf")
		discoveryFile(t, leaf, Filename, "version: 1\n")
		_ = discoveryReject(t, SelectionOptions{WorkingDirectory: leaf})
	}
	// A definition's own lock does not prohibit its own exclusions.
	root := discoveryRoot(t)
	discoveryDir(t, root, ".git")
	discoveryFile(t, root, Filename, "version: 1\nallow_nested_excludes: false\nexclude: [own-exclusion]\n")
	got := discoveryResolve(t, SelectionOptions{WorkingDirectory: root}, baseCaps())
	if !reflect.DeepEqual(got.Resolution.Settings.Exclude, []string{"own-exclusion"}) {
		t.Fatal(got.Resolution.Settings.Exclude)
	}
}

func TestDiscoveryPermissionOriginsAndOwnedSnapshots(t *testing.T) {
	root := discoveryRoot(t)
	discoveryDir(t, root, ".git")
	top := discoveryFile(t, root, Filename, "version: 1\ndirectives: {enabled: false}\nallow_nested_excludes: false\nrules: {MD013: {allow_directives: false, length: 80}}\n")
	middle := discoveryDir(t, root, "middle")
	mid := discoveryFile(t, middle, Filename, "version: 1\ndirectives: {enabled: false}\nallow_nested_excludes: false\nrules: {MD013: {allow_directives: false, length: 90}}\n")
	leaf := discoveryDir(t, middle, "leaf")
	selected := discoveryFile(t, leaf, Filename, "version: 1\ndirectives: {enabled: true}\nallow_nested_excludes: true\nrules: {MD013: {allow_directives: true, length: 101}}\n")
	opts := SelectionOptions{WorkingDirectory: leaf}
	caps := baseCaps()
	caps.DirectiveProcessing = false
	got := discoveryResolve(t, opts, caps)
	if got.SelectedPath != selected || !reflect.DeepEqual(got.AncestorPaths, []string{top, mid}) {
		t.Fatalf("wrong chain: %#v", got)
	}
	for field, origins := range map[string][]Origin{
		"directives.enabled":           got.Resolution.EffectivePermissions.Directives,
		"allow_nested_excludes":        got.Resolution.EffectivePermissions.NestedExcludes,
		"rules.MD013.allow_directives": got.Resolution.EffectivePermissions.RuleDirectives["MD013"],
	} {
		if len(origins) != 2 {
			t.Fatalf("%s: lost restrictions: %#v", field, origins)
		}
		for i, path := range []string{top, mid} {
			if origins[i].Path != path || origins[i].PropertyPath != field || origins[i].Position == nil {
				t.Fatalf("%s: wrong restriction %d: %#v", field, i, origins[i])
			}
		}
	}
	if got.Resolution.Settings.DirectivesEnabled || got.Resolution.Settings.AllowNestedExcludes || len(got.Resolution.OwnPermissions.Directives) != 0 {
		t.Fatal("permission AND/own values incorrect")
	}
	if !reflect.DeepEqual(got.Resolution.Rules["MD013"].Options["length"], 101) || got.Resolution.Rules["MD013"].AllowDirectives {
		t.Fatal("ancestor rule option merged or directive denial reopened")
	}
	// All governing exclusion denials survive diagnostics, not only the first.
	//nolint:gosec // selected is a configuration fixture inside this test's temporary root.
	original, err := os.ReadFile(selected)
	if err != nil {
		t.Fatal(err)
	}
	discoveryFile(t, leaf, Filename, string(original)+"exclude: [blocked]\n")
	_, err = Resolve(opts, registryFor(t), engine.ValidationProviders{}, caps)
	var policy *PolicyError
	if !errors.As(err, &policy) || len(policy.Restrictions) != 2 || policy.Restrictions[0].Path != top || policy.Restrictions[1].Path != mid {
		t.Fatalf("policy error lost governing denials: %v", err)
	}
	discoveryFile(t, leaf, Filename, string(original))
	got.AncestorPaths[0] = "mutated"
	got.Resolution.EffectivePermissions.Directives[0].Position.Line = 999
	got.Resolution.EffectivePermissions.RuleDirectives["MD013"][0] = Origin{}
	got.Resolution.Settings.FrontmatterFormats[0] = "mutated"
	got.Resolution.Rules["MD013"].Options["length"] = 999
	again := discoveryResolve(t, opts, caps)
	if again.AncestorPaths[0] != top || again.Resolution.EffectivePermissions.Directives[0].Position.Line != 2 || again.Resolution.EffectivePermissions.RuleDirectives["MD013"][0].Path != top || again.Resolution.Settings.FrontmatterFormats[0] != "yaml" || !reflect.DeepEqual(again.Resolution.Rules["MD013"].Options["length"], 101) {
		t.Fatal("selection results share mutable state")
	}
}

func TestDiscoveryRejectsUnsafeConfigurationPaths(t *testing.T) {
	root := discoveryRoot(t)
	discoveryDir(t, root, ".git")
	targetDir := discoveryDir(t, root, "real")
	target := discoveryFile(t, targetDir, "custom.yaml", "version: 1\n")
	linkDir := filepath.Join(root, "linked")
	if err := os.Symlink(targetDir, linkDir); err != nil {
		t.Fatal(err)
	}
	linkFile := filepath.Join(root, "linked.yaml")
	if err := os.Symlink(target, linkFile); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(root, "dangling.yaml")
	if err := os.Symlink(filepath.Join(root, "missing.yaml"), dangling); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{linkFile, dangling, filepath.Join(linkDir, "custom.yaml"), linkDir + string(filepath.Separator) + ".." + string(filepath.Separator) + "real" + string(filepath.Separator) + "custom.yaml", discoveryDir(t, root, "directory.yaml"), filepath.Join(root, "absent.yaml")} {
		_ = discoveryReject(t, SelectionOptions{WorkingDirectory: root, ExplicitPath: path})
	}
	_ = discoveryReject(t, SelectionOptions{WorkingDirectory: linkDir, BoundaryDirectory: linkDir})
	// A recognized symlink is an error even when an otherwise valid fallback is
	// available in a parent directory.
	discoveryFile(t, root, Filename, "version: 1\n")
	child := discoveryDir(t, root, "child")
	if err := os.Symlink(target, filepath.Join(child, Filename)); err != nil {
		t.Fatal(err)
	}
	_ = discoveryReject(t, SelectionOptions{WorkingDirectory: child})
	if err := os.Remove(filepath.Join(child, Filename)); err != nil {
		t.Fatal(err)
	}
	discoveryDir(t, child, Filename)
	_ = discoveryReject(t, SelectionOptions{WorkingDirectory: child})
}
