package md031_test

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
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

type md031Literal struct {
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
		Side, Message     string
	}
}

func md031Literals(t *testing.T) []md031Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md031.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "8972795f62e028ac78f3574b4723572988300140e007474bca298e3d2f796876" {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md031Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 57 {
		t.Fatal(e, len(v.Cases))
	}
	return v.Cases
}

func TestMD031FrozenProductionOracle(t *testing.T) {
	for _, tc := range md031Literals(t) {
		for _, flavor := range tc.Flavors {
			for _, inspect := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/inspect-%v", tc.Name, flavor, inspect), func(t *testing.T) {
					if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
						t.Fatal("source identity")
					}
					s := ruletest.Settings("MD031")
					setting := s["MD031"]
					if tc.Options.Enabled != nil {
						setting.Enabled = tc.Options.Enabled
					}
					if tc.Options.Omitted {
						setting.Enabled = nil
					}
					if tc.Options.Empty {
						setting.Options = engine.Options{}
					}
					s["MD031"] = setting
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
						if d.Rule != "MD031" || d.Message != w.Message || d.Path != "literal.md" || d.FixAvailable || d.ByteRange.Start != w.Bytes[0] || d.ByteRange.End != w.Bytes[1] || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] {
							t.Fatal(d, w)
						}
					}
					if inspect {
						found := false
						for _, ev := range trace.Evaluations() {
							if ev.Rule == "MD031" {
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

func TestMD031AvailabilityAndNoOptions(t *testing.T) {
	r := ruletest.Registry(t, rules.Registrations())
	s, e := r.ResolveSettings(nil, engine.ValidationProviders{})
	if e != nil || !s["MD031"].Available || !s["MD031"].Enabled || len(s["MD031"].Options) != 0 {
		t.Fatal(s["MD031"], e)
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD031" && (m.Fix != engine.PlannedFix || !m.RequiresSyntax || len(m.Options) != 0) {
			t.Fatal(m)
		}
	}
}

func TestMD031DirectivesSharedObservationAndInactiveDemand(t *testing.T) {
	for _, tc := range []struct {
		name, source                  string
		allow, enabled, inactive      bool
		visible, hidden, audit, calls int
	}{
		{"used-both", "<!-- mdchk:disable MD031 -->\n```\nx\n```\nTail", true, true, false, 0, 2, 0, 1},
		{"denied", "<!-- mdchk:disable MD031 -->\n```\nx\n```\nTail", false, true, false, 2, 0, 0, 1},
		{"unused", "<!-- mdchk:disable MD031 -->\n\n```\nx\n```", true, true, false, 0, 0, 1, 1},
		{"disabled", "x\n```\n```", true, false, false, 0, 0, 0, 0},
		{"inactive", "x\n```\n```", true, false, true, 0, 1, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			regs := rules.Registrations()
			for i := range regs {
				if regs[i].ID == "MD031" {
					actual := regs[i].Check
					regs[i].Check = func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome { calls++; return actual(ctx, c) }
				}
			}
			s := ruletest.Settings("MD031")
			on := true
			s["MD031"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allow}
			s["MD087"] = engine.RuleSetting{Enabled: &on}
			sel, e := ruletest.Registry(t, regs).SelectRules(s, engine.ValidationProviders{})
			if e != nil {
				t.Fatal(e)
			}
			p, e := sel.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
			if e != nil {
				t.Fatal(e)
			}
			in, e := p.NewInput("literal.md", []byte(tc.source))
			if e != nil {
				t.Fatal(e)
			}
			out, tr, e := p.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{ObserveInactive: tc.inactive})
			if e != nil || !out.Complete || calls != tc.calls {
				t.Fatal(out, e, calls)
			}
			visible, audit, hidden := 0, 0, 0
			for _, d := range out.Diagnostics {
				switch d.Rule {
				case "MD031":
					visible++
				case "MD087":
					audit++
				default:
					t.Fatal(d)
				}
			}
			if tc.inactive {
				// Requested inactive observations belong to the trace, separate from
				// directive-hidden active results retained in the result envelope.
				for _, h := range tr.Suppressed() {
					if h.Diagnostic.Rule == "MD031" {
						hidden++
						if h.Diagnostic.FixAvailable || h.Diagnostic.ByteRange != (result.ByteRange{Start: 2, End: 2}) {
							t.Fatal(h)
						}
					}
				}
				if len(out.SuppressedDiagnostics) != 0 {
					t.Fatal(out.SuppressedDiagnostics)
				}
			} else {
				if len(tr.Suppressed()) != 0 {
					t.Fatal("unexpected inactive observations", tr.Suppressed())
				}
				for i, raw := range out.SuppressedDiagnostics {
					var h result.SuppressedDiagnostic
					if e := json.Unmarshal(raw, &h); e != nil {
						t.Fatal(e)
					}
					if h.Diagnostic.Rule != "MD031" || h.Diagnostic.FixAvailable || h.SuppressedBy.Kind != "directive" {
						t.Fatal(h)
					}
					hidden++
					if tc.name == "used-both" {
						points := []int{29, 38}
						messages := []string{"Fenced code block requires a blank body line before it.", "Fenced code block requires a blank body line after it."}
						if i >= len(points) || h.Diagnostic.ByteRange != (result.ByteRange{Start: points[i], End: points[i]}) || h.Diagnostic.Message != messages[i] {
							t.Fatal(h)
						}
					}
				}
			}
			if visible != tc.visible || hidden != tc.hidden || audit != tc.audit {
				t.Fatal(visible, hidden, audit, out)
			}
		})
	}
}

func TestMD031UnknownContextWithholdsObservation(t *testing.T) {
	for _, audit := range []bool{false, true} {
		t.Run(fmt.Sprintf("audit-%v", audit), func(t *testing.T) {
			source := "x\n```\n```\n\n<div>\n<x title=\"\n\n~~~\nx\n~~~"
			s := ruletest.Settings("MD031")
			if audit {
				source = "<!-- mdchk:disable MD031 -->\n" + source
				s["MD087"] = engine.RuleSetting{Enabled: &audit}
			}
			out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), source, s, engine.DocumentContext{}, engine.RunOptions{})
			want := 1
			if audit {
				want = 2
			}
			if out.Complete || len(out.Errors)+len(out.Diagnostics)+len(out.SuppressedDiagnostics) != 0 || len(out.Coverage) != want {
				t.Fatal(out)
			}
			for _, c := range out.Coverage {
				if c.Reason != result.ContextUnavailable || (c.Rule != "MD031" && c.Rule != "MD087") {
					t.Fatal(c)
				}
			}
		})
	}
}

func TestMD031RepeatedDisableLastOwner(t *testing.T) {
	source := "<!-- mdchk:disable MD031 -->\n<!-- mdchk:disable MD031 -->\n```\nx\n```"
	s := ruletest.Settings("MD031")
	on := true
	s["MD087"] = engine.RuleSetting{Enabled: &on}
	sel, e := ruletest.Registry(t, rules.Registrations()).SelectRules(s, engine.ValidationProviders{})
	if e != nil {
		t.Fatal(e)
	}
	plan, e := sel.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	in, e := plan.NewInput("literal.md", []byte(source))
	if e != nil {
		t.Fatal(e)
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	out, trace, e := plan.Inspect(context.Background(), in, engine.RunOptions{Budget: budget}, engine.InspectionOptions{})
	if e != nil || !out.Complete || len(out.Diagnostics) != 1 || len(out.SuppressedDiagnostics) != 1 || len(out.Errors)+len(out.Coverage) != 0 {
		t.Fatal(out, e)
	}
	unused := out.Diagnostics[0]
	if unused.Rule != "MD087" || unused.Message != "Suppression for MD031 hides no findings." || unused.ByteRange != (result.ByteRange{Start: 19, End: 24}) {
		t.Fatal(unused)
	}
	var wire result.SuppressedDiagnostic
	if e := json.Unmarshal(out.SuppressedDiagnostics[0], &wire); e != nil {
		t.Fatal(e)
	}
	if wire.SuppressedBy.Kind != "directive" || wire.Diagnostic.Rule != "MD031" || wire.Diagnostic.ByteRange != (result.ByteRange{Start: 58, End: 58}) {
		t.Fatal(wire)
	}
	var hidden []engine.DirectiveSuppression
	for _, record := range out.AnalysisRecords() {
		if a, ok := record.(engine.DirectiveAnalysis); ok {
			hidden = append(hidden, a.Suppressed()...)
		}
	}
	if len(hidden) != 1 || hidden[0].Owner != 1 || hidden[0].Token.Start() != 48 || hidden[0].Token.End() != 53 || hidden[0].Diagnostic.Rule != "MD031" || hidden[0].Diagnostic.ByteRange != wire.Diagnostic.ByteRange {
		t.Fatal(hidden)
	}
	if len(trace.Suppressed()) != 0 {
		t.Fatal("active findings are not inactive observations", trace.Suppressed())
	}
	decisions, e := trace.SuppressionDecisions(context.Background(), budget)
	if e != nil || len(decisions) != 2 {
		t.Fatal(decisions, e)
	}
	for i, d := range decisions {
		reason := "suppression-unused"
		start := 19
		if i == 1 {
			reason = "suppression-used"
			start = 48
		}
		if d.Owner != i || d.Effect != 0 || d.Rule != "MD031" || d.Reason != reason || d.Token.Start() != start || d.Token.End() != start+5 || d.Token.SourceToken() != in.Source().Token() {
			t.Fatal(d)
		}
	}
}
