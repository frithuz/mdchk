package main

import (
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/result"
)

type md070NativeLiteral struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Options      struct {
		Enabled     *bool
		Omitted     bool `json:"enabled_omitted"`
		Empty       bool `json:"empty_options"`
		Frontmatter string
	}
	Findings []struct {
		Bytes, Start, End [2]int
		Message           string
	}
}

func md070NativeLiterals(t *testing.T) map[string]md070NativeLiteral {
	t.Helper()
	b, e := os.ReadFile("../../internal/rules/testdata/md070.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "9e3ca955c4cca090d3f80175d6195cf2779e24578520f64c3a2c79bab365f4be" {
		t.Fatal(e, "oracle identity")
	}
	var v struct{ Cases []md070NativeLiteral }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 88 {
		t.Fatal(e)
	}
	out := map[string]md070NativeLiteral{}
	for _, tc := range v.Cases {
		if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
			t.Fatal("literal source identity", tc.Name)
		}
		if _, ok := out[tc.Name]; ok {
			t.Fatal("duplicate")
		}
		out[tc.Name] = tc
	}
	return out
}

func TestCompiledPlatformMD070InspectionFocus(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	base := md070NativeLiterals(t)["inner-unclosed"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, focus := range []string{"", "2", "3", "6"} {
			t.Run(flavor+"/line-"+focus, func(t *testing.T) {
				tc := base
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md070NativeConfig(flavor, tc))
				putConfigurationFile(t, root, "doc.md", tc.Source)
				before := readinessState(t, root)
				args := []string{"inspect", "doc.md", "--format=json"}
				if focus != "" {
					args = append(args, "--lines="+focus)
				}
				got := md014NativeRun(t, binary, root, args...)
				out := md014NativeJSON(t, []byte(got.Stdout), "inspect")
				if got.Status != 0 {
					t.Fatal(got)
				}
				if focus == "3" || focus == "6" {
					tc.Findings = nil
				}
				md070NativeAssert(t, out, tc, "original")
				readinessAssertEqual(t, "full-region recognition before focus", before, readinessState(t, root))
			})
		}
	}
}

func TestCompiledPlatformMD070ManualAndMD047Preview(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	base := md070NativeLiterals(t)["inner-unclosed"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, composition := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/composition-%v", flavor, composition), func(t *testing.T) {
				tc := base
				tc.Source += "\n\nTail"
				conf := md070NativeConfig(flavor, tc)
				if composition {
					conf = strings.Replace(conf, "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
				}
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				putConfigurationFile(t, root, "doc.md", tc.Source)
				before := readinessState(t, root)
				got := md014NativeRun(t, binary, root, "fix", "doc.md", "--dry-run", "--format=json")
				out := md014NativeJSON(t, []byte(got.Stdout), "fix")
				changes := 0
				if composition {
					changes = 1
					tc.Source += "\n"
				}
				if got.Status != 10 || len(out.Changes) != changes {
					t.Fatal(got, out)
				}
				for _, change := range out.Changes {
					if len(change.Rules) != 1 || change.Rules[0] != "MD047" {
						t.Fatal(change)
					}
				}
				md070NativeAssert(t, out, tc, "candidate")
				readinessAssertEqual(t, "manual-only MD070 plus safe prose EOF", before, readinessState(t, root))
			})
		}
	}
}

func TestCompiledPlatformMD070ClosingFenceEOFRefusal(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	base := md070NativeLiterals(t)["inner-unclosed"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			tc := base
			tc.Source += "\n\n~~~\nx\n~~~"
			root := configurationProject(t)
			conf := strings.Replace(md070NativeConfig(flavor, tc), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", tc.Source)
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, root, "fix", "doc.md", "--dry-run", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "fix")
			if got.Status != 10 || !out.Complete || len(out.Changes) != 0 || len(out.DeferredFixes) != 1 || len(out.Diagnostics) != 2 {
				t.Fatal(got, out)
			}
			if out.DeferredFixes[0].Rule != "MD047" || out.DeferredFixes[0].Reason != "candidate-preservation-failed" {
				t.Fatal(out.DeferredFixes)
			}
			var manual []result.Diagnostic
			terminal := 0
			for _, d := range out.Diagnostics {
				if d.Rule == "MD070" {
					manual = append(manual, d)
				} else if d.Rule == "MD047" {
					terminal++
					if d.FixAvailable || d.ByteRange != (result.ByteRange{Start: len(tc.Source), End: len(tc.Source)}) {
						t.Fatal(d)
					}
				} else {
					t.Fatal(d)
				}
			}
			if terminal != 1 {
				t.Fatal(out)
			}
			out.Diagnostics = manual
			out.DeferredFixes = nil
			md070NativeAssert(t, out, tc, "candidate")
			readinessAssertEqual(t, "protected terminal fence remains manual", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD070DirectiveControls(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	base := md070NativeLiterals(t)["inner-unclosed"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"used", "denied", "unused", "enable", "enable-denied", "unknown"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				tc := base
				source := "<!-- mdchk:disable MD070 -->\n" + tc.Source
				conf := strings.Replace(md070NativeConfig(flavor, tc), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				conf = strings.Replace(conf, "MD087: {enabled: false}", "MD087: {enabled: true}", 1)
				if mode == "unused" {
					source = "<!-- mdchk:disable MD070 -->\n" + md070NativeLiterals(t)["different-character"].Source
				}
				if mode == "enable" || mode == "enable-denied" {
					source = "<!-- mdchk:enable MD070 -->\n" + tc.Source
					conf = strings.Replace(conf, "MD070: {enabled: true}", "MD070: {enabled: false}", 1)
				}
				if mode == "denied" {
					conf = strings.Replace(conf, "MD070: {enabled: true}", "MD070: {enabled: true, allow_directives: false}", 1)
				}
				if mode == "enable-denied" {
					conf = strings.Replace(conf, "MD070: {enabled: false}", "MD070: {enabled: false, allow_directives: false}", 1)
				}
				if mode == "unknown" {
					source += "\n\n<div>\n<x title=\"\n\n" + tc.Source
				}
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				putConfigurationFile(t, root, "doc.md", source)
				before := readinessState(t, root)
				got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
				out := md014NativeJSON(t, []byte(got.Stdout), "lint")
				visible, hidden, coverage, status := 0, 1, 0, 0
				switch mode {
				case "denied", "enable":
					visible, hidden, status = 1, 0, 10
				case "enable-denied":
					hidden = 0
				case "unused":
					visible, hidden, status = 1, 0, 10
				case "unknown":
					hidden, coverage, status = 0, 2, 15
				}
				if got.Status != status || out.Complete == (mode == "unknown") || len(out.Diagnostics) != visible || len(out.SuppressedDiagnostics) != hidden || len(out.Coverage) != coverage || len(out.Errors)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(got, out)
				}
				for _, d := range out.Diagnostics {
					if mode == "unused" {
						if d.Rule != "MD087" {
							t.Fatal(d)
						}
					} else if d.Rule != "MD070" || d.Message != base.Findings[0].Message || d.Range != (result.Range{Start: result.Position{Line: 3, Column: 1}, End: result.Position{Line: 3, Column: 4}}) || d.FixAvailable {
						t.Fatal(d)
					}
				}
				for _, wire := range out.SuppressedDiagnostics {
					var h result.SuppressedDiagnostic
					if err := json.Unmarshal(wire, &h); err != nil {
						t.Fatal(err)
					}
					if h.Diagnostic.Rule != "MD070" || h.Diagnostic.ByteRange != (result.ByteRange{Start: 52, End: 55}) || h.SuppressedBy.Kind != "directive" {
						t.Fatal(h)
					}
				}
				for _, c := range out.Coverage {
					if c.Reason != result.ContextUnavailable {
						t.Fatal(c)
					}
				}
				readinessAssertEqual(t, "MD070 directive controls", before, readinessState(t, root))
			})
		}
	}
}
func md070NativeConfig(flavor string, tc md070NativeLiteral) string {
	c := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
	value := "{enabled: true}"
	if tc.Options.Enabled != nil {
		value = fmt.Sprintf("{enabled: %v}", *tc.Options.Enabled)
	}
	if tc.Options.Empty {
		value = "{}"
	}
	c = strings.Replace(c, "MD070: {enabled: false}", "MD070: "+value, 1)
	if tc.Options.Omitted && !tc.Options.Empty {
		c = strings.Replace(c, "  MD070: "+value+"\n", "", 1)
	}
	if tc.Options.Frontmatter != "" {
		c += "frontmatter: {enabled: true, formats: [" + tc.Options.Frontmatter + "]}\n"
	}
	return c
}
func md070NativeAssert(t *testing.T, out result.Result, tc md070NativeLiteral, view string) {
	t.Helper()
	if !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.DeferredFixes)+len(out.Documents) != 0 || len(out.Diagnostics) != len(tc.Findings) {
		t.Fatal(out)
	}
	for i, d := range out.Diagnostics {
		w := tc.Findings[i]
		if d.Rule != "MD070" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] || d.Snapshot != (result.Snapshot{View: view, SizeBytes: len(tc.Source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source)))}) {
			t.Fatal(d, w)
		}
	}
}
func TestCompiledPlatformMD070LiteralLint(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	cases := md070NativeLiterals(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, name := range md070NativeNames(cases) {
			t.Run(flavor+"/"+name, func(t *testing.T) {
				tc, ok := cases[name]
				if !ok {
					t.Fatal(name)
				}
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md070NativeConfig(flavor, tc))
				putConfigurationFile(t, root, "doc.md", tc.Source)
				before := readinessState(t, root)
				got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
				out := md014NativeJSON(t, []byte(got.Stdout), "lint")
				want := 0
				if len(tc.Findings) > 0 {
					want = 10
				}
				if got.Status != want || len(out.Changes) != 0 {
					t.Fatal(got, out)
				}
				md070NativeAssert(t, out, tc, "original")
				readinessAssertEqual(t, "MD070 lint", before, readinessState(t, root))
			})
		}
	}
}

type md070InvalidLiteral struct {
	Name  string
	Value json.RawMessage `json:"md070_value"`
}

func md070NativeInvalid(t *testing.T) []md070InvalidLiteral {
	t.Helper()
	b, e := os.ReadFile("../../internal/rules/testdata/md070.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "9e3ca955c4cca090d3f80175d6195cf2779e24578520f64c3a2c79bab365f4be" {
		t.Fatal(e)
	}
	var v struct {
		Invalid []md070InvalidLiteral `json:"invalid_configurations"`
	}
	if e = json.Unmarshal(b, &v); e != nil || len(v.Invalid) != 13 {
		t.Fatal(e)
	}
	for i := range v.Invalid {
		// JSON indentation is legal YAML flow whitespace, but compact to one line.
		var value any
		if e = json.Unmarshal(v.Invalid[i].Value, &value); e != nil {
			t.Fatal(e)
		}
		encoded, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		v.Invalid[i].Value = json.RawMessage(encoded)
	}
	return v.Invalid
}

func TestCompiledPlatformMD070InvalidAdmission(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md070NativeLiterals(t)["page-bad"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, command := range []string{"lint", "fix"} {
			for _, row := range md070NativeInvalid(t) {
				value := string(row.Value)
				t.Run(flavor+"/"+command+"/"+row.Name, func(t *testing.T) {
					root := configurationProject(t)
					conf := strings.Replace(md070NativeConfig(flavor, tc), "MD070: {enabled: true}", "MD070: "+value, 1)
					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					before := readinessState(t, root)
					got := md014NativeRun(t, binary, root, command, "missing.md", "--format=json")
					out := md014NativeJSON(t, []byte(got.Stdout), command)
					if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles)+len(out.Diagnostics)+len(out.Changes) != 0 {
						t.Fatal(got, out)
					}
					for _, e := range out.Errors {
						if e.Category != result.Configuration {
							t.Fatal(e)
						}
					}
					readinessAssertEqual(t, "MD070 invalid preflight", before, readinessState(t, root))
				})
			}
		}
	}
}
func TestCompiledPlatformMD070NoCGO(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	binary := buildConfigurationExecutable(t, "")
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		t.Fatal("read actual compiled binary metadata", err)
	}
	found := false
	for _, setting := range info.Settings {
		if setting.Key == "CGO_ENABLED" {
			if found || setting.Value != "0" {
				t.Fatal("actual binary CGO mismatch", info.Settings)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("actual binary lacks CGO_ENABLED metadata", info.Settings)
	}
	t.Log("actual compiled binary metadata: CGO_ENABLED=0")

	tc := md070NativeLiterals(t)["page-bad"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md070NativeConfig(flavor, tc))
			putConfigurationFile(t, root, "doc.md", tc.Source)
			before := readinessState(t, root)
			for _, command := range []string{"lint", "inspect"} {
				got := md014NativeRun(t, binary, root, command, "doc.md", "--format=json")
				out := md014NativeJSON(t, []byte(got.Stdout), command)
				want := 10
				if command == "inspect" {
					want = 0
				}
				if got.Status != want {
					t.Fatal(got, out)
				}
				md070NativeAssert(t, out, tc, "original")
			}
			validated := md014NativeRun(t, binary, root, "config", "validate", "--format=json")
			config := md014NativeJSON(t, []byte(validated.Stdout), "config validate")
			if validated.Status != 0 || !config.Complete || len(config.Errors) != 0 || config.ConfigValidation == nil || !config.ConfigValidation.ScanComplete || config.ConfigValidation.ValidDefinitions != 1 {
				t.Fatal(validated, config)
			}
			preview := md014NativeRun(t, binary, root, "fix", "doc.md", "--dry-run", "--format=json")
			previewResult := md014NativeJSON(t, []byte(preview.Stdout), "fix")
			if preview.Status != 10 || len(previewResult.Changes) != 0 {
				t.Fatal(preview, previewResult)
			}
			md070NativeAssert(t, previewResult, tc, "candidate")
			got := md014NativeRun(t, binary, root, "fix", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "fix")
			if !md038NativeRefusal(t, got, out) || len(out.Changes)+len(out.Documents)+len(out.Diagnostics) != 0 {
				t.Fatal(got, out)
			}
			readinessAssertEqual(t, "CGO0 analysis and write refusal", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD070ConfiguredOffInspection(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md070NativeLiterals(t)["explicit-disabled"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md070NativeConfig(flavor, tc))
			putConfigurationFile(t, root, "doc.md", tc.Source)
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, root, "inspect", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "inspect")
			if got.Status != 0 || len(out.SuppressedDiagnostics) != 0 {
				t.Fatal(got, out)
			}
			md070NativeAssert(t, out, tc, "original")
			var projection inspection.Projection
			if err := json.Unmarshal(out.Inspection, &projection); err != nil || projection.Rules.Data == nil {
				t.Fatal(err, projection)
			}
			found := false
			for _, state := range *projection.Rules.Data {
				if state.Rule == "MD070" {
					found = true
					if state.ConfiguredEnabled || state.ActiveEvaluation != "skipped" || state.ObservationalEvaluation != "not-requested" {
						t.Fatal(state)
					}
				}
			}
			if !found {
				t.Fatal("missing MD070 evaluation")
			}
			readinessAssertEqual(t, "native configured-off observation is not requested", before, readinessState(t, root))
		})
	}
}

func md070NativeNames(cases map[string]md070NativeLiteral) []string {
	names := make([]string, 0, len(cases))
	for name := range cases {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestCompiledPlatformMD070ReportersAndManualRefusal(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md070NativeLiterals(t)["page-bad"]
	for _, format := range []string{"text", "json", "vscode", "github"} {
		t.Run(format, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md070NativeConfig("commonmark", tc))
			putConfigurationFile(t, root, "doc.md", tc.Source)
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format="+format)
			if got.Status != 10 || !strings.Contains(got.Stdout, "MD070") || !strings.Contains(got.Stdout, tc.Findings[0].Message) {
				t.Fatal(got)
			}
			if format == "json" {
				md070NativeAssert(t, md014NativeJSON(t, []byte(got.Stdout), "lint"), tc, "original")
			}
			got = md014NativeRun(t, binary, root, "fix", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "fix")
			if got.Status == 13 {
				if !md038NativeRefusal(t, got, out) || len(out.Changes)+len(out.Documents)+len(out.Diagnostics) != 0 {
					t.Fatal(got, out)
				}
			} else {
				if got.Status != 10 || len(out.Changes) != 0 {
					t.Fatal(got, out)
				}
				md070NativeAssert(t, out, tc, "saved")
			}
			readinessAssertEqual(t, "manual-only apply or unsupported refusal", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD070LiteralInspection(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	cases := md070NativeLiterals(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, name := range md070NativeNames(cases) {
			t.Run(flavor+"/"+name, func(t *testing.T) {
				tc, ok := cases[name]
				if !ok {
					t.Fatal(name)
				}
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md070NativeConfig(flavor, tc))
				putConfigurationFile(t, root, "doc.md", tc.Source)
				before := readinessState(t, root)
				got := md014NativeRun(t, binary, root, "inspect", "doc.md", "--format=json")
				out := md014NativeJSON(t, []byte(got.Stdout), "inspect")
				want := 0

				if got.Status != want || len(out.Changes) != 0 {
					t.Fatal(got, out)
				}
				md070NativeAssert(t, out, tc, "original")
				readinessAssertEqual(t, "MD070 lint", before, readinessState(t, root))
			})
		}
	}
}
func TestCompiledPlatformMD070InheritedDirectiveDenial(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprintf("global-%v", global), func(t *testing.T) {
			root := configurationProject(t)
			child := strings.Replace(md070NativeConfig("gfm", md070NativeLiteral{}), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			parent := strings.Replace(child, "MD070: {enabled: true}", "MD070: {enabled: true, allow_directives: false}", 1)
			if global {
				parent = strings.Replace(child, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", parent)
			putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
			source := "<!-- mdchk:disable MD070 -->\n<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->"
			putConfigurationFile(t, root, "nested/doc.md", source)
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, filepath.Join(root, "nested"), "inspect", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "inspect")
			if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 1 || len(out.SuppressedDiagnostics)+len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(got, out)
			}
			for _, d := range out.Diagnostics {
				if d.Rule != "MD070" || d.FixAvailable {
					t.Fatal(d)
				}
			}
			var projection inspection.Projection
			if e := json.Unmarshal(out.Inspection, &projection); e != nil {
				t.Fatal(e)
			}
			if projection.Directives.Data == nil || len(*projection.Directives.Data) != 1 {
				t.Fatal(projection.Directives)
			}
			directive := (*projection.Directives.Data)[0]
			if global {
				// Global denial stops before grammar interpretation; the confirmed
				// candidate is inactive and has no per-rule permission effects.
				if directive.State != "inactive" || directive.Operation != "" || len(directive.Effects) != 0 {
					t.Fatal(directive)
				}
				if len(out.Notes) != 1 {
					t.Fatal(out.Notes)
				}
				var note result.Note
				if err := json.Unmarshal(out.Notes[0], &note); err != nil || note.Code != "directives-disabled" {
					t.Fatal(err, note)
				}
				configuration := projection.Configuration.Data
				if configuration == nil {
					t.Fatal(projection.Configuration)
				}
				effective, ok := configuration.Effective["directives"].(map[string]any)
				selected, selectedOK := configuration.Effective["selected_permissions"].(map[string]any)
				if !ok || effective["enabled"] != false || !selectedOK || selected["/directives/enabled"] != true {
					t.Fatal(configuration)
				}
				inherited := 0
				for _, origin := range configuration.Provenance {
					if origin.Pointer == "/directives/enabled" && origin.Inherited {
						inherited++
						if origin.SourceKind != "ancestor-config" || origin.Path != "../.mdchk.yaml" || origin.Range == nil {
							t.Fatal(origin)
						}
					}
				}
				if inherited != 1 {
					t.Fatal(configuration.Provenance)
				}
			} else {
				if len(directive.Effects) != 1 {
					t.Fatal(directive)
				}
				effect := directive.Effects[0]
				if effect.Permission != "denied" || effect.Effect != "blocked" || len(effect.Provenance) == 0 {
					t.Fatal(effect)
				}
			}
			if projection.RawCandidates.Data == nil || len(*projection.RawCandidates.Data) != 1 {
				t.Fatal(projection.RawCandidates)
			}
			raw := (*projection.RawCandidates.Data)[0]
			comment := strings.SplitN(source, "\n", 2)[0]
			if raw.Context != "confirmed" || raw.DirectiveID != directive.ID || raw.ByteRange != (result.ByteRange{Start: 0, End: len(comment)}) || raw.RawBase64 != base64.StdEncoding.EncodeToString([]byte(comment)) {
				t.Fatal(raw)
			}
			readinessAssertEqual(t, "MD070 inherited permission cannot be relaxed", before, readinessState(t, root))
		})
	}
}
