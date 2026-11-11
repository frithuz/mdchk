package md044_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

type correctionCase struct {
	Name, Source, Candidate, Flavor, Config string
	Names                                   []string
	CodeBlocks                              bool
}

func correctionCases(t *testing.T) []correctionCase {
	t.Helper()
	data, err := os.ReadFile("testdata/md044_corrections.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []correctionCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for i := range cases {
		if cases[i].Names == nil {
			cases[i].Names = []string{"GitHub"}
		}
	}
	return cases
}

// caseRules returns the rules a fixture's Config enables besides MD044.
func caseRules(config string) []string {
	switch config {
	case "md047":
		return []string{"MD047"}
	case "composition":
		return []string{"MD009", "MD047", "MD064", "MD085"}
	}
	return nil
}

// md044Corrected validates producer's corrections of text with MD044 set to
// names and code_blocks and the named extra rules enabled.
func md044Corrected(t *testing.T, text, flavor string, names []string, code bool, producer fix.Producer, extra ...string) fix.FileView {
	t.Helper()
	r := ruletest.Registry(t, rules.Registrations())
	s := ruletest.Settings(extra...)
	on := true
	s["MD044"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"names": names, "code_blocks": code}}
	selection, err := r.SelectRules(s, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: flavor}, nil)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := fix.NewValidator(r, rules.CorrectionCheckers())
	if err != nil {
		t.Fatal(err)
	}
	view, out := ruletest.Corrected(t, validator, plan, text, producer)
	if !out.Complete() {
		t.Fatalf("incomplete: %+v", view.Reasons)
	}
	return view
}

func TestMD044CorrectionFixtures(t *testing.T) {
	for _, tc := range correctionCases(t) {
		// Directive regions need directive processing, which the acceptance test enables.
		if tc.Config == "directives" {
			continue
		}
		for _, flavor := range []string{"commonmark", "gfm"} {
			if tc.Flavor != "" && tc.Flavor != flavor {
				continue
			}
			t.Run(flavor+"/"+tc.Name, func(t *testing.T) {
				// Every proposed MD044 edit replaces exactly a reported occurrence with
				// a configured spelling of the same fold.
				producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
					groups, err := rules.Corrections(ctx, c)
					if err != nil {
						return nil, err
					}
					reported := map[[2]int]bool{}
					for _, d := range c.Analysis().Diagnostics {
						if d.Rule == "MD044" {
							reported[[2]int{d.ByteRange.Start, d.ByteRange.End}] = true
						}
					}
					for _, g := range groups {
						if g.Rule != "MD044" {
							continue
						}
						if len(g.Edits) != 1 {
							t.Fatalf("group %+v", g)
						}
						e := g.Edits[0]
						text, _ := c.Current().Text(e.Span)
						budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
						before, _ := parser.FoldCase(ctx, budget, text)
						after, _ := parser.FoldCase(ctx, budget, string(e.Replacement))
						known := false
						for _, n := range tc.Names {
							known = known || n == string(e.Replacement)
						}
						if string(e.Expected) != text || before != after || !known || g.Anchor != e.Span || !reported[[2]int{e.Span.Start(), e.Span.End()}] {
							t.Fatalf("group %+v", g)
						}
					}
					return groups, nil
				}
				view := md044Corrected(t, tc.Source, flavor, tc.Names, tc.CodeBlocks, producer, caseRules(tc.Config)...)
				if got := string(view.Candidate.Bytes()); got != tc.Candidate {
					t.Fatalf("got %q want %q", got, tc.Candidate)
				}
				if tc.Source != tc.Candidate && view.ChangingRounds < 1 {
					t.Fatal("rounds", view.ChangingRounds)
				}
				for _, d := range view.Analysis.Diagnostics {
					if d.FixAvailable {
						t.Fatal("remaining finding advertised", d)
					}
				}
				// Reparse the candidate: a second run changes nothing.
				again := md044Corrected(t, tc.Candidate, flavor, tc.Names, tc.CodeBlocks, rules.Corrections, caseRules(tc.Config)...)
				if string(again.Candidate.Bytes()) != tc.Candidate || again.ChangingRounds != 0 {
					t.Fatalf("second run %q rounds %d", again.Candidate.Bytes(), again.ChangingRounds)
				}
			})
		}
	}
}

func TestMD044AmbiguousSpellingStaysManual(t *testing.T) {
	source := "Use GITHUB.\n"
	view := md044Corrected(t, source, "commonmark", []string{"GitHub", "Github"}, false, rules.Corrections)
	if string(view.Candidate.Bytes()) != source || view.ChangingRounds != 0 {
		t.Fatalf("changed %q", view.Candidate.Bytes())
	}
	if len(view.Analysis.Diagnostics) != 1 || view.Analysis.Diagnostics[0].FixAvailable || view.Analysis.Diagnostics[0].ByteRange.Start != 4 || view.Analysis.Diagnostics[0].ByteRange.End != 10 {
		t.Fatal(view.Analysis.Diagnostics)
	}
}

func TestMD044PreciseReplacedSpan(t *testing.T) {
	var spans [][2]int
	producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
		groups, err := rules.Corrections(ctx, c)
		for _, g := range groups {
			if g.Rule == "MD044" {
				spans = append(spans, [2]int{g.Edits[0].Span.Start(), g.Edits[0].Span.End()})
			}
		}
		return groups, err
	}
	view := md044Corrected(t, "Use github.", "commonmark", []string{"GitHub"}, false, producer)
	if string(view.Candidate.Bytes()) != "Use GitHub." || len(spans) != 1 || spans[0] != [2]int{4, 10} {
		t.Fatalf("%q %v", view.Candidate.Bytes(), spans)
	}
}

func TestMD044ComposesWithOtherCorrections(t *testing.T) {
	source := "Use  github \nand\n   more github"
	view := md044Corrected(t, source, "commonmark", []string{"GitHub"}, false, rules.Corrections, "MD009", "MD047", "MD064", "MD085")
	if got := string(view.Candidate.Bytes()); got != "Use GitHub\nand\nmore GitHub\n" {
		t.Fatalf("got %q", got)
	}
	if len(view.Analysis.Diagnostics) != 0 {
		t.Fatal(view.Analysis.Diagnostics)
	}
	// MD044 shares its round with no edit but MD047.
	for _, a := range view.Attempts {
		named, other := false, false
		for _, d := range a.Decisions {
			if !d.Accepted {
				continue
			}
			named = named || d.Group.Rule == "MD044"
			other = other || d.Group.Rule != "MD044" && d.Group.Rule != "MD047"
		}
		if named && other {
			t.Fatalf("MD044 shared a round: %+v", a.Decisions)
		}
	}
}

func TestMD044IndependentCheckerRejectsForgedChanges(t *testing.T) {
	retarget := func(g *fix.Group, c fix.RoundContext, start, end int, replacement string) {
		s, _ := c.Current().Span(start, end)
		text, _ := c.Current().Text(s)
		g.Anchor = s
		g.Edits[0] = fix.Edit{Span: s, Expected: []byte(text), Replacement: []byte(replacement)}
	}
	for _, tc := range []struct {
		name, source string
		alter        func(*fix.Group, fix.RoundContext)
	}{
		{"other spelling", "Use github.\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte("GITHUB") }},
		{"other letters", "Use github.\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte("GitLab") }},
		{"wider span", "Use github.\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 3, 10, " GitHub") }},
		{"narrower span", "Use github.\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 4, 5, "G") }},
		{"inside a code span", "Use github `github`\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 12, 18, "GitHub") }},
		{"inside a destination", "[github](https://github.com)\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 17, 23, "GitHub") }},
		{"foreign anchor", "Use github.\n", func(g *fix.Group, c fix.RoundContext) { g.Anchor, _ = c.Current().Span(0, 1) }},
		{"wrong expected", "Use github.\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Expected = []byte("GitHub") }},
		{"two edits", "Use github.\n", func(g *fix.Group, c fix.RoundContext) {
			s, _ := c.Current().Span(0, 1)
			g.Edits = append(g.Edits, fix.Edit{Span: s, Expected: []byte("U"), Replacement: []byte("u")})
		}},
		{"unknown rule", "Use github.\n", func(g *fix.Group, _ fix.RoundContext) { g.Rule = "MD064" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			altered := false
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := rules.Corrections(ctx, c)
				for i := range groups {
					if groups[i].Rule == "MD044" {
						tc.alter(&groups[i], c)
						altered = true
						break
					}
				}
				return groups, err
			}
			view := md044Corrected(t, tc.source, "commonmark", []string{"GitHub"}, false, producer)
			if !altered || string(view.Candidate.Bytes()) != tc.source || len(view.Attempts) == 0 {
				t.Fatalf("forged correction accepted: %q %+v", view.Candidate.Bytes(), view.Attempts)
			}
		})
	}
}
