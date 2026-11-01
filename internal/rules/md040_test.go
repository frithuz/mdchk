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

type md040Literal struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Options      struct {
		Enabled     *bool
		Omitted     bool `json:"enabled_omitted"`
		Empty       bool `json:"empty_options"`
		Frontmatter string
		Allowed     []string `json:"allowed_languages"`
	}
	Findings []struct {
		Bytes, Start, End [2]int
		Kind, Message     string
	}
}

func md040Literals(t *testing.T) []md040Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md040.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "b8967e0a2f607a15ba75d0c78e0a770052ff9c27d1e3ee21ca6559df4fe9eaad" {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md040Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 83 {
		t.Fatal(e, len(v.Cases))
	}
	return v.Cases
}

func md040Settings() engine.Settings {
	s := md031Settings()
	off, on := false, true
	s["MD031"] = engine.RuleSetting{Enabled: &off}
	s["MD040"] = engine.RuleSetting{Enabled: &on}
	return s
}

func TestMD040FrozenProductionOracle(t *testing.T) {
	for _, tc := range md040Literals(t) {
		for _, flavor := range tc.Flavors {
			for _, inspect := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/inspect-%v", tc.Name, flavor, inspect), func(t *testing.T) {
					if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
						t.Fatal("source identity")
					}
					s := md040Settings()
					setting := s["MD040"]
					if tc.Options.Enabled != nil {
						setting.Enabled = tc.Options.Enabled
					}
					if tc.Options.Omitted {
						setting.Enabled = nil
					}
					if tc.Options.Empty {
						setting.Options = engine.Options{}
					}
					if tc.Options.Allowed != nil {
						setting.Options = engine.Options{"allowed_languages": tc.Options.Allowed}
					}
					s["MD040"] = setting
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
						if d.Rule != "MD040" || d.Message != w.Message || d.Path != "literal.md" || d.FixAvailable || d.ByteRange.Start != w.Bytes[0] || d.ByteRange.End != w.Bytes[1] || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] {
							t.Fatal(d, w)
						}
					}
					if inspect {
						found := false
						for _, ev := range trace.Evaluations() {
							if ev.Rule == "MD040" {
								found = true
								if tc.Options.Omitted || tc.Options.Enabled != nil && !*tc.Options.Enabled {
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

func TestMD040AvailabilityAndOptions(t *testing.T) {
	r := registry(t, Registrations())
	s, e := r.ResolveSettings(nil, engine.ValidationProviders{})
	if e != nil || !s["MD040"].Available || s["MD040"].Enabled || len(s["MD040"].Options) != 1 {
		t.Fatal(s["MD040"], e)
	}
	allowed, ok := s["MD040"].Options["allowed_languages"].([]string)
	if !ok || len(allowed) != 0 {
		t.Fatal(s["MD040"])
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD040" && (m.Fix != engine.NoFix || !m.RequiresSyntax || len(m.Options) != 1) {
			t.Fatal(m)
		}
	}
}

func TestMD040DirectivesSharedObservationAndInactiveDemand(t *testing.T) {
	for _, tc := range []struct {
		name, source                  string
		allow, enabled, inactive      bool
		visible, hidden, audit, calls int
	}{
		{"used", "<!-- mdchk:disable MD040 -->\n```\nx\n```\nTail", true, true, false, 0, 1, 0, 1},
		{"denied", "<!-- mdchk:disable MD040 -->\n```\nx\n```\nTail", false, true, false, 1, 0, 0, 1},
		{"unused", "<!-- mdchk:disable MD040 -->\n\n```go\nx\n```", true, true, false, 0, 0, 1, 1},
		{"disabled", "x\n```\n```", true, false, false, 0, 0, 0, 0},
		{"inactive", "x\n```\n```", true, false, true, 0, 1, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			regs := Registrations()
			for i := range regs {
				if regs[i].ID == "MD040" {
					actual := regs[i].Check
					regs[i].Check = func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome { calls++; return actual(ctx, c) }
				}
			}
			s := md040Settings()
			on := true
			s["MD040"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allow}
			s["MD087"] = engine.RuleSetting{Enabled: &on}
			sel, e := registry(t, regs).SelectRules(s, engine.ValidationProviders{})
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
				case "MD040":
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
					if h.Diagnostic.Rule == "MD040" {
						hidden++
						if h.Diagnostic.FixAvailable || h.Diagnostic.ByteRange != (result.ByteRange{Start: 5, End: 5}) {
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
					if h.Diagnostic.Rule != "MD040" || h.Diagnostic.FixAvailable || h.SuppressedBy.Kind != "directive" {
						t.Fatal(h)
					}
					hidden++
					if tc.name == "used" {
						if i != 0 || h.Diagnostic.ByteRange != (result.ByteRange{Start: 32, End: 32}) || h.Diagnostic.Message != "Fenced code block requires a language identifier." {
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

func TestMD040UnknownContextWithholdsObservation(t *testing.T) {
	for _, audit := range []bool{false, true} {
		t.Run(fmt.Sprintf("audit-%v", audit), func(t *testing.T) {
			source := "x\n```\n```\n\n<div>\n<x title=\"\n\n~~~\nx\n~~~"
			s := md040Settings()
			if audit {
				source = "<!-- mdchk:disable MD040 -->\n" + source
				s["MD087"] = engine.RuleSetting{Enabled: &audit}
			}
			out := analyzeHeadings(t, registry(t, Registrations()), source, s, engine.DocumentContext{}, engine.RunOptions{})
			want := 1
			if audit {
				want = 2
			}
			if out.Complete || len(out.Errors)+len(out.Diagnostics)+len(out.SuppressedDiagnostics) != 0 || len(out.Coverage) != want {
				t.Fatal(out)
			}
			for _, c := range out.Coverage {
				if c.Reason != result.ContextUnavailable || (c.Rule != "MD040" && c.Rule != "MD087") {
					t.Fatal(c)
				}
			}
		})
	}
}

func TestMD040RepeatedDisableLastOwner(t *testing.T) {
	source := "<!-- mdchk:disable MD040 -->\n<!-- mdchk:disable MD040 -->\n```\nx\n```"
	s := md040Settings()
	on := true
	s["MD087"] = engine.RuleSetting{Enabled: &on}
	sel, e := registry(t, Registrations()).SelectRules(s, engine.ValidationProviders{})
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
	if unused.Rule != "MD087" || unused.Message != "Suppression for MD040 hides no findings." || unused.ByteRange != (result.ByteRange{Start: 19, End: 24}) {
		t.Fatal(unused)
	}
	var wire result.SuppressedDiagnostic
	if e := json.Unmarshal(out.SuppressedDiagnostics[0], &wire); e != nil {
		t.Fatal(e)
	}
	if wire.SuppressedBy.Kind != "directive" || wire.Diagnostic.Rule != "MD040" || wire.Diagnostic.ByteRange != (result.ByteRange{Start: 61, End: 61}) {
		t.Fatal(wire)
	}
	var hidden []engine.DirectiveSuppression
	for _, record := range out.AnalysisRecords() {
		if a, ok := record.(engine.DirectiveAnalysis); ok {
			hidden = append(hidden, a.Suppressed()...)
		}
	}
	if len(hidden) != 1 || hidden[0].Owner != 1 || hidden[0].Token.Start() != 48 || hidden[0].Token.End() != 53 || hidden[0].Diagnostic.Rule != "MD040" || hidden[0].Diagnostic.ByteRange != wire.Diagnostic.ByteRange {
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
		if d.Owner != i || d.Effect != 0 || d.Rule != "MD040" || d.Reason != reason || d.Token.Start() != start || d.Token.End() != start+5 || d.Token.SourceToken() != in.Source().Token() {
			t.Fatal(d)
		}
	}
}

func TestMD040AdmittedAllowlistOwnership(t *testing.T) {
	list := []string{"go"}
	s := md040Settings()
	setting := s["MD040"]
	setting.Options = engine.Options{"allowed_languages": list}
	s["MD040"] = setting
	selected, e := registry(t, Registrations()).SelectRules(s, engine.ValidationProviders{})
	if e != nil {
		t.Fatal(e)
	}
	list[0] = "ruby"
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
