package engine_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

func TestExecutionPlanRetainsRegistryAndMembership(t *testing.T) {
	r, err := engine.NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := r.SelectRules(nil, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	denied := map[string]bool{"MD001": true}
	p, err := selection.Plan(engine.DocumentContext{Flavor: "commonmark"}, denied)
	if err != nil {
		t.Fatal(err)
	}
	denied["MD001"] = false
	view := selection.Rules()
	delete(view, "MD001")
	if rule := p.Rules()["MD001"]; rule.Explicit || !rule.Enabled || rule.AllowDirectives {
		t.Fatalf("lost membership/denial: %+v", rule)
	}
	in, err := p.NewInput("a.md", []byte("text\n"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := engine.NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{}); err == nil {
		t.Fatal("accepted another registry's plan")
	}
	otherPlan, err := selection.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := otherPlan.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{}); err == nil {
		t.Fatal("accepted another plan's input")
	}
	plain, err := engine.NewInput("a.md", []byte("text\n"), nil, engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Lint(context.Background(), []engine.Input{plain}, engine.RunOptions{}); err == nil {
		t.Fatal("accepted unadmitted input")
	}
	if _, err := selection.Plan(engine.DocumentContext{}, nil); err == nil {
		t.Fatal("empty flavor silently selected a profile")
	}
	if _, err := selection.Plan(engine.DocumentContext{Flavor: "unknown"}, nil); err == nil {
		t.Fatal("accepted unknown profile")
	}
}
