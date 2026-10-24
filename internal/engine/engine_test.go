package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func metadata(id string) engine.Metadata {
	return engine.Metadata{ID: id, Name: "test " + id, DefaultEnabled: true, Fix: engine.NoFix}
}
func input(t *testing.T, path string, b []byte, settings engine.Settings) engine.Input {
	t.Helper()
	in, err := engine.NewInput(path, b, settings, engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	return in
}
func registry(t *testing.T, m []engine.Metadata, r ...engine.Registration) *engine.Registry {
	t.Helper()
	reg, err := engine.NewRegistry(m, r)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}
func lint(t *testing.T, r *engine.Registry, in ...engine.Input) result.Result {
	t.Helper()
	out, err := r.Lint(context.Background(), in, engine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func boolptr(b bool) *bool { return &b }
func finding(start, end int) engine.Check {
	return func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		s, err := c.Source().Span(start, end)
		if err != nil {
			return engine.RuleOutcome{Err: err}
		}
		return engine.RuleOutcome{Findings: []engine.Finding{{Message: "a finding", Span: s, FixEligible: true}}}
	}
}

func TestRegistryValidationAndIsolation(t *testing.T) {
	good := metadata("MD001")
	invalid := []struct {
		name          string
		catalog       []engine.Metadata
		registrations []engine.Registration
	}{
		{"duplicate metadata", []engine.Metadata{good, good}, nil},
		{"noncanonical", []engine.Metadata{metadata("md001")}, nil},
		{"reserved", []engine.Metadata{metadata("MD006")}, nil},
		{"zero ID", []engine.Metadata{metadata("MD000")}, nil},
		{"missing name", []engine.Metadata{{ID: "MD001", Fix: engine.NoFix}}, nil},
		{"missing fix metadata", []engine.Metadata{{ID: "MD001", Name: "name"}}, nil},
		{"foreign registration", []engine.Metadata{good}, []engine.Registration{{ID: "MD003", Check: finding(0, 0)}}},
		{"nil check", []engine.Metadata{good}, []engine.Registration{{ID: "MD001"}}},
		{"duplicate check", []engine.Metadata{good}, []engine.Registration{{ID: "MD001", Check: finding(0, 0)}, {ID: "MD001", Check: finding(0, 0)}}},
		{"MD094 gate", []engine.Metadata{metadata("MD094")}, []engine.Registration{{ID: "MD094", Check: finding(0, 0)}}},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := engine.NewRegistry(tt.catalog, tt.registrations); err == nil {
				t.Fatal("accepted invalid registration")
			}
		})
	}
	good.Options = engine.OptionSchema{"style": {Kind: engine.String, Default: "a", Choices: []string{"a", "b"}}, "count": {Kind: engine.Integer, Default: 1}}
	reg := registry(t, []engine.Metadata{good}, engine.Registration{ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		if c.Options()["style"] != "a" {
			t.Error("metadata mutated")
		}
		return engine.RuleOutcome{}
	}})
	good.Options["style"] = engine.OptionSpec{Kind: engine.Boolean, Default: true}
	returned := reg.Metadata()
	returned[0].Options["style"] = engine.OptionSpec{Kind: engine.Boolean, Default: true}
	returned[0].ID = "MD999"
	if got := lint(t, reg, input(t, "a", nil, nil)); got.Status != "clean" {
		t.Fatal(got)
	}
	second := registry(t, []engine.Metadata{metadata("MD001")})
	if second.Available("MD001") {
		t.Fatal("registry state leaked")
	}
	for _, bad := range []engine.OptionSpec{{Kind: engine.Integer, Default: "1"}, {Kind: "unknown", Default: true}, {Kind: engine.String, Default: "bad", Choices: []string{"a"}}} {
		m := metadata("MD001")
		m.Options = engine.OptionSchema{"x": bad}
		if _, err := engine.NewRegistry([]engine.Metadata{m}, nil); err == nil {
			t.Fatalf("accepted invalid schema %+v", bad)
		}
	}
}

func TestPreflightAndAvailability(t *testing.T) {
	m := metadata("MD001")
	m.Options = engine.OptionSchema{"flag": {Kind: engine.Boolean, Default: true}}
	calls := 0
	r := registry(t, []engine.Metadata{m, metadata("MD003")}, engine.Registration{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { calls++; return engine.RuleOutcome{} }})
	for _, settings := range []engine.Settings{
		{"MD999": {}}, {"MD003": {Enabled: boolptr(true)}}, {"MD001": {Enabled: boolptr(false), Options: engine.Options{"unknown": true}}}, {"MD001": {Enabled: boolptr(false), Options: engine.Options{"flag": "yes"}}},
	} {
		_, err := r.Lint(context.Background(), []engine.Input{input(t, "a", nil, nil), input(t, "z", nil, settings)}, engine.RunOptions{})
		if err == nil {
			t.Fatal("accepted invalid settings")
		}
		if calls != 0 {
			t.Fatal("processed before preflight completed")
		}
	}
	out := lint(t, r, input(t, "a", nil, nil))
	if out.Complete || len(out.Coverage) != 1 || out.Coverage[0].Rule != "MD003" || len(out.SkippedFiles) != 0 {
		t.Fatal(out)
	}
	out = lint(t, r, input(t, "a", nil, engine.Settings{"MD003": {Enabled: boolptr(false)}}))
	if !out.Complete {
		t.Fatal(out)
	}
	m = metadata("MD003")
	m.AppliesTo = func(dc engine.DocumentContext) bool { return dc.Project == "explicit-project" }
	r = registry(t, []engine.Metadata{m})
	if out = lint(t, r, input(t, "a", nil, nil)); !out.Complete {
		t.Fatal("proven inapplicable rule reduced coverage", out)
	}
	_, err := r.Lint(context.Background(), []engine.Input{input(t, "a", nil, nil), input(t, "a", nil, nil)}, engine.RunOptions{})
	if err == nil {
		t.Fatal("accepted duplicate path")
	}
}

func TestCoordinatesAndImmutableInputs(t *testing.T) {
	bytes := []byte("a\tπ\r\nb")
	enabled := true
	list := []string{"original"}
	settings := engine.Settings{"MD001": {Enabled: &enabled, Options: engine.Options{"labels": list}}}
	in := input(t, "a", bytes, settings)
	bytes[0] = 'x'
	enabled = false
	list[0] = "mutated"
	settings["MD001"] = engine.RuleSetting{Enabled: boolptr(false)}
	m := metadata("MD001")
	m.Options = engine.OptionSchema{"labels": {Kind: engine.Strings, Default: []string{}}}
	m.Fix = engine.PlannedFix
	m.RequiresSyntax = true
	r := registry(t, []engine.Metadata{m}, engine.Registration{ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		if c.Document() == nil || c.Document().Source() != c.Source() {
			t.Fatal("parser context lost ownership")
		}
		b := c.Source().Bytes()
		b[0] = 'z'
		if string(c.Source().Bytes()) != "a\tπ\r\nb" {
			t.Fatal("source was mutated")
		}
		o := c.Options()
		o["labels"].([]string)[0] = "bad"
		if c.Options()["labels"].([]string)[0] != "original" {
			t.Fatal("options leaked")
		}
		span, _ := c.Source().Span(2, 4)
		eof, _ := c.Source().Span(c.Source().Len(), c.Source().Len())
		return engine.RuleOutcome{Findings: []engine.Finding{{Message: "pi", Span: span, FixEligible: true}, {Message: "EOF", Span: eof}}}
	}})
	out := lint(t, r, in)
	if !out.Complete || len(out.Diagnostics) != 2 {
		t.Fatal(out)
	}
	d := out.Diagnostics[0]
	if d.Range.Start != (parser.Position{Line: 1, Column: 3}) || d.Range.End != (parser.Position{Line: 1, Column: 4}) || d.FixAvailable {
		t.Fatal(d)
	}
	if out.Diagnostics[1].Range.Start != (parser.Position{Line: 2, Column: 2}) {
		t.Fatal(out.Diagnostics[1])
	}
	for _, tt := range []struct {
		name string
		span func(parser.Source) parser.Span
	}{
		{"foreign", func(parser.Source) parser.Span { s, _ := parser.NewSource([]byte("a\tπ\r\nb")).Span(2, 4); return s }},
		{"continuation", func(s parser.Source) parser.Span { p, _ := s.Span(3, 4); return p }},
		{"CRLF interior", func(s parser.Source) parser.Span { p, _ := s.Span(5, 5); return p }},
		{"absent", func(parser.Source) parser.Span { return parser.Span{} }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := registry(t, []engine.Metadata{metadata("MD001")}, engine.Registration{ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
				return engine.RuleOutcome{Findings: []engine.Finding{{Message: "bad", Span: tt.span(c.Source())}}}
			}})
			out := lint(t, r, inForRule(t, "a\tπ\r\nb"))
			if out.Status != "error" || out.Complete || len(out.Diagnostics) != 0 || out.Errors[0].Code != "invalid-finding" {
				t.Fatal(out)
			}
		})
	}
}
func inForRule(t *testing.T, s string) engine.Input { return input(t, "a", []byte(s), nil) }

func TestUTF8Gate(t *testing.T) {
	calls := 0
	r := registry(t, []engine.Metadata{metadata("MD001"), metadata("MD094")}, engine.Registration{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { calls++; return engine.RuleOutcome{} }})
	for _, enabled := range []bool{true, false} {
		out := lint(t, r, input(t, "bad", []byte{'\t', 0xcf, 0x80, '\r', '\n', 'a', 0xff}, engine.Settings{"MD094": {Enabled: boolptr(enabled)}}))
		if len(out.Errors) != 1 || len(out.Diagnostics) != 0 || out.Errors[0].Category != result.IO || out.Errors[0].Code != "invalid-utf8" || *out.Errors[0].ByteOffset != 6 || out.Errors[0].Range.Start != (parser.Position{Line: 2, Column: 2}) || len(out.CheckedFiles) != 1 || calls != 0 {
			t.Fatal(out)
		}
		if (out.Errors[0].Rule == "MD094") != enabled {
			t.Fatal(out.Errors[0])
		}
	}
	out := lint(t, r, input(t, "bad", []byte{0xff}, nil), input(t, "valid", []byte("\ufffd"), nil))
	if calls != 1 || len(out.Errors) != 1 || len(out.CheckedFiles) != 2 || out.Files[1].Analysis != "complete" {
		t.Fatal(out)
	}
}

func TestDeterministicResults(t *testing.T) {
	a, b := metadata("MD001"), metadata("MD003")
	in1, in2 := input(t, "a", []byte("abc"), nil), input(t, "b", []byte("abc"), nil)
	r1 := registry(t, []engine.Metadata{a, b}, engine.Registration{ID: a.ID, Check: finding(1, 2)}, engine.Registration{ID: b.ID, Check: finding(0, 1)})
	r2 := registry(t, []engine.Metadata{b, a}, engine.Registration{ID: b.ID, Check: finding(0, 1)}, engine.Registration{ID: a.ID, Check: finding(1, 2)})
	x, y := lint(t, r1, in1, in2), lint(t, r2, in2, in1)
	if !reflect.DeepEqual(x, y) {
		t.Fatalf("order changed output:\n%+v\n%+v", x, y)
	}
	if x.Diagnostics[0].Rule != "MD003" || x.Diagnostics[0].ID != "diagnostic-1" {
		t.Fatal(x)
	}
}

func TestInterruptionAndReliablePartialFindings(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := registry(t, []engine.Metadata{metadata("MD001"), metadata("MD003")}, engine.Registration{ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		cancel()
		return finding(0, 0)(ctx, c)
	}}, engine.Registration{ID: "MD003", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome {
		t.Fatal("scheduled after cancellation")
		return engine.RuleOutcome{}
	}})
	out, err := r.Lint(ctx, []engine.Input{input(t, "a", nil, nil), input(t, "b", nil, nil)}, engine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Complete || out.Status != "incomplete" || len(out.Diagnostics) != 1 || len(out.Errors) != 0 || len(out.Interruptions) != 1 || out.Interruptions[0].Reason != "cancelled" || out.Files[1].Analysis != "not-started" || len(out.CheckedFiles) != 1 {
		t.Fatal(out)
	}
	limits := parser.DefaultLimits()
	limits.WorkUnits = 1
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	out, err = r.Lint(context.Background(), []engine.Input{input(t, "a", []byte("large"), nil)}, engine.RunOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "incomplete" || len(out.Errors) != 0 || out.Interruptions[0].Limit == nil {
		t.Fatal(out)
	}
	r = registry(t, []engine.Metadata{metadata("MD001")}, engine.Registration{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome {
		return engine.RuleOutcome{Err: errors.New("failed")}
	}})
	out = lint(t, r, input(t, "a", nil, nil))
	if out.Complete || out.Status != "error" {
		t.Fatal(out)
	}
}

func TestUnsupportedSyntaxCannotLookClean(t *testing.T) {
	m := metadata("MD001")
	m.RequiresSyntax = true
	r := registry(t, []engine.Metadata{m}, engine.Registration{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome {
		t.Fatal("syntax rule ran without provider")
		return engine.RuleOutcome{}
	}})
	in, err := engine.NewInput("a", []byte("text"), nil, engine.DocumentContext{Flavor: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if out := lint(t, r, in); out.Complete || len(out.Coverage) == 0 || out.Status != "incomplete" {
		t.Fatal(out)
	}
}

// Set MDCHK_RESULT_VALIDATOR_PYTHON to a Python with the pinned jsonschema
// requirements to additionally validate actual producer results against the
// accepted schema and its semantic validator. No production Python dependency.
func TestProducerSchema(t *testing.T) {
	r := registry(t, []engine.Metadata{metadata("MD001"), metadata("MD094")}, engine.Registration{ID: "MD001", Check: finding(0, 0)})
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	interrupted, err := r.Lint(cancelled, []engine.Input{input(t, "stopped", nil, nil)}, engine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	clean := registry(t, []engine.Metadata{metadata("MD001")}, engine.Registration{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { return engine.RuleOutcome{} }})
	outcomes := []result.Result{lint(t, clean, input(t, "clean", nil, nil)), lint(t, r, input(t, "violation", []byte("π"), nil)), lint(t, r, input(t, "bad", []byte{0xff}, nil)), interrupted, denseProjection(t), partialOperationalFailure(t)}
	python := os.Getenv("MDCHK_RESULT_VALIDATOR_PYTHON")
	dir := t.TempDir()
	paths := []string{}
	for i, out := range outcomes {
		data, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), ":null") {
			t.Fatal("null required array", string(data))
		}
		path := filepath.Join(dir, string(rune('a'+i))+".json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	if python == "" {
		t.Log("schema execution requires MDCHK_RESULT_VALIDATOR_PYTHON; producer envelopes passed local state assertions")
		return
	}
	script := `import json,runpy,sys
v=runpy.run_path('../../docs/contracts/results/validate.py')
for p in sys.argv[1:]:
 x=json.load(open(p));v['validator'].validate(x);v['semantic'](x)
print('actual lint producer outcomes validated')`
	args := append([]string{"-c", script}, paths...)
	output, err := exec.Command(python, args...).CombinedOutput() //nolint:gosec // G204: test-only interpreter path is explicitly supplied by the developer; arguments are fixed validation code and temporary fixture paths.
	if err != nil {
		t.Fatalf("schema validation: %v\n%s", err, output)
	}
	t.Log(string(output))
}

func TestSharedRuleAndDocumentBudget(t *testing.T) {
	limits := parser.DefaultLimits()
	limits.WorkUnits = 10000
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	r := registry(t, []engine.Metadata{metadata("MD001")}, engine.Registration{ID: "MD001", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
		calls++
		return engine.RuleOutcome{Err: c.Checkpoint(ctx, 6000)}
	}})
	out, err := r.Lint(context.Background(), []engine.Input{input(t, "a", nil, nil), input(t, "b", nil, nil), input(t, "c", nil, nil)}, engine.RunOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Reason != "limit-exceeded" || len(out.Errors) != 0 || out.Files[2].Analysis != "not-started" {
		t.Fatalf("document/rule work did not share one budget: calls=%d result=%+v", calls, out)
	}
	out, err = r.Lint(context.Background(), []engine.Input{input(t, "d", nil, nil)}, engine.RunOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || out.Complete || len(out.Interruptions) != 1 {
		t.Fatal("reused operation forgot exhaustion", out)
	}
}

func TestOversizePreflightAndInvalidBudget(t *testing.T) {
	calls := 0
	r := registry(t, []engine.Metadata{metadata("MD001")}, engine.Registration{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { calls++; return engine.RuleOutcome{} }})
	limits := parser.DefaultLimits()
	limits.DocumentBytes = 1
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Lint(context.Background(), []engine.Input{input(t, "a", nil, nil), input(t, "b", []byte("ab"), nil)}, engine.RunOptions{Budget: budget})
	if err == nil || calls != 0 {
		t.Fatal("known oversized batch reached rule execution")
	}
	_, err = r.Lint(context.Background(), nil, engine.RunOptions{Budget: &parser.OperationBudget{}})
	if err == nil {
		t.Fatal("invalid budget admitted")
	}
}

func TestAllReservedIDsRejected(t *testing.T) {
	for _, id := range []string{"MD002", "MD006", "MD008", "MD015", "MD016", "MD017"} {
		t.Run(id, func(t *testing.T) {
			if _, err := engine.NewRegistry([]engine.Metadata{metadata(id)}, nil); err == nil {
				t.Fatal("reserved metadata accepted")
			}
			if _, err := engine.NewRegistry([]engine.Metadata{metadata("MD001")}, []engine.Registration{{ID: id, Check: finding(0, 0)}}); err == nil {
				t.Fatal("reserved executable accepted")
			}
		})
	}
}

func denseProjection(t *testing.T) result.Result {
	t.Helper()
	source := []byte(strings.Repeat("π\t", 32768))
	m := metadata("MD001")
	r := registry(t, []engine.Metadata{m}, engine.Registration{ID: m.ID, Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
		findings := make([]engine.Finding, 0, 10000)
		for i := 0; i < 10000; i++ {
			span, _ := c.Source().Span(i*3, i*3+2)
			findings = append(findings, engine.Finding{Message: "pi", Span: span})
		}
		// This callback returns reliable observations alongside exhaustion. Projection
		// must retain what fits the one reporting reserve, not discard all findings.
		err := c.Checkpoint(ctx, 10000000)
		return engine.RuleOutcome{Findings: findings, Err: err}
	}})
	run := func() result.Result {
		limits := parser.DefaultLimits()
		limits.WorkUnits = 10000000
		budget, err := parser.NewOperationBudget(limits)
		if err != nil {
			t.Fatal(err)
		}
		out, err := r.Lint(context.Background(), []engine.Input{input(t, "dense", source, nil)}, engine.RunOptions{Budget: budget})
		if err != nil {
			t.Fatal(err)
		}
		if out.Complete || len(out.Diagnostics) == 0 || len(out.Diagnostics) >= 10000 || len(out.Omissions) != 1 || len(out.Interruptions) != 1 || len(out.Errors) != 0 {
			t.Fatalf("projection did not bound/retain findings: %d diagnostics, %+v", len(out.Diagnostics), out)
		}
		omission := out.Omissions[0]
		if omission.Section != "/diagnostics" || omission.Count == nil || *omission.Count != 10000-len(out.Diagnostics) {
			t.Fatal(omission)
		}
		for i, d := range out.Diagnostics {
			if d.Range.Start != (parser.Position{Line: 1, Column: 2*i + 1}) || d.Range.End != (parser.Position{Line: 1, Column: 2*i + 2}) {
				t.Fatal("dense Unicode mapping changed", d)
			}
		}
		return out
	}
	return run()
}
func TestDenseProjectionIsBoundedAndKeepsReliableFindings(t *testing.T) {
	first, second := denseProjection(t), denseProjection(t)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("budget-limited projection or IDs are nondeterministic")
	}
}

func partialOperationalFailure(t *testing.T) result.Result {
	t.Helper()
	r := registry(t, []engine.Metadata{metadata("MD001")}, engine.Registration{ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		span, err := c.Source().Span(6, 8)
		if err != nil {
			t.Fatal(err)
		}
		return engine.RuleOutcome{Findings: []engine.Finding{{Span: span, Message: "observed pi"}}, IncompleteReason: "Resource context could not be acquired.", Err: errors.New("resource acquisition failed")}
	}})
	out := lint(t, r, input(t, "partial.md", []byte("a\tπ\r\nπ"), nil))
	if out.Status != "error" || out.Complete || len(out.Diagnostics) != 1 || len(out.Coverage) != 2 || len(out.Errors) != 1 || len(out.Interruptions) != 0 {
		t.Fatal("lost partial callback observations", out)
	}
	d := out.Diagnostics[0]
	if d.Range.Start != (result.Position{Line: 2, Column: 1}) || d.Range.End != (result.Position{Line: 2, Column: 2}) || d.ByteRange.Start != 6 || d.ByteRange.End != 8 || out.Files[0].InputSnapshot == nil || *out.Files[0].InputSnapshot != d.Snapshot {
		t.Fatal("partial finding changed coordinates/snapshot", d)
	}
	for _, c := range out.Coverage {
		if c.Reason != result.ContextUnavailable || c.Rule != "MD001" || len(c.CauseIDs) != 1 || c.CauseIDs[0] != out.Errors[0].ID {
			t.Fatal("partial coverage lost cause", c)
		}
	}
	if len(out.Files[0].CoverageIDs) != 2 || len(out.Files[0].ErrorIDs) != 1 {
		t.Fatal(out.Files[0])
	}
	return out
}

func TestPartialCallbackPreservesFindingsCoverageAndOperationalError(t *testing.T) {
	partialOperationalFailure(t)
}

func TestDeadlineIsIncompleteWithoutOperationalError(t *testing.T) {
	r := registry(t, []engine.Metadata{metadata("MD001")}, engine.Registration{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome {
		return engine.RuleOutcome{Err: context.DeadlineExceeded}
	}})
	out := lint(t, r, input(t, "a", nil, nil))
	if out.Status != "incomplete" || out.Complete || len(out.Errors) != 0 || len(out.Interruptions) != 1 || out.Interruptions[0].Reason != "deadline-exceeded" || len(out.Coverage) != 1 || len(out.Coverage[0].CauseIDs) != 1 {
		t.Fatal(out)
	}
}

func TestCommonMarkExtensionLikeTextUsesOrdinaryInterpretation(t *testing.T) {
	m := metadata("MD001")
	m.RequiresSyntax = true
	calls := 0
	r := registry(t, []engine.Metadata{m}, engine.Registration{ID: m.ID, Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		calls++
		if c.Document() == nil {
			t.Fatal("ordinary CommonMark lost parser context")
		}
		return engine.RuleOutcome{}
	}})
	out := lint(t, r, input(t, "a", []byte("| a | b |\n| --- | --- |\n\n~~strike~~ [^note] {.class}\n\n::: custom\ntext\n:::\n"), nil))
	if calls != 1 || !out.Complete || out.Status != "clean" || len(out.Coverage) != 0 {
		t.Fatal(out)
	}
}

func TestAlreadyCancelledTargetsDoNotEnterEncodingGate(t *testing.T) {
	for _, raw := range [][]byte{[]byte("valid 雪\r\n"), {0xff}} {
		t.Run(fmt.Sprintf("%x", raw), func(t *testing.T) {
			calls := 0
			r := registry(t, []engine.Metadata{metadata("MD001"), metadata("MD094")}, engine.Registration{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome {
				calls++
				return engine.RuleOutcome{}
			}})
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			out, err := r.Lint(ctx, []engine.Input{input(t, "b.md", []byte("later"), nil), input(t, "a.md", raw, nil)}, engine.RunOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if calls != 0 || len(out.CheckedFiles) != 0 || len(out.Errors) != 0 || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 || out.Interruptions[0].Reason != "cancelled" || out.Interruptions[0].Phase != "acquisition" || out.Complete || out.Status != "incomplete" {
				t.Fatalf("pre-cancelled validation: %+v; callbacks=%d", out, calls)
			}
			if len(out.Files) != 2 || len(out.Coverage) != 2 {
				t.Fatal(out)
			}
			for _, file := range out.Files {
				if file.Analysis != "not-started" || file.InputSnapshot != nil || file.ResultSnapshot != nil || len(file.ErrorIDs) != 0 || len(file.CoverageIDs) != 1 {
					t.Fatalf("unvisited target was processed: %+v", file)
				}
			}
		})
	}
}
