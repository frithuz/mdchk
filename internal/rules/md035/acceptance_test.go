package md035_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
	"github.com/frithuz/mdchk/internal/result"
)

// TestMain removes this process's private builds once every test has finished
// with them.
func TestMain(m *testing.M) {
	m.Run()
	acceptance.Cleanup()
}

func md035Configuration(flavor string) string {
	return strings.Replace(strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD035: {enabled: false}", "MD035: {}", 1)
}

func TestCompiledPlatformMD035ProductionAnalysis(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	data, err := os.ReadFile("testdata/md035.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Style, SHA256, Flavor string
		Size                                int `json:"size_bytes"`
		Expected                            []struct {
			Range  []int
			Source string
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, f := range fixtures {
			if f.Flavor != "" && f.Flavor != flavor {
				continue
			}
			t.Run(flavor+"/"+f.Name, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(md035Configuration(flavor), "MD035: {}", "MD035: {style: "+f.Style+"}", 1)
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", f.Source)
				if len(f.Source) != f.Size || fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source))) != f.SHA256 {
					t.Fatal("literal source provenance")
				}
				before := acceptance.ReadinessState(t, root)
				for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
					got := acceptance.RunProcess(t, binary, root, args...)
					out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
					status := 0
					if len(f.Expected) > 0 {
						status = 10
					}
					if got.Status != status || !out.Complete || len(out.Diagnostics) != len(f.Expected) || len(out.Changes) != 0 || len(out.Documents) != 0 || len(out.Errors) != 0 {
						t.Fatal(got)
					}
					for i, w := range f.Expected {
						d := out.Diagnostics[i]
						actual := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
						if !reflect.DeepEqual(actual, w.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source || d.Rule != "MD035" || d.FixAvailable || d.Message != "Horizontal rule marker style does not match the expected style." {
							t.Fatal(d, w)
						}
					}
				}
				got, p := acceptance.RunInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
				if got.Status != 0 || len(got.Result.Diagnostics) != len(f.Expected) {
					t.Fatal(got)
				}
				found := false
				if p.Rules.Data != nil {
					for _, r := range *p.Rules.Data {
						if r.Rule == "MD035" {
							found = r.Implementation == "available" && r.ConfiguredEnabled && r.ActiveEvaluation == "complete"
						}
					}
				}
				if !found {
					t.Fatal(p.Rules)
				}
				acceptance.ReadinessAssertEqual(t, "manual analysis", before, acceptance.ReadinessState(t, root))
			})
		}
	}
}

func TestCompiledPlatformMD035DefaultsAndConfiguration(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, setting := range []string{"OMITTED", "{}", "{enabled: false}", "{style: consistent}", "{style: dash}", "{style: asterisk}", "{style: underscore}"} {
			t.Run(flavor+"/"+setting, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := md035Configuration(flavor)
				if setting == "OMITTED" {
					conf = strings.Replace(conf, "  MD035: {}\n", "", 1)
				} else {
					conf = strings.Replace(conf, "MD035: {}", "MD035: "+setting, 1)
				}
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", "---\n\n***\n")
				before := acceptance.ReadinessState(t, root)
				v := acceptance.RunProcessEnv(t, binary, root, acceptance.ValidationEnvironment(t), "config", "validate", "--format=json")
				out := acceptance.ValidateOperationJSON(t, []byte(v.Stdout), "config validate")
				if v.Status != 0 || len(out.Errors) != 0 {
					t.Fatal(v)
				}
				got := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
				out = acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
				want := 1
				if setting == "{enabled: false}" {
					want = 0
				}
				if setting == "{style: underscore}" {
					want = 2
				}
				status := 0
				if want > 0 {
					status = 10
				}
				if got.Status != status || !out.Complete || len(out.Diagnostics) != want {
					t.Fatal(got)
				}
				acceptance.ReadinessAssertEqual(t, "settings", before, acceptance.ReadinessState(t, root))
			})
		}
		for _, setting := range []string{"null", "{style: null}", "{enabled: false, style: ''}", "{enabled: false, style: Dash}", "{enabled: false, style: 3}", "{enabled: false, style: true}", "{enabled: false, style: []}", "{enabled: false, style: {}}", "{enabled: false, style: other}", "{enabled: false, markers: 3}", "{enabled: null}", "{enabled: 'true'}", "{allow_directives: 1}", "{enabled: false, allow_directives: null}"} {
			t.Run(flavor+"/invalid/"+setting, func(t *testing.T) {
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", strings.Replace(md035Configuration(flavor), "MD035: {}", "MD035: "+setting, 1))
				if _, err := os.Stat(filepath.Join(root, "missing.md")); !os.IsNotExist(err) {
					t.Fatal("missing target premise", err)
				}
				before := acceptance.ReadinessState(t, root)
				for _, args := range [][]string{{"lint", "missing.md", "--format=json"}, {"config", "validate", "--format=json"}} {
					got := acceptance.RunProcessEnv(t, binary, root, acceptance.ValidationEnvironment(t), args...)
					operation := args[0]
					if operation == "config" {
						operation = "config validate"
					}
					out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), operation)
					if got.Status != 12 || len(out.Errors) == 0 || (operation == "lint" && len(out.CheckedFiles) != 0) {
						t.Fatal(got)
					}
				}
				acceptance.ReadinessAssertEqual(t, "invalid configuration before target", before, acceptance.ReadinessState(t, root))
			})
		}
	}
}

func TestCompiledPlatformMD035ManualApplyAndInvalidUTF8(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", md035Configuration(flavor))
			acceptance.PutFile(t, root, "doc.md", "---\n\n***\n")
			path := filepath.Join(root, "doc.md")
			info, e := os.Stat(path)
			if e != nil {
				t.Fatal(e)
			}
			before := acceptance.ConfigurationBytes(t, root)
			beforeApply := acceptance.ReadinessState(t, root)
			got := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
			out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
			after, e := os.Stat(path)
			if e != nil {
				t.Fatal(e)
			}
			if !out.Complete || len(out.Changes) != 0 || len(out.Documents) != 0 || !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) || info.Mode() != after.Mode() {
				t.Fatal(got)
			}
			acceptance.ReadinessAssertEqual(t, "manual-only apply", beforeApply, acceptance.ReadinessState(t, root))
			// The actual acquisition result establishes native capability, not GOOS.
			// Unsupported adapters refuse before analysis, even with no MD035 edits.
			if got.Status == 13 {
				if out.Status != "error" || len(out.Errors) != 1 || len(out.Diagnostics) != 0 || len(out.Files) != 0 || len(out.CheckedFiles) != 0 {
					t.Fatal("not an explicit no-effect adapter refusal", got)
				}
				failure := out.Errors[0]
				if failure.Category != result.IO || failure.Code != "write-acquisition" || failure.Scope != "operation" || !strings.HasPrefix(failure.Message, "native replacement is unsupported on ") {
					t.Fatal(failure)
				}
				t.Log("actual native adapter unsupported; exact refusal and no effects verified")
			} else {
				if got.Status != 10 || out.Status != "violations" || len(out.Errors) != 0 || len(out.Diagnostics) != 1 {
					t.Fatal(got)
				}
				d := out.Diagnostics[0]
				if d.Rule != "MD035" || d.FixAvailable || d.Message != "Horizontal rule marker style does not match the expected style." || d.ByteRange.Start != 5 || d.ByteRange.End != 8 {
					t.Fatal(d)
				}
				t.Log("actual native acquisition succeeded; manual MD035 finding and no effects verified")
			}
			// Continue mandatory encoding checks on both native capability paths.
			if e = os.WriteFile(path, []byte{'>', ' ', ' ', 0xff, '\n'}, 0600); e != nil {
				t.Fatal(e)
			}
			before = acceptance.ConfigurationBytes(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
				got = acceptance.RunProcess(t, binary, root, args...)
				out = acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
				encoding := 0
				for _, failure := range out.Errors {
					if failure.Code == "invalid-utf8" {
						encoding++
						if failure.ByteOffset == nil || *failure.ByteOffset != 3 {
							t.Fatal(failure)
						}
					} else {
						t.Fatal(failure)
					}
				}
				if encoding != 1 {
					t.Fatal(out.Errors)
				}
				if got.Status != 13 || out.Complete || len(out.Errors) != 1 || len(out.Diagnostics) != 0 || len(out.Changes) != 0 || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				t.Logf("invalid UTF-8 %s verified after manual apply", args[0])
			}
			if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
				t.Fatal("invalid input changed")
			}
		})
	}
}

func TestCompiledPlatformMD035Composition(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			conf := strings.Replace(md035Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			candidate := "---\n\n***\n"
			for pass, source := range []string{"---\n\n***", candidate} {
				acceptance.PutFile(t, root, "doc.md", source)
				before := acceptance.ConfigurationBytes(t, root)
				got := acceptance.RunProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
				if got.Status != 10 || !out.Complete || len(out.Diagnostics) != 1 || len(out.Changes) != 1-pass || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				snap := out.Files[0].ResultSnapshot
				d := out.Diagnostics[0]
				if snap == nil || snap.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(candidate))) || snap.SizeBytes != 9 || d.Snapshot != *snap || d.Rule != "MD035" || d.ByteRange.Start != 5 || d.ByteRange.End != 8 || d.FixAvailable {
					t.Fatal(out)
				}
				if pass == 0 {
					c := out.Changes[0]
					if !reflect.DeepEqual(c.Rules, []string{"MD047"}) || c.EditCount != 1 || c.Candidate != *snap {
						t.Fatal(c)
					}
				}
				if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal("dry-run mutation")
				}
			}
			acceptance.PutFile(t, root, "doc.md", "---\n\n***")
			before := acceptance.ReadinessState(t, root)
			got := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
			out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
			if got.Status == 13 {
				if len(out.Errors) != 1 || out.Errors[0].Code != "write-acquisition" || !strings.HasPrefix(out.Errors[0].Message, "native replacement is unsupported on ") {
					t.Fatal(got)
				}
				acceptance.ReadinessAssertEqual(t, "unsupported composition", before, acceptance.ReadinessState(t, root))
			} else {
				data, e := os.ReadFile(filepath.Join(root, "doc.md")) //nolint:gosec // Test-owned fixed target under t.TempDir.
				if e != nil {
					t.Fatal(e)
				}
				if got.Status != 10 || !out.Complete || string(data) != candidate || len(out.Changes) != 1 || len(out.Diagnostics) != 1 || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				snap := out.Files[0].ResultSnapshot
				d := out.Diagnostics[0]
				if snap == nil || snap.View != "saved" || snap.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) || snap.SizeBytes != 9 || d.Snapshot != *snap || d.Rule != "MD035" || d.ByteRange.Start != 5 || d.ByteRange.End != 8 || d.FixAvailable || !reflect.DeepEqual(out.Changes[0].Rules, []string{"MD047"}) {
					t.Fatal(out)
				}
			}
		})
	}
}

func TestCompiledPlatformMD035DirectivesBaselineAndSelection(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source, setting, line string
			active, hidden              int
		}{
			{"suppressed-first", "<!-- mdchk:disable MD035 -->\n---\n<!-- mdchk:enable MD035 -->\n***", "{}", "4", 1, 0},
			{"off-first", "---\n<!-- mdchk:enable MD035 -->\n***", "{enabled: false}", "3", 1, 0},
			{"suppressed-later", "---\n<!-- mdchk:disable MD035 -->\n***", "{}", "3", 0, 1},
			{"denied-disable", "---\n<!-- mdchk:disable MD035 -->\n***", "{allow_directives: false}", "3", 1, 0},
			{"denied-enable", "---\n<!-- mdchk:enable MD035 -->\n***", "{enabled: false, allow_directives: false}", "3", 0, 0},
			{"raw-global", "text<script>\n\n---\n\n</script>\n\n***\n\n___", "{}", "9", 1, 0},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(md035Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				conf = strings.Replace(conf, "MD035: {}", "MD035: "+tc.setting, 1)
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", tc.source)
				before := acceptance.ReadinessState(t, root)
				for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"inspect", "doc.md", "--lines=" + tc.line, "--format=json"}} {
					got := acceptance.RunProcess(t, binary, root, args...)
					out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
					status := 0
					if args[0] != "inspect" && tc.active > 0 {
						status = 10
					}
					if got.Status != status || !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden || len(out.Changes) != 0 {
						t.Fatal(got)
					}
				}
				acceptance.ReadinessAssertEqual(t, "context before selection", before, acceptance.ReadinessState(t, root))
			})
		}
	}
}

func TestCompiledPlatformMD035InheritedPermissionsAndReset(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, global := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/global-%t", flavor, global), func(t *testing.T) {
				root := acceptance.Project(t)
				parent := md035Configuration(flavor)
				if global {
					parent = strings.Replace(parent, "directives: {enabled: false}", "directives: {enabled: false}", 1)
				} else {
					parent = strings.Replace(parent, "directives: {enabled: false}", "directives: {enabled: true}", 1)
					parent = strings.Replace(parent, "MD035: {}", "MD035: {allow_directives: false}", 1)
				}
				child := strings.Replace(md035Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				child = strings.Replace(child, "MD035: {}", "MD035: {allow_directives: true}", 1)
				acceptance.PutFile(t, root, ".mdchk.yaml", parent)
				acceptance.PutFile(t, root, "nested/.mdchk.yaml", child)
				acceptance.PutFile(t, root, "nested/doc.md", "---\n<!-- mdchk:disable MD035 -->\n***\n")
				before := acceptance.ReadinessState(t, root)
				got, p := acceptance.RunInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=3", "--format=json")
				if got.Status != 0 || len(got.Result.Diagnostics) != 1 || len(got.Result.SuppressedDiagnostics) != 0 || p.File.SelectedLines.Start != 3 {
					t.Fatal(got, p)
				}
				acceptance.ReadinessAssertEqual(t, "inherited denial", before, acceptance.ReadinessState(t, root))
			})
		}
		t.Run(flavor+"/reset", func(t *testing.T) {
			root := acceptance.Project(t)
			conf := strings.Replace(md035Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			acceptance.PutFile(t, root, "first.md", "<!-- mdchk:disable MD035 -->\n---\n\n***\n")
			acceptance.PutFile(t, root, "second.md", "___\n\n---\n")
			before := acceptance.ReadinessState(t, root)
			for _, format := range []string{"text", "json", "vscode", "github"} {
				got := acceptance.RunProcess(t, binary, root, "lint", "first.md", "second.md", "--format="+format)
				if got.Status != 10 || !strings.Contains(got.Stdout, "MD035") {
					t.Fatal(got)
				}
				if format == "json" {
					out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
					if len(out.Diagnostics) != 1 || out.Diagnostics[0].Path != "second.md" || len(out.SuppressedDiagnostics) != 1 {
						t.Fatal(out)
					}
				}
			}
			acceptance.ReadinessAssertEqual(t, "independent document baseline", before, acceptance.ReadinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD035ImplicitDefaultCoverage(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", "version: 1\nflavor: "+flavor+"\n")
			acceptance.PutFile(t, root, "doc.md", "---\n\n***\n")
			got := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
			out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
			// Every default-enabled rule is available, so defaults are complete.
			if got.Status != 10 || !out.Complete || len(out.Coverage) != 0 {
				t.Fatal(got)
			}
			found := false
			for _, d := range out.Diagnostics {
				if d.Rule == "MD035" {
					found = true
				}
			}
			if !found {
				t.Fatal(got)
			}
		})
	}
}

func TestCompiledPlatformMD035MandatoryEncodingAttribution(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, attribution := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", flavor, attribution), func(t *testing.T) {
				root := acceptance.Project(t)
				conf := md035Configuration(flavor)
				if attribution {
					conf = strings.Replace(conf, "MD094: {enabled: false}", "MD094: {enabled: true}", 1)
				}
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				path := filepath.Join(root, "doc.md")
				if err := os.WriteFile(path, []byte{'x', 0xff, '\n'}, 0600); err != nil {
					t.Fatal(err)
				}
				before := acceptance.ReadinessState(t, root)
				for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
					got := acceptance.RunProcess(t, binary, root, args...)
					out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
					if got.Status != 13 || out.Complete || len(out.Diagnostics) != 0 || len(out.Changes) != 0 {
						t.Fatal(got)
					}
					count := 0
					for _, e := range out.Errors {
						if e.Code == "invalid-utf8" {
							count++
							if e.ByteOffset == nil || *e.ByteOffset != 1 || (e.Rule == "MD094") != attribution {
								t.Fatal(e)
							}
						}
					}
					if count != 1 {
						t.Fatal(out.Errors)
					}
				}
				acceptance.ReadinessAssertEqual(t, "encoding gate", before, acceptance.ReadinessState(t, root))
				acceptance.PutFile(t, root, "doc.md", "�\n\n---\n\n***\n")
				got := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
				if got.Status != 10 || !out.Complete || len(out.Diagnostics) != 1 || len(out.Errors) != 0 {
					t.Fatal(got)
				}
			})
		}
	}
}
