package md064_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

type correctionCase struct {
	Name, Source, Candidate, Flavor, Config string
	Maximum                                 int
}

func correctionCases(t *testing.T) []correctionCase {
	t.Helper()
	data, err := os.ReadFile("testdata/md064_corrections.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []correctionCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

// md064Corrected validates producer's corrections of text with MD064 at maximum
// and the named extra rules enabled.
func md064Corrected(t *testing.T, text, flavor string, maximum int, producer fix.Producer, extra ...string) fix.FileView {
	t.Helper()
	r := registry(t, rules.Registrations())
	s := ruletest.Settings(extra...)
	on := true
	s["MD064"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"maximum": maximum}}
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

func caseRules(config string) []string {
	switch config {
	case "md047":
		return []string{"MD047"}
	case "md009":
		return []string{"MD009", "MD047"}
	case "md010":
		return []string{"MD010"}
	}
	return nil
}

func TestMD064CorrectionFixtures(t *testing.T) {
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
				// Every proposed MD064 edit deletes exactly a reported excess run.
				producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
					groups, err := rules.Corrections(ctx, c)
					if err != nil {
						return nil, err
					}
					reported := map[[2]int]bool{}
					for _, d := range c.Analysis().Diagnostics {
						if d.Rule == "MD064" {
							reported[[2]int{d.ByteRange.Start, d.ByteRange.End}] = true
						}
					}
					for _, g := range groups {
						if g.Rule != "MD064" {
							continue
						}
						if len(g.Edits) != 1 {
							t.Fatalf("group %+v", g)
						}
						e := g.Edits[0]
						text, _ := c.Current().Text(e.Span)
						if len(e.Replacement) != 0 || string(e.Expected) != text || strings.Trim(text, " ") != "" || g.Anchor != e.Span || !reported[[2]int{e.Span.Start(), e.Span.End()}] {
							t.Fatalf("group %+v", g)
						}
					}
					return groups, nil
				}
				view := md064Corrected(t, tc.Source, flavor, tc.Maximum, producer, caseRules(tc.Config)...)
				if got := string(view.Candidate.Bytes()); got != tc.Candidate {
					t.Fatalf("got %q want %q", got, tc.Candidate)
				}
				if tc.Source != tc.Candidate && view.ChangingRounds < 1 {
					t.Fatal("rounds", view.ChangingRounds)
				}
				for _, d := range view.Analysis.Diagnostics {
					if (d.Rule == "MD064" || d.Rule == "MD009" || d.Rule == "MD010" || d.Rule == "MD047") && d.FixAvailable {
						t.Fatal("remaining finding advertised", d)
					}
				}
				// Reparse the candidate: a second run changes nothing.
				again := md064Corrected(t, tc.Candidate, flavor, tc.Maximum, rules.Corrections, caseRules(tc.Config)...)
				if string(again.Candidate.Bytes()) != tc.Candidate || again.ChangingRounds != 0 {
					t.Fatalf("second run %q rounds %d", again.Candidate.Bytes(), again.ChangingRounds)
				}
			})
		}
	}
}

func TestMD064ComposesWithMD009AndMD047(t *testing.T) {
	view := md064Corrected(t, "One  two \n\n- a  b \nend", "commonmark", 1, rules.Corrections, "MD009", "MD047")
	if got := string(view.Candidate.Bytes()); got != "One two\n\n- a b\nend\n" {
		t.Fatalf("got %q", got)
	}
	if len(view.Analysis.Diagnostics) != 0 {
		t.Fatal(view.Analysis.Diagnostics)
	}
	// MD064 waits for the MD009 round, then shares its own with nothing else.
	for _, a := range view.Attempts {
		md064, other := false, false
		for _, d := range a.Decisions {
			if !d.Accepted {
				continue
			}
			md064 = md064 || d.Group.Rule == "MD064"
			other = other || d.Group.Rule == "MD009"
		}
		if md064 && other {
			t.Fatalf("MD064 shared a round with MD009: %+v", a.Decisions)
		}
	}
}

func TestMD064IneligibleFindingsStayManual(t *testing.T) {
	for _, source := range []string{"---\nOne  two\n---\nthree  four\n", "`a  b`\n"} {
		view := md064Corrected(t, source, "commonmark", 1, rules.Corrections)
		if string(view.Candidate.Bytes()) != source {
			t.Fatalf("changed %q", view.Candidate.Bytes())
		}
		for _, d := range view.Analysis.Diagnostics {
			if d.FixAvailable {
				t.Fatal("ineligible finding advertised", d)
			}
		}
	}
}

func TestMD064IndependentCheckerRejectsForgedChanges(t *testing.T) {
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
		{"delete every space", "One   two\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 3, 6, "") }},
		{"delete a word byte", "One  two\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 5, 6, "") }},
		{"replace spaces with a tab", "One   two\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 4, 6, "\t") }},
		{"keep an excess space", "One    two\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 4, 6, "") }},
		{"span inside a code span", "One  two `a  b`\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 12, 13, "") }},
		{"foreign anchor", "One  two\n", func(g *fix.Group, c fix.RoundContext) { g.Anchor, _ = c.Current().Span(0, 1) }},
		{"foreign anchor source", "One  two\n", func(g *fix.Group, _ fix.RoundContext) {
			g.Anchor, _ = parser.NewSource([]byte("One  two\n")).Span(4, 5)
		}},
		{"replacement group", "One  two\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte(" ") }},
		{"two edits", "One  two\n", func(g *fix.Group, c fix.RoundContext) {
			s, _ := c.Current().Span(0, 1)
			g.Edits = append(g.Edits, fix.Edit{Span: s, Expected: []byte("O")})
		}},
		{"unknown rule", "One  two\n", func(g *fix.Group, _ fix.RoundContext) { g.Rule = "MD009" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			altered := false
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := rules.Corrections(ctx, c)
				for i := range groups {
					if groups[i].Rule == "MD064" {
						tc.alter(&groups[i], c)
						altered = true
						break
					}
				}
				return groups, err
			}
			view := md064Corrected(t, tc.source, "commonmark", 1, producer)
			if !altered || string(view.Candidate.Bytes()) != tc.source || len(view.Attempts) == 0 {
				t.Fatalf("forged correction accepted: %q %+v", view.Candidate.Bytes(), view.Attempts)
			}
		})
	}
}

func TestMD064CheckerRejectsUnreportedDeletion(t *testing.T) {
	// A hand-made MD064 group deleting a code span's interior space is never proven.
	source := "`a  b`\n"
	producer := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
		span, _ := c.Current().Span(2, 3)
		return []fix.Group{{ID: "forged", Rule: "MD064", Provenance: "forged", Anchor: span, Edits: []fix.Edit{{Span: span, Expected: []byte(" ")}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}}}}, nil
	}
	view := md064Corrected(t, source, "commonmark", 1, producer)
	if string(view.Candidate.Bytes()) != source || len(view.Attempts) == 0 {
		t.Fatalf("forged correction accepted: %q", view.Candidate.Bytes())
	}
}
