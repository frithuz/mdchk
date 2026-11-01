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

type md048Literal struct {
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

func md048Literals(t *testing.T) []md048Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md048.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "afef57dcdf5974d81ddbe9c85e688f1ac0f42b6d4befcd68b056a28b4b254807" {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md048Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 65 {
		t.Fatal(e, len(v.Cases))
	}
	return v.Cases
}

func md048Settings() engine.Settings {
	s := md046Settings()
	off, on := false, true
	s["MD046"] = engine.RuleSetting{Enabled: &off}
	s["MD048"] = engine.RuleSetting{Enabled: &on}
	return s
}

func TestMD048FrozenProductionOracle(t *testing.T) {
	for _, tc := range md048Literals(t) {
		for _, flavor := range tc.Flavors {
			for _, inspect := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/inspect-%v", tc.Name, flavor, inspect), func(t *testing.T) {
					if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
						t.Fatal("source identity")
					}
					s := md048Settings()
					setting := s["MD048"]
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
					s["MD048"] = setting
					if tc.Options.Omitted && !tc.Options.Empty {
						delete(s, "MD048")
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
						if d.Rule != "MD048" || d.Message != w.Message || d.Path != "literal.md" || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}) || d.FixAvailable || d.ByteRange.Start != w.Bytes[0] || d.ByteRange.End != w.Bytes[1] || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] {
							t.Fatal(d, w)
						}
					}
					if inspect {
						found := false
						for _, ev := range trace.Evaluations() {
							if ev.Rule == "MD048" {
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

func TestMD048AvailabilityAndOptions(t *testing.T) {
	r := registry(t, Registrations())
	s, e := r.ResolveSettings(nil, engine.ValidationProviders{})
	if e != nil || !s["MD048"].Available || !s["MD048"].Enabled || len(s["MD048"].Options) != 1 {
		t.Fatal(s["MD048"], e)
	}
	style, ok := s["MD048"].Options["style"].(string)
	if !ok || style != "consistent" {
		t.Fatal(s["MD048"])
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD048" && (m.Fix != engine.PlannedFix || !m.RequiresSyntax || len(m.Options) != 1) {
			t.Fatal(m)
		}
	}
}

func TestMD048DirectivesSharedObservationAndInactiveDemand(t *testing.T) {
	for _, tc := range []struct {
		name, source                  string
		allow, enabled, inactive      bool
		visible, hidden, audit, calls int
	}{
		{"used", "<!-- mdchk:disable MD048 -->\n```\nx\n```\nTail", true, true, false, 0, 1, 0, 1},
		{"denied", "<!-- mdchk:disable MD048 -->\n```\nx\n```\nTail", false, true, false, 1, 0, 0, 1},
		{"unused", "<!-- mdchk:disable MD048 -->\n\n    x", true, true, false, 0, 0, 1, 1},
		{"disabled", "x\n```\n```", true, false, false, 0, 0, 0, 0},
		{"inactive", "x\n```\n```", true, false, true, 0, 1, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			regs := Registrations()
			for i := range regs {
				if regs[i].ID == "MD048" {
					actual := regs[i].Check
					regs[i].Check = func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome { calls++; return actual(ctx, c) }
				}
			}
			s := md048Settings()
			on := true
			s["MD048"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allow, Options: engine.Options{"style": "tilde"}}
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
				case "MD048":
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
					if h.Diagnostic.Rule == "MD048" {
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
					if h.Diagnostic.Rule != "MD048" || h.Diagnostic.FixAvailable || h.SuppressedBy.Kind != "directive" {
						t.Fatal(h)
					}
					hidden++
					if tc.name == "used" {
						if i != 0 || h.Diagnostic.ByteRange != (result.ByteRange{Start: 29, End: 32}) || h.Diagnostic.Message != "Fenced code block should use tilde delimiters." {
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

func TestMD048UnknownContextWithholdsObservation(t *testing.T) {
	for _, audit := range []bool{false, true} {
		t.Run(fmt.Sprintf("audit-%v", audit), func(t *testing.T) {
			source := "x\n```\n```\n\n<div>\n<x title=\"\n\n```\nx\n```"
			s := md048Settings()
			s["MD048"] = engine.RuleSetting{Options: engine.Options{"style": "tilde"}}
			if audit {
				source = "<!-- mdchk:disable MD048 -->\n" + source
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
				if c.Reason != result.ContextUnavailable || (c.Rule != "MD048" && c.Rule != "MD087") {
					t.Fatal(c)
				}
			}
		})
	}
}

func TestMD048RepeatedDisableLastOwner(t *testing.T) {
	source := "<!-- mdchk:disable MD048 -->\n<!-- mdchk:disable MD048 -->\n```\nx\n```"
	s := md048Settings()
	s["MD048"] = engine.RuleSetting{Options: engine.Options{"style": "tilde"}}
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
	if unused.Rule != "MD087" || unused.Message != "Suppression for MD048 hides no findings." || unused.ByteRange != (result.ByteRange{Start: 19, End: 24}) {
		t.Fatal(unused)
	}
	var wire result.SuppressedDiagnostic
	if e := json.Unmarshal(out.SuppressedDiagnostics[0], &wire); e != nil {
		t.Fatal(e)
	}
	if wire.SuppressedBy.Kind != "directive" || wire.Diagnostic.Rule != "MD048" || wire.Diagnostic.ByteRange != (result.ByteRange{Start: 58, End: 61}) {
		t.Fatal(wire)
	}
	var hidden []engine.DirectiveSuppression
	for _, record := range out.AnalysisRecords() {
		if a, ok := record.(engine.DirectiveAnalysis); ok {
			hidden = append(hidden, a.Suppressed()...)
		}
	}
	if len(hidden) != 1 || hidden[0].Owner != 1 || hidden[0].Token.Start() != 48 || hidden[0].Token.End() != 53 || hidden[0].Diagnostic.Rule != "MD048" || hidden[0].Diagnostic.ByteRange != wire.Diagnostic.ByteRange {
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
		if d.Owner != i || d.Effect != 0 || d.Rule != "MD048" || d.Reason != reason || d.Token.Start() != start || d.Token.End() != start+5 || d.Token.SourceToken() != in.Source().Token() {
			t.Fatal(d)
		}
	}
}

func TestMD048AdmittedStyleOwnership(t *testing.T) {
	options := engine.Options{"style": "tilde"}
	s := md048Settings()
	s["MD048"] = engine.RuleSetting{Options: options}
	selected, e := registry(t, Registrations()).SelectRules(s, engine.ValidationProviders{})
	if e != nil {
		t.Fatal(e)
	}
	options["style"] = "backtick"
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

func TestMD048SuppressedExemplarAndDocumentIsolation(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			s := md048Settings()
			source := "<!-- mdchk:disable MD048 -->\n```\nx\n```\n<!-- mdchk:enable MD048 -->\n~~~\ny\n~~~"
			out := analyzeHeadings(t, registry(t, Registrations()), source, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Message != "Fenced code block should use backtick delimiters." || out.Diagnostics[0].Range != (result.Range{Start: result.Position{Line: 6, Column: 1}, End: result.Position{Line: 6, Column: 4}}) {
				t.Fatal(out)
			}
			sources := []string{"```\nx\n```\n\n~~~\ny\n~~~", "~~~\nx\n~~~\n\n```\ny\n```"}
			messages := []string{"Fenced code block should use backtick delimiters.", "Fenced code block should use tilde delimiters."}
			var inputs []engine.Input
			for i, source := range sources {
				in, e := engine.NewInput(fmt.Sprintf("%d.md", i), []byte(source), s, engine.DocumentContext{Flavor: flavor})
				if e != nil {
					t.Fatal(e)
				}
				inputs = append(inputs, in)
				single, e := registry(t, Registrations()).Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
				if e != nil || !single.Complete || len(single.Diagnostics) != 1 || single.Diagnostics[0].Message != messages[i] || single.Diagnostics[0].Range != (result.Range{Start: result.Position{Line: 5, Column: 1}, End: result.Position{Line: 5, Column: 4}}) {
					t.Fatal(single, e)
				}
			}
			both, e := registry(t, Registrations()).Lint(context.Background(), inputs, engine.RunOptions{})
			if e != nil || !both.Complete || len(both.Diagnostics) != 2 || both.Diagnostics[0].Message != messages[0] || both.Diagnostics[1].Message != messages[1] {
				t.Fatal(both, e)
			}
		})
	}
}

func TestMD048ConfiguredOffEnableAndDenial(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow-%v", allow), func(t *testing.T) {
			enabled := false
			s := md048Settings()
			s["MD048"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow, Options: engine.Options{"style": "backtick"}}
			out := analyzeHeadings(t, registry(t, Registrations()), "<!-- mdchk:enable MD048 -->\n~~~\nx\n~~~", s, engine.DocumentContext{}, engine.RunOptions{})
			want := 0
			if allow {
				want = 1
			}
			if !out.Complete || len(out.Diagnostics) != want || len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(out)
			}
			if allow && (out.Diagnostics[0].Message != "Fenced code block should use backtick delimiters." || out.Diagnostics[0].Range != (result.Range{Start: result.Position{Line: 2, Column: 1}, End: result.Position{Line: 2, Column: 4}})) {
				t.Fatal(out)
			}
		})
	}
}

func TestMD048MixedConsistencyUnknownAfterPositive(t *testing.T) {
	source := "```\nfirst\n```\n\n~~~\nsecond\n~~~\n\n<div>\n<x title=\"\n\n~~~\nlast\n~~~"
	out := analyzeHeadings(t, registry(t, Registrations()), source, md048Settings(), engine.DocumentContext{}, engine.RunOptions{})
	if out.Complete || len(out.Diagnostics)+len(out.Errors)+len(out.SuppressedDiagnostics) != 0 || len(out.Coverage) != 1 || out.Coverage[0].Rule != "MD048" || out.Coverage[0].Reason != result.ContextUnavailable {
		t.Fatal(out)
	}
}

func TestMD048LiteralSuppressionControls(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, allow := range []bool{false, true} {
			for _, matching := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/allow-%v/matching-%v", flavor, allow, matching), func(t *testing.T) {
					source := "<!-- mdchk:disable MD048 -->\n~~~\nx\n~~~"
					if matching {
						source = "<!-- mdchk:disable MD048 -->\n```\nx\n```"
					}
					s := md048Settings()
					on := true
					s["MD087"] = engine.RuleSetting{Enabled: &on}
					s["MD048"] = engine.RuleSetting{AllowDirectives: &allow, Options: engine.Options{"style": "backtick"}}
					out := analyzeHeadings(t, registry(t, Registrations()), source, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
					visible, hidden, audit := 0, 0, 0
					if matching {
						if allow {
							audit = 1
						}
					} else if allow {
						hidden = 1
					} else {
						visible = 1
					}
					if !out.Complete || len(out.Diagnostics) != visible+audit || len(out.SuppressedDiagnostics) != hidden || len(out.Errors)+len(out.Coverage) != 0 {
						t.Fatal(out)
					}
					var d result.Diagnostic
					if visible == 1 {
						d = out.Diagnostics[0]
					}
					if hidden == 1 {
						var h result.SuppressedDiagnostic
						if e := json.Unmarshal(out.SuppressedDiagnostics[0], &h); e != nil {
							t.Fatal(e)
						}
						d = h.Diagnostic
					}
					if visible+hidden == 1 && (d.Rule != "MD048" || d.Message != "Fenced code block should use backtick delimiters." || d.Range != (result.Range{Start: result.Position{Line: 2, Column: 1}, End: result.Position{Line: 2, Column: 4}})) {
						t.Fatal(d)
					}
					if audit == 1 && out.Diagnostics[0].Rule != "MD087" {
						t.Fatal(out)
					}
				})
			}
		}
	}
}
