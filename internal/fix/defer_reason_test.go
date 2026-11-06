package fix_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
)

func deferSetup(t *testing.T, check fix.Checker) (*fix.Validator, *engine.ExecutionPlan) {
	t.Helper()
	registry, err := engine.NewRegistry([]engine.Metadata{{ID: "MD001", Name: "deferral fixture", DefaultEnabled: true, Fix: engine.PlannedFix}}, []engine.Registration{{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { return engine.RuleOutcome{} }}})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := registry.SelectRules(nil, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selected.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := fix.NewValidator(registry, []fix.CheckerRegistration{{Rule: "MD001", Check: check}})
	if err != nil {
		t.Fatal(err)
	}
	return validator, plan
}

func TestProducerDeferReason(t *testing.T) {
	validator, plan := deferSetup(t, func(context.Context, fix.CheckContext) (fix.Proof, error) { return fix.Preserved, nil })
	for _, tc := range []struct {
		name   string
		reason string
		edit   bool
		want   string
	}{{"named", "meaning-not-proven", false, "meaning-not-proven"}, {"unnamed", "", false, "empty-group"}, {"with edits", "meaning-not-proven", true, "invalid-deferral"}} {
		t.Run(tc.name, func(t *testing.T) {
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			produce := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
				s, err := c.Current().Span(1, 2)
				if err != nil {
					return nil, err
				}
				g := fix.Group{ID: "g", Rule: "MD001", Anchor: s, DeferReason: tc.reason, Requirements: []fix.Requirement{{Kind: fix.Meaning}}}
				if tc.edit {
					g.Edits = []fix.Edit{{Span: s, Expected: []byte(" ")}}
				}
				return []fix.Group{g}, nil
			}
			source := "a \n"
			out, err := validator.Validate(context.Background(), []fix.Target{{Path: "doc.md", Source: parser.NewSource([]byte(source)), Plan: plan, Produce: produce}}, fix.ValidationOptions{Budget: budget})
			if err != nil {
				t.Fatal(err)
			}
			view, _ := out.File("doc.md")
			d := view.Attempts[0].Decisions[0]
			if string(view.Candidate.Bytes()) != source || d.Accepted || d.Reasons[0].Code != tc.want {
				t.Fatalf("%+v", d)
			}
		})
	}
}
