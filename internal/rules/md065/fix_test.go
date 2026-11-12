package md065_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/md065"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

// md065Producer runs only MD065's part of rules.Corrections.
func md065Producer(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
	analysis, err := c.BoundedAnalysis(ctx)
	if err != nil {
		return nil, err
	}
	var findings []result.Diagnostic
	for _, d := range analysis.Diagnostics {
		if d.Rule == "MD065" && d.FixAvailable {
			findings = append(findings, d)
		}
	}
	return md065.Correction(ctx, c, findings)
}

func md065Corrected(t *testing.T, text string, producer fix.Producer, ids ...string) (fix.FileView, fix.Validated) {
	t.Helper()
	return ruletest.CorrectedFlavor(t, rules.Registrations(), rules.CorrectionCheckers(), text, "commonmark", producer, ids...)
}

func TestMD065CorrectionOutputs(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"both sides", "Text\n***\nMore\n", "Text\n\n***\n\nMore\n"},
		{"before", "Text\n***\n\nMore\n", "Text\n\n***\n\nMore\n"},
		{"after", "Text\n\n***\nMore\n", "Text\n\n***\n\nMore\n"},
		{"no final newline", "Text\n***\nMore", "Text\n\n***\n\nMore"},
		{"crlf", "Text\r\n***\r\nMore\r\n", "Text\r\n\r\n***\r\n\r\nMore\r\n"},
		{"cr", "Text\r***\rMore\r", "Text\r\r***\r\rMore\r"},
		{"unicode", "雪の日\n***\n終わり ❄\n", "雪の日\n\n***\n\n終わり ❄\n"},
		{"underscores", "Text\n_ _ _\nMore\n", "Text\n\n_ _ _\n\nMore\n"},
		{"blockquote", "> a\n> ***\n> b\n", "> a\n>\n> ***\n>\n> b\n"},
		{"indented quote", " > a\n > ***\n > b\n", " > a\n >\n > ***\n >\n > b\n"},
		{"nested quote", "> > a\n> > ***\n> > b\n", "> > a\n> >\n> > ***\n> >\n> > b\n"},
		{"quote neighbours", "> a\n***\n> b\n", "> a\n\n***\n\n> b\n"},
		{"quote in list", "1. a\n   > q\n   > ***\n", "1. a\n   > q\n   >\n   > ***\n"},
		{"loose list item", "- a\n\n  b\n  ***\n- c\n", "- a\n\n  b\n\n  ***\n- c\n"},
		{"list neighbour", "- a\n\n***\nText\n", "- a\n\n***\n\nText\n"},
		{"shared gap", "Text\n\n***\n***\n\nMore\n", "Text\n\n***\n\n***\n\nMore\n"},
		{"three breaks", "***\n***\n***\n", "***\n\n***\n\n***\n"},
		{"setext guard", "Text\n***\n---\n", "Text\n\n***\n\n---\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, out := md065Corrected(t, tc.source, rules.Corrections, "MD065")
			if !out.Complete() || string(view.Candidate.Bytes()) != tc.want {
				t.Fatalf("%q complete %v", view.Candidate.Bytes(), out.Complete())
			}
			for _, d := range view.Analysis.Diagnostics {
				if d.Rule == "MD065" {
					t.Fatal("finding remains", d)
				}
			}
			again, _ := md065Corrected(t, tc.want, rules.Corrections, "MD065")
			if string(again.Candidate.Bytes()) != tc.want || again.ChangingRounds != 0 {
				t.Fatalf("second run %q", again.Candidate.Bytes())
			}
		})
	}
}

// Each group inserts exactly one prefix-only line at a line start, anchored at
// its finding's point. A shared gap gets two groups with one identical edit.
func TestMD065CorrectionGroups(t *testing.T) {
	type want struct {
		id         string
		anchor, at int
		line       string
	}
	for _, tc := range []struct {
		name, source string
		groups       []want
	}{
		{"both sides", "Text\n***\nMore\n", []want{{"MD065-before-5", 5, 5, "\n"}, {"MD065-after-9", 8, 9, "\n"}}},
		{"crlf", "Text\r\n***\r\nMore\r\n", []want{{"MD065-before-6", 6, 6, "\r\n"}, {"MD065-after-11", 9, 11, "\r\n"}}},
		{"unicode", "雪の日\n***\n終わり ❄\n", []want{{"MD065-before-10", 10, 10, "\n"}, {"MD065-after-14", 13, 14, "\n"}}},
		{"blockquote", "> a\n> ***\n> b\n", []want{{"MD065-before-4", 6, 4, ">\n"}, {"MD065-after-10", 9, 10, ">\n"}}},
		{"quote in list", "1. a\n   > q\n   > ***\n", []want{{"MD065-before-12", 17, 12, "   >\n"}}},
		{"shared gap", "Text\n\n***\n***\n\nMore\n", []want{{"MD065-after-10", 9, 10, "\n"}, {"MD065-before-10", 10, 10, "\n"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, _ := md065Corrected(t, tc.source, md065Producer, "MD065")
			decisions := view.Attempts[0].Decisions
			if len(decisions) != len(tc.groups) {
				t.Fatal(decisions)
			}
			byID := map[string]fix.Decision{}
			for _, d := range decisions {
				byID[d.Group.ID] = d
			}
			for _, w := range tc.groups {
				d, ok := byID[w.id]
				if !ok || !d.Accepted || d.Group.Rule != "MD065" || d.Group.Anchor.Start() != w.anchor || d.Group.Anchor.End() != w.anchor || len(d.Group.Edits) != 1 || len(d.Group.Requirements) != 3 {
					t.Fatalf("%s: %+v", w.id, d)
				}
				e := d.Group.Edits[0]
				if e.Span.Start() != w.at || e.Span.End() != w.at || string(e.Replacement) != w.line || len(e.Expected) != 0 {
					t.Fatalf("%s: %+v", w.id, e)
				}
			}
			if tc.name == "shared gap" && string(view.Candidate.Bytes()) != "Text\n\n***\n\n***\n\nMore\n" {
				t.Fatalf("%q", view.Candidate.Bytes())
			}
		})
	}
}

// A blank line inside a tight list's item would make the list loose, a lazy or
// shallower neighbour lacks the break's quote prefix, a tab in the prefix can't
// be classified, a code or HTML neighbour is protected and an unrecognized
// leading block withholds the whole document, so these findings stay manual.
func TestMD065CorrectionDeferrals(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		starts       []int
	}{
		{"tight list", "- a\n  ***\n- b\n", []int{6}},
		{"unknown tightness", "- a\n  ***\n", []int{6}},
		{"lazy neighbour", "> a\nb\n> ***\n", []int{8}},
		{"tab prefix", "> a\n>\t***\n", []int{6}},
		{"fenced code before", "```\nx\n```\n***\n\nMore\n", []int{10}},
		{"fenced code after", "Text\n\n***\n```\nx\n```\n", []int{9}},
		{"indented code", "    x\n***\n\nMore\n", []int{6}},
		{"html comment", "<!-- c -->\n***\n\nMore\n", []int{11}},
		{"html block", "Text\n\n***\n<div>\nx\n</div>\n", []int{9}},
		{"unrecognized front matter", "---\ntitle: x\n---\nText\n***\nMore\n", []int{3, 22, 25}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, _ := md065Corrected(t, tc.source, rules.Corrections, "MD065")
			if string(view.Candidate.Bytes()) != tc.source || len(view.Attempts[0].Decisions) != 0 {
				t.Fatalf("%q %+v", view.Candidate.Bytes(), view.Attempts)
			}
			var starts []int
			for _, d := range view.Analysis.Diagnostics {
				if d.Rule == "MD065" {
					starts = append(starts, d.ByteRange.Start)
					if d.FixAvailable {
						t.Fatal("deferred correction advertised", d)
					}
				}
			}
			if len(starts) != len(tc.starts) {
				t.Fatal(starts, tc.starts)
			}
			for i := range starts {
				if starts[i] != tc.starts[i] {
					t.Fatal(starts, tc.starts)
				}
			}
		})
	}
}

// MD065 composes with the other fixers that edit the same lines and converges in
// one run: a second run changes nothing.
func TestMD065Composition(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
		ids                []string
	}{
		{"md009 md047", "Text \n***\nMore", "Text\n\n***\n\nMore\n", []string{"MD009", "MD047", "MD065"}},
		{"md022", "# H\n***\nText\n", "# H\n\n***\n\nText\n", []string{"MD022", "MD065"}},
		{"md022 shared", "Text\n***\n# H\nText\n", "Text\n\n***\n\n# H\n\nText\n", []string{"MD022", "MD065"}},
		{"md032 after list", "- a\n***\nText\n", "- a\n\n***\n\nText\n", []string{"MD032", "MD065"}},
		{"md032 before list", "Text\n***\n- a\n", "Text\n\n***\n\n- a\n", []string{"MD032", "MD065"}},
		{"md012", "Text\n\n\n***\nMore\n", "Text\n\n***\n\nMore\n", []string{"MD012", "MD065"}},
		{"md023", "Text\n***\n  # H\n\nMore\n", "Text\n\n***\n\n# H\n\nMore\n", []string{"MD022", "MD023", "MD065"}},
		{"all", "#  H\n***\n- a \n\n\n***\nMore", "# H\n\n***\n\n- a\n\n***\n\nMore\n", []string{"MD009", "MD012", "MD019", "MD022", "MD032", "MD047", "MD065"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, out := md065Corrected(t, tc.source, rules.Corrections, tc.ids...)
			if !out.Complete() || string(view.Candidate.Bytes()) != tc.want || len(view.Analysis.Diagnostics) != 0 {
				t.Fatalf("%q %+v", view.Candidate.Bytes(), view.Analysis.Diagnostics)
			}
			again, _ := md065Corrected(t, tc.want, rules.Corrections, tc.ids...)
			if string(again.Candidate.Bytes()) != tc.want || again.ChangingRounds != 0 {
				t.Fatalf("second run %q", again.Candidate.Bytes())
			}
		})
	}
}

// The checker recomputes the facts from the before parse, so a forged line, a
// forged offset or an edit moved to another side's boundary is unsafe and the
// round defers.
func TestMD065CheckerRejectsForgedGroups(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		forge        func(*fix.Group, fix.RoundContext)
	}{
		{"replacement", "> a\n> ***\n> b\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte("> \n") }},
		{"two lines", "Text\n***\nMore\n", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte("\n\n") }},
		{"offset", "Text\n***\nMore\n", func(g *fix.Group, c fix.RoundContext) {
			g.Edits[0].Span, _ = c.Current().Span(0, 0)
		}},
		{"other side", "Text\n***\nMore\n", func(g *fix.Group, c fix.RoundContext) {
			if g.Edits[0].Span.Start() == 5 {
				g.Edits[0].Span, _ = c.Current().Span(9, 9)
			}
		}},
		{"expected bytes", "Text\n***\nMore\n", func(g *fix.Group, c fix.RoundContext) {
			g.Edits[0].Span, _ = c.Current().Span(g.Edits[0].Span.Start(), g.Edits[0].Span.Start()+1)
			g.Edits[0].Expected = []byte(c.Current().Bytes()[g.Edits[0].Span.Start() : g.Edits[0].Span.Start()+1])
			g.Edits[0].Replacement = append([]byte("\n"), g.Edits[0].Expected...)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forged := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := md065Producer(ctx, c)
				for i := range groups {
					tc.forge(&groups[i], c)
				}
				return groups, err
			}
			view, _ := md065Corrected(t, tc.source, forged, "MD065")
			if string(view.Candidate.Bytes()) != tc.source {
				t.Fatalf("%q", view.Candidate.Bytes())
			}
			for _, d := range view.Attempts[0].Decisions {
				if d.Accepted {
					t.Fatal("forged group accepted", d)
				}
			}
		})
	}
}

// Facts claiming one boundary are kept only while they derive the same line.
func TestMD065BlankLineFactsAmbiguousOffset(t *testing.T) {
	source := parser.NewSource([]byte("> a\n> ***\n> ***\n"))
	span := func(start, end int) parser.Span {
		s, err := source.Span(start, end)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	fact := func(point int, before bool, prefixEnd int) parser.ThematicSpacingFact {
		return parser.ThematicSpacingFact{Point: span(point, point), Before: before, At: span(10, 10), Prefix: span(10, prefixEnd), Ending: span(9, 10), Neighbour: span(4, 10)}
	}
	free := func(context.Context, int64) error { return nil }
	for _, tc := range []struct {
		name  string
		facts []parser.ThematicSpacingFact
		want  int
	}{
		{"identical", []parser.ThematicSpacingFact{fact(9, false, 12), fact(12, true, 12)}, 2},
		{"different", []parser.ThematicSpacingFact{fact(9, false, 12), fact(12, true, 10)}, 0},
		{"different then identical", []parser.ThematicSpacingFact{fact(9, false, 12), fact(12, true, 10), fact(12, true, 12)}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := md065.BlankLineFacts(context.Background(), source, tc.facts, free)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == 0 {
				if len(got) != 0 {
					t.Fatal("ambiguous boundary kept", got)
				}
				return
			}
			b, ok := got[10]
			if len(got) != 1 || !ok || len(b.Facts()) != tc.want || string(b.Line()) != ">\n" {
				t.Fatal(got)
			}
		})
	}
}
