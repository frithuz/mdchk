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

func md010Configuration(flavor string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD010: {enabled: false}", "MD010: {enabled: true}", 1)
}
func TestCompiledPlatformMD010ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	data, err := os.ReadFile("../../internal/rules/testdata/md010.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor, Style               string
		Recognize, Incomplete, Strict, CodeBlocks bool
		Spaces                                    int
		SHA256                                    string
		SizeBytes                                 int

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
					conf := strings.Replace(md010Configuration(flavor), "MD010: {enabled: true}", fmt.Sprintf("MD010: {spaces_per_tab: %d, code_blocks: %v}", f.Spaces, f.CodeBlocks), 1)
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
							if !reflect.DeepEqual(actual, w.Range) || d.Rule != "MD010" || d.FixAvailable || d.Message != w.Message || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source {
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
								if r.Rule == "MD010" {
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

func TestCompiledPlatformMD010DefaultsAndConfiguration(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, setting := range []string{"OMITTED", "{}", "{enabled: true}", "{spaces_per_tab: 4, code_blocks: true}", "{spaces_per_tab: 9223372036854775807}"} {
			t.Run(flavor+"/"+setting, func(t *testing.T) {
				root := configurationProject(t)
				conf := md010Configuration(flavor)
				if setting == "OMITTED" {
					conf = strings.Replace(conf, "  MD010: {enabled: true}\n", "", 1)
				} else {
					conf = strings.Replace(conf, "MD010: {enabled: true}", "MD010: "+setting, 1)
				}
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				putConfigurationFile(t, root, "doc.md", "a\t\n")
				validated := runConfigurationProcessEnv(t, binary, root, validationEnvironment(t), "config", "validate", "--format=json")
				if validated.Status != 0 {
					t.Fatal(validated)
				}
				got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
				out := validateConfigurationJSON(t, []byte(got.Stdout))
				if got.Status != 10 || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD010" || out.Diagnostics[0].FixAvailable {
					t.Fatal(got)
				}
			})
		}
		for _, option := range []string{"null", "{enabled: false, unknown: true}", "{enabled: null}", "{enabled: false, allow_directives: null}", "{spaces_per_tab: 0}", "{spaces_per_tab: -2}", "{enabled: false, spaces_per_tab: null}", "{spaces_per_tab: '2'}", "{spaces_per_tab: 2.0}", "{spaces_per_tab: 2e0}", "{spaces_per_tab: true}", "{spaces_per_tab: []}", "{code_blocks: null}", "{code_blocks: 'true'}", "{code_blocks: 1}"} {
			t.Run(flavor+"/invalid_"+option, func(t *testing.T) {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", strings.Replace(md010Configuration(flavor), "MD010: {enabled: true}", "MD010: "+option, 1))
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

func TestCompiledPlatformMD010DirectivesAndValidation(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	source := "<!-- mdchk:disable MD010 -->\na\t\nb <!-- mdchk:enable MD010 -->\t\nz"
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, setting  string
			active, hidden int
		}{{"hidden before", "{enabled: true}", 1, 1}, {"configured off", "{enabled: false}", 1, 1}, {"denied disable", "{enabled: true, allow_directives: false}", 2, 0}, {"denied enable", "{enabled: false, allow_directives: false}", 0, 0}, {"empty mapping", "{}", 1, 1}} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(md010Configuration(flavor), "MD010: {enabled: true}", "MD010: "+tc.setting, 1)
				conf = strings.Replace(conf, "directives: {enabled: false}", "directives: {enabled: true}", 1)
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				putConfigurationFile(t, root, "doc.md", source)
				before := configurationBytes(t, root)
				validated := runConfigurationProcessEnv(t, binary, root, validationEnvironment(t), "config", "validate", "--format=json")
				if validated.Status != 0 {
					t.Fatal(validated)
				}
				for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
					got := runConfigurationProcess(t, binary, root, args...)
					out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
					status := 0
					if tc.active > 0 {
						status = 10
					}
					if got.Status != status || !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden || len(out.Changes) != 0 || len(out.Documents) != 0 {
						t.Fatal(got)
					}
					if tc.active == 1 && out.Diagnostics[0].ByteRange != (result.ByteRange{Start: len(source) - 3, End: len(source) - 2}) {
						t.Fatal(out.Diagnostics)
					}
				}
				if !reflect.DeepEqual(before, configurationBytes(t, root)) {
					t.Fatal("analysis changed bytes")
				}
			})
		}
	}
}

func TestCompiledPlatformMD010InheritedDirectivePermissions(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name         string
			global, rule bool
			hidden       int
		}{{"allowed", true, true, 2}, {"global denial", false, true, 0}, {"rule denial", true, false, 0}} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := configurationProject(t)
				parent := strings.Replace(md010Configuration(flavor), "directives: {enabled: false}", fmt.Sprintf("directives: {enabled: %v}", tc.global), 1)
				parent = strings.Replace(parent, "MD010: {enabled: true}", fmt.Sprintf("MD010: {enabled: true, allow_directives: %v}", tc.rule), 1)
				child := strings.Replace(md010Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				child = strings.Replace(child, "MD010: {enabled: true}", "MD010: {enabled: true, allow_directives: true}", 1)
				putConfigurationFile(t, root, ".mdchk.yaml", parent)
				putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
				putConfigurationFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD010 -->\na\t\nb\t\nz")
				before := configurationBytes(t, root)
				got, p := runInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--format=json")
				if got.Status != 0 || len(got.Result.SuppressedDiagnostics) != tc.hidden || len(got.Result.Diagnostics) != 2-tc.hidden {
					t.Fatal(got, p)
				}
				if !reflect.DeepEqual(before, configurationBytes(t, root)) {
					t.Fatal("analysis changed files")
				}
			})
		}
	}
}

func TestCompiledPlatformMD010InspectionRetainsHiddenParent(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md010Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			source := "<!-- mdchk:disable MD010 -->\na\t\nb <!-- mdchk:enable MD010 -->\t\nz\t\n"
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", source)
			before := configurationBytes(t, root)
			got, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--lines=4", "--format=json")
			if got.Status != 0 || !got.Result.Complete || len(got.Result.Diagnostics) != 1 || len(got.Result.SuppressedDiagnostics) != 0 || p.File.SelectedLines.Start != 4 || p.AST.Data.Root.ByteRange.Start != 0 || p.AST.Data.Root.ByteRange.End != len(source) {
				t.Fatal(got, p)
			}
			d := got.Result.Diagnostics[0]
			if d.Rule != "MD010" || d.Message != "Tab character." || d.ByteRange.Start != len(source)-2 || d.ByteRange.End != len(source)-1 || d.FixAvailable {
				t.Fatal(d)
			}
			if !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("inspection changed source")
			}
		})
	}
}

func TestCompiledPlatformMD010Composition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"ordinary", "code", "structural"} {
			for _, ending := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}, {"CR", "\r"}} {
				t.Run(flavor+"/"+mode+"/"+ending.name, func(t *testing.T) {
					root := configurationProject(t)
					conf := strings.Replace(md010Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
					original := "雪\t" + ending.text + "tail"
					start, end, line, column := 3, 4, 1, 2
					if mode == "code" {
						original = "```" + ending.text + "雪\t" + ending.text + "```" + ending.text + "tail"
						start = 6 + len(ending.text)
						end = start + 1
						line = 2
						conf = strings.Replace(conf, "MD010: {enabled: true}", "MD010: {code_blocks: true}", 1)
					}
					if mode == "structural" {
						original = "-   雪" + ending.text + ending.text + "\tprose"
						start = 7 + 2*len(ending.text)
						end = start + 1
						line = 3
						column = 1
					}
					candidate := original + ending.text
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
						if d.Rule != "MD010" || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: start, End: end}) || d.Range.Start != (result.Position{Line: line, Column: column}) || d.Range.End != (result.Position{Line: line, Column: column + end - start}) || d.Snapshot != *snapshot {
							t.Fatal(d, start, end)
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

func TestCompiledPlatformMD010ActiveMetadataDirectiveControls(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, header := range []struct{ name, source string }{{"yaml", "---\nx: y\n<!-- mdchk:disable MD010 -->\n---\na\t\n"}, {"toml", "+++\nx = 1\n<!-- mdchk:disable MD010 -->\n+++\na\t\n"}} {
			for _, recognize := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/recognize_%v", flavor, header.name, recognize), func(t *testing.T) {
					root := configurationProject(t)
					conf := strings.Replace(md010Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
					conf += fmt.Sprintf("frontmatter: {enabled: %v}\n", recognize)
					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					putConfigurationFile(t, root, "doc.md", header.source)
					before := configurationBytes(t, root)
					got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
					out := validateConfigurationJSON(t, []byte(got.Stdout))
					want := 0
					status := 0
					if recognize {
						want = 1
						status = 10
					}
					if got.Status != status || !out.Complete || len(out.Diagnostics) != want || len(out.SuppressedDiagnostics) != 1-want {
						t.Fatal(got)
					}
					var d result.Diagnostic
					if recognize {
						d = out.Diagnostics[0]
					} else {
						var hidden result.SuppressedDiagnostic
						if err := json.Unmarshal(out.SuppressedDiagnostics[0], &hidden); err != nil {
							t.Fatal(err)
						}
						d = hidden.Diagnostic
					}
					start := len(header.source) - 2
					if d.Rule != "MD010" || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: start, End: start + 1}) || d.Range.Start != (result.Position{Line: 5, Column: 2}) || d.Range.End != (result.Position{Line: 5, Column: 3}) {
						t.Fatal(d)
					}
					if !reflect.DeepEqual(before, configurationBytes(t, root)) {
						t.Fatal("analysis changed source/config")
					}
				})
			}
		}
	}
}

func TestCompiledPlatformMD010ActiveLiteralAndStartControls(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source                                   string
			start, line, column, active, hidden, directive int
		}{
			{"inline-late", "a\t<!-- mdchk:disable MD010 -->\n", 1, 1, 2, 1, 0, 0},
			{"inline-before", "a <!-- mdchk:disable MD010 -->\tb\n", 30, 1, 31, 0, 1, 0},
			{"code-span", "a `<!-- mdchk:disable MD010 -->`\tb\n", 32, 1, 33, 1, 0, 0},
			{"code-block", "```\n<!-- mdchk:disable MD010 -->\n```\na\t\n", 38, 4, 2, 1, 0, 0},
			{"raw-inline", "a <script><!-- mdchk:disable MD010 -->x</script>\tb\n", 48, 1, 49, 1, 0, 0},
			{"raw-block", "<script>\n<!-- mdchk:disable MD010 -->\n</script>\na\t\n", 49, 4, 2, 1, 0, 0},
			{"malformed", "<!-- mdchk:disable MD010 extra -->\na\t\n", 36, 2, 2, 1, 0, 1},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(md010Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				putConfigurationFile(t, root, "doc.md", tc.source)
				before := configurationBytes(t, root)
				for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}} {
					got := runConfigurationProcess(t, binary, root, args...)
					out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
					status := 0
					if args[0] != "inspect" && (tc.active+tc.directive) > 0 {
						status = 10
					}
					if got.Status != status || !out.Complete || len(out.Diagnostics) != tc.active+tc.directive || len(out.SuppressedDiagnostics) != tc.hidden || len(out.Changes) != 0 || len(out.Documents) != 0 {
						t.Fatal(got)
					}
					var actual []result.Diagnostic
					for _, d := range out.Diagnostics {
						if d.Rule == "MD010" {
							actual = append(actual, d)
						}
					}
					for _, raw := range out.SuppressedDiagnostics {
						var hidden result.SuppressedDiagnostic
						if err := json.Unmarshal(raw, &hidden); err != nil {
							t.Fatal(err)
						}
						actual = append(actual, hidden.Diagnostic)
					}
					if len(actual) != 1 {
						t.Fatal(actual)
					}
					d := actual[0]
					if d.Rule != "MD010" || d.Message != "Tab character." || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: tc.start, End: tc.start + 1}) || d.Range.Start != (result.Position{Line: tc.line, Column: tc.column}) || d.Range.End != (result.Position{Line: tc.line, Column: tc.column + 1}) {
						t.Fatal(d)
					}
				}
				if !reflect.DeepEqual(before, configurationBytes(t, root)) {
					t.Fatal("analysis changed source/config")
				}
			})
		}
		t.Run(flavor+"/per-document-reset", func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md010Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "first.md", "<!-- mdchk:disable MD010 -->\na\t\n")
			putConfigurationFile(t, root, "second.md", "b\t\n")
			before := configurationBytes(t, root)
			got := runConfigurationProcess(t, binary, root, "lint", "first.md", "second.md", "--format=json")
			out := validateConfigurationJSON(t, []byte(got.Stdout))
			if got.Status != 10 || !out.Complete || len(out.Diagnostics) != 1 || len(out.SuppressedDiagnostics) != 1 || out.Diagnostics[0].Path != "second.md" || out.Diagnostics[0].Rule != "MD010" || out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 1, End: 2}) {
				t.Fatal(got)
			}
			if !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("analysis changed source/config")
			}
		})
	}
}
