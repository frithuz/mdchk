package main

import (
	"bytes"
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

	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/result"
)

type md092NativeLiteral struct {
	Name, Source  string
	Configuration struct {
		Enabled     bool
		Omitted     bool `json:"enabled_omitted"`
		MD087       bool `json:"md087_enabled"`
		Allow       bool `json:"allow_directives"`
		Frontmatter string
	}
	Expected struct {
		Complete   bool
		Suppressed []result.Diagnostic
		Findings   []result.Diagnostic
		MD087      []result.Diagnostic `json:"md087_findings"`
	}
}

func md092NativeLiterals(t *testing.T) map[string]md092NativeLiteral {
	t.Helper()
	out := map[string]md092NativeLiteral{}
	for _, fixture := range []struct {
		path, hash string
		count      int
	}{
		{"../../internal/rules/testdata/md092.json", "1a60725172f8b8e5038604145766f2c9cd9c9ce00b3acb16d8b3ca078477bb25", 81},
		{"../../internal/rules/testdata/md092_raw_lifetime.json", "1d5ab8daa2ce6d3315e7439f36311d22a24881aac59b9d3f3831f81a2131df32", 6},
		{"../../internal/rules/testdata/md092_unknown.json", "486afc71751f0ea8c3ffd8a94f7bdfb09dced1731cd36a9ded5e0ce801827aed", 3},
	} {
		b, err := os.ReadFile(fixture.path)
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != fixture.hash {
			t.Fatal("root fixture identity", fixture.path, err)
		}
		var oracle struct{ Cases []md092NativeLiteral }
		if err = json.Unmarshal(b, &oracle); err != nil || len(oracle.Cases) != fixture.count {
			t.Fatal(err, len(oracle.Cases))
		}
		for _, tc := range oracle.Cases {
			if _, exists := out[tc.Name]; exists {
				t.Fatal("duplicate fixture", tc.Name)
			}
			out[tc.Name] = tc
		}
	}
	return out
}

func md092NativeConfig(flavor string, tc md092NativeLiteral, md047 bool) string {
	conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
	conf = strings.Replace(conf, "directives: {enabled: false}", "directives: {enabled: true}", 1)
	conf = strings.Replace(conf, "MD092: {enabled: false}", fmt.Sprintf("MD092: {enabled: %v, allow_directives: %v}", tc.Configuration.Enabled, tc.Configuration.Allow), 1)
	conf = strings.Replace(conf, "MD087: {enabled: false}", fmt.Sprintf("MD087: {enabled: %v}", tc.Configuration.MD087), 1)
	if md047 {
		conf = strings.Replace(conf, "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	}
	if tc.Configuration.Omitted {
		conf = strings.Replace(conf, "  "+fmt.Sprintf("MD092: {enabled: %v, allow_directives: %v}", tc.Configuration.Enabled, tc.Configuration.Allow)+"\n", "", 1)
	}
	if tc.Configuration.Frontmatter != "" && tc.Configuration.Frontmatter != "none" {
		conf += "frontmatter: {enabled: true, formats: [" + tc.Configuration.Frontmatter + "]}\n"
	}
	return conf
}

func md092NativeDiagnostics(t *testing.T, out result.Result, tc md092NativeLiteral, source, view string) {
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

func TestCompiledPlatformMD092LiteralLint(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	cases := md092NativeLiterals(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, name := range []string{"published-cycle", "default-on-omitted", "bare-open", "bare-close", "bare-ancestor", "bare-separator", "six-all", "arbitrary-suffix-all", "nested-open-saturation", "physical-close-before-quote-stripping", "later-comment-does-not-mask", "nested-quote-fence-physical-close-protected", "yaml-recognized-protected", "toml-recognized-protected", "unicode-labels-crlf", "unicode-labels-cr", "owner-changes-keep-active", "hidden-open-keeps-context", "hidden-close-ends-context", "permission-denied-disable", "configured-off-enable", "configured-off-enable-denied", "md087-used-hidden-open", "md087-unused-no-open", "md087-used-hidden-standalone-close", "incoming-raw-htmlblock-matched-closer", "incoming-raw-htmlblock-no-closer", "incoming-raw-htmlblock-different-closer", "htmlblock-raw-open-persists", "htmlblock-raw-open-close-resumes", "form1-any4-close-resumes", "unknown-owned-tag-withholds-earlier-opener", "md087-unknown-withheld-prefix-uncertain", "known-unclosed-opaque-keeps-earlier-opener"} {
			t.Run(flavor+"/"+name, func(t *testing.T) {
				tc := cases[name]
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md092NativeConfig(flavor, tc, false))
				putConfigurationFile(t, root, "doc.md", tc.Source)
				before := readinessState(t, root)
				got := md092NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
				after := readinessState(t, root)
				readinessAssertEqual(t, "MD092 literal lint", before, after)
				out := md092NativeJSON(t, []byte(got.Stdout), "lint")
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
					if coverage.Reason != result.ContextUnavailable || (coverage.Rule != "MD092" && coverage.Rule != "MD087") {
						t.Fatal(coverage)
					}
				}
				md092NativeDiagnostics(t, out, tc, tc.Source, "original")
			})
		}
	}
}

func TestCompiledPlatformMD092InspectionContextAndPermissions(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	cases := md092NativeLiterals(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, row := range []struct {
			name, focus string
			inherited   bool
		}{
			{"published-cycle", "", false}, {"published-cycle", "3", false}, {"hidden-open-keeps-context", "4", false}, {"md087-unknown-withheld-prefix-uncertain", "", false}, {"permission-denied-disable", "", true},
		} {
			t.Run(flavor+"/"+row.name+"/"+row.focus, func(t *testing.T) {
				tc := cases[row.name]
				root := configurationProject(t)
				cwd := root
				conf := md092NativeConfig(flavor, tc, false)
				if row.inherited {
					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					cwd = filepath.Join(root, "nested")
					tc.Configuration.Allow = true
					conf = md092NativeConfig(flavor, tc, false)
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
				got := md092NativeRun(t, binary, cwd, args...)
				got.Result = md092NativeJSON(t, []byte(got.Stdout), "inspect")
				var p inspection.Projection
				if err := json.Unmarshal(got.Result.Inspection, &p); err != nil {
					t.Fatal(err)
				}
				readinessAssertEqual(t, "MD092 inspect", before, readinessState(t, root))
				if got.Status != 0 || got.Result.Complete != tc.Expected.Complete {
					t.Fatal(got, p)
				}
				if row.name == "published-cycle" {
					want := 3
					if row.focus == "3" {
						want = 1
					}
					if len(got.Result.Diagnostics) != want {
						t.Fatal(got.Result.Diagnostics)
					}
				}
				if row.name == "hidden-open-keeps-context" {
					if len(got.Result.Diagnostics) != 1 || got.Result.Diagnostics[0].ByteRange != (result.ByteRange{Start: 70, End: 77}) {
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
					if e.Permission != "denied" || e.Effect != "blocked" || len(e.Provenance) == 0 || len(got.Result.Diagnostics) != 2 {
						t.Fatal(e, got.Result)
					}
				}
			})
		}
	}
}

func TestCompiledPlatformMD092MD047Composition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md092NativeLiterals(t)["bare-open"]
	tc.Source = "<<<<<<<\nend"
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, dry := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/dry-%v", flavor, dry), func(t *testing.T) {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md092NativeConfig(flavor, tc, true))
				putConfigurationFile(t, root, "doc.md", tc.Source)
				for pass := 0; pass < 2; pass++ {
					before := readinessState(t, root)
					args := []string{"fix", "doc.md", "--format=json"}
					if dry {
						args = append(args, "--dry-run")
					}
					got := md092NativeRun(t, binary, root, args...)
					after := readinessState(t, root)
					out := md092NativeJSON(t, []byte(got.Stdout), "fix")
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
					md092NativeDiagnostics(t, out, tc, tc.Source+"\n", view)
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

func TestCompiledPlatformMD092ProtectedEOFFix(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md092NativeLiterals(t)["bare-open"]
	tc.Source = "<<<<<<<\n\n```\nx"
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md092NativeConfig(flavor, tc, true))
			putConfigurationFile(t, root, "doc.md", tc.Source)
			before := readinessState(t, root)
			got := md092NativeRun(t, binary, root, "fix", "doc.md", "--format=json")
			after := readinessState(t, root)
			out := md092NativeJSON(t, []byte(got.Stdout), "fix")
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
			if out.Diagnostics[0].Rule != "MD092" || out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 0, End: 7}) || out.Diagnostics[1].Rule != "MD047" || out.Diagnostics[1].ByteRange != (result.ByteRange{Start: len(tc.Source), End: len(tc.Source)}) {
				t.Fatal(out.Diagnostics)
			}
		})
	}
}

func TestCompiledPlatformMD092NoCGOAnalysisAndRefusal(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	binary := buildConfigurationExecutable(t, "")
	tc := md092NativeLiterals(t)["bare-open"]
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
			putConfigurationFile(t, root, ".mdchk.yaml", md092NativeConfig(flavor, tc, false))
			putConfigurationFile(t, root, "doc.md", tc.Source)
			before := readinessState(t, root)
			got := md092NativeRunEnv(t, binary, root, childEnv, "lint", "doc.md", "--format=json")
			out := md092NativeJSON(t, []byte(got.Stdout), "lint")
			if got.Status != 10 || !out.Complete {
				t.Fatal(got, out)
			}
			md092NativeDiagnostics(t, out, tc, tc.Source, "original")
			ins := md092NativeRunEnv(t, binary, root, childEnv, "inspect", "doc.md", "--format=json")
			ins.Result = md092NativeJSON(t, []byte(ins.Stdout), "inspect")
			var p inspection.Projection
			if err := json.Unmarshal(ins.Result.Inspection, &p); err != nil {
				t.Fatal(err)
			}
			if ins.Status != 0 || !ins.Result.Complete || len(ins.Result.Diagnostics) != 1 {
				t.Fatal(ins, p)
			}
			md092NativeDiagnostics(t, ins.Result, tc, tc.Source, "original")
			config := md092NativeRunEnv(t, binary, root, childEnv, "config", "validate", "--format=json")
			validated := md092NativeJSON(t, []byte(config.Stdout), "config validate")
			stats := validated.ConfigValidation
			if config.Status != 0 || !validated.Complete || len(validated.Errors) != 0 || stats == nil || !stats.ScanComplete || stats.DefinitionsChecked != 1 || stats.ValidDefinitions != 1 || stats.InvalidDefinitions != 0 || stats.SkippedDefinitions != 0 {
				t.Fatal(config, validated)
			}
			write := md092NativeRunEnv(t, binary, root, childEnv, "fix", "doc.md", "--format=json")
			refused := md092NativeJSON(t, []byte(write.Stdout), "fix")
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

func TestCompiledPlatformMD092DefaultsAndAdmission(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md092NativeLiterals(t)["bare-open"]
	for _, setting := range []string{"OMITTED", "{}", "{enabled: true}", "{enabled: false}"} {
		t.Run(setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := md092NativeConfig("gfm", tc, false)
			old := "MD092: {enabled: true, allow_directives: true}"
			if setting == "OMITTED" {
				conf = strings.Replace(conf, "  "+old+"\n", "", 1)
			} else {
				conf = strings.Replace(conf, old, "MD092: "+setting, 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", tc.Source)
			before := readinessState(t, root)
			got := md092NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
			out := md092NativeJSON(t, []byte(got.Stdout), "lint")
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
			conf := strings.Replace(md092NativeConfig("gfm", tc, false), "MD092: {enabled: true, allow_directives: true}", "MD092: "+setting, 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			before := readinessState(t, root)
			got := md092NativeRun(t, binary, root, "lint", "missing.md", "--format=json")
			if got.Status != 12 {
				t.Fatal(got)
			}
			_ = md092NativeJSON(t, []byte(got.Stdout), "lint")
			readinessAssertEqual(t, "admission before acquisition", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD092FourReporters(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md092NativeLiterals(t)["bare-open"]
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md092NativeConfig("gfm", tc, false))
	putConfigurationFile(t, root, "doc.md", tc.Source)
	before := readinessState(t, root)
	for _, format := range []string{"text", "json", "vscode", "github"} {
		t.Run(format, func(t *testing.T) {
			got := md092NativeRun(t, binary, root, "lint", "doc.md", "--format="+format)
			if got.Status != 10 || !strings.Contains(got.Stdout, "MD092") || !strings.Contains(got.Stdout, "Unresolved version-control conflict marker.") {
				t.Fatal(got)
			}
			switch format {
			case "text":
				if !strings.Contains(got.Stdout, "doc.md:1:1 MD092:") {
					t.Fatal(got)
				}
			case "vscode":
				if !strings.Contains(got.Stdout, "doc.md:1:1: error: MD092:") {
					t.Fatal(got)
				}
			case "github":
				if strings.Count(got.Stdout, "::error file=doc.md,line=1,title=MD092::") != 1 {
					t.Fatal(got)
				}
			}
			if format == "json" {
				out := md092NativeJSON(t, []byte(got.Stdout), "lint")
				md092NativeDiagnostics(t, out, tc, tc.Source, "original")
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
				invalid["unexpected_md092_field"] = true
				if md092SchemaCheck(schema, schema, invalid) == nil {
					t.Fatal("schema accepted extra field")
				}
				delete(invalid, "unexpected_md092_field")
				delete(invalid, "schema_version")
				if md092SchemaCheck(schema, schema, invalid) == nil {
					t.Fatal("schema accepted missing envelope")
				}

			}
		})
	}
	readinessAssertEqual(t, "four reporters", before, readinessState(t, root))
}

// Only CLI children receive fixture-owned absent Git inputs. Build processes,
// HOME, authentication and every other inherited environment entry are retained.
func md092NativeEnvironment(t *testing.T, cwd string) []string {
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

func md092NativeRun(t *testing.T, binary, cwd string, args ...string) configurationRun {
	t.Helper()
	return md092NativeRunEnv(t, binary, cwd, md092NativeEnvironment(t, cwd), args...)
}

func md092NativeRunEnv(t *testing.T, binary, cwd string, env []string, args ...string) configurationRun {
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
	t.Logf("MD092_NATIVE binary=%q cwd=%q argv=%q inherited_env=preserved child_controls=%q status=%d stdout=%q stderr=%q", binary, cwd, args, controls, got.Status, got.Stdout, "")
	return got
}

// Evaluate the exact checked-in closed schema without introducing a hosted
// Python/jsonschema dependency. These are the assertion keywords present in the
// normative schema; unsupported future keywords fail closed.
func md092SchemaCheck(schema, root, data any) error {
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
		if err := md092SchemaCheck(resolved, root, data); err != nil {
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
				if err := md092SchemaCheck(child, root, value); err != nil {
					return fmt.Errorf("%s: %w", key, err)
				}
			} else if additional, exists := s["additionalProperties"]; exists {
				if err := md092SchemaCheck(additional, root, value); err != nil {
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
				if err := md092SchemaCheck(child, root, value); err != nil {
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
			if err := md092SchemaCheck(child, root, data); err != nil {
				return err
			}
		}
	}
	if children, ok := s["anyOf"].([]any); ok {
		found := false
		for _, child := range children {
			found = found || md092SchemaCheck(child, root, data) == nil
		}
		if !found {
			return fmt.Errorf("anyOf")
		}
	}
	if child, exists := s["not"]; exists && md092SchemaCheck(child, root, data) == nil {
		return fmt.Errorf("not")
	}
	if condition, exists := s["if"]; exists {
		branch := "else"
		if md092SchemaCheck(condition, root, data) == nil {
			branch = "then"
		}
		if child, exists := s[branch]; exists {
			if err := md092SchemaCheck(child, root, data); err != nil {
				return err
			}
		}
	}
	return nil
}

func md092NativeJSON(t *testing.T, data []byte, operation string) result.Result {
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
	if err = md092SchemaCheck(schema, schema, wire); err != nil {
		t.Fatal("closed normative result schema", err, string(data))
	}
	out := validateConfigurationOperationJSON(t, data, operation)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var strict result.Result
	if err = decoder.Decode(&strict); err != nil {
		t.Fatal(err)
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

func TestCompiledPlatformMD092NoFix(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md092NativeLiterals(t)["bare-open"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, dry := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry-%v", flavor, dry), func(t *testing.T) {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md092NativeConfig(flavor, tc, false))
				putConfigurationFile(t, root, "doc.md", tc.Source)
				before := readinessState(t, root)
				args := []string{"fix", "doc.md", "--format=json"}
				if dry {
					args = append(args, "--dry-run")
				}
				got := md092NativeRun(t, binary, root, args...)
				out := md092NativeJSON(t, []byte(got.Stdout), "fix")
				readinessAssertEqual(t, "MD092 NoFix", before, readinessState(t, root))
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
				md092NativeDiagnostics(t, out, tc, tc.Source, view)
			})
		}
	}
}

func TestCompiledPlatformMD092InvalidUTF8Admission(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md092NativeLiterals(t)["bare-open"]
	for _, attribution := range []bool{false, true} {
		t.Run(fmt.Sprintf("md094-%v", attribution), func(t *testing.T) {
			root := configurationProject(t)
			conf := md092NativeConfig("gfm", tc, false)
			conf = strings.Replace(conf, "MD094: {enabled: false}", fmt.Sprintf("MD094: {enabled: %v}", attribution), 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", string(append([]byte("<<<<<<<\n"), 0xff)))
			before := readinessState(t, root)
			got := md092NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
			out := md092NativeJSON(t, []byte(got.Stdout), "lint")
			if got.Status != 13 || out.Complete || len(out.Errors) != 1 || len(out.Diagnostics)+len(out.SuppressedDiagnostics)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 || len(out.Coverage) != 1 || out.Coverage[0].Reason != "decoding-failed" {
				t.Fatal(got, out)
			}
			e := out.Errors[0]
			wantRule := ""
			if attribution {
				wantRule = "MD094"
			}
			if e.Category != result.IO || e.Code != "invalid-utf8" || e.Rule != wantRule || e.ByteOffset == nil || *e.ByteOffset != 8 {
				t.Fatal(e)
			}
			readinessAssertEqual(t, "UTF8 admission before MD092", before, readinessState(t, root))
		})
	}
}
