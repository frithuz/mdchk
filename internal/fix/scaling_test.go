package fix_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
)

// Each independent fixture correction changes a single inter-word ASCII space
// to a tab in plain prose. The constant trusted checker isolates coordinator
// cost; it neither requests all round edits nor rescans the complete source.
func independentFixture(tb testing.TB, n int, checker fix.Checker) (*fix.Validator, []fix.Target) {
	tb.Helper()
	registry, err := engine.NewRegistry([]engine.Metadata{{ID: "MD001", Name: "independent whitespace fixture", DefaultEnabled: true, Fix: engine.PlannedFix}}, []engine.Registration{{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { return engine.RuleOutcome{} }}})
	if err != nil {
		tb.Fatal(err)
	}
	selected, err := registry.SelectRules(nil, engine.ValidationProviders{})
	if err != nil {
		tb.Fatal(err)
	}
	plan, err := selected.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
	if err != nil {
		tb.Fatal(err)
	}
	if checker == nil {
		checker = func(context.Context, fix.CheckContext) (fix.Proof, error) { return fix.Preserved, nil }
	}
	validator, err := fix.NewValidator(registry, []fix.CheckerRegistration{{Rule: "MD001", Check: checker}})
	if err != nil {
		tb.Fatal(err)
	}
	producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
		raw := c.Current().Bytes()
		if err := c.Checkpoint(ctx, int64(len(raw))+int64(n)); err != nil {
			return nil, err
		}
		groups := make([]fix.Group, 0, n)
		for i := 1; i < len(raw); i += 2 {
			if raw[i] != ' ' {
				continue
			}
			span, err := c.Current().Span(i, i+1)
			if err != nil {
				return nil, err
			}
			groups = append(groups, fix.Group{ID: fmt.Sprintf("group-%04d", i/2), Rule: "MD001", Provenance: "independent fixture", Anchor: span, Edits: []fix.Edit{{Span: span, Expected: []byte(" "), Replacement: []byte("\t")}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}}})
		}
		return groups, nil
	}
	return validator, []fix.Target{{Path: "independent.md", Source: parser.NewSource([]byte(strings.Repeat("x ", n))), Plan: plan, Produce: producer}}
}
func TestCandidateManyIndependentGroups(t *testing.T) {
	for _, n := range []int{1024, 2048} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			validator, targets := independentFixture(t, n, nil)
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			out, err := validator.Validate(context.Background(), targets, fix.ValidationOptions{Budget: budget})
			if err != nil {
				t.Fatal(err)
			}
			file, ok := out.File("independent.md")
			if !ok || !out.Complete() || !bytes.Equal(file.Candidate.Bytes(), []byte(strings.Repeat("x\t", n))) {
				t.Fatalf("independent corrections failed: complete=%v reasons=%v", out.Complete(), file.Reasons)
			}
			if file.ChangingRounds != 1 || len(file.Attempts) != 2 {
				t.Fatal("independent corrections missed final no-change check")
			}
		})
	}
}
func TestCandidateSharedRoundEditsRemainPrivate(t *testing.T) {
	checks := 0
	checker := func(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
		// Accessor construction is explicit checker work, unlike eagerly copying an
		// unused round set for every callback. This fixture requests only four edits.
		if err := c.Before.Checkpoint(ctx, 64); err != nil {
			return fix.Unknown, err
		}
		edits := c.Edits()
		if len(edits) != 4 {
			t.Fatalf("edit count %d", len(edits))
		}
		if string(edits[0].Edit.Expected) != " " || string(edits[0].Edit.Replacement) != "\t" || edits[0].Groups[0] != "group-0000" {
			t.Fatal("another checker mutated shared round authority")
		}
		edits[0].Edit.Expected[0] = 'X'
		edits[0].Edit.Replacement[0] = 'Y'
		edits[0].Groups[0] = "mutated"
		edits[0].Groups = append(edits[0].Groups, "extra")
		fresh := c.Edits()
		if string(fresh[0].Edit.Expected) != " " || string(fresh[0].Edit.Replacement) != "\t" || len(fresh[0].Groups) != 1 || fresh[0].Groups[0] != "group-0000" {
			t.Fatal("public Edits accessor aliases private edits")
		}
		checks++
		return fix.Preserved, nil
	}
	validator, targets := independentFixture(t, 4, checker)
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	out, err := validator.Validate(context.Background(), targets, fix.ValidationOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	file, _ := out.File("independent.md")
	if checks != 4 || !out.Complete() || string(file.Candidate.Bytes()) != "x\tx\tx\tx\t" || string(targets[0].Source.Bytes()) != "x x x x " {
		t.Fatal("checker mutation affected another context, candidate or original")
	}
}
func BenchmarkCandidateIndependentGroups(b *testing.B) {
	for _, n := range []int{64, 128, 256, 512, 1024, 2048} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			validator, targets := independentFixture(b, n, nil)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				out, err := validator.Validate(context.Background(), targets, fix.ValidationOptions{Budget: budget})
				if err != nil || !out.Complete() {
					b.Fatalf("independent correction failed: %v", err)
				}
			}
		})
	}
}
