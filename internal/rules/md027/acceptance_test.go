package md027_test

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

func md027Configuration(flavor string) string {
	return strings.Replace(strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD027: {enabled: false}", "MD027: {enabled: true}", 1)
}
func TestCompiledPlatformMD027ProductionAnalysis(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	data, err := os.ReadFile("testdata/md027.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor  string
		Recognize, Incomplete bool
		// Manual marks a document opening with an unrecognized block, whose
		// findings get no correction.
		Manual    bool
		SHA256    string
		SizeBytes int

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
					conf := md027Configuration(flavor)
					if f.Recognize {
						conf += "frontmatter: {enabled: true}\n"
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
						if changes == 1 && !reflect.DeepEqual(out.Changes[0].Rules, []string{"MD027"}) {
							t.Fatal(out.Changes)
						}
						for i, w := range expected {
							d := out.Diagnostics[i]
							actual := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
							if !reflect.DeepEqual(actual, w.Range) || d.Rule != "MD027" || d.FixAvailable == f.Manual || d.Message != w.Message || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source {
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
								if r.Rule == "MD027" {
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

func TestCompiledPlatformMD027DefaultsAndConfiguration(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, setting := range []string{"OMITTED", "{}", "{enabled: true}", "{enabled: false}"} {
			t.Run(flavor+"/"+setting, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := md027Configuration(flavor)
				if setting == "OMITTED" {
					conf = strings.Replace(conf, "  MD027: {enabled: true}\n", "", 1)
				} else {
					conf = strings.Replace(conf, "MD027: {enabled: true}", "MD027: "+setting, 1)
				}
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", ">  text\n")
				before := acceptance.ConfigurationBytes(t, root)
				validated := acceptance.RunProcessEnv(t, binary, root, acceptance.ValidationEnvironment(t), "config", "validate", "--format=json")
				acceptance.ValidateOperationJSON(t, []byte(validated.Stdout), "config validate")
				if validated.Status != 0 {
					t.Fatal(validated)
				}
				got := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
				want, exit := 1, 10
				if setting == "{enabled: false}" {
					want, exit = 0, 0
				}
				if got.Status != exit || !out.Complete || len(out.Diagnostics) != want || !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal(got)
				}
			})
		}
		for _, option := range []string{"null", "{enabled: false, spaces: 1}", "{enabled: false, spaces_per_tab: 4}", "{enabled: false, prose: true}", "{enabled: false, code_blocks: true}", "{enabled: null}", "{enabled: 'true'}", "{enabled: false, allow_directives: null}", "{allow_directives: 1}"} {
			t.Run(flavor+"/invalid_"+option, func(t *testing.T) {
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", strings.Replace(md027Configuration(flavor), "MD027: {enabled: true}", "MD027: "+option, 1))
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
func TestCompiledPlatformMD027ActiveControls(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source, setting     string
			active, hidden, directive int
		}{
			{"before", "<!-- mdchk:disable MD027 -->\n>  text\n", "{enabled: true}", 0, 1, 0},
			{"late", ">  text <!-- mdchk:disable MD027 -->\n", "{enabled: true}", 1, 0, 0},
			{"later-enable", "<!-- mdchk:disable MD027 -->\n>  text\n<!-- mdchk:enable MD027 -->\n", "{enabled: true}", 0, 1, 0},
			{"configured-off-enable", "<!-- mdchk:enable MD027 -->\n>  text\n", "{enabled: false}", 1, 0, 0},
			{"denied-disable", "<!-- mdchk:disable MD027 -->\n>  text\n", "{enabled: true, allow_directives: false}", 1, 0, 0},
			{"denied-enable", "<!-- mdchk:enable MD027 -->\n>  text\n", "{enabled: false, allow_directives: false}", 0, 0, 0},
			{"code-span", "`<!-- mdchk:disable MD027 -->`\n>  text\n", "{enabled: true}", 1, 0, 0},
			{"code-block", "```\n<!-- mdchk:disable MD027 -->\n```\n>  text\n", "{enabled: true}", 1, 0, 0},
			{"raw", "<script>\n<!-- mdchk:disable MD027 -->\n</script>\n>  text\n", "{enabled: true}", 1, 0, 0},
			{"malformed", "<!-- mdchk:disable MD027 extra -->\n>  text\n", "{enabled: true}", 1, 0, 1},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(md027Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				conf = strings.Replace(conf, "MD027: {enabled: true}", "MD027: "+tc.setting, 1)
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
						if d.Rule == "MD027" && (!d.FixAvailable || d.ByteRange.End <= d.ByteRange.Start || d.Message != "Extra spaces after blockquote marker.") {
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
func TestCompiledPlatformMD027ActiveMetadata(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	data, e := os.ReadFile("testdata/md027_active_metadata.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixtures []struct {
		Kind, Source       string
		ExpectedRecognized []struct {
			Range           []int
			Source, Message string
		}
	}
	if e = json.Unmarshal(data, &fixtures); e != nil {
		t.Fatal(e)
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, f := range fixtures {
			for _, recognize := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%v", flavor, f.Kind, recognize), func(t *testing.T) {
					root := acceptance.Project(t)
					conf := strings.Replace(md027Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1) + fmt.Sprintf("frontmatter: {enabled: %v}\n", recognize)
					acceptance.PutFile(t, root, ".mdchk.yaml", conf)
					acceptance.PutFile(t, root, "doc.md", f.Source)
					before := acceptance.ConfigurationBytes(t, root)
					for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}} {
						got := acceptance.RunProcess(t, binary, root, args...)
						out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
						want, status := 0, 0
						if !recognize {
							var hidden result.SuppressedDiagnostic
							if e := json.Unmarshal(out.SuppressedDiagnostics[0], &hidden); e != nil {
								t.Fatal(e)
							}
							d, w := hidden.Diagnostic, f.ExpectedRecognized[0]
							got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
							if !reflect.DeepEqual(got, w.Range) || d.Message != w.Message || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source || d.FixAvailable {
								t.Fatal(d, w)
							}
						}

						hidden, changes := 1, 0
						if recognize {
							want, hidden = 1, 0
							if args[0] != "inspect" {
								status = 10
							}
							// A preview corrects the active finding.
							if args[0] == "fix" {
								want, status, changes = 0, 0, 1
							}
						}
						if got.Status != status || !out.Complete || len(out.Diagnostics) != want || len(out.SuppressedDiagnostics) != hidden || len(out.Changes) != changes || len(out.Documents) != 0 {
							t.Fatal(got)
						}
						if want == 1 {
							d := out.Diagnostics[0]
							w := f.ExpectedRecognized[0]
							actual := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
							if !reflect.DeepEqual(actual, w.Range) || d.Message != w.Message || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source || !d.FixAvailable {
								t.Fatal(d, w)
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
}
func TestCompiledPlatformMD027InheritedPermissionsAndSlicing(t *testing.T) {
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
				parent := strings.Replace(md027Configuration(flavor), "directives: {enabled: false}", fmt.Sprintf("directives: {enabled: %v}", tc.global), 1)
				parent = strings.Replace(parent, "MD027: {enabled: true}", fmt.Sprintf("MD027: {enabled: true, allow_directives: %v}", tc.rule), 1)
				child := strings.Replace(md027Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				acceptance.PutFile(t, root, ".mdchk.yaml", parent)
				acceptance.PutFile(t, root, "nested/.mdchk.yaml", child)
				acceptance.PutFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD027 -->\n>  text\nok\n")
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
				acceptance.PutFile(t, root, "nested/doc.md", "a <script>\n>  text\n</script>\n")
				got, _ = acceptance.RunInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=2", "--format=json")
				if got.Status != 0 || len(got.Result.Diagnostics) != 0 {
					t.Fatal(got)
				}
				acceptance.PutFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD027 -->\n>  text\nok\n")
				if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal("changed nested source/config")
				}
			})
		}
	}
}
func TestCompiledPlatformMD027ReportersAndReset(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			conf := strings.Replace(md027Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			acceptance.PutFile(t, root, "first.md", "<!-- mdchk:disable MD027 -->\n>  text\n")
			acceptance.PutFile(t, root, "second.md", ">  text\n")
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
					if got.Status != status || operation == "lint" && !strings.Contains(got.Stdout, "MD027") {
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
func TestCompiledPlatformMD027Composition(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	data, e := os.ReadFile("testdata/md027_composition.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixtures []struct {
		Kind, Ending, Source, Candidate, CandidateSHA256, ExpectedSource, ExpectedMessage string
		CandidateSizeBytes                                                                int
		ExpectedRange                                                                     []int
	}
	if e = json.Unmarshal(data, &fixtures); e != nil {
		t.Fatal(e)
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, f := range fixtures {
			t.Run(flavor+"/"+f.Kind+"/"+f.Ending, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(md027Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", f.Source)
				got := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
				if got.Status != 10 || !out.Complete || len(out.Diagnostics) != 2 {
					t.Fatal(got)
				}
				for _, d := range out.Diagnostics {
					if d.Rule != "MD027" {
						continue
					}
					actual := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if !reflect.DeepEqual(actual, f.ExpectedRange) || !d.FixAvailable || d.Message != f.ExpectedMessage || f.Source[d.ByteRange.Start:d.ByteRange.End] != f.ExpectedSource {
						t.Fatal(d)
					}
				}
				// One preview corrects MD027 and MD047 together; the candidate is clean.
				for pass, source := range []string{f.Source, f.Candidate} {
					acceptance.PutFile(t, root, "doc.md", source)
					before := acceptance.ConfigurationBytes(t, root)
					got := acceptance.RunProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
					out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
					if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 0 || len(out.Changes) != 1-pass || len(out.Documents) != 0 {
						t.Fatal(got)
					}
					snap := out.Files[0].ResultSnapshot
					if snap == nil || snap.SHA256 != f.CandidateSHA256 || snap.SizeBytes != f.CandidateSizeBytes {
						t.Fatal(out.Files)
					}
					if pass == 0 {
						c := out.Changes[0]
						if !reflect.DeepEqual(c.Rules, []string{"MD027", "MD047"}) || c.EditCount != 2 || c.Candidate != *snap {
							t.Fatal(c)
						}
					}
					if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
						t.Fatal("preview changed source")
					}
				}
			})
		}
	}
}

func TestCompiledPlatformMD027ApplyAndInvalidUTF8(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", md027Configuration(flavor))
			acceptance.PutFile(t, root, "doc.md", ">  text\n")
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
				if got.Status != 0 || out.Status != "clean" || len(out.Errors) != 0 || len(out.Diagnostics) != 0 || len(out.Changes) != 1 || !reflect.DeepEqual(out.Changes[0].Rules, []string{"MD027"}) || string(written) != "> text\n" {
					t.Fatal(got, string(written))
				}
				again := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
				out = acceptance.ValidateOperationJSON(t, []byte(again.Stdout), "fix")
				if again.Status != 0 || len(out.Diagnostics) != 0 || len(out.Changes) != 0 {
					t.Fatal("second fix changed", again)
				}
				t.Log("actual native acquisition succeeded; MD027 correction written once")
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
