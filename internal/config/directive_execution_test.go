package config_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

func TestAdmittedDirectivePolicyIsImmutableAndInherited(t *testing.T) {
	calls := 0
	r, err := engine.NewCatalogRegistry([]engine.Registration{
		{ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
			calls++
			s, _ := c.Source().Span(c.Source().Len(), c.Source().Len())
			return engine.RuleOutcome{Findings: []engine.Finding{{Message: "protected EOF", Span: s}}}
		}},
		{ID: "MD003", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome {
			t.Fatal("permitted disable did not skip configured-off callback")
			return engine.RuleOutcome{}
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	state := config.PolicyState{}
	for _, path := range []string{"ancestor/.mdchk.yaml", "middle/.mdchk.yaml"} {
		d, e := config.DecodeYAML(path, []byte("version: 1\nrules: {MD001: {allow_directives: false}}\n"))
		if e != nil {
			t.Fatal(e)
		}
		state, _, e = state.Apply(d, r, engine.ValidationProviders{}, config.Capabilities{})
		if e != nil {
			t.Fatal(e)
		}
	}
	var yaml strings.Builder
	yaml.WriteString("version: 1\nflavor: commonmark\nrules:\n")
	for _, m := range engine.Catalog() {
		if m.ID == "MD001" {
			yaml.WriteString("  MD001: {allow_directives: true}\n")
			continue
		}
		yaml.WriteString("  " + m.ID + ": {enabled: false}\n")
	}
	d, err := config.DecodeYAML("leaf/.mdchk.yaml", []byte(yaml.String()))
	if err != nil {
		t.Fatal(err)
	}
	v, err := config.ValidateDefinition(d, r, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	view, err := v.Admit(config.Capabilities{CommonMark: true, DirectiveProcessing: true}, state.Permissions())
	if err != nil {
		t.Fatal(err)
	}
	p := view.ExecutionPlan()
	view.Settings.DirectivesEnabled = false
	view.EffectivePermissions.RuleDirectives = nil
	view.Rules["MD001"] = engine.ResolvedRule{Enabled: false}
	const text = "雪\r\n<!-- mdchk:disable MD001 MD003 -->\r\n"
	in, err := p.NewInput("雪.md", []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(out.Diagnostics) != 1 || len(out.Notes) != 1 || !out.Complete || out.Status != "violations" {
		t.Fatalf("calls%d %+v", calls, out)
	}
	var note result.Note
	if err := json.Unmarshal(out.Notes[0], &note); err != nil {
		t.Fatal(err)
	}
	if note.Code != "directive-blocked" || note.Rule != "MD001" || note.Range.Start.Line != 2 || note.Range.Start.Column != 20 || !strings.Contains(note.Message, "rules.MD001.allow_directives") || strings.Contains(note.Message, "ancestor/") {
		t.Fatal(note)
	}
	a := out.AnalysisRecords()[0].(engine.DirectiveAnalysis)
	cs := a.State().Candidates()
	if len(cs) != 1 || cs[0].Effects[0].Disposition != engine.DirectiveBlocked || len(cs[0].Effects[0].Restrictions) != 2 || cs[0].Effects[1].Disposition != engine.DirectiveApplied {
		t.Fatal(cs)
	}
	eof, err := a.State().StateAt("MD001", len(text))
	if err != nil || !eof.Enabled || eof.Owner != -1 {
		t.Fatal(eof, err)
	}
	cs[0].Effects[0].Restrictions[0].Position.Line = 999
	if a.State().Candidates()[0].Effects[0].Restrictions[0].Position.Line == 999 {
		t.Fatal("restriction alias")
	}
}

func TestGlobalDirectiveDenialRetainsCandidatesWithoutSyntaxViolation(t *testing.T) {
	r, err := engine.NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	yaml := "version: 1\nflavor: commonmark\ndirectives: {enabled: false}\nrules:\n" + disabledCatalog()
	d, err := config.DecodeYAML(".mdchk.yaml", []byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	v, err := config.ValidateDefinition(d, r, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	view, err := v.Admit(config.Capabilities{CommonMark: true}, config.Permissions{})
	if err != nil {
		t.Fatal(err)
	}
	p := view.ExecutionPlan()
	in, err := p.NewInput("a.md", []byte("<!-- mdchk:INVALID MD999 -->"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Complete || len(out.Diagnostics) != 0 || len(out.Coverage) != 0 || len(out.Notes) != 1 {
		t.Fatal(out)
	}
	a := out.AnalysisRecords()[0].(engine.DirectiveAnalysis)
	if cs := a.State().Candidates(); len(cs) != 1 || cs[0].Disposition != engine.DirectiveInactive || len(a.State().Notes()[0].Restrictions) != 1 {
		t.Fatal(cs)
	}
}
func disabledCatalog() string {
	var b strings.Builder
	for _, m := range engine.Catalog() {
		b.WriteString("  " + m.ID + ": {enabled: false}\n")
	}
	return b.String()
}
