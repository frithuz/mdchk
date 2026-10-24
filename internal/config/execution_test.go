package config_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// These tests cross the actual YAML, catalog, admission, input and lint boundaries.
// Checks are deliberately tiny: their presence is distinct from a missing rule.
func executionRegistry(t *testing.T, omit map[string]bool, checks map[string]engine.Check, reverse bool) *engine.Registry {
	t.Helper()
	var registrations []engine.Registration
	for _, m := range engine.Catalog() {
		if m.ID == "MD094" || omit[m.ID] {
			continue
		}
		check := checks[m.ID]
		if check == nil {
			check = func(context.Context, engine.RuleContext) engine.RuleOutcome { return engine.RuleOutcome{} }
		}
		registrations = append(registrations, engine.Registration{ID: m.ID, Check: check})
	}
	if reverse {
		slices.Reverse(registrations)
	}
	r, err := engine.NewCatalogRegistry(registrations)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func executionValidated(t *testing.T, r *engine.Registry, yaml string, providers engine.ValidationProviders) *config.Validated {
	t.Helper()
	d, err := config.DecodeYAML(".mdchk.yaml", []byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	v, err := config.ValidateDefinition(d, r, providers)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func executionResolution(t *testing.T, r *engine.Registry, extra string, providers engine.ValidationProviders) *config.Resolution {
	t.Helper()
	// An ancestor permission is resolved through the same syntax/schema boundary.
	ancestor := executionValidated(t, r, "version: 1\ndirectives: {enabled: false}\n", engine.ValidationProviders{})
	v := executionValidated(t, r, "version: 1\nflavor: commonmark\n"+extra, providers)
	resolved, err := v.Admit(config.Capabilities{CommonMark: true}, ancestor.Permissions(config.Permissions{}))
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func executionInput(t *testing.T, p *config.ExecutionPlan, path string, b []byte) engine.Input {
	t.Helper()
	in, err := p.NewInput(path, b)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func executionLint(t *testing.T, p *config.ExecutionPlan, ctx context.Context, inputs ...engine.Input) result.Result {
	t.Helper()
	out, err := p.Lint(ctx, inputs, engine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func executionFinding(c engine.RuleContext) engine.Finding {
	span, err := c.Source().Span(0, 1)
	if err != nil {
		panic(err)
	}
	return engine.Finding{Message: "retained finding", Span: span}
}

func TestExecutionMissingDefaultIsCoverageNotEmptySuccess(t *testing.T) {
	for _, finding := range []bool{false, true} {
		t.Run(fmt.Sprint(finding), func(t *testing.T) {
			checks := map[string]engine.Check{"MD003": func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
				if finding {
					return engine.RuleOutcome{Findings: []engine.Finding{executionFinding(c)}}
				}
				return engine.RuleOutcome{}
			}}
			r := executionRegistry(t, map[string]bool{"MD001": true}, checks, false)
			yaml := []byte("version: 1\nflavor: commonmark\ndirectives: {enabled: false}\n")
			original := slices.Clone(yaml)
			d, err := config.DecodeYAML(".mdchk.yaml", yaml)
			if err != nil {
				t.Fatal(err)
			}
			v, err := config.ValidateDefinition(d, r, engine.ValidationProviders{})
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := v.Admit(config.Capabilities{CommonMark: true}, config.Permissions{})
			if err != nil {
				t.Fatal(err)
			}
			if resolved.Settings.DirectivesEnabled || resolved.Rules["MD003"].AllowDirectives {
				t.Fatal("ancestor denial was lost")
			}
			p := resolved.ExecutionPlan()
			out := executionLint(t, p, context.Background(), executionInput(t, p, "a.md", []byte("text\n")))
			if out.Complete || out.Status != "incomplete" || len(out.Errors) != 0 || len(out.Coverage) != 1 {
				t.Fatalf("missing rule result: %+v", out)
			}
			if c := out.Coverage[0]; c.Rule != "MD001" || c.Reason != result.RuleUnavailable || c.Path != "a.md" || c.Range != nil || c.Snapshot != nil {
				t.Fatalf("coverage: %+v", c)
			}
			if !slices.Equal(yaml, original) {
				t.Fatal("configuration bytes changed during validation or execution")
			}
			want := 0
			if finding {
				want = 1
			}
			if len(out.Diagnostics) != want {
				t.Fatalf("diagnostics: %+v", out.Diagnostics)
			}
		})
	}
}

func TestExecutionExplicitMissingFailsBeforeTargets(t *testing.T) {
	calls := 0
	r := executionRegistry(t, map[string]bool{"MD001": true}, map[string]engine.Check{"MD003": func(context.Context, engine.RuleContext) engine.RuleOutcome { calls++; return engine.RuleOutcome{} }}, false)
	v := executionValidated(t, r, "version: 1\nflavor: commonmark\ndirectives: {enabled: false}\nrules: {MD001: {enabled: true}}\n", engine.ValidationProviders{})
	resolved, err := v.Admit(config.Capabilities{CommonMark: true}, config.Permissions{})
	if err == nil || resolved != nil || !strings.Contains(err.Error(), "MD001") || calls != 0 {
		t.Fatalf("admission=%v, %v; calls=%d", resolved, err, calls)
	}
}

func TestExecutionDisabledAndDialectInapplicableMissingAreComplete(t *testing.T) {
	// MD055 and MD066 are default-enabled but genuinely absent in CommonMark.
	r := executionRegistry(t, map[string]bool{"MD001": true, "MD055": true, "MD066": true}, nil, false)
	p := executionResolution(t, r, "rules: {MD001: {enabled: false}}\n", engine.ValidationProviders{}).ExecutionPlan()
	out := executionLint(t, p, context.Background(), executionInput(t, p, "a.md", []byte("ordinary\n")))
	if !out.Complete || out.Status != "clean" || len(out.Coverage) != 0 {
		t.Fatalf("inapplicable or disabled rules penalized: %+v", out)
	}
}

func TestExecutionContextFailurePreservesFindingAndOperationalError(t *testing.T) {
	r := executionRegistry(t, map[string]bool{"MD004": true}, map[string]engine.Check{"MD001": func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		return engine.RuleOutcome{Findings: []engine.Finding{executionFinding(c)}, IncompleteReason: "resource context unavailable", Err: errors.New("provider failed")}
	}}, false)
	p := executionResolution(t, r, "", engine.ValidationProviders{}).ExecutionPlan()
	out := executionLint(t, p, context.Background(), executionInput(t, p, "a.md", []byte("text\n")))
	if out.Complete || out.Status != "error" || len(out.Diagnostics) != 1 || len(out.Errors) != 1 || out.Errors[0].Code != "rule-failure" {
		t.Fatalf("partial finding/error lost: %+v", out)
	}
	contextSeen, missingSeen := false, false
	for _, c := range out.Coverage {
		if c.Rule == "MD001" && c.Reason == result.ContextUnavailable {
			contextSeen = true
			continue
		}
		if c.Rule == "MD004" && c.Reason == result.RuleUnavailable {
			missingSeen = true
			continue
		}
		t.Fatalf("unexpected coverage: %+v", c)
	}
	if !contextSeen || !missingSeen {
		t.Fatalf("independent deficits lost: %+v", out.Coverage)
	}
}

func TestExecutionCommonMarkExtensionLikeTextIsOrdinaryComplete(t *testing.T) {
	calls := 0
	r := executionRegistry(t, nil, map[string]engine.Check{"MD001": func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		calls++
		if c.Document() == nil {
			t.Error("CommonMark syntax unavailable")
		}
		return engine.RuleOutcome{}
	}}, false)
	p := executionResolution(t, r, "", engine.ValidationProviders{}).ExecutionPlan()
	out := executionLint(t, p, context.Background(), executionInput(t, p, "a.md", []byte("| a | b |\n| - | - |\n\n[^note]: ordinary text\n\n~~ordinary~~\n")))
	if !out.Complete || calls != 1 || len(out.Coverage) != 0 {
		t.Fatalf("ordinary CommonMark result: %+v calls=%d", out, calls)
	}
}

func TestExecutionCancellationRetainsReliableFindingAndStopsLaterWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	later := 0
	r := executionRegistry(t, nil, map[string]engine.Check{
		"MD001": func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
			f := executionFinding(c)
			cancel()
			return engine.RuleOutcome{Findings: []engine.Finding{f}, Err: context.Canceled}
		},
		"MD003": func(context.Context, engine.RuleContext) engine.RuleOutcome { later++; return engine.RuleOutcome{} },
	}, false)
	p := executionResolution(t, r, "", engine.ValidationProviders{}).ExecutionPlan()
	out := executionLint(t, p, ctx, executionInput(t, p, "z.md", []byte("later\n")), executionInput(t, p, "a.md", []byte("first\n")))
	if out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Path != "a.md" || later != 0 || len(out.Interruptions) != 1 || out.Interruptions[0].Reason != "cancelled" {
		t.Fatalf("cancellation result: %+v later=%d", out, later)
	}
	if len(out.CheckedFiles) != 1 || out.CheckedFiles[0] != "a.md" || len(out.Files) != 2 || out.Files[1].Analysis != "not-started" {
		t.Fatalf("later target started: %+v", out)
	}
}

func TestExecutionCanonicalAcrossRegistrationAndInputOrder(t *testing.T) {
	var outputs []result.Result
	for _, reverse := range []bool{false, true} {
		checks := map[string]engine.Check{"MD003": func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
			return engine.RuleOutcome{Findings: []engine.Finding{executionFinding(c)}}
		}}
		r := executionRegistry(t, map[string]bool{"MD001": true}, checks, reverse)
		p := executionResolution(t, r, "", engine.ValidationProviders{}).ExecutionPlan()
		inputs := []engine.Input{executionInput(t, p, "a.md", []byte("first\n")), executionInput(t, p, "z.md", []byte("last\n"))}
		if reverse {
			slices.Reverse(inputs)
		}
		outputs = append(outputs, executionLint(t, p, context.Background(), inputs...))
	}
	if !reflect.DeepEqual(outputs[0], outputs[1]) {
		t.Fatalf("order changed canonical result:\n%+v\n%+v", outputs[0], outputs[1])
	}
}

func TestExecutionEncodingGatePreservesBytesAndContinuesIndependentFile(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			var seen []string
			r := executionRegistry(t, nil, map[string]engine.Check{"MD001": func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
				seen = append(seen, c.Path())
				return engine.RuleOutcome{}
			}}, false)
			p := executionResolution(t, r, fmt.Sprintf("rules: {MD094: {enabled: %t}}\n", enabled), engine.ValidationProviders{}).ExecutionPlan()
			original := []byte{0xff, 'x', '\r', '\n'}
			bad := executionInput(t, p, "a.md", original)
			original[0] = 'a'
			if got := bad.Source().Bytes(); !slices.Equal(got, []byte{0xff, 'x', '\r', '\n'}) {
				t.Fatalf("input aliased source: %v", got)
			}
			out := executionLint(t, p, context.Background(), bad, executionInput(t, p, "b.md", []byte("valid\n")))
			if out.Complete || len(out.Errors) != 1 || out.Errors[0].Code != "invalid-utf8" || out.Errors[0].ByteOffset == nil || *out.Errors[0].ByteOffset != 0 || !slices.Equal(seen, []string{"b.md"}) {
				t.Fatalf("UTF-8 gate: %+v seen=%v", out, seen)
			}
			wantRule := ""
			if enabled {
				wantRule = "MD094"
			}
			e := out.Errors[0]
			if e.Rule != wantRule || e.Snapshot == nil || e.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte{0xff, 'x', '\r', '\n'})) || e.Range == nil || e.Range.Start.Line != 1 || e.Range.Start.Column != 1 {
				t.Fatalf("gate provenance: %+v", e)
			}
			if out.Files[0].Analysis != "failed" || out.Files[1].Analysis != "complete" {
				t.Fatalf("file states: %+v", out.Files)
			}
		})
	}
}

func TestExecutionPlanOwnsAuthorityAndDoesNotRepeatSemanticProviders(t *testing.T) {
	calls := 0
	providers := engine.ValidationProviders{ValidateHostname: func(host string) error {
		calls++
		if host != "example.org" {
			t.Errorf("host=%q", host)
		}
		return nil
	}}
	r := executionRegistry(t, nil, nil, false)
	resolved := executionResolution(t, r, "rules: {MD095: {enabled: false, allowed_hosts: [example.org]}}\n", providers)
	if calls != 1 {
		t.Fatalf("schema provider calls=%d", calls)
	}
	p := resolved.ExecutionPlan()
	view := p.Configuration()
	if view.Settings.Flavor != "commonmark" || view.Settings.DirectivesEnabled || len(view.EffectivePermissions.Directives) != 1 || len(view.OwnPermissions.Directives) != 0 {
		t.Fatalf("admitted configuration lost ancestor permissions: %+v", view)
	}
	origin := view.EffectivePermissions.Directives[0]
	if origin.Path != ".mdchk.yaml" || origin.PropertyPath != "directives.enabled" || origin.Position == nil || origin.Position.Line != 2 {
		t.Fatalf("ancestor permission lost provenance: %+v", origin)
	}
	view.Settings.Flavor = "gfm"
	view.Settings.FrontmatterFormats[0] = "changed"
	view.EffectivePermissions.Directives[0].Position.Line = 99
	view.EffectivePermissions.Directives[0].Path = "changed"
	view.Origins["flavor"] = config.Origin{Path: "changed"}
	view.Rules["MD095"].Options["allowed_hosts"].([]string)[0] = "view.invalid"
	again := p.Configuration()
	if again.Settings.Flavor != "commonmark" || again.Settings.FrontmatterFormats[0] != "yaml" || again.EffectivePermissions.Directives[0].Position.Line != 2 || again.EffectivePermissions.Directives[0].Path != ".mdchk.yaml" || again.Origins["flavor"].Path != ".mdchk.yaml" {
		t.Fatalf("configuration view mutated plan: %+v", again)
	}
	resolved.EffectivePermissions.Directives[0].Position.Line = 100
	resolved.Settings.Flavor = "gfm"
	resolved.Rules["MD001"] = engine.ResolvedRule{}
	resolved.Rules["MD095"].Options["allowed_hosts"].([]string)[0] = "changed.invalid"
	rules := p.Rules()
	delete(rules, "MD001")
	rules["MD095"].Options["allowed_hosts"].([]string)[0] = "also.invalid"
	if !p.Rules()["MD001"].Enabled || p.Rules()["MD095"].Options["allowed_hosts"].([]string)[0] != "example.org" {
		t.Fatal("passive mutation changed authority")
	}
	out := executionLint(t, p, context.Background(), executionInput(t, p, "a.md", []byte("valid\n")), executionInput(t, p, "b.md", []byte("valid\n")))
	if !out.Complete || calls != 1 {
		t.Fatalf("execution revalidated providers or lost settings: %+v calls=%d", out, calls)
	}
	forged := &config.Resolution{Settings: resolved.Settings, Rules: resolved.Rules}
	if forged.ExecutionPlan() != nil {
		t.Fatal("passive resolution forged execution authority")
	}
}

func TestExecutionExplicitOptionsAndResultsRemainIndependent(t *testing.T) {
	calls, disabledCalls := 0, 0
	r := executionRegistry(t, nil, map[string]engine.Check{
		"MD013": func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
			calls++
			if c.Options()["length"] != 80 {
				t.Errorf("explicit option lost: %+v", c.Options())
			}
			return engine.RuleOutcome{Findings: []engine.Finding{executionFinding(c)}}
		},
		"MD003": func(context.Context, engine.RuleContext) engine.RuleOutcome {
			disabledCalls++
			return engine.RuleOutcome{}
		},
	}, false)
	resolved := executionResolution(t, r, "rules: {MD013: {enabled: true, length: 80}, MD003: {enabled: false}}\n", engine.ValidationProviders{})
	p := resolved.ExecutionPlan()
	if state := p.Rules()["MD013"]; !state.Enabled || !state.Explicit || state.Options["length"] != 80 {
		t.Fatalf("selection lost explicit enablement/options: %+v", state)
	}
	in := executionInput(t, p, "a.md", []byte("valid\n"))
	first := executionLint(t, p, context.Background(), in)
	if !first.Complete || len(first.Diagnostics) != 1 || calls != 1 || disabledCalls != 0 {
		t.Fatalf("explicit selection not executed: %+v calls=%d/%d", first, calls, disabledCalls)
	}
	expected := executionLint(t, p, context.Background(), in)
	first.Diagnostics[0].Message = "changed"
	first.Diagnostics[0].Snapshot.SHA256 = "changed"
	first.Files[0].InputSnapshot.SHA256 = "changed"
	first.CheckedFiles[0] = "changed"
	again := executionLint(t, p, context.Background(), in)
	if !reflect.DeepEqual(expected, again) || disabledCalls != 0 {
		t.Fatalf("earlier result mutated plan/input: %+v", again)
	}
}

func TestExecutionUsesRemainingSharedOperationBudget(t *testing.T) {
	calls := 0
	r := executionRegistry(t, nil, map[string]engine.Check{"MD001": func(context.Context, engine.RuleContext) engine.RuleOutcome { calls++; return engine.RuleOutcome{} }}, false)
	p := executionResolution(t, r, "", engine.ValidationProviders{}).ExecutionPlan()
	limits := parser.DefaultLimits()
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ChargeWork(limits.WorkUnits - 1); err != nil {
		t.Fatal(err)
	}
	in := executionInput(t, p, "a.md", []byte("original\n"))
	out, err := p.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	if out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Reason != "limit-exceeded" || out.Interruptions[0].Limit == nil || out.Interruptions[0].Limit.Name != "work-units" || calls != 0 {
		t.Fatalf("shared budget was reset: %+v calls=%d", out, calls)
	}
}
