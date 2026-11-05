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

const md071Violating = "---\ntitle: Guide\n---\n# Guide\n"

func md071Configuration(flavor string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD071: {enabled: false}", "MD071: {enabled: true}", 1)
}

func md071Frontmatter(formats []string) string {
	if formats == nil {
		return "frontmatter: {enabled: true}\n"
	}
	return fmt.Sprintf("frontmatter: {enabled: true, formats: [%s]}\n", strings.Join(formats, ", "))
}

func TestCompiledPlatformMD071ProductionAnalysis(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	data, err := os.ReadFile("../../internal/rules/testdata/md071.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source          string
		Recognize, Incomplete bool
		Formats               []string
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
				t.Run(f.Name, func(t *testing.T) {
					root := configurationProject(t)
					conf := md071Configuration(flavor)
					if f.Recognize {
						conf += md071Frontmatter(f.Formats)
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
						if got.Status != status || out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Diagnostics) != len(f.Expected) || len(out.Changes) != 0 || len(out.Documents) != 0 || len(out.DeferredFixes) != 0 {
							t.Fatal(got)
						}
						uncertain := false
						for _, c := range out.Coverage {
							if c.Rule == "MD071" && c.Reason != result.ParserUncertain {
								t.Fatal(c)
							}
							uncertain = uncertain || c.Reason == result.ParserUncertain
						}
						if uncertain != f.Incomplete || (!f.Incomplete && len(out.Coverage) != 0) {
							t.Fatal(out.Coverage)
						}
						for i, w := range f.Expected {
							d := out.Diagnostics[i]
							actual := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
							if !reflect.DeepEqual(actual, w.Range) || d.Rule != "MD071" || d.FixAvailable || d.Message != w.Message || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source {
								t.Fatal(d, w)
							}
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

func TestCompiledPlatformMD071Configuration(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			setting string
			want    int
		}{{"OMITTED", 1}, {"{}", 1}, {"{enabled: true}", 1}, {"{allow_directives: false}", 1}, {"{enabled: false}", 0}, {"{enabled: false, allow_directives: true}", 0}} {
			t.Run(flavor+"/"+tc.setting, func(t *testing.T) {
				root := configurationProject(t)
				conf := md071Configuration(flavor)
				if tc.setting == "OMITTED" {
					conf = strings.Replace(conf, "  MD071: {enabled: true}\n", "", 1)
				} else {
					conf = strings.Replace(conf, "MD071: {enabled: true}", "MD071: "+tc.setting, 1)
				}
				putConfigurationFile(t, root, ".mdchk.yaml", conf+md071Frontmatter(nil))
				putConfigurationFile(t, root, "doc.md", md071Violating)
				got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
				out := validateConfigurationJSON(t, []byte(got.Stdout))
				status := 0
				if tc.want > 0 {
					status = 10
				}
				if got.Status != status || !out.Complete || len(out.Diagnostics) != tc.want || len(out.Coverage) != 0 || (tc.want > 0 && (out.Diagnostics[0].Rule != "MD071" || out.Diagnostics[0].FixAvailable)) {
					t.Fatal(got)
				}
			})
		}
		for _, option := range []string{"null", "{unknown: true}", "{enabled: false, unknown: true}", "{enabled: null}", "{enabled: 'false'}", "{allow_directives: null}", "{enabled: false, allow_directives: null}", "{allow_directives: 1}"} {
			t.Run(flavor+"/invalid_"+option, func(t *testing.T) {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", strings.Replace(md071Configuration(flavor), "MD071: {enabled: true}", "MD071: "+option, 1)+md071Frontmatter(nil))
				putConfigurationFile(t, root, "doc.md", md071Violating)
				before := configurationBytes(t, root)
				got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
				out := validateConfigurationJSON(t, []byte(got.Stdout))
				if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 || !reflect.DeepEqual(before, configurationBytes(t, root)) {
					t.Fatal(got)
				}
			})
		}
	}
}

func TestCompiledPlatformMD071InapplicableAndDefaults(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, recognize := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", flavor, recognize), func(t *testing.T) {
				root := configurationProject(t)
				conf := md071Configuration(flavor)
				if recognize {
					conf += md071Frontmatter(nil)
				}
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				putConfigurationFile(t, root, "doc.md", md071Violating)
				got, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
				want := 0
				if recognize {
					want = 1
				}
				if got.Status != 0 || !got.Result.Complete || len(got.Result.Diagnostics) != want || len(got.Result.Coverage) != 0 || p.Rules.Data == nil {
					t.Fatal(got, p.Rules)
				}
				found := false
				for _, r := range *p.Rules.Data {
					if r.Rule == "MD071" {
						found = true
						inapplicable := reflect.DeepEqual(r.Reasons, []string{"inapplicable"}) && r.ActiveEvaluation == "skipped"
						if inapplicable == recognize || r.Implementation != "available" {
							t.Fatal(r)
						}
					}
				}
				if !found {
					t.Fatal(p.Rules)
				}
			})
		}
	}
	// No definition selects default GFM, where every default-enabled rule runs.
	root := configurationProject(t)
	putConfigurationFile(t, root, "doc.md", "# Title\n")
	got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
	out := validateConfigurationJSON(t, []byte(got.Stdout))
	if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 0 || len(out.Coverage) != 0 {
		t.Fatal(got)
	}
}
