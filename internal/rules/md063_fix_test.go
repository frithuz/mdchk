package rules

import (
	"context"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

type caseOptions struct {
	style     string
	names     []string
	recognize bool
	ids       []string
}

func caseSettings(o caseOptions) engine.Settings {
	s := spacingSettings(append([]string{"MD063"}, o.ids...)...)
	names := o.names
	if names == nil {
		names = []string{}
	}
	on := true
	s["MD063"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"style": o.style, "proper_names": names}}
	return s
}

func caseContext(o caseOptions) engine.DocumentContext {
	fm, _ := parser.NewFrontmatterOptions(o.recognize, nil)
	return engine.DocumentContext{Flavor: "commonmark", Frontmatter: fm}
}

// caseCorrected runs production correction and records the first round's MD063 groups.
func caseCorrected(t *testing.T, text string, o caseOptions, producer fix.Producer) (fix.FileView, fix.Validated, []fix.Group) {
	t.Helper()
	r := registry(t, Registrations())
	selection, err := r.SelectRules(caseSettings(o), engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(caseContext(o), nil)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := fix.NewValidator(r, CorrectionCheckers())
	if err != nil {
		t.Fatal(err)
	}
	var first []fix.Group
	rounds := 0
	recording := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
		groups, err := producer(ctx, c)
		if rounds == 0 {
			for _, g := range groups {
				if g.Rule == "MD063" {
					first = append(first, g)
				}
			}
		}
		rounds++
		return groups, err
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	out, err := validator.Validate(context.Background(), []fix.Target{{Path: "doc.md", Source: parser.NewSource([]byte(text)), Plan: plan, Produce: recording}}, fix.ValidationOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	view, ok := out.File("doc.md")
	if !ok {
		t.Fatal("missing file")
	}
	return view, out, first
}

// caseLint returns the MD063 diagnostic of a lint run, failing unless there is exactly one.
func caseLint(t *testing.T, text string, o caseOptions) (result.Diagnostic, result.Result) {
	t.Helper()
	out := analyzeHeadings(t, registry(t, Registrations()), text, caseSettings(o), caseContext(o), engine.RunOptions{})
	var found []result.Diagnostic
	for _, d := range out.Diagnostics {
		if d.Rule == "MD063" {
			found = append(found, d)
		}
	}
	if len(found) != 1 {
		t.Fatalf("MD063 diagnostics %+v", out.Diagnostics)
	}
	return found[0], out
}

func caseRemaining(view fix.FileView) int {
	n := 0
	for _, d := range view.Analysis.Diagnostics {
		if d.Rule == "MD063" {
			n++
		}
	}
	return n
}

func TestMD063FixCorrects(t *testing.T) {
	sentence, title := caseOptions{style: "sentence"}, caseOptions{style: "title"}
	for _, tc := range []struct {
		name, source, want string
		o                  caseOptions
		spans              [][2]int
	}{
		{"sentence", "## installation guide\n", "## Installation guide\n", sentence, [][2]int{{3, 4}}},
		{"title", "# the quick-brown fox's den\n", "# The Quick-Brown Fox's Den\n", title, [][2]int{{2, 3}, {6, 7}, {12, 13}, {18, 19}, {24, 25}}},
		{"code title", "# `x` guide\n", "# `x` Guide\n", title, [][2]int{{6, 7}}},
		{"link text", "## [guide](lower)\n", "## [Guide](lower)\n", sentence, [][2]int{{4, 5}}},
		{"name later", "## setup for iPhone\n", "## Setup for iPhone\n", caseOptions{style: "sentence", names: []string{"iPhone"}}, [][2]int{{3, 4}}},
		{"multibyte", "## ébauche\n", "## Ébauche\n", sentence, [][2]int{{3, 5}}},
		{"titlecase digraph", "## ǆungla\n", "## ǅungla\n", sentence, [][2]int{{3, 5}}},
		{"uncased script", "## 東京 guide\n", "## 東京 Guide\n", sentence, [][2]int{{10, 11}}},
		{"block quote", "> ## intro\n", "> ## Intro\n", sentence, [][2]int{{5, 6}}},
		{"list item", "- # intro\n", "- # Intro\n", sentence, [][2]int{{4, 5}}},
		{"setext", "intro\n=====\n", "Intro\n=====\n", sentence, [][2]int{{0, 1}}},
		{"closed atx", "## intro ##\n", "## Intro ##\n", sentence, [][2]int{{3, 4}}},
		{"crlf", "## intro\r\n\r\ntext\r\n", "## Intro\r\n\r\ntext\r\n", sentence, [][2]int{{3, 4}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := caseLint(t, tc.source, tc.o)
			if !d.FixAvailable {
				t.Fatal("correctable finding not advertised", d)
			}
			view, out, groups := caseCorrected(t, tc.source, tc.o, Corrections)
			if !out.Complete() || string(view.Candidate.Bytes()) != tc.want || view.ChangingRounds != 1 || caseRemaining(view) != 0 {
				t.Fatalf("%q complete %v rounds %d %+v", view.Candidate.Bytes(), out.Complete(), view.ChangingRounds, view.Attempts)
			}
			if len(groups) != 1 || len(groups[0].Edits) != len(tc.spans) {
				t.Fatalf("groups %+v", groups)
			}
			for i, e := range groups[0].Edits {
				if e.Span.Start() != tc.spans[i][0] || e.Span.End() != tc.spans[i][1] {
					t.Fatalf("edit %d spans %d-%d", i, e.Span.Start(), e.Span.End())
				}
			}
			again, _, _ := caseCorrected(t, tc.want, tc.o, Corrections)
			if string(again.Candidate.Bytes()) != tc.want || again.ChangingRounds != 0 || caseRemaining(again) != 0 {
				t.Fatalf("second run changed %q", again.Candidate.Bytes())
			}
		})
	}
}

func TestMD063FixDefers(t *testing.T) {
	iphone := []string{"iPhone"}
	for _, tc := range []struct {
		name, source string
		o            caseOptions
	}{
		{"numeric entity", "## &#97;bout\n", caseOptions{style: "sentence"}},
		{"named entity", "## &eacute;bauche\n", caseOptions{style: "sentence"}},
		{"recognized front matter", "---\ntitle: a\n---\n# lower\n", caseOptions{style: "sentence", recognize: true}},
		{"case-insensitive name", "## iphone setup\n", caseOptions{style: "sentence", names: iphone}},
		{"adjacent name", "# iPhone case\n", caseOptions{style: "title", names: iphone}},
		{"no titlecase form", "# ßeta\n", caseOptions{style: "sentence"}},
		{"partial heading", "# the &#97;bout\n", caseOptions{style: "title"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := caseLint(t, tc.source, tc.o)
			if d.FixAvailable {
				t.Fatal("deferred finding advertised", d)
			}
			view, _, groups := caseCorrected(t, tc.source, tc.o, Corrections)
			if string(view.Candidate.Bytes()) != tc.source || len(groups) != 0 || caseRemaining(view) != 1 {
				t.Fatalf("%q groups %+v", view.Candidate.Bytes(), groups)
			}
		})
	}
}

func TestMD063FixLeavesUnrecognizedFrontMatter(t *testing.T) {
	for _, tc := range []struct {
		source    string
		recognize bool
	}{
		{"---\ntitle: a\n---\n# lower\n", false},
		{"---\n# lower\n", true},
	} {
		t.Run(tc.source, func(t *testing.T) {
			view, _, groups := caseCorrected(t, tc.source, caseOptions{style: "sentence", recognize: tc.recognize}, Corrections)
			if string(view.Candidate.Bytes()) != tc.source || len(groups) != 0 {
				t.Fatalf("%q groups %+v", view.Candidate.Bytes(), groups)
			}
		})
	}
}

func TestMD063FixAfterSeparatedFrontMatter(t *testing.T) {
	o := caseOptions{style: "sentence", recognize: true}
	source := "---\ntitle: a\n---\n\n# lower\n"
	view, _, _ := caseCorrected(t, source, o, Corrections)
	if string(view.Candidate.Bytes()) != "---\ntitle: a\n---\n\n# Lower\n" {
		t.Fatalf("%q", view.Candidate.Bytes())
	}
}

func TestMD063FixNeverTargetsDestinationsOrHTML(t *testing.T) {
	for _, source := range []string{"## [Guide](lower)\n", "## <a href=\"lower\">Guide</a>\n", "## Guide <b class=\"lower\">x</b>\n"} {
		t.Run(source, func(t *testing.T) {
			view, _, groups := caseCorrected(t, source, caseOptions{style: "sentence"}, Corrections)
			if string(view.Candidate.Bytes()) != source || len(groups) != 0 || caseRemaining(view) != 0 {
				t.Fatalf("%q groups %+v", view.Candidate.Bytes(), groups)
			}
		})
	}
}

func TestMD063FixSkipsSuppressed(t *testing.T) {
	source := "<!-- mdchk:disable MD063 -->\n## lower\n"
	o := caseOptions{style: "sentence"}
	out := analyzeHeadings(t, registry(t, Registrations()), source, caseSettings(o), caseContext(o), engine.RunOptions{})
	if len(out.Diagnostics) != 0 || len(out.SuppressedDiagnostics) != 1 {
		t.Fatal(out.Diagnostics, len(out.SuppressedDiagnostics))
	}
	view, _, groups := caseCorrected(t, source, o, Corrections)
	if string(view.Candidate.Bytes()) != source || len(groups) != 0 {
		t.Fatalf("%q groups %+v", view.Candidate.Bytes(), groups)
	}
}

func TestMD063FixComposesWithSpacingAndWhitespace(t *testing.T) {
	o := caseOptions{style: "sentence", ids: []string{"MD009", "MD019", "MD021", "MD047"}}
	for _, tc := range []struct {
		name, source, want string
		manual             []string
	}{
		{"same heading line", "##  hello world  ##   \ntext", "## Hello world ##\ntext\n", nil},
		{"two headings", "##  hello\n\n# world \n\nend", "## Hello\n\n# World\n\nend\n", nil},
		// A final line's suffix and a missing newline still conflict without MD063.
		{"unterminated suffix", "##  hello world  ##   ", "## Hello world ##   ", []string{"MD009", "MD047"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, out, _ := caseCorrected(t, tc.source, o, Corrections)
			var rules []string
			for _, d := range view.Analysis.Diagnostics {
				rules = append(rules, d.Rule)
			}
			if !out.Complete() || string(view.Candidate.Bytes()) != tc.want || strings.Join(rules, " ") != strings.Join(tc.manual, " ") {
				t.Fatalf("%q %v %+v", view.Candidate.Bytes(), rules, view.Attempts)
			}
			again, _, _ := caseCorrected(t, tc.want, o, Corrections)
			if string(again.Candidate.Bytes()) != tc.want || again.ChangingRounds != 0 {
				t.Fatalf("second run changed %q", again.Candidate.Bytes())
			}
		})
	}
}

func TestMD063IndependentCheckerRejectsForgedChanges(t *testing.T) {
	retarget := func(g *fix.Group, c fix.RoundContext, start, end int, replacement string) {
		s, _ := c.Current().Span(start, end)
		text, _ := c.Current().Text(s)
		g.Edits[0] = fix.Edit{Span: s, Expected: []byte(text), Replacement: []byte(replacement)}
	}
	for _, tc := range []struct {
		name, source, style string
		alter               func(*fix.Group, fix.RoundContext)
	}{
		{"uppercase another letter", "## lower case\n", "sentence", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 4, 5, "O") }},
		{"wrong replacement", "## lower\n", "sentence", func(g *fix.Group, c fix.RoundContext) { g.Edits[0].Replacement = []byte("X") }},
		{"two scalars", "## lower\n", "sentence", func(g *fix.Group, c fix.RoundContext) { retarget(g, c, 3, 5, "LO") }},
		{"drop a title edit", "# the den\n", "title", func(g *fix.Group, _ fix.RoundContext) { g.Edits = g.Edits[:1] }},
		{"foreign anchor", "## lower\n", "sentence", func(g *fix.Group, c fix.RoundContext) { g.Anchor, _ = c.Current().Span(0, 1) }},
		{"unknown rule", "## lower\n", "sentence", func(g *fix.Group, _ fix.RoundContext) { g.Rule = "MD019" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			altered := false
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := Corrections(ctx, c)
				for i := range groups {
					if groups[i].Rule == "MD063" {
						tc.alter(&groups[i], c)
						altered = true
					}
				}
				return groups, err
			}
			view, out, _ := caseCorrected(t, tc.source, caseOptions{style: tc.style}, producer)
			if !altered || !out.Complete() || string(view.Candidate.Bytes()) != tc.source {
				t.Fatalf("forged correction accepted: %q %+v", view.Candidate.Bytes(), view.Attempts)
			}
		})
	}
}

func TestMD063FixScalesUnderDefaultLimits(t *testing.T) {
	var b strings.Builder
	for i := range 500 {
		b.WriteString("## heading `c` number ")
		b.WriteString(strings.Repeat("x", i%7+1))
		b.WriteString("\n\n")
	}
	source := b.String()
	view, out, _ := caseCorrected(t, source, caseOptions{style: "title"}, Corrections)
	want := strings.NewReplacer("## heading `c` number x", "## Heading `c` Number X").Replace(source)
	if !out.Complete() || string(view.Candidate.Bytes()) != want || view.ChangingRounds != 1 || caseRemaining(view) != 0 {
		t.Fatal(out.Complete(), view.ChangingRounds, caseRemaining(view), view.Reasons)
	}
}
