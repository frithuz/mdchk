package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func md037Configuration(flavor string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD037: {enabled: false}", "MD037: {}", 1)
}

func TestCompiledPlatformMD037ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		name, source, flavor string
		want                 int
	}{
		{"edges", "A * x *", "commonmark", 2}, {"unicode-crlf", "> é ** 文 **\r\n", "gfm", 2},
		{"ambiguity", "A * one * and * two *", "commonmark", 0}, {"entity", "A * a&amp;b *", "gfm", 2},
		{"owned-code", "A *a `code` b *", "gfm", 0}, {"link-label", "[A * x *](u)", "commonmark", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md037Configuration(tc.flavor))
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := readinessState(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"fix", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}} {
				got := runConfigurationProcess(t, binary, root, args...)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
				if args[0] == "fix" && args[1] == "doc.md" && got.Status == 13 {
					if len(out.Errors) != 1 || out.Errors[0].Code != "write-acquisition" || !strings.Contains(out.Errors[0].Message, "unsupported") {
						t.Fatal(got)
					}
					continue // Explicit unsupported native adapter, no analysis claimed.
				}
				status := 0
				if tc.want > 0 && args[0] != "inspect" {
					status = 10
				}
				if got.Status != status || !out.Complete || len(out.Diagnostics) != tc.want || len(out.Changes) != 0 || len(out.Documents) != 0 || len(out.Errors) != 0 {
					t.Fatal(got)
				}
				for _, d := range out.Diagnostics {
					if d.Rule != "MD037" || d.FixAvailable || d.Message != "Whitespace within emphasis delimiters." || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.source))) {
						t.Fatal(d)
					}
				}
			}
			readinessAssertEqual(t, "manual analysis and apply", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD037ConfigurationAndReporters(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, setting := range []string{"OMITTED", "{}", "{enabled: false}", "{enabled: false, unknown: true}", "{enabled: false, allow_directives: null}", "null", "{enabled: nope}"} {
		t.Run(setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := md037Configuration("gfm")
			if setting == "OMITTED" {
				conf = strings.Replace(conf, "  MD037: {}\n", "", 1)
			} else {
				conf = strings.Replace(conf, "MD037: {}", "MD037: "+setting, 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "A * x *")
			valid := setting == "OMITTED" || setting == "{}" || setting == "{enabled: false}"
			for _, args := range [][]string{{"config", "validate", "--format=json"}, {"lint", "missing.md", "--format=json"}} {
				var got configurationRun
				if args[0] == "config" {
					got = runConfigurationProcessEnv(t, binary, root, validationEnvironment(t), args...)
				} else {
					got = runConfigurationProcess(t, binary, root, args...)
				}
				op := args[0]
				if op == "config" {
					op = "config validate"
				}
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), op)
				if !valid {
					if got.Status != 12 || (args[0] == "lint" && len(out.CheckedFiles) != 0) {
						t.Fatal(got)
					}
				} else {
					expected := 0
					if args[0] == "lint" {
						expected = 13
					}
					if got.Status != expected {
						t.Fatal(got)
					}
				}
			}
		})
	}
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md037Configuration("commonmark"))
	putConfigurationFile(t, root, "doc.md", "A * x *")
	before := readinessState(t, root)
	for _, format := range []string{"text", "json", "vscode", "github"} {
		t.Run(format, func(t *testing.T) {
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format="+format)
			if got.Status != 10 || !strings.Contains(got.Stdout, "MD037") {
				t.Fatal(got)
			}
		})
	}
	readinessAssertEqual(t, "reporters read-only", before, readinessState(t, root))
}

func TestCompiledPlatformMD037FullSourceInspectionAndReset(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		name, source, setting, line string
		active, hidden              int
	}{
		{"suppressed", "<!-- mdchk:disable MD037 -->\nA * x *", "{}", "2", 0, 2},
		{"enabled", "<!-- mdchk:enable MD037 -->\nA * x *", "{enabled: false}", "2", 2, 0},
		{"denied", "<!-- mdchk:disable MD037 -->\nA * x *", "{allow_directives: false}", "2", 2, 0},
		{"raw-context", "A <script>\n\nA * x *\n\n</script>\n\nA * y *", "{}", "3", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md037Configuration("gfm"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			conf = strings.Replace(conf, "MD037: {}", "MD037: "+tc.setting, 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := readinessState(t, root)
			got, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--lines="+tc.line, "--format=json")
			if got.Status != 0 || len(got.Result.Diagnostics) != tc.active || len(got.Result.SuppressedDiagnostics) != tc.hidden || p.File.SelectedLines.Start == 0 {
				t.Fatal(got, p)
			}
			readinessAssertEqual(t, "full source before inspection", before, readinessState(t, root))
		})
	}
	root := configurationProject(t)
	conf := strings.Replace(md037Configuration("gfm"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
	putConfigurationFile(t, root, ".mdchk.yaml", conf)
	putConfigurationFile(t, root, "first.md", "<!-- mdchk:disable MD037 -->\nA * x *")
	putConfigurationFile(t, root, "second.md", "A * x *")
	got := runConfigurationProcess(t, binary, root, "lint", "first.md", "second.md", "--format=json")
	out := validateConfigurationJSON(t, []byte(got.Stdout))
	if got.Status != 10 || len(out.Diagnostics) != 2 || out.Diagnostics[0].Path != "second.md" || len(out.SuppressedDiagnostics) != 2 {
		t.Fatal(got)
	}
}

func TestCompiledPlatformMD037EncodingAndMD047Composition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	conf := strings.Replace(md037Configuration("gfm"), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	putConfigurationFile(t, root, ".mdchk.yaml", conf)
	source := "A * x *"
	putConfigurationFile(t, root, "doc.md", source)
	dry := runConfigurationProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
	preview := validateConfigurationOperationJSON(t, []byte(dry.Stdout), "fix")
	candidate := source + "\n"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(candidate)))
	if dry.Status != 10 || len(preview.Changes) != 1 || preview.Changes[0].Candidate.SHA256 != digest || len(preview.Diagnostics) != 2 {
		t.Fatal(dry)
	}
	for _, d := range preview.Diagnostics {
		if d.Rule != "MD037" || d.FixAvailable || d.Snapshot.View != "candidate" || d.Snapshot.SHA256 != digest {
			t.Fatal(d)
		}
	}
	before := readinessState(t, root)
	apply := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
	saved := validateConfigurationOperationJSON(t, []byte(apply.Stdout), "fix")
	if apply.Status == 13 {
		if len(saved.Errors) != 1 || saved.Errors[0].Code != "write-acquisition" || !strings.Contains(saved.Errors[0].Message, "unsupported") {
			t.Fatal(apply)
		}
		readinessAssertEqual(t, "documented unsupported no-effects", before, readinessState(t, root))
		//nolint:gosec // Fixed test-owned document in an isolated project.
		data, _ := os.ReadFile(filepath.Join(root, "doc.md"))
		if string(data) != source {
			t.Fatal(string(data))
		}
	} else {
		//nolint:gosec // Fixed test-owned document in an isolated project.
		data, _ := os.ReadFile(filepath.Join(root, "doc.md"))
		if apply.Status != 10 || string(data) != candidate || len(saved.Diagnostics) != 2 {
			t.Fatal(apply, string(data))
		}
		for _, d := range saved.Diagnostics {
			if d.Rule != "MD037" || d.Snapshot.View != "saved" || d.Snapshot.SHA256 != digest {
				t.Fatal(d)
			}
		}
	}
	// Encoding checks continue on both supported and unsupported publication hosts.
	for _, attribution := range []bool{false, true} {
		t.Run(fmt.Sprint(attribution), func(t *testing.T) {
			conf := md037Configuration("gfm")
			if attribution {
				conf = strings.Replace(conf, "MD094: {enabled: false}", "MD094: {enabled: true}", 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "A * � *")
			if err := os.WriteFile(filepath.Join(root, "bad.md"), []byte{'x', 0xff}, 0600); err != nil {
				t.Fatal(err)
			}
			before := readinessState(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "bad.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "bad.md", "--format=json"}} {
				got := runConfigurationProcess(t, binary, root, args...)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
				if got.Status != 13 || out.Complete || len(out.Errors) != 1 || len(out.Diagnostics) != 2 || len(out.Changes) != 0 {
					t.Fatal(got)
				}
				e := out.Errors[0]
				if e.Code != "invalid-utf8" || e.ByteOffset == nil || *e.ByteOffset != 1 || (e.Rule == "MD094") != attribution {
					t.Fatal(e)
				}
			}
			got, p := runInspection(t, binary, root, root, "inspect", "bad.md", "--format=json")
			if got.Status != 0 || got.Result.Complete || p.AST.Availability != "unavailable" {
				t.Fatal(got, p)
			}
			readinessAssertEqual(t, "encoding read-only", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD037HeadersAndInheritedPermissions(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		name, source, extra string
		want, status        int
	}{
		{"yaml", "\ufeff---\nbad: [\n---\nA * x *", "frontmatter: {enabled: true}\n", 2, 10},
		{"toml", "+++\nbad = [\n+++\nA * x *", "frontmatter: {enabled: true}\n", 2, 10},
		{"unselected", "---\nA * x *\n---\nA * y *", "frontmatter: {enabled: true, formats: [toml]}\n", 4, 10},
		{"incomplete", "+++\nA * x *", "frontmatter: {enabled: true}\n", 0, 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md037Configuration("gfm")+tc.extra)
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := readinessState(t, root)
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
			out := validateConfigurationJSON(t, []byte(got.Stdout))
			if got.Status != tc.status || len(out.Diagnostics) != tc.want || out.Complete != (tc.status != 15) || len(out.Errors) != 0 {
				t.Fatal(got)
			}
			readinessAssertEqual(t, "opaque source headers", before, readinessState(t, root))
		})
	}
	for _, policy := range []string{"allowed", "global-denied", "rule-denied"} {
		t.Run(policy, func(t *testing.T) {
			root := configurationProject(t)
			parent := strings.Replace(md037Configuration("commonmark"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			if policy == "global-denied" {
				parent = strings.Replace(parent, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			if policy == "rule-denied" {
				parent = strings.Replace(parent, "MD037: {}", "MD037: {allow_directives: false}", 1)
			}
			child := strings.Replace(md037Configuration("commonmark"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			child = strings.Replace(child, "MD037: {}", "MD037: {allow_directives: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", parent)
			putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
			putConfigurationFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD037 -->\nA * x *")
			got, p := runInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=2", "--format=json")
			active, hidden := 2, 0
			if policy == "allowed" {
				active, hidden = 0, 2
			}
			if got.Status != 0 || len(got.Result.Diagnostics) != active || len(got.Result.SuppressedDiagnostics) != hidden || p.File.SelectedLines.Start != 2 {
				t.Fatal(got, p)
			}
		})
	}
}

func TestCompiledPlatformMD037MixedEncodingIndependentCorrection(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	config := strings.Replace(md037Configuration("gfm"), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	config = strings.Replace(config, "MD094: {enabled: false}", "MD094: {enabled: true}", 1)
	putConfigurationFile(t, root, ".mdchk.yaml", config)
	source, candidate := "A * � *", "A * � *\n"
	putConfigurationFile(t, root, "doc.md", source)
	bad := []byte{'x', 0xff}
	badPath := filepath.Join(root, "zz-bad.md")
	if err := os.WriteFile(badPath, bad, 0600); err != nil {
		t.Fatal(err)
	}
	before := readinessState(t, root)
	for _, dry := range []bool{true, false} {
		t.Run(fmt.Sprintf("dry-%t", dry), func(t *testing.T) {
			args := []string{"fix", "doc.md", "zz-bad.md", "--format=json"}
			if dry {
				args = append(args, "--dry-run")
			}
			got := runConfigurationProcess(t, binary, root, args...)
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
			if got.Status != 13 || out.Complete {
				t.Fatal(got)
			}
			//nolint:gosec // Fixed test-owned malformed document path.
			unchanged, err := os.ReadFile(badPath)
			if err != nil || string(unchanged) != string(bad) {
				t.Fatal(unchanged, err)
			}
			//nolint:gosec // Fixed test-owned valid document path.
			data, err := os.ReadFile(filepath.Join(root, "doc.md"))
			if err != nil {
				t.Fatal(err)
			}
			if !dry && len(out.Errors) == 1 && out.Errors[0].Code == "write-acquisition" {
				if !strings.Contains(out.Errors[0].Message, "unsupported") {
					t.Fatal(got)
				}
				readinessAssertEqual(t, "unsupported mixed apply", before, readinessState(t, root))
				return
			}
			if len(out.Errors) != 1 || out.Errors[0].Code != "invalid-utf8" || out.Errors[0].Path != "zz-bad.md" || out.Errors[0].ByteOffset == nil || *out.Errors[0].ByteOffset != 1 || len(out.Diagnostics) != 2 || len(out.Changes) != 1 || len(out.CheckedFiles) != 2 {
				t.Fatal(got)
			}
			view := "candidate"
			if dry {
				readinessAssertEqual(t, "mixed preview read-only", before, readinessState(t, root))
				if string(data) != source {
					t.Fatal(string(data))
				}
			} else {
				view = "saved"
				if string(data) != candidate || out.Changes[0].Disposition != "written" {
					t.Fatal(got, string(data))
				}
			}
			for i, d := range out.Diagnostics {
				start, end := 3, 4
				if i == 1 {
					start, end = 7, 8
				}
				if d.Rule != "MD037" || d.ByteRange.Start != start || d.ByteRange.End != end || d.Snapshot.View != view || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(candidate))) || d.Snapshot.SizeBytes != 10 || d.FixAvailable {
					t.Fatal(d)
				}
			}
		})
	}
}
