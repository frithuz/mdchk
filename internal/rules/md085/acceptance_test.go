package md085_test

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

func md085Configuration(flavor string) string {
	return strings.Replace(strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD085: {enabled: false}", "MD085: {enabled: true}", 1)
}
func TestCompiledPlatformMD085ProductionAnalysis(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	data, err := os.ReadFile("testdata/md085.json")
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
			Range        []int
			Source       string
			Message      string
			FixAvailable bool
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
					root := acceptance.Project(t)
					conf := md085Configuration(flavor)
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
						status := 0
						if len(f.Expected) > 0 {
							status = 10
						}
						if f.Incomplete {
							status = 15
						}
						// A fixable finding makes the preview a candidate, whose
						// remaining findings the fix tests cover.
						fixable := 0
						for _, w := range f.Expected {
							if w.FixAvailable {
								fixable++
							}
						}
						if args[0] == "fix" && fixable > 0 {
							if out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Changes) != 1 || !reflect.DeepEqual(out.Changes[0].Rules, []string{"MD085"}) || out.Changes[0].EditCount != fixable || len(out.Diagnostics) != len(f.Expected)-fixable {
								t.Fatal(got)
							}
							continue
						}
						if got.Status != status || out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Diagnostics) != len(f.Expected) || len(out.Changes) != 0 || len(out.Documents) != 0 {
							t.Fatal(got)
						}
						for i, w := range f.Expected {
							d := out.Diagnostics[i]
							actual := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
							if !reflect.DeepEqual(actual, w.Range) || d.Rule != "MD085" || d.FixAvailable != w.FixAvailable || d.Message != w.Message || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source {
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
								if r.Rule == "MD085" {
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

func TestCompiledPlatformMD085DefaultsAndConfiguration(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, setting := range []string{"OMITTED", "{}", "{enabled: true}", "{enabled: false}"} {
			t.Run(flavor+"/"+setting, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := md085Configuration(flavor)
				if setting == "OMITTED" {
					conf = strings.Replace(conf, "  MD085: {enabled: true}\n", "", 1)
				} else {
					conf = strings.Replace(conf, "MD085: {enabled: true}", "MD085: "+setting, 1)
				}
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", "a\n  b\n")
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
		for _, option := range []string{"null", "{enabled: false, maximum: 0}", "{enabled: false, maximum: -1}", "{enabled: false, maximum: 1.5}", "{enabled: false, maximum: true}", "{enabled: false, maximum: null}", "{enabled: false, maximum: '2'}", "{enabled: false, maximum: 9223372036854775808}", "{enabled: false, spaces_per_tab: 4}", "{enabled: false, prose: true}", "{enabled: false, code_blocks: true}", "{enabled: null}", "{enabled: 'true'}", "{enabled: false, allow_directives: null}", "{allow_directives: 1}"} {
			t.Run(flavor+"/invalid_"+option, func(t *testing.T) {
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", strings.Replace(md085Configuration(flavor), "MD085: {enabled: true}", "MD085: "+option, 1))
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
func TestCompiledPlatformMD085ActiveControls(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
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
				root := acceptance.Project(t)
				conf := strings.Replace(md085Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				conf = strings.Replace(conf, "MD085: {enabled: true}", "MD085: "+tc.setting, 1)
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", tc.source)
				before := acceptance.ConfigurationBytes(t, root)
				for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}} {
					// An active finding is fixable; its preview is covered by the fix tests.
					if tc.active > 0 && args[0] == "fix" {
						continue
					}
					got := acceptance.RunProcess(t, binary, root, args...)
					out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
					status := 0
					if args[0] != "inspect" && tc.active+tc.directive > 0 {
						status = 10
					}
					if got.Status != status || !out.Complete || len(out.Diagnostics) != tc.active+tc.directive || len(out.SuppressedDiagnostics) != tc.hidden || len(out.Changes) != 0 || len(out.Documents) != 0 {
						t.Fatal(got)
					}
					for _, d := range out.Diagnostics {
						if d.Rule == "MD085" && (!d.FixAvailable || d.ByteRange.End < d.ByteRange.Start || d.Message != "Additional indentation on paragraph continuation.") {
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
func TestCompiledPlatformMD085InheritedPermissionsAndSlicing(t *testing.T) {
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
				parent := strings.Replace(md085Configuration(flavor), "directives: {enabled: false}", fmt.Sprintf("directives: {enabled: %v}", tc.global), 1)
				parent = strings.Replace(parent, "MD085: {enabled: true}", fmt.Sprintf("MD085: {enabled: true, allow_directives: %v}", tc.rule), 1)
				child := strings.Replace(md085Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				acceptance.PutFile(t, root, ".mdchk.yaml", parent)
				acceptance.PutFile(t, root, "nested/.mdchk.yaml", child)
				acceptance.PutFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD085 -->\na\n  b\nok\n")
				before := acceptance.ConfigurationBytes(t, root)
				got, p := acceptance.RunInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=3", "--format=json")
				if got.Status != 0 || len(got.Result.Diagnostics) != 1-tc.hidden || len(got.Result.SuppressedDiagnostics) != tc.hidden || p.File.SelectedLines.Start != 3 {
					t.Fatal(got, p)
				}
				got, _ = acceptance.RunInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=4", "--format=json")
				if got.Status != 0 || len(got.Result.Diagnostics) != 0 || len(got.Result.SuppressedDiagnostics) != 0 {
					t.Fatal(got)
				}
				// Earlier hidden raw state controls the selected later physical line.
				acceptance.PutFile(t, root, "nested/doc.md", "one <script>\nx  y\n</script>\n")
				got, _ = acceptance.RunInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=3", "--format=json")
				if got.Status != 0 || len(got.Result.Diagnostics) != 0 {
					t.Fatal(got)
				}
				acceptance.PutFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD085 -->\na\n  b\nok\n")
				if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal("changed nested source/config")
				}
			})
		}
	}
}
func TestCompiledPlatformMD085ReportersAndReset(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			conf := strings.Replace(md085Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			acceptance.PutFile(t, root, "first.md", "<!-- mdchk:disable MD085 -->\na\n  b\n")
			acceptance.PutFile(t, root, "second.md", "a\n  b\n")
			before := acceptance.ConfigurationBytes(t, root)
			for _, operation := range []string{"lint", "fix"} {
				for _, format := range []string{"text", "json", "vscode", "github"} {
					args := []string{operation, "first.md", "second.md", "--format=" + format}
					if operation == "fix" {
						args = append(args, "--dry-run")
					}
					// The preview corrects second.md, so only lint reports it, and the
					// diagnostic-only vscode and github previews print nothing.
					got := acceptance.RunProcess(t, binary, root, args...)
					status, diagnostics, changes := 10, 1, 0
					if operation == "fix" {
						status, diagnostics, changes = 0, 0, 1
					}
					if got.Status != status || (diagnostics > 0 || format == "text" || format == "json") != strings.Contains(got.Stdout, "MD085") {
						t.Fatal(got)
					}
					if format == "json" {
						out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), operation)
						if len(out.Diagnostics) != diagnostics || len(out.SuppressedDiagnostics) != 1 || len(out.Changes) != changes || len(out.Documents) != 0 {
							t.Fatal(got)
						}
						if diagnostics > 0 && out.Diagnostics[0].Path != "second.md" || changes > 0 && out.Changes[0].Path != "second.md" {
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

func TestCompiledPlatformMD085ApplyAndInvalidUTF8(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", md085Configuration(flavor))
			acceptance.PutFile(t, root, "doc.md", "a\n  b\n")
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
			if !out.Complete && got.Status != 13 || len(out.Documents) != 0 || info.Mode() != after.Mode() {
				t.Fatal(got)
			}
			// The actual acquisition result establishes native capability, not GOOS.
			// Unsupported adapters refuse before analysis and change nothing.
			if got.Status == 13 {
				if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal(got)
				}
				acceptance.ReadinessAssertEqual(t, "refused apply", beforeApply, acceptance.ReadinessState(t, root))
				if out.Status != "error" || len(out.Errors) != 1 || len(out.Diagnostics) != 0 || len(out.Files) != 0 || len(out.CheckedFiles) != 0 {
					t.Fatal("not an explicit no-effect adapter refusal", got)
				}
				failure := out.Errors[0]
				if failure.Category != result.IO || failure.Code != "write-acquisition" || failure.Scope != "operation" || !strings.HasPrefix(failure.Message, "native replacement is unsupported on ") {
					t.Fatal(failure)
				}
				t.Log("actual native adapter unsupported; exact refusal and no effects verified")
			} else {
				if got.Status != 0 || len(out.Errors) != 0 || len(out.Diagnostics) != 0 || len(out.Changes) != 1 || !reflect.DeepEqual(out.Changes[0].Rules, []string{"MD085"}) || acceptance.Saved(t, root) != "a\nb\n" {
					t.Fatal(got)
				}
				t.Log("actual native acquisition succeeded; MD085 correction saved")
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
				t.Logf("invalid UTF-8 %s verified after apply", args[0])
			}
			if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
				t.Fatal("invalid input changed")
			}
		})
	}
}

// Root authored fixed composition literals; MD085 and MD047 converge in one run.
func TestCompiledPlatformMD085Composition(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	data, e := os.ReadFile("testdata/md085_composition.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixtures []struct {
		Name, Source, Candidate, CandidateSHA256 string
		CandidateSizeBytes                       int
	}
	if e = json.Unmarshal(data, &fixtures); e != nil {
		t.Fatal(e)
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, f := range fixtures {
			t.Run(flavor+"/"+f.Name, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(md085Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				for pass, source := range []string{f.Source, f.Candidate} {
					acceptance.PutFile(t, root, "doc.md", source)
					before := acceptance.ConfigurationBytes(t, root)
					got := acceptance.RunProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
					out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
					if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 0 || len(out.Changes) != 1-pass || len(out.Documents) != 0 {
						t.Fatal(got)
					}
					snap := out.Files[0].ResultSnapshot
					if snap == nil || snap.SHA256 != f.CandidateSHA256 || snap.SizeBytes != f.CandidateSizeBytes || acceptance.Digest(f.Candidate) != f.CandidateSHA256 {
						t.Fatal(out.Files)
					}
					if pass == 0 {
						c := out.Changes[0]
						if !reflect.DeepEqual(c.Rules, []string{"MD047", "MD085"}) || c.EditCount != 2 || c.Candidate != *snap {
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

func TestCompiledPlatformMD085CompositionApply(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			conf := strings.Replace(md085Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			acceptance.PutFile(t, root, "doc.md", "A\r\n  b")
			before := acceptance.ReadinessState(t, root)
			got := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
			out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
			if got.Status == 13 {
				if len(out.Errors) != 1 || out.Errors[0].Code != "write-acquisition" || !strings.HasPrefix(out.Errors[0].Message, "native replacement is unsupported on ") {
					t.Fatal(got)
				}
				acceptance.ReadinessAssertEqual(t, "unsupported composition", before, acceptance.ReadinessState(t, root))
				return
			}
			data, e := os.ReadFile(filepath.Join(root, "doc.md")) //nolint:gosec // Test-owned fixed target under t.TempDir.
			if e != nil {
				t.Fatal(e)
			}
			if got.Status != 0 || !out.Complete || string(data) != "A\r\nb\r\n" || len(out.Changes) != 1 || len(out.Diagnostics) != 0 || len(out.Documents) != 0 {
				t.Fatal(got)
			}
			snap := out.Files[0].ResultSnapshot
			if snap == nil || snap.View != "saved" || snap.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) || snap.SizeBytes != len(data) || !reflect.DeepEqual(out.Changes[0].Rules, []string{"MD047", "MD085"}) {
				t.Fatal(out)
			}
		})
	}
}

func TestCompiledPlatformMD085FullContextSelection(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			conf := strings.Replace(md085Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			conf = strings.Replace(conf, "MD085: {enabled: true}", "MD085: {enabled: false}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			acceptance.PutFile(t, root, "doc.md", "   a <!-- mdchk:enable MD085 -->\n b\n   c\n    d\n")
			before := acceptance.ConfigurationBytes(t, root)
			got, p := acceptance.RunInspection(t, binary, root, root, "inspect", "doc.md", "--lines=4", "--format=json")
			if got.Status != 0 || !got.Result.Complete || len(got.Result.Diagnostics) != 1 || p.File.SelectedLines.Start != 4 {
				t.Fatal(got)
			}
			d := got.Result.Diagnostics[0]
			if d.Rule != "MD085" || d.Range.Start.Line != 4 || d.Range.Start.Column != 4 || d.Range.End.Column != 5 {
				t.Fatal(d)
			}
			if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
				t.Fatal("selection mutated source")
			}
		})
	}
}

func TestCompiledPlatformMD085Corrections(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source, want string
			rules              []string
		}{
			{"paragraph", "a\n  b\n", "a\nb\n", []string{"MD085"}},
			{"crlf crossing tab", "a\r\n \tb\r\n", "a\r\nb\r\n", []string{"MD085"}},
			{"unicode", "é\n  ö\n", "é\nö\n", []string{"MD085"}},
			{"with MD009 and MD047", "a \n  b", "a\nb\n", []string{"MD009", "MD047", "MD085"}},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := md085Configuration(flavor)
				for _, id := range tc.rules {
					conf = strings.Replace(conf, id+": {enabled: false}", id+": {enabled: true}", 1)
				}
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", tc.source)
				preview := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
				out := preview.Result
				if preview.Status != 0 || !out.Complete || len(out.Diagnostics) != 0 || len(out.Changes) != 1 || !reflect.DeepEqual(out.Changes[0].Rules, tc.rules) || acceptance.Saved(t, root) != tc.source {
					t.Fatal(preview)
				}
				if s := out.Files[0].ResultSnapshot; s == nil || s.SHA256 != acceptance.Digest(tc.want) || s.SizeBytes != len(tc.want) {
					t.Fatal(out.Files)
				}
				saved := acceptance.RunFix(t, binary, root, "fix", "doc.md", "--format=json")
				if saved.Status != 0 || len(saved.Result.Changes) != 1 || acceptance.Saved(t, root) != tc.want {
					t.Fatalf("%v %q", saved, acceptance.Saved(t, root))
				}
				for _, args := range [][]string{{"fix", "--dry-run", "doc.md", "--format=json"}, {"fix", "doc.md", "--format=json"}} {
					again := acceptance.RunFix(t, binary, root, args...)
					if again.Status != 0 || len(again.Result.Changes) != 0 || len(again.Result.Diagnostics) != 0 || acceptance.Saved(t, root) != tc.want {
						t.Fatal(args, again)
					}
				}
			})
		}
	}
}

func TestCompiledPlatformMD085DeferredCorrections(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source            string
			diagnostics, suppressed int
		}{
			{"new list", "a\n    - b\n", 1, 0},
			{"setext underline", "a\n    ===\n", 1, 0},
			{"disabled interval", "<!-- mdchk:disable MD085 -->\n\na\n  b\n", 0, 1},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(md085Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", tc.source)
				for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"fix", "doc.md", "--format=json"}} {
					got := acceptance.RunProcess(t, binary, root, args...)
					out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
					if !out.Complete || len(out.Changes) != 0 || len(out.Diagnostics) != tc.diagnostics || len(out.SuppressedDiagnostics) != tc.suppressed || acceptance.Saved(t, root) != tc.source {
						t.Fatal(args, got)
					}
					for _, d := range out.Diagnostics {
						if d.Rule != "MD085" || d.FixAvailable {
							t.Fatal(d)
						}
					}
					for _, raw := range out.SuppressedDiagnostics {
						var s result.SuppressedDiagnostic
						if err := json.Unmarshal(raw, &s); err != nil || s.Diagnostic.Rule != "MD085" || s.Diagnostic.FixAvailable {
							t.Fatal(string(raw), err)
						}
					}
				}
			})
		}
	}
}
