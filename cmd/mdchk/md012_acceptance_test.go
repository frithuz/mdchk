package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/frithuz/mdchk/internal/result"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func md012Configuration(flavor string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD012: {enabled: false}", "MD012: {enabled: true}", 1)
}
func TestCompiledPlatformMD012ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	data, err := os.ReadFile("../../internal/rules/testdata/md012.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor  string
		Recognize, Incomplete bool
		Maximum               int
		SHA256                string
		SizeBytes             int

		Expected []struct {
			Range   []int
			Source  string
			Message string
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, f := range fixtures {
				if f.Flavor != "" && f.Flavor != flavor {
					continue
				}
				t.Run(f.Name, func(t *testing.T) {
					root := configurationProject(t)
					conf := strings.Replace(md012Configuration(flavor), "MD012: {enabled: true}", fmt.Sprintf("MD012: {maximum: %d}", f.Maximum), 1)
					if f.Recognize {
						conf += "frontmatter: {enabled: true}\n"
					}
					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					putConfigurationFile(t, root, "doc.md", f.Source)
					if f.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source))) || f.SizeBytes != len(f.Source) {
						t.Fatal("literal source provenance", f.Name)
					}
					before := configurationBytes(t, root)
					for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
						got := runConfigurationProcess(t, binary, root, args...)
						out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
						status := 0
						if len(f.Expected) > 0 {
							status = 10
						}
						if f.Incomplete {
							status = 15
						}
						if got.Status != status || out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Diagnostics) != len(f.Expected) || len(out.Changes) != 0 || len(out.Documents) != 0 {
							t.Fatal(got)
						}
						for i, w := range f.Expected {
							d := out.Diagnostics[i]
							actual := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
							if !reflect.DeepEqual(actual, w.Range) || d.Rule != "MD012" || d.FixAvailable || d.Message != w.Message || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source {
								t.Fatal(d, w)
							}
						}
					}
					got, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
					if got.Status != 0 {
						t.Fatal(got)
					}
					if !f.Incomplete {
						found := false
						if p.Rules.Data != nil {
							for _, r := range *p.Rules.Data {
								if r.Rule == "MD012" {
									found = r.Implementation == "available" && r.ActiveEvaluation == "complete"
								}
							}
						}
						if !found {
							t.Fatal(p.Rules)
						}
					}
					if !reflect.DeepEqual(before, configurationBytes(t, root)) {
						t.Fatal("read-only analysis changed bytes")
					}
				})
			}
		})
	}
}

func TestCompiledPlatformMD012DefaultsAndConfiguration(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, setting := range []string{"OMITTED", "{}", "{enabled: true}", "{maximum: 0}", "{maximum: 9223372036854775807}"} {
			t.Run(flavor+"/"+setting, func(t *testing.T) {
				root := configurationProject(t)
				conf := md012Configuration(flavor)
				if setting == "OMITTED" {
					conf = strings.Replace(conf, "  MD012: {enabled: true}\n", "", 1)
				} else {
					conf = strings.Replace(conf, "MD012: {enabled: true}", "MD012: "+setting, 1)
				}
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				putConfigurationFile(t, root, "doc.md", "a\n\n\n")
				validated := runConfigurationProcessEnv(t, binary, root, validationEnvironment(t), "config", "validate", "--format=json")
				if validated.Status != 0 {
					t.Fatal(validated)
				}
				got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
				out := validateConfigurationJSON(t, []byte(got.Stdout))
				want := 1
				if strings.Contains(setting, "9223372036854775807") {
					want = 0
				}
				status := 0
				if want > 0 {
					status = 10
				}
				if got.Status != status || !out.Complete || len(out.Diagnostics) != want || (want > 0 && (out.Diagnostics[0].Rule != "MD012" || out.Diagnostics[0].FixAvailable)) {
					t.Fatal(got)
				}
			})
		}
		for _, option := range []string{"null", "{enabled: false, unknown: true}", "{enabled: null}", "{enabled: false, allow_directives: null}", "{maximum: -2}", "{enabled: false, maximum: null}", "{maximum: '2'}", "{maximum: 2.0}", "{maximum: 2e0}", "{maximum: true}", "{maximum: []}", "{maximum: 9223372036854775808}", "{maximum: 1, maximum: 0}"} {
			t.Run(flavor+"/invalid_"+option, func(t *testing.T) {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", strings.Replace(md012Configuration(flavor), "MD012: {enabled: true}", "MD012: "+option, 1))
				before := configurationBytes(t, root)
				got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
				out := validateConfigurationJSON(t, []byte(got.Stdout))
				if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 || !reflect.DeepEqual(before, configurationBytes(t, root)) {
					t.Fatal(got)
				}
			})
		}
	}
}

func TestCompiledPlatformMD012ActiveControls(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source, setting     string
			active, hidden, directive int
		}{
			{"before", "<!-- mdchk:disable MD012 -->\na\n\n\nb\n", "{}", 0, 1, 0},
			{"late", "a\n\n\n<!-- mdchk:disable MD012 -->\n", "{}", 1, 0, 0},
			{"later-enable", "<!-- mdchk:disable MD012 -->\na\n\n\n<!-- mdchk:enable MD012 -->\nb\n", "{}", 0, 1, 0},
			{"configured-off-enable", "<!-- mdchk:enable MD012 -->\na\n\n\nb\n", "{enabled: false}", 1, 0, 0},
			{"denied-disable", "<!-- mdchk:disable MD012 -->\na\n\n\nb\n", "{allow_directives: false}", 1, 0, 0},
			{"denied-enable", "<!-- mdchk:enable MD012 -->\na\n\n\nb\n", "{enabled: false, allow_directives: false}", 0, 0, 0},
			{"code-span", "`<!-- mdchk:disable MD012 -->`\na\n\n\nb\n", "{}", 1, 0, 0},
			{"code-block", "```\n<!-- mdchk:disable MD012 -->\n```\na\n\n\nb\n", "{}", 1, 0, 0},
			{"raw", "<script>\n<!-- mdchk:disable MD012 -->\n</script>\na\n\n\nb\n", "{}", 1, 0, 0},
			{"malformed", "<!-- mdchk:disable MD012 extra -->\na\n\n\nb\n", "{}", 1, 0, 1},
			{"comment-break", "a\n\n<!-- mdchk:disable MD012\n\n\n-->\n\nb\n", "{}", 0, 0, 0},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(md012Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				conf = strings.Replace(conf, "MD012: {enabled: true}", "MD012: "+tc.setting, 1)
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				putConfigurationFile(t, root, "doc.md", tc.source)
				before := configurationBytes(t, root)
				for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}} {
					got := runConfigurationProcess(t, binary, root, args...)
					out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
					status := 0
					if args[0] != "inspect" && tc.active+tc.directive > 0 {
						status = 10
					}
					hidden := tc.hidden
					if got.Status != status || !out.Complete || len(out.Diagnostics) != tc.active+tc.directive || len(out.SuppressedDiagnostics) != hidden || len(out.Changes) != 0 || len(out.Documents) != 0 {
						t.Fatal(got)
					}
					for _, d := range out.Diagnostics {
						if d.Rule == "MD012" && (d.FixAvailable || d.ByteRange.Start != d.ByteRange.End || d.Message != "Too many consecutive blank body lines.") {
							t.Fatal(d)
						}
					}
				}
				if !reflect.DeepEqual(before, configurationBytes(t, root)) {
					t.Fatal("analysis changed files")
				}
			})
		}
	}
}
func TestCompiledPlatformMD012ActiveMetadata(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, header := range []struct{ name, delimiter string }{{"yaml", "---"}, {"toml", "+++"}} {
			for _, recognize := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%v", flavor, header.name, recognize), func(t *testing.T) {
					root := configurationProject(t)
					conf := strings.Replace(md012Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1) + fmt.Sprintf("frontmatter: {enabled: %v}\n", recognize)
					source := header.delimiter + "\nx: [broken lexical metadata\n<!-- mdchk:disable MD012 -->\n" + header.delimiter + "\na\n\n\nb\n"
					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					putConfigurationFile(t, root, "doc.md", source)
					before := configurationBytes(t, root)
					for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}} {
						got := runConfigurationProcess(t, binary, root, args...)
						out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
						want := 0
						if recognize {
							want = 1
						}
						status := 0
						if args[0] != "inspect" && want > 0 {
							status = 10
						}
						if got.Status != status || !out.Complete || len(out.Diagnostics) != want || len(out.SuppressedDiagnostics) != 1-want || len(out.Changes) != 0 || len(out.Documents) != 0 {
							t.Fatal(got)
						}
					}
					if !reflect.DeepEqual(before, configurationBytes(t, root)) {
						t.Fatal("changed source")
					}
				})
			}
		}
	}
}
func TestCompiledPlatformMD012InheritedPermissionsAndSlicing(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name         string
			global, rule bool
			hidden       int
		}{{"allowed", true, true, 1}, {"global", false, true, 0}, {"rule", true, false, 0}} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := configurationProject(t)
				parent := strings.Replace(md012Configuration(flavor), "directives: {enabled: false}", fmt.Sprintf("directives: {enabled: %v}", tc.global), 1)
				parent = strings.Replace(parent, "MD012: {enabled: true}", fmt.Sprintf("MD012: {enabled: true, allow_directives: %v}", tc.rule), 1)
				child := strings.Replace(md012Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				putConfigurationFile(t, root, ".mdchk.yaml", parent)
				putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
				putConfigurationFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD012 -->\na\n\n\n\nb\n")
				before := configurationBytes(t, root)
				got, p := runInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=4", "--format=json")
				if got.Status != 0 || len(got.Result.Diagnostics) != 1-tc.hidden || len(got.Result.SuppressedDiagnostics) != tc.hidden || p.File.SelectedLines.Start != 4 {
					t.Fatal(got, p)
				}
				// Hiding the original anchor does not invent another in the selected suffix.
				got, _ = runInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=5", "--format=json")
				if got.Status != 0 || len(got.Result.Diagnostics) != 0 || len(got.Result.SuppressedDiagnostics) != 0 {
					t.Fatal(got)
				}
				if !reflect.DeepEqual(before, configurationBytes(t, root)) {
					t.Fatal("changed nested source/config")
				}
			})
		}
	}
}
func TestCompiledPlatformMD012ReportersAndReset(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md012Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "first.md", "<!-- mdchk:disable MD012 -->\na\n\n\nb\n")
			putConfigurationFile(t, root, "second.md", "b\n\n\nc\n")
			before := configurationBytes(t, root)
			for _, format := range []string{"text", "json", "vscode", "github"} {
				got := runConfigurationProcess(t, binary, root, "lint", "first.md", "second.md", "--format="+format)
				if got.Status != 10 || !strings.Contains(got.Stdout, "MD012") {
					t.Fatal(got)
				}
				if format == "json" {
					out := validateConfigurationJSON(t, []byte(got.Stdout))
					if len(out.Diagnostics) != 1 || len(out.SuppressedDiagnostics) != 1 || out.Diagnostics[0].Path != "second.md" {
						t.Fatal(got)
					}
				}
			}
			if !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("changed source/config")
			}
		})
	}
}
func TestCompiledPlatformMD012Composition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"ordinary", "container", "code-tail"} {
			for _, ending := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}, {"CR", "\r"}} {
				t.Run(flavor+"/"+mode+"/"+ending.name, func(t *testing.T) {
					root := configurationProject(t)
					conf := strings.Replace(md012Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
					e := ending.text
					original := "a" + e + e + e + "tail"
					start := 1 + 2*len(e)
					line, column := 3, 1
					if mode == "container" {
						original = "> a" + e + ">" + e + ">" + e + "> tail"
						start = 5 + 2*len(e)
						column = 2
					}
					if mode == "code-tail" {
						original = "    a" + e + e + "    b" + e + e + e + "tail"
						start = 10 + 4*len(e)
						line = 5
					}
					candidate := original + e
					digest := fmt.Sprintf("%x", sha256.Sum256([]byte(candidate)))
					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					for pass, source := range []string{original, candidate} {
						putConfigurationFile(t, root, "doc.md", source)
						before := configurationBytes(t, root)
						got := runConfigurationProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
						out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
						if got.Status != 10 || !out.Complete || len(out.Diagnostics) != 1 || len(out.Changes) != 1-pass || len(out.Documents) != 0 {
							t.Fatal(got)
						}
						snapshot := out.Files[0].ResultSnapshot
						if snapshot == nil || snapshot.SHA256 != digest || snapshot.SizeBytes != len(candidate) {
							t.Fatal(out.Files)
						}
						d := out.Diagnostics[0]
						if d.Rule != "MD012" || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: start, End: start}) || d.Range.Start != (result.Position{Line: line, Column: column}) || d.Range.End != d.Range.Start || d.Snapshot != *snapshot {
							t.Fatal(d, start)
						}
						if pass == 0 {
							c := out.Changes[0]
							if !reflect.DeepEqual(c.Rules, []string{"MD047"}) || c.EditCount != 1 || c.Candidate != *snapshot {
								t.Fatal(c)
							}
						}
						if !reflect.DeepEqual(before, configurationBytes(t, root)) {
							t.Fatal("preview changed source")
						}
					}
				})
			}
		}
	}
}
