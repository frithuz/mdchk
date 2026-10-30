package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func md088Configuration(flavor string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD088: {enabled: false}", "MD088: {enabled: true, style: ascii}", 1)
}
func TestCompiledPlatformMD088ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		name, source, flavor string
		starts               []int
	}{
		{"six-class", "‘’“”–—", "commonmark", []int{0, 3, 6, 9, 12, 15}},
		{"quote-crlf", "> - 😀\t“x”—\r\n", "gfm", []int{9, 13, 16}},
		{"heading", "# “x”—", "commonmark", []int{2, 6, 9}},
		{"image", "![<script>](x) —", "gfm", []int{15}},
		{"protected", "`“a”—`", "gfm", nil},
		{"link", "[“x”](u) —", "commonmark", []int{13}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md088Configuration(tc.flavor))
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := readinessState(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"fix", "doc.md", "--format=json"}} {
				got := runConfigurationProcess(t, binary, root, args...)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
				if args[0] == "fix" && args[1] == "doc.md" && md038NativeRefusal(t, got, out) {
					continue
				}
				status := 0
				if len(tc.starts) > 0 && args[0] != "inspect" {
					status = 10
				}
				if got.Status != status || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(tc.starts) || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(got, out)
				}
				for i, d := range out.Diagnostics {
					view := "original"
					if args[0] == "fix" {
						view = "saved"
						if args[1] == "--dry-run" {
							view = "candidate"
						}
					}
					if d.Rule != "MD088" || d.Message != "Use ASCII quotes and dashes in prose." || d.FixAvailable || d.ByteRange.Start != tc.starts[i] || d.ByteRange.End != tc.starts[i]+3 || d.Range.End.Line != d.Range.Start.Line || d.Range.End.Column != d.Range.Start.Column+1 || d.Snapshot.View != view || d.Snapshot.SizeBytes != len(tc.source) || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.source))) {
						t.Fatal(d)
					}
				}
			}
			readinessAssertEqual(t, "manual MD088", before, readinessState(t, root))
		})
	}
}
func TestCompiledPlatformMD088TypedOptionsAndDefaults(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		setting          string
		status, findings int
	}{
		{"OMITTED", 0, 0}, {"{}", 0, 0}, {"{enabled: false}", 0, 0}, {"{style: ascii}", 0, 0}, {"{enabled: true}", 10, 6}, {"{enabled: true, style: ascii}", 10, 6},
	} {
		t.Run(tc.setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := md088Configuration("gfm")
			if tc.setting == "OMITTED" {
				conf = strings.Replace(conf, "  MD088: {enabled: true, style: ascii}\n", "", 1)
			} else {
				conf = strings.Replace(conf, "MD088: {enabled: true, style: ascii}", "MD088: "+tc.setting, 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "‘’“”–—")
			before := readinessState(t, root)
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
			if got.Status != tc.status || !out.Complete || len(out.Diagnostics) != tc.findings {
				t.Fatal(got, out)
			}
			readinessAssertEqual(t, "defaults", before, readinessState(t, root))
		})
	}
	for _, setting := range []string{"null", "{enabled: null}", "{allow_directives: null}", "{style: null}", "{enabled: false, style: ASCII}", "{enabled: false, style: ''}", "{enabled: false, style: 1}", "{enabled: false, replacements: {}}"} {
		t.Run(setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md088Configuration("gfm"), "MD088: {enabled: true, style: ascii}", "MD088: "+setting, 1)
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
func TestCompiledPlatformMD088DirectiveInheritanceAndSelection(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, denial := range []string{"allowed", "global", "rule"} {
		t.Run(denial, func(t *testing.T) {
			root := configurationProject(t)
			parent := strings.Replace(md088Configuration("gfm"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			if denial == "global" {
				parent = strings.Replace(parent, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			if denial == "rule" {
				parent = strings.Replace(parent, "style: ascii}", "style: ascii, allow_directives: false}", 1)
			}
			child := strings.Replace(md088Configuration("gfm"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			child = strings.Replace(child, "style: ascii}", "style: ascii, allow_directives: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", parent)
			putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
			putConfigurationFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD088 -->\n<!-- mdchk:disable MD088 -->\n—\n\nordinary")
			before := readinessState(t, root)
			got, p := runInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=3", "--format=json")
			active, hidden := 1, 0
			if denial == "allowed" {
				active, hidden = 0, 1
			}
			if got.Status != 0 || len(got.Result.Diagnostics) != active || len(got.Result.SuppressedDiagnostics) != hidden || p.File.SelectedLines.Start != 3 {
				t.Fatal(got, p)
			}
			got, _ = runInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=5", "--format=json")
			if got.Status != 0 || len(got.Result.Diagnostics)+len(got.Result.SuppressedDiagnostics) != 0 {
				t.Fatal(got)
			}
			readinessAssertEqual(t, "inspection", before, readinessState(t, root))
		})
	}
	t.Run("raw-context", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", md088Configuration("gfm"))
		putConfigurationFile(t, root, "doc.md", "a <script>\n\n“\n\n</script>\n\n—")
		before := readinessState(t, root)
		got, _ := runInspection(t, binary, root, root, "inspect", "doc.md", "--lines=3", "--format=json")
		if got.Status != 0 || len(got.Result.Diagnostics) != 0 {
			t.Fatal(got)
		}
		got, _ = runInspection(t, binary, root, root, "inspect", "doc.md", "--lines=7", "--format=json")
		if got.Status != 0 || len(got.Result.Diagnostics) != 1 || got.Result.Diagnostics[0].ByteRange.Start != 28 {
			t.Fatal(got)
		}
		readinessAssertEqual(t, "full raw context", before, readinessState(t, root))
	})
}
func TestCompiledPlatformMD088FourReporters(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md088Configuration("gfm"))
	putConfigurationFile(t, root, "doc.md", "—")
	before := readinessState(t, root)
	for _, format := range []string{"text", "json", "vscode", "github"} {
		t.Run(format, func(t *testing.T) {
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format="+format)
			if got.Status != 10 || !strings.Contains(got.Stdout, "MD088") || !strings.Contains(got.Stdout, "Use ASCII quotes and dashes in prose.") {
				t.Fatal(got)
			}
			switch format {
			case "text":
				if !strings.Contains(got.Stdout, "doc.md:1:1 MD088:") {
					t.Fatal(got)
				}
			case "vscode":
				if !strings.Contains(got.Stdout, "doc.md:1:1: error: MD088:") {
					t.Fatal(got)
				}
			case "github":
				if strings.Count(got.Stdout, "::error file=doc.md,line=1,title=MD088::") != 1 {
					t.Fatal(got)
				}
			case "json":
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
				if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange.Start != 0 || out.Diagnostics[0].ByteRange.End != 3 {
					t.Fatal(out)
				}
			}
		})
	}
	readinessAssertEqual(t, "reporters", before, readinessState(t, root))
}
func TestCompiledPlatformMD088MD047Composition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, source := range []string{"“a”—", "`“a”—`"} {
		t.Run(source, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md088Configuration("gfm"), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", source)
			before := readinessState(t, root)
			protected := source == "`“a”—`"
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
				changes, deferred, diagnostics := 1, 0, 3
				if protected {
					changes, deferred, diagnostics = 0, 1, 1
				} else {
					want += "\n"
				}
				if dry {
					view, disposition = "candidate", "proposed"
				}
				if got.Status != 10 || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != diagnostics || len(out.Changes) != changes || len(out.DeferredFixes) != deferred || len(out.Documents) != 0 {
					t.Fatal(got, out)
				}
				if !protected {
					for i, d := range out.Diagnostics {
						start := []int{0, 4, 7}[i]
						if d.Rule != "MD088" || d.FixAvailable || d.ByteRange.Start != start || d.ByteRange.End != start+3 || d.Snapshot.View != view || d.Snapshot.SizeBytes != 11 || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(want))) {
							t.Fatal(d)
						}
					}
					ch := out.Changes[0]
					if len(ch.Rules) != 1 || ch.Rules[0] != "MD047" || ch.Disposition != disposition {
						t.Fatal(ch)
					}
				} else if out.Diagnostics[0].Rule != "MD047" || out.Diagnostics[0].FixAvailable || out.DeferredFixes[0].Rule != "MD047" {
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
func TestCompiledPlatformMD088NoCGORefusal(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	conf := strings.Replace(md088Configuration("gfm"), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	putConfigurationFile(t, root, ".mdchk.yaml", conf)
	putConfigurationFile(t, root, "doc.md", "“a”—")
	before := readinessState(t, root)
	got := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
	out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
	if !md038NativeRefusal(t, got, out) {
		t.Fatal(got)
	}
	if !out.Complete || len(out.Errors) != 1 || len(out.Diagnostics)+len(out.CheckedFiles)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
		t.Fatal(out)
	}
	readinessAssertEqual(t, "actual cgo0 early refusal", before, readinessState(t, root))
}
