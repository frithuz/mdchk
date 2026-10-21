package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
)

const availableConfig = "version: 1\nflavor: commonmark\ndirectives: {enabled: false}\n"

func project(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return root
}
func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
func validProject(t *testing.T) string {
	t.Helper()
	root := project(t)
	put(t, filepath.Join(root, config.Filename), availableConfig)
	return root
}

func TestLintConfigurationStopsBeforeTargets(t *testing.T) {
	for _, tc := range []struct{ name, body, explicit, want string }{
		{"missing defaults", "", "", "flavor"},
		{"invalid nearest", "version: 2\n", "", "version"},
		{"missing explicit", availableConfig, "missing.yaml", "missing.yaml"},
		{"unavailable directives", "version: 1\nflavor: commonmark\n", "", "directives"},
		{"explicit unavailable rule", availableConfig + "rules: {MD013: {enabled: true}}\n", "", "MD013"},
		{"invalid disabled options", availableConfig + "rules: {MD013: {enabled: false, length: -1}}\n", "", "length"},
		{"unavailable GFM", "version: 1\n", "", "flavor"},
		{"unavailable Pandoc", "version: 1\nflavor: pandoc\n", "", "flavor"},
		{"invalid pattern", availableConfig + "exclude: ['bad/**x']\n", "", "exclude"},
		{"disabled hostname provider", availableConfig + "rules: {MD095: {enabled: false, allowed_hosts: [example.com]}}\n", "", "allowed_hosts"},
		{"frontmatter", availableConfig + "frontmatter: {enabled: true}\n", "", "frontmatter"},
		{"quarto", availableConfig + "integrations: {quarto: {enabled: true}}\n", "", "quarto"},
		{"slides", availableConfig + "documents: {slides: ['talk/**']}\n", "", "slides"},
		{"explicit resource default", availableConfig + "resources: {index_file: index.md}\n", "", "index_file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := project(t)
			if tc.body != "" {
				put(t, filepath.Join(root, config.Filename), tc.body)
			}
			args := []string{"lint", "unreadable-or-missing.md"}
			if tc.explicit != "" {
				args = append(args, "--config", tc.explicit)
			}
			calls := 0
			var out, stderr bytes.Buffer
			status := run(args, &out, &stderr, "dev", prepareLint, func(*lintInvocation, io.Writer, io.Writer) int { calls++; return 0 })
			if status != 12 || calls != 0 || out.Len() != 0 || !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("status=%d calls=%d out=%q err=%q", status, calls, &out, &stderr)
			}
			if got := Run(args, io.Discard, failingWriter{}, "dev"); got != 13 {
				t.Fatalf("config output failure=%d", got)
			}
		})
	}
}

func TestLintPreparationRetainsOwnedSelection(t *testing.T) {
	root := validProject(t)
	path := filepath.Join(root, config.Filename)
	body := availableConfig + "rules: {MD013: {enabled: false, length: 88}}\nexclude: ['generated/']\n"
	put(t, path, body)
	put(t, filepath.Join(root, "other", config.Filename), "invalid: [")
	put(t, filepath.Join(root, "other", "target.md"), "# untouched\n")
	calls := 0
	status := run([]string{"lint", "other/target.md", "missing.md"}, io.Discard, io.Discard, "dev", prepareLint, func(inv *lintInvocation, _, _ io.Writer) int {
		calls++
		if inv.selection.SelectedPath != path || inv.selection.Resolution.Settings.Flavor != "commonmark" || len(inv.selection.Resolution.Rules) != 89 || len(inv.options.paths) != 2 {
			t.Fatalf("incomplete selection: %+v", inv)
		}
		r := inv.selection.Resolution.Rules["MD013"]
		if r.Enabled || r.Options["length"] != 88 {
			t.Fatalf("rule=%+v", r)
		}
		if !inv.selection.Resolution.Rules["MD001"].Enabled || inv.selection.Resolution.Rules["MD001"].Available {
			t.Fatal("default catalog/availability changed")
		}
		return 15
	})
	if status != 15 || calls != 1 {
		t.Fatalf("status=%d calls=%d", status, calls)
	}
	for p, want := range map[string]string{path: body, filepath.Join(root, "other", "target.md"): "# untouched\n"} {
		//nolint:gosec // Read only test fixture paths.
		got, err := os.ReadFile(p)
		if err != nil || string(got) != want {
			t.Fatalf("file changed: %s %q %v", p, got, err)
		}
	}
}

func TestLintSelectedSettingsAndAncestorDenials(t *testing.T) {
	root := project(t)
	// Ancestors validate without requesting execution and supply permissions only.
	put(t, filepath.Join(root, config.Filename), "version: 1\nflavor: pandoc\ndirectives: {enabled: false}\nrules: {MD013: {length: 99}}\nexclude: ['parent/']\n")
	child := filepath.Join(root, "child")
	put(t, filepath.Join(child, config.Filename), "version: 1\nflavor: commonmark\ndirectives: {enabled: true}\nexclude: ['child/']\n")
	t.Chdir(child)
	inv, err := prepareLint(lintOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.selection.AncestorPaths) != 1 || inv.selection.Resolution.Settings.DirectivesEnabled || inv.selection.Resolution.Rules["MD013"].Options["length"] != 80 {
		t.Fatalf("ancestor settings merged or permission lost: %+v", inv.selection.Resolution)
	}
	if got, err := inv.excludes("parent/a.md", false); err != nil || got {
		t.Fatalf("ancestor exclusion merged: %v %v", got, err)
	}
	put(t, filepath.Join(root, config.Filename), "version: 1\nallow_nested_excludes: false\n")
	_, err = prepareLint(lintOptions{})
	var policy *config.PolicyError
	if !errors.As(err, &policy) || policy.Origin.Path != filepath.Join(child, config.Filename) || len(policy.Restrictions) != 1 {
		t.Fatalf("governing denial lost: %v", err)
	}
	// A nearer invalid file must not fall back to an otherwise valid parent.
	put(t, filepath.Join(root, config.Filename), availableConfig)
	put(t, filepath.Join(child, config.Filename), "version: [\n")
	if inv, err := prepareLint(lintOptions{}); err == nil || inv != nil || !strings.Contains(err.Error(), filepath.Join(child, config.Filename)) {
		t.Fatalf("nearest fallback: %v %v", inv, err)
	}
}

func TestLintExplicitSiblingAndBoundExclusions(t *testing.T) {
	root := validProject(t)
	cwd := filepath.Join(root, "work")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(root, "settings", "custom.yaml")
	put(t, selected, availableConfig+"exclude: ['generated/','direct.md']\n")
	t.Chdir(cwd)
	inv, err := prepareLint(lintOptions{config: filepath.Join("..", "settings", "custom.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if inv.selection.SelectedPath != selected || inv.exclusionBase != filepath.Dir(selected) || inv.workingDirectory != cwd {
		t.Fatalf("wrong explicit base: %+v", inv)
	}
	for _, tc := range []struct {
		path      string
		dir, want bool
	}{
		{filepath.Join("..", "settings", "generated", "deep", "x.md"), false, true},
		{filepath.Join("..", "settings", "generated"), true, true},
		{filepath.Join("..", "settings", "generated"), false, false},
		{filepath.Join("..", "settings", "direct.md"), false, true},
		{"generated/x.md", false, false},
		{filepath.Join("..", "settings", "other.md"), false, false},
	} {
		got, err := inv.excludes(tc.path, tc.dir)
		if err != nil || got != tc.want {
			t.Fatalf("excludes %q=%v,%v want %v", tc.path, got, err, tc.want)
		}
	}
	_, err = inv.excludes(filepath.Join(filepath.Dir(selected), strings.Repeat("x", config.MaxMatchNameBytes+1)), false)
	if !errors.Is(err, config.ErrPatternLimit) {
		t.Fatalf("matching cause lost: %v", err)
	}
}

func TestLintRejectsConfigNonregularAndSymlink(t *testing.T) {
	root := validProject(t)
	for _, name := range []string{"directory", "link.yaml"} {
		path := filepath.Join(root, name)
		if name == "directory" {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.Symlink(filepath.Join(root, config.Filename), path); err != nil {
				t.Skip(err)
			}
		}
		var stderr bytes.Buffer
		if status := Run([]string{"lint", "--config", path}, io.Discard, &stderr, "dev"); status != 12 || !strings.Contains(stderr.String(), path) {
			t.Fatalf("%s: status=%d %s", name, status, &stderr)
		}
	}
}

func TestLintMetadataBypassesPreparation(t *testing.T) {
	for _, flag := range []string{"--help", "--version"} {
		status := run([]string{"lint", "--config", "missing.yaml", flag}, io.Discard, io.Discard, "dev", func(lintOptions) (*lintInvocation, error) { t.Fatal("metadata prepared project"); return nil, nil }, func(*lintInvocation, io.Writer, io.Writer) int { t.Fatal("metadata continued"); return 0 })
		if status != 0 {
			t.Fatal(status)
		}
	}
}

func TestLintPreparationPreservesConfigurationCause(t *testing.T) {
	root := validProject(t)
	inv, err := prepareLint(lintOptions{config: "missing.yaml"})
	var located *config.ValidationError
	if inv != nil || !errors.Is(err, os.ErrNotExist) || !errors.As(err, &located) || located.Origin.Path != filepath.Join(root, "missing.yaml") {
		t.Fatalf("missing cause or origin: %v %v", inv, err)
	}
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	put(t, outside, availableConfig)
	if inv, err := prepareLint(lintOptions{config: outside}); inv != nil || err == nil {
		t.Fatalf("explicit config widened repository boundary: %v %v", inv, err)
	}
}

func TestLintExclusionBaseWithoutFile(t *testing.T) {
	// Admission currently rejects the unavailable default GFM before reaching
	// this continuation. The binding still defines the no-file selection base.
	root := project(t)
	patterns, err := config.CompilePatterns([]string{"generated/"})
	if err != nil {
		t.Fatal(err)
	}
	inv := lintInvocation{workingDirectory: root, exclusionBase: root, exclusions: patterns}
	if got, err := inv.excludes("generated/a.md", false); err != nil || !got {
		t.Fatalf("cwd exclusion base: %v %v", got, err)
	}
}
