package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func md033Configuration(flavor string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD033: {enabled: false}", "MD033: {enabled: true, allowed_tags: []}", 1)
}
func TestCompiledPlatformMD033ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		name, source, flavor string
		starts               []int
	}{
		{"pair", "a <b>x</b> z", "commonmark", []int{2, 6}},
		{"quote-crlf", "> 😀\t<x>\r\n> </x>", "gfm", []int{7, 14}},
		{"heading", "# <b>", "commonmark", []int{2}},
		{"image", "![<script>](x) <i>", "gfm", []int{15}},
		{"protected", "`<b>`", "gfm", nil},
		{"link", "[<b>](u)", "commonmark", []int{1}},
		{"raw", "<pre><b></script><i>", "gfm", []int{0, 8, 17}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md033Configuration(tc.flavor))
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
					if d.Rule != "MD033" || d.Message != "Embedded HTML tag is not allowed." || d.FixAvailable || d.ByteRange.Start != tc.starts[i] || d.ByteRange.End != tc.starts[i]+len(md033NativeTags(tc.name)[i]) || d.Snapshot.View != view || d.Snapshot.SizeBytes != len(tc.source) || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.source))) {
						t.Fatal(d)
					}
				}
			}
			readinessAssertEqual(t, "manual MD033", before, readinessState(t, root))
		})
	}
}
func TestCompiledPlatformMD033TypedOptionsAndDefaults(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		setting          string
		status, findings int
	}{
		{"OMITTED", 0, 0}, {"{}", 0, 0}, {"{enabled: false}", 0, 0}, {"{allowed_tags: []}", 0, 0}, {"{enabled: true}", 10, 1}, {"{enabled: true, allowed_tags: []}", 10, 1},
		{"{enabled: true, allowed_tags: [B, b]}", 0, 0},
		{"{enabled: true, allowed_tags: ['', ' b ', '<b>', '/b', 'ſ']}", 10, 1},
	} {
		t.Run(tc.setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := md033Configuration("gfm")
			if tc.setting == "OMITTED" {
				conf = strings.Replace(conf, "  MD033: {enabled: true, allowed_tags: []}\n", "", 1)
			} else {
				conf = strings.Replace(conf, "MD033: {enabled: true, allowed_tags: []}", "MD033: "+tc.setting, 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "<b>")
			before := readinessState(t, root)
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
			if got.Status != tc.status || !out.Complete || len(out.Diagnostics) != tc.findings {
				t.Fatal(got, out)
			}
			readinessAssertEqual(t, "defaults", before, readinessState(t, root))
		})
	}
	for _, setting := range []string{"null", "{enabled: null}", "{allow_directives: null}", "{allowed_tags: null}", "{enabled: false, allowed_tags: b}", "{enabled: false, allowed_tags: [b, 1]}", "{enabled: false, allowed_tags: 1}", "{enabled: false, replacements: {}}"} {
		t.Run(setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md033Configuration("gfm"), "MD033: {enabled: true, allowed_tags: []}", "MD033: "+setting, 1)
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
func TestCompiledPlatformMD033DirectiveInheritanceAndSelection(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, denial := range []string{"allowed", "global", "rule"} {
		t.Run(denial, func(t *testing.T) {
			root := configurationProject(t)
			parent := strings.Replace(md033Configuration("gfm"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			if denial == "global" {
				parent = strings.Replace(parent, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			if denial == "rule" {
				parent = strings.Replace(parent, "allowed_tags: []}", "allowed_tags: [], allow_directives: false}", 1)
			}
			child := strings.Replace(md033Configuration("gfm"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			child = strings.Replace(child, "allowed_tags: []}", "allowed_tags: [], allow_directives: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", parent)
			putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
			putConfigurationFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD033 -->\n<!-- mdchk:disable MD033 -->\n<b>\n\nordinary")
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
		putConfigurationFile(t, root, ".mdchk.yaml", md033Configuration("gfm"))
		putConfigurationFile(t, root, "doc.md", "a <script>\n\n<b>\n\n</script>\n\n<i>")
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
func TestCompiledPlatformMD033FourReporters(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md033Configuration("gfm"))
	putConfigurationFile(t, root, "doc.md", "<b>")
	before := readinessState(t, root)
	for _, format := range []string{"text", "json", "vscode", "github"} {
		t.Run(format, func(t *testing.T) {
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format="+format)
			if got.Status != 10 || !strings.Contains(got.Stdout, "MD033") || !strings.Contains(got.Stdout, "Embedded HTML tag is not allowed.") {
				t.Fatal(got)
			}
			switch format {
			case "text":
				if !strings.Contains(got.Stdout, "doc.md:1:1 MD033:") {
					t.Fatal(got)
				}
			case "vscode":
				if !strings.Contains(got.Stdout, "doc.md:1:1: error: MD033:") {
					t.Fatal(got)
				}
			case "github":
				if strings.Count(got.Stdout, "::error file=doc.md,line=1,title=MD033::") != 1 {
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
func TestCompiledPlatformMD033MD047Composition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, source := range []string{"a <b>x</b> z", "`a <b>x</b> z`"} {
		t.Run(source, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md033Configuration("gfm"), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", source)
			before := readinessState(t, root)
			protected := source == "`a <b>x</b> z`"
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
				changes, deferred, diagnostics := 1, 0, 2
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
						start := []int{2, 6}[i]
						if d.Rule != "MD033" || d.FixAvailable || d.ByteRange.Start != start || d.ByteRange.End != []int{5, 10}[i] || d.Snapshot.View != view || d.Snapshot.SizeBytes != 13 || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(want))) {
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
func TestCompiledPlatformMD033NoCGORefusal(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	conf := strings.Replace(md033Configuration("gfm"), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	putConfigurationFile(t, root, ".mdchk.yaml", conf)
	putConfigurationFile(t, root, "doc.md", "a <b>x</b> z")
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

func md033NativeTags(name string) []string {
	switch name {
	case "pair":
		return []string{"<b>", "</b>"}
	case "quote-crlf":
		return []string{"<x>", "</x>"}
	case "raw":
		return []string{"<pre>", "</script>", "<i>"}
	case "image":
		return []string{"<i>"}
	default:
		return []string{"<b>"}
	}
}
