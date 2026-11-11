package md038_test

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
}

func correctionCases(t *testing.T) []correctionCase {
	t.Helper()
	data, err := os.ReadFile("testdata/md038_corrections.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []correctionCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

// md038Corrected validates producer's corrections of text with MD038 and the
// named extra rules enabled.
func md038Corrected(t *testing.T, text, flavor string, producer fix.Producer, extra ...string) fix.FileView {
	t.Helper()
	r := ruletest.Registry(t, rules.Registrations())
	selection, err := r.SelectRules(ruletest.Settings(append(extra, "MD038")...), engine.ValidationProviders{})
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
	}
	return nil
}

func TestMD038CorrectionFixtures(t *testing.T) {
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
				// Every proposed MD038 group deletes exactly both reported edge runs
				// of one code span, each a single SPACE.
				producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
					groups, err := rules.Corrections(ctx, c)
					if err != nil {
						return nil, err
					}
					reported := map[[2]int]bool{}
					for _, d := range c.Analysis().Diagnostics {
						if d.Rule == "MD038" {
							reported[[2]int{d.ByteRange.Start, d.ByteRange.End}] = d.FixAvailable
						}
					}
					for _, g := range groups {
						if g.Rule != "MD038" {
							continue
						}
						if len(g.Edits) != 2 || g.Anchor != g.Edits[0].Span || g.Edits[0].Span.End() > g.Edits[1].Span.Start() {
							t.Fatalf("group %+v", g)
						}
						for _, e := range g.Edits {
							text, _ := c.Current().Text(e.Span)
							if len(e.Replacement) != 0 || string(e.Expected) != text || text != " " || !reported[[2]int{e.Span.Start(), e.Span.End()}] {
								t.Fatalf("group %+v", g)
							}
						}
					}
					return groups, nil
				}
				view := md038Corrected(t, tc.Source, flavor, producer, caseRules(tc.Config)...)
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
				again := md038Corrected(t, tc.Candidate, flavor, rules.Corrections, caseRules(tc.Config)...)
				if string(again.Candidate.Bytes()) != tc.Candidate || again.ChangingRounds != 0 {
					t.Fatalf("second run %q rounds %d", again.Candidate.Bytes(), again.ChangingRounds)
				}
			})
		}
	}
}

func TestMD038PreciseEditSpans(t *testing.T) {
	source := "a ` é ` b\r\n"
	var spans [][2]int
	producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
		groups, err := rules.Corrections(ctx, c)
		for _, g := range groups {
			for _, e := range g.Edits {
				spans = append(spans, [2]int{e.Span.Start(), e.Span.End()})
			}
		}
		return groups, err
	}
	view := md038Corrected(t, source, "commonmark", producer)
	if got := string(view.Candidate.Bytes()); got != "a `é` b\r\n" {
		t.Fatalf("got %q", got)
	}
	if len(spans) != 2 || spans[0] != [2]int{3, 4} || spans[1] != [2]int{6, 7} {
		t.Fatal(spans)
	}
}

func TestMD038ManualFindingsStayManual(t *testing.T) {
	for _, source := range []string{"`  text  `\n", "``  `a`  ``\n", "` \na `\n", "```\n` text `\n```\n", "<div>\n` text `\n</div>\n", "---\n` x `\n---\n` y `\n"} {
		view := md038Corrected(t, source, "commonmark", rules.Corrections)
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

func TestMD038ComposesWithMD009AndMD047(t *testing.T) {
	view := md038Corrected(t, "One ` a ` \n\n- ` b ` \nend", "commonmark", rules.Corrections, "MD009", "MD047")
	if got := string(view.Candidate.Bytes()); got != "One `a`\n\n- `b`\nend\n" {
		t.Fatalf("got %q", got)
	}
	if len(view.Analysis.Diagnostics) != 0 {
		t.Fatal(view.Analysis.Diagnostics)
	}
	// MD038 waits for the MD009 round, then shares its own with nothing else.
	for _, a := range view.Attempts {
		md038, other := false, false
		for _, d := range a.Decisions {
			if !d.Accepted {
				continue
			}
			md038 = md038 || d.Group.Rule == "MD038"
			other = other || d.Group.Rule == "MD009"
		}
		if md038 && other {
			t.Fatalf("MD038 shared a round with MD009: %+v", a.Decisions)
		}
	}
}

func TestMD038IndependentCheckerRejectsForgedChanges(t *testing.T) {
	edit := func(c fix.RoundContext, start, end int, replacement string) fix.Edit {
		s, _ := c.Current().Span(start, end)
		text, _ := c.Current().Text(s)
		return fix.Edit{Span: s, Expected: []byte(text), Replacement: []byte(replacement)}
	}
	for _, tc := range []struct {
		name, source string
		alter        func(*fix.Group, fix.RoundContext)
	}{
		{"one edge only", "` a `\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits = g.Edits[:1] }},
		{"delete a code byte", "` ab `\n", func(g *fix.Group, c fix.RoundContext) { g.Edits[1] = edit(c, 3, 4, "") }},
		{"replace with a tab", "` a `\n", func(g *fix.Group, c fix.RoundContext) { g.Edits[0] = edit(c, 1, 2, "\t") }},
		{"foreign anchor", "` a `\n", func(g *fix.Group, c fix.RoundContext) { g.Anchor, _ = c.Current().Span(0, 1) }},
		{"foreign anchor source", "` a `\n", func(g *fix.Group, _ fix.RoundContext) {
			g.Anchor, _ = parser.NewSource([]byte("` a `\n")).Span(1, 2)
		}},
		{"three edits", "` a ` b\n", func(g *fix.Group, c fix.RoundContext) { g.Edits = append(g.Edits, edit(c, 6, 7, "")) }},
		{"unknown rule", "` a `\n", func(g *fix.Group, _ fix.RoundContext) { g.Rule = "MD064" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			altered := false
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := rules.Corrections(ctx, c)
				for i := range groups {
					if groups[i].Rule == "MD038" {
						tc.alter(&groups[i], c)
						altered = true
						break
					}
				}
				return groups, err
			}
			view := md038Corrected(t, tc.source, "commonmark", producer)
			if !altered || string(view.Candidate.Bytes()) != tc.source || len(view.Attempts) == 0 {
				t.Fatalf("forged correction accepted: %q %+v", view.Candidate.Bytes(), view.Attempts)
			}
		})
	}
}

func TestMD038CheckerRejectsValueChange(t *testing.T) {
	// A hand-made MD038 group trimming reserved padding changes the code value.
	source := "`  a  `\n"
	producer := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
		left, _ := c.Current().Span(1, 2)
		right, _ := c.Current().Span(5, 6)
		return []fix.Group{{ID: "forged", Rule: "MD038", Provenance: "forged", Anchor: left, Edits: []fix.Edit{{Span: left, Expected: []byte(" ")}, {Span: right, Expected: []byte(" ")}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}}}}, nil
	}
	view := md038Corrected(t, source, "commonmark", producer)
	if string(view.Candidate.Bytes()) != source || len(view.Attempts) == 0 {
		t.Fatalf("forged correction accepted: %q", view.Candidate.Bytes())
	}
}
