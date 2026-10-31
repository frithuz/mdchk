package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
)

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
	b, err := os.ReadFile("../../internal/rules/testdata/md014-v2.json")
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
	conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
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
	binary := buildConfigurationExecutable(t, "")
	cases := md014NativeLiterals(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, name := range []string{"published-dollar-prompt", "published-without-prompt", "default-enabled-omitted", "empty-option-mapping-keeps-defaults", "multiple-prompts-ignore-ascii-blanks", "prompt-tab-separator", "crlf-payload", "lone-cr-payload", "unicode-prefix-snapshot-geometry", "quote-list-owned-payload", "partial-tab-retained-payload-is-output", "custom-language-selected", "custom-list-replaces-defaults", "empty-languages-select-none", "duplicate-languages-single-finding", "info-numeric-entity-decoded-once", "info-no-second-decoding", "yaml-header-opaque", "toml-header-opaque", "incoming-raw-through-html-owner-match", "incoming-raw-through-html-owner-different", "form1-any4-owner-close-resumes", "image-local-raw-open-does-not-protect", "image-local-raw-close-does-not-end-outer", "known-opaque-remainder-is-owner-local", "unknown-required-html-context", "unknown-unselected-language-irrelevant", "empty-languages-ignore-unknown-context", "unknown-withholds-earlier-prompt", "md087-used-hidden-prompt", "md087-unused-mixed-output", "repeated-disables-last-owner-used", "md087-unknown-withheld-prefix-uncertain"} {
			t.Run(flavor+"/"+name, func(t *testing.T) {
				tc := cases[name]
				if tc.Name == "" {
					t.Fatal("missing literal", name)
				}
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md014NativeConfig(flavor, tc, false))
				putConfigurationFile(t, root, "doc.md", tc.Source)
				before := readinessState(t, root)
				got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
				after := readinessState(t, root)
				readinessAssertEqual(t, "MD014 literal lint", before, after)
				out := md014NativeJSON(t, []byte(got.Stdout), "lint")
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
	binary := buildConfigurationExecutable(t, "")
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
				root := configurationProject(t)
				cwd := root
				conf := md014NativeConfig(flavor, tc, false)
				if row.inherited {
					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					cwd = filepath.Join(root, "nested")
					tc.Configuration.Allow = true
					conf = md014NativeConfig(flavor, tc, false)
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
				got := md014NativeRun(t, binary, cwd, args...)
				got.Result = md014NativeJSON(t, []byte(got.Stdout), "inspect")
				var p inspection.Projection
				if err := json.Unmarshal(got.Result.Inspection, &p); err != nil {
					t.Fatal(err)
				}
				readinessAssertEqual(t, "MD014 inspect", before, readinessState(t, root))
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
	binary := buildConfigurationExecutable(t, "")
	tc := md014NativeLiterals(t)["published-dollar-prompt"]
	tc.Source = "```sh\n$ pwd\n```\nend"
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, dry := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/dry-%v", flavor, dry), func(t *testing.T) {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md014NativeConfig(flavor, tc, true))
				putConfigurationFile(t, root, "doc.md", tc.Source)
				for pass := 0; pass < 2; pass++ {
					before := readinessState(t, root)
					args := []string{"fix", "doc.md", "--format=json"}
					if dry {
						args = append(args, "--dry-run")
					}
					got := md014NativeRun(t, binary, root, args...)
					after := readinessState(t, root)
					out := md014NativeJSON(t, []byte(got.Stdout), "fix")
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
						readinessAssertEqual(t, "stable repeat", before, after)
					}
					if !dry && pass == 0 {
						if out.Changes[0].Disposition != "written" || readinessFile(t, out, "doc.md").WriteStatus != "written" {
							t.Fatal("first apply was not written", out)
						}
						if before["doc.md"].Info.Mode() != after["doc.md"].Info.Mode() {
							t.Fatal("replacement changed mode")
						}
						readinessAssertParentAfterSave(t, before["."], after["."])
						delete(before, "doc.md")
						delete(after, "doc.md")
						delete(before, ".")
						delete(after, ".")
						readinessAssertEqual(t, "only MD047 document changed", before, after)
					}
				}
			})
		}
	}
}

func TestCompiledPlatformMD014ProtectedEOFFix(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md014NativeLiterals(t)["published-dollar-prompt"]
	tc.Source = "```sh\n$ pwd"
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md014NativeConfig(flavor, tc, true))
			putConfigurationFile(t, root, "doc.md", tc.Source)
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, root, "fix", "doc.md", "--format=json")
			after := readinessState(t, root)
			out := md014NativeJSON(t, []byte(got.Stdout), "fix")
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
			if out.Diagnostics[0].Rule != "MD014" || out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 6, End: 7}) || out.Diagnostics[1].Rule != "MD047" || out.Diagnostics[1].ByteRange != (result.ByteRange{Start: len(tc.Source), End: len(tc.Source)}) {
				t.Fatal(out.Diagnostics)
			}
		})
	}
}

func TestCompiledPlatformMD014NoCGOAnalysisAndRefusal(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	binary := buildConfigurationExecutable(t, "")
	tc := md014NativeLiterals(t)["published-dollar-prompt"]
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
				if key != "GIT_CONFIG_NOSYSTEM" && key != "GIT_CONFIG_GLOBAL" {
					childEnv = append(childEnv, entry)
				}
			}
			childEnv = append(childEnv, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+gitGlobal)
			if _, err := os.Lstat(gitGlobal); !os.IsNotExist(err) {
				t.Fatal("fixture global Git input must be absent", err)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", md014NativeConfig(flavor, tc, false))
			putConfigurationFile(t, root, "doc.md", tc.Source)
			before := readinessState(t, root)
			got := md014NativeRunEnv(t, binary, root, childEnv, "lint", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "lint")
			if got.Status != 10 || !out.Complete {
				t.Fatal(got, out)
			}
			md014NativeDiagnostics(t, out, tc, tc.Source, "original")
			ins := md014NativeRunEnv(t, binary, root, childEnv, "inspect", "doc.md", "--format=json")
			ins.Result = md014NativeJSON(t, []byte(ins.Stdout), "inspect")
			var p inspection.Projection
			if err := json.Unmarshal(ins.Result.Inspection, &p); err != nil {
				t.Fatal(err)
			}
			if ins.Status != 0 || !ins.Result.Complete || len(ins.Result.Diagnostics) != 1 {
				t.Fatal(ins, p)
			}
			md014NativeDiagnostics(t, ins.Result, tc, tc.Source, "original")
			config := md014NativeRunEnv(t, binary, root, childEnv, "config", "validate", "--format=json")
			validated := md014NativeJSON(t, []byte(config.Stdout), "config validate")
			stats := validated.ConfigValidation
			if config.Status != 0 || !validated.Complete || len(validated.Errors) != 0 || stats == nil || !stats.ScanComplete || stats.DefinitionsChecked != 1 || stats.ValidDefinitions != 1 || stats.InvalidDefinitions != 0 || stats.SkippedDefinitions != 0 {
				t.Fatal(config, validated)
			}
			write := md014NativeRunEnv(t, binary, root, childEnv, "fix", "doc.md", "--format=json")
			refused := md014NativeJSON(t, []byte(write.Stdout), "fix")
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

func TestCompiledPlatformMD014DefaultsAndAdmission(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md014NativeLiterals(t)["published-dollar-prompt"]
	for _, setting := range []string{"OMITTED", "{}", "{enabled: true}", "{enabled: false}"} {
		t.Run(setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := md014NativeConfig("gfm", tc, false)
			old := "MD014: {enabled: true, allow_directives: true}"
			if setting == "OMITTED" {
				conf = strings.Replace(conf, "  "+old+"\n", "", 1)
			} else {
				conf = strings.Replace(conf, old, "MD014: "+setting, 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", tc.Source)
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "lint")
			exit, want := 0, 0
			if setting != "{enabled: false}" {
				exit, want = 10, 1
			}
			if got.Status != exit || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != want {
				t.Fatal(got, out)
			}
			readinessAssertEqual(t, "default admission", before, readinessState(t, root))
		})
	}
	for _, setting := range []string{"null", "[]", "true", "{enabled: null}", "{enabled: 'false'}", "{allow_directives: null}", "{allow_directives: 1}", "{enabled: false, unknown: true}", "{enabled: false, options: {}}", "{enabled: false, allow_directives: null}", "{enabled: false, allow_directives: 1}", "{enabled: false, allow_directives: 'true'}"} {
		t.Run("invalid/"+setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md014NativeConfig("gfm", tc, false), "MD014: {enabled: true, allow_directives: true}", "MD014: "+setting, 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, root, "lint", "missing.md", "--format=json")
			if got.Status != 12 {
				t.Fatal(got)
			}
			_ = md014NativeJSON(t, []byte(got.Stdout), "lint")
			readinessAssertEqual(t, "admission before acquisition", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD014FourReporters(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md014NativeLiterals(t)["published-dollar-prompt"]
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md014NativeConfig("gfm", tc, false))
	putConfigurationFile(t, root, "doc.md", tc.Source)
	before := readinessState(t, root)
	for _, format := range []string{"text", "json", "vscode", "github"} {
		t.Run(format, func(t *testing.T) {
			got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format="+format)
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
				out := md014NativeJSON(t, []byte(got.Stdout), "lint")
				md014NativeDiagnostics(t, out, tc, tc.Source, "original")
				contract, e := os.ReadFile("../../docs/contracts/results/result.schema.json")
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
				if md014SchemaCheck(schema, schema, invalid) == nil {
					t.Fatal("schema accepted extra field")
				}
				delete(invalid, "unexpected_md014_field")
				delete(invalid, "schema_version")
				if md014SchemaCheck(schema, schema, invalid) == nil {
					t.Fatal("schema accepted missing envelope")
				}

			}
		})
	}
	readinessAssertEqual(t, "four reporters", before, readinessState(t, root))
}

// Only CLI children receive fixture-owned absent Git inputs. Build processes,
// HOME, authentication and every other inherited environment entry are retained.
func md014NativeEnvironment(t *testing.T, cwd string) []string {
	t.Helper()
	root, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	absent := filepath.Join(root, "absent-gitconfig")
	if _, err := os.Lstat(absent); !os.IsNotExist(err) {
		t.Fatal("fixture Git input must be absent", err)
	}
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "GIT_CONFIG_NOSYSTEM" && key != "GIT_CONFIG_GLOBAL" {
			env = append(env, entry)
		}
	}
	return append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+absent)
}

func md014NativeRun(t *testing.T, binary, cwd string, args ...string) configurationRun {
	t.Helper()
	return md014NativeRunEnv(t, binary, cwd, md014NativeEnvironment(t, cwd), args...)
}

func md014NativeRunEnv(t *testing.T, binary, cwd string, env []string, args ...string) configurationRun {
	t.Helper()
	got := runConfigurationProcessEnv(t, binary, cwd, env, args...)
	var controls []string
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if key == "GIT_CONFIG_NOSYSTEM" || key == "GIT_CONFIG_GLOBAL" || key == "CGO_ENABLED" {
			controls = append(controls, entry)
		}
	}
	// Native go-test JSON retains actual argv, child-specific controls and raw
	// stdout/status. The shared launcher asserts actual stderr is empty.
	t.Logf("MD014_NATIVE binary=%q cwd=%q argv=%q inherited_env=preserved child_controls=%q status=%d stdout=%q stderr=%q", binary, cwd, args, controls, got.Status, got.Stdout, "")
	return got
}

// Evaluate the exact checked-in closed schema without introducing a hosted
// Python/jsonschema dependency. These are the assertion keywords present in the
// normative schema; unsupported future keywords fail closed.
func md014SchemaCheck(schema, root, data any) error {
	if b, ok := schema.(bool); ok {
		if b {
			return nil
		}
		return fmt.Errorf("false schema")
	}
	s, ok := schema.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid schema node")
	}
	for key := range s {
		switch key {
		case "$defs", "$schema", "$ref", "title", "description", "contentEncoding", "type", "const", "enum", "required", "properties", "additionalProperties", "items", "minimum", "maximum", "minItems", "maxItems", "minLength", "pattern", "uniqueItems", "allOf", "anyOf", "if", "then", "else", "not":
		default:
			return fmt.Errorf("unsupported schema keyword %q", key)
		}
	}
	if ref, ok := s["$ref"].(string); ok {
		if !strings.HasPrefix(ref, "#/") {
			return fmt.Errorf("nonlocal schema ref")
		}
		resolved := root
		for _, key := range strings.Split(ref[2:], "/") {
			key = strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~")
			object, ok := resolved.(map[string]any)
			if !ok {
				return fmt.Errorf("bad schema reference")
			}
			resolved, ok = object[key]
			if !ok {
				return fmt.Errorf("unknown schema ref")
			}
		}
		if err := md014SchemaCheck(resolved, root, data); err != nil {
			return err
		}
	}
	if expected, exists := s["const"]; exists && !reflect.DeepEqual(expected, data) {
		return fmt.Errorf("const mismatch")
	}
	if choices, ok := s["enum"].([]any); ok {
		found := false
		for _, choice := range choices {
			found = found || reflect.DeepEqual(choice, data)
		}
		if !found {
			return fmt.Errorf("enum mismatch")
		}
	}
	if typ, exists := s["type"]; exists {
		match := func(kind any) bool {
			switch kind {
			case "object":
				_, ok := data.(map[string]any)
				return ok
			case "array":
				_, ok := data.([]any)
				return ok
			case "string":
				_, ok := data.(string)
				return ok
			case "boolean":
				_, ok := data.(bool)
				return ok
			case "integer":
				n, ok := data.(float64)
				return ok && math.Trunc(n) == n
			case "number":
				_, ok := data.(float64)
				return ok
			case "null":
				return data == nil
			}
			return false
		}
		valid := match(typ)
		if list, ok := typ.([]any); ok {
			valid = false
			for _, kind := range list {
				valid = valid || match(kind)
			}
		}
		if !valid {
			return fmt.Errorf("type mismatch")
		}
	}
	if object, ok := data.(map[string]any); ok {
		if required, ok := s["required"].([]any); ok {
			for _, key := range required {
				if _, exists := object[key.(string)]; !exists {
					return fmt.Errorf("missing %v", key)
				}
			}
		}
		properties, _ := s["properties"].(map[string]any)
		for key, value := range object {
			if child, exists := properties[key]; exists {
				if err := md014SchemaCheck(child, root, value); err != nil {
					return fmt.Errorf("%s: %w", key, err)
				}
			} else if additional, exists := s["additionalProperties"]; exists {
				if err := md014SchemaCheck(additional, root, value); err != nil {
					return fmt.Errorf("additional %s: %w", key, err)
				}
			}
		}
	}
	if array, ok := data.([]any); ok {
		if n, ok := s["minItems"].(float64); ok && float64(len(array)) < n {
			return fmt.Errorf("minItems")
		}
		if n, ok := s["maxItems"].(float64); ok && float64(len(array)) > n {
			return fmt.Errorf("maxItems")
		}
		seen := map[string]bool{}
		for _, value := range array {
			if child, exists := s["items"]; exists {
				if err := md014SchemaCheck(child, root, value); err != nil {
					return err
				}
			}
			if s["uniqueItems"] == true {
				b, _ := json.Marshal(value)
				if seen[string(b)] {
					return fmt.Errorf("uniqueItems")
				}
				seen[string(b)] = true
			}
		}
	}
	if text, ok := data.(string); ok {
		if n, ok := s["minLength"].(float64); ok && float64(utf8.RuneCountInString(text)) < n {
			return fmt.Errorf("minLength")
		}
		if pattern, ok := s["pattern"].(string); ok {
			expression, err := regexp.Compile(pattern)
			if err != nil {
				return err
			}
			if !expression.MatchString(text) {
				return fmt.Errorf("pattern")
			}
		}
	}
	if n, ok := data.(float64); ok {
		if minimum, ok := s["minimum"].(float64); ok && n < minimum {
			return fmt.Errorf("minimum")
		}
		if maximum, ok := s["maximum"].(float64); ok && n > maximum {
			return fmt.Errorf("maximum")
		}
	}
	if children, ok := s["allOf"].([]any); ok {
		for _, child := range children {
			if err := md014SchemaCheck(child, root, data); err != nil {
				return err
			}
		}
	}
	if children, ok := s["anyOf"].([]any); ok {
		found := false
		for _, child := range children {
			found = found || md014SchemaCheck(child, root, data) == nil
		}
		if !found {
			return fmt.Errorf("anyOf")
		}
	}
	if child, exists := s["not"]; exists && md014SchemaCheck(child, root, data) == nil {
		return fmt.Errorf("not")
	}
	if condition, exists := s["if"]; exists {
		branch := "else"
		if md014SchemaCheck(condition, root, data) == nil {
			branch = "then"
		}
		if child, exists := s[branch]; exists {
			if err := md014SchemaCheck(child, root, data); err != nil {
				return err
			}
		}
	}
	return nil
}

func md014NativeJSON(t *testing.T, data []byte, operation string) result.Result {
	t.Helper()
	// CLI reports retain portable-path and newline-framing validation.
	validateConfigurationOperationJSON(t, data, operation)
	return md014ResultJSON(t, data, operation)
}

func md014NativeAPIJSON(t *testing.T, data []byte, path string) result.Result {
	t.Helper()
	out := md014ResultJSON(t, data, "inspect")
	// The raw API retains canonical native paths, including platform separators.
	if !filepath.IsAbs(path) || len(out.CheckedFiles) != 1 || out.CheckedFiles[0] != path || len(out.Files) != 1 || out.Files[0].Path != path {
		t.Fatal("native API target identity", path, out)
	}
	if len(out.Changes)+len(out.DeferredFixes)+len(out.Documents) != 0 {
		t.Fatal("native API inspection proposed corrections", out)
	}
	return out
}

func md014ResultJSON(t *testing.T, data []byte, operation string) result.Result {
	t.Helper()
	b, err := os.ReadFile("../../docs/contracts/results/result.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema, wire any
	if err = json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if err = md014SchemaCheck(schema, schema, wire); err != nil {
		t.Fatal("closed normative result schema", err, string(data))
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var out result.Result
	if err = decoder.Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.SchemaVersion != 1 || out.Operation != operation {
		t.Fatal("invalid result envelope", out)
	}
	// Cross-record semantic invariants independent of the schema's shape checks.
	object := wire.(map[string]any)
	ids := map[string]bool{}
	recordSets := []string{"diagnostics", "coverage", "changes", "deferred_fixes", "errors", "notes", "interruptions"}
	for _, key := range recordSets {
		for _, raw := range object[key].([]any) {
			record := raw.(map[string]any)
			id := record["id"].(string)
			if ids[id] {
				t.Fatal("duplicate record id", id)
			}
			ids[id] = true
		}
	}
	files := map[string]result.File{}
	for _, f := range out.Files {
		if _, exists := files[f.Path]; exists {
			t.Fatal("duplicate target", f.Path)
		}
		files[f.Path] = f
	}
	for _, path := range out.CheckedFiles {
		if _, exists := files[path]; !exists {
			t.Fatal("checked file missing outcome", path)
		}
	}
	check := func(d result.Diagnostic) {
		f, exists := files[d.Path]
		if !exists {
			t.Fatal("diagnostic without target", d)
		}
		want := f.InputSnapshot
		if d.Snapshot.View != "original" {
			want = f.ResultSnapshot
		}
		if want == nil || d.Snapshot != *want {
			t.Fatal("diagnostic snapshot/target disagreement", d, f)
		}
		if d.ByteRange.Start < 0 || d.ByteRange.End < d.ByteRange.Start || d.ByteRange.End > d.Snapshot.SizeBytes {
			t.Fatal("diagnostic byte bounds", d)
		}
	}
	for _, d := range out.Diagnostics {
		check(d)
	}
	for _, raw := range out.SuppressedDiagnostics {
		var h result.SuppressedDiagnostic
		if err = json.Unmarshal(raw, &h); err != nil {
			t.Fatal(err)
		}
		if ids[h.Diagnostic.ID] {
			t.Fatal("duplicate hidden id")
		}
		ids[h.Diagnostic.ID] = true
		check(h.Diagnostic)
	}
	for _, f := range out.Files {
		for _, id := range append(append(append([]string{}, f.ErrorIDs...), f.CoverageIDs...), f.ChangeIDs...) {
			if !ids[id] {
				t.Fatal("dangling file record", id)
			}
		}
	}
	for _, c := range out.Coverage {
		for _, id := range c.CauseIDs {
			if !ids[id] {
				t.Fatal("dangling cause", id)
			}
		}
	}
	return out
}

func TestCompiledPlatformMD014NoFix(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md014NativeLiterals(t)["published-dollar-prompt"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, dry := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry-%v", flavor, dry), func(t *testing.T) {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md014NativeConfig(flavor, tc, false))
				putConfigurationFile(t, root, "doc.md", tc.Source)
				before := readinessState(t, root)
				args := []string{"fix", "doc.md", "--format=json"}
				if dry {
					args = append(args, "--dry-run")
				}
				got := md014NativeRun(t, binary, root, args...)
				out := md014NativeJSON(t, []byte(got.Stdout), "fix")
				readinessAssertEqual(t, "MD014 NoFix", before, readinessState(t, root))
				if !dry && md038NativeRefusal(t, got, out) {
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
	binary := buildConfigurationExecutable(t, "")
	tc := md014NativeLiterals(t)["published-dollar-prompt"]
	for _, attribution := range []bool{false, true} {
		t.Run(fmt.Sprintf("md094-%v", attribution), func(t *testing.T) {
			root := configurationProject(t)
			conf := md014NativeConfig("gfm", tc, false)
			conf = strings.Replace(conf, "MD094: {enabled: false}", fmt.Sprintf("MD094: {enabled: %v}", attribution), 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", string(append([]byte("```sh\n$ pwd\n```\n"), 0xff)))
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "lint")
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
			readinessAssertEqual(t, "UTF8 admission before MD014", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD014AllInvalidConfigurationOperations(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	b, err := os.ReadFile("../../internal/rules/testdata/md014-v2.json")
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
			root := configurationProject(t)
			conf := strings.Replace(md014NativeConfig("gfm", tc, false), "MD014: {enabled: true, allow_directives: true}", "MD014: "+compact.String(), 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", tc.Source)
			before := readinessState(t, root)
			for _, op := range row.Operations {
				got := md014NativeRun(t, binary, root, op, "missing.md", "--format=json")
				out := md014NativeJSON(t, []byte(got.Stdout), op)
				if got.Status != 12 || len(out.CheckedFiles)+len(out.Files)+len(out.Diagnostics)+len(out.Changes) != 0 || len(out.Errors) == 0 {
					t.Fatal(got, out)
				}
				for _, e := range out.Errors {
					if e.Category != result.Configuration {
						t.Fatal(e)
					}
				}
			}
			got := md014NativeRun(t, binary, root, "inspect", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "inspect")
			var view inspection.Projection
			if e := json.Unmarshal(out.Inspection, &view); e != nil {
				t.Fatal(e)
			}
			if got.Status != 0 || out.Status != "inspected" || out.Complete || len(out.CheckedFiles) != 1 || view.File.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) || len(out.Errors) == 0 || view.Rules.Availability != "unavailable" {
				t.Fatal(got, out, view)
			}
			valid := md014NativeRun(t, binary, root, "config", "validate", "--format=json")
			validated := md014NativeJSON(t, []byte(valid.Stdout), "config validate")
			if valid.Status != 12 || validated.ConfigValidation == nil || !validated.ConfigValidation.ScanComplete || validated.ConfigValidation.InvalidDefinitions != 1 || len(validated.Coverage) != 0 {
				t.Fatal(valid, validated)
			}
			readinessAssertEqual(t, "operation-scoped invalid configuration", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD014DefaultCatalogCoverage(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, "doc.md", "```sh\n$ pwd\n```\n")
	before := readinessState(t, root)
	got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
	out := md014NativeJSON(t, []byte(got.Stdout), "lint")
	if got.Status != 15 || out.Complete || len(out.Coverage) == 0 {
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
	readinessAssertEqual(t, "default catalog", before, readinessState(t, root))
}

func TestCompiledPlatformMD014FullBlockFocusAndInheritedDenials(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprintf("global-%v", global), func(t *testing.T) {
			tc := md014NativeLiterals(t)["denied-disable-keeps-prompt"]
			root := configurationProject(t)
			parent := md014NativeConfig("gfm", tc, false)
			if global {
				parent = strings.Replace(parent, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", parent)
			tc.Configuration.Allow = true
			putConfigurationFile(t, root, "nested/.mdchk.yaml", md014NativeConfig("gfm", tc, false))
			putConfigurationFile(t, root, "nested/doc.md", tc.Source)
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, filepath.Join(root, "nested"), "inspect", "doc.md", "--lines=3", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "inspect")
			if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 35, End: 36}) {
				t.Fatal(got, out)
			}
			readinessAssertEqual(t, "inherited denial", before, readinessState(t, root))
		})
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			tc := md014NativeLiterals(t)["output-after-prompt-outside-inspect-focus"]
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md014NativeConfig(flavor, tc, false))
			putConfigurationFile(t, root, "doc.md", tc.Source)
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, root, "inspect", "doc.md", "--lines=2", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "inspect")
			if got.Status != 0 || !out.Complete || len(out.Diagnostics)+len(out.SuppressedDiagnostics) != 0 {
				t.Fatal(got, out)
			}
			readinessAssertEqual(t, "full block precedes focus", before, readinessState(t, root))
		})
	}
}

// No public CLI flag requests inactive observations. Exercise the actual native
// inspection API separately, preserving that boundary instead of inventing one.
func TestNativeMD014RequestedInactiveInspectionAPI(t *testing.T) {
	for _, observe := range []bool{false, true} {
		t.Run(fmt.Sprintf("observe-%v", observe), func(t *testing.T) {
			tc := md014NativeLiterals(t)["configured-off-not-demanded"]
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md014NativeConfig("gfm", tc, false))
			putConfigurationFile(t, root, "doc.md", tc.Source)
			before := readinessState(t, root)
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
			readinessAssertEqual(t, "requested inactive inspection API", before, readinessState(t, root))
		})
	}
}
