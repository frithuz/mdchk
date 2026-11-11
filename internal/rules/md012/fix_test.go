package md012_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/md012"
)

// md012Corrected validates one document under MD012 with the given maximum and
// any other enabled rules.
func md012Corrected(t *testing.T, text string, maximum int, producer fix.Producer, enable ...string) fix.FileView {
	t.Helper()
	r := registry(t, rules.Registrations())
	s := md012Settings()
	s["MD012"] = engine.RuleSetting{Options: engine.Options{"maximum": maximum}}
	on := true
	for _, id := range enable {
		s[id] = engine.RuleSetting{Enabled: &on}
	}
	selection, err := r.SelectRules(s, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	v, err := fix.NewValidator(r, rules.CorrectionCheckers())
	if err != nil {
		t.Fatal(err)
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	out, err := v.Validate(context.Background(), []fix.Target{{Path: "doc.md", Source: parser.NewSource([]byte(text)), Plan: plan, Produce: producer}}, fix.ValidationOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	view, ok := out.File("doc.md")
	if !ok {
		t.Fatal("missing file")
	}
	return view
}

// md012Producer runs only MD012's part of Corrections.
func md012Producer(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
	analysis, err := c.BoundedAnalysis(ctx)
	if err != nil {
		return nil, err
	}
	var findings []result.Diagnostic
	for _, d := range analysis.Diagnostics {
		if d.Rule == "MD012" {
			findings = append(findings, d)
		}
	}
	return md012.Correction(ctx, c, findings)
}

func TestMD012ProducerGroups(t *testing.T) {
	view := md012Corrected(t, "a\n\n\n\nb\n\n\nc\n", 1, md012Producer)
	if got := string(view.Candidate.Bytes()); got != "a\n\nb\n\nc\n" {
		t.Fatalf("%q", got)
	}
	decisions := view.Attempts[0].Decisions
	if len(decisions) != 2 {
		t.Fatal(decisions)
	}
	for i, want := range []struct {
		id    string
		spans [][2]int
	}{{"MD012-line-3", [][2]int{{3, 4}, {4, 5}}}, {"MD012-line-7", [][2]int{{8, 9}}}} {
		d := decisions[i]
		if !d.Accepted || d.Group.ID != want.id || d.Group.Rule != "MD012" || len(d.Group.Edits) != len(want.spans) {
			t.Fatalf("%+v", d)
		}
		for j, e := range d.Group.Edits {
			if e.Span.Start() != want.spans[j][0] || e.Span.End() != want.spans[j][1] || string(e.Expected) != "\n" || len(e.Replacement) != 0 {
				t.Fatalf("%+v", e)
			}
		}
		if d.Group.Anchor.Start() != want.spans[0][0] || d.Group.Anchor.End() != want.spans[len(want.spans)-1][1] {
			t.Fatal(d.Group.Anchor)
		}
		kinds := map[fix.RequirementKind]bool{}
		for _, r := range d.Group.Requirements {
			kinds[r.Kind] = true
		}
		if !kinds[fix.Meaning] || !kinds[fix.ProtectedSyntax] || !kinds[fix.DirectiveStructure] {
			t.Fatal(d.Group.Requirements)
		}
	}
}

func TestMD012Candidates(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
		maximum            int
		enable             []string
	}{
		{"maximum one", "One\n\n\n\nTwo\n", "One\n\nTwo\n", 1, nil},
		{"maximum two", "a\n\n\n\n\nb\n", "a\n\n\nb\n", 2, nil},
		{"maximum zero heading", "# H\n\n\npara\n", "# H\npara\n", 0, nil},
		{"maximum zero merge", "One\n\nTwo\n", "One\n\nTwo\n", 0, nil},
		{"maximum large", "a\n\n\n\n\nb\n", "a\n\n\n\n\nb\n", 1 << 20, nil},
		{"crlf", "One\r\n\r\n\r\nTwo\r\n", "One\r\n\r\nTwo\r\n", 1, nil},
		{"cr", "One\r\r\r\rTwo\r", "One\r\rTwo\r", 1, nil},
		{"mixed", "a\r\n\n\r\r\nb\r\n\r\n\n\nc\n", "a\r\n\nb\r\n\r\nc\n", 1, nil},
		{"non-ascii", "héllo 日本\n\n\n\nwörld ✓\n", "héllo 日本\n\nwörld ✓\n", 1, nil},
		{"whitespace only", "a\n \n\t\n  \nb\n", "a\n \nb\n", 1, nil},
		{"eof whitespace", "a\n\n \t", "a\n\n", 1, nil},
		{"quote", "> a\n>\n> \t\n>\n> b\n", "> a\n>\n> b\n", 1, nil},
		{"quote prefix mismatch", "> a\n>\n>\n\n\nb\n", "> a\n>\n>\n\n\nb\n", 1, nil},
		{"item indent", "- a\n\n    \n  b\n", "- a\n\n  b\n", 1, nil},
		{"list loose", "- a\n\n\n\n- b\n", "- a\n\n- b\n", 1, nil},
		{"list tightness", "- a\n\n\n- b\n", "- a\n\n\n- b\n", 0, nil},
		{"list boundary", "- a\n\n\n  b\n", "- a\n\n\n  b\n", 0, nil},
		{"lazy", "> a\nb\n\n\n\nc\n", "> a\nb\n\nc\n", 1, nil},
		{"fenced", "```\n\n\n\n```\n\n\n\nend\n", "```\n\n\n\n```\n\nend\n", 1, nil},
		{"indented", "    a\n\n\n\n    b\n\n\nend\n", "    a\n\n\n\n    b\n\nend\n", 1, nil},
		{"html", "<script>\n\n\n\n</script>\n\n\n\nend\n", "<script>\n\n\n\n</script>\n\nend\n", 1, nil},
		{"comment", "<!--\n\n\n\n-->\n\n\n\nend\n", "<!--\n\n\n\n-->\n\nend\n", 1, nil},
		{"new md022", "# H\n\n\npara\n", "# H\n\n\npara\n", 0, []string{"MD022"}},
		{"new md031", "x\n\n\n```\ncode\n```\n", "x\n\n\n```\ncode\n```\n", 0, []string{"MD031"}},
		{"new md032", "para\n\n\n- a\n", "para\n\n\n- a\n", 0, []string{"MD032"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := md012Corrected(t, tc.source, tc.maximum, rules.Corrections, tc.enable...)
			if got := string(view.Candidate.Bytes()); got != tc.want {
				t.Fatalf("%q, want %q", got, tc.want)
			}
			again := md012Corrected(t, tc.want, tc.maximum, rules.Corrections, tc.enable...)
			if got := string(again.Candidate.Bytes()); got != tc.want {
				t.Fatalf("second run %q", got)
			}
		})
	}
}

func TestMD012PrefixMismatchDefers(t *testing.T) {
	view := md012Corrected(t, "> a\n>\n>\n\n\nb\n", 1, md012Producer)
	decisions := view.Attempts[0].Decisions
	if len(decisions) != 1 || decisions[0].Accepted || len(decisions[0].Group.Edits) != 0 || len(decisions[0].Reasons) != 1 || decisions[0].Reasons[0].Code != "prefix-mismatch" {
		t.Fatalf("%+v", decisions)
	}
}

func TestMD012ComposesWithMD009AndMD047(t *testing.T) {
	source := "a \n\n  \n\n\nb"
	view := md012Corrected(t, source, 1, rules.Corrections, "MD009", "MD047")
	if got := string(view.Candidate.Bytes()); got != "a\n\nb\n" {
		t.Fatalf("%q", got)
	}
	// MD009 suffixes go first; MD012 waits for a round without them.
	if len(view.Attempts) < 2 {
		t.Fatal(view.Attempts)
	}
	for _, d := range view.Attempts[0].Decisions {
		if d.Group.Rule == "MD012" {
			t.Fatalf("MD012 shared a round with MD009: %+v", d)
		}
	}
	again := md012Corrected(t, "a\n\nb\n", 1, rules.Corrections, "MD009", "MD047")
	if string(again.Candidate.Bytes()) != "a\n\nb\n" {
		t.Fatal(string(again.Candidate.Bytes()))
	}
}

func TestMD012CheckerRejectsTamperedGroups(t *testing.T) {
	tamper := func(rewrite func(*testing.T, fix.RoundContext, fix.Group) fix.Group) fix.Producer {
		return func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
			groups, err := md012Producer(ctx, c)
			if err != nil {
				return nil, err
			}
			for i := range groups {
				groups[i] = rewrite(t, c, groups[i])
			}
			return groups, nil
		}
	}
	for _, tc := range []struct {
		name, source string
		rewrite      func(*testing.T, fix.RoundContext, fix.Group) fix.Group
	}{
		{"replacement", "a\n\n\n\nb\n", func(_ *testing.T, _ fix.RoundContext, g fix.Group) fix.Group {
			g.Edits[0].Replacement = []byte("x\n")
			return g
		}},
		{"dropped edit", "a\n\n\n\nb\n", func(_ *testing.T, _ fix.RoundContext, g fix.Group) fix.Group {
			g.Edits = g.Edits[:1]
			return g
		}},
		{"kept line removed", "a\n\n\nb\n", func(t *testing.T, c fix.RoundContext, g fix.Group) fix.Group {
			t.Helper()
			span, err := c.Current().Span(g.Edits[0].Span.Start()-1, g.Edits[0].Span.Start())
			if err != nil {
				t.Fatal(err)
			}
			g.Edits[0].Span = span
			return g
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := md012Corrected(t, tc.source, 1, tamper(tc.rewrite))
			if string(view.Candidate.Bytes()) != tc.source {
				t.Fatalf("tampered candidate retained: %q", view.Candidate.Bytes())
			}
			decisions := view.Attempts[0].Decisions
			if len(decisions) != 1 || decisions[0].Accepted {
				t.Fatalf("%+v", decisions)
			}
		})
	}
}
