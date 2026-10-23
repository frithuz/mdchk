package rules

import (
	"context"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
)

// These are production MD047 registrations, producer and checker, with actual
// selected-flavor parses. The checker wrapper observes real candidate facts;
// the producer never consults the advertised eligibility flag.
func TestMD047GFMTaskEOFPreservation(t *testing.T) {
	for _, flavor := range []string{"gfm", "commonmark"} {
		for _, ending := range []string{"\n", "\r\n", "\r"} {
			for _, tail := range []string{"- [x]", "- [ ]", "> - [X]", "- outer\n  - [x]", "- [\n  ]", "> - [\n]", "- [x] ", "- [ ]\t", "> - [X] done", "ordinary"} {
				t.Run(flavor+"/"+strings.ReplaceAll(ending, "\r", "CR")+"/"+tail, func(t *testing.T) {
					text := "prefix" + ending + ending + strings.ReplaceAll(tail, "\n", ending)
					unsafe := flavor == "gfm" && strings.HasSuffix(tail, "]")
					r := registry(t, Registrations())
					selection, err := r.SelectRules(settings(), engine.ValidationProviders{})
					if err != nil {
						t.Fatal(err)
					}
					plan, err := selection.Plan(engine.DocumentContext{Flavor: flavor}, nil)
					if err != nil {
						t.Fatal(err)
					}
					checks := CorrectionCheckers()
					real := checks[0].Check
					checked := false
					checks[0].Check = func(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
						checked = true
						before, after := c.Before.Parsed(), c.After.Parsed()
						tasks := func(p parser.ParseResult) []parser.TaskProperties {
							var out []parser.TaskProperties
							parser.Walk(p.Document.Root(), func(n *parser.Node) bool {
								if n.Kind() == parser.Paragraph && n.Properties().Task != nil {
									out = append(out, *n.Properties().Task)
								}
								return true
							})
							return out
						}
						a, b := tasks(before), tasks(after)
						if unsafe {
							if len(a) != 0 || len(b) != 1 {
								t.Fatalf("actual task adoption before=%+v after=%+v", a, b)
							}
							marker, _ := after.Source.Text(b[0].Marker)
							separator, _ := after.Source.Text(b[0].Separator)
							start := strings.LastIndex(text, "[")
							if marker != text[start:] || b[0].Marker.Start() != start || b[0].Marker.End() != len(text) || separator != ending || b[0].Separator.Start() != len(text) {
								t.Fatalf("actual marker/separator facts: %+v %q %q", b[0], marker, separator)
							}
							visible := false
							parser.Walk(before.Document.Root(), func(n *parser.Node) bool {
								if n.Kind() == parser.Text {
									for _, s := range n.Content() {
										raw, _ := before.Source.Text(s.Source)
										if strings.Contains(raw, "[") {
											visible = true
										}
									}
								}
								return true
							})
							if !visible {
								t.Fatal("literal marker did not contribute visible source text")
							}
						}
						proof, err := real(ctx, c)
						if err == nil && (proof == fix.Preserved) == unsafe {
							t.Fatal("wrong independent proof", proof, unsafe)
						}
						return proof, err
					}
					validator, err := fix.NewValidator(r, checks)
					if err != nil {
						t.Fatal(err)
					}
					run := func(content string) (fix.FileView, fix.Validated) {
						t.Helper()
						budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
						out, err := validator.Validate(context.Background(), []fix.Target{{Path: "doc.md", Source: parser.NewSource([]byte(content)), Plan: plan, Produce: Corrections}}, fix.ValidationOptions{Budget: budget})
						if err != nil {
							t.Fatal(err)
						}
						view, ok := out.File("doc.md")
						if !ok {
							t.Fatal("missing file")
						}
						return view, out
					}
					input, _ := plan.NewInput("doc.md", []byte(text))
					lint, trace, err := plan.Inspect(context.Background(), input, engine.RunOptions{}, engine.InspectionOptions{})
					if err != nil || !lint.Complete || trace.Parsed().Status != parser.Complete || len(lint.Diagnostics) != 1 || lint.Diagnostics[0].FixAvailable == unsafe {
						t.Fatal("truthful actual eligibility", lint, err)
					}
					view, out := run(text)
					want := text + ending
					if unsafe {
						want = text
					}
					if !checked || !out.Complete() || string(view.Candidate.Bytes()) != want {
						t.Fatal("candidate", checked, view, out.Complete())
					}
					if unsafe && len(view.Attempts) == 0 {
						t.Fatal("missing explicit deferral")
					}
					// Safe candidates have no second proposal; unsafe candidates remain deferred.
					checked = false
					again, complete := run(want)
					if !complete.Complete() || string(again.Candidate.Bytes()) != want {
						t.Fatal("not idempotent", again)
					}
				})
			}
		}
	}
}

// Deliberately lie in the production finding after running the real rule. This
// proves the independent candidate checker cannot inherit eligibility authority.
func TestMD047GFMIndependentCheckerRejectsForcedEligibility(t *testing.T) {
	registrations := Registrations()
	realRule := registrations[0].Check
	registrations[0].Check = func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
		out := realRule(ctx, c)
		for i := range out.Findings {
			out.Findings[i].FixEligible = true
		}
		return out
	}
	r := registry(t, registrations)
	selection, err := r.SelectRules(settings(), engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: "gfm"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	checks := CorrectionCheckers()
	realCheck := checks[0].Check
	entered := false
	checks[0].Check = func(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
		entered = true
		proof, err := realCheck(ctx, c)
		if err == nil && proof != fix.Unsafe {
			t.Fatal("independent checker trusted forced flag", proof)
		}
		return proof, err
	}
	validator, err := fix.NewValidator(r, checks)
	if err != nil {
		t.Fatal(err)
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	out, err := validator.Validate(context.Background(), []fix.Target{{Path: "doc.md", Source: parser.NewSource([]byte("- [x]")), Plan: plan, Produce: Corrections}}, fix.ValidationOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	view, _ := out.File("doc.md")
	if !entered || !out.Complete() || string(view.Candidate.Bytes()) != "- [x]" || len(view.Attempts) == 0 || len(view.Analysis.Diagnostics) != 1 || !view.Analysis.Diagnostics[0].FixAvailable {
		t.Fatal("forced flag authorized candidate", entered, view)
	}
}
