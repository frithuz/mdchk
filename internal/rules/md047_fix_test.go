package rules

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"strings"
	"testing"
)

func correctionSetup(t *testing.T) (*fix.Validator, *engine.ExecutionPlan) {
	t.Helper()
	r := registry(t, Registrations())
	selection, err := r.SelectRules(settings(), engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := fix.NewValidator(r, CorrectionCheckers())
	if err != nil {
		t.Fatal(err)
	}
	return validator, plan
}
func corrected(t *testing.T, text string, producer fix.Producer) (fix.FileView, fix.Validated) {
	t.Helper()
	v, p := correctionSetup(t)
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	out, err := v.Validate(context.Background(), []fix.Target{{Path: "doc.md", Source: parser.NewSource([]byte(text)), Plan: p, Produce: producer}}, fix.ValidationOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	view, ok := out.File("doc.md")
	if !ok {
		t.Fatal("missing file")
	}
	return view, out
}
func TestMD047ProductionCorrectionProofAndIdempotence(t *testing.T) {
	for _, tc := range []struct {
		source, want string
		eligible     bool
	}{
		{"hello", "hello\n", true}, {"hello\r\n\n\r", "hello\r\n", true}, {"\r\n\n", "\r\n", true},
		{"a\rb", "a\rb\r", true}, {"x\r\ny\nz", "x\r\ny\nz\n", true}, {"x\n ", "x\n \n", true},
		{"x  ", "x  \n", true}, {"雪é", "雪é\n", true}, {"> - nested\n>   text", "> - nested\n>   text\n", true},
		{"```\ncode", "```\ncode", false}, {"    code", "    code", false}, {"<div>raw", "<div>raw", false},
		{"<!-- unclosed", "<!-- unclosed", false}, {"<!-- comment -->", "<!-- comment -->", false},
		{"```\ncode\n```\n\n", "```\ncode\n```\n", true},
		{"<!-- mdchk:disable MD047 -->tail", "<!-- mdchk:disable MD047 -->tail", false},
		{"<!-- mdchk:disable MD047 --><!-- mdchk:enable MD047 -->\ntext", "<!-- mdchk:disable MD047 --><!-- mdchk:enable MD047 -->\ntext\n", true},
	} {
		t.Run(tc.source, func(t *testing.T) {
			original := analyze(t, registry(t, Registrations()), tc.source, settings(), engine.RunOptions{})
			for _, d := range original.Diagnostics {
				if d.Rule == "MD047" && d.FixAvailable != tc.eligible {
					t.Fatalf("eligibility %v want %v: %+v", d.FixAvailable, tc.eligible, d)
				}
			}
			view, out := corrected(t, tc.source, Corrections)
			if !out.Complete() || string(view.Candidate.Bytes()) != tc.want {
				t.Fatalf("got %q want %q complete %v reasons %+v attempts %+v", view.Candidate.Bytes(), tc.want, out.Complete(), view.Reasons, view.Attempts)
			}
			if !tc.eligible {
				for _, d := range view.Analysis.Diagnostics {
					if d.Rule == "MD047" && d.FixAvailable {
						t.Fatal("unsafe retained fix advertised")
					}
				}
			}
			second, again := corrected(t, string(view.Candidate.Bytes()), Corrections)
			if !again.Complete() || string(second.Candidate.Bytes()) != tc.want {
				t.Fatal("not idempotent", second)
			}
		})
	}
}
func TestMD047IndependentCheckerRejectsForgedChanges(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		alter        func(*fix.Group, fix.RoundContext)
	}{
		{"body rewrite", "hello", func(g *fix.Group, c fix.RoundContext) {
			s, _ := c.Current().Span(0, 5)
			g.Edits[0] = fix.Edit{Span: s, Expected: []byte("hello"), Replacement: []byte("bye\n")}
		}},
		{"wrong terminator", "a\rb", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte("\n") }},
		{"remove first ending", "x\r\n\n\r", func(g *fix.Group, c fix.RoundContext) {
			s, _ := c.Current().Span(1, c.Current().Len())
			g.Anchor = s
			g.Edits[0] = fix.Edit{Span: s, Expected: []byte("\r\n\n\r"), Replacement: []byte("\n")}
		}},
		{"trim hardbreak spaces", "text  ", func(g *fix.Group, c fix.RoundContext) {
			s, _ := c.Current().Span(4, 6)
			g.Edits[0] = fix.Edit{Span: s, Expected: []byte("  "), Replacement: []byte("\n")}
		}},
		{"foreign anchor", "hello", func(g *fix.Group, _ fix.RoundContext) {
			s := parser.NewSource([]byte("hello"))
			g.Anchor, _ = s.Span(5, 5)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := Corrections(ctx, c)
				if len(groups) > 0 {
					tc.alter(&groups[0], c)
				}
				return groups, err
			}
			view, out := corrected(t, tc.source, producer)
			if !out.Complete() || string(view.Candidate.Bytes()) != tc.source || len(view.Attempts) == 0 {
				t.Fatal("forged correction accepted", view)
			}
		})
	}
}
func TestMD047CorrectionResidualAndLateCancellation(t *testing.T) {
	v, p := correctionSetup(t)
	for _, stage := range []string{"producer", "checker"} {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		producer := Corrections
		validator := v
		if stage == "producer" {
			producer = func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := Corrections(ctx, c)
				cancel()
				return groups, err
			}
		} else {
			checks := CorrectionCheckers()
			real := checks[0].Check
			checks[0].Check = func(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
				proof, err := real(ctx, c)
				cancel()
				return proof, err
			}
			// The plan and registry must share identity.
			r := registry(t, Registrations())
			selection, err := r.SelectRules(settings(), engine.ValidationProviders{})
			if err != nil {
				t.Fatal(err)
			}
			p, err = selection.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			validator, err = fix.NewValidator(r, checks)
			if err != nil {
				t.Fatal(err)
			}
		}
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		out, err := validator.Validate(ctx, []fix.Target{{Path: "doc.md", Source: parser.NewSource([]byte("hello")), Plan: p, Produce: producer}}, fix.ValidationOptions{Budget: budget})
		if !errors.Is(err, context.Canceled) || out.Complete() {
			t.Fatal(stage, err, out.Complete())
		}
		view, _ := out.File("doc.md")
		if string(view.Candidate.Bytes()) != "hello" {
			t.Fatal("unverified bytes escaped")
		}
	}
	for _, text := range []string{strings.Repeat("p\n\n", 8000) + "last", strings.Repeat("x", 100000), "text\n" + strings.Repeat("\r\n", 8000)} {
		view, out := corrected(t, text, Corrections)
		if !out.Complete() || len(view.Analysis.Diagnostics) != 0 {
			t.Fatal("default scaling", len(text), view.Reasons)
		}
	}
}

func TestMD047SourceOnlyFindingCannotPromiseCorrection(t *testing.T) {
	r := registry(t, Registrations())
	input, err := engine.NewInput("doc.md", []byte("hello"), settings(), engine.DocumentContext{Flavor: "unavailable"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Lint(context.Background(), []engine.Input{input}, engine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].FixAvailable || out.Diagnostics[0].ByteRange.Start != 5 {
		t.Fatal(out)
	}
}

func TestMD047ProductionProofSharedWorkLimit(t *testing.T) {
	for _, stage := range []string{"producer", "checker"} {
		t.Run(stage, func(t *testing.T) {
			r := registry(t, Registrations())
			selection, err := r.SelectRules(settings(), engine.ValidationProviders{})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := selection.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			checks := CorrectionCheckers()
			producer := Corrections
			entered := false
			if stage == "producer" {
				producer = func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
					groups, err := Corrections(ctx, c)
					if err != nil {
						return nil, err
					}
					entered = true
					return groups, c.Checkpoint(ctx, 1<<60)
				}
			} else {
				real := checks[0].Check
				checks[0].Check = func(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
					proof, err := real(ctx, c)
					if err != nil {
						return proof, err
					}
					entered = true
					return proof, c.Before.Checkpoint(ctx, 1<<60)
				}
			}
			validator, err := fix.NewValidator(r, checks)
			if err != nil {
				t.Fatal(err)
			}
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			out, err := validator.Validate(context.Background(), []fix.Target{{Path: "doc.md", Source: parser.NewSource([]byte("hello")), Plan: plan, Produce: producer}}, fix.ValidationOptions{Budget: budget})
			var limit *parser.LimitExceeded
			if !entered || !errors.As(err, &limit) || limit.Name != "work-units" || out.Complete() {
				t.Fatal(stage, entered, err)
			}
			view, _ := out.File("doc.md")
			if string(view.Candidate.Bytes()) != "hello" {
				t.Fatal("unproved quota-limited candidate escaped")
			}
		})
	}
}
