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

type md048NativeLiteral struct {
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

func md048NativeLiterals(t *testing.T) map[string]md048NativeLiteral {
	t.Helper()
	b, e := os.ReadFile("../../internal/rules/testdata/md048.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "afef57dcdf5974d81ddbe9c85e688f1ac0f42b6d4befcd68b056a28b4b254807" {
		t.Fatal(e, "oracle identity")
	}
	var v struct{ Cases []md048NativeLiteral }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 65 {
		t.Fatal(e)
	}
	out := map[string]md048NativeLiteral{}
	for _, tc := range v.Cases {
		if _, ok := out[tc.Name]; ok {
			t.Fatal("duplicate")
		}
		out[tc.Name] = tc
	}
	return out
}
func md048NativeConfig(flavor string, tc md048NativeLiteral) string {
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
	c = strings.Replace(c, "MD048: {enabled: false}", "MD048: "+value, 1)
	if tc.Options.Omitted && !tc.Options.Empty {
		c = strings.Replace(c, "  MD048: "+value+"\n", "", 1)
	}
	if tc.Options.Frontmatter != "" {
		c += "frontmatter: {enabled: true, formats: [" + tc.Options.Frontmatter + "]}\n"
	}
	return c
}
func md048NativeAssert(t *testing.T, out result.Result, tc md048NativeLiteral, view string) {
	t.Helper()
	if !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.DeferredFixes)+len(out.Documents) != 0 || len(out.Diagnostics) != len(tc.Findings) {
		t.Fatal(out)
	}
	for i, d := range out.Diagnostics {
		w := tc.Findings[i]
		if d.Rule != "MD048" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] || d.Snapshot != (result.Snapshot{View: view, SizeBytes: len(tc.Source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source)))}) {
			t.Fatal(d, w)
		}
	}
}
func TestCompiledPlatformMD048LiteralLint(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	cases := md048NativeLiterals(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, name := range md048NativeNames(cases) {
			t.Run(flavor+"/"+name, func(t *testing.T) {
				tc, ok := cases[name]
				if !ok {
					t.Fatal(name)
				}
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md048NativeConfig(flavor, tc))
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
				md048NativeAssert(t, out, tc, "original")
				readinessAssertEqual(t, "MD048 lint", before, readinessState(t, root))
			})
		}
	}
}
func TestCompiledPlatformMD048InspectionFocus(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	base := md048NativeLiterals(t)["alternating"]
	for _, focus := range []string{"", "5", "6", "13"} {
		t.Run("line-"+focus, func(t *testing.T) {
			tc := base
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md048NativeConfig("gfm", tc))
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
			case "5":
				tc.Findings = tc.Findings[:1]
			case "6":
				tc.Findings = nil
			case "13":
				tc.Findings = tc.Findings[1:]
			}
			md048NativeAssert(t, out, tc, "original")
			readinessAssertEqual(t, "MD048 focus", before, readinessState(t, root))
		})
	}
}
func TestCompiledPlatformMD048ManualAndMD047Preview(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	base := md048NativeLiterals(t)["fixed-tilde"]
	for _, composition := range []bool{false, true} {
		t.Run(fmt.Sprintf("composition-%v", composition), func(t *testing.T) {
			tc := base
			tc.Source = strings.TrimSuffix(tc.Source, "\n") + "\nTail"
			conf := md048NativeConfig("commonmark", tc)
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
			md048NativeAssert(t, out, tc, "candidate")
			readinessAssertEqual(t, "MD048 manual/MD047 preview", before, readinessState(t, root))
		})
	}
}

type md048InvalidLiteral struct {
	Name  string
	Value json.RawMessage `json:"md048_value"`
}

func md048NativeInvalid(t *testing.T) []md048InvalidLiteral {
	t.Helper()
	b, e := os.ReadFile("../../internal/rules/testdata/md048.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "afef57dcdf5974d81ddbe9c85e688f1ac0f42b6d4befcd68b056a28b4b254807" {
		t.Fatal(e)
	}
	var v struct {
		Invalid []md048InvalidLiteral `json:"invalid_configurations"`
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

func TestCompiledPlatformMD048InvalidAdmission(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	tc := md048NativeLiterals(t)["page-bad"]
	for _, command := range []string{"lint", "fix"} {
		for _, row := range md048NativeInvalid(t) {
			value := string(row.Value)
			t.Run(command+"/"+row.Name, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(md048NativeConfig("commonmark", tc), "MD048: {enabled: true}", "MD048: "+value, 1)
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
				readinessAssertEqual(t, "MD048 invalid preflight", before, readinessState(t, root))
			})
		}
	}
}
func TestCompiledPlatformMD048NoCGO(t *testing.T) {
	t.Parallel()
	binary := sharedNoCGOExecutable(t)
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

	tc := md048NativeLiterals(t)["page-bad"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md048NativeConfig(flavor, tc))
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
				md048NativeAssert(t, out, tc, "original")
			}
			validated := md014NativeRun(t, binary, root, "config", "validate", "--format=json")
			config := md014NativeJSON(t, []byte(validated.Stdout), "config validate")
			if validated.Status != 0 || !config.Complete || len(config.Errors) != 0 || config.ConfigValidation == nil || !config.ConfigValidation.ScanComplete || config.ConfigValidation.ValidDefinitions != 1 {
				t.Fatal(validated, config)
			}
			got := md014NativeRun(t, binary, root, "fix", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "fix")
			assertCGO0Apply(t, got, out, before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD048DirectiveAndUnknownCoverage(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, tc := range []struct {
		name, source              string
		denied, incomplete        bool
		visible, hidden, coverage int
	}{
		{"used", "<!-- mdchk:disable MD048 -->\n```\nx\n```\nTail", false, false, 0, 1, 0},
		{"denied", "<!-- mdchk:disable MD048 -->\n```\nx\n```\nTail", true, false, 1, 0, 0},
		{"unknown", "<!-- mdchk:disable MD048 -->\nx\n```\n```\n\n<div>\n<x title=\"\n\n```\nx\n```", false, true, 0, 0, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			conf := md048NativeConfig("gfm", md048NativeFixed())
			conf = strings.Replace(conf, "directives: {enabled: false}", "directives: {enabled: true}", 1)
			conf = strings.Replace(conf, "MD087: {enabled: false}", "MD087: {enabled: true}", 1)
			if tc.denied {
				conf = strings.Replace(conf, "MD048: {enabled: true, style: tilde}", "MD048: {enabled: true, style: tilde, allow_directives: false}", 1)
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
				if d.Rule != "MD048" || d.FixAvailable {
					t.Fatal(d)
				}
			}
			for _, c := range out.Coverage {
				if c.Reason != result.ContextUnavailable {
					t.Fatal(c)
				}
			}
			readinessAssertEqual(t, "MD048 directive and unknown", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD048InheritedDirectiveDenial(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprintf("global-%v", global), func(t *testing.T) {
			root := configurationProject(t)
			child := strings.Replace(md048NativeConfig("gfm", md048NativeFixed()), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			parent := strings.Replace(child, "MD048: {enabled: true, style: tilde}", "MD048: {enabled: true, style: tilde, allow_directives: false}", 1)
			if global {
				parent = strings.Replace(child, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", parent)
			putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
			source := "<!-- mdchk:disable MD048 -->\n```\nx\n```\nTail"
			putConfigurationFile(t, root, "nested/doc.md", source)
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, filepath.Join(root, "nested"), "inspect", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "inspect")
			if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 1 || len(out.SuppressedDiagnostics)+len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(got, out)
			}
			for _, d := range out.Diagnostics {
				if d.Rule != "MD048" || d.FixAvailable {
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
			readinessAssertEqual(t, "MD048 inherited permission cannot be relaxed", before, readinessState(t, root))
		})
	}
}

func md048NativeFixed() md048NativeLiteral {
	var tc md048NativeLiteral
	tc.Options.Style = "tilde"
	return tc
}

func TestCompiledPlatformMD048SuppressedExemplar(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	root := configurationProject(t)
	conf := strings.Replace(md048NativeConfig("commonmark", md048NativeLiteral{}), "directives: {enabled: false}", "directives: {enabled: true}", 1)
	source := "<!-- mdchk:disable MD048 -->\n```\nx\n```\n<!-- mdchk:enable MD048 -->\n~~~\ny\n~~~"
	putConfigurationFile(t, root, ".mdchk.yaml", conf)
	putConfigurationFile(t, root, "doc.md", source)
	before := readinessState(t, root)
	got := md014NativeRun(t, binary, root, "inspect", "doc.md", "--lines=6", "--format=json")
	out := md014NativeJSON(t, []byte(got.Stdout), "inspect")
	if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 1 || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents)+len(out.SuppressedDiagnostics) != 0 {
		t.Fatal(got, out)
	}
	d := out.Diagnostics[0]
	if d.Rule != "MD048" || d.Message != "Fenced code block should use backtick delimiters." || d.FixAvailable || d.Range != (result.Range{Start: result.Position{Line: 6, Column: 1}, End: result.Position{Line: 6, Column: 4}}) || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: len(source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}) {
		t.Fatal(d)
	}
	readinessAssertEqual(t, "suppressed exemplar still sets global style", before, readinessState(t, root))
}

func md048NativeNames(cases map[string]md048NativeLiteral) []string {
	names := make([]string, 0, len(cases))
	for name := range cases {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestCompiledPlatformMD048ReportersAndManualRefusal(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	tc := md048NativeLiterals(t)["page-bad"]
	for _, format := range []string{"text", "json", "vscode", "github"} {
		t.Run(format, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md048NativeConfig("commonmark", tc))
			putConfigurationFile(t, root, "doc.md", tc.Source)
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format="+format)
			if got.Status != 10 || !strings.Contains(got.Stdout, "MD048") || !strings.Contains(got.Stdout, tc.Findings[0].Message) {
				t.Fatal(got)
			}
			if format == "json" {
				md048NativeAssert(t, md014NativeJSON(t, []byte(got.Stdout), "lint"), tc, "original")
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
				md048NativeAssert(t, out, tc, "saved")
			}
			readinessAssertEqual(t, "manual-only apply or unsupported refusal", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD048ClosingFenceEOFRefusal(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	tc := md048NativeLiterals(t)["fixed-tilde"]
	root := configurationProject(t)
	conf := strings.Replace(md048NativeConfig("commonmark", tc), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	putConfigurationFile(t, root, ".mdchk.yaml", conf)
	putConfigurationFile(t, root, "doc.md", tc.Source)
	before := readinessState(t, root)
	got := md014NativeRun(t, binary, root, "fix", "doc.md", "--dry-run", "--format=json")
	out := md014NativeJSON(t, []byte(got.Stdout), "fix")
	if got.Status != 10 || !out.Complete || len(out.Changes) != 0 || len(out.DeferredFixes) != 1 || len(out.Diagnostics) != 3 {
		t.Fatal(got, out)
	}
	if out.DeferredFixes[0].Rule != "MD047" || out.DeferredFixes[0].Reason != "candidate-preservation-failed" {
		t.Fatal(out.DeferredFixes)
	}
	terminal := out.Diagnostics[2]
	if terminal.Rule != "MD047" || terminal.FixAvailable || terminal.ByteRange != (result.ByteRange{Start: 28, End: 28}) {
		t.Fatal(terminal)
	}
	out.DeferredFixes = nil
	out.Diagnostics = out.Diagnostics[:2]
	md048NativeAssert(t, out, tc, "candidate")
	readinessAssertEqual(t, "closing fence refusal", before, readinessState(t, root))
}

func TestCompiledPlatformMD048LiteralFocusControl(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	source := "```\nx\n```\n\n~~~\ny\n~~~"
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, focus := range []string{"", "5", "6"} {
			t.Run(flavor+"/line-"+focus, func(t *testing.T) {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md048NativeConfig(flavor, md048NativeLiteral{}))
				putConfigurationFile(t, root, "doc.md", source)
				before := readinessState(t, root)
				args := []string{"inspect", "doc.md", "--format=json"}
				if focus != "" {
					args = append(args, "--lines="+focus)
				}
				got := md014NativeRun(t, binary, root, args...)
				out := md014NativeJSON(t, []byte(got.Stdout), "inspect")
				want := 1
				if focus == "6" {
					want = 0
				}
				if got.Status != 0 || !out.Complete || len(out.Diagnostics) != want || len(out.Errors)+len(out.Coverage) != 0 {
					t.Fatal(got, out)
				}
				if want == 1 {
					d := out.Diagnostics[0]
					if d.Rule != "MD048" || d.Message != "Fenced code block should use backtick delimiters." || d.ByteRange != (result.ByteRange{Start: 11, End: 14}) || d.Range != (result.Range{Start: result.Position{Line: 5, Column: 1}, End: result.Position{Line: 5, Column: 4}}) || d.FixAvailable {
						t.Fatal(d)
					}
				}
				readinessAssertEqual(t, "literal focus", before, readinessState(t, root))
			})
		}
	}
}
