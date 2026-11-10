package md007_test

import (
	"context"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/md007"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

// md007FixSettings enables MD007 with its options plus any other named rules.
func md007FixSettings(options engine.Options, ids ...string) engine.Settings {
	s := ruletest.Settings(append(ids, "MD007")...)
	on := true
	if options == nil {
		options = engine.Options{}
	}
	s["MD007"] = engine.RuleSetting{Enabled: &on, Options: options}
	return s
}

func md007Corrected(t *testing.T, text string, s engine.Settings, dc engine.DocumentContext, producer fix.Producer) (fix.FileView, fix.Validated) {
	t.Helper()
	r := registry(t, rules.Registrations())
	selection, err := r.SelectRules(s, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	if dc.Flavor == "" {
		dc.Flavor = "commonmark"
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

// md007Analysis returns the MD007 findings of one analysis.
func md007Analysis(t *testing.T, text string, s engine.Settings, dc engine.DocumentContext) []result.Diagnostic {
	t.Helper()
	if dc.Flavor == "" {
		dc.Flavor = "commonmark"
	}
	out := analyzeHeadings(t, registry(t, rules.Registrations()), text, s, dc, engine.RunOptions{})
	var found []result.Diagnostic
	for _, d := range out.Diagnostics {
		if d.Rule == "MD007" {
			found = append(found, d)
		}
	}
	return found
}

func TestMD007Corrections(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
		options            engine.Options
		rounds             int
	}{
		{"over-indented child", "- a\n   - b\n     more\n", "- a\n  - b\n    more\n", nil, 1},
		{"under-indented child indent four", "- a\n  - b\n", "- a\n    - b\n", engine.Options{"indent": 4}, 1},
		{"over-indented child indent four", "- a\n     - b\n", "- a\n    - b\n", engine.Options{"indent": 4}, 1},
		{"start indented root", "- a\n", "  - a\n", engine.Options{"start_indented": true}, 1},
		{"start indented moves children", "- a\n  - b\n", "  - a\n    - b\n", engine.Options{"start_indented": true}, 1},
		{"start indented indent one", "- a\n- b\n", " - a\n - b\n", engine.Options{"indent": 1, "start_indented": true}, 1},
		{"over-indented roots", "  - a\n  - b\n", "- a\n- b\n", engine.Options{"start_indented": false}, 1},
		{"over-indented root indent one", " - a\n", "- a\n", engine.Options{"indent": 1}, 1},
		{"ordered parent", "1. a\n    - b\n", "1. a\n   - b\n", nil, 1},
		{"wide ordered parent", "10. a\n      - b\n", "10. a\n    - b\n", nil, 1},
		{"continuation and sublist", "- a\n   - b\n     more\n\n     para\n\n     - c\n       deep\n", "- a\n  - b\n    more\n\n    para\n\n    - c\n      deep\n", nil, 1},
		{"ordered child moves along", "  - a\n    1. b\n", "- a\n  1. b\n", nil, 1},
		{"nested findings converge", "  - a\n     - b\n", "- a\n  - b\n", nil, 2},
		{"crlf", "- a\r\n   - b\r\n     more\r\n", "- a\r\n  - b\r\n    more\r\n", nil, 1},
		{"unicode", "- 日本\n   - é 👍🏽 文字\n     ｗｉｄｅ\n", "- 日本\n  - é 👍🏽 文字\n    ｗｉｄｅ\n", nil, 1},
		{"eof without newline", "- a\n   - b", "- a\n  - b", nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := md007FixSettings(tc.options)
			before := md007Analysis(t, tc.source, s, engine.DocumentContext{})
			if len(before) == 0 || !before[0].FixAvailable {
				t.Fatal("not eligible", before)
			}
			view, out := md007Corrected(t, tc.source, s, engine.DocumentContext{}, rules.Corrections)
			if !out.Complete() || string(view.Candidate.Bytes()) != tc.want || view.ChangingRounds != tc.rounds {
				t.Fatalf("%q complete %v rounds %d %+v", view.Candidate.Bytes(), out.Complete(), view.ChangingRounds, view.Attempts)
			}
			if after := md007Analysis(t, tc.want, s, engine.DocumentContext{}); len(after) != 0 {
				t.Fatal("finding remains", after)
			}
			again, _ := md007Corrected(t, tc.want, s, engine.DocumentContext{}, rules.Corrections)
			if again.ChangingRounds != 0 || string(again.Candidate.Bytes()) != tc.want {
				t.Fatal("second run changed", again.Candidate.Bytes())
			}
		})
	}
}

func TestMD007CorrectionSpansAndGroups(t *testing.T) {
	text := "- a\n   - b\n\n     more\n"
	found := md007Analysis(t, text, md007FixSettings(nil), engine.DocumentContext{})
	if len(found) != 1 || found[0].ByteRange != (result.ByteRange{Start: 7, End: 8}) || found[0].Range.Start != (result.Position{Line: 2, Column: 4}) || !found[0].FixAvailable {
		t.Fatal(found)
	}
	view, _ := md007Corrected(t, text, md007FixSettings(nil), engine.DocumentContext{}, rules.Corrections)
	decisions := view.Attempts[0].Decisions
	if len(decisions) != 1 || !decisions[0].Accepted {
		t.Fatalf("%+v", decisions)
	}
	g := decisions[0].Group
	if g.ID != "MD007-item-7" || g.Rule != "MD007" || g.Anchor.Start() != 7 || g.Anchor.End() != 8 || len(g.Edits) != 2 {
		t.Fatalf("%+v", g)
	}
	// The blank line is left alone; each moved line loses one leading space.
	for i, start := range []int{4, 12} {
		e := g.Edits[i]
		if e.Span.Start() != start || e.Span.End() != start+1 || string(e.Expected) != " " || len(e.Replacement) != 0 {
			t.Fatalf("%d %+v", i, e)
		}
	}
}

func TestMD007CorrectionDeferrals(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		options      engine.Options
		recognize    bool
	}{
		{"tab indentation", "- a\n \t - b\n", nil, false},
		{"tab in continuation", "- a\n   - b\n\t more\n", nil, false},
		{"tab in nested padding", "- a\n   -\tb\n", nil, false},
		{"blockquote", "> - a\n>    - b\n", nil, false},
		{"blockquote inside item", "- a\n   - b\n\n     > quote\n", nil, false},
		{"lazy continuation", "- a\n   - b\nlazy\n", nil, false},
		{"fenced code", "- a\n   - b\n\n     ```\n     x\n     ```\n", nil, false},
		{"indented code", "- a\n   - b\n\n         code\n", nil, false},
		{"html block", "- a\n   - b\n\n     <div>\n     x\n     </div>\n", nil, false},
		{"directive comment inside item", "- a\n   - b\n\n     <!-- mdchk:disable MD009 -->\n", nil, false},
		{"unrecognized front matter", "---\nx: 1\n---\n\n - body\n", nil, false},
		{"growth past limit", "- a\n  - b\n", engine.Options{"indent": 40}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fm, _ := parser.NewFrontmatterOptions(tc.recognize, nil)
			dc := engine.DocumentContext{Frontmatter: fm}
			s := md007FixSettings(tc.options)
			found := md007Analysis(t, tc.source, s, dc)
			if len(found) == 0 {
				t.Fatal("no finding")
			}
			for _, d := range found {
				if d.FixAvailable {
					t.Fatal("eligible", d)
				}
			}
			view, _ := md007Corrected(t, tc.source, s, dc, rules.Corrections)
			if string(view.Candidate.Bytes()) != tc.source || view.ChangingRounds != 0 {
				t.Fatalf("changed %q", view.Candidate.Bytes())
			}
		})
	}
}

func TestMD007CorrectionDirectiveDisabled(t *testing.T) {
	text := "<!-- mdchk:disable MD007 -->\n- a\n   - b\n"
	s := md007FixSettings(nil)
	out := analyzeHeadings(t, registry(t, rules.Registrations()), text, s, engine.DocumentContext{Flavor: "commonmark"}, engine.RunOptions{})
	if len(out.Diagnostics) != 0 || len(out.SuppressedDiagnostics) != 1 {
		t.Fatal(out)
	}
	view, _ := md007Corrected(t, text, s, engine.DocumentContext{}, rules.Corrections)
	if string(view.Candidate.Bytes()) != text || view.ChangingRounds != 0 {
		t.Fatalf("changed %q", view.Candidate.Bytes())
	}
}

// A shift the analysis allows but the reparse rejects stays deferred.
func TestMD007CorrectionStructureChangeDeferred(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		options      engine.Options
	}{
		{"nested item would become sibling", "- a\n  - b\n", engine.Options{"indent": 1}},
		{"root would become indented code", "- a\n", engine.Options{"indent": 4, "start_indented": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := md007FixSettings(tc.options)
			found := md007Analysis(t, tc.source, s, engine.DocumentContext{})
			if len(found) != 1 || !found[0].FixAvailable {
				t.Fatal(found)
			}
			view, _ := md007Corrected(t, tc.source, s, engine.DocumentContext{}, rules.Corrections)
			if string(view.Candidate.Bytes()) != tc.source || view.ChangingRounds != 0 {
				t.Fatalf("changed %q", view.Candidate.Bytes())
			}
			decisions := view.Attempts[0].Decisions
			if len(decisions) != 1 || decisions[0].Accepted || decisions[0].Group.Rule != "MD007" {
				t.Fatalf("%+v", decisions)
			}
		})
	}
}

// MD009 suffixes go first; the MD007 shift follows in the next round of the same run.
func TestMD007CorrectionComposesWithOtherRules(t *testing.T) {
	text := "#  Title #\n\n- a \n   - b\n     more\n- c"
	want := "# Title #\n\n- a\n  - b\n    more\n- c\n"
	s := md007FixSettings(nil, "MD009", "MD019", "MD021", "MD047")
	view, out := md007Corrected(t, text, s, engine.DocumentContext{}, rules.Corrections)
	if !out.Complete() || string(view.Candidate.Bytes()) != want || view.ChangingRounds != 2 {
		t.Fatalf("%q complete %v rounds %d %+v", view.Candidate.Bytes(), out.Complete(), view.ChangingRounds, view.Attempts)
	}
	again, _ := md007Corrected(t, want, s, engine.DocumentContext{}, rules.Corrections)
	if again.ChangingRounds != 0 {
		t.Fatal("second run changed", again.Candidate.Bytes())
	}
}

// A tampered group must fail the independent checker whatever its producer claims.
func TestMD007CheckerRejectsTamperedGroups(t *testing.T) {
	md007Producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
		analysis, err := c.BoundedAnalysis(ctx)
		if err != nil {
			return nil, err
		}
		var findings []result.Diagnostic
		for _, d := range analysis.Diagnostics {
			if d.Rule == "MD007" {
				findings = append(findings, d)
			}
		}
		return md007.Correction(ctx, c, findings)
	}
	tamper := func(rewrite func(*testing.T, fix.RoundContext, fix.Group) fix.Group) fix.Producer {
		return func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
			groups, err := md007Producer(ctx, c)
			if err != nil {
				return nil, err
			}
			for i := range groups {
				groups[i] = rewrite(t, c, groups[i])
			}
			return groups, nil
		}
	}
	text := "- a\n   - b\n     more\n"
	for _, tc := range []struct {
		name    string
		rewrite func(*testing.T, fix.RoundContext, fix.Group) fix.Group
	}{
		{"dropped continuation edit", func(_ *testing.T, _ fix.RoundContext, g fix.Group) fix.Group {
			g.Edits = g.Edits[:1]
			return g
		}},
		{"wrong replacement", func(_ *testing.T, _ fix.RoundContext, g fix.Group) fix.Group {
			g.Edits[1].Replacement = []byte("x")
			return g
		}},
		{"edit outside indentation", func(t *testing.T, c fix.RoundContext, g fix.Group) fix.Group {
			t.Helper()
			span, err := c.Current().Span(g.Edits[1].Span.Start()+5, g.Edits[1].Span.Start()+6)
			if err != nil {
				t.Fatal(err)
			}
			text, _ := c.Current().Text(span)
			g.Edits[1].Span, g.Edits[1].Expected = span, []byte(text)
			return g
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, _ := md007Corrected(t, text, md007FixSettings(nil), engine.DocumentContext{}, tamper(tc.rewrite))
			if string(view.Candidate.Bytes()) != text {
				t.Fatalf("tampered candidate retained: %q", view.Candidate.Bytes())
			}
			decisions := view.Attempts[0].Decisions
			if len(decisions) != 1 || decisions[0].Accepted || len(decisions[0].Reasons) == 0 {
				t.Fatalf("%+v", decisions)
			}
		})
	}
	view, _ := md007Corrected(t, text, md007FixSettings(nil), engine.DocumentContext{}, md007Producer)
	if got := string(view.Candidate.Bytes()); got != strings.Replace(strings.Replace(text, "   -", "  -", 1), "     more", "    more", 1) {
		t.Fatalf("%q", got)
	}
}
