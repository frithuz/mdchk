package rules

import (
	"context"
	"encoding/json"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"os"
	"reflect"
	"strings"
	"testing"
)

func registry(t *testing.T, registrations []engine.Registration) *engine.Registry {
	t.Helper()
	r, err := engine.NewCatalogRegistry(registrations)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func settings() engine.Settings {
	out := engine.Settings{}
	for _, m := range engine.Catalog() {
		v := m.ID == "MD047"
		out[m.ID] = engine.RuleSetting{Enabled: &v}
	}
	return out
}
func analyze(t *testing.T, r *engine.Registry, text string, s engine.Settings, opts engine.RunOptions) result.Result {
	t.Helper()
	caller := []byte(text)
	in, err := engine.NewInput("doc.md", caller, s, engine.DocumentContext{Flavor: "commonmark"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Lint(context.Background(), []engine.Input{in}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if string(caller) != text || string(in.Source().Bytes()) != text || len(out.Changes) != 0 || len(out.Documents) != 0 || out.Files[0].WriteStatus != "not-requested" {
		t.Fatal("analysis changed source or requested edits", out)
	}
	return out
}
func TestMD047AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md047.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source string
		Expected     []int
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	r := registry(t, Registrations())
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			out := analyze(t, r, f.Source, settings(), engine.RunOptions{})
			again := analyze(t, r, f.Source, settings(), engine.RunOptions{})
			if !reflect.DeepEqual(out, again) {
				t.Fatal("nondeterministic analysis")
			}
			if !out.Complete || len(out.Errors) != 0 {
				t.Fatal(out)
			}
			if f.Expected == nil {
				if len(out.Diagnostics) != 0 || out.Status != "clean" {
					t.Fatal(out)
				}
				return
			}
			if len(out.Diagnostics) != 1 || out.Status != "violations" {
				t.Fatal(out)
			}
			d := out.Diagnostics[0]
			got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
			if d.Rule != "MD047" || !reflect.DeepEqual(got, f.Expected) {
				t.Fatalf("got %v want %v: %+v", got, f.Expected, d)
			}
			want := "File must end with exactly one line-ending sequence."
			if f.Expected[0] == f.Expected[1] {
				want = "File must end with a line-ending sequence."
			}
			if d.Message != want {
				t.Fatal(d)
			}
		})
	}
}
func TestMD047ProtectedContainersAndRetainedSourceConsumers(t *testing.T) {
	prefixes := []string{"```\ncode", "```\ncode\n```", "    code", "<div>raw", "<!-- comment -->", "<!-- unclosed", "> - nested\n>   text", "[ref]: /url"}
	for _, name := range []string{"mixed", "references"} {
		//nolint:gosec // Fixed retained parser fixtures in this repository.
		data, err := os.ReadFile("../parser/testdata/source_consumers/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var fixture struct{ Source string }
		if err = json.Unmarshal(data, &fixture); err != nil {
			t.Fatal(err)
		}
		prefixes = append(prefixes, fixture.Source)
	}
	r := registry(t, Registrations())
	for _, prefix := range prefixes {
		for _, ending := range []string{"\n", "\r\n", "\r"} {
			for count := 0; count < 4; count++ {
				text := strings.ReplaceAll(prefix, "\n", ending) + strings.Repeat(ending, count)
				out := analyze(t, r, text, settings(), engine.RunOptions{})
				if !out.Complete {
					t.Fatal(out)
				}
				if count == 1 {
					if len(out.Diagnostics) != 0 {
						t.Fatal(out)
					}
					continue
				}
				if len(out.Diagnostics) != 1 {
					t.Fatal(out)
				}
				start := len(text)
				if count > 1 {
					start = len(text) - (count-1)*len(ending)
				}
				if out.Diagnostics[0].ByteRange != (result.ByteRange{Start: start, End: len(text)}) {
					t.Fatal(out.Diagnostics)
				}
			}
		}
	}
}
func TestMD047SharedConfigurationAndOwnership(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD047") || r.Available("MD001") {
		t.Fatal("incorrect availability")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD047" && (!m.DefaultEnabled || m.RequiresSyntax || m.Fix != engine.ImplementedFix || len(m.Options) != 0) {
			t.Fatal(m)
		}
	}
	for _, mapping := range []map[string]any{{"enabled": "false"}, {"enabled": nil}, {"allow_directives": 0}, {"allow_directives": nil}, {"enabled": false, "unknown": true}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD047": setting}, engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal("accepted invalid settings", mapping)
		}
	}
	cases := []struct {
		name, text            string
		enabled, allowed      bool
		active, hidden, owner int
	}{
		{"missing disabled", "<!-- mdchk:disable MD047 -->tail", true, true, 0, 1, 0},
		{"EOF enable", "<!-- mdchk:enable MD047 -->", false, true, 1, 0, 0},
		{"EOF disable", "text<!-- mdchk:disable MD047 -->", true, true, 0, 1, 0},
		{"repeated missing", "<!-- mdchk:disable MD047 --><!-- mdchk:disable MD047 -->tail", true, true, 0, 1, 1},
		{"repeated surplus", "<!-- mdchk:disable MD047 --><!-- mdchk:disable MD047 -->\r\n\r\n", true, true, 0, 1, 1},
		{"permission blocked", "<!-- mdchk:disable MD047 -->tail", true, false, 1, 0, 0},
		{"literal comment", "```\n<!-- mdchk:disable MD047 -->", true, true, 1, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := settings()
			s["MD047"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allowed}
			out := analyze(t, r, tc.text, s, engine.RunOptions{})
			if len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden || !out.Complete {
				t.Fatal(out)
			}
			if tc.hidden > 0 {
				a := out.AnalysisRecords()[0].(engine.DirectiveAnalysis)
				hidden := a.Suppressed()
				if len(hidden) != 1 || hidden[0].Owner != tc.owner {
					t.Fatal(hidden)
				}
				want := len(tc.text)
				if strings.HasSuffix(tc.text, "\r\n\r\n") {
					want -= 2
				}
				if hidden[0].Diagnostic.ByteRange.Start != want {
					t.Fatal(hidden)
				}
			}
		})
	}
	s := settings()
	no := false
	s["MD047"] = engine.RuleSetting{Enabled: &no}
	if out := analyze(t, r, "missing", s, engine.RunOptions{}); len(out.Diagnostics) != 0 || !out.Complete {
		t.Fatal(out)
	}
}

// The callback gets the engine's real immutable context and residual budget.
// Interrupt after it has entered the suffix traversal, rather than cancelling
// before acquisition and accidentally testing only the engine's early gate.
type traversalContext struct {
	context.Context
	calls int
	err   error
}

func (c *traversalContext) Err() error {
	c.calls++
	if c.calls >= 4 {
		return c.err
	}
	return nil
}
func TestMD047SharedInterruptionsAndUTF8(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		r := registry(t, []engine.Registration{{ID: "MD047", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			return terminatingNewline(&traversalContext{Context: ctx, err: reason}, c)
		}}})
		out := analyze(t, r, strings.Repeat("\r\n", 64), settings(), engine.RunOptions{})
		if out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "analysis" || len(out.Diagnostics) != 0 {
			t.Fatal(out)
		}
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	r := registry(t, []engine.Registration{{ID: "MD047", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
		_ = budget.ChargeWork(budget.Limits().WorkUnits)
		return terminatingNewline(ctx, c)
	}}})
	out := analyze(t, r, "x\n\n", settings(), engine.RunOptions{Budget: budget})
	if out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Limit == nil || out.Interruptions[0].Limit.Name != "work-units" || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
	out = analyze(t, registry(t, Registrations()), string([]byte{'x', 0xff}), settings(), engine.RunOptions{})
	if len(out.Errors) != 1 || out.Errors[0].Code != "invalid-utf8" || len(out.Diagnostics) != 0 || out.Complete {
		t.Fatal(out)
	}
}

func TestMD047InactiveObservationUsesRealCallback(t *testing.T) {
	r := registry(t, Registrations())
	s := settings()
	off := false
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	selection, err := r.SelectRules(s, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	in, err := plan.NewInput("doc.md", []byte("x\r\n\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{ObserveInactive: true})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Complete || out.Status != "clean" || len(out.Diagnostics) != 0 || len(trace.Suppressed()) != 1 {
		t.Fatal(out, trace.Suppressed())
	}
	hidden := trace.Suppressed()[0]
	if hidden.Owner != -1 || hidden.Diagnostic.ByteRange != (result.ByteRange{Start: 3, End: 5}) || hidden.Diagnostic.FixAvailable {
		t.Fatal(hidden)
	}
	found := false
	for _, evaluation := range trace.Evaluations() {
		if evaluation.Rule == "MD047" {
			found = true
			if evaluation.Active != "skipped" || evaluation.Observational != "complete" || len(evaluation.ActiveCoverage) != 0 || len(evaluation.ObservationalCoverage) != 1 {
				t.Fatal(evaluation)
			}
		}
	}
	if !found {
		t.Fatal("missing execution record")
	}
}
