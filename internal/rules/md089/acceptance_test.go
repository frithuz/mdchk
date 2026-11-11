package md089_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/frithuz/mdchk/internal/acceptance"
	"github.com/frithuz/mdchk/internal/result"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestMain removes this process's private builds once every test has finished
// with them.
func TestMain(m *testing.M) {
	m.Run()
	acceptance.Cleanup()
}

func md089Configuration(flavor string) string {
	return strings.Replace(strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD089: {enabled: false}", "MD089: {enabled: true}", 1)
}
func TestCompiledPlatformMD089ProductionAnalysis(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	data, err := os.ReadFile("testdata/md089.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor  string
		Recognize, Incomplete bool
		// Manual marks findings that get no correction: in a document opening
		// with an unrecognized block, a heading or a reference label.
		Manual    bool
		SHA256    string
		SizeBytes int
		Maximum   int
		Formats   []string

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
					root := acceptance.Project(t)
					conf := md089Configuration(flavor)
					if f.Recognize {
						conf += "frontmatter: {enabled: true"
						if len(f.Formats) > 0 {
							conf += ", formats: [" + strings.Join(f.Formats, ",") + "]"
						}
						conf += "}\n"
					}
					acceptance.PutFile(t, root, ".mdchk.yaml", conf)
					acceptance.PutFile(t, root, "doc.md", f.Source)
					if f.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source))) || f.SizeBytes != len(f.Source) {
						t.Fatal("literal source provenance", f.Name)
					}
					before := acceptance.ConfigurationBytes(t, root)
					for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
						got := acceptance.RunProcess(t, binary, root, args...)
						out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
						// A preview corrects every fixable finding in one change.
						expected, changes := f.Expected, 0
						if args[0] == "fix" && !f.Manual && len(f.Expected) > 0 {
							expected, changes = nil, 1
						}
						status := 0
						if len(expected) > 0 {
							status = 10
						}
						if f.Incomplete {
							status = 15
						}
						if got.Status != status || out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Diagnostics) != len(expected) || len(out.Changes) != changes || len(out.Documents) != 0 {
							t.Fatal(got)
						}
						if changes == 1 && !reflect.DeepEqual(out.Changes[0].Rules, []string{"MD089"}) {
							t.Fatal(out.Changes)
						}
						for i, w := range expected {
							d := out.Diagnostics[i]
							actual := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
							if !reflect.DeepEqual(actual, w.Range) || d.Rule != "MD089" || d.FixAvailable == f.Manual || d.Message != w.Message || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source {
								t.Fatal(d, w)
							}
						}
					}
					got, p := acceptance.RunInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
					if got.Status != 0 {
						t.Fatal(got)
					}
					if !f.Incomplete {
						found := false
						if p.Rules.Data != nil {
							for _, r := range *p.Rules.Data {
								if r.Rule == "MD089" {
									found = r.Implementation == "available" && r.ActiveEvaluation == "complete"
								}
							}
						}
						if !found {
							t.Fatal(p.Rules)
						}
					}
					if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
						t.Fatal("read-only analysis changed bytes")
					}
				})
			}
		})
	}
}

func TestCompiledPlatformMD089DefaultsAndConfiguration(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, setting := range []string{"OMITTED", "{}", "{enabled: true}", "{enabled: false}"} {
			t.Run(flavor+"/"+setting, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := md089Configuration(flavor)
				if setting == "OMITTED" {
					conf = strings.Replace(conf, "  MD089: {enabled: true}\n", "", 1)
				} else {
					conf = strings.Replace(conf, "MD089: {enabled: true}", "MD089: "+setting, 1)
				}
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", "中A\n")
				before := acceptance.ConfigurationBytes(t, root)
				validated := acceptance.RunProcessEnv(t, binary, root, acceptance.ValidationEnvironment(t), "config", "validate", "--format=json")
				acceptance.ValidateOperationJSON(t, []byte(validated.Stdout), "config validate")
				if validated.Status != 0 {
					t.Fatal(validated)
				}
				got := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
				want, exit := 0, 0
				if setting == "{enabled: true}" {
					want, exit = 1, 10
				}
				if got.Status != exit || !out.Complete || len(out.Diagnostics) != want || !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal(got)
				}
			})
		}
		for _, option := range []string{"null", "{enabled: false, maximum: 1}", "{enabled: false, code_blocks: true}", "{enabled: null}", "{enabled: 'true'}", "{enabled: false, allow_directives: null}", "{allow_directives: 1}"} {
			t.Run(flavor+"/invalid_"+option, func(t *testing.T) {
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", strings.Replace(md089Configuration(flavor), "MD089: {enabled: true}", "MD089: "+option, 1))
				before := acceptance.ConfigurationBytes(t, root)
				got := acceptance.RunProcess(t, binary, root, "lint", "missing.md", "--format=json")
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
				if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 || !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal(got)
				}
			})
		}
	}
}
func TestCompiledPlatformMD089ActiveControls(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source, setting     string
			active, hidden, directive int
		}{
			{"before", "<!-- mdchk:disable MD089 -->\n中A\n", "{enabled: true}", 0, 1, 0},
			{"late", "中A <!-- mdchk:disable MD089 -->\n", "{enabled: true}", 1, 0, 0},
			{"later-enable", "<!-- mdchk:disable MD089 -->\n中A\n<!-- mdchk:enable MD089 -->\n", "{enabled: true}", 0, 1, 0},
			{"configured-off-enable", "<!-- mdchk:enable MD089 -->\n中A\n", "{enabled: false}", 1, 0, 0},
			{"configured-off-context", "<!-- mdchk:enable MD089 -->\n中A\n", "{enabled: false}", 1, 0, 0},
			{"denied-disable", "<!-- mdchk:disable MD089 -->\n中A\n", "{enabled: true, allow_directives: false}", 1, 0, 0},
			{"denied-enable", "<!-- mdchk:enable MD089 -->\n中A\n", "{enabled: false, allow_directives: false}", 0, 0, 0},
			{"code-span", "`<!-- mdchk:disable MD089 -->`\n中A\n", "{enabled: true}", 1, 0, 0},
			{"code-block", "```\n<!-- mdchk:disable MD089 -->\n```\n中A\n", "{enabled: true}", 1, 0, 0},
			{"raw", "<script>\n<!-- mdchk:disable MD089 -->\n</script>\n中A\n", "{enabled: true}", 1, 0, 0},
			{"malformed", "<!-- mdchk:disable MD089 extra -->\n中A\n", "{enabled: true}", 1, 0, 1},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(md089Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				conf = strings.Replace(conf, "MD089: {enabled: true}", "MD089: "+tc.setting, 1)
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", tc.source)
				before := acceptance.ConfigurationBytes(t, root)
				for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}} {
					got := acceptance.RunProcess(t, binary, root, args...)
					out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
					// A preview corrects every active finding.
					active, changes := tc.active, 0
					if args[0] == "fix" && active > 0 {
						active, changes = 0, 1
					}
					status := 0
					if args[0] != "inspect" && active+tc.directive > 0 {
						status = 10
					}
					if got.Status != status || !out.Complete || len(out.Diagnostics) != active+tc.directive || len(out.SuppressedDiagnostics) != tc.hidden || len(out.Changes) != changes || len(out.Documents) != 0 {
						t.Fatal(got)
					}
					for _, d := range out.Diagnostics {
						if d.Rule == "MD089" && (!d.FixAvailable || d.ByteRange.End != d.ByteRange.Start || d.Message != "Missing space between CJK and Latin text.") {
							t.Fatal(d)
						}
					}
				}
				if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal("changed source/config")
				}
			})
		}
	}
}
func TestCompiledPlatformMD089InheritedPermissionsAndSlicing(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name         string
			global, rule bool
			hidden       int
		}{{"allowed", true, true, 1}, {"global", false, true, 0}, {"rule", true, false, 0}} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := acceptance.Project(t)
				parent := strings.Replace(md089Configuration(flavor), "directives: {enabled: false}", fmt.Sprintf("directives: {enabled: %v}", tc.global), 1)
				parent = strings.Replace(parent, "MD089: {enabled: true}", fmt.Sprintf("MD089: {enabled: true, allow_directives: %v}", tc.rule), 1)
				child := strings.Replace(md089Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				acceptance.PutFile(t, root, ".mdchk.yaml", parent)
				acceptance.PutFile(t, root, "nested/.mdchk.yaml", child)
				acceptance.PutFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD089 -->\n中A\nok\n")
				before := acceptance.ConfigurationBytes(t, root)
				got, p := acceptance.RunInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=2", "--format=json")
				if got.Status != 0 || len(got.Result.Diagnostics) != 1-tc.hidden || len(got.Result.SuppressedDiagnostics) != tc.hidden || p.File.SelectedLines.Start != 2 {
					t.Fatal(got, p)
				}
				got, _ = acceptance.RunInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=3", "--format=json")
				if got.Status != 0 || len(got.Result.Diagnostics) != 0 || len(got.Result.SuppressedDiagnostics) != 0 {
					t.Fatal(got)
				}
				// Earlier hidden raw state controls the selected later physical line.
				acceptance.PutFile(t, root, "nested/doc.md", "one <script>\nx  y\n</script>\n")
				got, _ = acceptance.RunInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=2", "--format=json")
				if got.Status != 0 || len(got.Result.Diagnostics) != 0 {
					t.Fatal(got)
				}
				acceptance.PutFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD089 -->\n中A\nok\n")
				if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal("changed nested source/config")
				}
			})
		}
	}
}
func TestCompiledPlatformMD089ReportersAndReset(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			conf := strings.Replace(md089Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			acceptance.PutFile(t, root, "first.md", "<!-- mdchk:disable MD089 -->\n中A\n")
			acceptance.PutFile(t, root, "second.md", "中A\n")
			before := acceptance.ConfigurationBytes(t, root)
			for _, operation := range []string{"lint", "fix"} {
				for _, format := range []string{"text", "json", "vscode", "github"} {
					args := []string{operation, "first.md", "second.md", "--format=" + format}
					if operation == "fix" {
						args = append(args, "--dry-run")
					}
					got := acceptance.RunProcess(t, binary, root, args...)
					// A preview corrects the active finding and reports no violation.
					status, active := 10, 1
					if operation == "fix" {
						status, active = 0, 0
					}
					if got.Status != status || operation == "lint" && !strings.Contains(got.Stdout, "MD089") {
						t.Fatal(got)
					}
					if format == "json" {
						out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), operation)
						if len(out.Diagnostics) != active || len(out.SuppressedDiagnostics) != 1 || active == 1 && out.Diagnostics[0].Path != "second.md" || len(out.Changes) != 1-active || len(out.Documents) != 0 {
							t.Fatal(got)
						}
					}
				}
			}
			if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
				t.Fatal("changed source/config")
			}
		})
	}
}

func TestCompiledPlatformMD089ApplyAndInvalidUTF8(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", md089Configuration(flavor))
			acceptance.PutFile(t, root, "doc.md", "中A\n")
			path := filepath.Join(root, "doc.md")
			info, e := os.Stat(path)
			if e != nil {
				t.Fatal(e)
			}
			beforeApply := acceptance.ReadinessState(t, root)
			got := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
			out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
			after, e := os.Stat(path)
			if e != nil {
				t.Fatal(e)
			}
			if !out.Complete || len(out.Documents) != 0 || info.Mode() != after.Mode() {
				t.Fatal(got)
			}
			// The actual acquisition result establishes native capability, not GOOS.
			// Unsupported adapters refuse before analysis and write nothing.
			if got.Status == 13 {
				if out.Status != "error" || len(out.Errors) != 1 || len(out.Diagnostics) != 0 || len(out.Files) != 0 || len(out.CheckedFiles) != 0 || len(out.Changes) != 0 {
					t.Fatal("not an explicit no-effect adapter refusal", got)
				}
				failure := out.Errors[0]
				if failure.Category != result.IO || failure.Code != "write-acquisition" || failure.Scope != "operation" || !strings.HasPrefix(failure.Message, "native replacement is unsupported on ") {
					t.Fatal(failure)
				}
				acceptance.ReadinessAssertEqual(t, "refused apply", beforeApply, acceptance.ReadinessState(t, root))
				t.Log("actual native adapter unsupported; exact refusal and no effects verified")
			} else {
				//nolint:gosec // Access only synthetic test fixtures.
				written, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				if got.Status != 0 || out.Status != "clean" || len(out.Errors) != 0 || len(out.Diagnostics) != 0 || len(out.Changes) != 1 || !reflect.DeepEqual(out.Changes[0].Rules, []string{"MD089"}) || string(written) != "中 A\n" {
					t.Fatal(got, string(written))
				}
				again := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
				out = acceptance.ValidateOperationJSON(t, []byte(again.Stdout), "fix")
				if again.Status != 0 || len(out.Diagnostics) != 0 || len(out.Changes) != 0 {
					t.Fatal("second fix changed", again)
				}
				t.Log("actual native acquisition succeeded; MD089 correction written once")
			}
			// Continue mandatory encoding checks on both native capability paths.
			if e = os.WriteFile(path, []byte{'>', ' ', ' ', 0xff, '\n'}, 0600); e != nil {
				t.Fatal(e)
			}
			before := acceptance.ConfigurationBytes(t, root)
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
				t.Logf("invalid UTF-8 %s verified after apply", args[0])
			}
			if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
				t.Fatal("invalid input changed")
			}
		})
	}
}

func TestCompiledPlatformMD089Composition(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			conf := strings.Replace(md089Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			candidate := "中 A\n"
			for pass, source := range []string{"中A", candidate} {
				acceptance.PutFile(t, root, "doc.md", source)
				before := acceptance.ConfigurationBytes(t, root)
				got := acceptance.RunProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
				// One preview corrects MD089 and MD047 together; the candidate is clean.
				if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 0 || len(out.Changes) != 1-pass || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				snap := out.Files[0].ResultSnapshot
				if snap == nil || snap.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(candidate))) || snap.SizeBytes != 6 {
					t.Fatal(out)
				}
				if pass == 0 {
					c := out.Changes[0]
					if !reflect.DeepEqual(c.Rules, []string{"MD047", "MD089"}) || c.EditCount != 2 || c.Candidate != *snap {
						t.Fatal(c)
					}
				}
				if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal("dry-run mutation")
				}
			}
			acceptance.PutFile(t, root, "doc.md", "中A")
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
				if got.Status != 0 || !out.Complete || string(data) != candidate || len(out.Changes) != 1 || len(out.Diagnostics) != 0 || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				snap := out.Files[0].ResultSnapshot
				if snap == nil || snap.View != "saved" || snap.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) || snap.SizeBytes != 6 || !reflect.DeepEqual(out.Changes[0].Rules, []string{"MD047", "MD089"}) {
					t.Fatal(out)
				}
			}
		})
	}
}

func TestCompiledPlatformMD089FullContextSelection(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			conf := strings.Replace(md089Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			acceptance.PutFile(t, root, "doc.md", "x<script>中A\n\n中B\n\n</script>中C\n")
			before := acceptance.ConfigurationBytes(t, root)
			for _, line := range []string{"3", "5"} {
				got, _ := acceptance.RunInspection(t, binary, root, root, "inspect", "doc.md", "--lines="+line, "--format=json")
				want := 0
				if line == "5" {
					want = 1
				}
				if got.Status != 0 || !got.Result.Complete || len(got.Result.Diagnostics) != want {
					t.Fatal(got)
				}
				if want == 1 {
					d := got.Result.Diagnostics[0]
					if d.ByteRange.Start != 33 || d.ByteRange.End != 33 || d.Range.Start.Line != 5 || d.Range.Start.Column != 11 || d.Range.End != d.Range.Start {
						t.Fatal(d)
					}
				}
			}
			if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
				t.Fatal("selection mutation")
			}
		})
	}
}
