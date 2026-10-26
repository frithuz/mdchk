package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func md007Configuration(flavor string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD007: {enabled: false}", "MD007: {enabled: true}", 1)
}
func TestCompiledPlatformMD007ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	data, err := os.ReadFile("../../internal/rules/testdata/md007.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor  string
		Recognize, Incomplete bool
		Indent                int
		Start                 bool
		Expected              []struct {
			Range  []int
			Source string
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
					conf := md007Configuration(flavor)
					indent := f.Indent
					if indent == 0 {
						indent = 2
					}
					conf = strings.Replace(conf, "MD007: {enabled: true}", fmt.Sprintf("MD007: {enabled: true, indent: %d, start_indented: %t}", indent, f.Start), 1)
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
							if !reflect.DeepEqual(actual, w.Range) || d.Rule != "MD007" || d.FixAvailable || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source {
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
								if r.Rule == "MD007" {
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

func TestCompiledPlatformMD007ConfigurationAndComposition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, option := range []string{"null", "{enabled: false, unknown: true}", "{enabled: false, indent: 0}", "{enabled: false, indent: -1}", "{enabled: false, indent: 1.5}", "{enabled: false, indent: true}", "{enabled: false, indent: null}", "{enabled: false, indent: '2'}", "{enabled: false, start_indented: null}", "{enabled: false, start_indented: 1}", "{enabled: null}", "{enabled: false, allow_directives: 1}", "{enabled: false, allow_directives: null}"} {
			t.Run(flavor+"/invalid_"+option, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(md007Configuration(flavor), "MD007: {enabled: true}", "MD007: "+option, 1)
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
			conf := strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "  MD007: {enabled: false}\n", "", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "- first\n - second\n")
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
			if got.Status != 10 || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD007" || out.Diagnostics[0].FixAvailable {
				t.Fatal(got)
			}
			inspected, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
			found := false
			if p.Rules.Data != nil {
				for _, r := range *p.Rules.Data {
					if r.Rule == "MD007" {
						found = r.Implementation == "available" && r.ConfiguredEnabled && r.ActiveEvaluation == "complete"
					}
				}
			}
			if inspected.Status != 0 || !found {
				t.Fatal(inspected, p.Rules)
			}
		})
		for _, mode := range []string{"first"} {
			for _, ending := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}, {"CR", "\r"}} {
				t.Run(flavor+"/"+mode+"/"+ending.name, func(t *testing.T) {
					root := configurationProject(t)
					conf := strings.Replace(md007Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)

					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					original := "é" + ending.text + ending.text + "9. first" + ending.text + "    - last"
					candidate := original + ending.text
					digest := fmt.Sprintf("%x", sha256.Sum256([]byte(candidate)))
					start, line := 14+3*len(ending.text), 4
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
						if d.Rule != "MD007" || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: start, End: start + 1}) || d.Range.Start != (result.Position{Line: line, Column: 5}) || d.Range.End != (result.Position{Line: line, Column: 6}) || d.Snapshot != *snap {
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

func TestCompiledPlatformMD007DirectivesAndValidation(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	source := "<!-- mdchk:disable MD007 -->\n - one\n- two\n  <!-- mdchk:enable MD007 -->\n - three"
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, setting  string
			active, hidden int
		}{{"hidden first", "{enabled: true}", 1, 1}, {"configured off", "{enabled: false}", 1, 1}, {"denied disable", "{enabled: true, allow_directives: false}", 2, 0}, {"denied enable", "{enabled: false, allow_directives: false}", 0, 0}, {"empty mapping", "{}", 1, 1}} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(md007Configuration(flavor), "MD007: {enabled: true}", "MD007: "+tc.setting, 1)
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
					if tc.active == 1 && out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 73, End: 74}) {
						t.Fatal(out.Diagnostics)
					}
				}
				if !reflect.DeepEqual(before, configurationBytes(t, root)) {
					t.Fatal("read-only commands changed bytes")
				}
			})
		}
	}
}

func TestCompiledPlatformMD007LiteralOptions(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, indent := range []int{1, 2, 4, 1000000000, int(^uint(0) >> 1)} {
			for _, start := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/indent%d/start%t", flavor, indent, start), func(t *testing.T) {
					root := configurationProject(t)
					conf := strings.Replace(md007Configuration(flavor), "MD007: {enabled: true}", fmt.Sprintf("MD007: {enabled: true, indent: %d, start_indented: %t}", indent, start), 1)
					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					for local := 0; local < 4; local++ {
						source := strings.Repeat(" ", local) + "- One\n"
						putConfigurationFile(t, root, "doc.md", source)
						before := configurationBytes(t, root)
						got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
						out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
						count, status := 0, 0
						if (!start && local != 0) || (start && local != indent) {
							count, status = 1, 10
						}
						if got.Status != status || !out.Complete || len(out.Diagnostics) != count || !reflect.DeepEqual(before, configurationBytes(t, root)) {
							t.Fatal(local, got)
						}
						if count == 1 {
							d := out.Diagnostics[0]
							if d.Rule != "MD007" || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: local, End: local + 1}) {
								t.Fatal(d)
							}
						}
					}
					putConfigurationFile(t, root, "doc.md", "1. One\n   - Two\n")
					got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
					out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
					if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 0 {
						t.Fatal(got)
					}
				})
			}
		}
	}
}
