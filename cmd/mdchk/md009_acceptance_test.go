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

func md009Configuration(flavor string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD009: {enabled: false}", "MD009: {enabled: true}", 1)
}
func TestCompiledPlatformMD009ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	data, err := os.ReadFile("../../internal/rules/testdata/md009.json")
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
					conf := strings.Replace(md009Configuration(flavor), "MD009: {enabled: true}", fmt.Sprintf("MD009: {hard_break_spaces: %d, strict: %v, code_blocks: %v}", f.Spaces, f.Strict, f.CodeBlocks), 1)
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
							if !reflect.DeepEqual(actual, w.Range) || d.Rule != "MD009" || d.FixAvailable || d.Message != w.Message || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source {
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
								if r.Rule == "MD009" {
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

func TestCompiledPlatformMD009DefaultsAndConfiguration(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, setting := range []string{"OMITTED", "{}", "{enabled: true}", "{hard_break_spaces: 4, strict: true, code_blocks: true}", "{hard_break_spaces: 9223372036854775807}"} {
			t.Run(flavor+"/"+setting, func(t *testing.T) {
				root := configurationProject(t)
				conf := md009Configuration(flavor)
				if setting == "OMITTED" {
					conf = strings.Replace(conf, "  MD009: {enabled: true}\n", "", 1)
				} else {
					conf = strings.Replace(conf, "MD009: {enabled: true}", "MD009: "+setting, 1)
				}
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				putConfigurationFile(t, root, "doc.md", "a \n")
				validated := runConfigurationProcessEnv(t, binary, root, validationEnvironment(t), "config", "validate", "--format=json")
				if validated.Status != 0 {
					t.Fatal(validated)
				}
				got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
				out := validateConfigurationJSON(t, []byte(got.Stdout))
				if got.Status != 10 || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD009" || out.Diagnostics[0].FixAvailable {
					t.Fatal(got)
				}
			})
		}
		for _, option := range []string{"null", "{enabled: false, unknown: true}", "{enabled: null}", "{enabled: false, allow_directives: null}", "{hard_break_spaces: 0}", "{hard_break_spaces: 1}", "{hard_break_spaces: -2}", "{enabled: false, hard_break_spaces: null}", "{hard_break_spaces: '2'}", "{hard_break_spaces: 2.0}", "{hard_break_spaces: 2e0}", "{hard_break_spaces: true}", "{hard_break_spaces: []}", "{strict: null}", "{strict: 'true'}", "{strict: 1}", "{code_blocks: null}", "{code_blocks: 'true'}", "{code_blocks: 1}"} {
			t.Run(flavor+"/invalid_"+option, func(t *testing.T) {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", strings.Replace(md009Configuration(flavor), "MD009: {enabled: true}", "MD009: "+option, 1))
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

func TestCompiledPlatformMD009DirectivesAndValidation(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	source := "<!-- mdchk:disable MD009 -->\na \nb <!-- mdchk:enable MD009 --> \nz"
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, setting  string
			active, hidden int
		}{{"hidden before", "{enabled: true}", 1, 1}, {"configured off", "{enabled: false}", 1, 1}, {"denied disable", "{enabled: true, allow_directives: false}", 2, 0}, {"denied enable", "{enabled: false, allow_directives: false}", 0, 0}, {"empty mapping", "{}", 1, 1}} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(md009Configuration(flavor), "MD009: {enabled: true}", "MD009: "+tc.setting, 1)
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

func TestCompiledPlatformMD009InheritedDirectivePermissions(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name         string
			global, rule bool
			hidden       int
		}{{"allowed", true, true, 2}, {"global denial", false, true, 0}, {"rule denial", true, false, 0}} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := configurationProject(t)
				parent := strings.Replace(md009Configuration(flavor), "directives: {enabled: false}", fmt.Sprintf("directives: {enabled: %v}", tc.global), 1)
				parent = strings.Replace(parent, "MD009: {enabled: true}", fmt.Sprintf("MD009: {enabled: true, allow_directives: %v}", tc.rule), 1)
				child := strings.Replace(md009Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				child = strings.Replace(child, "MD009: {enabled: true}", "MD009: {enabled: true, allow_directives: true}", 1)
				putConfigurationFile(t, root, ".mdchk.yaml", parent)
				putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
				putConfigurationFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD009 -->\na \nb \nz")
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

func TestCompiledPlatformMD009InspectionRetainsHiddenParent(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md009Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			source := "<!-- mdchk:disable MD009 -->\na \nb <!-- mdchk:enable MD009 --> \nz \n"
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", source)
			before := configurationBytes(t, root)
			got, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--lines=4", "--format=json")
			if got.Status != 0 || !got.Result.Complete || len(got.Result.Diagnostics) != 1 || len(got.Result.SuppressedDiagnostics) != 0 || p.File.SelectedLines.Start != 4 || p.AST.Data.Root.ByteRange.Start != 0 || p.AST.Data.Root.ByteRange.End != len(source) {
				t.Fatal(got, p)
			}
			d := got.Result.Diagnostics[0]
			if d.Rule != "MD009" || d.Message != "Whitespace at line end." || d.ByteRange.Start != len(source)-2 || d.ByteRange.End != len(source)-1 || d.FixAvailable {
				t.Fatal(d)
			}
			if !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("inspection changed source")
			}
		})
	}
}

func TestCompiledPlatformMD009Composition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"ordinary", "code", "structural"} {
			for _, ending := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}, {"CR", "\r"}} {
				t.Run(flavor+"/"+mode+"/"+ending.name, func(t *testing.T) {
					root := configurationProject(t)
					conf := strings.Replace(md009Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
					original := "雪 " + ending.text + "tail"
					start, end, line, column := 3, 4, 1, 2
					if mode == "code" {
						original = "```" + ending.text + "雪 " + ending.text + "```" + ending.text + "tail"
						start = 6 + len(ending.text)
						end = start + 1
						line = 2
						conf = strings.Replace(conf, "MD009: {enabled: true}", "MD009: {code_blocks: true}", 1)
					}
					if mode == "structural" {
						original = "- 雪" + ending.text + "  " + ending.text + "  tail"
						start = 5 + len(ending.text)
						end = start + 2
						line = 2
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
						if d.Rule != "MD009" || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: start, End: end}) || d.Range.Start != (result.Position{Line: line, Column: column}) || d.Range.End != (result.Position{Line: line, Column: column + end - start}) || d.Snapshot != *snapshot {
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
