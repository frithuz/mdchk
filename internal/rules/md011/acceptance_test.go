package md011_test

import (
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/result"
)

// TestMain removes this process's private builds once every test has finished
// with them.
func TestMain(m *testing.M) {
	m.Run()
	acceptance.Cleanup()
}

type md011NativeLiteral struct {
	Name, Source string
	Flavors      []string
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

func md011NativeLiterals(t *testing.T) map[string]md011NativeLiteral {
	t.Helper()
	b, e := os.ReadFile("testdata/md011.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "b6e3e408b33fea1ec53de36b79ed6058ac45590c28526854261632685d3dcd29" {
		t.Fatal(e, "oracle identity")
	}
	var v struct{ Cases []md011NativeLiteral }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 115 {
		t.Fatal(e)
	}
	out := map[string]md011NativeLiteral{}
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

// Default registration is checked independently of explicitly enabled oracle rows.
func TestCompiledPlatformMD011DefaultOn(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	base := md011NativeLiterals(t)["page-bad"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"omitted", "empty-options"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				tc := base
				tc.Options.Omitted = mode == "omitted"
				tc.Options.Empty = mode == "empty-options"
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", md011NativeConfig(flavor, tc))
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				before := acceptance.ReadinessState(t, root)
				for _, command := range []string{"lint", "inspect"} {
					got := acceptance.MD014NativeRun(t, binary, root, command, "doc.md", "--format=json")
					want := 10
					if command == "inspect" {
						want = 0
					}
					out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), command)
					if got.Status != want || len(out.Changes) != 0 {
						t.Fatal(got, out)
					}
					md011NativeAssert(t, out, tc, "original")
					acceptance.ReadinessAssertEqual(t, "default-on analysis", before, acceptance.ReadinessState(t, root))
				}
			})
		}
	}
}

func TestCompiledPlatformMD011InspectionFocus(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	base := md011NativeLiterals(t)["lf"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, focus := range []string{"", "1", "2", "3"} {
			t.Run(flavor+"/line-"+focus, func(t *testing.T) {
				tc := base
				// A trailing newline alone does not create a selectable third line.
				tc.Source += "Tail\n"
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", md011NativeConfig(flavor, tc))
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				before := acceptance.ReadinessState(t, root)
				args := []string{"inspect", "doc.md", "--format=json"}
				if focus != "" {
					args = append(args, "--lines="+focus)
				}
				got := acceptance.MD014NativeRun(t, binary, root, args...)
				out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "inspect")
				if got.Status != 0 {
					t.Fatal(got)
				}
				if focus == "1" || focus == "3" {
					tc.Findings = nil
				}
				md011NativeAssert(t, out, tc, "original")
				acceptance.ReadinessAssertEqual(t, "full-region recognition before focus", before, acceptance.ReadinessState(t, root))
			})
		}
	}
}

func TestCompiledPlatformMD011ManualAndMD047Preview(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	base := md011NativeLiterals(t)["page-bad"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, composition := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/composition-%v", flavor, composition), func(t *testing.T) {
				tc := base
				tc.Source += "\n\nTail"
				original := tc.Source
				conf := md011NativeConfig(flavor, tc)
				if composition {
					conf = strings.Replace(conf, "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
				}
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				before := acceptance.ReadinessState(t, root)
				got := acceptance.MD014NativeRun(t, binary, root, "fix", "doc.md", "--dry-run", "--format=json")
				out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "fix")
				changes := 0
				if composition {
					changes = 1
					tc.Source += "\n"
				}
				if got.Status != 10 || len(out.Changes) != changes {
					t.Fatal(got, out)
				}
				for _, change := range out.Changes {
					if len(change.Rules) != 1 || change.Rules[0] != "MD047" || change.EditCount != 1 || change.Before.SizeBytes != len(original) || change.Before.SHA256 != acceptance.ReadinessHash(original) || change.Candidate.SizeBytes != len(tc.Source) || change.Candidate.SHA256 != acceptance.ReadinessHash(tc.Source) {
						t.Fatal(change)
					}
				}
				md011NativeAssert(t, out, tc, "candidate")
				acceptance.ReadinessAssertEqual(t, "manual-only MD011 plus safe prose EOF", before, acceptance.ReadinessState(t, root))
				applied := acceptance.MD014NativeRun(t, binary, root, "fix", "doc.md", "--format=json")
				saved := acceptance.MD014NativeJSON(t, []byte(applied.Stdout), "fix")
				if applied.Status == 13 {
					if !acceptance.MD038NativeRefusal(t, applied, saved) {
						t.Fatal(applied, saved)
					}
					acceptance.ReadinessAssertEqual(t, "unsupported composition has no effects", before, acceptance.ReadinessState(t, root))
				} else {
					if applied.Status != 10 || len(saved.Changes) != changes {
						t.Fatal(applied, saved)
					}
					for _, change := range saved.Changes {
						if len(change.Rules) != 1 || change.Rules[0] != "MD047" || change.EditCount != 1 || change.Before.SizeBytes != len(original) || change.Before.SHA256 != acceptance.ReadinessHash(original) || change.Candidate.SizeBytes != len(tc.Source) || change.Candidate.SHA256 != acceptance.ReadinessHash(tc.Source) {
							t.Fatal(change)
						}
					}
					md011NativeAssert(t, saved, tc, "saved")
					after := acceptance.ReadinessState(t, root)
					if composition {
						if after["doc.md"].Hash != acceptance.ReadinessHash(tc.Source) || after["doc.md"].Info.Mode() != before["doc.md"].Info.Mode() {
							t.Fatal("saved composition bytes or mode", after["doc.md"])
						}
						acceptance.ReadinessAssertParentAfterSave(t, before["."], after["."])
						// Only the documented file replacement and parent timestamp
						// may differ; compare every other inventory entry exactly.
						delete(before, "doc.md")
						delete(after, "doc.md")
						delete(before, ".")
						delete(after, ".")
					}
					acceptance.ReadinessAssertEqual(t, "composition preserves other fixture entries", before, after)
				}
			})
		}
	}
}

func TestCompiledPlatformMD011ClosingFenceEOFRefusal(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	base := md011NativeLiterals(t)["page-bad"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			tc := base
			tc.Source += "\n\n~~~\nx\n~~~"
			root := acceptance.Project(t)
			conf := strings.Replace(md011NativeConfig(flavor, tc), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			acceptance.PutFile(t, root, "doc.md", tc.Source)
			before := acceptance.ReadinessState(t, root)
			got := acceptance.MD014NativeRun(t, binary, root, "fix", "doc.md", "--dry-run", "--format=json")
			out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "fix")
			if got.Status != 10 || !out.Complete || len(out.Changes) != 0 || len(out.DeferredFixes) != 1 || len(out.Diagnostics) != 2 {
				t.Fatal(got, out)
			}
			if out.DeferredFixes[0].Rule != "MD047" || out.DeferredFixes[0].Reason != "candidate-preservation-failed" {
				t.Fatal(out.DeferredFixes)
			}
			var manual []result.Diagnostic
			terminal := 0
			for _, d := range out.Diagnostics {
				if d.Rule == "MD011" {
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
			md011NativeAssert(t, out, tc, "candidate")
			acceptance.ReadinessAssertEqual(t, "protected terminal fence remains manual", before, acceptance.ReadinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD011DirectiveControls(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	base := md011NativeLiterals(t)["page-bad"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"used", "denied", "unused", "enable", "enable-denied", "unknown"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				tc := base
				source := "<!-- mdchk:disable MD011 -->\n" + tc.Source
				conf := strings.Replace(md011NativeConfig(flavor, tc), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				conf = strings.Replace(conf, "MD087: {enabled: false}", "MD087: {enabled: true}", 1)
				if mode == "unused" {
					source = "<!-- mdchk:disable MD011 -->\n" + md011NativeLiterals(t)["page-good"].Source
				}
				if mode == "enable" || mode == "enable-denied" {
					source = "<!-- mdchk:enable MD011 -->\n" + tc.Source
					conf = strings.Replace(conf, "MD011: {enabled: true}", "MD011: {enabled: false}", 1)
				}
				if mode == "denied" {
					conf = strings.Replace(conf, "MD011: {enabled: true}", "MD011: {enabled: true, allow_directives: false}", 1)
				}
				if mode == "enable-denied" {
					conf = strings.Replace(conf, "MD011: {enabled: false}", "MD011: {enabled: false, allow_directives: false}", 1)
				}
				if mode == "unknown" {
					source += "\n\n<div>\n<x title=\"\n\n" + tc.Source
				}
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", source)
				before := acceptance.ReadinessState(t, root)
				got := acceptance.MD014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
				out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "lint")
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
					} else if d.Rule != "MD011" || d.Message != base.Findings[0].Message || d.Range != (result.Range{Start: result.Position{Line: 2, Column: 1}, End: result.Position{Line: 2, Column: 29}}) || d.FixAvailable {
						t.Fatal(d)
					}
					if d.Snapshot != (result.Snapshot{View: "original", SizeBytes: len(source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}) {
						t.Fatal(d)
					}
					if d.Rule == "MD011" {
						start := strings.Index(source, "\n") + 1
						if d.ByteRange != (result.ByteRange{Start: start, End: start + len(base.Source)}) {
							t.Fatal(d)
						}
					}
				}
				for _, wire := range out.SuppressedDiagnostics {
					var h result.SuppressedDiagnostic
					if err := json.Unmarshal(wire, &h); err != nil {
						t.Fatal(err)
					}
					if h.Diagnostic.Rule != "MD011" || h.Diagnostic.ByteRange != (result.ByteRange{Start: 29, End: 57}) || h.SuppressedBy.Kind != "directive" || h.Diagnostic.FixAvailable || h.Diagnostic.Message != base.Findings[0].Message || h.Diagnostic.Range != (result.Range{Start: result.Position{Line: 2, Column: 1}, End: result.Position{Line: 2, Column: 29}}) || h.Diagnostic.Snapshot != (result.Snapshot{View: "original", SizeBytes: len(source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}) {
						t.Fatal(h)
					}
				}
				for _, c := range out.Coverage {
					if c.Reason != result.ContextUnavailable {
						t.Fatal(c)
					}
				}
				acceptance.ReadinessAssertEqual(t, "MD011 directive controls", before, acceptance.ReadinessState(t, root))
			})
		}
	}
}
func md011NativeConfig(flavor string, tc md011NativeLiteral) string {
	c := strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
	value := "{enabled: true}"
	if tc.Options.Enabled != nil {
		value = fmt.Sprintf("{enabled: %v}", *tc.Options.Enabled)
	}
	if tc.Options.Empty {
		value = "{}"
	}
	c = strings.Replace(c, "MD011: {enabled: false}", "MD011: "+value, 1)
	if tc.Options.Omitted && !tc.Options.Empty {
		c = strings.Replace(c, "  MD011: "+value+"\n", "", 1)
	}
	if tc.Options.Frontmatter != "" {
		c += "frontmatter: {enabled: true, formats: [" + tc.Options.Frontmatter + "]}\n"
	}
	return c
}
func md011NativeAssert(t *testing.T, out result.Result, tc md011NativeLiteral, view string) {
	t.Helper()
	if !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.DeferredFixes)+len(out.Documents)+len(out.SuppressedDiagnostics) != 0 || len(out.Diagnostics) != len(tc.Findings) {
		t.Fatal(out)
	}
	for _, change := range out.Changes {
		if slices.Contains(change.Rules, "MD011") {
			t.Fatal("manual rule proposed a change", change)
		}
	}
	for i, d := range out.Diagnostics {
		w := tc.Findings[i]
		if d.Rule != "MD011" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] || d.Snapshot != (result.Snapshot{View: view, SizeBytes: len(tc.Source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source)))}) {
			t.Fatal(d, w)
		}
	}
}
func TestCompiledPlatformMD011LiteralLint(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	cases := md011NativeLiterals(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, name := range md011NativeNames(cases) {
			if !slices.Contains(cases[name].Flavors, flavor) {
				continue
			}
			t.Run(flavor+"/"+name, func(t *testing.T) {
				tc, ok := cases[name]
				if !ok {
					t.Fatal(name)
				}
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", md011NativeConfig(flavor, tc))
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				before := acceptance.ReadinessState(t, root)
				got := acceptance.MD014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
				out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "lint")
				want := 0
				if len(tc.Findings) > 0 {
					want = 10
				}
				if got.Status != want || len(out.Changes) != 0 {
					t.Fatal(got, out)
				}
				md011NativeAssert(t, out, tc, "original")
				acceptance.ReadinessAssertEqual(t, "MD011 literal analysis", before, acceptance.ReadinessState(t, root))
			})
		}
	}
}

type md011InvalidLiteral struct {
	Name string
	YAML string `json:"yaml"`
}

func md011NativeInvalid(t *testing.T) []md011InvalidLiteral {
	t.Helper()
	b, e := os.ReadFile("testdata/md011.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "b6e3e408b33fea1ec53de36b79ed6058ac45590c28526854261632685d3dcd29" {
		t.Fatal(e)
	}
	var v struct {
		Invalid []md011InvalidLiteral `json:"invalid_configurations"`
	}
	if e = json.Unmarshal(b, &v); e != nil || len(v.Invalid) != 13 {
		t.Fatal(e)
	}
	names := map[string]bool{}
	for _, row := range v.Invalid {
		if row.Name == "" || row.YAML == "" || names[row.Name] {
			t.Fatal("invalid configuration literal identity", row)
		}
		names[row.Name] = true
	}
	return v.Invalid
}

func TestCompiledPlatformMD011InvalidAdmission(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	tc := md011NativeLiterals(t)["page-bad"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, command := range []string{"lint", "fix"} {
			for _, row := range md011NativeInvalid(t) {
				t.Run(flavor+"/"+command+"/"+row.Name, func(t *testing.T) {
					root := acceptance.Project(t)
					// The oracle encodes YAML line separators as literal backslash-n.
					// Expand those separators while retaining its continuation
					// indentation so duplicate-rule tests duplicate mapping keys.
					yaml := strings.ReplaceAll(row.YAML, `\n`, "\n")
					conf := strings.Replace(md011NativeConfig(flavor, tc), "MD011: {enabled: true}", yaml, 1)
					acceptance.PutFile(t, root, ".mdchk.yaml", conf)
					before := acceptance.ReadinessState(t, root)
					got := acceptance.MD014NativeRun(t, binary, root, command, "missing.md", "--format=json")
					out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), command)
					if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles)+len(out.Files)+len(out.Diagnostics)+len(out.Changes)+len(out.SuppressedDiagnostics)+len(out.Coverage)+len(out.DeferredFixes)+len(out.Documents) != 0 {
						t.Fatal(got, out)
					}
					for _, e := range out.Errors {
						if e.Category != result.Configuration {
							t.Fatal(e)
						}
					}
					acceptance.ReadinessAssertEqual(t, "MD011 invalid preflight", before, acceptance.ReadinessState(t, root))
				})
			}
		}
	}
}
func TestCompiledPlatformMD011NoCGO(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedNoCGOExecutable(t)
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

	tc := md011NativeLiterals(t)["page-bad"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", md011NativeConfig(flavor, tc))
			acceptance.PutFile(t, root, "doc.md", tc.Source)
			before := acceptance.ReadinessState(t, root)
			for _, command := range []string{"lint", "inspect"} {
				got := acceptance.MD014NativeRun(t, binary, root, command, "doc.md", "--format=json")
				out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), command)
				want := 10
				if command == "inspect" {
					want = 0
				}
				if got.Status != want || len(out.Changes) != 0 {
					t.Fatal(got, out)
				}
				md011NativeAssert(t, out, tc, "original")
			}
			validated := acceptance.MD014NativeRun(t, binary, root, "config", "validate", "--format=json")
			config := acceptance.MD014NativeJSON(t, []byte(validated.Stdout), "config validate")
			if validated.Status != 0 || !config.Complete || len(config.Errors) != 0 || config.ConfigValidation == nil || !config.ConfigValidation.ScanComplete || config.ConfigValidation.ValidDefinitions != 1 {
				t.Fatal(validated, config)
			}
			preview := acceptance.MD014NativeRun(t, binary, root, "fix", "doc.md", "--dry-run", "--format=json")
			previewResult := acceptance.MD014NativeJSON(t, []byte(preview.Stdout), "fix")
			if preview.Status != 10 || len(previewResult.Changes) != 0 {
				t.Fatal(preview, previewResult)
			}
			md011NativeAssert(t, previewResult, tc, "candidate")
			got := acceptance.MD014NativeRun(t, binary, root, "fix", "doc.md", "--format=json")
			out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "fix")
			acceptance.AssertCGO0Apply(t, got, out, before, acceptance.ReadinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD011ConfiguredOffInspection(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	tc := md011NativeLiterals(t)["explicit-disabled"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", md011NativeConfig(flavor, tc))
			acceptance.PutFile(t, root, "doc.md", tc.Source)
			before := acceptance.ReadinessState(t, root)
			got := acceptance.MD014NativeRun(t, binary, root, "inspect", "doc.md", "--format=json")
			out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "inspect")
			if got.Status != 0 || len(out.SuppressedDiagnostics) != 0 {
				t.Fatal(got, out)
			}
			md011NativeAssert(t, out, tc, "original")
			var projection inspection.Projection
			if err := json.Unmarshal(out.Inspection, &projection); err != nil || projection.Rules.Data == nil {
				t.Fatal(err, projection)
			}
			found := false
			for _, state := range *projection.Rules.Data {
				if state.Rule == "MD011" {
					found = true
					if state.ConfiguredEnabled || state.ActiveEvaluation != "skipped" || state.ObservationalEvaluation != "not-requested" {
						t.Fatal(state)
					}
				}
			}
			if !found {
				t.Fatal("missing MD011 evaluation")
			}
			acceptance.ReadinessAssertEqual(t, "native configured-off observation is not requested", before, acceptance.ReadinessState(t, root))
		})
	}
}

func md011NativeNames(cases map[string]md011NativeLiteral) []string {
	names := make([]string, 0, len(cases))
	for name := range cases {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestCompiledPlatformMD011ReportersAndManualRefusal(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	tc := md011NativeLiterals(t)["page-bad"]
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, format := range []string{"text", "json", "vscode", "github"} {
			t.Run(flavor+"/"+format, func(t *testing.T) {
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", md011NativeConfig(flavor, tc))
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				before := acceptance.ReadinessState(t, root)
				got := acceptance.MD014NativeRun(t, binary, root, "lint", "doc.md", "--format="+format)
				if got.Status != 10 || !strings.Contains(got.Stdout, "MD011") || !strings.Contains(got.Stdout, tc.Findings[0].Message) {
					t.Fatal(got)
				}
				if format == "json" {
					report := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "lint")
					if len(report.Changes) != 0 {
						t.Fatal(report.Changes)
					}
					md011NativeAssert(t, report, tc, "original")
				}
				got = acceptance.MD014NativeRun(t, binary, root, "fix", "doc.md", "--format=json")
				out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "fix")
				if got.Status == 13 {
					if !acceptance.MD038NativeRefusal(t, got, out) || len(out.Changes)+len(out.Documents)+len(out.Diagnostics) != 0 {
						t.Fatal(got, out)
					}
				} else {
					if got.Status != 10 || len(out.Changes) != 0 {
						t.Fatal(got, out)
					}
					md011NativeAssert(t, out, tc, "saved")
				}
				acceptance.ReadinessAssertEqual(t, "manual-only apply or unsupported refusal", before, acceptance.ReadinessState(t, root))
			})
		}
	}
}

func TestCompiledPlatformMD011LiteralInspection(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	cases := md011NativeLiterals(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, name := range md011NativeNames(cases) {
			if !slices.Contains(cases[name].Flavors, flavor) {
				continue
			}
			t.Run(flavor+"/"+name, func(t *testing.T) {
				tc, ok := cases[name]
				if !ok {
					t.Fatal(name)
				}
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", md011NativeConfig(flavor, tc))
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				before := acceptance.ReadinessState(t, root)
				got := acceptance.MD014NativeRun(t, binary, root, "inspect", "doc.md", "--format=json")
				out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "inspect")
				want := 0

				if got.Status != want || len(out.Changes) != 0 {
					t.Fatal(got, out)
				}
				md011NativeAssert(t, out, tc, "original")
				acceptance.ReadinessAssertEqual(t, "MD011 literal analysis", before, acceptance.ReadinessState(t, root))
			})
		}
	}
}
func TestCompiledPlatformMD011InheritedDirectiveDenial(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprintf("global-%v", global), func(t *testing.T) {
			root := acceptance.Project(t)
			child := strings.Replace(md011NativeConfig("gfm", md011NativeLiteral{}), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			parent := strings.Replace(child, "MD011: {enabled: true}", "MD011: {enabled: true, allow_directives: false}", 1)
			if global {
				parent = strings.Replace(child, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			acceptance.PutFile(t, root, ".mdchk.yaml", parent)
			acceptance.PutFile(t, root, "nested/.mdchk.yaml", child)
			source := "<!-- mdchk:disable MD011 -->\n(Guide)[https://example.org]"
			acceptance.PutFile(t, root, "nested/doc.md", source)
			before := acceptance.ReadinessState(t, root)
			got := acceptance.MD014NativeRun(t, binary, filepath.Join(root, "nested"), "inspect", "doc.md", "--format=json")
			out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "inspect")
			if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 1 || len(out.SuppressedDiagnostics)+len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(got, out)
			}
			tc := md011NativeLiterals(t)["page-bad"]
			tc.Source = source
			// Copy before shifting so the independent literal remains immutable.
			tc.Findings = slices.Clone(tc.Findings)
			tc.Findings[0].Bytes = [2]int{29, 57}
			tc.Findings[0].Start = [2]int{2, 1}
			tc.Findings[0].End = [2]int{2, 29}
			md011NativeAssert(t, out, tc, "original")
			if len(out.Changes) != 0 {
				t.Fatal(out.Changes)
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
			acceptance.ReadinessAssertEqual(t, "MD011 inherited permission cannot be relaxed", before, acceptance.ReadinessState(t, root))
		})
	}
}
