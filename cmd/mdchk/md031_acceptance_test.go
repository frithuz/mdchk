package main

import (
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/result"
)

type md031NativeLiteral struct {
	Name, Source string
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

func md031NativeLiterals(t *testing.T) map[string]md031NativeLiteral {
	t.Helper()
	b, e := os.ReadFile("../../internal/rules/testdata/md031.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "8972795f62e028ac78f3574b4723572988300140e007474bca298e3d2f796876" {
		t.Fatal(e, "oracle identity")
	}
	var v struct{ Cases []md031NativeLiteral }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 57 {
		t.Fatal(e)
	}
	out := map[string]md031NativeLiteral{}
	for _, tc := range v.Cases {
		if _, ok := out[tc.Name]; ok {
			t.Fatal("duplicate")
		}
		out[tc.Name] = tc
	}
	return out
}
func md031NativeConfig(flavor string, tc md031NativeLiteral) string {
	c := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
	value := "{enabled: true}"
	if tc.Options.Enabled != nil {
		value = fmt.Sprintf("{enabled: %v}", *tc.Options.Enabled)
	}
	if tc.Options.Empty {
		value = "{}"
	}
	c = strings.Replace(c, "MD031: {enabled: false}", "MD031: "+value, 1)
	if tc.Options.Omitted {
		c = strings.Replace(c, "  MD031: "+value+"\n", "", 1)
	}
	if tc.Options.Frontmatter != "" {
		c += "frontmatter: {enabled: true, formats: [" + tc.Options.Frontmatter + "]}\n"
	}
	return c
}
func md031NativeAssert(t *testing.T, out result.Result, tc md031NativeLiteral, view string) {
	t.Helper()
	if !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.DeferredFixes)+len(out.Documents) != 0 || len(out.Diagnostics) != len(tc.Findings) {
		t.Fatal(out)
	}
	for i, d := range out.Diagnostics {
		w := tc.Findings[i]
		if d.Rule != "MD031" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] || d.Snapshot != (result.Snapshot{View: view, SizeBytes: len(tc.Source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source)))}) {
			t.Fatal(d, w)
		}
	}
}
func TestCompiledPlatformMD031LiteralLint(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	cases := md031NativeLiterals(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, name := range []string{"page-bad", "page-good", "both", "quote", "items", "nested-quote-item", "definition-before", "quote-owned-blank", "header", "disabled", "default-enable", "empty-options", "unicode-crlf", "both-cr", "adjacent", "padding"} {
			t.Run(flavor+"/"+name, func(t *testing.T) {
				tc, ok := cases[name]
				if !ok {
					t.Fatal(name)
				}
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md031NativeConfig(flavor, tc))
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
				md031NativeAssert(t, out, tc, "original")
				readinessAssertEqual(t, "MD031 lint", before, readinessState(t, root))
			})
		}
	}
}
func TestCompiledPlatformMD031InspectionFocus(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	base := md031NativeLiterals(t)["both"]
	for _, focus := range []string{"", "2", "3", "4"} {
		t.Run("line-"+focus, func(t *testing.T) {
			tc := base
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md031NativeConfig("gfm", tc))
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
			switch focus {
			case "2":
				tc.Findings = tc.Findings[:1]
			case "3":
				tc.Findings = nil
			case "4":
				tc.Findings = tc.Findings[1:]
			}
			md031NativeAssert(t, out, tc, "original")
			readinessAssertEqual(t, "MD031 focus", before, readinessState(t, root))
		})
	}
}
func TestCompiledPlatformMD031ManualAndMD047Preview(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	base := md031NativeLiterals(t)["both"]
	for _, composition := range []bool{false, true} {
		t.Run(fmt.Sprintf("composition-%v", composition), func(t *testing.T) {
			tc := base
			tc.Source = strings.TrimSuffix(tc.Source, "\n")
			conf := md031NativeConfig("commonmark", tc)
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
			for _, c := range out.Changes {
				if len(c.Rules) != 1 || c.Rules[0] != "MD047" {
					t.Fatal(c)
				}
			}
			md031NativeAssert(t, out, tc, "candidate")
			readinessAssertEqual(t, "MD031 manual/MD047 preview", before, readinessState(t, root))
		})
	}
}
func TestCompiledPlatformMD031InvalidAdmission(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md031NativeLiterals(t)["page-bad"]
	for _, command := range []string{"lint", "fix"} {
		for _, value := range []string{"{enabled: false, list_items: true}", "{enabled: 'true'}", "null"} {
			t.Run(command+"/"+value, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(md031NativeConfig("commonmark", tc), "MD031: {enabled: true}", "MD031: "+value, 1)
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
				readinessAssertEqual(t, "MD031 invalid preflight", before, readinessState(t, root))
			})
		}
	}
}
func TestCompiledPlatformMD031NoCGO(t *testing.T) {
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
	tc := md031NativeLiterals(t)["both"]
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md031NativeConfig("commonmark", tc))
	putConfigurationFile(t, root, "doc.md", tc.Source)
	before := readinessState(t, root)
	got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
	out := md014NativeJSON(t, []byte(got.Stdout), "lint")
	if got.Status != 10 {
		t.Fatal(got)
	}
	md031NativeAssert(t, out, tc, "original")
	got = md014NativeRun(t, binary, root, "fix", "doc.md", "--format=json")
	out = md014NativeJSON(t, []byte(got.Stdout), "fix")
	assertCGO0Apply(t, got, out, before, readinessState(t, root))
}

func TestCompiledPlatformMD031DirectiveAndUnknownCoverage(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		name, source              string
		denied, incomplete        bool
		visible, hidden, coverage int
	}{
		{"used", "<!-- mdchk:disable MD031 -->\n```\nx\n```\nTail", false, false, 0, 2, 0},
		{"denied", "<!-- mdchk:disable MD031 -->\n```\nx\n```\nTail", true, false, 2, 0, 0},
		{"unknown", "<!-- mdchk:disable MD031 -->\nx\n```\n```\n\n<div>\n<x title=\"\n\n~~~\nx\n~~~", false, true, 0, 0, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			conf := md031NativeConfig("gfm", md031NativeLiteral{})
			conf = strings.Replace(conf, "directives: {enabled: false}", "directives: {enabled: true}", 1)
			conf = strings.Replace(conf, "MD087: {enabled: false}", "MD087: {enabled: true}", 1)
			if tc.denied {
				conf = strings.Replace(conf, "MD031: {enabled: true}", "MD031: {enabled: true, allow_directives: false}", 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "lint")
			want := 0
			if tc.visible > 0 {
				want = 10
			}
			if tc.incomplete {
				want = 15
			}
			if got.Status != want || out.Complete == tc.incomplete || len(out.Diagnostics) != tc.visible || len(out.SuppressedDiagnostics) != tc.hidden || len(out.Coverage) != tc.coverage || len(out.Errors)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
				t.Fatal(got, out)
			}
			for _, d := range out.Diagnostics {
				if d.Rule != "MD031" || d.FixAvailable {
					t.Fatal(d)
				}
			}
			for _, c := range out.Coverage {
				if c.Reason != result.ContextUnavailable {
					t.Fatal(c)
				}
			}
			readinessAssertEqual(t, "MD031 directive and unknown", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD031InheritedDirectiveDenial(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprintf("global-%v", global), func(t *testing.T) {
			root := configurationProject(t)
			child := strings.Replace(md031NativeConfig("gfm", md031NativeLiteral{}), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			parent := strings.Replace(child, "MD031: {enabled: true}", "MD031: {enabled: true, allow_directives: false}", 1)
			if global {
				parent = strings.Replace(child, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", parent)
			putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
			source := "<!-- mdchk:disable MD031 -->\n```\nx\n```\nTail"
			putConfigurationFile(t, root, "nested/doc.md", source)
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, filepath.Join(root, "nested"), "inspect", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "inspect")
			if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 2 || len(out.SuppressedDiagnostics)+len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(got, out)
			}
			for _, d := range out.Diagnostics {
				if d.Rule != "MD031" || d.FixAvailable {
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
			readinessAssertEqual(t, "MD031 inherited permission cannot be relaxed", before, readinessState(t, root))
		})
	}
}
