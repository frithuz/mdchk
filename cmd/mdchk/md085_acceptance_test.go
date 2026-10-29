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

func md085Configuration(flavor string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD085: {enabled: false}", "MD085: {enabled: true}", 1)
}
func TestCompiledPlatformMD085ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	data, err := os.ReadFile("../../internal/rules/testdata/md085.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor  string
		Recognize, Incomplete bool
		SHA256                string
		SizeBytes             int
		Maximum               int
		Formats               []string

		Expected []struct {
			Range   []int
			Source  string
			Message string
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	rootData, e := os.ReadFile("testdata/md085_root_literals.json")
	if e != nil {
		t.Fatal(e)
	}
	extra := fixtures[:0:0]
	if e = json.Unmarshal(rootData, &extra); e != nil {
		t.Fatal(e)
	}
	for i := range extra {
		extra[i].Name = "root-literal-" + extra[i].Name
		extra[i].Maximum = 1
	}
	fixtures = append(fixtures, extra...)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, f := range fixtures {
				if f.Flavor != "" && f.Flavor != flavor {
					continue
				}
				t.Run(f.Name, func(t *testing.T) {
					root := configurationProject(t)
					conf := md085Configuration(flavor)
					if f.Recognize {
						conf += "frontmatter: {enabled: true"
						if len(f.Formats) > 0 {
							conf += ", formats: [" + strings.Join(f.Formats, ",") + "]"
						}
						conf += "}\n"
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
							if !reflect.DeepEqual(actual, w.Range) || d.Rule != "MD085" || d.FixAvailable || d.Message != w.Message || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source {
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
								if r.Rule == "MD085" {
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

func TestCompiledPlatformMD085DefaultsAndConfiguration(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, setting := range []string{"OMITTED", "{}", "{enabled: true}", "{enabled: false}"} {
			t.Run(flavor+"/"+setting, func(t *testing.T) {
				root := configurationProject(t)
				conf := md085Configuration(flavor)
				if setting == "OMITTED" {
					conf = strings.Replace(conf, "  MD085: {enabled: true}\n", "", 1)
				} else {
					conf = strings.Replace(conf, "MD085: {enabled: true}", "MD085: "+setting, 1)
				}
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				putConfigurationFile(t, root, "doc.md", "a\n  b\n")
				before := configurationBytes(t, root)
				validated := runConfigurationProcessEnv(t, binary, root, validationEnvironment(t), "config", "validate", "--format=json")
				validateConfigurationOperationJSON(t, []byte(validated.Stdout), "config validate")
				if validated.Status != 0 {
					t.Fatal(validated)
				}
				got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
				out := validateConfigurationJSON(t, []byte(got.Stdout))
				want, exit := 0, 0
				if setting == "{enabled: true}" {
					want, exit = 1, 10
				}
				if got.Status != exit || !out.Complete || len(out.Diagnostics) != want || !reflect.DeepEqual(before, configurationBytes(t, root)) {
					t.Fatal(got)
				}
			})
		}
		for _, option := range []string{"null", "{enabled: false, maximum: 0}", "{enabled: false, maximum: -1}", "{enabled: false, maximum: 1.5}", "{enabled: false, maximum: true}", "{enabled: false, maximum: null}", "{enabled: false, maximum: '2'}", "{enabled: false, maximum: 9223372036854775808}", "{enabled: false, spaces_per_tab: 4}", "{enabled: false, prose: true}", "{enabled: false, code_blocks: true}", "{enabled: null}", "{enabled: 'true'}", "{enabled: false, allow_directives: null}", "{allow_directives: 1}"} {
			t.Run(flavor+"/invalid_"+option, func(t *testing.T) {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", strings.Replace(md085Configuration(flavor), "MD085: {enabled: true}", "MD085: "+option, 1))
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
func TestCompiledPlatformMD085ActiveControls(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source, setting     string
			active, hidden, directive int
		}{
			{"before", "<!-- mdchk:disable MD085 -->\na\n  b\n", "{enabled: true}", 0, 1, 0},
			{"late", "a\n  b <!-- mdchk:disable MD085 -->\n", "{enabled: true}", 1, 0, 0},
			{"later-enable", "<!-- mdchk:disable MD085 -->\na\n  b\n<!-- mdchk:enable MD085 -->\n", "{enabled: true}", 0, 1, 0},
			{"configured-off-enable", "<!-- mdchk:enable MD085 -->\na\n  b\n", "{enabled: false}", 1, 0, 0},
			{"configured-off-context", "<!-- mdchk:enable MD085 -->\na\n   b\n", "{enabled: false}", 1, 0, 0},
			{"denied-disable", "<!-- mdchk:disable MD085 -->\na\n  b\n", "{enabled: true, allow_directives: false}", 1, 0, 0},
			{"denied-enable", "<!-- mdchk:enable MD085 -->\na\n  b\n", "{enabled: false, allow_directives: false}", 0, 0, 0},
			{"code-span", "`<!-- mdchk:disable MD085 -->`\na\n  b\n", "{enabled: true}", 1, 0, 0},
			{"code-block", "```\n<!-- mdchk:disable MD085 -->\n```\na\n  b\n", "{enabled: true}", 1, 0, 0},
			{"raw", "<script>\n<!-- mdchk:disable MD085 -->\n</script>\na\n  b\n", "{enabled: true}", 1, 0, 0},
			{"malformed", "<!-- mdchk:disable MD085 extra -->\na\n  b\n", "{enabled: true}", 1, 0, 1},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(md085Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				conf = strings.Replace(conf, "MD085: {enabled: true}", "MD085: "+tc.setting, 1)
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
					if got.Status != status || !out.Complete || len(out.Diagnostics) != tc.active+tc.directive || len(out.SuppressedDiagnostics) != tc.hidden || len(out.Changes) != 0 || len(out.Documents) != 0 {
						t.Fatal(got)
					}
					for _, d := range out.Diagnostics {
						if d.Rule == "MD085" && (d.FixAvailable || d.ByteRange.End < d.ByteRange.Start || d.Message != "Additional indentation on paragraph continuation.") {
							t.Fatal(d)
						}
					}
				}
				if !reflect.DeepEqual(before, configurationBytes(t, root)) {
					t.Fatal("changed source/config")
				}
			})
		}
	}
}
func TestCompiledPlatformMD085InheritedPermissionsAndSlicing(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name         string
			global, rule bool
			hidden       int
		}{{"allowed", true, true, 1}, {"global", false, true, 0}, {"rule", true, false, 0}} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := configurationProject(t)
				parent := strings.Replace(md085Configuration(flavor), "directives: {enabled: false}", fmt.Sprintf("directives: {enabled: %v}", tc.global), 1)
				parent = strings.Replace(parent, "MD085: {enabled: true}", fmt.Sprintf("MD085: {enabled: true, allow_directives: %v}", tc.rule), 1)
				child := strings.Replace(md085Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				putConfigurationFile(t, root, ".mdchk.yaml", parent)
				putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
				putConfigurationFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD085 -->\na\n  b\nok\n")
				before := configurationBytes(t, root)
				got, p := runInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=3", "--format=json")
				if got.Status != 0 || len(got.Result.Diagnostics) != 1-tc.hidden || len(got.Result.SuppressedDiagnostics) != tc.hidden || p.File.SelectedLines.Start != 3 {
					t.Fatal(got, p)
				}
				got, _ = runInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=4", "--format=json")
				if got.Status != 0 || len(got.Result.Diagnostics) != 0 || len(got.Result.SuppressedDiagnostics) != 0 {
					t.Fatal(got)
				}
				// Earlier hidden raw state controls the selected later physical line.
				putConfigurationFile(t, root, "nested/doc.md", "one <script>\nx  y\n</script>\n")
				got, _ = runInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=3", "--format=json")
				if got.Status != 0 || len(got.Result.Diagnostics) != 0 {
					t.Fatal(got)
				}
				putConfigurationFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD085 -->\na\n  b\nok\n")
				if !reflect.DeepEqual(before, configurationBytes(t, root)) {
					t.Fatal("changed nested source/config")
				}
			})
		}
	}
}
func TestCompiledPlatformMD085ReportersAndReset(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md085Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "first.md", "<!-- mdchk:disable MD085 -->\na\n  b\n")
			putConfigurationFile(t, root, "second.md", "a\n  b\n")
			before := configurationBytes(t, root)
			for _, operation := range []string{"lint", "fix"} {
				for _, format := range []string{"text", "json", "vscode", "github"} {
					args := []string{operation, "first.md", "second.md", "--format=" + format}
					if operation == "fix" {
						args = append(args, "--dry-run")
					}
					got := runConfigurationProcess(t, binary, root, args...)
					if got.Status != 10 || !strings.Contains(got.Stdout, "MD085") {
						t.Fatal(got)
					}
					if format == "json" {
						out := validateConfigurationOperationJSON(t, []byte(got.Stdout), operation)
						if len(out.Diagnostics) != 1 || len(out.SuppressedDiagnostics) != 1 || out.Diagnostics[0].Path != "second.md" || len(out.Changes) != 0 || len(out.Documents) != 0 {
							t.Fatal(got)
						}
					}
				}
			}
			if !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("changed source/config")
			}
		})
	}
}

func TestCompiledPlatformMD085ManualApplyAndInvalidUTF8(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md085Configuration(flavor))
			putConfigurationFile(t, root, "doc.md", "a\n  b\n")
			path := filepath.Join(root, "doc.md")
			info, e := os.Stat(path)
			if e != nil {
				t.Fatal(e)
			}
			before := configurationBytes(t, root)
			beforeApply := readinessState(t, root)
			got := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
			after, e := os.Stat(path)
			if e != nil {
				t.Fatal(e)
			}
			if !out.Complete || len(out.Changes) != 0 || len(out.Documents) != 0 || !reflect.DeepEqual(before, configurationBytes(t, root)) || info.Mode() != after.Mode() {
				t.Fatal(got)
			}
			readinessAssertEqual(t, "manual-only apply", beforeApply, readinessState(t, root))
			// The actual acquisition result establishes native capability, not GOOS.
			// Unsupported adapters refuse before analysis, even with no MD085 edits.
			if got.Status == 13 {
				if out.Status != "error" || len(out.Errors) != 1 || len(out.Diagnostics) != 0 || len(out.Files) != 0 || len(out.CheckedFiles) != 0 {
					t.Fatal("not an explicit no-effect adapter refusal", got)
				}
				failure := out.Errors[0]
				if failure.Category != result.IO || failure.Code != "write-acquisition" || failure.Scope != "operation" || failure.Message != "native replacement requires verified Darwin arm64 APFS and cgo; this adapter is unsupported" {
					t.Fatal(failure)
				}
				t.Log("actual native adapter unsupported; exact refusal and no effects verified")
			} else {
				if got.Status != 10 || out.Status != "violations" || len(out.Errors) != 0 || len(out.Diagnostics) != 1 {
					t.Fatal(got)
				}
				d := out.Diagnostics[0]
				if d.Rule != "MD085" || d.FixAvailable || d.Message != "Additional indentation on paragraph continuation." || d.ByteRange.Start != 2 || d.ByteRange.End != 4 {
					t.Fatal(d)
				}
				t.Log("actual native acquisition succeeded; manual MD085 finding and no effects verified")
			}
			// Continue mandatory encoding checks on both native capability paths.
			if e = os.WriteFile(path, []byte{'>', ' ', ' ', 0xff, '\n'}, 0600); e != nil {
				t.Fatal(e)
			}
			before = configurationBytes(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
				got = runConfigurationProcess(t, binary, root, args...)
				out = validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
				encoding := 0
				for _, failure := range out.Errors {
					if failure.Code == "invalid-utf8" {
						encoding++
						if failure.ByteOffset == nil || *failure.ByteOffset != 3 {
							t.Fatal(failure)
						}
					} else if failure.Code != "validation-failed" || args[0] != "fix" {
						t.Fatal(failure)
					}
				}
				if encoding != 1 {
					t.Fatal(out.Errors)
				}
				if got.Status != 13 || out.Complete || len(out.Errors) != map[string]int{"lint": 1, "fix": 2}[args[0]] || len(out.Diagnostics) != 0 || len(out.Changes) != 0 || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				t.Logf("invalid UTF-8 %s verified after manual apply", args[0])
			}
			if !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("invalid input changed")
			}
		})
	}
}

// Root authored fixed composition literals; MD047 alone changes EOF.
func TestCompiledPlatformMD085Composition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	data, e := os.ReadFile("testdata/md085_composition.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixtures []struct {
		Name, Source, Candidate, CandidateSHA256, ExpectedSource, ExpectedMessage string
		CandidateSizeBytes                                                        int
		ExpectedRange                                                             []int
	}
	if e = json.Unmarshal(data, &fixtures); e != nil {
		t.Fatal(e)
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, f := range fixtures {
			t.Run(flavor+"/"+f.Name, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(md085Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				for pass, source := range []string{f.Source, f.Candidate} {
					putConfigurationFile(t, root, "doc.md", source)
					before := configurationBytes(t, root)
					got := runConfigurationProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
					out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
					if got.Status != 10 || !out.Complete || len(out.Diagnostics) != 1 || len(out.Changes) != 1-pass || len(out.Documents) != 0 {
						t.Fatal(got)
					}
					snap := out.Files[0].ResultSnapshot
					if snap == nil || snap.SHA256 != f.CandidateSHA256 || snap.SizeBytes != f.CandidateSizeBytes {
						t.Fatal(out.Files)
					}
					d := out.Diagnostics[0]
					actual := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if !reflect.DeepEqual(actual, f.ExpectedRange) || d.Rule != "MD085" || d.FixAvailable || d.Message != f.ExpectedMessage || f.Candidate[d.ByteRange.Start:d.ByteRange.End] != f.ExpectedSource || d.Snapshot != *snap {
						t.Fatal(d)
					}
					if pass == 0 {
						c := out.Changes[0]
						if !reflect.DeepEqual(c.Rules, []string{"MD047"}) || c.EditCount != 1 || c.Candidate != *snap {
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

func TestCompiledPlatformMD085CompositionApply(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md085Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "A\r\n  b")
			before := readinessState(t, root)
			got := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
			if got.Status == 13 {
				if len(out.Errors) != 1 || out.Errors[0].Code != "write-acquisition" || out.Errors[0].Message != "native replacement requires verified Darwin arm64 APFS and cgo; this adapter is unsupported" {
					t.Fatal(got)
				}
				readinessAssertEqual(t, "unsupported composition", before, readinessState(t, root))
				return
			}
			data, e := os.ReadFile(filepath.Join(root, "doc.md")) //nolint:gosec // Test-owned fixed target under t.TempDir.
			if e != nil {
				t.Fatal(e)
			}
			if got.Status != 10 || !out.Complete || string(data) != "A\r\n  b\r\n" || len(out.Changes) != 1 || len(out.Diagnostics) != 1 || len(out.Documents) != 0 {
				t.Fatal(got)
			}
			snap := out.Files[0].ResultSnapshot
			d := out.Diagnostics[0]
			if snap == nil || snap.View != "saved" || snap.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) || snap.SizeBytes != len(data) || d.Snapshot != *snap || d.Rule != "MD085" || d.ByteRange.Start != 3 || d.ByteRange.End != 5 || d.FixAvailable || !reflect.DeepEqual(out.Changes[0].Rules, []string{"MD047"}) {
				t.Fatal(out)
			}
		})
	}
}

func TestCompiledPlatformMD085FullContextSelection(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md085Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			conf = strings.Replace(conf, "MD085: {enabled: true}", "MD085: {enabled: false}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "   a <!-- mdchk:enable MD085 -->\n b\n   c\n    d\n")
			before := configurationBytes(t, root)
			got, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--lines=4", "--format=json")
			if got.Status != 0 || !got.Result.Complete || len(got.Result.Diagnostics) != 1 || p.File.SelectedLines.Start != 4 {
				t.Fatal(got)
			}
			d := got.Result.Diagnostics[0]
			if d.Rule != "MD085" || d.Range.Start.Line != 4 || d.Range.Start.Column != 4 || d.Range.End.Column != 5 {
				t.Fatal(d)
			}
			if !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("selection mutated source")
			}
		})
	}
}
