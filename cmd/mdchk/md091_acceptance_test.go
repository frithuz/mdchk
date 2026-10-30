package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/result"
)

type md091NativeLiteral struct {
	Name, Source  string
	Configuration struct {
		Enabled     bool
		MD087       bool `json:"md087_enabled"`
		Allow       bool `json:"allow_directives"`
		Frontmatter string
	}
	Expected struct {
		Complete bool
		Findings []result.Diagnostic
		MD087    []result.Diagnostic `json:"md087_findings"`
	}
}

func md091NativeLiterals(t *testing.T) map[string]md091NativeLiteral {
	t.Helper()
	b, err := os.ReadFile("../../internal/rules/testdata/md091.json")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "2110047ce76969edbe4970529358d4caef98a4d0021bcff824db3426f013f05d" {
		t.Fatal("root oracle identity", err)
	}
	var o struct{ Cases []md091NativeLiteral }
	if err = json.Unmarshal(b, &o); err != nil {
		t.Fatal(err)
	}
	out := map[string]md091NativeLiteral{}
	for _, tc := range o.Cases {
		out[tc.Name] = tc
	}
	return out
}

func md091NativeConfig(flavor string, tc md091NativeLiteral, md047 bool) string {
	conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
	conf = strings.Replace(conf, "directives: {enabled: false}", "directives: {enabled: true}", 1)
	conf = strings.Replace(conf, "MD091: {enabled: false}", fmt.Sprintf("MD091: {enabled: %v, allow_directives: %v}", tc.Configuration.Enabled, tc.Configuration.Allow), 1)
	conf = strings.Replace(conf, "MD087: {enabled: false}", fmt.Sprintf("MD087: {enabled: %v}", tc.Configuration.MD087), 1)
	if md047 {
		conf = strings.Replace(conf, "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	}
	return conf
}

func md091NativeDiagnostics(t *testing.T, out result.Result, tc md091NativeLiteral, source, view string) {
	t.Helper()
	want := append(append([]result.Diagnostic{}, tc.Expected.Findings...), tc.Expected.MD087...)
	if len(out.Diagnostics) != len(want) {
		t.Fatal(out.Diagnostics, want)
	}
	for i, d := range out.Diagnostics {
		w := want[i]
		if d.Rule != w.Rule || d.Message != w.Message || d.ByteRange != w.ByteRange || d.Range != w.Range || d.FixAvailable || d.Snapshot != (result.Snapshot{View: view, SizeBytes: len(source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}) {
			t.Fatal(d, w)
		}
	}
}

func TestCompiledPlatformMD091LiteralLint(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	cases := md091NativeLiterals(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, name := range []string{"basic-strong", "actual-html-markup", "inline-html-exempt", "quote-prefix-mapping", "astral-prefix-scalar-columns", "multiple-flat-candidates", "form1-any-four-case-insensitive-closer", "form1-closer-inside-attribute-keeps-attribute-protected", "failed-named-context-atomic-withholding", "md087-real-hidden-finding-used", "md087-real-complete-absence-unused", "md087-atomic-withheld-prefix-uncertain", "default-disabled", "permitted-enable-of-configured-off"} {
			t.Run(flavor+"/"+name, func(t *testing.T) {
				tc := cases[name]
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md091NativeConfig(flavor, tc, false))
				putConfigurationFile(t, root, "doc.md", tc.Source)
				before := readinessState(t, root)
				got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
				after := readinessState(t, root)
				readinessAssertEqual(t, "MD091 literal lint", before, after)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
				exit := 0
				if !tc.Expected.Complete {
					exit = 15
				} else if len(tc.Expected.Findings)+len(tc.Expected.MD087) > 0 {
					exit = 10
				}
				if got.Status != exit || out.Complete != tc.Expected.Complete || len(out.Errors) != 0 || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(got, out)
				}
				md091NativeDiagnostics(t, out, tc, tc.Source, "original")
			})
		}
	}
}

func TestCompiledPlatformMD091InspectionContextAndPermissions(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	cases := md091NativeLiterals(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, row := range []struct {
			name, focus string
			inherited   bool
		}{
			{"basic-strong", "", false}, {"basic-strong", "1", false}, {"md087-real-hidden-finding-used", "3", false}, {"md087-atomic-withheld-prefix-uncertain", "", false}, {"denied-disable-remains-active", "", true},
		} {
			t.Run(flavor+"/"+row.name+"/"+row.focus, func(t *testing.T) {
				tc := cases[row.name]
				root := configurationProject(t)
				cwd := root
				conf := md091NativeConfig(flavor, tc, false)
				if row.inherited {
					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					cwd = filepath.Join(root, "nested")
					tc.Configuration.Allow = true
					conf = md091NativeConfig(flavor, tc, false)
				}
				if row.inherited {
					putConfigurationFile(t, root, "nested/.mdchk.yaml", conf)
					putConfigurationFile(t, root, "nested/doc.md", tc.Source)
				} else {
					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					putConfigurationFile(t, root, "doc.md", tc.Source)
				}
				before := readinessState(t, root)
				args := []string{"inspect", "doc.md", "--format=json"}
				if row.focus != "" {
					args = append(args, "--lines="+row.focus)
				}
				got, p := runInspection(t, binary, cwd, root, args...)
				readinessAssertEqual(t, "MD091 inspect", before, readinessState(t, root))
				if got.Status != 0 || got.Result.Complete != tc.Expected.Complete {
					t.Fatal(got, p)
				}
				if row.name == "basic-strong" {
					want := 1
					if row.focus == "1" {
						want = 0
					}
					if len(got.Result.Diagnostics) != want {
						t.Fatal(got.Result.Diagnostics)
					}
				}
				if row.name == "md087-real-hidden-finding-used" {
					if p.Directives.Data == nil || len(*p.Directives.Data) != 1 || !(*p.Directives.Data)[0].ContextOnly || (*p.Directives.Data)[0].Effects[0].Reason != "suppression-used" {
						t.Fatal(p.Directives)
					}
				}
				if row.inherited {
					if p.Directives.Data == nil || len(*p.Directives.Data) != 1 {
						t.Fatal(p.Directives)
					}
					e := (*p.Directives.Data)[0].Effects[0]
					if e.Permission != "denied" || e.Effect != "blocked" || len(e.Provenance) == 0 || len(got.Result.Diagnostics) != 1 {
						t.Fatal(e, got.Result)
					}
				}
			})
		}
	}
}

func TestCompiledPlatformMD091MD047Composition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md091NativeLiterals(t)["basic-strong"]
	tc.Source = "<div>\n**x**\n</div>\n\nend"
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, dry := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/dry-%v", flavor, dry), func(t *testing.T) {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md091NativeConfig(flavor, tc, true))
				putConfigurationFile(t, root, "doc.md", tc.Source)
				for pass := 0; pass < 2; pass++ {
					before := readinessState(t, root)
					args := []string{"fix", "doc.md", "--format=json"}
					if dry {
						args = append(args, "--dry-run")
					}
					got := runConfigurationProcess(t, binary, root, args...)
					after := readinessState(t, root)
					out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
					if !dry && md038NativeRefusal(t, got, out) {
						readinessAssertEqual(t, "native refusal", before, after)
						return
					}
					changes := 1
					if !dry && pass == 1 {
						changes = 0
					}
					if got.Status != 10 || !out.Complete || len(out.Errors) != 0 || len(out.Changes) != changes || len(out.Documents)+len(out.DeferredFixes) != 0 {
						t.Fatal(got, out)
					}
					view := "saved"
					if dry {
						view = "candidate"
					}
					md091NativeDiagnostics(t, out, tc, tc.Source+"\n", view)
					for _, change := range out.Changes {
						if len(change.Rules) != 1 || change.Rules[0] != "MD047" {
							t.Fatal(change)
						}
					}
					//nolint:gosec // Fixed document under the canonical test-owned temporary root.
					actual, err := os.ReadFile(filepath.Join(root, "doc.md"))
					want := tc.Source
					if !dry {
						want += "\n"
					}
					if err != nil || string(actual) != want {
						t.Fatal(string(actual), err)
					}
					if dry || pass == 1 {
						readinessAssertEqual(t, "stable repeat", before, after)
					}
				}
			})
		}
	}
}

func TestCompiledPlatformMD091ProtectedEOFFix(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md091NativeLiterals(t)["missing-final-newline"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md091NativeConfig(flavor, tc, true))
			putConfigurationFile(t, root, "doc.md", tc.Source)
			before := readinessState(t, root)
			got := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
			after := readinessState(t, root)
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
			readinessAssertEqual(t, "protected EOF", before, after)
			if md038NativeRefusal(t, got, out) {
				return
			}
			if got.Status != 10 || !out.Complete || len(out.Changes) != 0 || len(out.DeferredFixes) != 1 || len(out.Diagnostics) != 2 {
				t.Fatal(got, out)
			}
			for _, d := range out.Diagnostics {
				if d.FixAvailable {
					t.Fatal(d)
				}
			}
			if out.Diagnostics[0].Rule != "MD091" || out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 6, End: 8}) || out.Diagnostics[1].Rule != "MD047" || out.Diagnostics[1].ByteRange != (result.ByteRange{Start: 11, End: 11}) {
				t.Fatal(out.Diagnostics)
			}
		})
	}
}

func TestCompiledPlatformMD091NoCGOAnalysisAndRefusal(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	binary := buildConfigurationExecutable(t, "")
	tc := md091NativeLiterals(t)["basic-strong"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := configurationProject(t)
			// Builds above retain their original environment. Only actual CLI
			// children use these fixture-owned Git inputs; preserve HOME and all
			// non-Git environment entries, never host Git files or security policy.
			gitGlobal := filepath.Join(root, "absent-gitconfig")
			var childEnv []string
			for _, entry := range os.Environ() {
				key, _, _ := strings.Cut(entry, "=")
				if !strings.HasPrefix(key, "GIT_") {
					childEnv = append(childEnv, entry)
				}
			}
			childEnv = append(childEnv, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+gitGlobal)
			if _, err := os.Lstat(gitGlobal); !os.IsNotExist(err) {
				t.Fatal("fixture global Git input must be absent", err)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", md091NativeConfig(flavor, tc, false))
			putConfigurationFile(t, root, "doc.md", tc.Source)
			before := readinessState(t, root)
			got := runConfigurationProcessEnv(t, binary, root, childEnv, "lint", "doc.md", "--format=json")
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
			if got.Status != 10 || !out.Complete {
				t.Fatal(got, out)
			}
			md091NativeDiagnostics(t, out, tc, tc.Source, "original")
			ins := runConfigurationProcessEnv(t, binary, root, childEnv, "inspect", "doc.md", "--format=json")
			ins.Result = validateConfigurationOperationJSON(t, []byte(ins.Stdout), "inspect")
			var p inspection.Projection
			if err := json.Unmarshal(ins.Result.Inspection, &p); err != nil {
				t.Fatal(err)
			}
			if ins.Status != 0 || !ins.Result.Complete || len(ins.Result.Diagnostics) != 1 {
				t.Fatal(ins, p)
			}
			md091NativeDiagnostics(t, ins.Result, tc, tc.Source, "original")
			config := runConfigurationProcessEnv(t, binary, root, childEnv, "config", "validate", "--format=json")
			validated := validateConfigurationOperationJSON(t, []byte(config.Stdout), "config validate")
			stats := validated.ConfigValidation
			if config.Status != 0 || !validated.Complete || len(validated.Errors) != 0 || stats == nil || !stats.ScanComplete || stats.DefinitionsChecked != 1 || stats.ValidDefinitions != 1 || stats.InvalidDefinitions != 0 || stats.SkippedDefinitions != 0 {
				t.Fatal(config, validated)
			}
			write := runConfigurationProcessEnv(t, binary, root, childEnv, "fix", "doc.md", "--format=json")
			refused := validateConfigurationOperationJSON(t, []byte(write.Stdout), "fix")
			if !md038NativeRefusal(t, write, refused) || !refused.Complete || len(refused.Errors) != 1 || len(refused.Diagnostics)+len(refused.CheckedFiles)+len(refused.Changes)+len(refused.Documents)+len(refused.DeferredFixes) != 0 {
				t.Fatal(write, refused)
			}
			readinessAssertEqual(t, "actual CGO0", before, readinessState(t, root))
			if _, err := os.Lstat(gitGlobal); !os.IsNotExist(err) {
				t.Fatal("fixture global Git input must remain absent", err)
			}
		})
	}
}

func TestCompiledPlatformMD091DefaultsAndAdmission(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md091NativeLiterals(t)["basic-strong"]
	for _, setting := range []string{"OMITTED", "{}", "{enabled: true}", "{enabled: false}"} {
		t.Run(setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := md091NativeConfig("gfm", tc, false)
			old := "MD091: {enabled: true, allow_directives: true}"
			if setting == "OMITTED" {
				conf = strings.Replace(conf, "  "+old+"\n", "", 1)
			} else {
				conf = strings.Replace(conf, old, "MD091: "+setting, 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", tc.Source)
			before := readinessState(t, root)
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
			exit, want := 0, 0
			if setting == "{enabled: true}" {
				exit, want = 10, 1
			}
			if got.Status != exit || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != want {
				t.Fatal(got, out)
			}
			readinessAssertEqual(t, "default admission", before, readinessState(t, root))
		})
	}
	for _, setting := range []string{"null", "[]", "true", "{enabled: null}", "{enabled: 'false'}", "{allow_directives: null}", "{allow_directives: 1}", "{enabled: false, unknown: true}", "{enabled: false, options: {}}"} {
		t.Run("invalid/"+setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md091NativeConfig("gfm", tc, false), "MD091: {enabled: true, allow_directives: true}", "MD091: "+setting, 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			before := readinessState(t, root)
			got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
			if got.Status != 12 {
				t.Fatal(got)
			}
			readinessAssertEqual(t, "admission before acquisition", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD091FourReporters(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md091NativeLiterals(t)["basic-strong"]
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md091NativeConfig("gfm", tc, false))
	putConfigurationFile(t, root, "doc.md", tc.Source)
	before := readinessState(t, root)
	for _, format := range []string{"text", "json", "vscode", "github"} {
		t.Run(format, func(t *testing.T) {
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format="+format)
			if got.Status != 10 || !strings.Contains(got.Stdout, "MD091") || !strings.Contains(got.Stdout, "Markdown syntax is literal inside this HTML block.") {
				t.Fatal(got)
			}
			if format == "json" {
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
				md091NativeDiagnostics(t, out, tc, tc.Source, "original")
			}
		})
	}
	readinessAssertEqual(t, "four reporters", before, readinessState(t, root))
}
