package main

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func md084Configuration(flavor, setting string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD084: {enabled: false}", "MD084: "+setting, 1)
}

func TestCompiledPlatformMD084ProductionAnalysis(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, tc := range []struct {
		id, source, flavor, setting string
		ranges                      [][2]int
		messages                    []string
	}{
		{"basic", "word\u200bword", "commonmark", "{enabled: true}", [][2]int{{4, 7}}, []string{"U+200B"}},
		{"allowed", "word\u200bword", "gfm", "{enabled: true, allowed_codepoints: [200b]}", nil, nil},
		{"allowed-other", "a\u200bb\u202ec", "gfm", "{enabled: true, allowed_codepoints: ['00200B']}", [][2]int{{5, 8}}, []string{"U+202E"}},
		{"leading-bom", "\ufeffa\ufeffb", "commonmark", "{enabled: true}", [][2]int{{4, 7}}, []string{"U+FEFF"}},
		{"code-span", "`a\u200bb`", "gfm", "{enabled: true}", nil, nil},
		{"fenced", "~~~\na\u200bb\n~~~", "commonmark", "{enabled: true}", nil, nil},
		{"nested", "- item\u200b\n\n> quote\u202e", "gfm", "{enabled: true}", [][2]int{{6, 9}, {18, 21}}, []string{"U+200B", "U+202E"}},
		{"crlf", "a\r\nb\u200bc", "commonmark", "{enabled: true}", [][2]int{{4, 7}}, []string{"U+200B"}},
	} {
		t.Run(tc.id, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md084Configuration(tc.flavor, tc.setting))
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
					if d.Rule != "MD084" || d.Message != "Hidden or discouraged Unicode character "+tc.messages[i]+"." || d.ByteRange.Start != w[0] || d.ByteRange.End != w[1] || d.FixAvailable || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.source))) || d.Snapshot.SizeBytes != len(tc.source) {
						t.Fatal(d)
					}
					if tc.id == "crlf" && (d.Range.Start.Line != 2 || d.Range.Start.Column != 2) || tc.id == "basic" && (d.Range.Start.Line != 1 || d.Range.Start.Column != 5) {
						t.Fatal(d.Range)
					}
				}
			}
			readinessAssertEqual(t, "report-only MD084", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD084OptionsAndInheritance(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, setting := range []string{"{enabled: false, allowed_codepoints: null}", "{enabled: false, allowed_codepoints: [U+200B]}", "{enabled: true, allowed_codepoints: ['0x200b']}", "{enabled: false, allowed_codepoints: [D800]}", "{enabled: false, unknown: true}", "{enabled: false, allow_directives: null}"} {
		t.Run(setting, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md084Configuration("gfm", setting))
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
			parent := strings.Replace(md084Configuration("commonmark", "{enabled: true}"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			if denial == "global" {
				parent = strings.Replace(parent, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			if denial == "rule" {
				parent = strings.Replace(parent, "MD084: {enabled: true}", "MD084: {enabled: true, allow_directives: false}", 1)
			}
			child := strings.Replace(md084Configuration("commonmark", "{enabled: true, allow_directives: true}"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", parent)
			putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
			putConfigurationFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD084 -->\na\u200bb")
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
