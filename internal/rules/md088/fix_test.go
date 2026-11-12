package md088_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

type correctionCase struct {
	Name, Source, Candidate, Flavor, Config string
	Manual                                  int
}

func correctionCases(t *testing.T) []correctionCase {
	t.Helper()
	data, err := os.ReadFile("testdata/md088_corrections.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []correctionCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

// caseRules returns the rules a fixture's Config enables besides MD088.
func caseRules(config string) []string {
	switch config {
	case "md047":
		return []string{"MD047"}
	case "composition":
		return []string{"MD009", "MD047"}
	}
	return nil
}

var mappings = map[string]string{"‘": "'", "’": "'", "“": `"`, "”": `"`, "–": "-", "—": "--"}

// md088Corrected validates producer's corrections of text with MD088 and the
// named extra rules enabled, front matter recognized when config asks for it,
// and checks as the trusted checkers.
func md088Corrected(t *testing.T, text, flavor, config string, checks []fix.CheckerRegistration, producer fix.Producer, extra ...string) fix.FileView {
	t.Helper()
	r := ruletest.Registry(t, rules.Registrations())
	s := ruletest.Settings(extra...)
	on := true
	s["MD088"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"style": "ascii"}}
	selection, err := r.SelectRules(s, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	dc := engine.DocumentContext{Flavor: flavor}
	if config == "frontmatter" {
		if dc.Frontmatter, err = parser.NewFrontmatterOptions(true, nil); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := selection.Plan(dc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if checks == nil {
		checks = rules.CorrectionCheckers()
	}
	validator, err := fix.NewValidator(r, checks)
	if err != nil {
		t.Fatal(err)
	}
	view, out := ruletest.Corrected(t, validator, plan, text, producer)
	if !out.Complete() {
		t.Fatalf("incomplete: %+v", view.Reasons)
	}
	return view
}

func TestMD088FixFixtures(t *testing.T) {
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
				// Every proposed MD088 group replaces exactly one reported
				// scalar with its mapping.
				producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
					groups, err := rules.Corrections(ctx, c)
					if err != nil {
						return nil, err
					}
					reported := map[int]bool{}
					for _, d := range c.Analysis().Diagnostics {
						if d.Rule == "MD088" && d.FixAvailable {
							reported[d.ByteRange.Start] = true
						}
					}
					for _, g := range groups {
						if g.Rule != "MD088" {
							continue
						}
						if len(g.Edits) != 1 || g.Anchor != g.Edits[0].Span || !reported[g.Anchor.Start()] {
							t.Fatalf("group %+v", g)
						}
						e := g.Edits[0]
						text, _ := c.Current().Text(e.Span)
						if string(e.Expected) != text || len(text) != 3 || mappings[text] == "" || string(e.Replacement) != mappings[text] {
							t.Fatalf("group %+v", g)
						}
					}
					return groups, nil
				}
				view := md088Corrected(t, tc.Source, flavor, tc.Config, nil, producer, caseRules(tc.Config)...)
				if got := string(view.Candidate.Bytes()); got != tc.Candidate {
					t.Fatalf("got %q want %q", got, tc.Candidate)
				}
				manual := 0
				for _, d := range view.Analysis.Diagnostics {
					if d.FixAvailable {
						t.Fatal("remaining finding advertised", d)
					}
					if d.Rule == "MD088" {
						manual++
					}
				}
				if manual != tc.Manual {
					t.Fatal("manual", manual, view.Analysis.Diagnostics)
				}
				// Reparse the candidate: a second run changes nothing.
				again := md088Corrected(t, tc.Candidate, flavor, tc.Config, nil, rules.Corrections, caseRules(tc.Config)...)
				if string(again.Candidate.Bytes()) != tc.Candidate || again.ChangingRounds != 0 {
					t.Fatalf("second run %q rounds %d", again.Candidate.Bytes(), again.ChangingRounds)
				}
			})
		}
	}
}

// Every scalar of ‘’“”–— in prose becomes its mapping in one run.
func TestMD088FixEveryMapping(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		view := md088Corrected(t, "‘’“”–—", flavor, "", nil, rules.Corrections)
		if got := string(view.Candidate.Bytes()); got != `''""---` || len(view.Analysis.Diagnostics) != 0 {
			t.Fatalf("%s: %q %v", flavor, got, view.Analysis.Diagnostics)
		}
	}
}

func TestMD088FixPreciseEditSpans(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
		spans              [][2]int
	}{
		{"astral tab", "😀\t“a”\n", "😀\t\"a\"\n", [][2]int{{5, 8}, {9, 12}}},
		{"tab", "x\t— y\n", "x\t-- y\n", [][2]int{{2, 5}}},
		{"crlf", "a — b\r\nc “d”\r\n", "a -- b\r\nc \"d\"\r\n", [][2]int{{2, 5}, {11, 14}, {15, 18}}},
		{"adjacent", "a“”——b", "a\"\"----b", [][2]int{{1, 4}, {4, 7}, {7, 10}, {10, 13}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var spans [][2]int
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := rules.Corrections(ctx, c)
				for _, g := range groups {
					if g.Rule == "MD088" {
						spans = append(spans, [2]int{g.Edits[0].Span.Start(), g.Edits[0].Span.End()})
					}
				}
				return groups, err
			}
			view := md088Corrected(t, tc.source, "commonmark", "", nil, producer)
			if string(view.Candidate.Bytes()) != tc.want || len(spans) != len(tc.spans) {
				t.Fatalf("%q %v", view.Candidate.Bytes(), spans)
			}
			for i, s := range spans {
				if s != tc.spans[i] || s[1]-s[0] != 3 {
					t.Fatalf("%q %v", view.Candidate.Bytes(), spans)
				}
			}
		})
	}
}

// Every deferral stays reported and manual, with the source unchanged.
func TestMD088FixDeferralsStayManual(t *testing.T) {
	for _, tc := range []struct {
		name, source, flavor string
		manual               int
	}{
		{"list marker", "– item\n", "", 1},
		{"setext underline", "a\n———\n", "", 3},
		{"thematic break", "– – –\n", "", 3},
		{"table delimiter cell", "| a |\n| – |\n", "gfm", 1},
		{"comment opener", "<!—x\n", "", 1},
		{"escape quote", "\\“a\n", "", 1},
		{"escape dash", "\\—a\n", "", 1},
		{"autolink domain", "www.example.com.—x\n", "gfm", 1},
		{"autolink email", "a—b@example.com\n", "gfm", 1},
		{"heading dash", "# A — B\n", "", 1},
		{"setext heading dash", "A — B\n===\n", "", 1},
	} {
		for _, flavor := range []string{"commonmark", "gfm"} {
			if tc.flavor != "" && tc.flavor != flavor {
				continue
			}
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				view := md088Corrected(t, tc.source, flavor, "", nil, rules.Corrections)
				if string(view.Candidate.Bytes()) != tc.source || view.ChangingRounds != 0 {
					t.Fatalf("changed %q %+v", view.Candidate.Bytes(), view.Attempts)
				}
				if len(view.Analysis.Diagnostics) != tc.manual {
					t.Fatal(view.Analysis.Diagnostics)
				}
				for _, d := range view.Analysis.Diagnostics {
					if d.Rule != "MD088" || d.FixAvailable {
						t.Fatal(view.Analysis.Diagnostics)
					}
				}
			})
		}
	}
}

func TestMD088FixComposesWithOtherCorrections(t *testing.T) {
	source := "“a” \ntext \nb—c"
	view := md088Corrected(t, source, "commonmark", "", nil, rules.Corrections, "MD009", "MD047")
	if got := string(view.Candidate.Bytes()); got != "\"a\"\ntext\nb--c\n" {
		t.Fatalf("got %q", got)
	}
	if len(view.Analysis.Diagnostics) != 0 {
		t.Fatal(view.Analysis.Diagnostics)
	}
	// MD088 shares its round with no edit but MD047.
	for _, a := range view.Attempts {
		scalar, other := false, false
		for _, d := range a.Decisions {
			if !d.Accepted {
				continue
			}
			scalar = scalar || d.Group.Rule == "MD088"
			other = other || d.Group.Rule != "MD088" && d.Group.Rule != "MD047"
		}
		if scalar && other {
			t.Fatalf("MD088 shared a round: %+v", a.Decisions)
		}
	}
	again := md088Corrected(t, string(view.Candidate.Bytes()), "commonmark", "", nil, rules.Corrections, "MD009", "MD047")
	if again.ChangingRounds != 0 {
		t.Fatal("second run rounds", again.ChangingRounds)
	}
}

// MD088 rewrites follow MD050 and MD049 marker rewrites in later rounds of one
// run.
func TestMD088FixComposesWithDelimiterRules(t *testing.T) {
	source := "“a” __b__ _c_ — *d*\n"
	view := md088Corrected(t, source, "commonmark", "", nil, rules.Corrections, "MD049", "MD050")
	if got := string(view.Candidate.Bytes()); got != "\"a\" __b__ _c_ -- _d_\n" {
		t.Fatalf("got %q", got)
	}
	if len(view.Analysis.Diagnostics) != 0 {
		t.Fatal(view.Analysis.Diagnostics)
	}
}

func TestMD088FixIndependentCheckerRejectsForgedChanges(t *testing.T) {
	retarget := func(g *fix.Group, c fix.RoundContext, start, end int, replacement string) {
		s, _ := c.Current().Span(start, end)
		text, _ := c.Current().Text(s)
		g.Edits[0] = fix.Edit{Span: s, Expected: []byte(text), Replacement: []byte(replacement)}
		g.Anchor = s
	}
	// Each source has one fix-available finding, whose group is forged. When
	// checked is set, the shared validator's own checks pass and the MD088
	// checker must refuse the group itself.
	for _, tc := range []struct {
		name, source string
		checked      bool
		alter        func(*fix.Group, fix.RoundContext)
	}{
		{"wrong replacement", "“a\n", true, func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte("-") }},
		{"single hyphen for em dash", "a — b\n", true, func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte("-") }},
		{"wider span", "“a\n", true, func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 0, 4, `"a`) }},
		{"deferred list marker", "– item “x\n", true, func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 0, 3, "-") }},
		{"heading dash", "# A — B “c\n", true, func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 4, 7, "--") }},
		{"inside a code span", "“ `“b`\n", false, func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 5, 8, `"`) }},
		{"inside a link destination", "“ [b](/“c)\n", false, func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 9, 12, `"`) }},
		{"foreign anchor", "x “a\n", false, func(g *fix.Group, c fix.RoundContext) { g.Anchor, _ = c.Current().Span(0, 1) }},
		{"wrong expected", "“a\n", false, func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Expected = []byte("”") }},
		{"two edits", "“a\n", false, func(g *fix.Group, _ fix.RoundContext) { g.Edits = append(g.Edits, g.Edits[0]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			forged := ""
			var proofs []fix.Proof
			checks := rules.CorrectionCheckers()
			for i := range checks {
				if checks[i].Rule != "MD088" {
					continue
				}
				inner := checks[i].Check
				checks[i].Check = func(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
					proof, err := inner(ctx, c)
					mu.Lock()
					defer mu.Unlock()
					if c.Group().ID == forged {
						proofs = append(proofs, proof)
					}
					return proof, err
				}
			}
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := rules.Corrections(ctx, c)
				for i := range groups {
					if groups[i].Rule == "MD088" {
						tc.alter(&groups[i], c)
						mu.Lock()
						forged = groups[i].ID
						mu.Unlock()
						break
					}
				}
				return groups, err
			}
			view := md088Corrected(t, tc.source, "commonmark", "", checks, producer)
			if forged == "" || string(view.Candidate.Bytes()) != tc.source || len(view.Attempts) == 0 {
				t.Fatalf("forged correction accepted: %q %+v", view.Candidate.Bytes(), view.Attempts)
			}
			if tc.checked && len(proofs) == 0 {
				t.Fatal("checker never saw the forged group")
			}
			for _, p := range proofs {
				if p == fix.Preserved {
					t.Fatal("checker preserved a forged group")
				}
			}
		})
	}
}

// Many fixable quotes and one unsafe line-start dash: bisection keeps the
// reparse count logarithmic, so every quote is corrected within the default
// budget and the dash stays manual.
func TestMD088FixBoundedWork(t *testing.T) {
	var b strings.Builder
	b.WriteString("– item\n\n")
	for range 300 {
		b.WriteString("“a” ‘b’ “c” ‘d’\n")
	}
	source := b.String()
	if strings.Count(source, "“")+strings.Count(source, "”")+strings.Count(source, "‘")+strings.Count(source, "’") < 1000 {
		t.Fatal("too few quotes")
	}
	want := strings.NewReplacer("“", `"`, "”", `"`, "‘", "'", "’", "'").Replace(source)
	view := md088Corrected(t, source, "commonmark", "", nil, rules.Corrections)
	if got := string(view.Candidate.Bytes()); got != want {
		t.Fatalf("got %q", got)
	}
	if len(view.Analysis.Diagnostics) != 1 || view.Analysis.Diagnostics[0].ByteRange.Start != 0 || view.Analysis.Diagnostics[0].FixAvailable {
		t.Fatal(view.Analysis.Diagnostics)
	}
}
