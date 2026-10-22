//go:build darwin && arm64 && cgo

package main

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCompiledApplySelectionAndInheritedPolicy(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, false))
	putConfigurationFile(t, root, "nested/.mdchk.yaml", md047Configuration(true, true)+"exclude: ['excluded.md']\n")
	sources := map[string]string{"safe.md": "safe", "blocked.md": "<!-- mdchk:disable MD047 -->\ntext", "manual.md": "```\nprotected", "excluded.md": "excluded"}
	for p, s := range sources {
		putConfigurationFile(t, root, "nested/"+p, s)
	}
	child := filepath.Join(root, "nested")
	inventory := configurationBytes(t, root)
	protected := map[string]os.FileInfo{}
	for _, p := range []string{".mdchk.yaml", "nested/.mdchk.yaml", "nested/excluded.md", "nested/manual.md"} {
		info, e := os.Stat(filepath.Join(root, p))
		if e != nil {
			t.Fatal(e)
		}
		protected[p] = info
	}
	got := runAppliedFix(t, binary, child, "fix", ".", "--format=json")
	if got.Status != 10 || len(got.Result.Changes) != 2 || len(got.Result.Diagnostics) != 1 || got.Result.Diagnostics[0].Path != "manual.md" {
		t.Fatal(got)
	}
	for p, s := range sources {
		want := s
		status := "not-needed"
		if p == "safe.md" || p == "blocked.md" {
			want += "\n"
			status = "written"
		}
		if p != "excluded.md" {
			assertSavedApplyFile(t, child, p, want, status, got, "json")
		}
		inventory[filepath.Join(child, p)] = sha256.Sum256([]byte(want))
	}
	excluded := false
	for _, s := range got.Result.SkippedFiles {
		if s.Path == "excluded.md" && s.Reason == "excluded" {
			excluded = true
		}
	}
	if !excluded || len(got.Result.SuppressedDiagnostics) != 0 {
		t.Fatal(got)
	}
	if !reflect.DeepEqual(inventory, configurationBytes(t, root)) {
		t.Fatal("policy/exclusion changed unrelated bytes")
	}
	for p, info := range protected {
		after, e := os.Stat(filepath.Join(root, p))
		if e != nil || !os.SameFile(info, after) || info.Mode() != after.Mode() || !info.ModTime().Equal(after.ModTime()) {
			t.Fatal("changed protected/config/excluded metadata", p, e)
		}
	}
	verify := unchangedFixFixture(t, root)
	second := runAppliedFix(t, binary, child, "fix", ".", "--format=json")
	if second.Status != 10 || len(second.Result.Changes) != 0 {
		t.Fatal(second)
	}
	verify()
}

func TestCompiledApplyMalformedExistingConfigBeforeTargets(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	config := strings.Replace(md047Configuration(true, true), "MD047: {enabled: true, allow_directives: true}", "MD047: {enabled: true, unknown_option: true}", 1)
	putConfigurationFile(t, root, ".mdchk.yaml", config)
	putConfigurationFile(t, root, "a.md", "a")
	putConfigurationFile(t, root, "b.md", "b")
	verify := unchangedFixFixture(t, root)
	for _, format := range []string{"json", "text"} {
		got := runAppliedFix(t, binary, root, "fix", "a.md", "b.md", "--format="+format)
		if got.Status != 12 {
			t.Fatal(got)
		}
		if format == "json" && (len(got.Result.Changes) != 0 || len(got.Result.CheckedFiles) != 0 || len(got.Result.Errors) == 0) {
			t.Fatal(got)
		}
		verify()
	}
}

func TestCompiledApplySymlinkSelection(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, kind := range []string{"symlink-target", "symlink-parent"} {
		t.Run(kind, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
			putConfigurationFile(t, root, "a.md", "a")
			putConfigurationFile(t, root, "c.md", "c")
			outside := configurationProject(t)
			putConfigurationFile(t, outside, "external.md", "external")
			externalVerify := unchangedFixFixture(t, outside)
			bad := "b.md"
			switch kind {
			case "symlink-target":
				if e := os.Symlink(filepath.Join(outside, "external.md"), filepath.Join(root, bad)); e != nil {
					t.Fatal(e)
				}
			case "symlink-parent":
				bad = filepath.Join("linked", "external.md")
				if e := os.Symlink(outside, filepath.Join(root, "linked")); e != nil {
					t.Fatal(e)
				}
			}
			entry := filepath.Join(root, "b.md")
			if kind == "symlink-parent" {
				entry = filepath.Join(root, "linked")
			}
			before, e := os.Lstat(entry)
			if e != nil {
				t.Fatal(e)
			}
			inventory := configurationBytes(t, root)
			got := runAppliedFix(t, binary, root, "fix", "a.md", bad, "c.md", "--format=json")
			if got.Status != 0 {
				t.Fatal(got)
			}
			for _, p := range []string{"a.md", "c.md"} {
				assertSavedApplyFile(t, root, p, strings.TrimSuffix(p, ".md")+"\n", "written", got, "json")
				inventory[filepath.Join(root, p)] = sha256.Sum256([]byte(strings.TrimSuffix(p, ".md") + "\n"))
			}
			skipped := false
			expectedSkip := filepath.Base(entry)
			for _, skip := range got.Result.SkippedFiles {
				if skip.Path == expectedSkip && skip.Reason == "symlink" {
					skipped = true
				}
			}
			if !skipped {
				t.Fatal("missing symlink selection refusal", got)
			}
			after, e := os.Lstat(entry)
			if e != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("refusal changed native permissions/path", e)
			}
			if !reflect.DeepEqual(inventory, configurationBytes(t, root)) {
				t.Fatal("native refusal changed protected bytes/inventory")
			}
			externalVerify()
		})
	}
}
