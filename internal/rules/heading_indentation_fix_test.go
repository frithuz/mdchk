package rules

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/frithuz/mdchk/internal/fix"
)

func TestHeadingIndentationFixturesReparseIdentically(t *testing.T) {
	data, err := os.ReadFile("../../cmd/mdchk/testdata/heading_indentation_corrections.json")
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
			if tc.Config != "" && tc.Config != "compose" {
				continue
			}
			t.Run(flavor+"/"+tc.Name, func(t *testing.T) {
				ids := []string{"MD023"}
				if tc.Config == "compose" {
					ids = append(ids, "MD009", "MD019", "MD021", "MD047")
				}
				view, out := spacingCorrected(t, tc.Source, flavor, Corrections, ids...)
				if !out.Complete() || string(view.Candidate.Bytes()) != tc.Candidate {
					t.Fatalf("got %q complete %v attempts %+v", view.Candidate.Bytes(), out.Complete(), view.Attempts)
				}
				if tc.Config == "" {
					before, after := headingFactsOf(t, tc.Source, flavor), headingFactsOf(t, string(view.Candidate.Bytes()), flavor)
					if len(before) != len(after) {
						t.Fatal(before, after)
					}
					for i := range before {
						// Only a Setext label's raw continuation indentation may change.
						before[i].Label, after[i].Label = "", ""
						if before[i] != after[i] {
							t.Fatalf("%+v became %+v", before[i], after[i])
						}
					}
				}
				if tc.Source != string(view.Candidate.Bytes()) && view.ChangingRounds != 1 {
					t.Fatal("rounds", view.ChangingRounds)
				}
			})
		}
	}
}

func TestHeadingIndentationGroupsHoldEveryLine(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		spans        [][2]int
		anchor       [2]int
	}{
		{"atx", " ## Install\n", [][2]int{{0, 1}}, [2]int{1, 3}},
		{"quote", ">  # H\n", [][2]int{{2, 3}}, [2]int{3, 4}},
		{"list continuation", "- Text\n   # H\n", [][2]int{{9, 10}}, [2]int{10, 11}},
		{"setext", "  Alpha\n Beta\n ===\n", [][2]int{{0, 2}, {8, 9}, {14, 15}}, [2]int{15, 18}},
		{"setext tab", "Alpha\n\tBeta\n===\n", [][2]int{{6, 7}}, [2]int{12, 15}},
		{"crlf", " Alpha\r\n Beta\r\n===\r\n", [][2]int{{0, 1}, {8, 9}}, [2]int{15, 18}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var groups []fix.Group
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				out, err := Corrections(ctx, c)
				if groups == nil {
					groups = out
				}
				return out, err
			}
			spacingCorrected(t, tc.source, "commonmark", producer, "MD023")
			if len(groups) != 1 || groups[0].Rule != "MD023" || groups[0].Anchor.Start() != tc.anchor[0] || groups[0].Anchor.End() != tc.anchor[1] || len(groups[0].Edits) != len(tc.spans) {
				t.Fatalf("%+v", groups)
			}
			for i, e := range groups[0].Edits {
				if e.Span.Start() != tc.spans[i][0] || e.Span.End() != tc.spans[i][1] || len(e.Replacement) != 0 || string(e.Expected) != tc.source[tc.spans[i][0]:tc.spans[i][1]] {
					t.Fatalf("edit %d: %+v", i, e)
				}
			}
		})
	}
}

func TestHeadingIndentationManualFindingsProduceNothing(t *testing.T) {
	for _, source := range []string{">\t# H\n", "- Text\n\t# H\n", "> Alpha\n  Beta\n> ===\n", "> > Alpha\n>  Beta\n> > ===\n", "---\nt: y\n---\n # H\n", "<!-- mdchk:disable MD023 -->\n # H\n"} {
		t.Run(source, func(t *testing.T) {
			view, out := spacingCorrected(t, source, "commonmark", Corrections, "MD023")
			if !out.Complete() || string(view.Candidate.Bytes()) != source || view.ChangingRounds != 0 {
				t.Fatalf("%q %+v", view.Candidate.Bytes(), view.Attempts)
			}
			for _, a := range view.Attempts {
				if len(a.Decisions) != 0 {
					t.Fatal("manual finding proposed", a.Decisions)
				}
			}
		})
	}
}

func TestHeadingIndentationIndependentCheckerRejectsForgedChanges(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		alter        func(*fix.Group, fix.RoundContext)
	}{
		{"partial setext", "  Alpha\n Beta\n ===\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits = g.Edits[:2] }},
		{"replacement", " # H\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte("\t") }},
		{"delete marker", " # H\n", func(g *fix.Group, c fix.RoundContext) {
			s, _ := c.Current().Span(0, 2)
			g.Edits[0] = fix.Edit{Span: s, Expected: []byte(" #")}
		}},
		{"delete quote prefix", ">  # H\n", func(g *fix.Group, c fix.RoundContext) {
			s, _ := c.Current().Span(0, 3)
			g.Edits[0] = fix.Edit{Span: s, Expected: []byte(">  ")}
		}},
		{"delete list padding", "- Text\n   # H\n", func(g *fix.Group, c fix.RoundContext) {
			s, _ := c.Current().Span(7, 10)
			g.Edits[0] = fix.Edit{Span: s, Expected: []byte("   ")}
		}},
		{"foreign anchor", " # H\n\n # J\n", func(g *fix.Group, c fix.RoundContext) { g.Anchor, _ = c.Current().Span(7, 8) }},
		{"unknown rule", " # H\n", func(g *fix.Group, _ fix.RoundContext) { g.Rule = "MD019" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			altered := false
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := Corrections(ctx, c)
				for i := range groups {
					if groups[i].Rule == "MD023" && !altered {
						tc.alter(&groups[i], c)
						altered = true
						groups = groups[i : i+1]
						break
					}
				}
				return groups, err
			}
			view, out := spacingCorrected(t, tc.source, "commonmark", producer, "MD023")
			if !altered || !out.Complete() || string(view.Candidate.Bytes()) != tc.source || len(view.Attempts) == 0 {
				t.Fatalf("forged correction accepted: %q %+v", view.Candidate.Bytes(), view.Attempts)
			}
		})
	}
}

func TestHeadingIndentationComposesInOneRound(t *testing.T) {
	text := " ##  Title  ##\n\ntext   \nmore\n\n  Alpha\n Beta\n ===\n\n> - a\n>\n>    ## Deep\n\nend"
	want := "## Title ##\n\ntext  \nmore\n\nAlpha\nBeta\n===\n\n> - a\n>\n>   ## Deep\n\nend\n"
	view, out := spacingCorrected(t, text, "commonmark", Corrections, "MD009", "MD019", "MD021", "MD023", "MD047")
	if !out.Complete() || string(view.Candidate.Bytes()) != want || view.ChangingRounds != 1 || len(view.Attempts) != 2 {
		t.Fatalf("%q complete %v rounds %d attempts %+v", view.Candidate.Bytes(), out.Complete(), view.ChangingRounds, view.Attempts)
	}
	var rules []string
	for _, d := range view.Attempts[0].Decisions {
		if !d.Accepted {
			t.Fatal("deferred", d)
		}
		rules = append(rules, d.Group.Rule)
	}
	for _, id := range []string{"MD009", "MD019", "MD021", "MD023", "MD047"} {
		if !slices.Contains(rules, id) {
			t.Fatal("missing", id, rules)
		}
	}
	again, _ := spacingCorrected(t, want, "commonmark", Corrections, "MD009", "MD019", "MD021", "MD023", "MD047")
	if string(again.Candidate.Bytes()) != want || again.ChangingRounds != 0 {
		t.Fatal("second run changed", again.Candidate.Bytes())
	}
}
