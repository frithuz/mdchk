package md085_test

import (
	"context"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/md085"
)

// md085Fixed validates every production correction of text in flavor with
// MD085 and the extra rules enabled.
func md085Fixed(t *testing.T, flavor, text string, producer fix.Producer, extra ...string) fix.FileView {
	t.Helper()
	r := registry(t, rules.Registrations())
	s := md085Settings()
	on := true
	for _, id := range extra {
		s[id] = engine.RuleSetting{Enabled: &on}
	}
	selection, err := r.SelectRules(s, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: flavor}, nil)
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
	return view
}

// md085Producer runs only MD085's part of Corrections.
func md085Producer(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
	analysis, err := c.BoundedAnalysis(ctx)
	if err != nil {
		return nil, err
	}
	var findings []result.Diagnostic
	for _, d := range analysis.Diagnostics {
		if d.Rule == "MD085" {
			findings = append(findings, d)
		}
	}
	return md085.Correction(ctx, c, findings)
}

func md085Kinds(t *testing.T, flavor, text string) string {
	t.Helper()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithOptions(context.Background(), parser.NewSource([]byte(text)), budget, parser.ParseOptions{GFM: flavor == "gfm"})
	if parsed.Document == nil || parsed.Status != parser.Complete {
		t.Fatal("parse", parsed.Status)
	}
	var kinds []string
	var walk func(*parser.Node)
	walk = func(n *parser.Node) {
		kinds = append(kinds, string(n.Kind()))
		for _, c := range n.Children() {
			walk(c)
		}
	}
	walk(parsed.Document.Root())
	return strings.Join(kinds, ",")
}

func TestMD085ProducerGroups(t *testing.T) {
	view := md085Fixed(t, "commonmark", "a\n  b\n", md085Producer)
	if got := string(view.Candidate.Bytes()); got != "a\nb\n" {
		t.Fatalf("%q", got)
	}
	decisions := view.Attempts[0].Decisions
	if len(decisions) != 1 {
		t.Fatal(decisions)
	}
	d := decisions[0]
	if !d.Accepted || d.Group.ID != "MD085-byte-2" || d.Group.Rule != "MD085" || d.Group.Anchor.Start() != 2 || d.Group.Anchor.End() != 4 || len(d.Group.Edits) != 1 || d.Group.Edits[0].Span != d.Group.Anchor || string(d.Group.Edits[0].Expected) != "  " || len(d.Group.Edits[0].Replacement) != 0 {
		t.Fatalf("%+v", d)
	}
	kinds := map[fix.RequirementKind]bool{}
	for _, r := range d.Group.Requirements {
		kinds[r.Kind] = true
	}
	if !kinds[fix.Meaning] || !kinds[fix.ProtectedSyntax] || !kinds[fix.DirectiveStructure] {
		t.Fatal(d.Group.Requirements)
	}
}

func TestMD085CorrectsContinuationIndentation(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
	}{
		{"space", "a\n  b\n", "a\nb\n"},
		{"tab", "a\n\tb\n", "a\nb\n"},
		{"mixed", "a\n \t b\n", "a\nb\n"},
		{"crossing tab", "  a\n\tb\n", "  a\nb\n"},
		{"tab phase", "   a\n \tb\n", "   a\n b\n"},
		{"relative", "   a\n b\n   c\n    d\n", "   a\n b\n   c\n   d\n"},
		{"several", "a\n  b\n    c\n", "a\nb\nc\n"},
		{"crlf", "a\r\n \tb\r\n", "a\r\nb\r\n"},
		{"cr", "a\r  b\r", "a\rb\r"},
		{"mixed endings", "a\r\n  b\r  c\n", "a\r\nb\rc\n"},
		{"unicode", "é\n  ö\n", "é\nö\n"},
		{"astral", "雪\n  😀 b\n", "雪\n😀 b\n"},
		{"after hard break", "a  \n  b\n  c\n", "a  \n  b\nc\n"},
		{"after backslash break", "a\\\r\n  b\r\n  c\r\n", "a\\\r\n  b\r\nc\r\n"},
		{"after root block quote", "> a\n\nb\n  c\n", "> a\n\nb\nc\n"},
		{"formatting", "a\n  **b** [c](/u)\n", "a\n**b** [c](/u)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, flavor := range []string{"commonmark", "gfm"} {
				view := md085Fixed(t, flavor, tc.source, rules.Corrections)
				if got := string(view.Candidate.Bytes()); got != tc.want {
					t.Fatalf("%s: got %q want %q", flavor, got, tc.want)
				}
				if md085Kinds(t, flavor, tc.source) != md085Kinds(t, flavor, tc.want) {
					t.Fatal("structure changed")
				}
				for _, d := range view.Analysis.Diagnostics {
					if d.Rule == "MD085" {
						t.Fatal(d)
					}
				}
			}
		})
	}
}

func TestMD085LeavesContainersAndCode(t *testing.T) {
	for _, tc := range []struct {
		name, source string
	}{
		{"block quote", "> a\n>   b\n"},
		{"list item", "- a\n    b\n"},
		{"nested", "> - a\n>     b\n"},
		{"lazy quote", "> a\n  b\n"},
		{"lazy list", "- a\n  b\n"},
		{"fenced code", "```\na\n  b\n```\n"},
		{"indented code", "    a\n      b\n"},
		{"html block", "<div>\na\n  b\n</div>\n"},
		{"code span", "a `x\n  payload`\n"},
		{"setext", "a\n  b\n---\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := md085Fixed(t, "commonmark", tc.source, rules.Corrections)
			if got := string(view.Candidate.Bytes()); got != tc.source {
				t.Fatalf("%q", got)
			}
			for _, d := range view.Analysis.Diagnostics {
				if d.Rule == "MD085" {
					t.Fatal(d)
				}
			}
		})
	}
}

func TestMD085DefersWhenStructureWouldChange(t *testing.T) {
	for _, tc := range []struct {
		name, flavor, source string
	}{
		{"bullet", "commonmark", "a\n    - b\n"},
		{"ordered", "commonmark", "a\n    1. b\n"},
		{"heading", "commonmark", "a\n    # b\n"},
		{"block quote", "commonmark", "a\n    > b\n"},
		{"fence", "commonmark", "a\n     ```\n"},
		{"thematic break", "commonmark", "a\n    ***\n"},
		{"setext underline", "commonmark", "a\n    ===\n"},
		{"table delimiter", "gfm", "a | b\n    --- | ---\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := md085Fixed(t, tc.flavor, tc.source, rules.Corrections)
			if got := string(view.Candidate.Bytes()); got != tc.source {
				t.Fatalf("%q", got)
			}
			found := 0
			for _, d := range view.Analysis.Diagnostics {
				if d.Rule == "MD085" {
					found++
					if d.FixAvailable {
						t.Fatal(d)
					}
				}
			}
			if found != 1 {
				t.Fatal(view.Analysis.Diagnostics)
			}
		})
	}
	// An unsafe line does not hold back a safe one.
	view := md085Fixed(t, "commonmark", "a\n  b\n    - c\n", rules.Corrections)
	if got := string(view.Candidate.Bytes()); got != "a\nb\n    - c\n" {
		t.Fatalf("%q", got)
	}
	if len(view.Analysis.Diagnostics) != 1 || view.Analysis.Diagnostics[0].FixAvailable {
		t.Fatal(view.Analysis.Diagnostics)
	}
}

func TestMD085RespectsDirectives(t *testing.T) {
	source := "<!-- mdchk:disable MD085 -->\n\na\n  b\n\n<!-- mdchk:enable MD085 -->\n\nc\n  d\n"
	view := md085Fixed(t, "commonmark", source, rules.Corrections)
	if got, want := string(view.Candidate.Bytes()), "<!-- mdchk:disable MD085 -->\n\na\n  b\n\n<!-- mdchk:enable MD085 -->\n\nc\nd\n"; got != want {
		t.Fatalf("%q", got)
	}
	for _, d := range view.Analysis.Diagnostics {
		if d.Rule == "MD085" {
			t.Fatal(d)
		}
	}
}

func TestMD085ComposesWithMD009AndMD047(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"a \n  b", "a\nb\n"},
		{"a\n  b \n  c", "a\nb\nc\n"},
		{"a\r\n\tb\t\r\n  c", "a\r\nb\r\nc\r\n"},
	} {
		view := md085Fixed(t, "commonmark", tc.source, rules.Corrections, "MD009", "MD047")
		if got := string(view.Candidate.Bytes()); got != tc.want {
			t.Fatalf("%q: got %q want %q", tc.source, got, tc.want)
		}
		again := md085Fixed(t, "commonmark", tc.want, rules.Corrections, "MD009", "MD047")
		if string(again.Candidate.Bytes()) != tc.want || len(again.Analysis.Diagnostics) != 0 {
			t.Fatal("second run changed", again.Analysis.Diagnostics)
		}
	}
}

func TestMD085WaitsWithMD010(t *testing.T) {
	view := md085Fixed(t, "commonmark", "a\tb\n  c\n", rules.Corrections, "MD010")
	if got := string(view.Candidate.Bytes()); got != "a    b\nc\n" {
		t.Fatalf("%q", got)
	}
}

func TestMD085CheckerRejectsTamperedGroups(t *testing.T) {
	tamper := func(rewrite func(fix.Group) fix.Group) fix.Producer {
		return func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
			groups, err := md085Producer(ctx, c)
			if err != nil {
				return nil, err
			}
			for i := range groups {
				groups[i] = rewrite(groups[i])
			}
			return groups, nil
		}
	}
	for _, tc := range []struct {
		name    string
		rewrite func(fix.Group) fix.Group
	}{
		{"kept space", func(g fix.Group) fix.Group { g.Edits[0].Replacement = []byte(" "); return g }},
		{"text replacement", func(g fix.Group) fix.Group { g.Edits[0].Replacement = []byte("x"); return g }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "a\n  b\n"
			view := md085Fixed(t, "commonmark", source, tamper(tc.rewrite))
			if string(view.Candidate.Bytes()) != source {
				t.Fatalf("tampered candidate retained: %q", view.Candidate.Bytes())
			}
			decisions := view.Attempts[0].Decisions
			if len(decisions) != 1 || decisions[0].Accepted || len(decisions[0].Reasons) == 0 {
				t.Fatalf("%+v", decisions)
			}
		})
	}
}
