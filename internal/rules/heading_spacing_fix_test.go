package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

func spacingSettings(ids ...string) engine.Settings {
	out := engine.Settings{}
	for _, m := range engine.Catalog() {
		v := false
		for _, id := range ids {
			v = v || m.ID == id
		}
		out[m.ID] = engine.RuleSetting{Enabled: &v}
	}
	return out
}

func spacingCorrected(t *testing.T, text, flavor string, producer fix.Producer, ids ...string) (fix.FileView, fix.Validated) {
	t.Helper()
	r := registry(t, Registrations())
	selection, err := r.SelectRules(spacingSettings(ids...), engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: flavor}, nil)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := fix.NewValidator(r, CorrectionCheckers())
	if err != nil {
		t.Fatal(err)
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	out, err := validator.Validate(context.Background(), []fix.Target{{Path: "doc.md", Source: parser.NewSource([]byte(text)), Plan: plan, Produce: producer}}, fix.ValidationOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	view, ok := out.File("doc.md")
	if !ok {
		t.Fatal("missing file")
	}
	return view, out
}

type headingFacts struct {
	Level                                   int
	Style, Visible, Label, Containers, Base string
	Closed                                  bool
}

func headingFactsOf(t *testing.T, text, flavor string) []headingFacts {
	t.Helper()
	ctx := context.Background()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	source := parser.NewSource([]byte(text))
	options := engine.DocumentContext{Flavor: flavor}.ParseOptions()
	parsed := parser.ParseWithOptions(ctx, source, budget, options)
	headings, err := parsed.Document.Headings(ctx, budget)
	if err != nil {
		t.Fatal(err)
	}
	anchors, availability, err := resource.OriginalHeadingAnchors(ctx, parsed, budget, parser.NewLineIndex(source), options, "github")
	if err != nil || availability != resource.Available || len(anchors) != len(headings) {
		t.Fatal(err, availability, len(anchors))
	}
	out := make([]headingFacts, len(headings))
	for i, h := range headings {
		label, _ := source.Text(h.Label)
		kinds := make([]string, len(h.Containers))
		for j, c := range h.Containers {
			kinds[j] = string(c.Kind)
		}
		out[i] = headingFacts{h.Level, h.Style, h.Visible, label, strings.Join(kinds, ","), anchors[i].Base.Value, h.Closed}
	}
	return out
}

func TestHeadingSpacingFixturesReparseIdentically(t *testing.T) {
	data, err := os.ReadFile("../../cmd/mdchk/testdata/heading_spacing_corrections.json")
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
			if tc.Config != "" && tc.Config != "md047" {
				continue
			}
			t.Run(flavor+"/"+tc.Name, func(t *testing.T) {
				view, out := spacingCorrected(t, tc.Source, flavor, Corrections, "MD019", "MD021", "MD047")
				if !out.Complete() || string(view.Candidate.Bytes()) != tc.Candidate {
					t.Fatalf("got %q complete %v attempts %+v", view.Candidate.Bytes(), out.Complete(), view.Attempts)
				}
				before, after := headingFactsOf(t, tc.Source, flavor), headingFactsOf(t, string(view.Candidate.Bytes()), flavor)
				if len(before) != len(after) {
					t.Fatal(before, after)
				}
				for i := range before {
					if before[i] != after[i] {
						t.Fatalf("%+v became %+v", before[i], after[i])
					}
				}
				if tc.Source != string(view.Candidate.Bytes()) && view.ChangingRounds != 1 {
					t.Fatal("rounds", view.ChangingRounds)
				}
			})
		}
	}
}

func TestHeadingSpacingOneChangingRound(t *testing.T) {
	text := "#  a\n\n##  b  ##\n\n> ###   c   ###\n\n- ####  d\n\n#####     e #####\n\ntail"
	want := "# a\n\n## b ##\n\n> ### c ###\n\n- #### d\n\n##### e #####\n\ntail\n"
	view, out := spacingCorrected(t, text, "commonmark", Corrections, "MD019", "MD021", "MD047")
	if !out.Complete() || string(view.Candidate.Bytes()) != want || view.ChangingRounds != 1 || len(view.Attempts) != 2 {
		t.Fatalf("%q complete %v rounds %d attempts %d", view.Candidate.Bytes(), out.Complete(), view.ChangingRounds, len(view.Attempts))
	}
	if decisions := view.Attempts[1].Decisions; len(decisions) != 0 {
		t.Fatal("final check proposed", decisions)
	}
}

func TestHeadingSpacingIndependentCheckerRejectsForgedChanges(t *testing.T) {
	retarget := func(g *fix.Group, c fix.RoundContext, start, end int, replacement string) {
		s, _ := c.Current().Span(start, end)
		text, _ := c.Current().Text(s)
		g.Anchor = s
		g.Edits[0] = fix.Edit{Span: s, Expected: []byte(text), Replacement: []byte(replacement)}
	}
	for _, tc := range []struct {
		name, source string
		rule         string
		alter        func(*fix.Group, fix.RoundContext)
	}{
		{"delete every space", "##   Title\n", "MD019", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 2, 5, "") }},
		{"delete heading text byte", "##  Title\n", "MD019", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 4, 5, "") }},
		{"replace spaces with a tab", "##   Title\n", "MD019", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 3, 5, "\t") }},
		{"delete first excess space", "##    Title\n", "MD019", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 2, 5, "") }},
		{"keep last excess space", "##    Title\n", "MD019", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 3, 5, "") }},
		{"span beyond the gap", "##  Title\n", "MD019", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 3, 5, "") }},
		{"MD019 anchored on closing gap", "## Title   ##\n", "MD021", func(g *fix.Group, c fix.RoundContext) { g.Rule = "MD019" }},
		{"foreign anchor in the document", "##  Title  ##\n", "MD019", func(g *fix.Group, c fix.RoundContext) { g.Anchor, _ = c.Current().Span(0, 1) }},
		{"foreign anchor source", "##  Title\n", "MD019", func(g *fix.Group, _ fix.RoundContext) {
			g.Anchor, _ = parser.NewSource([]byte("##  Title\n")).Span(3, 4)
		}},
		{"replacement group", "##  Title\n", "MD019", func(g *fix.Group, _ fix.RoundContext) {
			g.Edits[0].Replacement = []byte(" ")
		}},
		{"two edits", "##  Title  ##\n", "MD019", func(g *fix.Group, c fix.RoundContext) {
			s, _ := c.Current().Span(10, 11)
			g.Edits = append(g.Edits, fix.Edit{Span: s, Expected: []byte(" ")})
		}},
		{"unknown rule", "##  Title\n", "MD019", func(g *fix.Group, _ fix.RoundContext) { g.Rule = "MD018" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			altered := false
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := Corrections(ctx, c)
				for i := range groups {
					if groups[i].Rule == tc.rule {
						tc.alter(&groups[i], c)
						altered = true
						break
					}
				}
				return groups, err
			}
			view, out := spacingCorrected(t, tc.source, "commonmark", producer, "MD019", "MD021")
			if !altered || !out.Complete() || string(view.Candidate.Bytes()) != tc.source || len(view.Attempts) == 0 {
				t.Fatalf("forged correction accepted: %q %+v", view.Candidate.Bytes(), view.Attempts)
			}
		})
	}
}

func TestHeadingSpacingCheckerRejectsEditInMixedGap(t *testing.T) {
	for _, source := range []string{"#\t  Mixed\n", "#  \t text\n", "# x  \t#\n"} {
		for _, rule := range []string{"MD019", "MD021"} {
			t.Run(rule+"/"+source, func(t *testing.T) {
				producer := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
					at := strings.Index(source, "  ")
					span, _ := c.Current().Span(at+1, at+2)
					return []fix.Group{{ID: "forged", Rule: rule, Provenance: "forged", Anchor: span, Edits: []fix.Edit{{Span: span, Expected: []byte(" ")}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}}}}, nil
				}
				view, out := spacingCorrected(t, source, "commonmark", producer, "MD019", "MD021")
				if !out.Complete() || string(view.Candidate.Bytes()) != source || len(view.Attempts) == 0 {
					t.Fatalf("forged correction accepted: %q", view.Candidate.Bytes())
				}
			})
		}
	}
}

func TestHeadingSpacingMD047Composition(t *testing.T) {
	for _, tc := range []struct {
		name, source, want, flavor string
		rounds, attempts           int
		manual                     bool
	}{
		{"shares the round", "##  a", "## a\n", "commonmark", 1, 2, false},
		{"tasks shift", "##  Title\n\n- [x] task\n\nend", "## Title\n\n- [x] task\n\nend\n", "gfm", 1, 2, false},
		{"unterminated fence", "##  Title\n\n```\ncode", "## Title\n\n```\ncode", "commonmark", 1, 2, true},
		{"literal task marker", "##  Title\n\n- [x]", "## Title\n\n- [x]", "gfm", 1, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, _ := spacingCorrected(t, tc.source, tc.flavor, Corrections, "MD019", "MD021", "MD047")
			if string(view.Candidate.Bytes()) != tc.want || view.ChangingRounds != tc.rounds || len(view.Attempts) != tc.attempts {
				t.Fatalf("%q rounds %d attempts %+v", view.Candidate.Bytes(), view.ChangingRounds, view.Attempts)
			}
			terminal := 0
			for _, d := range view.Analysis.Diagnostics {
				if d.Rule == "MD047" {
					terminal++
					if d.FixAvailable {
						t.Fatal("deferred correction advertised", d)
					}
				}
			}
			if (terminal == 1) != tc.manual {
				t.Fatal("remaining MD047", terminal, view.Analysis.Diagnostics)
			}
			for _, d := range view.Attempts[len(view.Attempts)-1].Decisions {
				if d.Group.Rule != "MD047" || d.Accepted || !tc.manual {
					t.Fatalf("final check: %+v", d)
				}
			}
		})
	}
}

func spacingScaleSource(n int) string {
	var b strings.Builder
	for i := range n {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "##  Title `c%d` x  ##\n", i)
	}
	return b.String()
}

func TestHeadingSpacingScalesUnderDefaultLimits(t *testing.T) {
	source := spacingScaleSource(500)
	view, out := spacingCorrected(t, source, "commonmark", Corrections, "MD019", "MD021", "MD047")
	want := strings.ReplaceAll(strings.ReplaceAll(source, "##  Title", "## Title"), "x  ##", "x ##")
	if !out.Complete() || string(view.Candidate.Bytes()) != want || view.ChangingRounds != 1 || len(view.Attempts) != 2 || len(view.Analysis.Diagnostics) != 0 {
		t.Fatal(out.Complete(), view.ChangingRounds, len(view.Attempts), len(view.Analysis.Diagnostics), view.Reasons)
	}
}
