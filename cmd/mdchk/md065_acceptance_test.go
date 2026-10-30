package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func md065Configuration(flavor string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD065: {enabled: false}", "MD065: {}", 1)
}

func TestCompiledPlatformMD065ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		name, source, flavor string
		points               [][3]int
	}{
		{"basic", "Text\n***\nMore", "commonmark", [][3]int{{5, 2, 1}, {8, 2, 4}}},
		{"unicode-crlf", "雪\r\n***\r\nMore", "gfm", [][3]int{{5, 2, 1}, {8, 2, 4}}},
		{"quote", " > Text\n > ***\n > More", "gfm", [][3]int{{11, 2, 4}, {14, 2, 7}}},
		{"partial-tab", "- Text\n\t***\n\tMore", "commonmark", [][3]int{{8, 2, 2}, {11, 2, 5}}},
		{"accepted", "Text\n\n***\n\nMore", "gfm", nil},
		{"code-protected", "`***`", "gfm", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md065Configuration(tc.flavor))
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := readinessState(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"fix", "doc.md", "--format=json"}} {
				got := runConfigurationProcess(t, binary, root, args...)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
				if args[0] == "fix" && args[1] == "doc.md" && md038NativeRefusal(t, got, out) {
					continue
				}
				status := 0
				if len(tc.points) > 0 && args[0] != "inspect" {
					status = 10
				}
				if got.Status != status || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(tc.points) || len(out.Changes)+len(out.DeferredFixes)+len(out.Documents) != 0 {
					t.Fatal(got)
				}
				for i, d := range out.Diagnostics {
					w := tc.points[i]
					view := "original"
					if args[0] == "fix" {
						view = "saved"
						if args[1] == "--dry-run" {
							view = "candidate"
						}
					}
					if d.Rule != "MD065" || d.Message != []string{"Horizontal rule requires a blank body line before it.", "Horizontal rule requires a blank body line after it."}[i] || d.ByteRange.Start != w[0] || d.ByteRange.End != w[0] || d.Range.Start.Line != w[1] || d.Range.Start.Column != w[2] || d.Range.End != d.Range.Start || d.FixAvailable || d.Snapshot.View != view || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.source))) || d.Snapshot.SizeBytes != len(tc.source) {
						t.Fatal(d)
					}
				}
			}
			readinessAssertEqual(t, "manual MD065", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD065StrictOptionsAndDefaults(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, setting := range []string{"null", "{enabled: null}", "{enabled: 'false'}", "{enabled: false, before: 1}", "{enabled: false, after: 1}", "{enabled: false, minimum: 1}", "{enabled: false, style: consistent}", "{enabled: false, allow_directives: null}"} {
		t.Run(setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md065Configuration("gfm"), "MD065: {}", "MD065: "+setting, 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			before := readinessState(t, root)
			got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
			out := validateConfigurationJSON(t, []byte(got.Stdout))
			if got.Status != 12 || len(out.CheckedFiles) != 0 {
				t.Fatal(got)
			}
			readinessAssertEqual(t, "rejected configuration", before, readinessState(t, root))
		})
	}
	for _, setting := range []string{"OMITTED", "{}", "{enabled: false}"} {
		t.Run(setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := md065Configuration("gfm")
			if setting == "OMITTED" {
				conf = strings.Replace(conf, "  MD065: {}\n", "", 1)
			} else {
				conf = strings.Replace(conf, "MD065: {}", "MD065: "+setting, 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "Text\n***\nMore")
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
			want, status := 2, 10
			if setting == "{enabled: false}" {
				want, status = 0, 0
			}
			if got.Status != status || !out.Complete || len(out.Diagnostics) != want {
				t.Fatal(got)
			}
		})
	}
}

func TestCompiledPlatformMD065DirectiveInheritanceAndSelection(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, denial := range []string{"allowed", "global", "rule"} {
		t.Run(denial, func(t *testing.T) {
			root := configurationProject(t)
			parent := strings.Replace(md065Configuration("commonmark"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			if denial == "global" {
				parent = strings.Replace(parent, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			if denial == "rule" {
				parent = strings.Replace(parent, "MD065: {}", "MD065: {allow_directives: false}", 1)
			}
			child := strings.Replace(md065Configuration("commonmark"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			child = strings.Replace(child, "MD065: {}", "MD065: {allow_directives: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", parent)
			putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
			putConfigurationFile(t, root, "nested/doc.md", "Text\n<!-- mdchk:disable MD065 -->\n***\nMore")
			got, p := runInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=3", "--format=json")
			active, hidden := 2, 0
			if denial == "allowed" {
				active, hidden = 0, 2
			}
			if got.Status != 0 || len(got.Result.Diagnostics) != active || len(got.Result.SuppressedDiagnostics) != hidden || p.File.SelectedLines.Start != 3 {
				t.Fatal(got, p)
			}
		})
	}
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md065Configuration("gfm"))
	putConfigurationFile(t, root, "doc.md", "Text\n***\nMore")
	got, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--lines=2", "--format=json")
	if got.Status != 0 || len(got.Result.Diagnostics) != 2 || p.File.SelectedLines.Start != 2 {
		t.Fatal(got, p)
	}
	got, p = runInspection(t, binary, root, root, "inspect", "doc.md", "--lines=3", "--format=json")
	if got.Status != 0 || len(got.Result.Diagnostics) != 0 {
		t.Fatal(got, p)
	}
}

func TestCompiledPlatformMD065FourReporters(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md065Configuration("commonmark"))
	putConfigurationFile(t, root, "doc.md", "Text\n***\nMore")
	for _, format := range []string{"text", "json", "vscode", "github"} {
		t.Run(format, func(t *testing.T) {
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format="+format)
			if got.Status != 10 || !strings.Contains(got.Stdout, "MD065") || !strings.Contains(got.Stdout, "Horizontal rule requires a blank body line before it.") || !strings.Contains(got.Stdout, "Horizontal rule requires a blank body line after it.") {
				t.Fatal(got)
			}
			switch format {
			case "text":
				if !strings.Contains(got.Stdout, "doc.md:2:1 MD065:") || !strings.Contains(got.Stdout, "doc.md:2:4 MD065:") {
					t.Fatal(got)
				}
			case "vscode":
				if !strings.Contains(got.Stdout, "doc.md:2:1: error: MD065:") || !strings.Contains(got.Stdout, "doc.md:2:4: error: MD065:") {
					t.Fatal(got)
				}
			case "github":
				if strings.Count(got.Stdout, "::error file=doc.md,line=2,title=MD065::") != 2 {
					t.Fatal(got)
				}
			case "json":
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
				if !out.Complete || len(out.Diagnostics) != 2 || out.Diagnostics[0].ByteRange.Start != 5 || out.Diagnostics[1].ByteRange.Start != 8 {
					t.Fatal(out)
				}
			}
		})
	}
}

func TestCompiledPlatformMD065MD047Composition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, source := range []string{"Text\n***\nMore", "`***`"} {
		t.Run(source, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md065Configuration("gfm"), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", source)
			before := readinessState(t, root)
			protected := source == "`***`"
			for _, dry := range []bool{true, false} {
				args := []string{"fix", "doc.md", "--format=json"}
				if dry {
					args = []string{"fix", "--dry-run", "doc.md", "--format=json"}
				}
				got := runConfigurationProcess(t, binary, root, args...)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
				if !dry && md038NativeRefusal(t, got, out) {
					readinessAssertEqual(t, "native refusal", before, readinessState(t, root))
					return
				}
				want, view, disposition := source, "saved", "written"
				changes, deferred, diagnostics := 1, 0, 2
				if !protected {
					want += "\n"
				} else {
					changes, deferred, diagnostics = 0, 1, 1
				}
				if dry {
					view, disposition = "candidate", "proposed"
				}
				if got.Status != 10 || !out.Complete || len(out.Errors) != 0 || len(out.Changes) != changes || len(out.DeferredFixes) != deferred || len(out.Diagnostics) != diagnostics || len(out.Documents) != 0 {
					t.Fatal(got, out)
				}
				if !protected {
					for i, d := range out.Diagnostics {
						point := []int{5, 8}[i]
						if d.Rule != "MD065" || d.ByteRange.Start != point || d.ByteRange.End != point || d.FixAvailable || d.Snapshot.View != view || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(want))) || d.Snapshot.SizeBytes != 14 {
							t.Fatal(d)
						}
					}
					ch := out.Changes[0]
					if len(ch.Rules) != 1 || ch.Rules[0] != "MD047" || ch.Disposition != disposition {
						t.Fatal(ch)
					}
				} else if out.Diagnostics[0].Rule != "MD047" || out.Diagnostics[0].FixAvailable {
					t.Fatal(out.Diagnostics)
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

func TestCompiledPlatformMD065NoCGORefusal(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	conf := strings.Replace(md065Configuration("gfm"), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	putConfigurationFile(t, root, ".mdchk.yaml", conf)
	putConfigurationFile(t, root, "doc.md", "Text\n***\nMore")
	before := readinessState(t, root)
	got := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
	out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
	if !md038NativeRefusal(t, got, out) {
		t.Fatal(got)
	}
	readinessAssertEqual(t, "actual no-cgo early refusal", before, readinessState(t, root))
}
