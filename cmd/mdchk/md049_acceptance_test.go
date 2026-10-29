package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func md049Configuration(flavor string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD049: {enabled: false}", "MD049: {enabled: true, style: consistent}", 1)
}

func TestCompiledPlatformMD049ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		id, source, flavor string
		ranges             [][2]int
	}{
		{"basic", "*one* and _two_", "commonmark", [][2]int{{10, 11}}},
		{"scalar", "é *a* _b_", "gfm", [][2]int{{7, 8}}},
		{"nested", "*outer _inner_*", "commonmark", [][2]int{{7, 8}}},
		{"strong-does-not-seed", "**bold** _a_ *b*", "gfm", [][2]int{{13, 14}}},
		{"code-protected", "`_a_` *b*", "gfm", nil},
	} {
		t.Run(tc.id, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md049Configuration(tc.flavor))
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

					if d.Rule != "MD049" || d.Message != "Italic delimiter style does not match the expected style." || d.ByteRange.Start != w[0] || d.ByteRange.End != w[1] || d.FixAvailable || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.source))) || d.Snapshot.SizeBytes != len(tc.source) {
						t.Fatal(d)
					}
				}
			}
			readinessAssertEqual(t, "manual MD049", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD049OptionsAndInheritance(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, setting := range []string{"{enabled: false, style: null}", "{enabled: false, style: [asterisk]}", "{enabled: false, style: Asterisk}", "{enabled: false, style: dash}", "{enabled: false, unknown: true}", "{enabled: false, allow_directives: null}"} {
		t.Run(setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md049Configuration("gfm"), "MD049: {enabled: true, style: consistent}", "MD049: "+setting, 1)
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
			parent := strings.Replace(md049Configuration("commonmark"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			if denial == "global" {
				parent = strings.Replace(parent, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			if denial == "rule" {
				parent = strings.Replace(parent, "style: consistent", "style: consistent, allow_directives: false", 1)
			}
			child := strings.Replace(md049Configuration("commonmark"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			child = strings.Replace(child, "style: consistent", "style: consistent, allow_directives: true", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", parent)
			putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
			putConfigurationFile(t, root, "nested/doc.md", "*a*\n<!-- mdchk:disable MD049 -->\n_b_")
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

func TestCompiledPlatformMD049NoCGORefusal(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md049Configuration("gfm"))
	putConfigurationFile(t, root, "doc.md", "*a* _b_")
	before := readinessState(t, root)
	got := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
	out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
	if !md038NativeRefusal(t, got, out) {
		t.Fatal(got)
	}
	readinessAssertEqual(t, "actual no-cgo early refusal", before, readinessState(t, root))
}

func TestCompiledPlatformMD049MD047Composition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, source := range []string{"`_a_`", "`_a_` tail"} {
		t.Run(source, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md049Configuration("gfm"), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", source)
			before := readinessState(t, root)
			dry := runConfigurationProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
			preview := validateConfigurationOperationJSON(t, []byte(dry.Stdout), "fix")
			protected := source == "`_a_`"
			changes, deferred := 1, 0
			if protected {
				changes, deferred = 0, 1
			}
			if dry.Status != 10 && protected || dry.Status != 0 && !protected || !preview.Complete || len(preview.Changes) != changes || len(preview.DeferredFixes) != deferred {
				t.Fatal(dry)
			}
			readyness := readinessState(t, root)
			readinessAssertEqual(t, "preview", before, readyness)
			got := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
			if md038NativeRefusal(t, got, out) {
				readinessAssertEqual(t, "unsupported", before, readinessState(t, root))
				return
			}
			//nolint:gosec // Fixed isolated test-owned document.
			data, err := os.ReadFile(filepath.Join(root, "doc.md"))
			if err != nil {
				t.Fatal(err)
			}
			want := source
			status := 10
			if !protected {
				want += "\n"
				status = 0
			}
			if got.Status != status || string(data) != want || len(out.Changes) != changes || len(out.Diagnostics) != deferred {
				t.Fatal(got, string(data))
			}
			if !protected && (len(out.Changes[0].Rules) != 1 || out.Changes[0].Rules[0] != "MD047" || out.Changes[0].Disposition != "written") {
				t.Fatal(out.Changes)
			}
		})
	}
}

func TestCompiledPlatformMD049DefaultsStylesAndOriginalContext(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		name, setting, source string
		want, start           int
	}{
		{"default", "OMITTED", "*one* and _two_", 1, 10},
		{"empty-options", "{}", "_one_ and *two*", 1, 10},
		{"asterisk", "{style: asterisk}", "_one_ and *two*", 1, 0},
		{"underscore", "{style: underscore}", "*one* and _two_", 1, 0},
		{"disabled", "{enabled: false}", "*one* and _two_", 0, 0},
		{"suppressed-baseline", "{}", "<!-- mdchk:disable MD049 -->\n_a_\n<!-- mdchk:enable MD049 -->\n*b*\n", 1, 61},
		{"initial-off", "{enabled: false}", "<!-- mdchk:disable MD049 -->\n_a_\n<!-- mdchk:enable MD049 -->\n*b*\n", 1, 61},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			conf := md049Configuration("gfm")
			conf = strings.Replace(conf, "directives: {enabled: false}", "directives: {enabled: true}", 1)
			if tc.setting == "OMITTED" {
				conf = strings.Replace(conf, "  MD049: {enabled: true, style: consistent}\n", "", 1)
			} else {
				conf = strings.Replace(conf, "MD049: {enabled: true, style: consistent}", "MD049: "+tc.setting, 1)
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
			if tc.want > 0 && (out.Diagnostics[0].ByteRange.Start != tc.start || out.Diagnostics[0].ByteRange.End != tc.start+1) {
				t.Fatal(out.Diagnostics)
			}
			if tc.name == "suppressed-baseline" {
				got, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--lines=4", "--format=json")
				if got.Status != 0 || p.File.SelectedLines.Start != 4 || len(got.Result.Diagnostics) != 1 || got.Result.Diagnostics[0].ByteRange.Start != 61 {
					t.Fatal(got, p)
				}
			}
			readinessAssertEqual(t, "style/default/directive read-only", before, readinessState(t, root))
		})
	}
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md049Configuration("commonmark"))
	putConfigurationFile(t, root, "doc.md", "*a* _b_")
	for _, format := range []string{"text", "json", "vscode", "github"} {
		got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format="+format)
		if got.Status != 10 || !strings.Contains(got.Stdout, "MD049") {
			t.Fatal(format, got)
		}
	}
	putConfigurationFile(t, root, "doc.md", "_a_ *b*")
	got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
	out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
	if got.Status != 10 || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange.Start != 4 {
		t.Fatal(got)
	}
}
