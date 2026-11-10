// Package ruletest holds the helpers shared by rule tests: registries,
// settings, analysis and correction runs over production registrations.
//
// It is for tests only. Production code must not import it, and it must not
// import internal/rules, so that rule packages and their external test
// packages can both use it.
package ruletest

import (
	"context"
	"slices"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// Registry builds a catalog registry from registrations.
func Registry(t testing.TB, registrations []engine.Registration) *engine.Registry {
	t.Helper()
	r, err := engine.NewCatalogRegistry(registrations)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Settings enables only the named rules and disables every other catalog rule.
func Settings(ids ...string) engine.Settings {
	out := engine.Settings{}
	for _, m := range engine.Catalog() {
		v := slices.Contains(ids, m.ID)
		out[m.ID] = engine.RuleSetting{Enabled: &v}
	}
	return out
}

// Analyze lints text as CommonMark doc.md and fails if analysis changed the
// source or requested edits.
func Analyze(t *testing.T, r *engine.Registry, text string, s engine.Settings, opts engine.RunOptions) result.Result {
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

// CorrectionSetup returns a validator over checkers and a CommonMark plan for
// s, both sharing one registry built from registrations.
func CorrectionSetup(t *testing.T, registrations []engine.Registration, checkers []fix.CheckerRegistration, s engine.Settings) (*fix.Validator, *engine.ExecutionPlan) {
	t.Helper()
	r := Registry(t, registrations)
	selection, err := r.SelectRules(s, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := fix.NewValidator(r, checkers)
	if err != nil {
		t.Fatal(err)
	}
	return validator, plan
}

// Corrected validates producer's corrections of text as doc.md with the
// default limits.
func Corrected(t *testing.T, v *fix.Validator, plan *engine.ExecutionPlan, text string, producer fix.Producer) (fix.FileView, fix.Validated) {
	t.Helper()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	out, err := v.Validate(context.Background(), []fix.Target{{Path: "doc.md", Source: parser.NewSource([]byte(text)), Plan: plan, Produce: producer}}, fix.ValidationOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	view, ok := out.File("doc.md")
	if !ok {
		t.Fatal("missing file")
	}
	return view, out
}
