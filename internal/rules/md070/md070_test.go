package md070_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

const md070Example = "<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->"

func TestMD070AvailabilityAndOptions(t *testing.T) {
	r := ruletest.Registry(t, rules.Registrations())
	s, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil || !s["MD070"].Available || s["MD070"].Enabled || len(s["MD070"].Options) != 0 {
		t.Fatal(s["MD070"], err)
	}
	for _, meta := range r.Metadata() {
		if meta.ID == "MD070" && (meta.Fix != engine.PlannedFix || !meta.RequiresSyntax || len(meta.Options) != 0) {
			t.Fatal(meta)
		}
	}
}

func TestMD070SuppressionInactiveAndRepeatedOwnership(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"used", "denied", "unused", "inactive", "repeated"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				source := "<!-- mdchk:disable MD070 -->\n" + md070Example
				if mode == "unused" {
					source = "<!-- mdchk:disable MD070 -->\n<!-- mdchk-example -->\n```md\n~~~go\nx\n~~~\n```\n<!-- /mdchk-example -->"
				}
				if mode == "repeated" {
					source = "<!-- mdchk:disable MD070 -->\n" + source
				}
				if mode == "inactive" {
					source = md070Example
				}
				s := md070Settings()
				on, allow, enabled := true, mode != "denied", mode != "inactive"
				s["MD070"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow}
				s["MD087"] = engine.RuleSetting{Enabled: &on}
				sel, err := ruletest.Registry(t, rules.Registrations()).SelectRules(s, engine.ValidationProviders{})
				if err != nil {
					t.Fatal(err)
				}
				plan, err := sel.Plan(engine.DocumentContext{Flavor: flavor}, nil)
				if err != nil {
					t.Fatal(err)
				}
				in, err := plan.NewInput("literal.md", []byte(source))
				if err != nil {
					t.Fatal(err)
				}
				out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{ObserveInactive: mode == "inactive"})
				if err != nil || !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(out, err)
				}
				visible, audit := 0, 0
				for _, d := range out.Diagnostics {
					if d.Rule == "MD070" {
						visible++
						if d.Range.Start.Line != 3 || d.Range.Start.Column != 1 || d.Range.End.Column != 4 || d.Message != "Outer Markdown fence must be longer than every matching inner fence." || d.FixAvailable {
							t.Fatal(d)
						}
					} else if d.Rule == "MD087" {
						audit++
					} else {
						t.Fatal(d)
					}
				}
				wantVisible, wantHidden, wantAudit := 0, 1, 0
				if mode == "denied" {
					wantVisible, wantHidden = 1, 0
				}
				if mode == "unused" {
					wantHidden, wantAudit = 0, 1
				}
				if mode == "repeated" {
					wantAudit = 1
				}
				if mode == "inactive" {
					count := 0
					for _, observation := range trace.Suppressed() {
						if observation.Diagnostic.Rule != "MD070" {
							continue
						}
						count++
						if observation.Diagnostic.ByteRange != (result.ByteRange{Start: 23, End: 26}) {
							t.Fatal(observation)
						}
					}
					if count != 1 {
						t.Fatal("MD070 inactive observation count", count)
					}
					wantHidden = 0
				}
				if visible != wantVisible || audit != wantAudit || len(out.SuppressedDiagnostics) != wantHidden {
					t.Fatal(out)
				}
				for _, wire := range out.SuppressedDiagnostics {
					var hidden result.SuppressedDiagnostic
					if err := json.Unmarshal(wire, &hidden); err != nil {
						t.Fatal(err)
					}
					line, start := 3, 52
					if mode == "repeated" {
						line, start = 4, 81
					}
					if hidden.Diagnostic.Rule != "MD070" || hidden.Diagnostic.ByteRange != (result.ByteRange{Start: start, End: start + 3}) || hidden.Diagnostic.Range.Start.Line != line || hidden.SuppressedBy.Kind != "directive" {
						t.Fatal(hidden)
					}
				}
				if mode == "repeated" {
					var hidden []engine.DirectiveSuppression
					for _, record := range out.AnalysisRecords() {
						if a, ok := record.(engine.DirectiveAnalysis); ok {
							hidden = append(hidden, a.Suppressed()...)
						}
					}
					if len(hidden) != 1 || hidden[0].Owner != 1 || hidden[0].Token.Start() != 48 || hidden[0].Token.End() != 53 {
						t.Fatal(hidden)
					}
				}
			})
		}
	}
}

func TestMD070ConfiguredOffEnableAndDenial(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow-%v", allow), func(t *testing.T) {
			off := false
			s := md070Settings()
			s["MD070"] = engine.RuleSetting{Enabled: &off, AllowDirectives: &allow}
			out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), "<!-- mdchk:enable MD070 -->\n"+md070Example, s, engine.DocumentContext{}, engine.RunOptions{})
			want := 0
			if allow {
				want = 1
			}
			if !out.Complete || len(out.Diagnostics) != want || len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(out)
			}
			if allow && (out.Diagnostics[0].Range != (result.Range{Start: result.Position{Line: 3, Column: 1}, End: result.Position{Line: 3, Column: 4}})) {
				t.Fatal(out)
			}
		})
	}
}

func TestMD070UnknownContextWithholdsObservation(t *testing.T) {
	for _, audit := range []bool{false, true} {
		t.Run(fmt.Sprintf("audit-%v", audit), func(t *testing.T) {
			s := md070Settings()
			source := md070Example + "\n\n<div>\n<x title=\"\n\n" + md070Example
			if audit {
				on := true
				s["MD087"] = engine.RuleSetting{Enabled: &on}
				source = "<!-- mdchk:disable MD070 -->\n" + source
			}
			out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), source, s, engine.DocumentContext{}, engine.RunOptions{})
			want := 1
			if audit {
				want = 2
			}
			if out.Complete || len(out.Diagnostics)+len(out.Errors)+len(out.SuppressedDiagnostics) != 0 || len(out.Coverage) != want {
				t.Fatal(out)
			}
			for _, c := range out.Coverage {
				if c.Reason != result.ContextUnavailable {
					t.Fatal(c)
				}
			}
		})
	}
}

func TestMD070OriginalParsingAndDocumentIsolation(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			source := "<!-- mdchk-example -->\n```markdown\n```python\nprint(1)\n```\n```\n<!-- /mdchk-example -->"
			var baseline []result.Diagnostic
			for _, enabled := range []bool{false, true} {
				s := md070Settings()
				on := true
				s["MD070"] = engine.RuleSetting{Enabled: &enabled}
				s["MD040"] = engine.RuleSetting{Enabled: &on}
				out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), source, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Errors)+len(out.Coverage) != 0 {
					t.Fatal(out)
				}
				var original []result.Diagnostic
				for _, d := range out.Diagnostics {
					if d.Rule == "MD040" {
						d.ID = "" // Envelope numbering changes when another rule adds a finding.
						original = append(original, d)
					}
				}
				if !enabled {
					baseline = original
				} else if !reflect.DeepEqual(baseline, original) {
					t.Fatal(baseline, original)
				}
			}
			if len(baseline) != 1 {
				t.Fatal("original final fence language precondition", baseline)
			}
			r := ruletest.Registry(t, rules.Registrations())
			a, err := engine.NewInput("a.md", []byte(md070Example), md070Settings(), engine.DocumentContext{Flavor: flavor})
			if err != nil {
				t.Fatal(err)
			}
			b, err := engine.NewInput("b.md", []byte(strings.TrimSuffix(md070Example, "<!-- /mdchk-example -->")), md070Settings(), engine.DocumentContext{Flavor: flavor})
			if err != nil {
				t.Fatal(err)
			}
			out, err := r.Lint(context.Background(), []engine.Input{a, b}, engine.RunOptions{})
			if err != nil || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Path != "a.md" {
				t.Fatal(out, err)
			}
		})
	}
}

func TestMD070DoesNotExecutePayloadDirectives(t *testing.T) {
	source := "<!-- mdchk-example -->\n```md\n```go\n<!-- mdchk:disable MD040 -->\nx\n```\n<!-- /mdchk-example -->"
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enabled-%v", flavor, enabled), func(t *testing.T) {
				s := md070Settings()
				on := true
				s["MD070"] = engine.RuleSetting{Enabled: &enabled}
				s["MD040"] = engine.RuleSetting{Enabled: &on}
				selection, err := ruletest.Registry(t, rules.Registrations()).SelectRules(s, engine.ValidationProviders{})
				if err != nil {
					t.Fatal(err)
				}
				plan, err := selection.Plan(engine.DocumentContext{Flavor: flavor}, nil)
				if err != nil {
					t.Fatal(err)
				}
				in, err := plan.NewInput("literal.md", []byte(source))
				if err != nil {
					t.Fatal(err)
				}
				out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{})
				if err != nil || !out.Complete || len(trace.Directives().Candidates()) != 0 || len(out.SuppressedDiagnostics)+len(out.Errors)+len(out.Coverage) != 0 {
					t.Fatal(out, err, trace.Directives().Candidates())
				}
				want := 0
				if enabled {
					want = 1
				}
				if len(out.Diagnostics) != want {
					t.Fatal(out)
				}
			})
		}
	}
}

type md070Literal struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Options      struct {
		Enabled     *bool
		Omitted     bool `json:"enabled_omitted"`
		Empty       bool `json:"empty_options"`
		Frontmatter string
	}
	Findings []struct {
		Bytes, Start, End [2]int
		Expected, Message string
	}
}

func md070Literals(t *testing.T) []md070Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md070.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "9e3ca955c4cca090d3f80175d6195cf2779e24578520f64c3a2c79bab365f4be" {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md070Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 88 {
		t.Fatal(e, len(v.Cases))
	}
	return v.Cases
}

func md070Settings() engine.Settings {
	s := ruletest.Settings("MD048")
	off, on := false, true
	s["MD048"] = engine.RuleSetting{Enabled: &off}
	s["MD070"] = engine.RuleSetting{Enabled: &on}
	return s
}

func TestMD070FrozenProductionOracle(t *testing.T) {
	for _, tc := range md070Literals(t) {
		for _, flavor := range tc.Flavors {
			for _, inspect := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/inspect-%v", tc.Name, flavor, inspect), func(t *testing.T) {
					if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
						t.Fatal("source identity")
					}
					s := md070Settings()
					setting := s["MD070"]
					if tc.Options.Enabled != nil {
						setting.Enabled = tc.Options.Enabled
					}
					if tc.Options.Omitted {
						setting.Enabled = nil
					}
					if tc.Options.Empty {
						setting.Options = engine.Options{}
					}
					s["MD070"] = setting
					if tc.Options.Omitted && !tc.Options.Empty {
						delete(s, "MD070")
					}
					dc := engine.DocumentContext{Flavor: flavor}
					if tc.Options.Frontmatter != "" {
						var e error
						dc.Frontmatter, e = parser.NewFrontmatterOptions(true, []string{tc.Options.Frontmatter})
						if e != nil {
							t.Fatal(e)
						}
					}
					selection, e := ruletest.Registry(t, rules.Registrations()).SelectRules(s, engine.ValidationProviders{})
					if e != nil {
						t.Fatal(e)
					}
					plan, e := selection.Plan(dc, nil)
					if e != nil {
						t.Fatal(e)
					}
					original := []byte(tc.Source)
					in, e := plan.NewInput("literal.md", original)
					if e != nil {
						t.Fatal(e)
					}
					token := in.Source().Token()
					var out result.Result
					var trace *engine.InspectionTrace
					if inspect {
						out, trace, e = plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{})
					} else {
						out, e = plan.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
					}
					if e != nil || !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes)+len(out.Interruptions)+len(out.Omissions)+len(out.SuppressedDiagnostics) != 0 || string(original) != tc.Source || string(in.Source().Bytes()) != tc.Source || in.Source().Token() != token {
						t.Fatal(out, e)
					}
					if len(out.Files) != 1 || out.Files[0].WriteStatus != "not-requested" || len(out.Files[0].ChangeIDs) != 0 || !reflect.DeepEqual(out.CheckedFiles, []string{"literal.md"}) {
						t.Fatal(out.Files)
					}
					status := "clean"
					if len(tc.Findings) > 0 {
						status = "violations"
					}
					if out.Status != status || len(out.Diagnostics) != len(tc.Findings) {
						t.Fatal(out.Status, out.Diagnostics, tc.Findings)
					}
					for i, d := range out.Diagnostics {
						w := tc.Findings[i]
						if d.Rule != "MD070" || d.Message != w.Message || d.Path != "literal.md" || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}) || d.FixAvailable || d.ByteRange.Start != w.Bytes[0] || d.ByteRange.End != w.Bytes[1] || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] {
							t.Fatal(d, w)
						}
					}
					if inspect {
						found := false
						for _, ev := range trace.Evaluations() {
							if ev.Rule == "MD070" {
								found = true
								if tc.Options.Omitted || tc.Options.Empty || tc.Options.Enabled != nil && !*tc.Options.Enabled {
									if ev.Active != "skipped" {
										t.Fatal(ev)
									}
								} else if ev.Active != "complete" {
									t.Fatal(ev)
								}
							}
						}
						if !found {
							t.Fatal("evaluation missing")
						}
					}
				})
			}
		}
	}
}
