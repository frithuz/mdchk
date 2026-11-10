package rules

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
)

func alignmentFindings(t *testing.T, text string, dc engine.DocumentContext) []bool {
	t.Helper()
	s := md005Settings()
	out := analyzeHeadings(t, registry(t, Registrations()), text, s, dc, engine.RunOptions{})
	var fixable []bool
	for _, d := range out.Diagnostics {
		if d.Rule == "MD005" {
			fixable = append(fixable, d.FixAvailable)
		}
	}
	return fixable
}

func TestMD005FixSpans(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		marker, edit [2]int
		replacement  string
	}{
		{"over-indented", "- One\n - Two\n", [2]int{7, 8}, [2]int{6, 7}, ""},
		{"under-indented", "  - One\n - Two\n", [2]int{9, 10}, [2]int{8, 9}, "  "},
		{"ordered", "9. one\n10. two\n 11. three\n", [2]int{16, 19}, [2]int{15, 16}, ""},
		{"blockquote", "> - One\n>  - Two\n", [2]int{11, 12}, [2]int{10, 11}, ""},
		{"nested", "- one\n  - child\n   - child2\n", [2]int{19, 20}, [2]int{18, 19}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, md005Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange.Start != tc.marker[0] || out.Diagnostics[0].ByteRange.End != tc.marker[1] || !out.Diagnostics[0].FixAvailable {
				t.Fatal(out.Diagnostics)
			}
			var groups []fix.Group
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				g, err := Corrections(ctx, c)
				if groups == nil {
					groups = g
				}
				return g, err
			}
			view, _ := spacingCorrected(t, tc.source, "commonmark", producer, "MD005")
			if len(groups) != 1 || len(groups[0].Edits) != 1 || groups[0].Rule != "MD005" {
				t.Fatal(groups)
			}
			g, e := groups[0], groups[0].Edits[0]
			if g.Anchor.Start() != tc.marker[0] || g.Anchor.End() != tc.marker[1] || e.Span.Start() != tc.edit[0] || e.Span.End() != tc.edit[1] || string(e.Replacement) != tc.replacement || string(e.Expected) != tc.source[tc.edit[0]:tc.edit[1]] {
				t.Fatalf("%+v", g)
			}
			want := tc.source[:tc.edit[0]] + tc.replacement + tc.source[tc.edit[1]:]
			if string(view.Candidate.Bytes()) != want {
				t.Fatalf("%q", view.Candidate.Bytes())
			}
		})
	}
}

func TestMD005FixCorrections(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"LF", "- One\n - Two\n", "- One\n- Two\n"},
		{"CRLF", "- One\r\n - Two\r\n", "- One\r\n- Two\r\n"},
		{"CR", "- One\r - Two\r", "- One\r- Two\r"},
		{"Unicode", "- é\n - 🐈 cat\n", "- é\n- 🐈 cat\n"},
		{"task items", "- [ ] one\n - [x] two\n", "- [ ] one\n- [x] two\n"},
		{"blockquote", "> - One\n>  - Two\n", "> - One\n> - Two\n"},
		{"nested quote", "- > - one\n  >  - two\n", "- > - one\n  > - two\n"},
		{"nested list", "- one\n  - child\n   - child2\n - parent2\n", "- one\n  - child\n  - child2\n- parent2\n"},
		{"over-indented", "  - a\n   - b\n", "  - a\n  - b\n"},
		{"under-indented", "   - a\n  - b\n - c\n", "   - a\n   - b\n   - c\n"},
		{"ordered widths", "9. one\n10. two\n 11. three\n", "9. one\n10. two\n11. three\n"},
		{"ordered widths aligned", "9. one\n10. two\n", "9. one\n10. two\n"},
		{"protected fenced code", "```\n- fake\n - fake\n```\n", "```\n- fake\n - fake\n```\n"},
		{"recognized front matter", "---\n- a\n - b\n---\n- one\n - two\n", "---\n- a\n - b\n---\n- one\n- two\n"},
	} {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				dc := engine.DocumentContext{Flavor: flavor}
				if tc.name == "recognized front matter" {
					dc.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil)
				}
				view, out := alignmentCorrected(t, tc.source, dc, "MD005")
				if !out.Complete() || string(view.Candidate.Bytes()) != tc.want {
					t.Fatalf("%q complete %v attempts %+v", view.Candidate.Bytes(), out.Complete(), view.Attempts)
				}
				if again := alignmentFindings(t, tc.want, dc); len(again) != 0 && tc.name != "protected fenced code" {
					t.Fatal("findings remain", again)
				}
				second, _ := alignmentCorrected(t, tc.want, dc, "MD005")
				if string(second.Candidate.Bytes()) != tc.want || second.ChangingRounds != 0 {
					t.Fatalf("second run %q rounds %d", second.Candidate.Bytes(), second.ChangingRounds)
				}
			})
		}
	}
}

func TestMD005FixDeferrals(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		fixable      []bool
		recognize    bool
	}{
		{"tab in indentation", "- p\n    - One\n\t - Two\n", []bool{false}, false},
		{"tab baseline", "- p\n\t- One\n  - Two\n", []bool{false}, false},
		{"continuation line", "- One\n - Two\n   continued\n", []bool{false}, false},
		{"nested child", "- One\n - Two\n   - child\n", []bool{false}, false},
		{"lazy line", "- One\n - Two\nlazy\n", []bool{false}, false},
		{"disabled interval", "<!-- mdchk:disable MD005 -->\n\n- One\n - Two\n", nil, false},
		{"unrecognized front matter", "---\n- fake\n - fake\n---\n- one\n - two\n", []bool{false, false}, false},
		{"insertion", " - One\n- Two\n", []bool{false}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fm, _ := parser.NewFrontmatterOptions(tc.recognize, nil)
			dc := engine.DocumentContext{Flavor: "commonmark", Frontmatter: fm}
			fixable := alignmentFindings(t, tc.source, dc)
			for i, f := range fixable {
				if f || i >= len(tc.fixable) || tc.fixable[i] {
					t.Fatal(fixable)
				}
			}
			if len(fixable) != len(tc.fixable) {
				t.Fatal(fixable)
			}
			view, _ := alignmentCorrected(t, tc.source, dc, "MD005")
			if string(view.Candidate.Bytes()) != tc.source {
				t.Fatalf("%q", view.Candidate.Bytes())
			}
		})
	}
}

func TestMD005FixComposesWithOtherCorrections(t *testing.T) {
	text := "# Title  #\n\n#  Spaced\n\n- One \n - Two\n\nText   \nend"
	want := "# Title #\n\n# Spaced\n\n- One\n- Two\n\nText  \nend\n"
	view, out := alignmentCorrected(t, text, engine.DocumentContext{Flavor: "commonmark"}, "MD005", "MD009", "MD019", "MD021", "MD047")
	if !out.Complete() || string(view.Candidate.Bytes()) != want {
		t.Fatalf("%q complete %v attempts %+v", view.Candidate.Bytes(), out.Complete(), view.Attempts)
	}
	second, _ := alignmentCorrected(t, want, engine.DocumentContext{Flavor: "commonmark"}, "MD005", "MD009", "MD019", "MD021", "MD047")
	if string(second.Candidate.Bytes()) != want || second.ChangingRounds != 0 {
		t.Fatalf("second run %q rounds %d", second.Candidate.Bytes(), second.ChangingRounds)
	}
}

// MD022, MD023 and MD032 compare whole parses across line-end whitespace like
// MD009, so MD005 must wait for their edits instead of failing their round.
func TestMD005FixWaitsForWholeParseCorrections(t *testing.T) {
	for _, tc := range []struct{ rule, text, want string }{
		{"MD022", "# Title\n- One\n - Two\n", "# Title\n\n- One\n- Two\n"},
		{"MD023", " # Title\n\n- One\n - Two\n", "# Title\n\n- One\n- Two\n"},
		{"MD032", "Text\n- One\n - Two\n", "Text\n\n- One\n- Two\n"},
	} {
		t.Run(tc.rule, func(t *testing.T) {
			view, out := alignmentCorrected(t, tc.text, engine.DocumentContext{Flavor: "commonmark"}, "MD005", tc.rule)
			if !out.Complete() || string(view.Candidate.Bytes()) != tc.want {
				t.Fatalf("%q complete %v attempts %+v", view.Candidate.Bytes(), out.Complete(), view.Attempts)
			}
			second, _ := alignmentCorrected(t, tc.want, engine.DocumentContext{Flavor: "commonmark"}, "MD005", tc.rule)
			if string(second.Candidate.Bytes()) != tc.want || second.ChangingRounds != 0 {
				t.Fatalf("second run %q rounds %d", second.Candidate.Bytes(), second.ChangingRounds)
			}
		})
	}
}

func TestMD005CheckerRejectsForgedChanges(t *testing.T) {
	text := "- One\n - Two\n"
	for name, forge := range map[string]func(*fix.Group){
		"replacement": func(g *fix.Group) { g.Edits[0].Replacement = []byte("  ") },
		"anchor":      func(g *fix.Group) { g.Anchor = g.Edits[0].Span },
	} {
		t.Run(name, func(t *testing.T) {
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := Corrections(ctx, c)
				for i := range groups {
					forge(&groups[i])
				}
				return groups, err
			}
			view, _ := alignmentCorrectedWith(t, text, engine.DocumentContext{Flavor: "commonmark"}, producer, "MD005")
			if string(view.Candidate.Bytes()) != text {
				t.Fatalf("%q", view.Candidate.Bytes())
			}
		})
	}
}

func alignmentCorrected(t *testing.T, text string, dc engine.DocumentContext, ids ...string) (fix.FileView, fix.Validated) {
	t.Helper()
	return alignmentCorrectedWith(t, text, dc, Corrections, ids...)
}

func alignmentCorrectedWith(t *testing.T, text string, dc engine.DocumentContext, producer fix.Producer, ids ...string) (fix.FileView, fix.Validated) {
	t.Helper()
	r := registry(t, Registrations())
	selection, err := r.SelectRules(spacingSettings(ids...), engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(dc, nil)
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
