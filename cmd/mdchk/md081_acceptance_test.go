package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func md081Configuration(flavor string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD081: {enabled: false}", "MD081: {enabled: true, minimum_characters: 1}", 1)
}
func TestCompiledPlatformMD081ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		name, source, flavor                         string
		start, end, line, column, endline, endcolumn int
		finding                                      bool
	}{
		{"basic", "**ab** c", "commonmark", 0, 8, 1, 1, 1, 9, true}, {"quote-crlf", "> **ab**\r\n> c\r\n", "gfm", 2, 13, 1, 3, 2, 4, true}, {"task", "- [x] **ab**", "gfm", 6, 12, 1, 7, 1, 13, true}, {"unicode", "雪 **ab** c", "commonmark", 0, 12, 1, 1, 1, 11, false}, {"equality", "**ab** cd", "gfm", 0, 0, 0, 0, 0, 0, false}, {"protected", "`**ab**`", "gfm", 0, 0, 0, 0, 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md081Configuration(tc.flavor))
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := readinessState(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"fix", "doc.md", "--format=json"}} {
				got := runConfigurationProcess(t, binary, root, args...)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
				if args[0] == "fix" && args[1] == "doc.md" && md038NativeRefusal(t, got, out) {
					continue
				}
				status, want := 0, 0
				if tc.finding {
					want = 1
					if args[0] != "inspect" {
						status = 10
					}
				}
				if got.Status != status || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != want || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(got, out)
				}
				if tc.finding {
					d := out.Diagnostics[0]
					view := "original"
					if args[0] == "fix" {
						view = "saved"
						if args[1] == "--dry-run" {
							view = "candidate"
						}
					}
					if d.Rule != "MD081" || d.Message != "Paragraph contains excessive bold or italic text." || d.FixAvailable || d.ByteRange.Start != tc.start || d.ByteRange.End != tc.end || d.Range.Start.Line != tc.line || d.Range.Start.Column != tc.column || d.Range.End.Line != tc.endline || d.Range.End.Column != tc.endcolumn || d.Snapshot.View != view || d.Snapshot.SizeBytes != len(tc.source) || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.source))) {
						t.Fatal(d)
					}
				}
			}
			readinessAssertEqual(t, "manual MD081", before, readinessState(t, root))
		})
	}
}
func TestCompiledPlatformMD081TypedOptionsAndDefaults(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		setting          string
		status, findings int
	}{
		{"OMITTED", 0, 0}, {"{}", 0, 0}, {"{enabled: false}", 0, 0}, {"{enabled: true, minimum_characters: 1, maximum_ratio: 0}", 10, 1}, {"{enabled: true, minimum_characters: 1, maximum_ratio: 1}", 0, 0}, {"{enabled: true, minimum_characters: 1, maximum_ratio: 0.6666666666666666}", 0, 0}, {"{enabled: true, minimum_characters: 1, maximum_ratio: 0.6666666666666665}", 10, 1},
	} {
		t.Run(tc.setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := md081Configuration("gfm")
			if tc.setting == "OMITTED" {
				conf = strings.Replace(conf, "  MD081: {enabled: true, minimum_characters: 1}\n", "", 1)
			} else {
				conf = strings.Replace(conf, "MD081: {enabled: true, minimum_characters: 1}", "MD081: "+tc.setting, 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			source := "**ab** c"
			if tc.setting == "OMITTED" || tc.setting == "{}" || tc.setting == "{enabled: false}" {
				source = "**This entire paragraph is strongly emphasized.**"
			}
			putConfigurationFile(t, root, "doc.md", source)
			before := readinessState(t, root)
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
			if got.Status != tc.status || !out.Complete || len(out.Diagnostics) != tc.findings {
				t.Fatal(got, out)
			}
			readinessAssertEqual(t, "typed analysis", before, readinessState(t, root))
		})
	}
	for _, setting := range []string{"null", "{enabled: null}", "{allow_directives: null}", "{enabled: false, minimum_characters: 20.0}", "{enabled: false, minimum_characters: 2e1}", "{enabled: false, maximum_ratio: 1.01}", "{enabled: false, maximum_ratio: .nan}", "{enabled: false, unknown: 1}"} {
		t.Run(setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md081Configuration("gfm"), "MD081: {enabled: true, minimum_characters: 1}", "MD081: "+setting, 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			before := readinessState(t, root)
			got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
			if got.Status != 12 {
				t.Fatal(got)
			}
			readinessAssertEqual(t, "invalid settings", before, readinessState(t, root))
		})
	}
}
func TestCompiledPlatformMD081DirectiveInheritanceAndSelection(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, denial := range []string{"allowed", "global", "rule"} {
		t.Run(denial, func(t *testing.T) {
			root := configurationProject(t)
			parent := strings.Replace(md081Configuration("gfm"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			if denial == "global" {
				parent = strings.Replace(parent, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			if denial == "rule" {
				parent = strings.Replace(parent, "MD081: {enabled: true, minimum_characters: 1}", "MD081: {enabled: true, minimum_characters: 1, allow_directives: false}", 1)
			}
			child := strings.Replace(md081Configuration("gfm"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			child = strings.Replace(child, "minimum_characters: 1}", "minimum_characters: 1, allow_directives: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", parent)
			putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
			putConfigurationFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD081 -->\n<!-- mdchk:disable MD081 -->\n**ab**\nc\n\nordinary")
			before := readinessState(t, root)
			got, p := runInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=4", "--format=json")
			active, hidden := 1, 0
			if denial == "allowed" {
				active, hidden = 0, 1
			}
			if got.Status != 0 || len(got.Result.Diagnostics) != active || len(got.Result.SuppressedDiagnostics) != hidden || p.File.SelectedLines.Start != 4 {
				t.Fatal(got, p)
			}
			got, _ = runInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=6", "--format=json")
			if got.Status != 0 || len(got.Result.Diagnostics)+len(got.Result.SuppressedDiagnostics) != 0 {
				t.Fatal(got)
			}
			readinessAssertEqual(t, "inspection", before, readinessState(t, root))
		})
	}
}
func TestCompiledPlatformMD081FourReporters(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md081Configuration("gfm"))
	putConfigurationFile(t, root, "doc.md", "**ab** c")
	before := readinessState(t, root)
	for _, format := range []string{"text", "json", "vscode", "github"} {
		t.Run(format, func(t *testing.T) {
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format="+format)
			if got.Status != 10 || !strings.Contains(got.Stdout, "MD081") || !strings.Contains(got.Stdout, "Paragraph contains excessive bold or italic text.") {
				t.Fatal(got)
			}
			switch format {
			case "text":
				if !strings.Contains(got.Stdout, "doc.md:1:1 MD081:") {
					t.Fatal(got)
				}
			case "vscode":
				if !strings.Contains(got.Stdout, "doc.md:1:1: error: MD081:") {
					t.Fatal(got)
				}
			case "github":
				if strings.Count(got.Stdout, "::error file=doc.md,line=1,title=MD081::") != 1 {
					t.Fatal(got)
				}
			case "json":
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
				if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange.Start != 0 || out.Diagnostics[0].ByteRange.End != 8 {
					t.Fatal(out)
				}
			}
		})
	}
	readinessAssertEqual(t, "reporters", before, readinessState(t, root))
}
func TestCompiledPlatformMD081MD047Composition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, source := range []string{"**ab** c", "`**ab**`"} {
		t.Run(source, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md081Configuration("gfm"), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", source)
			before := readinessState(t, root)
			protected := source == "`**ab**`"
			for _, dry := range []bool{true, false} {
				args := []string{"fix", "doc.md", "--format=json"}
				if dry {
					args = []string{"fix", "--dry-run", "doc.md", "--format=json"}
				}
				got := runConfigurationProcess(t, binary, root, args...)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
				if !dry && md038NativeRefusal(t, got, out) {
					readinessAssertEqual(t, "refusal", before, readinessState(t, root))
					return
				}
				want, view, disposition := source, "saved", "written"
				changes, deferred := 1, 0
				if protected {
					changes, deferred = 0, 1
				} else {
					want += "\n"
				}
				if dry {
					view, disposition = "candidate", "proposed"
				}
				if got.Status != 10 || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != 1 || len(out.Changes) != changes || len(out.DeferredFixes) != deferred || len(out.Documents) != 0 {
					t.Fatal(got, out)
				}
				d := out.Diagnostics[0]
				if !protected {
					if d.Rule != "MD081" || d.FixAvailable || d.ByteRange.Start != 0 || d.ByteRange.End != 8 || d.Snapshot.View != view || d.Snapshot.SizeBytes != 9 || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(want))) {
						t.Fatal(d)
					}
					ch := out.Changes[0]
					if len(ch.Rules) != 1 || ch.Rules[0] != "MD047" || ch.Disposition != disposition {
						t.Fatal(ch)
					}
				} else if d.Rule != "MD047" || d.FixAvailable || out.DeferredFixes[0].Rule != "MD047" {
					t.Fatal(out)
				}
				//nolint:gosec // Isolated fixed test-owned document.
				actual, e := os.ReadFile(filepath.Join(root, "doc.md"))
				saved := source
				if !dry {
					saved = want
				}
				if e != nil || string(actual) != saved {
					t.Fatal(string(actual), e)
				}
				if dry || protected {
					readinessAssertEqual(t, "unchanged", before, readinessState(t, root))
				}
			}
		})
	}
}
func TestCompiledPlatformMD081NoCGORefusal(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	conf := strings.Replace(md081Configuration("gfm"), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	putConfigurationFile(t, root, ".mdchk.yaml", conf)
	putConfigurationFile(t, root, "doc.md", "**ab** c")
	before := readinessState(t, root)
	got := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
	out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
	if !md038NativeRefusal(t, got, out) {
		t.Fatal(got)
	}
	readinessAssertEqual(t, "actual cgo0 early refusal", before, readinessState(t, root))
}
