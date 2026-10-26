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

func md093Configuration(flavor string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD093: {enabled: false}", "MD093: {enabled: true}", 1)
}
func TestCompiledPlatformMD093ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	data, err := os.ReadFile("../../internal/rules/testdata/md093.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor  string
		Allowed               []string
		Recognize, Incomplete bool
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
					conf := md093Configuration(flavor)
					allowed, _ := json.Marshal(f.Allowed)
					conf = strings.Replace(conf, "MD093: {enabled: true}", "MD093: {enabled: true, allowed_constructs: "+string(allowed)+"}", 1)
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
							if !reflect.DeepEqual(actual, w.Range) || d.Rule != "MD093" || d.FixAvailable || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source {
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
								if r.Rule == "MD093" {
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

func TestCompiledPlatformMD093ConfigurationAndComposition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, option := range []string{"null", "code", "true", "{}", "[code, wrong]", "[emphasis, 1]", "[Code]"} {
			t.Run(flavor+"/invalid_"+option, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(md093Configuration(flavor), "MD093: {enabled: true}", "MD093: {enabled: false, allowed_constructs: "+option+"}", 1)
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				before := configurationBytes(t, root)
				got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
				if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 || !reflect.DeepEqual(before, configurationBytes(t, root)) {
					t.Fatal(got)
				}
			})
		}
		t.Run(flavor+"/default_disabled", func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "# *x*\n")
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
			if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 0 {
				t.Fatal(got)
			}
			inspected, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
			found := false
			if p.Rules.Data != nil {
				for _, rule := range *p.Rules.Data {
					if rule.Rule == "MD093" {
						found = rule.Implementation == "available"
					}
				}
			}
			if inspected.Status != 0 || !found {
				t.Fatal(inspected, p.Rules)
			}
		})
		for _, allowed := range []bool{false, true} {
			for _, ending := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}, {"CR", "\r"}} {
				t.Run(fmt.Sprintf("%s/composition/%t/%s", flavor, allowed, ending.name), func(t *testing.T) {
					root := configurationProject(t)
					conf := strings.Replace(md093Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
					if allowed {
						conf = strings.Replace(conf, "MD093: {enabled: true}", "MD093: {enabled: true, allowed_constructs: [strong, strong]}", 1)
					}
					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					original := "plain" + ending.text + ending.text + "# é\t**界**"
					candidate := original + ending.text
					digest := fmt.Sprintf("%x", sha256.Sum256([]byte(candidate)))
					want, status := 1, 10
					if allowed {
						want, status = 0, 0
					}
					for pass, source := range []string{original, candidate} {
						putConfigurationFile(t, root, "doc.md", source)
						before := configurationBytes(t, root)
						got := runConfigurationProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
						out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
						if got.Status != status || !out.Complete || len(out.Diagnostics) != want || len(out.Changes) != 1-pass || len(out.Documents) != 0 || len(out.Files) != 1 {
							t.Fatal(got)
						}
						snap := out.Files[0].ResultSnapshot
						if snap == nil || snap.SHA256 != digest || snap.SizeBytes != len(candidate) {
							t.Fatal(out.Files)
						}
						if want == 1 {
							d := out.Diagnostics[0]
							start := 10 + 2*len(ending.text)
							if d.Rule != "MD093" || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: start, End: start + 7}) || d.Range.Start != (result.Position{Line: 3, Column: 5}) || d.Range.End != (result.Position{Line: 3, Column: 10}) || d.Snapshot != *snap {
								t.Fatal(d)
							}
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
