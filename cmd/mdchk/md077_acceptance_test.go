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

func md077Configuration(flavor string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD077: {enabled: false}", "MD077: {enabled: true}", 1)
}
func TestCompiledPlatformMD077ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	data, err := os.ReadFile("../../internal/rules/testdata/md077.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor, Style string
		Recognize, Incomplete       bool

		Expected []struct {
			Range            []int
			Source           string
			Actual, Expected int
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
					conf := md077Configuration(flavor)
					if f.Recognize {
						conf += "frontmatter: {enabled: true}\n"
					}
					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					putConfigurationFile(t, root, "doc.md", f.Source)
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
							if !reflect.DeepEqual(actual, w.Range) || d.Rule != "MD077" || d.FixAvailable || d.Message != fmt.Sprintf("List continuation has %d indentation columns; expected %d.", w.Actual, w.Expected) || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source {
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
								if r.Rule == "MD077" {
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
func TestCompiledPlatformMD077ConfigurationAndComposition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, option := range []string{"null", "{enabled: false, unknown: true}", "{enabled: null}", "{enabled: false, allow_directives: null}", "{enabled: false, allow_directives: 1}", "{enabled: false, style: Loose}", "{style: null}", "{style: 1}", "{style: bad}"} {
			t.Run(flavor+"/invalid_"+option, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(md077Configuration(flavor), "MD077: {enabled: true}", "MD077: "+option, 1)
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				before := configurationBytes(t, root)
				got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
				if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 || !reflect.DeepEqual(before, configurationBytes(t, root)) {
					t.Fatal(got)
				}
			})
		}
		t.Run(flavor+"/implicit_default_enabled", func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "  MD077: {enabled: false}\n", "", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "- A\n Text\n")
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
			if got.Status != 10 || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD077" || out.Diagnostics[0].FixAvailable {
				t.Fatal(got)
			}
			inspected, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
			found := false
			if p.Rules.Data != nil {
				for _, r := range *p.Rules.Data {
					if r.Rule == "MD077" {
						found = r.Implementation == "available" && r.ConfiguredEnabled && r.ActiveEvaluation == "complete"
					}
				}
			}
			if inspected.Status != 0 || !found {
				t.Fatal(inspected, p.Rules)
			}
		})
		for _, mode := range []string{"deficit", "atomic surplus"} {
			for _, ending := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}, {"CR", "\r"}} {
				t.Run(flavor+"/"+mode+"/"+ending.name, func(t *testing.T) {
					root := configurationProject(t)
					conf := strings.Replace(md077Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					original := "- é" + ending.text + " Text"
					start, end, column, endcolumn, actual, line := 5+len(ending.text), 5+len(ending.text), 2, 2, 1, 2
					if mode == "atomic surplus" {
						original = "- é" + ending.text + ending.text + "\tText"
						start = 4 + 2*len(ending.text)
						end = start + 1
						column = 1
						endcolumn = 2
						actual = 4
						line = 3
					}
					candidate := original + ending.text
					digest := fmt.Sprintf("%x", sha256.Sum256([]byte(candidate)))
					for pass, source := range []string{original, candidate} {
						putConfigurationFile(t, root, "doc.md", source)
						before := configurationBytes(t, root)
						got := runConfigurationProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
						out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
						if got.Status != 10 || !out.Complete || len(out.Diagnostics) != 1 || len(out.Changes) != 1-pass || len(out.Documents) != 0 || len(out.Files) != 1 {
							t.Fatal(got)
						}
						snap := out.Files[0].ResultSnapshot
						if snap == nil || snap.SHA256 != digest || snap.SizeBytes != len(candidate) {
							t.Fatal(out.Files)
						}
						d := out.Diagnostics[0]
						if d.Rule != "MD077" || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: start, End: end}) || d.Range.Start != (result.Position{Line: line, Column: column}) || d.Range.End != (result.Position{Line: line, Column: endcolumn}) || d.Snapshot != *snap || d.Message != fmt.Sprintf("List continuation has %d indentation columns; expected 2.", actual) {
							t.Fatal(d)
						}
						if pass == 0 {
							c := out.Changes[0]
							if !reflect.DeepEqual(c.Rules, []string{"MD047"}) || c.EditCount != 1 || c.Candidate != *snap {
								t.Fatal(c)
							}
						}
						if !reflect.DeepEqual(before, configurationBytes(t, root)) {
							t.Fatal("preview changed bytes")
						}
					}
				})
			}
		}
	}
}

func TestCompiledPlatformMD077DirectivesAndValidation(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	source := "<!-- mdchk:disable MD077 -->\n- A\n Text <!-- mdchk:enable MD077 -->\n More"
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, setting  string
			active, hidden int
		}{{"hidden before", "{enabled: true}", 1, 1}, {"configured off", "{enabled: false}", 1, 1}, {"denied disable", "{enabled: true, allow_directives: false}", 2, 0}, {"denied enable", "{enabled: false, allow_directives: false}", 0, 0}, {"empty mapping", "{}", 1, 1}} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(md077Configuration(flavor), "MD077: {enabled: true}", "MD077: "+tc.setting, 1)
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
					if tc.active == 1 && out.Diagnostics[0].ByteRange != (result.ByteRange{Start: len(source) - 4, End: len(source) - 4}) {
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

func TestCompiledPlatformMD077InheritedDirectivePermissions(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name         string
			global, rule bool
			hidden       int
		}{{"allowed", true, true, 2}, {"global denial", false, true, 0}, {"rule denial", true, false, 0}} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := configurationProject(t)
				parent := strings.Replace(md077Configuration(flavor), "directives: {enabled: false}", fmt.Sprintf("directives: {enabled: %v}", tc.global), 1)
				parent = strings.Replace(parent, "MD077: {enabled: true}", fmt.Sprintf("MD077: {enabled: true, allow_directives: %v}", tc.rule), 1)
				child := strings.Replace(md077Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				child = strings.Replace(child, "MD077: {enabled: true}", "MD077: {enabled: true, allow_directives: true}", 1)
				putConfigurationFile(t, root, ".mdchk.yaml", parent)
				putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
				putConfigurationFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD077 -->\n- A\n Text\n More")
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

func TestCompiledPlatformMD077InspectionRetainsHiddenParent(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md077Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			source := "<!-- mdchk:disable MD077 -->\n - - One\n Text <!-- mdchk:enable MD077 -->\n More\n"
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", source)
			before := configurationBytes(t, root)
			got, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--lines=4", "--format=json")
			if got.Status != 0 || !got.Result.Complete || len(got.Result.Diagnostics) != 1 || len(got.Result.SuppressedDiagnostics) != 0 || p.File.SelectedLines.Start != 4 || p.AST.Data.Root.ByteRange.Start != 0 || p.AST.Data.Root.ByteRange.End != len(source) {
				t.Fatal(got, p)
			}
			d := got.Result.Diagnostics[0]
			if d.Rule != "MD077" || d.Message != "List continuation has 1 indentation columns; expected 5." || d.ByteRange.Start != len(source)-5 || d.ByteRange.End != len(source)-5 || d.FixAvailable {
				t.Fatal(d)
			}
			if !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("inspection changed source")
			}
		})
	}
}
