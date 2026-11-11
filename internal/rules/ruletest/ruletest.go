// Package ruletest holds the helpers shared by rule tests: registries,
// settings, analysis and correction runs over production registrations.
//
// It is for tests only. Production code must not import it, and it must not
// import internal/rules, so that rule packages and their external test
// packages can both use it.
package ruletest

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
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

// AnalyzeDocument lints source as heading.md in dc. It fails if analysis
// changed the source or proposed edits, or if a suppressed finding or a
// report-only rule's finding grants a fix.
func AnalyzeDocument(t *testing.T, r *engine.Registry, source string, s engine.Settings, dc engine.DocumentContext, opts engine.RunOptions) result.Result {
	t.Helper()
	caller := []byte(source)
	in, err := engine.NewInput("heading.md", caller, s, dc)
	if err != nil {
		t.Fatal(err)
	}
	token := in.Source().Token()
	out, err := r.Lint(context.Background(), []engine.Input{in}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if string(caller) != source || string(in.Source().Bytes()) != source || in.Source().Token() != token || len(out.Changes) != 0 || len(out.Documents) != 0 || out.Files[0].WriteStatus != "not-requested" {
		t.Fatal("analysis changed original or proposed edits", out)
	}
	for _, raw := range out.SuppressedDiagnostics {
		var suppressed result.SuppressedDiagnostic
		if err := json.Unmarshal(raw, &suppressed); err != nil || suppressed.Diagnostic.FixAvailable {
			t.Fatal("suppressed heading grants fix", string(raw), err)
		}
	}
	for _, d := range out.Diagnostics {
		if d.FixAvailable && d.Rule != "MD003" && d.Rule != "MD004" && d.Rule != "MD005" && d.Rule != "MD007" && d.Rule != "MD009" && d.Rule != "MD019" && d.Rule != "MD021" && d.Rule != "MD022" && d.Rule != "MD023" && d.Rule != "MD029" && d.Rule != "MD030" && d.Rule != "MD032" && d.Rule != "MD063" && d.Rule != "MD076" && d.Rule != "MD077" {
			t.Fatal("report-only heading grants fix", d)
		}
	}
	return out
}

// LateStopContext counts the production callback's context checks so a test
// can stop after a completed inventory and the first emission, without fixing
// a parser work count. From the Stop-th call on, Err returns Reason.
type LateStopContext struct {
	context.Context
	Calls, Stop int
	Reason      error
}

func (c *LateStopContext) Err() error {
	c.Calls++
	if c.Stop > 0 && c.Calls >= c.Stop {
		return c.Reason
	}
	return c.Context.Err()
}

// CorrectedFlavor validates producer's corrections of text as doc.md in
// flavor with the default limits, with only the named rules enabled.
func CorrectedFlavor(t *testing.T, registrations []engine.Registration, checkers []fix.CheckerRegistration, text, flavor string, producer fix.Producer, ids ...string) (fix.FileView, fix.Validated) {
	t.Helper()
	r := Registry(t, registrations)
	selection, err := r.SelectRules(Settings(ids...), engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: flavor}, nil)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := fix.NewValidator(r, checkers)
	if err != nil {
		t.Fatal(err)
	}
	return Corrected(t, validator, plan, text, producer)
}

// HeadingFacts are the parser and anchor facts of one heading that a
// correction must preserve.
type HeadingFacts struct {
	Level                                   int
	Style, Visible, Label, Containers, Base string
	Closed                                  bool
}

// HeadingFactsOf parses text in flavor and returns the facts of each heading.
func HeadingFactsOf(t *testing.T, text, flavor string) []HeadingFacts {
	t.Helper()
	ctx := context.Background()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	source := parser.NewSource([]byte(text))
	options := engine.DocumentContext{Flavor: flavor}.ParseOptions()
	parsed := parser.ParseWithOptions(ctx, source, budget, options)
	headings, err := parsed.Document.Headings(ctx, budget)
	if err != nil {
		t.Fatal(err)
	}
	anchors, availability, err := resource.OriginalHeadingAnchors(ctx, parsed, budget, parser.NewLineIndex(source), options, "github")
	if err != nil || availability != resource.Available || len(anchors) != len(headings) {
		t.Fatal(err, availability, len(anchors))
	}
	out := make([]HeadingFacts, len(headings))
	for i, h := range headings {
		label, _ := source.Text(h.Label)
		kinds := make([]string, len(h.Containers))
		for j, c := range h.Containers {
			kinds[j] = string(c.Kind)
		}
		out[i] = HeadingFacts{h.Level, h.Style, h.Visible, label, strings.Join(kinds, ","), anchors[i].Base.Value, h.Closed}
	}
	return out
}
