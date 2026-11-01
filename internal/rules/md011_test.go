package rules

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

const md011Example = "(Guide)[https://example.org]"

func TestMD011AvailabilityAndOptions(t *testing.T) {
	r := registry(t, Registrations())
	s, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil || !s["MD011"].Available || !s["MD011"].Enabled || len(s["MD011"].Options) != 0 {
		t.Fatal(s["MD011"], err)
	}
	for _, meta := range r.Metadata() {
		if meta.ID == "MD011" && (meta.Fix != engine.PlannedFix || !meta.RequiresSyntax || len(meta.Options) != 0) {
			t.Fatal(meta)
		}
	}
}

func TestMD011SuppressionInactiveAndRepeatedOwnership(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"used", "denied", "unused", "inactive", "repeated"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				source := "<!-- mdchk:disable MD011 -->\n" + md011Example
				if mode == "unused" {
					source = "<!-- mdchk:disable MD011 -->\n[Guide](https://example.org)"
				}
				if mode == "repeated" {
					source = "<!-- mdchk:disable MD011 -->\n" + source
				}
				if mode == "inactive" {
					source = md011Example
				}
				s := md011Settings()
				on, allow, enabled := true, mode != "denied", mode != "inactive"
				s["MD011"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow}
				s["MD087"] = engine.RuleSetting{Enabled: &on}
				sel, err := registry(t, Registrations()).SelectRules(s, engine.ValidationProviders{})
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
					if d.Rule == "MD011" {
						visible++
						if d.Range.Start.Line != 2 || d.Range.Start.Column != 1 || d.Range.End.Column != 29 || d.Message != "Link label and destination delimiters are swapped." || d.FixAvailable {
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
						if observation.Diagnostic.Rule != "MD011" {
							continue
						}
						count++
						if observation.Diagnostic.ByteRange != (result.ByteRange{Start: 0, End: 28}) {
							t.Fatal(observation)
						}
					}
					if count != 1 {
						t.Fatal("MD011 inactive observation count", count)
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
					line, start := 2, 29
					if mode == "repeated" {
						line, start = 3, 58
					}
					if hidden.Diagnostic.Rule != "MD011" || hidden.Diagnostic.ByteRange != (result.ByteRange{Start: start, End: start + 28}) || hidden.Diagnostic.Range.Start.Line != line || hidden.SuppressedBy.Kind != "directive" {
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

func TestMD011ConfiguredOffEnableAndDenial(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow-%v", allow), func(t *testing.T) {
			off := false
			s := md011Settings()
			s["MD011"] = engine.RuleSetting{Enabled: &off, AllowDirectives: &allow}
			out := analyzeHeadings(t, registry(t, Registrations()), "<!-- mdchk:enable MD011 -->\n"+md011Example, s, engine.DocumentContext{}, engine.RunOptions{})
			want := 0
			if allow {
				want = 1
			}
			if !out.Complete || len(out.Diagnostics) != want || len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(out)
			}
			if allow && (out.Diagnostics[0].Range != (result.Range{Start: result.Position{Line: 2, Column: 1}, End: result.Position{Line: 2, Column: 29}})) {
				t.Fatal(out)
			}
		})
	}
}

func TestMD011UnknownContextWithholdsObservation(t *testing.T) {
	for _, audit := range []bool{false, true} {
		t.Run(fmt.Sprintf("audit-%v", audit), func(t *testing.T) {
			s := md011Settings()
			source := md011Example + "\n\n<div>\n<x title=\"\n\n" + md011Example
			if audit {
				on := true
				s["MD087"] = engine.RuleSetting{Enabled: &on}
				source = "<!-- mdchk:disable MD011 -->\n" + source
			}
			out := analyzeHeadings(t, registry(t, Registrations()), source, s, engine.DocumentContext{}, engine.RunOptions{})
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

func TestMD011OriginalParsingAndDocumentIsolation(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			source := md011Example + "\n\n```\n" + md011Example + "\n```"
			var baseline []result.Diagnostic
			for _, enabled := range []bool{false, true} {
				s := md011Settings()
				on := true
				s["MD011"] = engine.RuleSetting{Enabled: &enabled}
				s["MD040"] = engine.RuleSetting{Enabled: &on}
				out := analyzeHeadings(t, registry(t, Registrations()), source, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
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
				t.Fatal("original fence language precondition", baseline)
			}
			r := registry(t, Registrations())
			a, err := engine.NewInput("a.md", []byte(md011Example), md011Settings(), engine.DocumentContext{Flavor: flavor})
			if err != nil {
				t.Fatal(err)
			}
			b, err := engine.NewInput("b.md", []byte("[Guide](https://example.org)"), md011Settings(), engine.DocumentContext{Flavor: flavor})
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

type md011Literal struct {
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

func md011Literals(t *testing.T) []md011Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md011.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "b6e3e408b33fea1ec53de36b79ed6058ac45590c28526854261632685d3dcd29" {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md011Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 115 {
		t.Fatal(e, len(v.Cases))
	}
	return v.Cases
}

func md011Settings() engine.Settings {
	s := md048Settings()
	off, on := false, true
	s["MD048"] = engine.RuleSetting{Enabled: &off}
	s["MD011"] = engine.RuleSetting{Enabled: &on}
	return s
}

func TestMD011FrozenProductionOracle(t *testing.T) {
	for _, tc := range md011Literals(t) {
		for _, flavor := range tc.Flavors {
			for _, inspect := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/inspect-%v", tc.Name, flavor, inspect), func(t *testing.T) {
					if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
						t.Fatal("source identity")
					}
					s := md011Settings()
					setting := s["MD011"]
					if tc.Options.Enabled != nil {
						setting.Enabled = tc.Options.Enabled
					}
					if tc.Options.Omitted {
						setting.Enabled = nil
					}
					if tc.Options.Empty {
						setting.Options = engine.Options{}
					}
					s["MD011"] = setting
					if tc.Options.Omitted && !tc.Options.Empty {
						delete(s, "MD011")
					}
					dc := engine.DocumentContext{Flavor: flavor}
					if tc.Options.Frontmatter != "" {
						var e error
						dc.Frontmatter, e = parser.NewFrontmatterOptions(true, []string{tc.Options.Frontmatter})
						if e != nil {
							t.Fatal(e)
						}
					}
					selection, e := registry(t, Registrations()).SelectRules(s, engine.ValidationProviders{})
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
						if d.Rule != "MD011" || d.Message != w.Message || d.Path != "literal.md" || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}) || d.FixAvailable || d.ByteRange.Start != w.Bytes[0] || d.ByteRange.End != w.Bytes[1] || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] {
							t.Fatal(d, w)
						}
					}
					if inspect {
						found := false
						for _, ev := range trace.Evaluations() {
							if ev.Rule == "MD011" {
								found = true
								if tc.Options.Enabled != nil && !*tc.Options.Enabled {
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

func TestMD011DoesNotExecuteProtectedDirectives(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			s := md011Settings()
			on := true
			s["MD087"] = engine.RuleSetting{Enabled: &on}
			selected, err := registry(t, Registrations()).SelectRules(s, engine.ValidationProviders{})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := selected.Plan(engine.DocumentContext{Flavor: flavor}, nil)
			if err != nil {
				t.Fatal(err)
			}
			source := "```md\n<!-- mdchk:disable MD011 -->\n" + md011Example + "\n```\n\n" + md011Example
			in, err := plan.NewInput("literal.md", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{})
			if err != nil || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD011" || out.Diagnostics[0].Range.Start.Line != 6 || len(trace.Directives().Candidates()) != 0 || len(out.Errors)+len(out.Coverage)+len(out.SuppressedDiagnostics) != 0 {
				t.Fatal(out, err)
			}
		})
	}
}

func TestMD011DefaultAndEmptyOptionsEnabled(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, empty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/empty-%v", flavor, empty), func(t *testing.T) {
				s := md011Settings()
				delete(s, "MD011")
				if empty {
					s["MD011"] = engine.RuleSetting{Options: engine.Options{}}
				}
				out := analyzeHeadings(t, registry(t, Registrations()), md011Example, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD011" || out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 0, End: 28}) || out.Diagnostics[0].FixAvailable || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(out)
				}
			})
		}
	}
}
