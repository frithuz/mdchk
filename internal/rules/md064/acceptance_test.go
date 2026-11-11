package md064_test

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

func md064Configuration(flavor string) string {
	return strings.Replace(strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD064: {enabled: false}", "MD064: {enabled: true}", 1)
}
func TestCompiledPlatformMD064ProductionAnalysis(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	data, err := os.ReadFile("testdata/md064.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor          string
		Recognize, Incomplete, Manual bool
		SHA256                        string
		SizeBytes                     int
		Maximum                       int
		Formats                       []string

		Expected []struct {
			Range   []int
			Source  string
			Message string
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	rootData, e := os.ReadFile("testdata/md064_root_literals.json")
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
					conf := md064Configuration(flavor)
					conf = strings.Replace(conf, "MD064: {enabled: true}", fmt.Sprintf("MD064: {enabled: true, maximum: %d}", f.Maximum), 1)
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
					// Corrections of these findings are covered by TestCompiledMD064FixWorkflow.
					for _, args := range [][]string{{"lint", "doc.md", "--format=json"}} {
						got := acceptance.RunProcess(t, binary, root, args...)
						out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
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
							if !reflect.DeepEqual(actual, w.Range) || d.Rule != "MD064" || d.FixAvailable == (f.Incomplete || f.Manual) || d.Message != w.Message || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source {
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
								if r.Rule == "MD064" {
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

func TestCompiledPlatformMD064DefaultsAndConfiguration(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, setting := range []string{"OMITTED", "{}", "{enabled: true}", "{enabled: false}"} {
			t.Run(flavor+"/"+setting, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := md064Configuration(flavor)
				if setting == "OMITTED" {
					conf = strings.Replace(conf, "  MD064: {enabled: true}\n", "", 1)
				} else {
					conf = strings.Replace(conf, "MD064: {enabled: true}", "MD064: "+setting, 1)
				}
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", "One  two\n")
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
		for _, option := range []string{"null", "{enabled: false, maximum: 0}", "{enabled: false, maximum: -1}", "{enabled: false, maximum: 1.5}", "{enabled: false, maximum: true}", "{enabled: false, maximum: null}", "{enabled: false, maximum: '2'}", "{enabled: false, maximum: 9223372036854775808}", "{enabled: false, spaces_per_tab: 4}", "{enabled: false, prose: true}", "{enabled: false, code_blocks: true}", "{enabled: null}", "{enabled: 'true'}", "{enabled: false, allow_directives: null}", "{allow_directives: 1}"} {
			t.Run(flavor+"/invalid_"+option, func(t *testing.T) {
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", strings.Replace(md064Configuration(flavor), "MD064: {enabled: true}", "MD064: "+option, 1))
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
func TestCompiledPlatformMD064ActiveControls(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source, setting     string
			active, hidden, directive int
		}{
			{"before", "<!-- mdchk:disable MD064 -->\nOne  two\n", "{enabled: true}", 0, 1, 0},
			{"late", "One  two <!-- mdchk:disable MD064 -->\n", "{enabled: true}", 1, 0, 0},
			{"later-enable", "<!-- mdchk:disable MD064 -->\nOne  two\n<!-- mdchk:enable MD064 -->\n", "{enabled: true}", 0, 1, 0},
			{"configured-off-enable", "<!-- mdchk:enable MD064 -->\nOne  two\n", "{enabled: false}", 1, 0, 0},
			{"configured-off-maximum", "<!-- mdchk:enable MD064 -->\nOne   two\n", "{enabled: false, maximum: 2}", 1, 0, 0},
			{"denied-disable", "<!-- mdchk:disable MD064 -->\nOne  two\n", "{enabled: true, allow_directives: false}", 1, 0, 0},
			{"denied-enable", "<!-- mdchk:enable MD064 -->\nOne  two\n", "{enabled: false, allow_directives: false}", 0, 0, 0},
			{"code-span", "`<!-- mdchk:disable MD064 -->`\nOne  two\n", "{enabled: true}", 1, 0, 0},
			{"code-block", "```\n<!-- mdchk:disable MD064 -->\n```\nOne  two\n", "{enabled: true}", 1, 0, 0},
			{"raw", "<script>\n<!-- mdchk:disable MD064 -->\n</script>\nOne  two\n", "{enabled: true}", 1, 0, 0},
			{"malformed", "<!-- mdchk:disable MD064 extra -->\nOne  two\n", "{enabled: true}", 1, 0, 1},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(md064Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				conf = strings.Replace(conf, "MD064: {enabled: true}", "MD064: "+tc.setting, 1)
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", tc.source)
				before := acceptance.ConfigurationBytes(t, root)
				for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}} {
					got := acceptance.RunProcess(t, binary, root, args...)
					out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
					// The preview corrects every active finding, leaving only directive findings.
					diagnostics, changes := tc.active+tc.directive, 0
					if args[0] == "fix" && tc.active > 0 {
						diagnostics, changes = tc.directive, 1
					}
					status := 0
					if args[0] != "inspect" && diagnostics > 0 {
						status = 10
					}
					if got.Status != status || !out.Complete || len(out.Diagnostics) != diagnostics || len(out.SuppressedDiagnostics) != tc.hidden || len(out.Changes) != changes || len(out.Documents) != 0 {
						t.Fatal(got)
					}
					for _, raw := range out.SuppressedDiagnostics {
						var suppressed result.SuppressedDiagnostic
						if err := json.Unmarshal(raw, &suppressed); err != nil || suppressed.Diagnostic.FixAvailable {
							t.Fatal("suppressed finding advertised", string(raw), err)
						}
					}
					for _, d := range out.Diagnostics {
						if d.Rule == "MD064" && (!d.FixAvailable || d.ByteRange.End < d.ByteRange.Start || d.Message != "Repeated spaces in prose.") {
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
func TestCompiledPlatformMD064InheritedPermissionsAndSlicing(t *testing.T) {
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
				parent := strings.Replace(md064Configuration(flavor), "directives: {enabled: false}", fmt.Sprintf("directives: {enabled: %v}", tc.global), 1)
				parent = strings.Replace(parent, "MD064: {enabled: true}", fmt.Sprintf("MD064: {enabled: true, allow_directives: %v}", tc.rule), 1)
				child := strings.Replace(md064Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				acceptance.PutFile(t, root, ".mdchk.yaml", parent)
				acceptance.PutFile(t, root, "nested/.mdchk.yaml", child)
				acceptance.PutFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD064 -->\nOne  two\nok\n")
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
				acceptance.PutFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD064 -->\nOne  two\nok\n")
				if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal("changed nested source/config")
				}
			})
		}
	}
}
func TestCompiledPlatformMD064ReportersAndReset(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			conf := strings.Replace(md064Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			acceptance.PutFile(t, root, "first.md", "<!-- mdchk:disable MD064 -->\nOne  two\n")
			acceptance.PutFile(t, root, "second.md", "One  two\n")
			before := acceptance.ConfigurationBytes(t, root)
			for _, operation := range []string{"lint", "fix"} {
				for _, format := range []string{"text", "json", "vscode", "github"} {
					args := []string{operation, "first.md", "second.md", "--format=" + format}
					if operation == "fix" {
						args = append(args, "--dry-run")
					}
					got := acceptance.RunProcess(t, binary, root, args...)
					// The preview corrects second.md, so only lint still reports it.
					if operation == "lint" && (got.Status != 10 || !strings.Contains(got.Stdout, "MD064")) || operation == "fix" && got.Status != 0 {
						t.Fatal(got)
					}
					if format == "json" {
						out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), operation)
						if operation == "lint" && (len(out.Diagnostics) != 1 || out.Diagnostics[0].Path != "second.md" || len(out.Changes) != 0) {
							t.Fatal(got)
						}
						if operation == "fix" && (len(out.Diagnostics) != 0 || len(out.Changes) != 1 || out.Changes[0].Path != "second.md") {
							t.Fatal(got)
						}
						if len(out.SuppressedDiagnostics) != 1 || len(out.Documents) != 0 {
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

func TestCompiledPlatformMD064ApplyAndInvalidUTF8(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", md064Configuration(flavor))
			acceptance.PutFile(t, root, "doc.md", "One  two\n")
			path := filepath.Join(root, "doc.md")
			info, e := os.Stat(path)
			if e != nil {
				t.Fatal(e)
			}
			before := acceptance.ConfigurationBytes(t, root)
			got := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
			out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
			after, e := os.Stat(path)
			if e != nil {
				t.Fatal(e)
			}
			if info.Mode() != after.Mode() {
				t.Fatal(got)
			}
			// The actual acquisition result establishes native capability, not GOOS.
			// Unsupported adapters refuse before analysis.
			if got.Status == 13 {
				if out.Status != "error" || len(out.Errors) != 1 || len(out.Diagnostics) != 0 || len(out.Files) != 0 || len(out.CheckedFiles) != 0 || !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal("not an explicit no-effect adapter refusal", got)
				}
				failure := out.Errors[0]
				if failure.Category != result.IO || failure.Code != "write-acquisition" || failure.Scope != "operation" || !strings.HasPrefix(failure.Message, "native replacement is unsupported on ") {
					t.Fatal(failure)
				}
				t.Log("actual native adapter unsupported; exact refusal and no effects verified")
			} else {
				if got.Status != 0 || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != 0 || len(out.Changes) != 1 || !reflect.DeepEqual(out.Changes[0].Rules, []string{"MD064"}) || out.Changes[0].EditCount != 1 {
					t.Fatal(got)
				}
				if saved := acceptance.Saved(t, root); saved != "One two\n" {
					t.Fatalf("saved %q", saved)
				}
				t.Log("actual native acquisition succeeded; MD064 correction verified")
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

// Root authored fixed composition literals; MD064 and MD047 share one round.
func TestCompiledPlatformMD064Composition(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	data, e := os.ReadFile("testdata/md064_composition.json")
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
				conf := strings.Replace(md064Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
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
					if snap == nil || snap.SHA256 != f.CandidateSHA256 || snap.SizeBytes != f.CandidateSizeBytes {
						t.Fatal(out.Files)
					}
					if pass == 0 {
						c := out.Changes[0]
						if !reflect.DeepEqual(c.Rules, []string{"MD047", "MD064"}) || c.EditCount != 2 || c.Candidate != *snap {
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
