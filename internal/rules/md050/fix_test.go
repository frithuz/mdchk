package md050_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

type correctionCase struct {
	Name, Source, Candidate, Style, Flavor, Config string
	Manual                                         int
}

func correctionCases(t *testing.T) []correctionCase {
	t.Helper()
	data, err := os.ReadFile("testdata/md050_corrections.json")
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

// caseRules returns the rules a fixture's Config enables besides MD050.
func caseRules(config string) []string {
	switch config {
	case "md047":
		return []string{"MD047"}
	case "composition":
		return []string{"MD009", "MD047"}
	}
	return nil
}

// md050Corrected validates producer's corrections of text with MD050 set to
// style and the named extra rules enabled.
func md050Corrected(t *testing.T, text, flavor, style string, producer fix.Producer, extra ...string) fix.FileView {
	t.Helper()
	r := ruletest.Registry(t, rules.Registrations())
	s := ruletest.Settings(extra...)
	on := true
	s["MD050"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"style": style}}
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

func TestMD050FixFixtures(t *testing.T) {
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
				// Every proposed MD050 group rewrites both two-byte markers of a
				// reported Strong to one marker.
				producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
					groups, err := rules.Corrections(ctx, c)
					if err != nil {
						return nil, err
					}
					reported := map[int]bool{}
					for _, d := range c.Analysis().Diagnostics {
						if d.Rule == "MD050" && d.FixAvailable {
							reported[d.ByteRange.Start] = true
						}
					}
					for _, g := range groups {
						if g.Rule != "MD050" {
							continue
						}
						if len(g.Edits) != 2 || g.Anchor != g.Edits[0].Span || !reported[g.Anchor.Start()] {
							t.Fatalf("group %+v", g)
						}
						for _, e := range g.Edits {
							text, _ := c.Current().Text(e.Span)
							r := e.Replacement
							if string(e.Expected) != text || len(text) != 2 || len(r) != 2 || r[0] != r[1] || r[0] == text[0] || r[0] != '*' && r[0] != '_' {
								t.Fatalf("group %+v", g)
							}
						}
					}
					return groups, nil
				}
				view := md050Corrected(t, tc.Source, flavor, tc.Style, producer, caseRules(tc.Config)...)
				if got := string(view.Candidate.Bytes()); got != tc.Candidate {
					t.Fatalf("got %q want %q", got, tc.Candidate)
				}
				manual := 0
				for _, d := range view.Analysis.Diagnostics {
					if d.FixAvailable {
						t.Fatal("remaining finding advertised", d)
					}
					if d.Rule == "MD050" {
						manual++
					}
				}
				if manual != tc.Manual {
					t.Fatal("manual", manual, view.Analysis.Diagnostics)
				}
				// Reparse the candidate: a second run changes nothing.
				again := md050Corrected(t, tc.Candidate, flavor, tc.Style, rules.Corrections, caseRules(tc.Config)...)
				if string(again.Candidate.Bytes()) != tc.Candidate || again.ChangingRounds != 0 {
					t.Fatalf("second run %q rounds %d", again.Candidate.Bytes(), again.ChangingRounds)
				}
			})
		}
	}
}

func TestMD050FixPreciseEditSpans(t *testing.T) {
	for _, tc := range []struct {
		name, source, style, want string
		spans                     [][2][2]int
	}{
		{"asterisk", "**a** __b__", "asterisk", "**a** **b**", [][2][2]int{{{6, 8}, {9, 11}}}},
		{"underscore", "**a** __b__", "underscore", "__a__ __b__", [][2][2]int{{{0, 2}, {3, 5}}}},
		{"unicode", "é __ü__ **x**", "asterisk", "é **ü** **x**", [][2][2]int{{{3, 5}, {7, 9}}}},
		{"crlf", "**a**\r\n__b__\r\n", "consistent", "**a**\r\n**b**\r\n", [][2][2]int{{{7, 9}, {10, 12}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var spans [][2][2]int
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := rules.Corrections(ctx, c)
				for _, g := range groups {
					if g.Rule == "MD050" {
						spans = append(spans, [2][2]int{{g.Edits[0].Span.Start(), g.Edits[0].Span.End()}, {g.Edits[1].Span.Start(), g.Edits[1].Span.End()}})
					}
				}
				return groups, err
			}
			view := md050Corrected(t, tc.source, "commonmark", tc.style, producer)
			if string(view.Candidate.Bytes()) != tc.want || len(spans) != len(tc.spans) || spans[0] != tc.spans[0] {
				t.Fatalf("%q %v", view.Candidate.Bytes(), spans)
			}
		})
	}
}

func TestMD050FixDeferralsStayManual(t *testing.T) {
	for _, tc := range []struct {
		name, source, style string
	}{
		{"intraword underscore", "foo**bar**baz\n", "underscore"},
		{"triple asterisk run", "__a__ ***x***\n", "consistent"},
		{"triple underscore run", "**a** ___x___\n", "consistent"},
		{"nested runs merge", "**outer __inner__**\n", "consistent"},
		{"adjacent run", "**a** __b__*c*\n", "consistent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := md050Corrected(t, tc.source, "commonmark", tc.style, rules.Corrections)
			if string(view.Candidate.Bytes()) != tc.source || view.ChangingRounds != 0 {
				t.Fatalf("changed %q", view.Candidate.Bytes())
			}
			if len(view.Analysis.Diagnostics) != 1 || view.Analysis.Diagnostics[0].Rule != "MD050" || view.Analysis.Diagnostics[0].FixAvailable {
				t.Fatal(view.Analysis.Diagnostics)
			}
		})
	}
}

func TestMD050FixComposesWithOtherCorrections(t *testing.T) {
	source := "**a** __b__ \ntext \n__c__"
	view := md050Corrected(t, source, "commonmark", "consistent", rules.Corrections, "MD009", "MD047")
	if got := string(view.Candidate.Bytes()); got != "**a** **b**\ntext\n**c**\n" {
		t.Fatalf("got %q", got)
	}
	if len(view.Analysis.Diagnostics) != 0 {
		t.Fatal(view.Analysis.Diagnostics)
	}
	// MD050 shares its round with no edit but MD047.
	for _, a := range view.Attempts {
		bold, other := false, false
		for _, d := range a.Decisions {
			if !d.Accepted {
				continue
			}
			bold = bold || d.Group.Rule == "MD050"
			other = other || d.Group.Rule != "MD050" && d.Group.Rule != "MD047"
		}
		if bold && other {
			t.Fatalf("MD050 shared a round: %+v", a.Decisions)
		}
	}
}

// MD050 and MD049 rewrite bold and italic markers in separate rounds of one run.
func TestMD050FixComposesWithMD049(t *testing.T) {
	source := "**a** __b__ *c* _d_ ***e***\n"
	view := md050Corrected(t, source, "commonmark", "consistent", rules.Corrections, "MD049")
	if got := string(view.Candidate.Bytes()); got != "**a** **b** *c* *d* ***e***\n" {
		t.Fatalf("got %q", got)
	}
	if len(view.Analysis.Diagnostics) != 0 {
		t.Fatal(view.Analysis.Diagnostics)
	}
	again := md050Corrected(t, string(view.Candidate.Bytes()), "commonmark", "consistent", rules.Corrections, "MD049")
	if again.ChangingRounds != 0 {
		t.Fatal("second run rounds", again.ChangingRounds)
	}
}

func TestMD050FixIndependentCheckerRejectsForgedChanges(t *testing.T) {
	retarget := func(g *fix.Group, c fix.RoundContext, i, start, end int, replacement string) {
		s, _ := c.Current().Span(start, end)
		text, _ := c.Current().Text(s)
		g.Edits[i] = fix.Edit{Span: s, Expected: []byte(text), Replacement: []byte(replacement)}
		if i == 0 {
			g.Anchor = s
		}
	}
	for _, tc := range []struct {
		name, source string
		alter        func(*fix.Group, fix.RoundContext)
	}{
		{"wrong marker", "**a** __b__\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits[1].Replacement = []byte("__") }},
		{"mixed markers", "**a** __b__\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits[1].Replacement = []byte("*_") }},
		{"one edit", "**a** __b__\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits = g.Edits[:1] }},
		{"wider span", "**a** __b__\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 0, 5, 8, " **") }},
		{"inside a code span", "**a** __b__ `__c__`\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 0, 13, 15, "**") }},
		{"deferred strong", "__a__ **b** foo**c**bar\n", func(g *fix.Group, c fix.RoundContext) {
			retarget(g, c, 0, 15, 17, "__")
			retarget(g, c, 1, 18, 20, "__")
		}},
		{"foreign anchor", "**a** __b__\n", func(g *fix.Group, c fix.RoundContext) { g.Anchor, _ = c.Current().Span(0, 1) }},
		{"wrong expected", "**a** __b__\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Expected = []byte("**") }},
		{"unknown rule", "**a** __b__\n", func(g *fix.Group, _ fix.RoundContext) { g.Rule = "MD064" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			altered := false
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := rules.Corrections(ctx, c)
				for i := range groups {
					if groups[i].Rule == "MD050" {
						tc.alter(&groups[i], c)
						altered = true
						break
					}
				}
				return groups, err
			}
			view := md050Corrected(t, tc.source, "commonmark", "consistent", producer)
			if !altered || string(view.Candidate.Bytes()) != tc.source || len(view.Attempts) == 0 {
				t.Fatalf("forged correction accepted: %q %+v", view.Candidate.Bytes(), view.Attempts)
			}
		})
	}
}
