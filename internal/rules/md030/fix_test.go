package md030_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

type separatorCase struct {
	flavor      string
	spaces      int
	frontmatter bool
	ids         []string
}

func separatorCorrected(t *testing.T, text string, tc separatorCase, producer fix.Producer) (fix.FileView, fix.Validated) {
	t.Helper()
	r := registry(t, rules.Registrations())
	ids := tc.ids
	if ids == nil {
		ids = []string{"MD030"}
	}
	s := ruletest.Settings(ids...)
	if tc.spaces != 0 {
		on := true
		s["MD030"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"spaces": tc.spaces}}
	}
	selection, err := r.SelectRules(s, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	flavor := tc.flavor
	if flavor == "" {
		flavor = "commonmark"
	}
	fm, _ := parser.NewFrontmatterOptions(tc.frontmatter, nil)
	plan, err := selection.Plan(engine.DocumentContext{Flavor: flavor, Frontmatter: fm}, nil)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := fix.NewValidator(r, rules.CorrectionCheckers())
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

// listShape is the list structure the checker must preserve, without coordinates.
func listShape(t *testing.T, text, flavor string) []string {
	t.Helper()
	ctx := context.Background()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	source := parser.NewSource([]byte(text))
	options := engine.DocumentContext{Flavor: flavor}.ParseOptions()
	parsed := parser.ParseWithOptions(ctx, source, budget, options)
	items, err := parsed.Document.ListItems(ctx, budget, parser.NewLineIndex(source), options)
	if err != nil {
		t.Fatal(err)
	}
	index := map[parser.Span]int{}
	out := make([]string, len(items))
	for i, item := range items {
		index[item.Item] = i
		parent := -1
		if item.ParentItem.Present() {
			parent = index[item.ParentItem]
		}
		marker, _ := source.Text(item.Marker)
		out[i] = fmt.Sprintf("%v %q %d", item.Ordered, marker, parent)
	}
	return out
}

func TestMD030CorrectionsRewriteSeparators(t *testing.T) {
	for _, tc := range []struct {
		name, source, want, flavor string
		spaces                     int
	}{
		{"published", "-  One\n", "- One\n", "", 0},
		{"spaces 2 grows", "- One\n", "-  One\n", "", 2},
		{"spaces 4 ordered", "1.  x\n", "1.    x\n", "", 4},
		{"spaces 2 shrinks", "-    x\n", "-  x\n", "", 2},
		{"siblings", "-  a\n-   b\n\n1)  c\n", "- a\n- b\n\n1) c\n", "", 0},
		{"crlf", "-  One\r\n-  Two\r\n", "- One\r\n- Two\r\n", "", 0},
		{"cr", "-  One\r-  Two\r", "- One\r- Two\r", "", 0},
		{"unicode", "-  雪 😀 é\n", "- 雪 😀 é\n", "", 0},
		{"blockquote", "> -  quoted\n", "> - quoted\n", "", 0},
		{"nested", "- a\n  -  b\n", "- a\n  - b\n", "", 0},
		{"quote in list", "- > -  x\n", "- > - x\n", "", 0},
		{"same-line inner", "-  -  x\n", "-  - x\n", "", 0},
		{"loose list", "-  a\n\n-  b\n", "- a\n\n- b\n", "", 0},
		{"gfm task", "-  [x] done\n", "- [x] done\n", "gfm", 0},
		{"followed by paragraph", "-  a\n\nafter\n", "- a\n\nafter\n", "", 0},
		{"no final newline", "-  a", "- a", "", 0},
	} {
		for _, flavor := range []string{"commonmark", "gfm"} {
			if tc.flavor != "" && tc.flavor != flavor {
				continue
			}
			t.Run(tc.name+"/"+flavor, func(t *testing.T) {
				c := separatorCase{flavor: flavor, spaces: tc.spaces}
				view, out := separatorCorrected(t, tc.source, c, rules.Corrections)
				if !out.Complete() || string(view.Candidate.Bytes()) != tc.want || view.ChangingRounds != 1 || len(view.Attempts) != 2 {
					t.Fatalf("%q complete %v rounds %d attempts %+v", view.Candidate.Bytes(), out.Complete(), view.ChangingRounds, view.Attempts)
				}
				for _, d := range view.Analysis.Diagnostics {
					if d.Rule == "MD030" && d.FixAvailable {
						t.Fatal("remaining correctable finding", d)
					}
				}
				before, after := listShape(t, tc.source, flavor), listShape(t, tc.want, flavor)
				if strings.Join(before, "|") != strings.Join(after, "|") {
					t.Fatal(before, after)
				}
				again, _ := separatorCorrected(t, tc.want, c, rules.Corrections)
				if again.ChangingRounds != 0 || string(again.Candidate.Bytes()) != tc.want {
					t.Fatal("second run changed", again.Candidate.Bytes())
				}
			})
		}
	}
}

func TestMD030GroupEditSpans(t *testing.T) {
	for _, tc := range []struct {
		source      string
		spaces      int
		start, end  int
		replacement string
	}{
		{"-  One\n", 1, 1, 3, " "},
		{"-  One\r\n", 1, 1, 3, " "},
		{"> 10.   雪\n", 2, 5, 8, "  "},
		{"- One\n", 4, 1, 2, "    "},
	} {
		t.Run(tc.source, func(t *testing.T) {
			var groups []fix.Group
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				g, err := rules.Corrections(ctx, c)
				if groups == nil {
					groups = g
				}
				return g, err
			}
			separatorCorrected(t, tc.source, separatorCase{spaces: tc.spaces}, producer)
			if len(groups) != 1 || groups[0].Rule != "MD030" || len(groups[0].Edits) != 1 {
				t.Fatalf("%+v", groups)
			}
			g, e := groups[0], groups[0].Edits[0]
			if e.Span.Start() != tc.start || e.Span.End() != tc.end || g.Anchor != e.Span || string(e.Expected) != tc.source[tc.start:tc.end] || string(e.Replacement) != tc.replacement {
				t.Fatalf("%+v", g)
			}
			kinds := map[fix.RequirementKind]bool{}
			for _, r := range g.Requirements {
				kinds[r.Kind] = true
			}
			if !kinds[fix.Meaning] || !kinds[fix.ProtectedSyntax] || !kinds[fix.DirectiveStructure] {
				t.Fatal(g.Requirements)
			}
		})
	}
}

func TestMD030ManualCasesStayUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		spaces       int
		frontmatter  bool
	}{
		{"continuation", "-  One\n   two\n", 0, false},
		{"lazy continuation", "-  One\ntwo\n", 0, false},
		{"child", "-  One\n\n   - child\n", 0, false},
		{"same-line child", "-  -  x\n", 5, false},
		{"tab", "-\tOne\n", 2, false},
		{"tab then spaces", "-\t  One\n", 2, false},
		{"fallback", "-      code\n", 2, false},
		{"fallback ordered", "1.      code\n", 3, false},
		{"spaces 5", "- One\n", 5, false},
		{"would absorb sibling", "-   a\n  - b\n", 1, false},
		{"would absorb paragraph", "-   a\n\n  b\n", 1, false},
		{"would absorb setext underline", "-   a\n  ---\n", 1, false},
		{"quoted with following line", "> -  a\n>\n> b\n", 0, false},
		{"directive", "<!-- mdchk:disable MD030 -->\n-  x\n", 0, false},
		{"directive later item", "- a\n<!-- mdchk:disable MD030 -->\n-  x\n", 0, false},
		{"unrecognized front matter", "---\nt: y\n---\n\n-  x\n", 0, false},
		{"unrecognized toml front matter", "+++\nt = 1\n+++\n\n-  x\n", 0, false},
		{"fenced", "```\n-  x\n```\n", 0, false},
		{"html comment", "<!--\n-  x\n-->\n", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, out := separatorCorrected(t, tc.source, separatorCase{spaces: tc.spaces, frontmatter: tc.frontmatter, ids: []string{"MD030"}}, rules.Corrections)
			if !out.Complete() || string(view.Candidate.Bytes()) != tc.source || view.ChangingRounds != 0 {
				t.Fatalf("%q rounds %d", view.Candidate.Bytes(), view.ChangingRounds)
			}
			for _, d := range view.Analysis.Diagnostics {
				if d.Rule == "MD030" && d.FixAvailable {
					t.Fatal("manual case advertised", d)
				}
			}
		})
	}
}

func TestMD030ManualCasesReportFindings(t *testing.T) {
	r := registry(t, rules.Registrations())
	for _, tc := range []struct {
		name, source string
		spaces       int
	}{
		{"continuation", "-  One\n   two\n", 1},
		{"child", "-  One\n\n   - child\n", 1},
		{"tab", "-\tOne\n", 2},
		{"fallback", "-      code\n", 2},
		{"spaces 5", "- One\n", 5},
		{"outer same-line", "-  -  x\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := md030Settings()
			s["MD030"] = engine.RuleSetting{Options: engine.Options{"spaces": tc.spaces}}
			out := analyzeHeadings(t, r, tc.source, s, engine.DocumentContext{}, engine.RunOptions{})
			if len(out.Diagnostics) == 0 || out.Diagnostics[0].Rule != "MD030" || out.Diagnostics[0].FixAvailable {
				t.Fatal(out.Diagnostics)
			}
		})
	}
}

func TestMD030FrontMatter(t *testing.T) {
	source := "---\n-  yaml\n---\n\n-  outside\n"
	view, out := separatorCorrected(t, source, separatorCase{frontmatter: true}, rules.Corrections)
	if !out.Complete() || string(view.Candidate.Bytes()) != "---\n-  yaml\n---\n\n- outside\n" || view.ChangingRounds != 1 {
		t.Fatalf("%q", view.Candidate.Bytes())
	}
	unrecognized := "---\n-  yaml\n---\n\n-  outside\n"
	view, out = separatorCorrected(t, unrecognized, separatorCase{}, rules.Corrections)
	if !out.Complete() || string(view.Candidate.Bytes()) != unrecognized || view.ChangingRounds != 0 {
		t.Fatalf("%q", view.Candidate.Bytes())
	}
}

func TestMD030Composition(t *testing.T) {
	source := "#  Title\n\n-  item   \n-  two\n\ntext"
	want := "# Title\n\n- item\n- two\n\ntext\n"
	c := separatorCase{ids: []string{"MD009", "MD019", "MD021", "MD030", "MD047"}}
	view, out := separatorCorrected(t, source, c, rules.Corrections)
	if !out.Complete() || string(view.Candidate.Bytes()) != want || view.ChangingRounds != 1 || len(view.Attempts) != 2 || len(view.Analysis.Diagnostics) != 0 {
		t.Fatalf("%q rounds %d attempts %+v diagnostics %+v", view.Candidate.Bytes(), view.ChangingRounds, view.Attempts, view.Analysis.Diagnostics)
	}
	seen := map[string]bool{}
	for _, d := range view.Attempts[0].Decisions {
		if !d.Accepted {
			t.Fatalf("deferred %+v", d)
		}
		seen[d.Group.Rule] = true
	}
	for _, id := range []string{"MD009", "MD019", "MD030", "MD047"} {
		if !seen[id] {
			t.Fatal("missing", id, seen)
		}
	}
	again, _ := separatorCorrected(t, want, c, rules.Corrections)
	if again.ChangingRounds != 0 || string(again.Candidate.Bytes()) != want {
		t.Fatal("second run changed", again.Candidate.Bytes())
	}
}

func TestMD030CheckerRejectsForgedChanges(t *testing.T) {
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
		{"too wide", "-  One\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte("  ") }},
		{"empty", "-  One\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = nil }},
		{"tab", "-  One\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte("\t") }},
		{"partial separator", "-   One\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 2, 4, "") }},
		{"into payload", "-  One\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 1, 4, " ") }},
		{"marker", "-  One\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 0, 3, "* ") }},
		{"changes structure", "-   a\n  - b\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 1, 4, " ") }},
		{"continuation", "-  a\n   b\n", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 1, 3, " ") }},
		{"foreign anchor", "-  One\n", func(g *fix.Group, c fix.RoundContext) { g.Anchor, _ = c.Current().Span(0, 1) }},
		{"two edits", "-  One\n", func(g *fix.Group, c fix.RoundContext) {
			s, _ := c.Current().Span(5, 6)
			g.Edits = append(g.Edits, fix.Edit{Span: s, Expected: []byte("e")})
		}},
		{"other rule", "-  One\n", func(g *fix.Group, _ fix.RoundContext) { g.Rule = "MD019" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			altered := false
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := rules.Corrections(ctx, c)
				if len(groups) == 0 && err == nil {
					// Manual cases still get a forged MD030 group to reject.
					s, _ := c.Current().Span(1, 2)
					groups = []fix.Group{{ID: "forged", Rule: "MD030", Provenance: "forged", Anchor: s, Edits: []fix.Edit{{Span: s, Expected: []byte(" "), Replacement: []byte(" ")}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}}}}
				}
				for i := range groups {
					if groups[i].Rule == "MD030" && !altered {
						tc.alter(&groups[i], c)
						altered = true
					}
				}
				return groups, err
			}
			view, out := separatorCorrected(t, tc.source, separatorCase{}, producer)
			if !altered || !out.Complete() || string(view.Candidate.Bytes()) != tc.source || len(view.Attempts) == 0 {
				t.Fatalf("forged correction accepted: %q %+v", view.Candidate.Bytes(), view.Attempts)
			}
		})
	}
}

func TestMD030ScalesUnderDefaultLimits(t *testing.T) {
	var b strings.Builder
	for i := range 500 {
		fmt.Fprintf(&b, "-  item %d\n", i)
	}
	source := b.String()
	view, out := separatorCorrected(t, source, separatorCase{}, rules.Corrections)
	want := strings.ReplaceAll(source, "-  item", "- item")
	if !out.Complete() || string(view.Candidate.Bytes()) != want || view.ChangingRounds != 1 || len(view.Attempts) != 2 {
		t.Fatal(out.Complete(), view.ChangingRounds, len(view.Attempts), view.Reasons)
	}
}
