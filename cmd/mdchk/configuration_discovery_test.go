package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

// These cases deliberately run the production executable. Definitions beside a
// target cannot accidentally supply settings to an invocation elsewhere.
func testConfigurationDiscovery(t *testing.T, binary string) {
	t.Run("working directory controls discovery", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", availableConfiguration)
		putConfigurationFile(t, root, "targets/.mdchk.yaml", "version: 2\n")
		putConfigurationFile(t, root, "targets/guide.md", "# untouched\n")
		got := runConfiguration(t, binary, root, root, "lint", "--format=json", "targets/guide.md")
		requireDiscoveryChecked(t, got, "targets/guide.md")
	})
	t.Run("relative custom sibling uses selected pattern base", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\n")
		putConfigurationFile(t, root, "work/.mdchk.yaml", "version: 2\n")
		putConfigurationFile(t, root, "settings/.mdchk.yaml", "version: 2\n")
		putConfigurationFile(t, root, "settings/custom.yaml", availableConfiguration+"exclude: ['generated/']\n")
		putConfigurationFile(t, root, "settings/generated/skip.md", "# excluded\n")
		putConfigurationFile(t, root, "work/generated/keep.md", "# selected\n")
		got := runConfiguration(t, binary, filepath.Join(root, "work"), root, "lint", "--format=json", "--config", "../settings/custom.yaml", "../settings/generated/skip.md", "generated/keep.md")
		requireDiscoveryChecked(t, got, "generated/keep.md")
		requireDiscoverySkip(t, got, "../settings/generated")
	})
	t.Run("invalid nearest has no fallback", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", availableConfiguration)
		putConfigurationFile(t, root, "child/.mdchk.yaml", "version: 2\n")
		putConfigurationFile(t, root, "child/guide.md", "# untouched\n")
		got := runConfiguration(t, binary, filepath.Join(root, "child"), root, "lint", "--format=json", "guide.md")
		requireDiscoveryConfigurationError(t, got, ".mdchk.yaml", "version")
	})
	for _, marker := range []string{"directory", "worktree file"} {
		t.Run(marker+" boundary includes root and ignores outer policy", func(t *testing.T) {
			outer := configurationProject(t)
			putConfigurationFile(t, outer, ".mdchk.yaml", "version: 2\nallow_nested_excludes: false\n")
			repo := filepath.Join(outer, "nested")
			if marker == "directory" {
				if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
					t.Fatal(err)
				}
			} else {
				putConfigurationFile(t, outer, "nested/.git", "gitdir: /not-read-by-discovery\n")
			}
			putConfigurationFile(t, repo, ".mdchk.yaml", availableConfiguration+"exclude: ['skip/']\n")
			putConfigurationFile(t, repo, "deep/guide.md", "# kept\n")
			putConfigurationFile(t, repo, "skip/guide.md", "# excluded\n")
			got := runConfiguration(t, binary, filepath.Join(repo, "deep"), outer, "lint", "--format=json", "guide.md", "../skip/guide.md")
			requireDiscoveryChecked(t, got, "guide.md")
			requireDiscoverySkip(t, got, "../skip")
		})
	}
	t.Run("explicit configuration cannot escape repository", func(t *testing.T) {
		outer := configurationProject(t)
		putConfigurationFile(t, outer, "outside.yaml", availableConfiguration)
		putConfigurationFile(t, outer, "repo/.git", "gitdir: unused\n")
		putConfigurationFile(t, outer, "repo/.mdchk.yaml", availableConfiguration)
		putConfigurationFile(t, outer, "repo/guide.md", "# untouched\n")
		got := runConfiguration(t, binary, filepath.Join(outer, "repo"), outer, "lint", "--format=json", "--config", "../outside.yaml", "guide.md")
		requireDiscoveryConfigurationError(t, got, "../outside.yaml", "boundary")
	})
	t.Run("explicit configuration cannot enter nested repository", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", availableConfiguration)
		putConfigurationFile(t, root, "nested/.git", "gitdir: unused\n")
		putConfigurationFile(t, root, "nested/custom.yaml", availableConfiguration)
		putConfigurationFile(t, root, "guide.md", "# untouched\n")
		got := runConfiguration(t, binary, root, root, "lint", "--format=json", "--config", "nested/custom.yaml", "guide.md")
		requireDiscoveryConfigurationError(t, got, "nested/custom.yaml", "repository scope")
	})
	for _, intermediate := range []bool{false, true} {
		name := "configuration symlink"
		if intermediate {
			name = "intermediate directory symlink"
		}
		t.Run(name, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", availableConfiguration)
			putConfigurationFile(t, root, "real/custom.yaml", availableConfiguration)
			putConfigurationFile(t, root, "guide.md", "# untouched\n")
			link, destination, selected := "link.yaml", filepath.Join(root, "real/custom.yaml"), "link.yaml"
			if intermediate {
				link, destination, selected = "linked", filepath.Join(root, "real"), "linked/custom.yaml"
			}
			if err := os.Symlink(destination, filepath.Join(root, link)); err != nil {
				t.Skipf("symlink fixture unavailable: %v", err)
			}
			got := runConfiguration(t, binary, root, root, "lint", "--format=json", "--config", selected, "guide.md")
			requireDiscoveryConfigurationError(t, got, selected, "")
		})
	}
	t.Run("invalid ancestor is validated", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\nrules: {MD013: {enabled: false, length: -1}}\n")
		putConfigurationFile(t, root, "child/.mdchk.yaml", availableConfiguration)
		putConfigurationFile(t, root, "child/guide.md", "# untouched\n")
		got := runConfiguration(t, binary, filepath.Join(root, "child"), root, "lint", "--format=json", "guide.md")
		requireDiscoveryConfigurationError(t, got, "../.mdchk.yaml", "length")
		requireDiscoveryErrorSource(t, got, filepath.Join(root, "child"), "../.mdchk.yaml", 2, 41)
	})
	t.Run("ancestor execution settings are not admitted or merged", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\nflavor: commonmark\nrules: {MD001: {}, MD013: {enabled: true, length: 99}}\nexclude: ['child/']\n")
		putConfigurationFile(t, root, "child/.mdchk.yaml", availableConfiguration)
		putConfigurationFile(t, root, "child/guide.md", "# untouched\n")
		got := runConfiguration(t, binary, filepath.Join(root, "child"), root, "lint", "--format=json", "guide.md")
		requireDiscoveryChecked(t, got, "guide.md")
		for _, c := range got.Result.Coverage {
			if c.Rule == "MD013" {
				t.Fatal("ancestor enabled state was inherited")
			}
		}
	})
	t.Run("ancestor global false dominates selected true", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\ndirectives: {enabled: false}\n")
		putConfigurationFile(t, root, "child/.mdchk.yaml", "version: 1\nflavor: commonmark\ndirectives: {enabled: true}\n")
		putConfigurationFile(t, root, "child/guide.md", "<!-- mdchk-disable MD001 -->\n\n# untouched\n")
		got := runConfiguration(t, binary, filepath.Join(root, "child"), root, "lint", "--format=json", "guide.md")
		requireDiscoveryChecked(t, got, "guide.md")
	})
	t.Run("exclusion lock survives intermediate true and custom selection", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\nallow_nested_excludes: false\n")
		putConfigurationFile(t, root, "middle/.mdchk.yaml", "version: 1\nallow_nested_excludes: true\n")
		putConfigurationFile(t, root, "middle/leaf/custom.yaml", availableConfiguration+"allow_nested_excludes: true\nexclude: ['generated/']\n")
		putConfigurationFile(t, root, "middle/leaf/guide.md", "# untouched\n")
		cwd := filepath.Join(root, "middle/leaf")
		got := runConfiguration(t, binary, cwd, root, "lint", "--format=json", "--config", "custom.yaml", "guide.md")
		requireDiscoveryConfigurationError(t, got, "custom.yaml", "nonempty exclusions prohibited")
		requireDiscoveryErrorSource(t, got, cwd, "custom.yaml", 5, 10)
		requireDiscoveryErrorSource(t, got, cwd, "../../.mdchk.yaml", 2, 24)
		if len(got.Result.Errors) != 2 {
			t.Fatalf("policy must retain selected and governing locations: %+v", got.Result.Errors)
		}
	})
	t.Run("definition can forbid nested exclusions and retain own", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", availableConfiguration+"allow_nested_excludes: false\nexclude: ['generated/']\n")
		putConfigurationFile(t, root, "guide.md", "# kept\n")
		putConfigurationFile(t, root, "generated/deep/guide.md", "# excluded\n")
		got := runConfiguration(t, binary, root, root, "lint", "--format=json", "generated/deep/guide.md", "guide.md")
		requireDiscoveryChecked(t, got, "guide.md")
		requireDiscoverySkip(t, got, "generated")
		got = runConfiguration(t, binary, root, root, "lint", "--format=json", "generated/deep/guide.md")
		if got.Status != 11 || len(got.Result.CheckedFiles) != 0 || len(got.Result.Files) != 0 || len(got.Result.Diagnostics) != 0 || len(got.Result.Errors) != 1 || got.Result.Errors[0].Category != result.Usage {
			t.Fatalf("excluded-only result: %+v", got)
		}
		requireDiscoverySkip(t, got, "generated")
	})
}

func requireDiscoveryChecked(t *testing.T, got configurationRun, paths ...string) {
	t.Helper()
	if got.Status != 15 || got.Result.Complete || got.Result.Status != "incomplete" || !reflect.DeepEqual(got.Result.CheckedFiles, paths) || len(got.Result.Errors) != 0 || len(got.Result.Diagnostics) != 0 {
		t.Fatalf("expected production missing-rule coverage for %v: %+v", paths, got)
	}
	found := false
	for _, c := range got.Result.Coverage {
		if c.Rule == "MD010" && c.Reason == "rule-unavailable" {
			found = true
		}
	}
	if !found {
		t.Fatal("implicit default-enabled MD010 missing from coverage")
	}
}

func requireDiscoverySkip(t *testing.T, got configurationRun, path string) {
	t.Helper()
	for _, skip := range got.Result.SkippedFiles {
		if skip.Path == path && skip.Reason == "excluded" {
			return
		}
	}
	t.Fatalf("missing excluded skip %q: %+v", path, got.Result.SkippedFiles)
}

func requireDiscoveryConfigurationError(t *testing.T, got configurationRun, path, message string) {
	t.Helper()
	// Rejected configuration completes preflight; no Markdown work was requested
	// from the engine, so complete remains true with an error status.
	if got.Status != 12 || got.Result.Status != "error" || !got.Result.Complete || len(got.Result.CheckedFiles) != 0 || len(got.Result.Files) != 0 || len(got.Result.Diagnostics) != 0 || len(got.Result.Coverage) != 0 || len(got.Result.Errors) == 0 {
		t.Fatalf("configuration did not stop before target processing: %+v", got)
	}
	found := false
	for _, e := range got.Result.Errors {
		if e.Category != result.Configuration {
			t.Fatalf("nonconfiguration error: %+v", e)
		}
		if e.Path == path && strings.Contains(e.Message, message) {
			found = true
		}
	}
	if !found {
		t.Fatalf("configuration origin/message: %+v; want %q containing %q", got.Result.Errors, path, message)
	}
}

func requireDiscoveryErrorSource(t *testing.T, got configurationRun, cwd, path string, line, column int) {
	t.Helper()
	//nolint:gosec // Read only the test-owned source named by the assertion.
	data, err := os.ReadFile(filepath.Join(cwd, path))
	if err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, e := range got.Result.Errors {
		if e.Path == path && e.Range != nil && e.Range.Start.Line == line && e.Range.Start.Column == column && e.Snapshot != nil && e.Snapshot.View == "original" && e.Snapshot.SHA256 == hash && e.Snapshot.SizeBytes == len(data) {
			return
		}
	}
	t.Fatalf("missing original configuration location %s:%d:%d: %+v", path, line, column, got.Result.Errors)
}
