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

// md003FixSettings enables MD003 with the style and the other named rules.
func md003FixSettings(style string, ids ...string) engine.Settings {
	s := spacingSettings(append([]string{"MD003"}, ids...)...)
	on := true
	s["MD003"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"style": style}}
	return s
}

func md003Corrected(t *testing.T, text, style string, producer fix.Producer, ids ...string) fix.FileView {
	t.Helper()
	r := registry(t, Registrations())
	selection, err := r.SelectRules(md003FixSettings(style, ids...), engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
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
	if !ok || !out.Complete() {
		t.Fatal("missing or incomplete file")
	}
	return view
}

func md003Findings(t *testing.T, text, style string) []result.Diagnostic {
	t.Helper()
	out := analyzeHeadings(t, registry(t, Registrations()), text, md003FixSettings(style), engine.DocumentContext{Flavor: "commonmark"}, engine.RunOptions{})
	return out.Diagnostics
}

func TestMD003CorrectionStyles(t *testing.T) {
	for _, tc := range []struct {
		name, style, source, want string
	}{
		{"open to closed", "atx_closed", "# A\n", "# A #\n"},
		{"open h3 to closed", "atx_closed", "### A b\n", "### A b ###\n"},
		{"empty open to closed", "atx_closed", "#\n", "# #\n"},
		{"closed to open", "atx", "## A ##\n", "## A\n"},
		{"wide closing run to open", "atx", "## A ####\n", "## A\n"},
		{"empty closed to open", "atx", "# #\n", "#\n"},
		{"open to setext h1", "setext", "# A\n", "A\n===\n"},
		{"open to setext h2", "setext", "## Title\n", "Title\n-----\n"},
		{"closed to setext", "setext", "# A b #\n", "A b\n===\n"},
		{"setext to open", "atx", "B\n-\n", "## B\n"},
		{"setext to closed", "atx_closed", "B\n===\n", "# B #\n"},
		{"setext at eof", "atx", "B\n--", "## B\n"},
		{"indented setext", "atx", "  B  \n---\n", "  ## B  \n"},
		{"consistent atx baseline", "consistent", "# A\n\nB\n-\n\n## C ##\n", "# A\n\n## B\n\n## C\n"},
		{"consistent closed baseline", "consistent", "# A #\n\n## B\n\nC\n-\n", "# A #\n\n## B ##\n\n## C ##\n"},
		{"consistent setext baseline", "consistent", "A\n=\n\n## B\n\n## C ##\n", "A\n=\n\nB\n---\n\nC\n---\n"},
		{"unicode underline width", "setext", "# Äöü 日本\n", "Äöü 日本\n======\n"},
		{"crlf setext to atx", "atx", "# A\r\n\r\nB\r\n---\r\n", "# A\r\n\r\n## B\r\n"},
		{"crlf atx to setext", "setext", "A\r\n=\r\n\r\n## B\r\n", "A\r\n=\r\n\r\nB\r\n---\r\n"},
		{"block quote closed", "atx_closed", "> ## A\n", "> ## A ##\n"},
		{"block quote to setext", "setext", "> # A\n", "> A\n> ===\n"},
		{"block quote setext to atx", "atx", "> B\n> ---\n", "> ## B\n"},
		{"list item to open", "atx", "- ## A ##\n", "- ## A\n"},
		{"code span content", "atx_closed", "## `code`\n", "## `code` ##\n"},
		{"list item setext to atx", "atx", "- B\n  ---\n", "- ## B\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := md003Corrected(t, tc.source, tc.style, Corrections)
			got := string(view.Candidate.Bytes())
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if strings.Contains(tc.source, "\r\n") && strings.Count(got, "\n") != strings.Count(got, "\r\n") {
				t.Fatalf("line endings changed: %q", got)
			}
			before, after := headingFactsOf(t, tc.source, "commonmark"), headingFactsOf(t, got, "commonmark")
			if len(before) != len(after) {
				t.Fatal(before, after)
			}
			for i := range before {
				if before[i].Level != after[i].Level || before[i].Visible != after[i].Visible || before[i].Containers != after[i].Containers || before[i].Base != after[i].Base {
					t.Fatalf("%+v became %+v", before[i], after[i])
				}
			}
			if len(md003Findings(t, got, tc.style)) != 0 {
				t.Fatal("findings remain", md003Findings(t, got, tc.style))
			}
			again := md003Corrected(t, got, tc.style, Corrections)
			if string(again.Candidate.Bytes()) != got || again.ChangingRounds != 0 {
				t.Fatalf("second run changed %q", again.Candidate.Bytes())
			}
		})
	}
}

func TestMD003CorrectionEditSpans(t *testing.T) {
	for _, tc := range []struct {
		style, source string
		edits         [][3]any
	}{
		{"atx_closed", "## A\n", [][3]any{{4, 4, " ##"}}},
		{"atx", "## A  ##\n", [][3]any{{4, 8, ""}}},
		{"setext", "## A ##\r\n", [][3]any{{0, 3, ""}, {4, 7, ""}, {9, 9, "---\r\n"}}},
		{"atx_closed", "B\n==\n", [][3]any{{0, 0, "# "}, {1, 1, " #"}, {2, 5, ""}}},
	} {
		t.Run(tc.source, func(t *testing.T) {
			var groups []fix.Group
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				g, err := Corrections(ctx, c)
				if groups == nil {
					groups = g
				}
				return g, err
			}
			md003Corrected(t, tc.source, tc.style, producer)
			if len(groups) != 1 || groups[0].Rule != "MD003" || groups[0].Anchor.Start() != 0 || groups[0].Anchor.End() != len(tc.source) || len(groups[0].Edits) != len(tc.edits) {
				t.Fatalf("groups %+v", groups)
			}
			for i, e := range groups[0].Edits {
				want := tc.edits[i]
				if e.Span.Start() != want[0] || e.Span.End() != want[1] || string(e.Replacement) != want[2] || string(e.Expected) != tc.source[e.Span.Start():e.Span.End()] {
					t.Fatalf("edit %d = %d-%d %q, want %v", i, e.Span.Start(), e.Span.End(), e.Replacement, want)
				}
			}
		})
	}
}

func TestMD003CorrectionDeferrals(t *testing.T) {
	for _, tc := range []struct {
		name, style, source string
		recognize           bool
	}{
		{"h3 to setext", "setext", "### C\n", false},
		{"multiline setext", "atx", "B\nC\n---\n", false},
		{"content hashes to open", "atx", "Foo #\n---\n", false},
		{"closed content hashes to open", "atx", "## Foo \\# # ##\n", false},
		{"empty to setext", "setext", "#\n", false},
		{"list marker content to setext", "setext", "# - x\n", false},
		{"paragraph before setext", "setext", "para\n# A\n", false},
		{"list item to setext", "setext", "- # A\n", false},
		{"lazy prefix to setext", "setext", "> x\n# A\n", false},
		{"no line ending to setext", "setext", "# A", false},
		{"disabled directive", "atx", "<!-- mdchk:disable MD003 -->\n## A ##\n", false},
		{"unrecognized front matter", "atx", "---\ntitle: x\n---\n\n## A ##\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			findings := md003Findings(t, tc.source, tc.style)
			md003 := 0
			for _, d := range findings {
				if d.Rule == "MD003" {
					md003++
					if d.FixAvailable {
						t.Fatal("deferred case advertised", d)
					}
				}
			}
			if md003 == 0 && tc.name != "disabled directive" {
				t.Fatal("no MD003 finding", findings)
			}
			view := md003Corrected(t, tc.source, tc.style, Corrections)
			if string(view.Candidate.Bytes()) != tc.source {
				t.Fatalf("changed to %q", view.Candidate.Bytes())
			}
		})
	}
}

func TestMD003CorrectionRecognizedFrontMatter(t *testing.T) {
	source := "---\ntitle: x\n---\n\n## A ##\n"
	r := registry(t, Registrations())
	fm, err := parser.NewFrontmatterOptions(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := analyzeHeadings(t, r, source, md003FixSettings("atx"), engine.DocumentContext{Flavor: "commonmark", Frontmatter: fm}, engine.RunOptions{})
	if len(out.Diagnostics) != 1 || !out.Diagnostics[0].FixAvailable {
		t.Fatal("body heading after recognized front matter", out.Diagnostics)
	}
}

func TestMD003CheckerRejectsForgedChanges(t *testing.T) {
	for _, tc := range []struct {
		name, style, source string
		alter               func(*fix.Group, fix.RoundContext)
	}{
		{"wider closing run", "atx_closed", "# A\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte(" ##") }},
		{"changed content", "atx_closed", "# A\n", func(g *fix.Group, c fix.RoundContext) {
			s, _ := c.Current().Span(2, 3)
			g.Edits[0] = fix.Edit{Span: s, Expected: []byte("A"), Replacement: []byte("B #")}
		}},
		{"keep underline", "atx", "B\n-\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits = g.Edits[:1] }},
		{"wrong rule", "atx", "## A ##\n", func(g *fix.Group, _ fix.RoundContext) { g.Rule = "MD021" }},
		{"foreign anchor", "atx", "## A ##\n\n## B ##\n", func(g *fix.Group, c fix.RoundContext) { g.Anchor, _ = c.Current().Span(9, 17) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			altered := false
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := Corrections(ctx, c)
				if len(groups) > 0 {
					tc.alter(&groups[0], c)
					altered = true
				}
				return groups, err
			}
			view := md003Corrected(t, tc.source, tc.style, producer)
			if !altered || string(view.Candidate.Bytes()) != tc.source || len(view.Attempts) == 0 {
				t.Fatalf("forged correction accepted: %q %+v", view.Candidate.Bytes(), view.Attempts)
			}
		})
	}
}

func TestMD003CorrectionComposes(t *testing.T) {
	source := "# Title\n\n##  Spaced\n\n### Closed   ###\n\nText   \n\nSub\n---\n\nend"
	want := "# Title\n\n## Spaced\n\n### Closed\n\nText\n\n## Sub\n\nend\n"
	ids := []string{"MD009", "MD019", "MD021", "MD047"}
	view := md003Corrected(t, source, "consistent", Corrections, ids...)
	if string(view.Candidate.Bytes()) != want {
		t.Fatalf("got %q", view.Candidate.Bytes())
	}
	again := md003Corrected(t, want, "consistent", Corrections, ids...)
	if string(again.Candidate.Bytes()) != want || again.ChangingRounds != 0 {
		t.Fatalf("second run changed %q", again.Candidate.Bytes())
	}
}

// Analysis eligibility and the validated correction must agree: a fixable
// finding saves a change, and a manual one leaves the bytes alone.
func TestMD003EligibilityMatchesCorrection(t *testing.T) {
	for _, content := range []string{"[foo]: /url", "[foo]", "a |", "Foo\\", "1. x", "<div>", "***", "`code`", "plain", "a \\#"} {
		for _, style := range []string{"setext", "atx_closed"} {
			source := "# " + content + "\n"
			t.Run(style+"/"+content, func(t *testing.T) {
				findings := md003Findings(t, source, style)
				if len(findings) != 1 {
					t.Fatal(findings)
				}
				view := md003Corrected(t, source, style, Corrections)
				changed := string(view.Candidate.Bytes()) != source
				if changed != findings[0].FixAvailable {
					t.Fatalf("fix_available %v but changed %v: %q", findings[0].FixAvailable, changed, view.Candidate.Bytes())
				}
			})
		}
	}
}
