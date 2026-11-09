package rules

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules/internal/listspacing"
)

// listSpacingCorrected validates the production corrections with MD076 at style
// and every other listed rule enabled at its defaults.
func listSpacingCorrected(t *testing.T, text, flavor, style string, recognize bool, producer fix.Producer, ids ...string) (fix.FileView, fix.Validated) {
	t.Helper()
	r := registry(t, Registrations())
	s := spacingSettings(append(ids, "MD076")...)
	on := true
	s["MD076"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"style": style}}
	selection, err := r.SelectRules(s, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	fm, _ := parser.NewFrontmatterOptions(recognize, nil)
	plan, err := selection.Plan(engine.DocumentContext{Flavor: flavor, Frontmatter: fm}, nil)
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

// md076Fixable reports how many MD076 findings remain and whether any of them
// advertises a correction.
func md076Fixable(view fix.FileView) (int, bool) {
	count, fixable := 0, false
	for _, d := range view.Analysis.Diagnostics {
		if d.Rule == "MD076" {
			count++
			fixable = fixable || d.FixAvailable
		}
	}
	return count, fixable
}

func TestListSpacingCorrections(t *testing.T) {
	for _, tc := range []struct {
		name, style, source, want string
	}{
		{"loose deficit", "loose", "- A\n- B\n", "- A\n\n- B\n"},
		{"tight surplus", "tight", "- A\n\n\n- B\n", "- A\n- B\n"},
		{"loose surplus", "loose", "- A\n\n\n- B\n", "- A\n\n- B\n"},
		{"consistent baseline", "consistent", "- A\n\n- B\n- C\n", "- A\n\n- B\n\n- C\n"},
		{"consistent baseline of two", "consistent", "- A\n\n\n- B\n- C\n\n\n\n- D\n", "- A\n\n\n- B\n\n\n- C\n\n\n- D\n"},
		{"CRLF deficit", "loose", "- A\r\n- B\r\n", "- A\r\n\r\n- B\r\n"},
		{"CRLF surplus", "tight", "- A\r\n\r\n\r\n- B\r\n", "- A\r\n- B\r\n"},
		{"CR deficit", "loose", "- A\r- B\r", "- A\r\r- B\r"},
		{"CR surplus", "loose", "- A\r\r\r\r- B\r", "- A\r\r- B\r"},
		{"Unicode content", "loose", "- é ünï\n- 日本語 🙂\n", "- é ünï\n\n- 日本語 🙂\n"},
		{"ordered", "tight", "1. one\n\n2. two\n\n3. three\n", "1. one\n2. two\n3. three\n"},
		{"nested", "loose", "- a\n  - x\n  - y\n- b\n", "- a\n  - x\n\n  - y\n\n- b\n"},
		{"nested tight", "tight", "- a\n  - x\n\n  - y\n- b\n", "- a\n  - x\n  - y\n- b\n"},
		{"blockquote", "loose", "> - A\n> - B\n", "> - A\n>\n> - B\n"},
		{"blockquote surplus with trailing prefix spaces", "tight", "> - A\n> \n>\n> - B\n", "> - A\n> - B\n"},
		{"nested blockquote", "loose", "> > - A\n> > - B\n", "> > - A\n> >\n> > - B\n"},
		{"quote inside item", "loose", "- a\n  > - x\n  > - y\n", "- a\n  > - x\n  >\n  > - y\n"},
		{"closed fence before gap", "loose", "- a\n  ```\n  x\n  ```\n- b\n", "- a\n  ```\n  x\n  ```\n\n- b\n"},
		{"code span on marker line", "loose", "- a\n- `b`\n", "- a\n\n- `b`\n"},
		{"after recognized front matter", "loose", "---\na: 1\n---\n- A\n- B\n", "---\na: 1\n---\n- A\n\n- B\n"},
		{"list in front matter before a body list", "loose", "---\n- A\n- B\n---\n- C\n- D\n", "---\n- A\n- B\n---\n- C\n\n- D\n"},
	} {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				view, out := listSpacingCorrected(t, tc.source, flavor, tc.style, true, Corrections)
				if !out.Complete() || string(view.Candidate.Bytes()) != tc.want {
					t.Fatalf("got %q complete %v attempts %+v", view.Candidate.Bytes(), out.Complete(), view.Attempts)
				}
				if count, _ := md076Fixable(view); count != 0 {
					t.Fatal("findings remain", view.Analysis.Diagnostics)
				}
				again, _ := listSpacingCorrected(t, tc.want, flavor, tc.style, true, Corrections)
				if string(again.Candidate.Bytes()) != tc.want || again.ChangingRounds != 0 {
					t.Fatalf("second run changed %q", again.Candidate.Bytes())
				}
			})
		}
	}
}

func TestListSpacingDeferrals(t *testing.T) {
	for _, tc := range []struct {
		name, style, source string
		recognize           bool
		reported            int
	}{
		{"lazy continuation", "loose", "> - A\nlazy\n> - B\n", false, 1},
		{"nested lazy continuation", "loose", "- a\n  - x\nlazy\n  - y\n", false, 1},
		{"mismatched quote spacing", "loose", ">- A\n> - B\n", false, 1},
		{"mismatched marker prefix", "tight", "> - A\n>\n>  - B\n", false, 1},
		{"tab in prefix", "loose", ">\t- A\n>\t- B\n", false, 1},
		{"tab in gap", "tight", "- A\n\t\n- B\n", false, 1},
		{"unclosed fence", "loose", "- a\n  ```\n- b\n", false, 1},
		{"HTML block", "loose", "- a\n  <div>\n- b\n", false, 1},
		{"unrecognized front matter", "loose", "---\n- A\n- B\n---\n", false, 1},
		{"recognized front matter", "loose", "---\n- A\n- B\n---\n", true, 0},
		{"directive-disabled interval", "loose", "<!-- mdchk:disable MD076 -->\n- A\n- B\n", false, 0},
	} {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				view, out := listSpacingCorrected(t, tc.source, flavor, tc.style, tc.recognize, Corrections)
				if !out.Complete() || string(view.Candidate.Bytes()) != tc.source {
					t.Fatalf("changed %q complete %v", view.Candidate.Bytes(), out.Complete())
				}
				count, fixable := md076Fixable(view)
				if count != tc.reported || fixable {
					t.Fatal("findings", view.Analysis.Diagnostics)
				}
			})
		}
	}
}

func TestListSpacingCheckerRejectsForgedChanges(t *testing.T) {
	forge := func(g *fix.Group, c fix.RoundContext, start, end int, replacement string) {
		s, _ := c.Current().Span(start, end)
		text, _ := c.Current().Text(s)
		g.Edits[0] = fix.Edit{Span: s, Expected: []byte(text), Replacement: []byte(replacement)}
	}
	for _, tc := range []struct {
		name, style, source string
		alter               func(*fix.Group, fix.RoundContext)
	}{
		{"two lines for one", "loose", "- A\n- B\n", func(g *fix.Group, c fix.RoundContext) { forge(g, c, 4, 4, "\n\n") }},
		{"prefix with trailing space", "loose", "> - A\n> - B\n", func(g *fix.Group, c fix.RoundContext) { forge(g, c, 6, 6, "> \n") }},
		{"insertion after the marker", "loose", "- A\n- B\n", func(g *fix.Group, c fix.RoundContext) { forge(g, c, 5, 5, "\n") }},
		{"delete the retained line", "loose", "- A\n\n\n- B\n", func(g *fix.Group, c fix.RoundContext) { forge(g, c, 4, 5, "") }},
		{"delete every blank line", "loose", "- A\n\n\n- B\n", func(g *fix.Group, c fix.RoundContext) { forge(g, c, 4, 6, "") }},
		{"retighten a nested list", "tight", "- A\n  - x\n  - y\n\n- B\n", func(g *fix.Group, c fix.RoundContext) { forge(g, c, 10, 10, "\n") }},
		{"unknown rule", "loose", "- A\n- B\n", func(g *fix.Group, _ fix.RoundContext) { g.Rule = "MD077" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			altered := false
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := Corrections(ctx, c)
				for i := range groups {
					if groups[i].Rule == "MD076" {
						tc.alter(&groups[i], c)
						altered = true
						break
					}
				}
				return groups, err
			}
			view, out := listSpacingCorrected(t, tc.source, "commonmark", tc.style, false, producer)
			if !altered || !out.Complete() || string(view.Candidate.Bytes()) != tc.source || len(view.Attempts) == 0 {
				t.Fatalf("forged correction accepted: %q %+v", view.Candidate.Bytes(), view.Attempts)
			}
		})
	}
}

func TestListSpacingComposesWithOtherFixers(t *testing.T) {
	source := "#  Title\n\n- A   \n- B\n\n## Sub  ##\n\n> - q\n> - r\n\n- x\n\n\n- y"
	want := "# Title\n\n- A\n\n- B\n\n## Sub ##\n\n> - q\n>\n> - r\n\n- x\n\n- y\n"
	ids := []string{"MD009", "MD019", "MD021", "MD047"}
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			view, out := listSpacingCorrected(t, source, flavor, "loose", false, Corrections, ids...)
			if !out.Complete() || string(view.Candidate.Bytes()) != want {
				t.Fatalf("got %q attempts %+v", view.Candidate.Bytes(), view.Attempts)
			}
			for _, d := range view.Analysis.Diagnostics {
				if strings.Contains("MD009 MD019 MD021 MD047 MD076", d.Rule) {
					t.Fatal("finding remains", d)
				}
			}
			again, _ := listSpacingCorrected(t, want, flavor, "loose", false, Corrections, ids...)
			if string(again.Candidate.Bytes()) != want || again.ChangingRounds != 0 {
				t.Fatalf("second run changed %q", again.Candidate.Bytes())
			}
		})
	}
}

func TestListSpacingWaitsForHeadingAndSuffixEdits(t *testing.T) {
	// The heading and suffix edits wait for the separator round, then run.
	view, out := listSpacingCorrected(t, "##  a\n\n- A  \t\n- B\n", "commonmark", "loose", false, Corrections, "MD009", "MD019")
	if !out.Complete() || string(view.Candidate.Bytes()) != "## a\n\n- A\n\n- B\n" || view.ChangingRounds != 2 {
		t.Fatalf("got %q rounds %d", view.Candidate.Bytes(), view.ChangingRounds)
	}
	first := view.Attempts[0].Decisions
	if len(first) != 1 || first[0].Group.Rule != "MD076" || !first[0].Accepted {
		t.Fatalf("first round %+v", first)
	}
}

func TestListSpacingInsertionEndingMatchesSharedPolicy(t *testing.T) {
	for _, source := range []string{"- A\n- B\r\n- C\r- D\n", "- A\r\n- B\r- C\n", "> - A\r> - B\r\n"} {
		t.Run(source, func(t *testing.T) {
			ctx := context.Background()
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			src := parser.NewSource([]byte(source))
			options := engine.DocumentContext{Flavor: "commonmark"}.ParseOptions()
			parsed := parser.ParseWithOptions(ctx, src, budget, options)
			lines := parser.NewLineIndex(src)
			facts, err := parsed.Document.ListItemSpacing(ctx, budget, lines, options, "loose")
			if err != nil || len(facts) == 0 {
				t.Fatal(facts, err)
			}
			checkpoint := func(context.Context, int64) error { return nil }
			for _, f := range facts {
				s, ok, err := listspacing.Correct(ctx, src, lines, nil, f, checkpoint)
				if err != nil || !ok {
					t.Fatal(ok, err)
				}
				want, err := fix.InsertionEnding(ctx, src, lines, s.Edit.Span.Start(), checkpoint)
				if err != nil || !strings.HasSuffix(string(s.Edit.Replacement), string(want)) || strings.Count(string(s.Edit.Replacement), "\n")+strings.Count(string(s.Edit.Replacement), "\r") != len(want) {
					t.Fatalf("%q want ending %q", s.Edit.Replacement, want)
				}
			}
		})
	}
}

func TestListSpacingScalesUnderDefaultLimits(t *testing.T) {
	var source, want strings.Builder
	for i := range 500 {
		if i > 0 {
			want.WriteString("\n")
		}
		fmt.Fprintf(&source, "- item %d\n", i)
		fmt.Fprintf(&want, "- item %d\n", i)
	}
	view, out := listSpacingCorrected(t, source.String(), "commonmark", "loose", false, Corrections)
	if !out.Complete() || string(view.Candidate.Bytes()) != want.String() || view.ChangingRounds != 1 || len(view.Attempts) != 2 {
		t.Fatal(out.Complete(), view.ChangingRounds, len(view.Attempts), view.Reasons)
	}
}
