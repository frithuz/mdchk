package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func TestDirectiveLintAnchorsFullContextAndOwnership(t *testing.T) {
	const text = "雪\r\n<!-- mdchk:disable MD001 -->inside<!-- mdchk:disable MD001 -->repeat<!-- mdchk:enable MD001 -->after"
	first := strings.Index(text, "inside")
	repeated := strings.Index(text, "repeat")
	enabled := strings.Index(text, "after")
	anchors := [][2]int{{0, 0}, {first - 1, first + 2}, {first, first + 2}, {repeated, repeated + 2}, {enabled - 1, enabled + 1}, {enabled, enabled + 1}, {len(text), len(text)}}
	calls := 0
	r := registry(t, []engine.Metadata{metadata("MD001")}, engine.Registration{ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		calls++
		if c.Document() == nil || string(c.Source().Bytes()) != text {
			t.Fatal("callback lost full original context")
		}
		findings := make([]engine.Finding, 0, len(anchors))
		for _, a := range anchors {
			s, e := c.Source().Span(a[0], a[1])
			if e != nil {
				t.Fatal(e)
			}
			findings = append(findings, engine.Finding{Message: "anchor", Span: s})
		}
		return engine.RuleOutcome{Findings: findings}
	}})
	out := lint(t, r, input(t, "雪.md", []byte(text), nil))
	if calls != 1 || len(out.Diagnostics) != 4 || len(out.SuppressedDiagnostics) != 3 || !out.Complete || out.Status != "violations" {
		t.Fatalf("calls%d %+v", calls, out)
	}
	var active []int
	for _, d := range out.Diagnostics {
		active = append(active, d.ByteRange.Start)
	}
	if !reflect.DeepEqual(active, []int{0, first - 1, enabled, len(text)}) {
		t.Fatal(active)
	}
	if out.Diagnostics[1].Range.Start != (parser.Position{Line: 2, Column: 28}) {
		t.Fatal(out.Diagnostics[1])
	}
	records := out.AnalysisRecords()
	if len(records) != 1 {
		t.Fatal(records)
	}
	a := records[0].(engine.DirectiveAnalysis)
	hidden := a.Suppressed()
	if len(hidden) != 3 || hidden[0].Owner != 0 || hidden[1].Owner != 1 || hidden[2].Owner != 1 {
		t.Fatal(hidden)
	}
	candidates := a.State().Candidates()
	if string([]byte(text)[hidden[1].Token.Start():hidden[1].Token.End()]) != "MD001" || candidates[hidden[1].Owner].Raw != "<!-- mdchk:disable MD001 -->" {
		t.Fatal(candidates, hidden)
	}
	hidden[0].Owner = 99
	candidates[0].IDs[0] = "MD999"
	if a.Suppressed()[0].Owner != 0 || a.State().Candidates()[0].IDs[0] != "MD001" {
		t.Fatal("analysis alias escaped")
	}
	for _, raw := range out.SuppressedDiagnostics {
		var s result.SuppressedDiagnostic
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatal(err)
		}
		if s.Diagnostic.ID == "" || s.SuppressedBy.Kind != "directive" || s.SuppressedBy.DirectiveID != "" {
			t.Fatal(s)
		}
	}
	validateWire(t, out)
}

func TestDirectiveDynamicEnableAndCoverage(t *testing.T) {
	for _, tc := range []struct {
		name, text                          string
		available                           bool
		wantCalls, wantCoverage, wantActive int
	}{
		{"disabled throughout", "plain", true, 0, 0, 0},
		{"enabled tail", "<!-- mdchk:enable MD001 -->tail", true, 1, 0, 1},
		{"enabled only EOF", "<!-- mdchk:enable MD001 -->", true, 1, 0, 1},
		{"missing enabled EOF", "<!-- mdchk:enable MD001 -->", false, 0, 1, 0},
		{"missing disabled everywhere", "plain", false, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			m := metadata("MD001")
			m.DefaultEnabled = false
			m.Options = engine.OptionSchema{"style": {Kind: engine.String, Default: "retained"}}
			var regs []engine.Registration
			if tc.available {
				regs = []engine.Registration{{ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
					calls++
					if c.Options()["style"] != "retained" || c.Document() == nil {
						t.Fatal("lost options/context")
					}
					s, _ := c.Source().Span(c.Source().Len(), c.Source().Len())
					return engine.RuleOutcome{Findings: []engine.Finding{{Message: "EOF", Span: s}}}
				}}}
			}
			r := registry(t, []engine.Metadata{m}, regs...)
			out := lint(t, r, input(t, "a.md", []byte(tc.text), nil))
			if calls != tc.wantCalls || len(out.Coverage) != tc.wantCoverage || len(out.Diagnostics) != tc.wantActive {
				t.Fatalf("calls%d %+v", calls, out)
			}
			if tc.wantCoverage > 0 && out.Coverage[0].Reason != "rule-unavailable" {
				t.Fatal(out.Coverage)
			}
		})
	}
}

func TestDirectiveMalformedCannotBeSuppressedAndGlobalOffIsInactive(t *testing.T) {
	r := registry(t, []engine.Metadata{metadata("MD001")}, engine.Registration{ID: "MD001", Check: finding(0, 0)})
	out := lint(t, r, input(t, "a.md", []byte("<!-- mdchk:disable MD001 --><!-- mdchk:disable MD999 -->"), nil))
	if len(out.Diagnostics) != 2 || out.Diagnostics[1].Rule != "directive" || out.Status != "violations" {
		t.Fatal(out)
	}
	// Validation precedes suppression: a foreign source finding remains an internal
	// failure even when the finding's apparent anchor lies in a disabled interval.
	foreign := parser.NewSource([]byte("foreign"))
	span, _ := foreign.Span(0, 0)
	bad := registry(t, []engine.Metadata{metadata("MD001")}, engine.Registration{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome {
		return engine.RuleOutcome{Findings: []engine.Finding{{Message: "bad", Span: span}}}
	}})
	got := lint(t, bad, input(t, "a.md", []byte("<!-- mdchk:disable MD001 -->tail"), nil))
	if len(got.Errors) != 1 || got.Errors[0].Code != "invalid-finding" || len(got.SuppressedDiagnostics) != 0 {
		t.Fatal(got)
	}
}

func TestDirectiveObservationalConfiguredDisableAndDocumentIsolation(t *testing.T) {
	m := metadata("MD001")
	m.DefaultEnabled = false
	r := registry(t, []engine.Metadata{m}, engine.Registration{ID: "MD001", Check: finding(0, 0)})
	a := input(t, "a.md", []byte("<!-- mdchk:enable MD001 -->"), nil)
	b := input(t, "b.md", []byte("plain"), nil)
	out, err := r.Lint(context.Background(), []engine.Input{b, a}, engine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Diagnostics) != 0 || len(out.SuppressedDiagnostics) != 1 || len(out.Coverage) != 0 || !out.Complete {
		t.Fatal(out)
	}
	for _, raw := range out.SuppressedDiagnostics {
		var s result.SuppressedDiagnostic
		_ = json.Unmarshal(raw, &s)
		if s.SuppressedBy.Kind != "configuration" {
			t.Fatal(s)
		}
	}
	again, err := r.Lint(context.Background(), []engine.Input{a, b}, engine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	x, _ := json.Marshal(out)
	y, _ := json.Marshal(again)
	if string(x) != string(y) {
		t.Fatal("request/argument-order state leaked")
	}
}

func validateWire(t *testing.T, out result.Result) {
	t.Helper()
	python := os.Getenv("MDCHK_RESULT_VALIDATOR_PYTHON")
	if python == "" {
		return
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "result.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	script := `import json,runpy,sys
v=runpy.run_path('../../docs/contracts/results/validate.py')
x=json.load(open(sys.argv[1]));v['validator'].validate(x);v['semantic'](x)`
	command := exec.Command(python, "-c", script, path) //nolint:gosec // G204: test-only interpreter explicitly selected by the developer; fixed script and owned file.
	if b, err := command.CombinedOutput(); err != nil {
		t.Fatalf("schema validation: %v %s", err, b)
	}
}

// A finite residual quota after a completed parse must not publish guessed
// directive state or enter any callbacks. Earlier reliable syntax observations
// remain available, and later documents remain explicitly unstarted.
func TestDirectiveEngineResidualQuotaWithholdsCallbacks(t *testing.T) {
	calls := 0
	r := registry(t, []engine.Metadata{metadata("MD001")}, engine.Registration{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { calls++; return engine.RuleOutcome{} }})
	source := parser.NewSource([]byte("<!-- mdchk:wat MD999 -->" + strings.Repeat("x<!-- mdchk:disable MD001 -->", 200)))
	measure, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, measure)
	rules, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	complete := engine.ProcessDirectives(context.Background(), parsed, testDirectivePolicy{rules}, measure)
	if !complete.Complete() {
		t.Fatal(complete.Err())
	}
	var charged *parser.LimitExceeded
	if !errors.As(measure.ChargeWork(parser.DefaultLimits().WorkUnits), &charged) {
		t.Fatal("measurement failed")
	}
	consumed := charged.Observed - parser.DefaultLimits().WorkUnits
	limits := parser.DefaultLimits()
	limits.WorkUnits = consumed + int64(4*source.Len()+1) - 1000
	budget, _ := parser.NewOperationBudget(limits)
	out, err := r.Lint(context.Background(), []engine.Input{input(t, "a.md", source.Bytes(), nil), input(t, "z.md", []byte("plain"), nil)}, engine.RunOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || out.Complete || len(out.Interruptions) != 1 || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "directive" || out.Files[1].Analysis != "not-started" {
		t.Fatalf("calls%d %+v", calls, out)
	}
	a := out.AnalysisRecords()[0].(engine.DirectiveAnalysis)
	if a.State().Complete() || len(a.State().Intervals("MD001")) != 0 {
		t.Fatal("partial state published")
	}
	validateWire(t, out)
}

type testDirectivePolicy struct {
	rules map[string]engine.ResolvedRule
}

func (p testDirectivePolicy) DirectiveConfiguration() (bool, map[string]engine.ResolvedRule, error) {
	return true, p.rules, nil
}
func (p testDirectivePolicy) DirectivePermission(string) engine.DirectiveDecision {
	return engine.DirectiveDecision{Allowed: true}
}
func (p testDirectivePolicy) GlobalDirectivePermission() engine.DirectiveDecision {
	return engine.DirectiveDecision{Allowed: true}
}
func (p testDirectivePolicy) DirectivePermissionWork(string) (int64, error) { return 0, nil }
