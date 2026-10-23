package fix_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
)

const gfmFixSource = "> - [X]\té ~~ok~~ www.a.b\r\n\r\n| a | b |\n| :- | -: |\n| short |\n| long | `code` | excess |\n\n<textarea>\nopaque\n</textarea>\n\nlast"

func assertGFMFixParse(t *testing.T, r parser.ParseResult) {
	t.Helper()
	if r.Status != parser.Complete || r.AST != parser.Available || r.Phases.Blocks != parser.PhaseComplete || r.Phases.Inlines != parser.PhaseComplete || r.Document == nil || r.Document.References().Availability != parser.Available || len(r.Uncertainty) != 0 {
		t.Fatal("not authoritative integrated parse", r)
	}
	counts := map[parser.Kind]int{}
	tasks, bare, tags := 0, 0, 0
	parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
		counts[n.Kind()]++
		p := n.Properties()
		if p.Task != nil {
			tasks++
		}
		if p.Link != nil && p.Link.Origin == "bare-gfm" {
			bare++
		}
		if p.HTML != nil {
			tags += len(p.HTML.FilteredTags)
		}
		return true
	})
	if counts[parser.Table] != 1 || counts[parser.Strike] != 1 || tasks != 2 || bare != 1 || tags != 2 {
		t.Fatal("candidate lost GFM", counts, tasks, bare, tags)
	}
}
func gfmSemanticSetup(t *testing.T) semanticFixture {
	t.Helper()
	f := semanticSetup(t, nil, func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		if c.Context().Flavor != "gfm" || c.Document() == nil {
			t.Fatal("wrong actual callback parser")
		}
		return engine.RuleOutcome{}
	})
	sel, err := f.registry.SelectRules(nil, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	f.plan, err = sel.Plan(engine.DocumentContext{Flavor: "gfm"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestGFMCandidatePreservationAndReparse(t *testing.T) {
	checks, produces := 0, 0
	producer := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
		produces++
		assertGFMFixParse(t, c.Parsed())
		if !bytes.Equal(c.Parsed().Source.Bytes(), c.Current().Bytes()) || c.Parsed().Source != c.Current() {
			t.Fatal("round source identity")
		}
		if bytes.HasPrefix(c.Current().Bytes(), []byte("\n")) {
			return nil, nil
		}
		return []fix.Group{syntheticGroup(t, c, 0, 0, "\n")}, nil
	}
	checker := func(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
		checks++
		assertGFMFixParse(t, c.Before.Parsed())
		assertGFMFixParse(t, c.After.Parsed())
		if c.Before.Current() == c.After.Current() {
			t.Fatal("candidate wasn't freshly parsed")
		}
		return blankPrefixProof(ctx, c)
	}
	out, err := validateSynthetic(t, gfmSemanticSetup(t), gfmFixSource, producer, checker)
	if err != nil {
		t.Fatal(err)
	}
	file, _ := out.File("a.md")
	if !out.Complete() || string(file.Candidate.Bytes()) != "\n"+gfmFixSource || string(file.Original.Bytes()) != gfmFixSource || file.ChangingRounds != 1 || checks == 0 || produces < 2 {
		t.Fatal("not a validated changing/converged candidate", file, checks, produces)
	}
	assertGFMFixParse(t, file.Parsed())
	again, err := validateSynthetic(t, gfmSemanticSetup(t), string(file.Candidate.Bytes()), producer, checker)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := again.File("a.md")
	if !again.Complete() || second.ChangingRounds != 0 || second.Candidate != second.Original {
		t.Fatal("not idempotent", second)
	}
}

func TestGFMUnsafeCandidateGroups(t *testing.T) {
	for _, tc := range []struct {
		token, replacement string
		permissive         bool
	}{
		{"X", "x", false}, {"excess", "lost", false}, {"www.a.b", "www.c.d", false}, {"<textarea>", "&lt;textarea>", true}, {"code", "oops", true}, {"opaque", "changed", true},
	} {
		t.Run(tc.token, func(t *testing.T) {
			producer := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
				s := string(c.Current().Bytes())
				at := strings.Index(s, tc.token)
				if at < 0 {
					return nil, nil
				}
				return []fix.Group{syntheticGroup(t, c, at, at+len(tc.token), tc.replacement)}, nil
			}
			checker := blankPrefixProof
			if tc.permissive {
				checker = func(context.Context, fix.CheckContext) (fix.Proof, error) { return fix.Preserved, nil }
			}
			out, err := validateSynthetic(t, gfmSemanticSetup(t), gfmFixSource, producer, checker)
			if err != nil {
				t.Fatal(err)
			}
			file, _ := out.File("a.md")
			if !out.Complete() || string(file.Candidate.Bytes()) != gfmFixSource || len(file.Attempts) == 0 {
				t.Fatal("unsafe candidate accepted", file)
			}
		})
	}
}

func TestGFMPlannerAuthorityAndBudget(t *testing.T) {
	ctx := context.Background()
	r, err := engine.NewRegistry([]engine.Metadata{{ID: "MD001", Name: "fixture", DefaultEnabled: true, Fix: engine.PlannedFix}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	source := parser.NewSource([]byte(gfmFixSource))
	parsed := parser.ParseGFMWithBudget(ctx, source, b)
	assertGFMFixParse(t, parsed)
	state := engine.ProcessDirectives(ctx, parsed, policy{resolved, true}, b)
	if !state.Complete() {
		t.Fatal(state.Err())
	}
	for _, mode := range []string{"foreign-operation", "foreign-source", "partial", "bad-source"} {
		t.Run(mode, func(t *testing.T) {
			op := b
			actual := parsed
			sp := span(t, source, source.Len(), source.Len())
			switch mode {
			case "foreign-operation":
				op, _ = parser.NewOperationBudget(parser.DefaultLimits())
			case "foreign-source":
				foreign := parser.NewSource(source.Bytes())
				sp = span(t, foreign, foreign.Len(), foreign.Len())
			case "partial":
				l := parser.DefaultLimits()
				l.SyntaxDepth = 2
				op, _ = parser.NewOperationBudget(l)
				actual = parser.ParseGFMWithBudget(ctx, source, op)
				if actual.Status != parser.Incomplete || actual.Limit == nil {
					t.Fatal(actual)
				}
			}
			g := fix.Group{ID: "fixture", Rule: "MD001", Anchor: sp, Edits: []fix.Edit{{Span: sp, Expected: []byte{}, Replacement: []byte("\n")}}}
			if mode == "bad-source" {
				g.Edits[0].Expected = []byte("not the original bytes")
			}
			planned, err := fix.Plan(ctx, actual, state, r, []fix.Group{g}, op)
			if err == nil && (!planned.Complete() || len(planned.Decisions()) != 1) {
				t.Fatal("refusal lost decision", mode)
			}
			if err == nil && planned.Complete() {
				for _, decision := range planned.Decisions() {
					if decision.Accepted {
						t.Fatal("foreign/partial authority accepted", mode)
					}
				}
			}
		})
	}
	// Exhaust the SAME operation after real GFM analysis, before candidate edits.
	f := gfmSemanticSetup(t)
	out, err := validateSynthetic(t, f, gfmFixSource, func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
		assertGFMFixParse(t, c.Parsed())
		return nil, c.Checkpoint(ctx, f.budget.Limits().WorkUnits)
	}, blankPrefixProof)
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || limit.Name != "work-units" || out.Complete() {
		t.Fatal("missing cumulative typed failure", err)
	}
	file, ok := out.File("a.md")
	if !ok || string(file.Candidate.Bytes()) != gfmFixSource {
		t.Fatal("exhausted operation published candidate", file)
	}
}
