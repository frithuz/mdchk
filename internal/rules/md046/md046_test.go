package md046_test

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

type md046Literal struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Options      struct {
		Enabled     *bool
		Omitted     bool `json:"enabled_omitted"`
		Empty       bool `json:"empty_options"`
		Frontmatter string
		Style       string
	}
	Findings []struct {
		Bytes, Start, End [2]int
		Expected, Message string
	}
}

func md046Literals(t *testing.T) []md046Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md046.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "1fff51e531a8c0c7958ef64c617a93df77933d3bb32e62ef75f2a54d4c4788fb" {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md046Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 64 {
		t.Fatal(e, len(v.Cases))
	}
	return v.Cases
}

func TestMD046FrozenProductionOracle(t *testing.T) {
	for _, tc := range md046Literals(t) {
		for _, flavor := range tc.Flavors {
			for _, inspect := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/inspect-%v", tc.Name, flavor, inspect), func(t *testing.T) {
					if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
						t.Fatal("source identity")
					}
					s := ruletest.Settings("MD046")
					setting := s["MD046"]
					if tc.Options.Enabled != nil {
						setting.Enabled = tc.Options.Enabled
					}
					if tc.Options.Omitted {
						setting.Enabled = nil
					}
					if tc.Options.Empty {
						setting.Options = engine.Options{}
					}
					if tc.Options.Style != "" {
						setting.Options = engine.Options{"style": tc.Options.Style}
					}
					s["MD046"] = setting
					if tc.Options.Omitted && !tc.Options.Empty {
						delete(s, "MD046")
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
						if d.Rule != "MD046" || d.Message != w.Message || d.Path != "literal.md" || d.FixAvailable || d.ByteRange.Start != w.Bytes[0] || d.ByteRange.End != w.Bytes[1] || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] {
							t.Fatal(d, w)
						}
					}
					if inspect {
						found := false
						for _, ev := range trace.Evaluations() {
							if ev.Rule == "MD046" {
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

func TestMD046AvailabilityAndOptions(t *testing.T) {
	r := ruletest.Registry(t, rules.Registrations())
	s, e := r.ResolveSettings(nil, engine.ValidationProviders{})
	if e != nil || !s["MD046"].Available || !s["MD046"].Enabled || len(s["MD046"].Options) != 1 {
		t.Fatal(s["MD046"], e)
	}
	style, ok := s["MD046"].Options["style"].(string)
	if !ok || style != "consistent" {
		t.Fatal(s["MD046"])
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD046" && (m.Fix != engine.PlannedFix || !m.RequiresSyntax || len(m.Options) != 1) {
			t.Fatal(m)
		}
	}
}

func TestMD046DirectivesSharedObservationAndInactiveDemand(t *testing.T) {
	for _, tc := range []struct {
		name, source                  string
		allow, enabled, inactive      bool
		visible, hidden, audit, calls int
	}{
		{"used", "<!-- mdchk:disable MD046 -->\n```\nx\n```\nTail", true, true, false, 0, 1, 0, 1},
		{"denied", "<!-- mdchk:disable MD046 -->\n```\nx\n```\nTail", false, true, false, 1, 0, 0, 1},
		{"unused", "<!-- mdchk:disable MD046 -->\n\n    x", true, true, false, 0, 0, 1, 1},
		{"disabled", "x\n```\n```", true, false, false, 0, 0, 0, 0},
		{"inactive", "x\n```\n```", true, false, true, 0, 1, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			regs := rules.Registrations()
			for i := range regs {
				if regs[i].ID == "MD046" {
					actual := regs[i].Check
					regs[i].Check = func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome { calls++; return actual(ctx, c) }
				}
			}
			s := ruletest.Settings("MD046")
			on := true
			s["MD046"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allow, Options: engine.Options{"style": "indented"}}
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
				case "MD046":
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
					if h.Diagnostic.Rule == "MD046" {
						hidden++
						if h.Diagnostic.FixAvailable || h.Diagnostic.ByteRange != (result.ByteRange{Start: 2, End: 5}) {
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
					if h.Diagnostic.Rule != "MD046" || h.Diagnostic.FixAvailable || h.SuppressedBy.Kind != "directive" {
						t.Fatal(h)
					}
					hidden++
					if tc.name == "used" {
						if i != 0 || h.Diagnostic.ByteRange != (result.ByteRange{Start: 29, End: 32}) || h.Diagnostic.Message != "Code block should use indented style." {
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

func TestMD046UnknownContextWithholdsObservation(t *testing.T) {
	for _, audit := range []bool{false, true} {
		t.Run(fmt.Sprintf("audit-%v", audit), func(t *testing.T) {
			source := "x\n```\n```\n\n<div>\n<x title=\"\n\n~~~\nx\n~~~"
			s := ruletest.Settings("MD046")
			s["MD046"] = engine.RuleSetting{Options: engine.Options{"style": "indented"}}
			if audit {
				source = "<!-- mdchk:disable MD046 -->\n" + source
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
				if c.Reason != result.ContextUnavailable || (c.Rule != "MD046" && c.Rule != "MD087") {
					t.Fatal(c)
				}
			}
		})
	}
}

func TestMD046RepeatedDisableLastOwner(t *testing.T) {
	source := "<!-- mdchk:disable MD046 -->\n<!-- mdchk:disable MD046 -->\n```\nx\n```"
	s := ruletest.Settings("MD046")
	s["MD046"] = engine.RuleSetting{Options: engine.Options{"style": "indented"}}
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
	if unused.Rule != "MD087" || unused.Message != "Suppression for MD046 hides no findings." || unused.ByteRange != (result.ByteRange{Start: 19, End: 24}) {
		t.Fatal(unused)
	}
	var wire result.SuppressedDiagnostic
	if e := json.Unmarshal(out.SuppressedDiagnostics[0], &wire); e != nil {
		t.Fatal(e)
	}
	if wire.SuppressedBy.Kind != "directive" || wire.Diagnostic.Rule != "MD046" || wire.Diagnostic.ByteRange != (result.ByteRange{Start: 58, End: 61}) {
		t.Fatal(wire)
	}
	var hidden []engine.DirectiveSuppression
	for _, record := range out.AnalysisRecords() {
		if a, ok := record.(engine.DirectiveAnalysis); ok {
			hidden = append(hidden, a.Suppressed()...)
		}
	}
	if len(hidden) != 1 || hidden[0].Owner != 1 || hidden[0].Token.Start() != 48 || hidden[0].Token.End() != 53 || hidden[0].Diagnostic.Rule != "MD046" || hidden[0].Diagnostic.ByteRange != wire.Diagnostic.ByteRange {
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
		if d.Owner != i || d.Effect != 0 || d.Rule != "MD046" || d.Reason != reason || d.Token.Start() != start || d.Token.End() != start+5 || d.Token.SourceToken() != in.Source().Token() {
			t.Fatal(d)
		}
	}
}

func TestMD046AdmittedStyleOwnership(t *testing.T) {
	options := engine.Options{"style": "fenced"}
	s := ruletest.Settings("MD046")
	s["MD046"] = engine.RuleSetting{Options: options}
	selected, e := ruletest.Registry(t, rules.Registrations()).SelectRules(s, engine.ValidationProviders{})
	if e != nil {
		t.Fatal(e)
	}
	options["style"] = "indented"
	plan, e := selected.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	in, e := plan.NewInput("literal.md", []byte("~~~go\n~~~"))
	if e != nil {
		t.Fatal(e)
	}
	out, e := plan.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if e != nil || !out.Complete || len(out.Diagnostics)+len(out.Errors)+len(out.Coverage) != 0 {
		t.Fatal(out, e)
	}
}

func TestMD046SuppressedFirstExemplarAndDocumentIsolation(t *testing.T) {
	s := ruletest.Settings("MD046")
	source := "<!-- mdchk:disable MD046 -->\n    first\n\n<!-- mdchk:enable MD046 -->\n~~~\nsecond\n~~~"
	out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), source, s, engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD046" || out.Diagnostics[0].Message != "Code block should use indented style." || out.Diagnostics[0].Range.Start.Line != 5 || out.Diagnostics[0].Range.Start.Column != 1 || out.Diagnostics[0].Range.End.Column != 4 {
		t.Fatal(out)
	}
	r := ruletest.Registry(t, rules.Registrations())
	a, e := engine.NewInput("a.md", []byte("    first\n\n~~~\n~~~"), s, engine.DocumentContext{})
	if e != nil {
		t.Fatal(e)
	}
	b, e := engine.NewInput("b.md", []byte("~~~\n~~~\n\n    second"), s, engine.DocumentContext{})
	if e != nil {
		t.Fatal(e)
	}
	both, e := r.Lint(context.Background(), []engine.Input{a, b}, engine.RunOptions{})
	if e != nil || !both.Complete || len(both.Diagnostics) != 2 || both.Diagnostics[0].Message != "Code block should use indented style." || both.Diagnostics[1].Message != "Code block should use fenced style." {
		t.Fatal(both, e)
	}
}

func TestMD046MixedConsistencyUnknownAfterPositive(t *testing.T) {
	source := "    first\n\n~~~\nsecond\n~~~\n\n<div>\n<x title=\"\n\n    last"
	out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), source, ruletest.Settings("MD046"), engine.DocumentContext{}, engine.RunOptions{})
	if out.Complete || len(out.Diagnostics)+len(out.Errors)+len(out.SuppressedDiagnostics) != 0 || len(out.Coverage) != 1 || out.Coverage[0].Rule != "MD046" || out.Coverage[0].Reason != result.ContextUnavailable {
		t.Fatal(out)
	}
}
