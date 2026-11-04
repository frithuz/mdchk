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

type md040NativeLiteral struct {
	Name, Source string
	Options      struct {
		Enabled     *bool
		Omitted     bool `json:"enabled_omitted"`
		Empty       bool `json:"empty_options"`
		Frontmatter string
		Allowed     []string `json:"allowed_languages"`
	}
	Findings []struct {
		Bytes, Start, End [2]int
		Message           string
	}
}

func md040NativeLiterals(t *testing.T) map[string]md040NativeLiteral {
	t.Helper()
	b, e := os.ReadFile("../../internal/rules/testdata/md040.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "b8967e0a2f607a15ba75d0c78e0a770052ff9c27d1e3ee21ca6559df4fe9eaad" {
		t.Fatal(e, "oracle identity")
	}
	var v struct{ Cases []md040NativeLiteral }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 83 {
		t.Fatal(e)
	}
	out := map[string]md040NativeLiteral{}
	for _, tc := range v.Cases {
		if _, ok := out[tc.Name]; ok {
			t.Fatal("duplicate")
		}
		out[tc.Name] = tc
	}
	return out
}
func md040NativeConfig(flavor string, tc md040NativeLiteral) string {
	c := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
	value := "{enabled: true}"
	if tc.Options.Enabled != nil {
		value = fmt.Sprintf("{enabled: %v}", *tc.Options.Enabled)
	}
	if tc.Options.Allowed != nil {
		b, e := json.Marshal(tc.Options.Allowed)
		if e != nil {
			panic(e)
		}
		value = strings.TrimSuffix(value, "}") + ", allowed_languages: " + string(b) + "}"
	}
	if tc.Options.Empty {
		value = "{}"
	}
	c = strings.Replace(c, "MD040: {enabled: false}", "MD040: "+value, 1)
	if tc.Options.Omitted && !tc.Options.Empty {
		c = strings.Replace(c, "  MD040: "+value+"\n", "", 1)
	}
	if tc.Options.Frontmatter != "" {
		c += "frontmatter: {enabled: true, formats: [" + tc.Options.Frontmatter + "]}\n"
	}
	return c
}
func md040NativeAssert(t *testing.T, out result.Result, tc md040NativeLiteral, view string) {
	t.Helper()
	if !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.DeferredFixes)+len(out.Documents) != 0 || len(out.Diagnostics) != len(tc.Findings) {
		t.Fatal(out)
	}
	for i, d := range out.Diagnostics {
		w := tc.Findings[i]
		if d.Rule != "MD040" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] || d.Snapshot != (result.Snapshot{View: view, SizeBytes: len(tc.Source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source)))}) {
			t.Fatal(d, w)
		}
	}
}
func TestCompiledPlatformMD040LiteralLint(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	cases := md040NativeLiterals(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, name := range []string{"page-bad", "page-good", "both", "nested", "unicode", "allow-miss-crlf", "decoded-label-miss", "brace-class", "header", "default-disabled", "empty-options-disabled", "explicit-disabled", "entity-leading-space-miss", "escape-miss", "page-bad-cr", "padding"} {
			t.Run(flavor+"/"+name, func(t *testing.T) {
				tc, ok := cases[name]
				if !ok {
					t.Fatal(name)
				}
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md040NativeConfig(flavor, tc))
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
				md040NativeAssert(t, out, tc, "original")
				readinessAssertEqual(t, "MD040 lint", before, readinessState(t, root))
			})
		}
	}
}
func TestCompiledPlatformMD040InspectionFocus(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	base := md040NativeLiterals(t)["both"]
	for _, focus := range []string{"", "1", "2", "4"} {
		t.Run("line-"+focus, func(t *testing.T) {
			tc := base
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md040NativeConfig("gfm", tc))
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
			case "1":
				tc.Findings = tc.Findings[:1]
			case "2":
				tc.Findings = nil
			case "4":
				tc.Findings = tc.Findings[1:]
			}
			md040NativeAssert(t, out, tc, "original")
			readinessAssertEqual(t, "MD040 focus", before, readinessState(t, root))
		})
	}
}
func TestCompiledPlatformMD040ManualAndMD047Preview(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	base := md040NativeLiterals(t)["both"]
	for _, composition := range []bool{false, true} {
		t.Run(fmt.Sprintf("composition-%v", composition), func(t *testing.T) {
			tc := base
			tc.Source = strings.TrimSuffix(tc.Source, "\n") + "\nTail"
			conf := md040NativeConfig("commonmark", tc)
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
			md040NativeAssert(t, out, tc, "candidate")
			readinessAssertEqual(t, "MD040 manual/MD047 preview", before, readinessState(t, root))
		})
	}
	t.Run("closing-fence-eof-refusal", func(t *testing.T) {
		tc := base
		tc.Source = strings.TrimSuffix(tc.Source, "\n")
		conf := strings.Replace(md040NativeConfig("commonmark", tc), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", conf)
		putConfigurationFile(t, root, "doc.md", tc.Source)
		before := readinessState(t, root)
		got := md014NativeRun(t, binary, root, "fix", "doc.md", "--dry-run", "--format=json")
		out := md014NativeJSON(t, []byte(got.Stdout), "fix")
		if got.Status != 10 || !out.Complete || len(out.Changes) != 0 || len(out.DeferredFixes) != 1 || len(out.Diagnostics) != 3 || len(out.Files) != 1 {
			t.Fatal(got, out)
		}
		original := result.Snapshot{View: "original", SizeBytes: len(tc.Source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source)))}
		candidate := original
		candidate.View = "candidate"
		deferred := out.DeferredFixes[0]
		if deferred.Rule != "MD047" || deferred.Reason != "candidate-preservation-failed" || deferred.Snapshot != original {
			t.Fatal(deferred)
		}
		file := out.Files[0]
		if file.InputSnapshot == nil || file.ResultSnapshot == nil || *file.InputSnapshot != original || *file.ResultSnapshot != candidate {
			t.Fatal(file)
		}
		terminal := out.Diagnostics[2]
		if terminal.Rule != "MD047" || terminal.Message != "File must end with a line-ending sequence." || terminal.FixAvailable || terminal.ByteRange != (result.ByteRange{Start: 23, End: 23}) || terminal.Range != (result.Range{Start: result.Position{Line: 6, Column: 4}, End: result.Position{Line: 6, Column: 4}}) || terminal.Snapshot != candidate {
			t.Fatal(terminal)
		}
		// The separately checked MD047 refusal leaves both exact MD040 findings
		// in the retained original content, rather than its rejected candidate.
		manual := out
		manual.DeferredFixes = nil
		manual.Diagnostics = out.Diagnostics[:2]
		md040NativeAssert(t, manual, tc, "candidate")
		readinessAssertEqual(t, "closing fence EOF refusal", before, readinessState(t, root))
	})
}

type md040InvalidLiteral struct {
	Name  string
	Value json.RawMessage `json:"md040_value"`
}

func md040NativeInvalid(t *testing.T) []md040InvalidLiteral {
	t.Helper()
	b, e := os.ReadFile("../../internal/rules/testdata/md040.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "b8967e0a2f607a15ba75d0c78e0a770052ff9c27d1e3ee21ca6559df4fe9eaad" {
		t.Fatal(e)
	}
	var v struct {
		Invalid []md040InvalidLiteral `json:"invalid_configurations"`
	}
	if e = json.Unmarshal(b, &v); e != nil || len(v.Invalid) != 15 {
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

func TestCompiledPlatformMD040InvalidAdmission(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	tc := md040NativeLiterals(t)["page-bad"]
	for _, command := range []string{"lint", "fix"} {
		for _, row := range md040NativeInvalid(t) {
			value := string(row.Value)
			t.Run(command+"/"+row.Name, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(md040NativeConfig("commonmark", tc), "MD040: {enabled: true}", "MD040: "+value, 1)
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
				readinessAssertEqual(t, "MD040 invalid preflight", before, readinessState(t, root))
			})
		}
	}
}
func TestCompiledPlatformMD040NoCGO(t *testing.T) {
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
	tc := md040NativeLiterals(t)["both"]
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md040NativeConfig("commonmark", tc))
	putConfigurationFile(t, root, "doc.md", tc.Source)
	before := readinessState(t, root)
	got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
	out := md014NativeJSON(t, []byte(got.Stdout), "lint")
	if got.Status != 10 {
		t.Fatal(got)
	}
	md040NativeAssert(t, out, tc, "original")
	got = md014NativeRun(t, binary, root, "fix", "doc.md", "--format=json")
	out = md014NativeJSON(t, []byte(got.Stdout), "fix")
	assertCGO0Apply(t, got, out, before, readinessState(t, root))
}

func TestCompiledPlatformMD040DirectiveAndUnknownCoverage(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		name, source              string
		denied, incomplete        bool
		visible, hidden, coverage int
	}{
		{"used", "<!-- mdchk:disable MD040 -->\n```\nx\n```\nTail", false, false, 0, 1, 0},
		{"denied", "<!-- mdchk:disable MD040 -->\n```\nx\n```\nTail", true, false, 1, 0, 0},
		{"unknown", "<!-- mdchk:disable MD040 -->\nx\n```\n```\n\n<div>\n<x title=\"\n\n~~~\nx\n~~~", false, true, 0, 0, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			conf := md040NativeConfig("gfm", md040NativeLiteral{})
			conf = strings.Replace(conf, "directives: {enabled: false}", "directives: {enabled: true}", 1)
			conf = strings.Replace(conf, "MD087: {enabled: false}", "MD087: {enabled: true}", 1)
			if tc.denied {
				conf = strings.Replace(conf, "MD040: {enabled: true}", "MD040: {enabled: true, allow_directives: false}", 1)
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
				if d.Rule != "MD040" || d.FixAvailable {
					t.Fatal(d)
				}
			}
			for _, c := range out.Coverage {
				if c.Reason != result.ContextUnavailable {
					t.Fatal(c)
				}
			}
			readinessAssertEqual(t, "MD040 directive and unknown", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD040InheritedDirectiveDenial(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprintf("global-%v", global), func(t *testing.T) {
			root := configurationProject(t)
			child := strings.Replace(md040NativeConfig("gfm", md040NativeLiteral{}), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			parent := strings.Replace(child, "MD040: {enabled: true}", "MD040: {enabled: true, allow_directives: false}", 1)
			if global {
				parent = strings.Replace(child, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", parent)
			putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
			source := "<!-- mdchk:disable MD040 -->\n```\nx\n```\nTail"
			putConfigurationFile(t, root, "nested/doc.md", source)
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, filepath.Join(root, "nested"), "inspect", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "inspect")
			if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 1 || len(out.SuppressedDiagnostics)+len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(got, out)
			}
			for _, d := range out.Diagnostics {
				if d.Rule != "MD040" || d.FixAvailable {
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
			readinessAssertEqual(t, "MD040 inherited permission cannot be relaxed", before, readinessState(t, root))
		})
	}
}
