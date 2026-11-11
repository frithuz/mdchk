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
		Name, Source, Style, SHA256, Flavor, Fixed string
		Size                                       int `json:"size_bytes"`
		Expected                                   []struct {
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
					// A preview that rewrites the breaks reports the candidate, which
					// every fixture leaves with no finding.
					expected, changes := f.Expected, 0
					if args[0] == "fix" && f.Fixed != f.Source {
						expected, changes = nil, 1
					}
					status := 0
					if len(expected) > 0 {
						status = 10
					}
					if got.Status != status || !out.Complete || len(out.Diagnostics) != len(expected) || len(out.Changes) != changes || len(out.Documents) != 0 || len(out.Errors) != 0 {
						t.Fatal(got)
					}
					if changes == 1 {
						c := out.Changes[0]
						if !reflect.DeepEqual(c.Rules, []string{"MD035"}) || c.Candidate.SHA256 != acceptance.Digest(f.Fixed) || c.Candidate.SizeBytes != len(f.Fixed) {
							t.Fatal(c)
						}
					}
					for i, w := range expected {
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
				acceptance.ReadinessAssertEqual(t, "read-only analysis", before, acceptance.ReadinessState(t, root))
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

func TestCompiledPlatformMD035ApplyAndInvalidUTF8(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", md035Configuration(flavor))
			acceptance.PutFile(t, root, "doc.md", "---\n\n***\n")
			path := filepath.Join(root, "doc.md")
			beforeApply := acceptance.ReadinessState(t, root)
			got := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
			out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
			// The actual acquisition result establishes native capability, not GOOS.
			// Unsupported adapters refuse before analysis.
			if got.Status == 13 {
				if out.Status != "error" || len(out.Errors) != 1 || len(out.Diagnostics) != 0 || len(out.Files) != 0 || len(out.CheckedFiles) != 0 {
					t.Fatal("not an explicit no-effect adapter refusal", got)
				}
				failure := out.Errors[0]
				if failure.Category != result.IO || failure.Code != "write-acquisition" || failure.Scope != "operation" || !strings.HasPrefix(failure.Message, "native replacement is unsupported on ") {
					t.Fatal(failure)
				}
				acceptance.ReadinessAssertEqual(t, "unsupported apply", beforeApply, acceptance.ReadinessState(t, root))
				t.Log("actual native adapter unsupported; exact refusal and no effects verified")
			} else {
				if got.Status != 0 || out.Status != "clean" || len(out.Errors) != 0 || len(out.Diagnostics) != 0 || len(out.Changes) != 1 || !reflect.DeepEqual(out.Changes[0].Rules, []string{"MD035"}) || acceptance.Saved(t, root) != "---\n\n---\n" {
					t.Fatal(got)
				}
				// A second run finds nothing left to correct.
				got = acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
				out = acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
				if got.Status != 0 || len(out.Diagnostics) != 0 || len(out.Changes) != 0 || acceptance.Saved(t, root) != "---\n\n---\n" {
					t.Fatal(got)
				}
				t.Log("actual native acquisition succeeded; MD035 correction saved and idempotent")
			}
			// Continue mandatory encoding checks on both native capability paths.
			if e := os.WriteFile(path, []byte{'>', ' ', ' ', 0xff, '\n'}, 0600); e != nil {
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

func TestCompiledPlatformMD035Composition(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			conf := md035Configuration(flavor)
			for _, id := range []string{"MD009", "MD012", "MD047"} {
				conf = strings.Replace(conf, id+": {enabled: false}", id+": {enabled: true}", 1)
			}
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			source, candidate := "a \n\n\n\n---\n\n***", "a\n\n---\n\n---\n"
			// One preview applies every rule's edits together, and the candidate
			// needs no further change.
			for pass, text := range []string{source, candidate} {
				acceptance.PutFile(t, root, "doc.md", text)
				before := acceptance.ConfigurationBytes(t, root)
				got := acceptance.RunProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
				if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 0 || len(out.Changes) != 1-pass || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				if pass == 0 {
					c := out.Changes[0]
					if !reflect.DeepEqual(c.Rules, []string{"MD009", "MD012", "MD035", "MD047"}) || c.Candidate.SHA256 != acceptance.Digest(candidate) || c.Candidate.SizeBytes != len(candidate) {
						t.Fatal(c)
					}
				}
				if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal("dry-run mutation")
				}
			}
			acceptance.PutFile(t, root, "doc.md", source)
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
			if got.Status != 0 || !out.Complete || acceptance.Saved(t, root) != candidate || len(out.Changes) != 1 || len(out.Diagnostics) != 0 || len(out.Documents) != 0 {
				t.Fatal(got)
			}
			got = acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
			out = acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
			if got.Status != 0 || len(out.Changes) != 0 || len(out.Diagnostics) != 0 || acceptance.Saved(t, root) != candidate {
				t.Fatal("second run", got)
			}
		})
	}
}

func TestCompiledPlatformMD035GuardsAndProtectedContent(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source, style, want string
			remaining                 int
		}{
			// Rewriting the break under a paragraph line would make a Setext heading.
			{"setext", "para\n***\n", "dash", "para\n***\n", 1},
			{"item-setext", "---\n\n- a\n  * * *\n", "consistent", "---\n\n- a\n  * * *\n", 1},
			{"list-position", "***\n\n* a\n* * *\n", "consistent", "***\n\n* a\n***\n", 0},
			{"quote-crlf", "---\r\n\r\n> ***\r\n", "consistent", "---\r\n\r\n> ---\r\n", 0},
			{"indent-crlf", "---\r\n\r\n  ***  \r\n", "consistent", "---\r\n\r\n  ---  \r\n", 0},
			{"consistent-spaced", "* * *\n\n---\n", "consistent", "* * *\n\n* * *\n", 0},
			{"fenced", "---\n\n```\n***\n```\n", "consistent", "---\n\n```\n***\n```\n", 0},
			{"html", "---\n\n<div>\n***\n</div>\n", "consistent", "---\n\n<div>\n***\n</div>\n", 0},
			{"front-matter", "---\ntitle: x\n---\n\n***\n\n___\n", "consistent", "---\ntitle: x\n---\n\n***\n\n***\n", 0},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(md035Configuration(flavor), "MD035: {}", "MD035: {style: "+tc.style+"}", 1)
				if tc.name == "front-matter" {
					conf += "frontmatter: {enabled: true}\n"
				}
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", tc.source)
				changes, status := 0, 0
				if tc.want != tc.source {
					changes = 1
				}
				if tc.remaining > 0 {
					status = 10
				}
				for _, args := range [][]string{{"fix", "--dry-run", "doc.md", "--format=json"}, {"fix", "doc.md", "--format=json"}} {
					got := acceptance.RunProcess(t, binary, root, args...)
					out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
					if got.Status == 13 {
						t.Skip("native replacement unsupported")
					}
					if got.Status != status || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != tc.remaining || len(out.Changes) != changes {
						t.Fatal(got)
					}
					for _, d := range out.Diagnostics {
						if d.Rule != "MD035" || d.FixAvailable {
							t.Fatal(d)
						}
					}
					if changes == 1 && (out.Changes[0].Candidate.SHA256 != acceptance.Digest(tc.want) || out.Changes[0].Candidate.SizeBytes != len(tc.want)) {
						t.Fatal(out.Changes[0])
					}
				}
				if acceptance.Saved(t, root) != tc.want {
					t.Fatalf("%q", acceptance.Saved(t, root))
				}
				got := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
				if got.Status != status || len(out.Changes) != 0 || len(out.Diagnostics) != tc.remaining {
					t.Fatal("second run", got)
				}
			})
		}
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
					// A preview corrects every active finding; a suppressed one gets no edit.
					status, active, changes := 0, tc.active, 0
					if args[0] == "fix" && active > 0 {
						active, changes = 0, 1
					}
					if args[0] != "inspect" && active > 0 {
						status = 10
					}
					if got.Status != status || !out.Complete || len(out.Diagnostics) != active || len(out.SuppressedDiagnostics) != tc.hidden || len(out.Changes) != changes {
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
