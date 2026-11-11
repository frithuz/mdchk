package md014_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/testenv"
)

// TestMain removes this process's private builds once every test has finished
// with them.
func TestMain(m *testing.M) {
	m.Run()
	acceptance.Cleanup()
}

type md014NativeLiteral struct {
	Name, Source  string
	Configuration struct {
		Enabled          bool
		Omitted          bool `json:"enabled_omitted"`
		MD087            bool `json:"md087_enabled"`
		Allow            bool `json:"allow_directives"`
		Frontmatter      string
		Languages        []string
		LanguagesOmitted bool `json:"languages_omitted"`
		EmptyOptions     bool `json:"empty_options"`
	}
	Expected struct {
		Complete   bool
		Suppressed []result.Diagnostic
		Findings   []result.Diagnostic
		MD087      []result.Diagnostic `json:"md087_findings"`
	}
}

func md014NativeLiterals(t *testing.T) map[string]md014NativeLiteral {
	t.Helper()
	b, err := os.ReadFile("testdata/md014-v2.json")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "b06f76546386295e38b4dc71b871d21398fc25b5e647ae1093ccdb1e5e787431" {
		t.Fatal("active oracle identity", err)
	}
	var oracle struct{ Cases []md014NativeLiteral }
	if err = json.Unmarshal(b, &oracle); err != nil || len(oracle.Cases) != 134 {
		t.Fatal(err, len(oracle.Cases))
	}
	out := map[string]md014NativeLiteral{}
	for _, tc := range oracle.Cases {
		if _, ok := out[tc.Name]; ok {
			t.Fatal("duplicate", tc.Name)
		}
		out[tc.Name] = tc
	}
	return out
}

func md014NativeConfig(flavor string, tc md014NativeLiteral, md047 bool) string {
	conf := strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
	conf = strings.Replace(conf, "directives: {enabled: false}", "directives: {enabled: true}", 1)
	conf = strings.Replace(conf, "MD014: {enabled: false}", fmt.Sprintf("MD014: {enabled: %v, allow_directives: %v}", tc.Configuration.Enabled, tc.Configuration.Allow), 1)
	conf = strings.Replace(conf, "MD087: {enabled: false}", fmt.Sprintf("MD087: {enabled: %v}", tc.Configuration.MD087), 1)
	if !tc.Configuration.LanguagesOmitted {
		list, err := json.Marshal(tc.Configuration.Languages)
		if err != nil {
			panic(err)
		}
		conf = strings.Replace(conf, fmt.Sprintf("MD014: {enabled: %v, allow_directives: %v}", tc.Configuration.Enabled, tc.Configuration.Allow), fmt.Sprintf("MD014: {enabled: %v, allow_directives: %v, languages: %s}", tc.Configuration.Enabled, tc.Configuration.Allow, list), 1)
	}
	if md047 {
		conf = strings.Replace(conf, "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	}
	if tc.Configuration.EmptyOptions {
		conf = strings.Replace(conf, fmt.Sprintf("MD014: {enabled: %v, allow_directives: %v}", tc.Configuration.Enabled, tc.Configuration.Allow), "MD014: {}", 1)
	} else if tc.Configuration.Omitted {
		conf = strings.Replace(conf, "  "+fmt.Sprintf("MD014: {enabled: %v, allow_directives: %v}", tc.Configuration.Enabled, tc.Configuration.Allow)+"\n", "", 1)
	}
	if tc.Configuration.Frontmatter != "" && tc.Configuration.Frontmatter != "none" {
		conf += "frontmatter: {enabled: true, formats: [" + tc.Configuration.Frontmatter + "]}\n"
	}
	return conf
}

func md014NativeDiagnostics(t *testing.T, out result.Result, tc md014NativeLiteral, source, view string) {
	t.Helper()
	want := append(append([]result.Diagnostic{}, tc.Expected.Findings...), tc.Expected.MD087...)
	sort.Slice(want, func(i, j int) bool { return want[i].ByteRange.Start < want[j].ByteRange.Start })
	if len(out.Diagnostics) != len(want) {
		t.Fatal(out.Diagnostics, want)
	}
	if len(out.SuppressedDiagnostics) != len(tc.Expected.Suppressed) {
		t.Fatal(out.SuppressedDiagnostics, tc.Expected.Suppressed)
	}
	for i, raw := range out.SuppressedDiagnostics {
		var h result.SuppressedDiagnostic
		if e := json.Unmarshal(raw, &h); e != nil {
			t.Fatal(e)
		}
		w := tc.Expected.Suppressed[i]
		d := h.Diagnostic
		if d.Rule != w.Rule || d.Message != w.Message || d.ByteRange != w.ByteRange || d.Range != w.Range || d.FixAvailable || d.Snapshot != (result.Snapshot{View: view, SizeBytes: len(source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}) {
			t.Fatal(d, w)
		}
	}
	for i, d := range out.Diagnostics {
		w := want[i]
		if d.Rule != w.Rule || d.Message != w.Message || d.ByteRange != w.ByteRange || d.Range != w.Range || d.FixAvailable || d.Snapshot != (result.Snapshot{View: view, SizeBytes: len(source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}) {
			t.Fatal(d, w)
		}
	}
}

func TestCompiledPlatformMD014LiteralLint(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	cases := md014NativeLiterals(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, name := range []string{"published-dollar-prompt", "published-without-prompt", "default-enabled-omitted", "empty-option-mapping-keeps-defaults", "multiple-prompts-ignore-ascii-blanks", "prompt-tab-separator", "crlf-payload", "lone-cr-payload", "unicode-prefix-snapshot-geometry", "quote-list-owned-payload", "partial-tab-retained-payload-is-output", "custom-language-selected", "custom-list-replaces-defaults", "empty-languages-select-none", "duplicate-languages-single-finding", "info-numeric-entity-decoded-once", "info-no-second-decoding", "yaml-header-opaque", "toml-header-opaque", "incoming-raw-through-html-owner-match", "incoming-raw-through-html-owner-different", "form1-any4-owner-close-resumes", "image-local-raw-open-does-not-protect", "image-local-raw-close-does-not-end-outer", "known-opaque-remainder-is-owner-local", "unknown-required-html-context", "unknown-unselected-language-irrelevant", "empty-languages-ignore-unknown-context", "unknown-withholds-earlier-prompt", "md087-used-hidden-prompt", "md087-unused-mixed-output", "repeated-disables-last-owner-used", "md087-unknown-withheld-prefix-uncertain"} {
			t.Run(flavor+"/"+name, func(t *testing.T) {
				tc := cases[name]
				if tc.Name == "" {
					t.Fatal("missing literal", name)
				}
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", md014NativeConfig(flavor, tc, false))
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				before := acceptance.ReadinessState(t, root)
				got := acceptance.MD014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
				after := acceptance.ReadinessState(t, root)
				acceptance.ReadinessAssertEqual(t, "MD014 literal lint", before, after)
				out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "lint")
				exit := 0
				if !tc.Expected.Complete {
					exit = 15
				} else if len(tc.Expected.Findings)+len(tc.Expected.MD087) > 0 {
					exit = 10
				}
				if got.Status != exit || out.Complete != tc.Expected.Complete || len(out.Errors) != 0 || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(got, out)
				}
				wantCoverage := 0
				if !tc.Expected.Complete {
					wantCoverage = 1
					if tc.Configuration.MD087 {
						wantCoverage = 2
					}
				}
				if len(out.Coverage) != wantCoverage {
					t.Fatal(out.Coverage)
				}
				for _, coverage := range out.Coverage {
					if coverage.Reason != result.ContextUnavailable || (coverage.Rule != "MD014" && coverage.Rule != "MD087") {
						t.Fatal(coverage)
					}
				}
				md014NativeDiagnostics(t, out, tc, tc.Source, "original")
			})
		}
	}
}

func TestCompiledPlatformMD014InspectionContextAndPermissions(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	cases := md014NativeLiterals(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, row := range []struct {
			name, focus string
			inherited   bool
		}{
			{"published-dollar-prompt", "", false}, {"published-dollar-prompt", "2", false}, {"hidden-first-block-visible-second", "7", false}, {"md087-unknown-withheld-prefix-uncertain", "", false}, {"denied-disable-keeps-prompt", "", true},
		} {
			t.Run(flavor+"/"+row.name+"/"+row.focus, func(t *testing.T) {
				tc := cases[row.name]
				root := acceptance.Project(t)
				cwd := root
				conf := md014NativeConfig(flavor, tc, false)
				if row.inherited {
					acceptance.PutFile(t, root, ".mdchk.yaml", conf)
					cwd = filepath.Join(root, "nested")
					tc.Configuration.Allow = true
					conf = md014NativeConfig(flavor, tc, false)
				}
				if row.inherited {
					acceptance.PutFile(t, root, "nested/.mdchk.yaml", conf)
					acceptance.PutFile(t, root, "nested/doc.md", tc.Source)
				} else {
					acceptance.PutFile(t, root, ".mdchk.yaml", conf)
					acceptance.PutFile(t, root, "doc.md", tc.Source)
				}
				before := acceptance.ReadinessState(t, root)
				args := []string{"inspect", "doc.md", "--format=json"}
				if row.focus != "" {
					args = append(args, "--lines="+row.focus)
				}
				got := acceptance.MD014NativeRun(t, binary, cwd, args...)
				got.Result = acceptance.MD014NativeJSON(t, []byte(got.Stdout), "inspect")
				var p inspection.Projection
				if err := json.Unmarshal(got.Result.Inspection, &p); err != nil {
					t.Fatal(err)
				}
				acceptance.ReadinessAssertEqual(t, "MD014 inspect", before, acceptance.ReadinessState(t, root))
				if got.Status != 0 || got.Result.Complete != tc.Expected.Complete {
					t.Fatal(got, p)
				}
				if row.name == "published-dollar-prompt" {
					want := 1
					if row.focus == "2" {
						want = 1
					}
					if len(got.Result.Diagnostics) != want {
						t.Fatal(got.Result.Diagnostics)
					}
				}
				if row.name == "hidden-first-block-visible-second" {
					if len(got.Result.Diagnostics) != 1 || got.Result.Diagnostics[0].ByteRange != (result.ByteRange{Start: 79, End: 80}) {
						t.Fatal(got.Result.Diagnostics)
					}
					if p.Directives.Data == nil || len(*p.Directives.Data) == 0 {
						t.Fatal(p.Directives)
					}
				}
				if row.name == "md087-unknown-withheld-prefix-uncertain" {
					if len(got.Result.Diagnostics) != 0 || len(got.Result.SuppressedDiagnostics) != 0 || len(got.Result.Coverage) != 2 || p.Directives.Data == nil || len(*p.Directives.Data) != 1 || (*p.Directives.Data)[0].Effects[0].Reason != "suppression-uncertain" {
						t.Fatal(got.Result, p.Directives)
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

func TestCompiledPlatformMD014MD047Composition(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	tc := md014NativeLiterals(t)["published-dollar-prompt"]
	tc.Source = "```sh\n$ pwd\n```\nend"
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, dry := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/dry-%v", flavor, dry), func(t *testing.T) {
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", md014NativeConfig(flavor, tc, true))
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				for pass := 0; pass < 2; pass++ {
					before := acceptance.ReadinessState(t, root)
					args := []string{"fix", "doc.md", "--format=json"}
					if dry {
						args = append(args, "--dry-run")
					}
					got := acceptance.MD014NativeRun(t, binary, root, args...)
					after := acceptance.ReadinessState(t, root)
					out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "fix")
					if !dry && acceptance.MD038NativeRefusal(t, got, out) {
						acceptance.ReadinessAssertEqual(t, "native refusal", before, after)
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
					md014NativeDiagnostics(t, out, tc, tc.Source+"\n", view)
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
						acceptance.ReadinessAssertEqual(t, "stable repeat", before, after)
					}
					if !dry && pass == 0 {
						if out.Changes[0].Disposition != "written" || acceptance.ReadinessFile(t, out, "doc.md").WriteStatus != "written" {
							t.Fatal("first apply was not written", out)
						}
						if before["doc.md"].Info.Mode() != after["doc.md"].Info.Mode() {
							t.Fatal("replacement changed mode")
						}
						acceptance.ReadinessAssertParentAfterSave(t, before["."], after["."])
						delete(before, "doc.md")
						delete(after, "doc.md")
						delete(before, ".")
						delete(after, ".")
						acceptance.ReadinessAssertEqual(t, "only MD047 document changed", before, after)
					}
				}
			})
		}
	}
}

func TestCompiledPlatformMD014ProtectedEOFFix(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	tc := md014NativeLiterals(t)["published-dollar-prompt"]
	tc.Source = "```sh\n$ pwd"
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", md014NativeConfig(flavor, tc, true))
			acceptance.PutFile(t, root, "doc.md", tc.Source)
			before := acceptance.ReadinessState(t, root)
			got := acceptance.MD014NativeRun(t, binary, root, "fix", "doc.md", "--format=json")
			after := acceptance.ReadinessState(t, root)
			out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "fix")
			acceptance.ReadinessAssertEqual(t, "protected EOF", before, after)
			if acceptance.MD038NativeRefusal(t, got, out) {
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
			if out.Diagnostics[0].Rule != "MD014" || out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 6, End: 7}) || out.Diagnostics[1].Rule != "MD047" || out.Diagnostics[1].ByteRange != (result.ByteRange{Start: len(tc.Source), End: len(tc.Source)}) {
				t.Fatal(out.Diagnostics)
			}
		})
	}
}

func TestCompiledPlatformMD014NoCGOAnalysisAndApply(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedNoCGOExecutable(t)
	tc := md014NativeLiterals(t)["published-dollar-prompt"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			// Builds above retain their original environment. Only actual CLI
			// children use these fixture-owned Git inputs; preserve HOME and all
			// non-Git environment entries, never host Git files or security policy.
			gitGlobal := filepath.Join(root, "absent-gitconfig")
			childEnv := testenv.Environ()
			childEnv = slices.DeleteFunc(childEnv, func(entry string) bool {
				key, _, _ := strings.Cut(entry, "=")
				return key == "GIT_CONFIG_NOSYSTEM" || key == "GIT_CONFIG_GLOBAL"
			})
			childEnv = append(childEnv, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+gitGlobal)
			if _, err := os.Lstat(gitGlobal); !os.IsNotExist(err) {
				t.Fatal("fixture global Git input must be absent", err)
			}
			acceptance.PutFile(t, root, ".mdchk.yaml", md014NativeConfig(flavor, tc, false))
			acceptance.PutFile(t, root, "doc.md", tc.Source)
			before := acceptance.ReadinessState(t, root)
			got := acceptance.MD014NativeRunEnv(t, binary, root, childEnv, "lint", "doc.md", "--format=json")
			out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "lint")
			if got.Status != 10 || !out.Complete {
				t.Fatal(got, out)
			}
			md014NativeDiagnostics(t, out, tc, tc.Source, "original")
			ins := acceptance.MD014NativeRunEnv(t, binary, root, childEnv, "inspect", "doc.md", "--format=json")
			ins.Result = acceptance.MD014NativeJSON(t, []byte(ins.Stdout), "inspect")
			var p inspection.Projection
			if err := json.Unmarshal(ins.Result.Inspection, &p); err != nil {
				t.Fatal(err)
			}
			if ins.Status != 0 || !ins.Result.Complete || len(ins.Result.Diagnostics) != 1 {
				t.Fatal(ins, p)
			}
			md014NativeDiagnostics(t, ins.Result, tc, tc.Source, "original")
			config := acceptance.MD014NativeRunEnv(t, binary, root, childEnv, "config", "validate", "--format=json")
			validated := acceptance.MD014NativeJSON(t, []byte(config.Stdout), "config validate")
			stats := validated.ConfigValidation
			if config.Status != 0 || !validated.Complete || len(validated.Errors) != 0 || stats == nil || !stats.ScanComplete || stats.DefinitionsChecked != 1 || stats.ValidDefinitions != 1 || stats.InvalidDefinitions != 0 || stats.SkippedDefinitions != 0 {
				t.Fatal(config, validated)
			}
			write := acceptance.MD014NativeRunEnv(t, binary, root, childEnv, "fix", "doc.md", "--format=json")
			refused := acceptance.MD014NativeJSON(t, []byte(write.Stdout), "fix")
			acceptance.AssertCGO0Apply(t, write, refused, before, acceptance.ReadinessState(t, root))
			if _, err := os.Lstat(gitGlobal); !os.IsNotExist(err) {
				t.Fatal("fixture global Git input must remain absent", err)
			}
		})
	}
}

func TestCompiledPlatformMD014DefaultsAndAdmission(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	tc := md014NativeLiterals(t)["published-dollar-prompt"]
	for _, setting := range []string{"OMITTED", "{}", "{enabled: true}", "{enabled: false}"} {
		t.Run(setting, func(t *testing.T) {
			root := acceptance.Project(t)
			conf := md014NativeConfig("gfm", tc, false)
			old := "MD014: {enabled: true, allow_directives: true}"
			if setting == "OMITTED" {
				conf = strings.Replace(conf, "  "+old+"\n", "", 1)
			} else {
				conf = strings.Replace(conf, old, "MD014: "+setting, 1)
			}
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			acceptance.PutFile(t, root, "doc.md", tc.Source)
			before := acceptance.ReadinessState(t, root)
			got := acceptance.MD014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
			out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "lint")
			exit, want := 0, 0
			if setting != "{enabled: false}" {
				exit, want = 10, 1
			}
			if got.Status != exit || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != want {
				t.Fatal(got, out)
			}
			acceptance.ReadinessAssertEqual(t, "default admission", before, acceptance.ReadinessState(t, root))
		})
	}
	for _, setting := range []string{"null", "[]", "true", "{enabled: null}", "{enabled: 'false'}", "{allow_directives: null}", "{allow_directives: 1}", "{enabled: false, unknown: true}", "{enabled: false, options: {}}", "{enabled: false, allow_directives: null}", "{enabled: false, allow_directives: 1}", "{enabled: false, allow_directives: 'true'}"} {
		t.Run("invalid/"+setting, func(t *testing.T) {
			root := acceptance.Project(t)
			conf := strings.Replace(md014NativeConfig("gfm", tc, false), "MD014: {enabled: true, allow_directives: true}", "MD014: "+setting, 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			before := acceptance.ReadinessState(t, root)
			got := acceptance.MD014NativeRun(t, binary, root, "lint", "missing.md", "--format=json")
			if got.Status != 12 {
				t.Fatal(got)
			}
			_ = acceptance.MD014NativeJSON(t, []byte(got.Stdout), "lint")
			acceptance.ReadinessAssertEqual(t, "admission before acquisition", before, acceptance.ReadinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD014FourReporters(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	tc := md014NativeLiterals(t)["published-dollar-prompt"]
	root := acceptance.Project(t)
	acceptance.PutFile(t, root, ".mdchk.yaml", md014NativeConfig("gfm", tc, false))
	acceptance.PutFile(t, root, "doc.md", tc.Source)
	before := acceptance.ReadinessState(t, root)
	for _, format := range []string{"text", "json", "vscode", "github"} {
		t.Run(format, func(t *testing.T) {
			got := acceptance.MD014NativeRun(t, binary, root, "lint", "doc.md", "--format="+format)
			if got.Status != 10 || !strings.Contains(got.Stdout, "MD014") || !strings.Contains(got.Stdout, "Shell command has a dollar prompt without output.") {
				t.Fatal(got)
			}
			switch format {
			case "text":
				if !strings.Contains(got.Stdout, "doc.md:2:1 MD014:") {
					t.Fatal(got)
				}
			case "vscode":
				if !strings.Contains(got.Stdout, "doc.md:2:1: error: MD014:") {
					t.Fatal(got)
				}
			case "github":
				if strings.Count(got.Stdout, "::error file=doc.md,line=2,title=MD014::") != 1 {
					t.Fatal(got)
				}
			}
			if format == "json" {
				out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "lint")
				md014NativeDiagnostics(t, out, tc, tc.Source, "original")
				contract, e := os.ReadFile("../../../docs/contracts/results/result.schema.json")
				if e != nil {
					t.Fatal(e)
				}
				var schema any
				var invalid map[string]any
				if e = json.Unmarshal(contract, &schema); e != nil {
					t.Fatal(e)
				}
				if e = json.Unmarshal([]byte(got.Stdout), &invalid); e != nil {
					t.Fatal(e)
				}
				invalid["unexpected_md014_field"] = true
				if acceptance.MD014SchemaCheck(schema, schema, invalid) == nil {
					t.Fatal("schema accepted extra field")
				}
				delete(invalid, "unexpected_md014_field")
				delete(invalid, "schema_version")
				if acceptance.MD014SchemaCheck(schema, schema, invalid) == nil {
					t.Fatal("schema accepted missing envelope")
				}

			}
		})
	}
	acceptance.ReadinessAssertEqual(t, "four reporters", before, acceptance.ReadinessState(t, root))
}

func md014NativeAPIJSON(t *testing.T, data []byte, path string) result.Result {
	t.Helper()
	out := acceptance.MD014ResultJSON(t, data, "inspect")
	// The raw API retains canonical native paths, including platform separators.
	if !filepath.IsAbs(path) || len(out.CheckedFiles) != 1 || out.CheckedFiles[0] != path || len(out.Files) != 1 || out.Files[0].Path != path {
		t.Fatal("native API target identity", path, out)
	}
	if len(out.Changes)+len(out.DeferredFixes)+len(out.Documents) != 0 {
		t.Fatal("native API inspection proposed corrections", out)
	}
	return out
}

func TestCompiledPlatformMD014NoFix(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	tc := md014NativeLiterals(t)["published-dollar-prompt"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, dry := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry-%v", flavor, dry), func(t *testing.T) {
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", md014NativeConfig(flavor, tc, false))
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				before := acceptance.ReadinessState(t, root)
				args := []string{"fix", "doc.md", "--format=json"}
				if dry {
					args = append(args, "--dry-run")
				}
				got := acceptance.MD014NativeRun(t, binary, root, args...)
				out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "fix")
				acceptance.ReadinessAssertEqual(t, "MD014 NoFix", before, acceptance.ReadinessState(t, root))
				if !dry && acceptance.MD038NativeRefusal(t, got, out) {
					return
				}
				if got.Status != 10 || !out.Complete || len(out.Changes)+len(out.DeferredFixes)+len(out.Documents) != 0 {
					t.Fatal(got, out)
				}
				view := "saved"
				if dry {
					view = "candidate"
				}
				md014NativeDiagnostics(t, out, tc, tc.Source, view)
			})
		}
	}
}

func TestCompiledPlatformMD014InvalidUTF8Admission(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	tc := md014NativeLiterals(t)["published-dollar-prompt"]
	for _, attribution := range []bool{false, true} {
		t.Run(fmt.Sprintf("md094-%v", attribution), func(t *testing.T) {
			root := acceptance.Project(t)
			conf := md014NativeConfig("gfm", tc, false)
			conf = strings.Replace(conf, "MD094: {enabled: false}", fmt.Sprintf("MD094: {enabled: %v}", attribution), 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			acceptance.PutFile(t, root, "doc.md", string(append([]byte("```sh\n$ pwd\n```\n"), 0xff)))
			before := acceptance.ReadinessState(t, root)
			got := acceptance.MD014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
			out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "lint")
			if got.Status != 13 || out.Complete || len(out.Errors) != 1 || len(out.Diagnostics)+len(out.SuppressedDiagnostics)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 || len(out.Coverage) != 1 || out.Coverage[0].Reason != "decoding-failed" {
				t.Fatal(got, out)
			}
			e := out.Errors[0]
			wantRule := ""
			if attribution {
				wantRule = "MD094"
			}
			if e.Category != result.IO || e.Code != "invalid-utf8" || e.Rule != wantRule || e.ByteOffset == nil || *e.ByteOffset != 16 {
				t.Fatal(e)
			}
			acceptance.ReadinessAssertEqual(t, "UTF8 admission before MD014", before, acceptance.ReadinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD014AllInvalidConfigurationOperations(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	b, err := os.ReadFile("testdata/md014-v2.json")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "b06f76546386295e38b4dc71b871d21398fc25b5e647ae1093ccdb1e5e787431" {
		t.Fatal(err)
	}
	var oracle struct {
		Invalid []struct {
			Name       string
			Value      json.RawMessage `json:"md014_value"`
			Operations []string
			Rejected   bool `json:"expected_rejected"`
			Precedes   bool `json:"must_precede_target_acquisition"`
		} `json:"invalid_configurations"`
	}
	if err = json.Unmarshal(b, &oracle); err != nil || len(oracle.Invalid) != 19 {
		t.Fatal(err, len(oracle.Invalid))
	}
	tc := md014NativeLiterals(t)["published-dollar-prompt"]
	for _, row := range oracle.Invalid {
		t.Run(row.Name, func(t *testing.T) {
			if !row.Rejected || !row.Precedes || !reflect.DeepEqual(row.Operations, []string{"lint", "fix"}) {
				t.Fatal(row)
			}
			var compact bytes.Buffer
			if e := json.Compact(&compact, row.Value); e != nil {
				t.Fatal(e)
			}
			root := acceptance.Project(t)
			conf := strings.Replace(md014NativeConfig("gfm", tc, false), "MD014: {enabled: true, allow_directives: true}", "MD014: "+compact.String(), 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			acceptance.PutFile(t, root, "doc.md", tc.Source)
			before := acceptance.ReadinessState(t, root)
			for _, op := range row.Operations {
				got := acceptance.MD014NativeRun(t, binary, root, op, "missing.md", "--format=json")
				out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), op)
				if got.Status != 12 || len(out.CheckedFiles)+len(out.Files)+len(out.Diagnostics)+len(out.Changes) != 0 || len(out.Errors) == 0 {
					t.Fatal(got, out)
				}
				for _, e := range out.Errors {
					if e.Category != result.Configuration {
						t.Fatal(e)
					}
				}
			}
			got := acceptance.MD014NativeRun(t, binary, root, "inspect", "doc.md", "--format=json")
			out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "inspect")
			var view inspection.Projection
			if e := json.Unmarshal(out.Inspection, &view); e != nil {
				t.Fatal(e)
			}
			if got.Status != 0 || out.Status != "inspected" || out.Complete || len(out.CheckedFiles) != 1 || view.File.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) || len(out.Errors) == 0 || view.Rules.Availability != "unavailable" {
				t.Fatal(got, out, view)
			}
			valid := acceptance.MD014NativeRun(t, binary, root, "config", "validate", "--format=json")
			validated := acceptance.MD014NativeJSON(t, []byte(valid.Stdout), "config validate")
			if valid.Status != 12 || validated.ConfigValidation == nil || !validated.ConfigValidation.ScanComplete || validated.ConfigValidation.InvalidDefinitions != 1 || len(validated.Coverage) != 0 {
				t.Fatal(valid, validated)
			}
			acceptance.ReadinessAssertEqual(t, "operation-scoped invalid configuration", before, acceptance.ReadinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD014DefaultCatalogCoverage(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	root := acceptance.Project(t)
	acceptance.PutFile(t, root, "doc.md", "```sh\n$ pwd\n```\n")
	before := acceptance.ReadinessState(t, root)
	got := acceptance.MD014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
	out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "lint")
	if got.Status != 10 || !out.Complete || len(out.Coverage) != 0 {
		t.Fatal(got, out)
	}
	found := false
	for _, d := range out.Diagnostics {
		if d.Rule == "MD014" {
			found = true
			if d.FixAvailable || d.ByteRange != (result.ByteRange{Start: 6, End: 7}) {
				t.Fatal(d)
			}
		}
	}
	for _, c := range out.Coverage {
		if c.Rule == "MD014" {
			t.Fatal("implemented default lost coverage", c)
		}
	}
	if !found {
		t.Fatal(out.Diagnostics)
	}
	acceptance.ReadinessAssertEqual(t, "default catalog", before, acceptance.ReadinessState(t, root))
}

func TestCompiledPlatformMD014FullBlockFocusAndInheritedDenials(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprintf("global-%v", global), func(t *testing.T) {
			tc := md014NativeLiterals(t)["denied-disable-keeps-prompt"]
			root := acceptance.Project(t)
			parent := md014NativeConfig("gfm", tc, false)
			if global {
				parent = strings.Replace(parent, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			acceptance.PutFile(t, root, ".mdchk.yaml", parent)
			tc.Configuration.Allow = true
			acceptance.PutFile(t, root, "nested/.mdchk.yaml", md014NativeConfig("gfm", tc, false))
			acceptance.PutFile(t, root, "nested/doc.md", tc.Source)
			before := acceptance.ReadinessState(t, root)
			got := acceptance.MD014NativeRun(t, binary, filepath.Join(root, "nested"), "inspect", "doc.md", "--lines=3", "--format=json")
			out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "inspect")
			if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 35, End: 36}) {
				t.Fatal(got, out)
			}
			acceptance.ReadinessAssertEqual(t, "inherited denial", before, acceptance.ReadinessState(t, root))
		})
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			tc := md014NativeLiterals(t)["output-after-prompt-outside-inspect-focus"]
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", md014NativeConfig(flavor, tc, false))
			acceptance.PutFile(t, root, "doc.md", tc.Source)
			before := acceptance.ReadinessState(t, root)
			got := acceptance.MD014NativeRun(t, binary, root, "inspect", "doc.md", "--lines=2", "--format=json")
			out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "inspect")
			if got.Status != 0 || !out.Complete || len(out.Diagnostics)+len(out.SuppressedDiagnostics) != 0 {
				t.Fatal(got, out)
			}
			acceptance.ReadinessAssertEqual(t, "full block precedes focus", before, acceptance.ReadinessState(t, root))
		})
	}
}

// No public CLI flag requests inactive observations. Exercise the actual native
// inspection API separately, preserving that boundary instead of inventing one.
func TestNativeMD014RequestedInactiveInspectionAPI(t *testing.T) {
	t.Parallel()
	for _, observe := range []bool{false, true} {
		t.Run(fmt.Sprintf("observe-%v", observe), func(t *testing.T) {
			tc := md014NativeLiterals(t)["configured-off-not-demanded"]
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", md014NativeConfig("gfm", tc, false))
			acceptance.PutFile(t, root, "doc.md", tc.Source)
			before := acceptance.ReadinessState(t, root)
			r, err := engine.NewCatalogRegistry(rules.Registrations())
			if err != nil {
				t.Fatal(err)
			}
			s, err := inspection.Acquire(context.Background(), "doc.md", inspection.Options{WorkingDirectory: root, BoundaryDirectory: root, Registry: r, Capabilities: config.Capabilities{CommonMark: true, GFM: true, DirectiveProcessing: true}})
			if err != nil {
				t.Fatal(err)
			}
			a, err := inspection.Analyze(context.Background(), s, inspection.AnalyzeOptions{ObserveInactive: observe})
			if err != nil || !a.Result.Complete || a.Projection.Rules.Data == nil || len(a.Result.Diagnostics) != 0 {
				t.Fatal(a, err)
			}
			found := false
			for _, row := range *a.Projection.Rules.Data {
				if row.Rule == "MD014" {
					found = true
					want := "not-requested"
					if observe {
						want = "complete"
					}
					if row.ConfiguredEnabled || row.ActiveEvaluation != "skipped" || row.ObservationalEvaluation != want {
						t.Fatal(row)
					}
				}
			}
			want := 0
			if observe {
				want = 1
			}
			wire, err := json.Marshal(a.Result)
			if err != nil {
				t.Fatal(err)
			}
			validated := md014NativeAPIJSON(t, wire, filepath.Join(root, "doc.md"))
			count := 0
			for _, raw := range validated.SuppressedDiagnostics {
				var hidden result.SuppressedDiagnostic
				if err := json.Unmarshal(raw, &hidden); err != nil {
					t.Fatal(err)
				}
				if hidden.Diagnostic.Rule != "MD014" {
					continue
				}
				count++
				d := hidden.Diagnostic
				if d.Message != "Shell command has a dollar prompt without output." || d.ByteRange != (result.ByteRange{Start: 6, End: 7}) || d.Range != (result.Range{Start: result.Position{Line: 2, Column: 1}, End: result.Position{Line: 2, Column: 2}}) || d.FixAvailable || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: len(tc.Source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source)))}) {
					t.Fatal(d)
				}
			}
			if !found || count != want || string(s.Source().Bytes()) != tc.Source {
				t.Fatal(a.Result, found)
			}
			acceptance.ReadinessAssertEqual(t, "requested inactive inspection API", before, acceptance.ReadinessState(t, root))
		})
	}
}
