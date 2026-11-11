package md022_test

import (
	"context"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules"
)

// blankSettings enables MD022 with options, plus any other listed rules.
func blankSettings(options engine.Options, ids ...string) engine.Settings {
	s := spacingSettings(append(ids, "MD022")...)
	on, allowed := true, true
	s["MD022"] = engine.RuleSetting{Enabled: &on, AllowDirectives: &allowed, Options: options}
	return s
}

func blankCorrected(t *testing.T, text string, s engine.Settings, dc engine.DocumentContext, producer fix.Producer) (fix.FileView, fix.Validated) {
	t.Helper()
	r := registry(t, rules.Registrations())
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

func TestMD022ProductionCorrectionAndIdempotence(t *testing.T) {
	two := engine.Options{"before": 2, "after": 2}
	for _, tc := range []struct {
		name, source, want string
		options            engine.Options
		ids                []string
		recognize          bool
	}{
		{"published", "Text\n## Install\nText\n", "Text\n\n## Install\n\nText\n", nil, nil, false},
		{"crlf", "Text\r\n## Install\r\nText\r\n", "Text\r\n\r\n## Install\r\n\r\nText\r\n", nil, nil, false},
		{"quote", "> Text\n> ## H\n> Text\n", "> Text\n>\n> ## H\n>\n> Text\n", nil, nil, false},
		{"nested quote", "> > Text\n> > # H\n> > Text\n", "> > Text\n> >\n> > # H\n> >\n> > Text\n", nil, nil, false},
		{"quote crlf", "> a\r\n> # H\r\n> b\r\n", "> a\r\n>\r\n> # H\r\n>\r\n> b\r\n", nil, nil, false},
		{"consecutive", "# A\n# B\n", "# A\n\n# B\n", nil, nil, false},
		{"consecutive different minima", "# A\n# B\n", "# A\n\n\n# B\n", engine.Options{"before": 1, "after": 2}, nil, false},
		{"before edge exempt", "\n# H\n\nText\n", "\n# H\n\n\nText\n", two, nil, false},
		{"two short by two", "Text\n# H\nText\n", "Text\n\n\n# H\n\n\nText\n", two, nil, false},
		{"one short of two", "Text\n\n# H\n\nText\n", "Text\n\n\n# H\n\n\nText\n", two, nil, false},
		{"before zero", "Text\n# H\nText\n", "Text\n# H\n\nText\n", engine.Options{"before": 0, "after": 1}, nil, false},
		{"after zero", "Text\n# H\nText\n", "Text\n\n# H\nText\n", engine.Options{"before": 1, "after": 0}, nil, false},
		{"both zero", "Text\n# H\nText\n", "Text\n# H\nText\n", engine.Options{"before": 0, "after": 0}, nil, false},
		{"multiline setext", "前\nAlpha\nBeta\n===\nTail\n", "前\nAlpha\nBeta\n===\n\nTail\n", nil, nil, false},
		{"setext after paragraph", "Text\n\nAlpha\n---\nTail\n", "Text\n\nAlpha\n---\n\nTail\n", nil, nil, false},
		{"unicode", "雪\n# Ünïcödé 雪\n本文\n", "雪\n\n# Ünïcödé 雪\n\n本文\n", nil, nil, false},
		{"list item", "- a\n  # H\n  b\n", "- a\n  # H\n  b\n", nil, nil, false},
		{"lazy", "> Text\nlazy\n> ## H\n", "> Text\nlazy\n> ## H\n", nil, nil, false},
		{"fence before", "```\nx\n```\n# H\n", "```\nx\n```\n# H\n", nil, nil, false},
		{"fence after", "# H\n```\nx\n```\n", "# H\n```\nx\n```\n", nil, nil, false},
		{"html after", "# H\n<div>\nx\n</div>\n", "# H\n<div>\nx\n</div>\n", nil, nil, false},
		{"comment before", "<!-- c -->\n# H\n", "<!-- c -->\n# H\n", nil, nil, false},
		{"recognized front matter", "---\nt: y\n---\n# H\n", "---\nt: y\n---\n# H\n", nil, nil, true},
		{"unrecognized front matter", "---\nt: y\n---\nText\n# H\n", "---\nt: y\n---\nText\n# H\n", nil, nil, false},
		{"directive", "Text\n<!-- mdchk:disable MD022 -->\n# H\nText\n", "Text\n<!-- mdchk:disable MD022 -->\n# H\nText\n", nil, nil, false},
		{"composition", "Text \n#  A  #\n# B\nend", "Text\n\n# A #\n\n# B\n\nend\n", nil, []string{"MD009", "MD019", "MD021", "MD047"}, false},
		{"composition crlf", "Text \r\n##  A\r\nend", "Text\r\n\r\n## A\r\n\r\nend\r\n", nil, []string{"MD009", "MD019", "MD047"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fm, _ := parser.NewFrontmatterOptions(tc.recognize, nil)
			dc := engine.DocumentContext{Flavor: "commonmark", Frontmatter: fm}
			s := blankSettings(tc.options, tc.ids...)
			view, out := blankCorrected(t, tc.source, s, dc, rules.Corrections)
			if !out.Complete() || string(view.Candidate.Bytes()) != tc.want {
				t.Fatalf("got %q want %q complete %v reasons %+v", view.Candidate.Bytes(), tc.want, out.Complete(), view.Reasons)
			}
			changed := tc.source != tc.want
			for _, d := range view.Analysis.Diagnostics {
				if d.Rule == "MD022" && (changed || d.FixAvailable) {
					t.Fatalf("MD022 finding left or unsafe fix advertised: %+v", d)
				}
			}
			second, again := blankCorrected(t, tc.want, s, dc, rules.Corrections)
			if !again.Complete() || string(second.Candidate.Bytes()) != tc.want {
				t.Fatalf("not idempotent: %q", second.Candidate.Bytes())
			}
		})
	}
}

func TestMD022InsertedLinesHaveNoTrailingWhitespace(t *testing.T) {
	view, _ := blankCorrected(t, ">  Text\n>  # H\n>  Text\n", blankSettings(nil, "MD009"), engine.DocumentContext{Flavor: "gfm"}, rules.Corrections)
	for _, line := range strings.Split(string(view.Candidate.Bytes()), "\n") {
		if strings.TrimRight(line, " \t") != line {
			t.Fatalf("trailing whitespace in %q", view.Candidate.Bytes())
		}
	}
	if string(view.Candidate.Bytes()) != ">  Text\n>\n>  # H\n>\n>  Text\n" {
		t.Fatalf("%q", view.Candidate.Bytes())
	}
}

func TestMD022HugeMinimumDefers(t *testing.T) {
	big := int(^uint(0) >> 1)
	source := "Text\n# H\nText\n"
	view, out := blankCorrected(t, source, blankSettings(engine.Options{"before": big, "after": big}), engine.DocumentContext{Flavor: "commonmark"}, rules.Corrections)
	if !out.Complete() || string(view.Candidate.Bytes()) != source {
		t.Fatalf("%q", view.Candidate.Bytes())
	}
}

func TestMD022CheckerRejectsForgedInsertions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alter func(*fix.Group, fix.RoundContext)
	}{
		{"extra line", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte("\n\n") }},
		{"trailing space", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte(" \n") }},
		{"wrong ending", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte("\r\n") }},
		{"text", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte("x\n") }},
		{"moved", func(g *fix.Group, c fix.RoundContext) {
			g.Edits[0].Span, _ = c.Current().Span(1, 1)
		}},
		{"replace", func(g *fix.Group, c fix.RoundContext) {
			s, _ := c.Current().Span(0, 4)
			g.Edits[0] = fix.Edit{Span: s, Expected: []byte("Text"), Replacement: []byte("Text\n")}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := rules.Corrections(ctx, c)
				if len(groups) > 0 {
					tc.alter(&groups[0], c)
					groups = groups[:1]
				}
				return groups, err
			}
			source := "Text\n# H\n\nText\n"
			view, out := blankCorrected(t, source, blankSettings(nil), engine.DocumentContext{Flavor: "commonmark"}, producer)
			if !out.Complete() || string(view.Candidate.Bytes()) != source || len(view.Attempts) == 0 {
				t.Fatalf("forged correction accepted: %q", view.Candidate.Bytes())
			}
		})
	}
}
