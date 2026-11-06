package rules

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func TestMD084OptionsAndAvailability(t *testing.T) {
	r := registry(t, Registrations())
	found := false
	for _, m := range r.Metadata() {
		if m.ID == "MD084" {
			found = true
			if m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.NoFix || len(m.Options) != 1 || m.Options["allowed_codepoints"].Domain != engine.UnicodeScalarHex {
				t.Fatal(m)
			}
		}
	}
	if !found || !r.Available("MD084") {
		t.Fatal("missing executable MD084")
	}
	for _, enabled := range []bool{false, true} {
		for _, options := range []engine.Options{{"allowed_codepoints": nil}, {"allowed_codepoints": "200b"}, {"allowed_codepoints": []any{"200b", 1}}, {"allowed_codepoints": []string{"U+200B"}}, {"allowed_codepoints": []string{""}}, {"allowed_codepoints": []string{"0x200b"}}, {"allowed_codepoints": []string{"D800"}}, {"allowed_codepoints": []string{"110000"}}, {"unknown": true}} {
			if _, err := r.ResolveSettings(engine.Settings{"MD084": {Enabled: &enabled, Options: options}}, engine.ValidationProviders{}); err == nil {
				t.Fatal(enabled, options)
			}
		}
	}
	allowed := []string{"200b", "FEFF", "00202E"}
	resolved, err := r.ResolveSettings(engine.Settings{"MD084": {Options: engine.Options{"allowed_codepoints": allowed}}}, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	expected := append([]string(nil), allowed...)
	allowed[0] = "changed"
	if !reflect.DeepEqual(resolved["MD084"].Options["allowed_codepoints"], expected) {
		t.Fatal(resolved)
	}
	defaults, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil || defaults["MD084"].Enabled || !reflect.DeepEqual(defaults["MD084"].Options["allowed_codepoints"], []string{}) {
		t.Fatal(defaults, err)
	}
	s := md084Settings(nil)
	delete(s, "MD084")
	out := analyzeHeadings(t, r, "a\u200bb", s, engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
}

func TestMD084StrictRuleContextOptions(t *testing.T) {
	// MD061's options are not MD084's shape, so the accessor must refuse them.
	var spans []parser.Span
	var observed error
	r := registry(t, []engine.Registration{{ID: "MD061", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
		spans, observed = c.HiddenCharacters(ctx)
		return engine.RuleOutcome{Err: observed}
	}}})
	off, on := false, true
	s := onlyDefaultMD038Settings()
	s["MD038"] = engine.RuleSetting{Enabled: &off}
	s["MD061"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"terms": []string{"a"}}}
	out := analyzeHeadings(t, r, "a\u200bb", s, engine.DocumentContext{}, engine.RunOptions{})
	if !errors.Is(observed, parser.ErrModel) || spans != nil || out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(observed, spans, out)
	}
}

func TestMD084BudgetExhaustion(t *testing.T) {
	source := "a\u200bb \u202ec\n\n- d\ufeffe\n\n> f\u202ag"
	for _, kind := range []string{"work", "nodes"} {
		var spans []parser.Span
		var observed error
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		r := registry(t, []engine.Registration{{ID: "MD084", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			if kind == "work" {
				_ = budget.ChargeWork(budget.Limits().WorkUnits)
			} else {
				_ = budget.ChargeNodes(budget.Limits().TotalNodes)
			}
			spans, observed = c.HiddenCharacters(ctx)
			return hiddenCharacters(ctx, c)
		}}})
		out := analyzeHeadings(t, r, source, md084Settings(nil), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
		if observed == nil || spans != nil || out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "analysis" || out.Interruptions[0].Limit == nil || len(out.Errors) != 0 || len(out.Diagnostics) != 0 {
			t.Fatal(kind, observed, out)
		}
	}
	// Each work allowance either yields every projected span or none of them.
	type observation struct {
		spans    int
		err      error
		complete bool
		reported int
	}
	run := func(allowance int64) (o observation) {
		limits := parser.DefaultLimits()
		limits.WorkUnits = allowance
		budget, err := parser.NewOperationBudget(limits)
		if err != nil {
			t.Fatal(err)
		}
		o.err = errors.New("projection not reached")
		r := registry(t, []engine.Registration{{ID: "MD084", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			var spans []parser.Span
			spans, o.err = c.HiddenCharacters(ctx)
			o.spans = len(spans)
			if o.err != nil && spans != nil {
				t.Fatal(allowance, "spans returned with error")
			}
			return reportHiddenCharacters(ctx, c, spans)
		}}})
		out := analyzeHeadings(t, r, source, md084Settings(nil), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
		o.complete, o.reported = out.Complete, len(out.Diagnostics)
		return o
	}
	low, high := int64(1), int64(1<<20)
	if !run(high).complete {
		t.Fatal("no allowance completed")
	}
	for low < high {
		if mid := (low + high) / 2; run(mid).complete {
			high = mid
		} else {
			low = mid + 1
		}
	}
	exhausted := 0
	for allowance := max(1, high-3000); allowance <= high; allowance++ {
		o := run(allowance)
		if o.err == nil && o.spans != 4 || o.err != nil && o.spans != 0 || o.reported != 0 && o.reported != 4 || allowance == high && (!o.complete || o.reported != 4) {
			t.Fatal(allowance, o)
		}
		if o.err != nil && o.err.Error() != "projection not reached" {
			exhausted++
		}
	}
	if exhausted == 0 {
		t.Fatal("projection never exhausted")
	}
}

func TestMD084SourceUnchanged(t *testing.T) {
	source := "\ufeffa\u200bb\r\n\r\n| c\u202bd |\r\n| --- |\r\n"
	caller := []byte(source)
	in, err := engine.NewInput("doc.md", caller, md084Settings([]string{"202b"}), engine.DocumentContext{Flavor: "gfm"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := registry(t, Registrations()).Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange.Start != 4 || string(caller) != source || string(in.Source().Bytes()) != source || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
		t.Fatal(out, err)
	}
}
