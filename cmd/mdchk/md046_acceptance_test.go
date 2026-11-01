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

type md046NativeLiteral struct {
	Name, Source string
	Options      struct {
		Enabled     *bool
		Omitted     bool `json:"enabled_omitted"`
		Empty       bool `json:"empty_options"`
		Frontmatter string
		Style       string
	}
	Findings []struct {
		Bytes, Start, End [2]int
		Message           string
	}
}

func md046NativeLiterals(t *testing.T) map[string]md046NativeLiteral {
	t.Helper()
	b, e := os.ReadFile("../../internal/rules/testdata/md046.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "1fff51e531a8c0c7958ef64c617a93df77933d3bb32e62ef75f2a54d4c4788fb" {
		t.Fatal(e, "oracle identity")
	}
	var v struct{ Cases []md046NativeLiteral }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 64 {
		t.Fatal(e)
	}
	out := map[string]md046NativeLiteral{}
	for _, tc := range v.Cases {
		if _, ok := out[tc.Name]; ok {
			t.Fatal("duplicate")
		}
		out[tc.Name] = tc
	}
	return out
}
func md046NativeConfig(flavor string, tc md046NativeLiteral) string {
	c := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
	value := "{enabled: true}"
	if tc.Options.Enabled != nil {
		value = fmt.Sprintf("{enabled: %v}", *tc.Options.Enabled)
	}
	if tc.Options.Style != "" {
		value = strings.TrimSuffix(value, "}") + ", style: " + tc.Options.Style + "}"
	}
	if tc.Options.Empty {
		value = "{}"
	}
	c = strings.Replace(c, "MD046: {enabled: false}", "MD046: "+value, 1)
	if tc.Options.Omitted && !tc.Options.Empty {
		c = strings.Replace(c, "  MD046: "+value+"\n", "", 1)
	}
	if tc.Options.Frontmatter != "" {
		c += "frontmatter: {enabled: true, formats: [" + tc.Options.Frontmatter + "]}\n"
	}
	return c
}
func md046NativeAssert(t *testing.T, out result.Result, tc md046NativeLiteral, view string) {
	t.Helper()
	if !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.DeferredFixes)+len(out.Documents) != 0 || len(out.Diagnostics) != len(tc.Findings) {
		t.Fatal(out)
	}
	for i, d := range out.Diagnostics {
		w := tc.Findings[i]
		if d.Rule != "MD046" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] || d.Snapshot != (result.Snapshot{View: view, SizeBytes: len(tc.Source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source)))}) {
			t.Fatal(d, w)
		}
	}
}
func TestCompiledPlatformMD046LiteralLint(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	cases := md046NativeLiterals(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, name := range []string{"page-bad", "page-good", "reverse", "fixed-fenced", "fixed-indented", "tab", "quote-shared-tab", "list-shared-tab", "nested-indented", "nested-fenced", "raw-exemplar", "frontmatter", "default-enabled", "empty-options-enabled", "disabled", "page-bad-crlf", "reverse-cr", "unicode-before", "nul-payload", "ordinary-html-then-code"} {
			t.Run(flavor+"/"+name, func(t *testing.T) {
				tc, ok := cases[name]
				if !ok {
					t.Fatal(name)
				}
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md046NativeConfig(flavor, tc))
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
				md046NativeAssert(t, out, tc, "original")
				readinessAssertEqual(t, "MD046 lint", before, readinessState(t, root))
			})
		}
	}
}
func TestCompiledPlatformMD046InspectionFocus(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	base := md046NativeLiterals(t)["alternating"]
	for _, focus := range []string{"", "3", "4", "9"} {
		t.Run("line-"+focus, func(t *testing.T) {
			tc := base
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md046NativeConfig("gfm", tc))
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
			case "3":
				tc.Findings = tc.Findings[:1]
			case "4":
				tc.Findings = nil
			case "9":
				tc.Findings = tc.Findings[1:]
			}
			md046NativeAssert(t, out, tc, "original")
			readinessAssertEqual(t, "MD046 focus", before, readinessState(t, root))
		})
	}
}
func TestCompiledPlatformMD046ManualAndMD047Preview(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	base := md046NativeLiterals(t)["fixed-indented"]
	for _, composition := range []bool{false, true} {
		t.Run(fmt.Sprintf("composition-%v", composition), func(t *testing.T) {
			tc := base
			tc.Source = strings.TrimSuffix(tc.Source, "\n") + "\nTail"
			conf := md046NativeConfig("commonmark", tc)
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
			md046NativeAssert(t, out, tc, "candidate")
			readinessAssertEqual(t, "MD046 manual/MD047 preview", before, readinessState(t, root))
		})
	}
}

type md046InvalidLiteral struct {
	Name  string
	Value json.RawMessage `json:"md046_value"`
}

func md046NativeInvalid(t *testing.T) []md046InvalidLiteral {
	t.Helper()
	b, e := os.ReadFile("../../internal/rules/testdata/md046.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "1fff51e531a8c0c7958ef64c617a93df77933d3bb32e62ef75f2a54d4c4788fb" {
		t.Fatal(e)
	}
	var v struct {
		Invalid []md046InvalidLiteral `json:"invalid_configurations"`
	}
	if e = json.Unmarshal(b, &v); e != nil || len(v.Invalid) != 17 {
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

func TestCompiledPlatformMD046InvalidAdmission(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md046NativeLiterals(t)["page-bad"]
	for _, command := range []string{"lint", "fix"} {
		for _, row := range md046NativeInvalid(t) {
			value := string(row.Value)
			t.Run(command+"/"+row.Name, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(md046NativeConfig("commonmark", tc), "MD046: {enabled: true}", "MD046: "+value, 1)
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
				readinessAssertEqual(t, "MD046 invalid preflight", before, readinessState(t, root))
			})
		}
	}
}
func TestCompiledPlatformMD046NoCGO(t *testing.T) {
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
	tc := md046NativeLiterals(t)["page-bad"]
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md046NativeConfig("commonmark", tc))
	putConfigurationFile(t, root, "doc.md", tc.Source)
	before := readinessState(t, root)
	got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
	out := md014NativeJSON(t, []byte(got.Stdout), "lint")
	if got.Status != 10 {
		t.Fatal(got)
	}
	md046NativeAssert(t, out, tc, "original")
	got = md014NativeRun(t, binary, root, "fix", "doc.md", "--format=json")
	out = md014NativeJSON(t, []byte(got.Stdout), "fix")
	if !md038NativeRefusal(t, got, out) || len(out.Changes)+len(out.Documents)+len(out.Diagnostics) != 0 {
		t.Fatal(got, out)
	}
	readinessAssertEqual(t, "MD046 CGO0", before, readinessState(t, root))
}

func TestCompiledPlatformMD046DirectiveAndUnknownCoverage(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		name, source              string
		denied, incomplete        bool
		visible, hidden, coverage int
	}{
		{"used", "<!-- mdchk:disable MD046 -->\n```\nx\n```\nTail", false, false, 0, 1, 0},
		{"denied", "<!-- mdchk:disable MD046 -->\n```\nx\n```\nTail", true, false, 1, 0, 0},
		{"unknown", "<!-- mdchk:disable MD046 -->\nx\n```\n```\n\n<div>\n<x title=\"\n\n~~~\nx\n~~~", false, true, 0, 0, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			conf := md046NativeConfig("gfm", md046NativeFixed())
			conf = strings.Replace(conf, "directives: {enabled: false}", "directives: {enabled: true}", 1)
			conf = strings.Replace(conf, "MD087: {enabled: false}", "MD087: {enabled: true}", 1)
			if tc.denied {
				conf = strings.Replace(conf, "MD046: {enabled: true, style: indented}", "MD046: {enabled: true, style: indented, allow_directives: false}", 1)
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
				if d.Rule != "MD046" || d.FixAvailable {
					t.Fatal(d)
				}
			}
			for _, c := range out.Coverage {
				if c.Reason != result.ContextUnavailable {
					t.Fatal(c)
				}
			}
			readinessAssertEqual(t, "MD046 directive and unknown", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD046InheritedDirectiveDenial(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprintf("global-%v", global), func(t *testing.T) {
			root := configurationProject(t)
			child := strings.Replace(md046NativeConfig("gfm", md046NativeFixed()), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			parent := strings.Replace(child, "MD046: {enabled: true, style: indented}", "MD046: {enabled: true, style: indented, allow_directives: false}", 1)
			if global {
				parent = strings.Replace(child, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", parent)
			putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
			source := "<!-- mdchk:disable MD046 -->\n```\nx\n```\nTail"
			putConfigurationFile(t, root, "nested/doc.md", source)
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, filepath.Join(root, "nested"), "inspect", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "inspect")
			if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 1 || len(out.SuppressedDiagnostics)+len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(got, out)
			}
			for _, d := range out.Diagnostics {
				if d.Rule != "MD046" || d.FixAvailable {
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
			readinessAssertEqual(t, "MD046 inherited permission cannot be relaxed", before, readinessState(t, root))
		})
	}
}

func md046NativeFixed() md046NativeLiteral {
	var tc md046NativeLiteral
	tc.Options.Style = "indented"
	return tc
}

func TestCompiledPlatformMD046SuppressedExemplar(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	conf := strings.Replace(md046NativeConfig("commonmark", md046NativeLiteral{}), "directives: {enabled: false}", "directives: {enabled: true}", 1)
	source := "<!-- mdchk:disable MD046 -->\n    first\n\n<!-- mdchk:enable MD046 -->\n~~~\nsecond\n~~~"
	putConfigurationFile(t, root, ".mdchk.yaml", conf)
	putConfigurationFile(t, root, "doc.md", source)
	before := readinessState(t, root)
	got := md014NativeRun(t, binary, root, "inspect", "doc.md", "--lines=5", "--format=json")
	out := md014NativeJSON(t, []byte(got.Stdout), "inspect")
	if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 1 || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents)+len(out.SuppressedDiagnostics) != 0 {
		t.Fatal(got, out)
	}
	d := out.Diagnostics[0]
	if d.Rule != "MD046" || d.Message != "Code block should use indented style." || d.FixAvailable || d.Range != (result.Range{Start: result.Position{Line: 5, Column: 1}, End: result.Position{Line: 5, Column: 4}}) || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: len(source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}) {
		t.Fatal(d)
	}
	readinessAssertEqual(t, "suppressed exemplar still sets global style", before, readinessState(t, root))
}
