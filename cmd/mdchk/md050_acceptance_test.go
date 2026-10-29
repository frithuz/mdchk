package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func md050Configuration(flavor string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD050: {enabled: false}", "MD050: {enabled: true, style: consistent}", 1)
}

func TestCompiledPlatformMD050ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		id, source, flavor string
		ranges             [][2]int
	}{
		{"basic", "**one** and __two__", "commonmark", [][2]int{{12, 14}}},
		{"scalar", "é **a** __b__", "gfm", [][2]int{{9, 11}}},
		{"nested", "**outer __inner__**", "commonmark", [][2]int{{8, 10}}},
		{"italic-does-not-seed", "*italic* __a__ **b**", "gfm", [][2]int{{15, 17}}},
		{"code-protected", "`__a__` **b**", "gfm", nil},
	} {
		t.Run(tc.id, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md050Configuration(tc.flavor))
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
					view := "original"
					if args[0] == "fix" {
						view = "saved"
						if args[1] == "--dry-run" {
							view = "candidate"
						}
					}
					if d.Snapshot.View != view {
						t.Fatal(d.Snapshot)
					}

					if d.Rule != "MD050" || d.Message != "Bold delimiter style does not match the expected style." || d.ByteRange.Start != w[0] || d.ByteRange.End != w[1] || d.FixAvailable || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.source))) || d.Snapshot.SizeBytes != len(tc.source) {
						t.Fatal(d)
					}
				}
			}
			readinessAssertEqual(t, "manual MD050", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD050OptionsAndInheritance(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, setting := range []string{"{enabled: false, style: null}", "{enabled: false, style: [asterisk]}", "{enabled: false, style: Asterisk}", "{enabled: false, style: dash}", "{enabled: false, unknown: true}", "{enabled: false, allow_directives: null}"} {
		t.Run(setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md050Configuration("gfm"), "MD050: {enabled: true, style: consistent}", "MD050: "+setting, 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
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
			parent := strings.Replace(md050Configuration("commonmark"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			if denial == "global" {
				parent = strings.Replace(parent, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			if denial == "rule" {
				parent = strings.Replace(parent, "style: consistent", "style: consistent, allow_directives: false", 1)
			}
			child := strings.Replace(md050Configuration("commonmark"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			child = strings.Replace(child, "style: consistent", "style: consistent, allow_directives: true", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", parent)
			putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
			putConfigurationFile(t, root, "nested/doc.md", "**a**\n<!-- mdchk:disable MD050 -->\n__b__")
			got, p := runInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=3", "--format=json")
			active, hidden := 1, 0
			if denial == "allowed" {
				active, hidden = 0, 1
			}
			if got.Status != 0 || len(got.Result.Diagnostics) != active || len(got.Result.SuppressedDiagnostics) != hidden || p.File.SelectedLines.Start != 3 {
				t.Fatal(got, p)
			}
		})
	}
}

func TestCompiledPlatformMD050NoCGORefusal(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md050Configuration("gfm"))
	putConfigurationFile(t, root, "doc.md", "**a** __b__")
	before := readinessState(t, root)
	got := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
	out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
	if !md038NativeRefusal(t, got, out) {
		t.Fatal(got)
	}
	readinessAssertEqual(t, "actual no-cgo early refusal", before, readinessState(t, root))
}

func TestCompiledPlatformMD050DefaultsStylesAndOriginalContext(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		name, setting, source string
		want, start           int
	}{
		{"default", "OMITTED", "**one** and __two__", 1, 12},
		{"empty-options", "{}", "__one__ and **two**", 1, 12},
		{"asterisk", "{style: asterisk}", "__one__ and **two**", 1, 0},
		{"underscore", "{style: underscore}", "**one** and __two__", 1, 0},
		{"disabled", "{enabled: false}", "**one** and __two__", 0, 0},
		{"suppressed-baseline", "{}", "<!-- mdchk:disable MD050 -->\n__a__\n<!-- mdchk:enable MD050 -->\n**b**\n", 1, 63},
		{"initial-off", "{enabled: false}", "<!-- mdchk:disable MD050 -->\n__a__\n<!-- mdchk:enable MD050 -->\n**b**\n", 1, 63},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			conf := md050Configuration("gfm")
			conf = strings.Replace(conf, "directives: {enabled: false}", "directives: {enabled: true}", 1)
			if tc.setting == "OMITTED" {
				conf = strings.Replace(conf, "  MD050: {enabled: true, style: consistent}\n", "", 1)
			} else {
				conf = strings.Replace(conf, "MD050: {enabled: true, style: consistent}", "MD050: "+tc.setting, 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := readinessState(t, root)
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
			status := 0
			if tc.want > 0 {
				status = 10
			}
			if got.Status != status || !out.Complete || len(out.Diagnostics) != tc.want {
				t.Fatal(got)
			}
			if tc.want > 0 && (out.Diagnostics[0].ByteRange.Start != tc.start || out.Diagnostics[0].ByteRange.End != tc.start+2) {
				t.Fatal(out.Diagnostics)
			}
			if tc.name == "suppressed-baseline" {
				got, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--lines=4", "--format=json")
				if got.Status != 0 || p.File.SelectedLines.Start != 4 || len(got.Result.Diagnostics) != 1 || got.Result.Diagnostics[0].ByteRange.Start != 63 {
					t.Fatal(got, p)
				}
			}
			readinessAssertEqual(t, "style/default/directive read-only", before, readinessState(t, root))
		})
	}
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md050Configuration("commonmark"))
	putConfigurationFile(t, root, "doc.md", "**a** __b__")
	for _, format := range []string{"text", "json", "vscode", "github"} {
		got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format="+format)
		if got.Status != 10 || !strings.Contains(got.Stdout, "MD050") || !strings.Contains(got.Stdout, "Bold delimiter style does not match the expected style.") {
			t.Fatal(format, got)
		}
		switch format {
		case "text":
			if !strings.Contains(got.Stdout, "doc.md:1:7 MD050:") {
				t.Fatal(got)
			}
		case "vscode":
			if !strings.Contains(got.Stdout, "doc.md:1:7: error: MD050:") {
				t.Fatal(got)
			}
		case "json":
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
			if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange.Start != 6 || out.Diagnostics[0].ByteRange.End != 8 || out.Diagnostics[0].Range.Start.Column != 7 || out.Diagnostics[0].Range.End.Column != 9 || out.Diagnostics[0].FixAvailable {
				t.Fatal(out)
			}
		case "github":
			if !strings.Contains(got.Stdout, "::error file=doc.md,line=1,title=MD050::") {
				t.Fatal(got)
			}
		}
	}
	putConfigurationFile(t, root, "doc.md", "__a__ **b**")
	got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
	out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
	if got.Status != 10 || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange.Start != 6 {
		t.Fatal(got)
	}
}

func TestCompiledPlatformMD050MD047Composition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, source := range []string{"**a** __b__", "`__a__`"} {
		t.Run(source, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md050Configuration("gfm"), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", source)
			before := readinessState(t, root)
			protected := source == "`__a__`"
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
				changes, deferred, diagnostics := 1, 0, 1
				if !protected {
					want += "\n"
				} else {
					changes, deferred = 0, 1
				}
				if dry {
					view, disposition = "candidate", "proposed"
				}
				if got.Status != 10 || !out.Complete || len(out.Errors) != 0 || len(out.Changes) != changes || len(out.DeferredFixes) != deferred || len(out.Diagnostics) != diagnostics || len(out.Documents) != 0 {
					t.Fatal(got, out)
				}
				if !protected {
					d := out.Diagnostics[0]
					if d.Rule != "MD050" || d.ByteRange.Start != 6 || d.ByteRange.End != 8 || d.FixAvailable || d.Snapshot.View != view || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(want))) || d.Snapshot.SizeBytes != len(want) {
						t.Fatal(d)
					}
					ch := out.Changes[0]
					if len(ch.Rules) != 1 || ch.Rules[0] != "MD047" || ch.Disposition != disposition {
						t.Fatal(ch)
					}
				} else if out.Diagnostics[0].Rule != "MD047" {
					t.Fatal(out.Diagnostics)
				}
				//nolint:gosec // Isolated fixed test-owned document.
				actual, err := os.ReadFile(filepath.Join(root, "doc.md"))
				saved := source
				if !dry {
					saved = want
				}
				if err != nil || string(actual) != saved {
					t.Fatal(string(actual), err)
				}
				if dry || protected {
					readinessAssertEqual(t, "unchanged", before, readinessState(t, root))
				}
			}
			//nolint:gosec // Isolated test-owned configuration.
			actualConf, err := os.ReadFile(filepath.Join(root, ".mdchk.yaml"))
			if err != nil || string(actualConf) != conf {
				t.Fatal(string(actualConf), err)
			}
		})
	}
}
