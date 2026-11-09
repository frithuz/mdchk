package rules

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func numberingCorrected(t *testing.T, text, style string, recognize bool, producer fix.Producer, ids ...string) (fix.FileView, fix.Validated) {
	t.Helper()
	r := registry(t, Registrations())
	settings := spacingSettings(ids...)
	on := true
	settings["MD029"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"style": style}}
	selection, err := r.SelectRules(settings, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	fm, _ := parser.NewFrontmatterOptions(recognize, nil)
	plan, err := selection.Plan(engine.DocumentContext{Flavor: "commonmark", Frontmatter: fm}, nil)
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

func numberingFindings(view fix.FileView) []result.Diagnostic {
	var out []result.Diagnostic
	for _, d := range view.Analysis.Diagnostics {
		if d.Rule == "MD029" {
			out = append(out, d)
		}
	}
	return out
}

func TestMD029Corrections(t *testing.T) {
	for _, tc := range []struct {
		name, style, source, want string
		recognize                 bool
		// remaining lists the marker text of each MD029 finding left after the fix.
		remaining []string
	}{
		{"ordered", "ordered", "1. a\n3. b\n", "1. a\n2. b\n", false, nil},
		{"one", "one", "1. a\n2. b\n3. c\n", "1. a\n1. b\n1. c\n", false, nil},
		{"one parenthesis leading zeros", "one", "01) a\n001) b\n", "01) a\n001) b\n", false, nil},
		{"one parenthesis renumbered", "one", "1) a\n2) b\n", "1) a\n1) b\n", false, nil},
		{"zero start", "ordered", "0. a\n5. b\n", "0. a\n1. b\n", false, nil},
		{"leading zeros", "ordered", "0009. x\n0011. y", "0009. x\n10. y", false, nil},
		{"parenthesis", "ordered", "1) a\n3) b\n", "1) a\n2) b\n", false, nil},
		{"crlf unicode", "ordered", "1. é 日本\r\n3. é 日本\r\n", "1. é 日本\r\n2. é 日本\r\n", false, nil},
		{"block quote", "ordered", "> 1. a\n> 3. b\n", "> 1. a\n> 2. b\n", false, nil},
		{"under bullet", "ordered", "- x\n\n  1. a\n  3. b\n", "- x\n\n  1. a\n  2. b\n", false, nil},
		{"under ordered", "ordered", "1. x\n\n   1. a\n   3. b\n", "1. x\n\n   1. a\n   2. b\n", false, nil},
		{"task payload", "ordered", "1. [ ] a\n3. [x] b\n", "1. [ ] a\n2. [x] b\n", false, nil},
		{"grows into padding", "ordered", "9. a\n9.  b\n    more\n", "9. a\n10. b\n    more\n", false, nil},
		{"shrinks into padding", "ordered", "8. a\n10. b\n    more\n", "8. a\n9.  b\n    more\n", false, nil},
		{"padding too short", "ordered", "9. a\n9. b\n   more\n", "9. a\n9. b\n   more\n", false, []string{"9."}},
		{"tab padding", "ordered", "9. a\n9.\tb\n", "9. a\n9.\tb\n", false, []string{"9."}},
		{"empty content", "ordered", "9. a\n9.\n", "9. a\n9.\n", false, []string{"9."}},
		{"ten digits", "ordered", "999999999. a\n999999999. b\n", "999999999. a\n999999999. b\n", false, []string{"999999999."}},
		{"deferred and corrected", "ordered", "9. a\n9. b\n12. c\n", "9. a\n9. b\n11. c\n", false, []string{"9."}},
		{"protected content", "ordered", "```\n1. a\n3. b\n```\n\n<div>\n1. a\n3. b\n</div>\n", "```\n1. a\n3. b\n```\n\n<div>\n1. a\n3. b\n</div>\n", false, nil},
		{"recognized front matter", "ordered", "---\n1. a\n3. b\n---\n\n1. x\n3. y\n", "---\n1. a\n3. b\n---\n\n1. x\n2. y\n", true, nil},
		{"unrecognized front matter", "ordered", "+++\n1. a\n3. b\n+++\n", "+++\n1. a\n3. b\n+++\n", false, []string{"3."}},
		{"directive interval", "ordered", "<!-- mdchk:disable MD029 -->\n1. a\n3. b\n", "<!-- mdchk:disable MD029 -->\n1. a\n3. b\n", false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, out := numberingCorrected(t, tc.source, tc.style, tc.recognize, Corrections, "MD029")
			if !out.Complete() || string(view.Candidate.Bytes()) != tc.want {
				t.Fatalf("got %q complete %v attempts %+v", view.Candidate.Bytes(), out.Complete(), view.Attempts)
			}
			remaining := numberingFindings(view)
			if len(remaining) != len(tc.remaining) {
				t.Fatal("remaining", remaining)
			}
			for i, d := range remaining {
				if d.FixAvailable || tc.want[d.ByteRange.Start:d.ByteRange.End] != tc.remaining[i] {
					t.Fatal("remaining", d)
				}
			}
			// A second run on the output makes no change.
			again, _ := numberingCorrected(t, tc.want, tc.style, tc.recognize, Corrections, "MD029")
			if string(again.Candidate.Bytes()) != tc.want || again.ChangingRounds != 0 {
				t.Fatalf("second run changed %q", again.Candidate.Bytes())
			}
		})
	}
}

func TestMD029EditSpansCoverOnlyDigits(t *testing.T) {
	for _, tc := range []struct {
		name, source         string
		start, end           int
		expected, replacment string
	}{
		{"leading zeros", "0009. x\n0011. y", 8, 12, "0011", "10"},
		{"same width", "1) a\n3) b\n", 5, 6, "3", "2"},
		{"container prefixes", "> - 1. a\n>   3. b\n", 13, 14, "3", "2"},
		{"grows into padding", "9. a\n9.  b\n    more\n", 5, 9, "9.  ", "10. "},
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
			numberingCorrected(t, tc.source, "ordered", false, producer, "MD029")
			if len(groups) != 1 || len(groups[0].Edits) != 1 {
				t.Fatal(groups)
			}
			e := groups[0].Edits[0]
			if e.Span.Start() != tc.start || e.Span.End() != tc.end || string(e.Expected) != tc.expected || string(e.Replacement) != tc.replacment {
				t.Fatalf("%d %d %q %q", e.Span.Start(), e.Span.End(), e.Expected, e.Replacement)
			}
		})
	}
}

func TestMD029CheckerRejectsForgedChanges(t *testing.T) {
	forge := func(start, end int, replacement string) func(*fix.Group, fix.RoundContext) {
		return func(g *fix.Group, c fix.RoundContext) {
			s, _ := c.Current().Span(start, end)
			text, _ := c.Current().Text(s)
			g.Edits[0] = fix.Edit{Span: s, Expected: []byte(text), Replacement: []byte(replacement)}
		}
	}
	for _, tc := range []struct {
		name, source string
		alter        func(*fix.Group, fix.RoundContext)
	}{
		// A non-1 start cannot interrupt a paragraph, so the list would disappear.
		{"interrupts a paragraph", "text\n1. a\n1. b\n", forge(5, 6, "2")},
		{"wrong number", "1. a\n3. b\n", forge(5, 6, "4")},
		{"leading zero", "1. a\n3. b\n", forge(5, 6, "02")},
		{"delimiter", "1. a\n3. b\n", forge(5, 7, "2)")},
		{"payload", "1. a\n3. b\n", forge(5, 9, "2. c")},
		{"foreign anchor", "1. a\n3. b\n", func(g *fix.Group, c fix.RoundContext) { g.Anchor, _ = c.Current().Span(0, 2) }},
		{"two edits", "1. a\n3. b\n", func(g *fix.Group, c fix.RoundContext) {
			s, _ := c.Current().Span(3, 4)
			g.Edits = append(g.Edits, fix.Edit{Span: s, Expected: []byte("a"), Replacement: []byte("b")})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			altered := false
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := Corrections(ctx, c)
				if len(groups) == 0 && err == nil {
					// Forge a group for a list the rule does not flag.
					s, _ := c.Current().Span(10, 12)
					groups = []fix.Group{{ID: "MD029-prefix-forged", Rule: "MD029", Provenance: "forged", Anchor: s, Edits: []fix.Edit{{}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}}}}
				}
				for i := range groups {
					if groups[i].Rule == "MD029" {
						tc.alter(&groups[i], c)
						altered = true
						break
					}
				}
				return groups, err
			}
			view, out := numberingCorrected(t, tc.source, "ordered", false, producer, "MD029")
			if !altered || !out.Complete() || string(view.Candidate.Bytes()) != tc.source || len(view.Attempts) == 0 {
				t.Fatalf("forged correction accepted: %q %+v", view.Candidate.Bytes(), view.Attempts)
			}
		})
	}
}

func TestMD029Composition(t *testing.T) {
	source := "#  Title\n\n1. a \n3. b\n\n## End  ##"
	want := "# Title\n\n1. a\n2. b\n\n## End ##\n"
	ids := []string{"MD009", "MD019", "MD021", "MD029", "MD047"}
	view, out := numberingCorrected(t, source, "ordered", false, Corrections, ids...)
	if !out.Complete() || string(view.Candidate.Bytes()) != want || len(view.Analysis.Diagnostics) != 0 {
		t.Fatalf("%q complete %v diagnostics %+v attempts %+v", view.Candidate.Bytes(), out.Complete(), view.Analysis.Diagnostics, view.Attempts)
	}
	again, _ := numberingCorrected(t, want, "ordered", false, Corrections, ids...)
	if string(again.Candidate.Bytes()) != want || again.ChangingRounds != 0 || len(again.Analysis.Diagnostics) != 0 {
		t.Fatalf("second run %q %+v", again.Candidate.Bytes(), again.Analysis.Diagnostics)
	}
}

func TestMD029SharesRoundWithMD047AtEOF(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"1. a\n3. b", "1. a\n2. b\n"},
		{"1. a\r\n0011. b", "1. a\r\n2. b\r\n"},
	} {
		view, out := numberingCorrected(t, tc.source, "ordered", false, Corrections, "MD029", "MD047")
		if !out.Complete() || string(view.Candidate.Bytes()) != tc.want || view.ChangingRounds != 1 || len(view.Analysis.Diagnostics) != 0 {
			t.Fatalf("%q rounds %d %+v", view.Candidate.Bytes(), view.ChangingRounds, view.Analysis.Diagnostics)
		}
	}
}
