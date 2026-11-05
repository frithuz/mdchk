package main

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func md061Configuration(flavor, setting string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD061: {enabled: false}", "MD061: "+setting, 1)
}

func TestCompiledPlatformMD061ProductionAnalysis(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, tc := range []struct {
		id, source, flavor, setting string
		ranges                      [][2]int
	}{
		{"basic", "Use blacklist.", "commonmark", "{enabled: true, terms: [blacklist]}", [][2]int{{4, 13}}},
		{"insensitive", "Use BLACKLIST.", "gfm", "{enabled: true, terms: [blacklist]}", [][2]int{{4, 13}}},
		{"sensitive", "Use BLACKLIST.", "gfm", "{enabled: true, terms: [blacklist], case_sensitive: true}", nil},
		{"overlap", "Use blacklist.", "commonmark", "{enabled: true, terms: [black, blacklist]}", [][2]int{{4, 13}}},
		{"empty-terms", "Use blacklist.", "commonmark", "{enabled: true, terms: ['']}", nil},
		{"code-span", "Use `blacklist`.", "gfm", "{enabled: true, terms: [blacklist]}", nil},
	} {
		t.Run(tc.id, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md061Configuration(tc.flavor, tc.setting))
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := readinessState(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"fix", "doc.md", "--format=json"}} {
				got := runConfigurationProcess(t, binary, root, args...)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
				if args[0] == "fix" && args[1] == "doc.md" && md038NativeRefusal(t, got, out) {
					continue
				}
				status := 0
				if len(tc.ranges) > 0 && args[0] != "inspect" {
					status = 10
				}
				if got.Status != status || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(tc.ranges) || len(out.Changes)+len(out.DeferredFixes)+len(out.Documents) != 0 {
					t.Fatal(got)
				}
				for i, d := range out.Diagnostics {
					w := tc.ranges[i]
					if args[0] != "fix" && d.Snapshot.View != "original" || args[0] == "fix" && d.Snapshot.View != "candidate" && d.Snapshot.View != "saved" {
						t.Fatal(d.Snapshot)
					}
					if d.Rule != "MD061" || d.Message != "Configured prohibited term." || d.ByteRange.Start != w[0] || d.ByteRange.End != w[1] || d.FixAvailable || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.source))) || d.Snapshot.SizeBytes != len(tc.source) {
						t.Fatal(d)
					}
				}
			}
			readinessAssertEqual(t, "report-only MD061", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD061OptionsAndInheritance(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, setting := range []string{"{enabled: false, terms: null}", "{enabled: false, terms: [blacklist, 1]}", "{enabled: false, case_sensitive: 1}", "{enabled: false, unknown: true}", "{enabled: false, allow_directives: null}"} {
		t.Run(setting, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md061Configuration("gfm", setting))
			got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
			out := validateConfigurationJSON(t, []byte(got.Stdout))
			if got.Status != 12 || len(out.CheckedFiles) != 0 {
				t.Fatal(got)
			}
		})
	}
	for _, denial := range []string{"allowed", "global", "rule"} {
		t.Run(denial, func(t *testing.T) {
			root := configurationProject(t)
			parent := strings.Replace(md061Configuration("commonmark", "{enabled: true, terms: [blacklist]}"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			if denial == "global" {
				parent = strings.Replace(parent, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			if denial == "rule" {
				parent = strings.Replace(parent, "terms: [blacklist]", "terms: [blacklist], allow_directives: false", 1)
			}
			child := strings.Replace(md061Configuration("commonmark", "{enabled: true, terms: [blacklist], allow_directives: true}"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", parent)
			putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
			putConfigurationFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD061 -->\nblacklist")
			got, p := runInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=2", "--format=json")
			active, hidden := 1, 0
			if denial == "allowed" {
				active, hidden = 0, 1
			}
			if got.Status != 0 || len(got.Result.Diagnostics) != active || len(got.Result.SuppressedDiagnostics) != hidden || p.File.SelectedLines.Start != 2 {
				t.Fatal(got, p)
			}
		})
	}
}
