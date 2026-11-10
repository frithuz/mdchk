package md077_test

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

// md077Shape lists every node kind with its depth in pre-order, so a reparse
// that moves a line into another item or block shows up as a different shape.
func md077Shape(t *testing.T, text, flavor string) string {
	t.Helper()
	ctx := context.Background()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithOptions(ctx, parser.NewSource([]byte(text)), budget, engine.DocumentContext{Flavor: flavor}.ParseOptions())
	var b strings.Builder
	var walk func(*parser.Node, int)
	walk = func(n *parser.Node, depth int) {
		b.WriteString(strings.Repeat(" ", depth) + string(n.Kind()) + "\n")
		for _, c := range n.Children() {
			walk(c, depth+1)
		}
	}
	walk(parsed.Document.Root(), 0)
	return b.String()
}

func TestMD077FixturesReparseIdentically(t *testing.T) {
	data, err := os.ReadFile("testdata/md077_corrections.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Source, Candidate, Config string
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range cases {
			// Configuration-dependent fixtures run through the production command.
			if tc.Config != "" || strings.Contains(tc.Source, "mdchk:") {
				continue
			}
			t.Run(flavor+"/"+tc.Name, func(t *testing.T) {
				view, out := spacingCorrected(t, tc.Source, flavor, rules.Corrections, "MD009", "MD019", "MD021", "MD047", "MD077")
				if !out.Complete() || string(view.Candidate.Bytes()) != tc.Candidate {
					t.Fatalf("got %q complete %v attempts %+v", view.Candidate.Bytes(), out.Complete(), view.Attempts)
				}
				if before, after := md077Shape(t, tc.Source, flavor), md077Shape(t, tc.Candidate, flavor); before != after {
					t.Fatalf("structure changed:\n%s\nbecame\n%s", before, after)
				}
				// MD077 waits for MD009, so the composition fixture takes two rounds.
				if rounds := 1 + strings.Count(tc.Name, "composition"); tc.Source != tc.Candidate && view.ChangingRounds != rounds {
					t.Fatal("rounds", view.ChangingRounds)
				}
				for _, d := range view.Analysis.Diagnostics {
					if d.Rule == "MD077" && d.FixAvailable {
						t.Fatal("remaining finding advertised", d)
					}
				}
			})
		}
	}
}

func TestMD077Edits(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
	}{
		{"surplus", "- One\n\n   Text\n", "- One\n\n  Text\n"},
		{"deficit", "- - One\n Text\n", "- - One\n    Text\n"},
		{"lazy deficit", "- - One\nText\n", "- - One\n    Text\n"},
		{"crlf", "- One\r\n\r\n   Text\r\n", "- One\r\n\r\n  Text\r\n"},
		{"cr", "- One\r\r   Text\r", "- One\r\r  Text\r"},
		{"unicode", "- One\n\n   Ünïcødé 😀\n", "- One\n\n  Ünïcødé 😀\n"},
		{"fence markers", "- One\n\n   ```\n  code\n   ```\n", "- One\n\n  ```\n  code\n  ```\n"},
		{"fence payload would shift", "- One\n\n   ```go\n      literal\n   ```\n", "- One\n\n   ```go\n      literal\n  ```\n"},
		{"tab", "- One\n\n\tText\n", "- One\n\n\tText\n"},
		{"present quote", "> - One\n> Text\n", "> - One\n> Text\n"},
		{"lazy quote", "> - - One\n Text\n", "> - - One\n Text\n"},
		{"protected inline html", "- - One\n<b>Text</b>\n", "- - One\n<b>Text</b>\n"},
		{"unrecognized front matter", "---\na: 1\n---\n- One\n\n   Text\n", "---\na: 1\n---\n- One\n\n   Text\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, out := spacingCorrected(t, tc.source, "commonmark", rules.Corrections, "MD077")
			if !out.Complete() || string(view.Candidate.Bytes()) != tc.want {
				t.Fatalf("got %q want %q attempts %+v", view.Candidate.Bytes(), tc.want, view.Attempts)
			}
			if tc.source == tc.want {
				for _, a := range view.Attempts {
					if len(a.Decisions) != 0 {
						t.Fatal("manual finding proposed", a.Decisions)
					}
				}
			}
			reported := false
			for _, d := range view.Analysis.Diagnostics {
				if d.Rule == "MD077" {
					reported = true
					if d.FixAvailable {
						t.Fatal("remaining finding advertised", d)
					}
				}
			}
			if reported != (tc.source == tc.want || tc.name == "fence payload would shift") {
				t.Fatal("remaining findings", view.Analysis.Diagnostics)
			}
		})
	}
}

// A recognized front matter block withholds every MD077 edit in the document.
func TestMD077FrontmatterWithholdsEdits(t *testing.T) {
	source := "---\na: 1\n---\n- One\n\n   Text\n"
	r := registry(t, rules.Registrations())
	selection, err := r.SelectRules(ruletest.Settings("MD077"), engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	plan, err := selection.Plan(engine.DocumentContext{Flavor: "commonmark", Frontmatter: fm}, nil)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := fix.NewValidator(r, rules.CorrectionCheckers())
	if err != nil {
		t.Fatal(err)
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	out, err := validator.Validate(context.Background(), []fix.Target{{Path: "doc.md", Source: parser.NewSource([]byte(source)), Plan: plan, Produce: rules.Corrections}}, fix.ValidationOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	view, _ := out.File("doc.md")
	if !out.Complete() || string(view.Candidate.Bytes()) != source || len(view.Analysis.Diagnostics) != 1 || view.Analysis.Diagnostics[0].FixAvailable {
		t.Fatalf("%q %+v", view.Candidate.Bytes(), view.Analysis.Diagnostics)
	}
}

func TestMD077ComposesInOneRun(t *testing.T) {
	text := "#  Title\n\n## Closed  ##\n\n- One\n\n   Text \n\n- Two\n Lazy"
	want := "# Title\n\n## Closed ##\n\n- One\n\n  Text\n\n- Two\n  Lazy\n"
	view, out := spacingCorrected(t, text, "commonmark", rules.Corrections, "MD009", "MD019", "MD021", "MD047", "MD077")
	if !out.Complete() || string(view.Candidate.Bytes()) != want || view.ChangingRounds != 2 || len(view.Attempts) != 3 {
		t.Fatalf("%q complete %v rounds %d attempts %+v", view.Candidate.Bytes(), out.Complete(), view.ChangingRounds, view.Attempts)
	}
	if decisions := view.Attempts[2].Decisions; len(decisions) != 0 {
		t.Fatal("final check proposed", decisions)
	}
	again, _ := spacingCorrected(t, want, "commonmark", rules.Corrections, "MD009", "MD019", "MD021", "MD047", "MD077")
	if string(again.Candidate.Bytes()) != want || again.ChangingRounds != 0 {
		t.Fatal("second run changed", again.Candidate.Bytes())
	}
}

// A real MD077 proposal whose reparse turns a lazy line into a setext underline
// is rejected, and the finding stays reported.
func TestMD077ReparseChangeIsDeferred(t *testing.T) {
	source := "- - One\n===\n"
	view, out := spacingCorrected(t, source, "commonmark", rules.Corrections, "MD077")
	if !out.Complete() || string(view.Candidate.Bytes()) != source || len(view.Attempts) == 0 {
		t.Fatalf("%q %+v", view.Candidate.Bytes(), view.Attempts)
	}
	deferred := false
	for _, d := range view.Attempts[0].Decisions {
		deferred = deferred || d.Group.Rule == "MD077" && !d.Accepted
	}
	if !deferred || len(view.Analysis.Diagnostics) != 1 || view.Analysis.Diagnostics[0].Rule != "MD077" {
		t.Fatal(view.Attempts, view.Analysis.Diagnostics)
	}
}

func TestMD077IndependentCheckerRejectsForgedChanges(t *testing.T) {
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
		// Deleting every space moves the line out of its item.
		{"surplus leaves the item", "- One\n\n   Text\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 7, 10, "") }},
		{"surplus keeps a space", "- One\n\n    Text\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 10, 11, "") }},
		{"surplus deletes a content byte", "- One\n\n   Text\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 10, 11, "") }},
		{"surplus becomes a tab", "- One\n\n   Text\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 7, 10, "\t") }},
		{"deficit too short", "- - One\n Text\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte("  ") }},
		{"deficit too long", "- - One\n Text\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte("        ") }},
		{"deficit tab", "- - One\n Text\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte("\t") }},
		{"foreign anchor", "- - One\n Text\n", func(g *fix.Group, c fix.RoundContext) { g.Anchor, _ = c.Current().Span(0, 1) }},
		{"two edits", "- One\n\n   Text\n", func(g *fix.Group, c fix.RoundContext) {
			s, _ := c.Current().Span(0, 0)
			g.Edits = append(g.Edits, fix.Edit{Span: s, Replacement: []byte(" ")})
		}},
		{"unknown rule", "- One\n\n   Text\n", func(g *fix.Group, _ fix.RoundContext) { g.Rule = "MD007" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			altered := false
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := rules.Corrections(ctx, c)
				for i := range groups {
					if groups[i].Rule == "MD077" && !altered {
						tc.alter(&groups[i], c)
						altered = true
					}
				}
				return groups, err
			}
			view, out := spacingCorrected(t, tc.source, "commonmark", producer, "MD077")
			if !altered || !out.Complete() || string(view.Candidate.Bytes()) != tc.source || len(view.Attempts) == 0 {
				t.Fatalf("forged correction accepted: %q %+v", view.Candidate.Bytes(), view.Attempts)
			}
		})
	}
}

// Indentation the analysis withholds is never authorized, even when a forged
// group spells the exact edit an eligible finding would get.
func TestMD077CheckerRejectsWithheldIndentation(t *testing.T) {
	for _, tc := range []struct {
		source            string
		start, end, fixed int
	}{
		{"- One\n\n\tText\n", 7, 8, 0},
		{"> - - One\n Text\n", 11, 11, 3},
		{"- One\n\n   ```go\n      literal\n   ```\n", 9, 10, 0},
	} {
		t.Run(tc.source, func(t *testing.T) {
			producer := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
				span, _ := c.Current().Span(tc.start, tc.end)
				text, _ := c.Current().Text(span)
				edit := fix.Edit{Span: span, Expected: []byte(text), Replacement: []byte(strings.Repeat(" ", tc.fixed))}
				return []fix.Group{{ID: "forged", Rule: "MD077", Provenance: "forged", Anchor: span, Edits: []fix.Edit{edit}, Requirements: []fix.Requirement{{Kind: fix.Meaning}}}}, nil
			}
			view, out := spacingCorrected(t, tc.source, "commonmark", producer, "MD077")
			if !out.Complete() || string(view.Candidate.Bytes()) != tc.source || len(view.Attempts) == 0 {
				t.Fatalf("forged correction accepted: %q", view.Candidate.Bytes())
			}
		})
	}
}
