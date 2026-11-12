package md049_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

type correctionCase struct {
	Name, Source, Candidate, Style, Flavor, Config string
}

func correctionCases(t *testing.T) []correctionCase {
	t.Helper()
	data, err := os.ReadFile("testdata/md049_corrections.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []correctionCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for i := range cases {
		if cases[i].Style == "" {
			cases[i].Style = "consistent"
		}
	}
	return cases
}

// caseRules returns the rules a fixture's Config enables besides MD049.
func caseRules(config string) []string {
	switch config {
	case "md047":
		return []string{"MD047"}
	case "composition":
		return []string{"MD009", "MD047"}
	}
	return nil
}

// md049Corrected validates producer's corrections of text in dc with MD049 set
// to style and the named extra rules enabled.
func md049Corrected(t *testing.T, text string, dc engine.DocumentContext, style string, producer fix.Producer, extra ...string) fix.FileView {
	t.Helper()
	r := ruletest.Registry(t, rules.Registrations())
	s := ruletest.Settings(extra...)
	on := true
	s["MD049"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"style": style}}
	selection, err := r.SelectRules(s, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(dc, nil)
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

// observed wraps rules.Corrections, keeping the first round's MD049 findings
// and every proposed MD049 group. It fails unless each group rewrites exactly
// the two one-byte markers of an italic its round reports, anchored on the
// opening marker.
func observed(t *testing.T, first *[]result.Diagnostic, groups *[]fix.Group) fix.Producer {
	seen := false
	return func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
		out, err := rules.Corrections(ctx, c)
		reported := map[int]bool{}
		for _, d := range c.Analysis().Diagnostics {
			if d.Rule != "MD049" {
				continue
			}
			reported[d.ByteRange.Start] = d.FixAvailable
			if !seen {
				*first = append(*first, d)
			}
		}
		seen = true
		for _, g := range out {
			if g.Rule != "MD049" {
				continue
			}
			if len(g.Edits) != 2 || g.Anchor != g.Edits[0].Span || !reported[g.Anchor.Start()] {
				t.Errorf("group %+v", g)
			}
			for _, e := range g.Edits {
				text, _ := c.Current().Text(e.Span)
				if e.Span.End()-e.Span.Start() != 1 || string(e.Expected) != text || len(e.Replacement) != 1 || e.Expected[0] == e.Replacement[0] || e.Replacement[0] != '*' && e.Replacement[0] != '_' {
					t.Errorf("edit %+v", e)
				}
			}
			*groups = append(*groups, g)
		}
		return out, err
	}
}

func TestMD049CorrectionFixtures(t *testing.T) {
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
				var first []result.Diagnostic
				var groups []fix.Group
				view := md049Corrected(t, tc.Source, engine.DocumentContext{Flavor: flavor}, tc.Style, observed(t, &first, &groups), caseRules(tc.Config)...)
				if got := string(view.Candidate.Bytes()); got != tc.Candidate {
					t.Fatalf("got %q want %q", got, tc.Candidate)
				}
				if tc.Source == tc.Candidate {
					for _, d := range first {
						if d.FixAvailable {
							t.Fatal("manual finding advertised", d)
						}
					}
				} else if view.ChangingRounds < 1 || len(groups) == 0 {
					t.Fatal("rounds", view.ChangingRounds)
				}
				for _, d := range view.Analysis.Diagnostics {
					if d.FixAvailable {
						t.Fatal("remaining finding advertised", d)
					}
				}
				// Reparse the candidate: a second run changes nothing.
				again := md049Corrected(t, tc.Candidate, engine.DocumentContext{Flavor: flavor}, tc.Style, rules.Corrections, caseRules(tc.Config)...)
				if string(again.Candidate.Bytes()) != tc.Candidate || again.ChangingRounds != 0 {
					t.Fatalf("second run %q rounds %d", again.Candidate.Bytes(), again.ChangingRounds)
				}
			})
		}
	}
}

func TestMD049StyleValues(t *testing.T) {
	for _, tc := range []struct{ style, source, want string }{
		{"underscore", "*a* _b_", "_a_ _b_"},
		{"asterisk", "_a_", "*a*"},
		{"asterisk", "*a* _b_", "*a* *b*"},
		{"consistent", "_a_ *b*", "_a_ _b_"},
		{"consistent", "*a* _b_", "*a* *b*"},
	} {
		view := md049Corrected(t, tc.source, engine.DocumentContext{Flavor: "commonmark"}, tc.style, rules.Corrections)
		if got := string(view.Candidate.Bytes()); got != tc.want {
			t.Fatalf("%s %q: got %q want %q", tc.style, tc.source, got, tc.want)
		}
	}
}

func TestMD049PreciseEditSpans(t *testing.T) {
	for _, tc := range []struct {
		source, want string
		spans        [][2]int
	}{
		{"*one* and _two_\n", "*one* and *two*\n", [][2]int{{10, 11}, {14, 15}}},
		{"*a*\r\n_b_\r\n", "*a*\r\n*b*\r\n", [][2]int{{5, 6}, {7, 8}}},
		{"é «_文_»\r\n*x*\r\n", "é «_文_»\r\n_x_\r\n", [][2]int{{14, 15}, {16, 17}}},
		{"*a* ___x___\n", "*a* *__x__*\n", [][2]int{{4, 5}, {10, 11}}},
	} {
		var first []result.Diagnostic
		var groups []fix.Group
		view := md049Corrected(t, tc.source, engine.DocumentContext{Flavor: "commonmark"}, "consistent", observed(t, &first, &groups))
		var spans [][2]int
		for _, g := range groups {
			for _, e := range g.Edits {
				spans = append(spans, [2]int{e.Span.Start(), e.Span.End()})
			}
		}
		if got := string(view.Candidate.Bytes()); got != tc.want || len(spans) != len(tc.spans) {
			t.Fatalf("%q: got %q spans %v", tc.source, got, spans)
		}
		for i, s := range spans {
			if s != tc.spans[i] {
				t.Fatalf("%q: spans %v want %v", tc.source, spans, tc.spans)
			}
		}
		if len(first) != 1 || first[0].ByteRange.Start != tc.spans[0][0] || first[0].ByteRange.End != tc.spans[0][1] || !first[0].FixAvailable {
			t.Fatalf("%q: findings %+v", tc.source, first)
		}
	}
}

// A finding whose rewrite would change the parse, or that touches protected
// content, stays reported with fix_available false and is never edited.
func TestMD049DeferredFindingsStayManual(t *testing.T) {
	for _, tc := range []struct{ name, source, style string }{
		{"intraword asterisk", "a*b*c\n", "underscore"},
		{"intraword asterisk run", "snake*case*word\n", "underscore"},
		{"flanking interaction", "*a* _x *y_ z*\n", "consistent"},
		{"strong around italic", "*a* **_b_**\n", "consistent"},
		{"unrecognized front matter", "---\nt: _a_\n---\n*a* _b_\n", "consistent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var first []result.Diagnostic
			var groups []fix.Group
			view := md049Corrected(t, tc.source, engine.DocumentContext{Flavor: "commonmark"}, tc.style, observed(t, &first, &groups))
			if string(view.Candidate.Bytes()) != tc.source || view.ChangingRounds != 0 || len(groups) != 0 {
				t.Fatalf("changed %q", view.Candidate.Bytes())
			}
			if len(first) == 0 {
				t.Fatal("no finding")
			}
			for _, d := range first {
				if d.FixAvailable {
					t.Fatal("eligible", d)
				}
			}
		})
	}
}

// Recognized front matter is protected metadata; the body is still corrected.
func TestMD049FrontMatterProtected(t *testing.T) {
	fm, err := parser.NewFrontmatterOptions(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := "---\nt: _a_ *b*\n---\n*a* _b_\n"
	view := md049Corrected(t, source, engine.DocumentContext{Flavor: "commonmark", Frontmatter: fm}, "consistent", rules.Corrections)
	if got := string(view.Candidate.Bytes()); got != "---\nt: _a_ *b*\n---\n*a* *b*\n" {
		t.Fatalf("got %q", got)
	}
}

func TestMD049ComposesWithOtherCorrections(t *testing.T) {
	source := "*a*  \nplain \n_b_"
	view := md049Corrected(t, source, engine.DocumentContext{Flavor: "commonmark"}, "consistent", rules.Corrections, "MD009", "MD047")
	if got := string(view.Candidate.Bytes()); got != "*a*  \nplain\n*b*\n" {
		t.Fatalf("got %q", got)
	}
	if len(view.Analysis.Diagnostics) != 0 {
		t.Fatal(view.Analysis.Diagnostics)
	}
	// MD049 shares its round with no edit but MD047.
	for _, a := range view.Attempts {
		italic, other := false, false
		for _, d := range a.Decisions {
			if !d.Accepted {
				continue
			}
			italic = italic || d.Group.Rule == "MD049"
			other = other || d.Group.Rule != "MD049" && d.Group.Rule != "MD047"
		}
		if italic && other {
			t.Fatalf("MD049 shared a round: %+v", a.Decisions)
		}
	}
	again := md049Corrected(t, string(view.Candidate.Bytes()), engine.DocumentContext{Flavor: "commonmark"}, "consistent", rules.Corrections, "MD009", "MD047")
	if again.ChangingRounds != 0 {
		t.Fatal("second run changed", again.ChangingRounds)
	}
}

func TestMD049IndependentCheckerRejectsForgedChanges(t *testing.T) {
	retarget := func(g *fix.Group, c fix.RoundContext, i, start int) {
		s, _ := c.Current().Span(start, start+1)
		text, _ := c.Current().Text(s)
		g.Edits[i] = fix.Edit{Span: s, Expected: []byte(text), Replacement: g.Edits[i].Replacement}
		if i == 0 {
			g.Anchor = s
		}
	}
	for _, tc := range []struct {
		name, source string
		alter        func(*fix.Group, fix.RoundContext)
	}{
		{"other marker", "*a* _b_\n", func(g *fix.Group, _ fix.RoundContext) {
			g.Edits[0].Replacement, g.Edits[1].Replacement = []byte("-"), []byte("-")
		}},
		{"half pair", "*a* _b_\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits = g.Edits[:1] }},
		{"strong marker", "*a* _b_ __c__\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 1, 9) }},
		{"other italic", "*a* _b_\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 0, 0) }},
		{"intraword", "*a* _b_ x_y_z\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 0, 9); retarget(g, c, 1, 11) }},
		{"inside a code span", "*a* _b_ `_c_`\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 0, 9); retarget(g, c, 1, 11) }},
		{"foreign anchor", "*a* _b_\n", func(g *fix.Group, c fix.RoundContext) { g.Anchor, _ = c.Current().Span(0, 1) }},
		{"wrong expected", "*a* _b_\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Expected = []byte("*") }},
		{"unknown rule", "*a* _b_\n", func(g *fix.Group, _ fix.RoundContext) { g.Rule = "MD050" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			altered := false
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := rules.Corrections(ctx, c)
				for i := range groups {
					if groups[i].Rule == "MD049" {
						tc.alter(&groups[i], c)
						altered = true
						break
					}
				}
				return groups, err
			}
			view := md049Corrected(t, tc.source, engine.DocumentContext{Flavor: "commonmark"}, "consistent", producer)
			if !altered || string(view.Candidate.Bytes()) != tc.source || len(view.Attempts) == 0 {
				t.Fatalf("forged correction accepted: %q %+v", view.Candidate.Bytes(), view.Attempts)
			}
		})
	}
}
