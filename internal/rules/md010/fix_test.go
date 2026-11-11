package md010_test

import (
	"context"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/md010"
)

// md010Fixed validates every production correction of text in flavor with
// MD010 options and the extra rules enabled.
func md010Fixed(t *testing.T, flavor, text string, options engine.Options, producer fix.Producer, extra ...string) fix.FileView {
	t.Helper()
	r := registry(t, rules.Registrations())
	s := md010Settings()
	s["MD010"] = engine.RuleSetting{Options: options}
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

// md010Producer runs only MD010's part of Corrections.
func md010Producer(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
	analysis, err := c.BoundedAnalysis(ctx)
	if err != nil {
		return nil, err
	}
	var findings []result.Diagnostic
	for _, d := range analysis.Diagnostics {
		if d.Rule == "MD010" {
			findings = append(findings, d)
		}
	}
	return md010.Correction(ctx, c, findings)
}

func md010Options(spaces int, code bool) engine.Options {
	return engine.Options{"spaces_per_tab": spaces, "code_blocks": code}
}

func md010Kinds(t *testing.T, flavor, text string) string {
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

func TestMD010ProducerGroups(t *testing.T) {
	for _, spaces := range []int{1, 4, parser.MaxTabSpaces} {
		view := md010Fixed(t, "commonmark", "Text\tcontinued\n", md010Options(spaces, false), md010Producer)
		if got, want := string(view.Candidate.Bytes()), "Text"+strings.Repeat(" ", spaces)+"continued\n"; got != want {
			t.Fatalf("%d: %q", spaces, got)
		}
		decisions := view.Attempts[0].Decisions
		if len(decisions) != 1 {
			t.Fatal(decisions)
		}
		d := decisions[0]
		if !d.Accepted || d.Group.ID != "MD010-byte-4" || d.Group.Rule != "MD010" || d.Group.Anchor.Start() != 4 || d.Group.Anchor.End() != 5 || len(d.Group.Edits) != 1 || d.Group.Edits[0].Span != d.Group.Anchor || string(d.Group.Edits[0].Expected) != "\t" || string(d.Group.Edits[0].Replacement) != strings.Repeat(" ", spaces) {
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
}

func TestMD010ExpansionLimitDefers(t *testing.T) {
	for _, spaces := range []int{parser.MaxTabSpaces + 1, int(^uint(0) >> 1)} {
		view := md010Fixed(t, "commonmark", "Text\tcontinued\n", md010Options(spaces, false), rules.Corrections)
		if got := string(view.Candidate.Bytes()); got != "Text\tcontinued\n" {
			t.Fatalf("%q", got)
		}
		decisions := view.Attempts[0].Decisions
		if len(decisions) != 1 || decisions[0].Accepted || len(decisions[0].Group.Edits) != 0 || len(decisions[0].Reasons) != 1 || decisions[0].Reasons[0].Code != "expansion-limit" {
			t.Fatalf("%+v", decisions)
		}
		if len(view.Analysis.Diagnostics) != 1 || view.Analysis.Diagnostics[0].Rule != "MD010" || view.Analysis.Diagnostics[0].FixAvailable {
			t.Fatal(view.Analysis.Diagnostics)
		}
	}
}

func TestMD010CorrectsInlineText(t *testing.T) {
	for _, tc := range []struct {
		name, flavor, source, want string
	}{
		{"paragraph", "commonmark", "Text\tcontinued\n", "Text    continued\n"},
		{"atx heading", "commonmark", "# A\tB\n", "# A    B\n"},
		{"setext heading", "commonmark", "A\tB\n===\n", "A    B\n===\n"},
		{"table cell", "gfm", "| a\tb | c |\n| - | - |\n| d\te | f |\n", "| a    b | c |\n| - | - |\n| d    e | f |\n"},
		{"list item", "commonmark", "- a\tb\n", "- a    b\n"},
		{"block quote", "commonmark", "> a\tb\n", "> a    b\n"},
		{"nested containers", "commonmark", "> - a\tb\n>   c\td\n", "> - a    b\n>   c    d\n"},
		{"link text", "commonmark", "[a\tb](u)\n", "[a    b](u)\n"},
		{"adjacent", "commonmark", "a\t\tb\tc\n", "a        b    c\n"},
		{"crlf", "commonmark", "a\tb\r\nc\td\r\n", "a    b\r\nc    d\r\n"},
		{"cr", "commonmark", "a\tb\rc\td\r", "a    b\rc    d\r"},
		{"unicode", "commonmark", "é\tö\n", "é    ö\n"},
		{"astral", "commonmark", "雪\t😀\n", "雪    😀\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, code := range []bool{false, true} {
				view := md010Fixed(t, tc.flavor, tc.source, md010Options(4, code), rules.Corrections)
				if got := string(view.Candidate.Bytes()); got != tc.want {
					t.Fatalf("code %v: got %q want %q", code, got, tc.want)
				}
				if md010Kinds(t, tc.flavor, tc.source) != md010Kinds(t, tc.flavor, tc.want) {
					t.Fatal("structure changed")
				}
				for _, d := range view.Analysis.Diagnostics {
					if d.Rule == "MD010" {
						t.Fatal(d)
					}
				}
			}
		})
	}
}

func TestMD010LeavesProtectedTabs(t *testing.T) {
	for _, tc := range []struct {
		name, flavor, source string
	}{
		{"leading", "commonmark", "\tText\n"},
		{"list prefix", "commonmark", "-\tText\n"},
		{"quote prefix", "commonmark", ">\tText\n"},
		{"fenced code", "commonmark", "```\na\tb\n```\n"},
		{"indented code", "commonmark", "    a\tb\n"},
		{"code span", "commonmark", "a `b\tc` d\n"},
		{"inline html", "commonmark", "a <b\tclass=\"x\">c</b>\n"},
		{"html block", "commonmark", "<div>\na\tb\n</div>\n"},
		{"pre body", "commonmark", "a <pre>b\tc</pre> d\n"},
		{"frontmatter", "commonmark", "---\na:\tb\n---\nc\n"},
		{"link destination", "commonmark", "[a](<b\tc>)\n"},
		{"link title", "commonmark", "[a](b \"c\td\")\n"},
		{"reference definition", "commonmark", "[a]:\t/u\t'title'\n\n[a]\n"},
		{"table delimiter", "gfm", "| a | b |\n|\t- | - |\n"},
		{"cell trimming", "gfm", "| a\t| b |\n| - | - |\n"},
		{"line end", "commonmark", "a\t\nb\n"},
		{"image description", "commonmark", "![a\tb](i)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, code := range []bool{false, true} {
				view := md010Fixed(t, tc.flavor, tc.source, md010Options(4, code), rules.Corrections)
				if got := string(view.Candidate.Bytes()); got != tc.source {
					t.Fatalf("code %v: %q", code, got)
				}
				for _, d := range view.Analysis.Diagnostics {
					if d.Rule == "MD010" && d.FixAvailable {
						t.Fatal(d)
					}
				}
			}
		})
	}
}

func TestMD010RespectsDirectives(t *testing.T) {
	r := registry(t, rules.Registrations())
	s := md010Settings()
	selection, err := r.SelectRules(s, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := fix.NewValidator(r, rules.CorrectionCheckers())
	if err != nil {
		t.Fatal(err)
	}
	source := "a\tb\n\n<!-- mdchk:disable MD010 -->\n\nc\td\n\n<!-- mdchk:enable MD010 -->\n\ne\tf\n"
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	out, err := validator.Validate(context.Background(), []fix.Target{{Path: "doc.md", Source: parser.NewSource([]byte(source)), Plan: plan, Produce: rules.Corrections}}, fix.ValidationOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	view, _ := out.File("doc.md")
	if got, want := string(view.Candidate.Bytes()), "a    b\n\n<!-- mdchk:disable MD010 -->\n\nc\td\n\n<!-- mdchk:enable MD010 -->\n\ne    f\n"; got != want {
		t.Fatalf("%q", got)
	}
}

func TestMD010ComposesWithMD009AndMD047(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"a\tb \nc\td", "a    b\nc    d\n"},
		{"a\tb  \nc\td", "a    b  \nc    d\n"},
		{"a\tb\t \nc\td", "a    b\nc    d\n"},
	} {
		view := md010Fixed(t, "commonmark", tc.source, md010Options(4, false), rules.Corrections, "MD009", "MD047")
		if got := string(view.Candidate.Bytes()); got != tc.want {
			t.Fatalf("%q: got %q want %q", tc.source, got, tc.want)
		}
		again := md010Fixed(t, "commonmark", tc.want, md010Options(4, false), rules.Corrections, "MD009", "MD047")
		if string(again.Candidate.Bytes()) != tc.want || len(again.Analysis.Diagnostics) != 0 {
			t.Fatal("second run changed", again.Analysis.Diagnostics)
		}
	}
}

func TestMD010CheckerRejectsTamperedGroups(t *testing.T) {
	tamper := func(rewrite func(fix.Group) fix.Group) fix.Producer {
		return func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
			groups, err := md010Producer(ctx, c)
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
		{"wrong replacement", func(g fix.Group) fix.Group { g.Edits[0].Replacement = []byte("  "); return g }},
		{"removed tab", func(g fix.Group) fix.Group { g.Edits[0].Replacement = nil; return g }},
		{"text replacement", func(g fix.Group) fix.Group { g.Edits[0].Replacement = []byte("x"); return g }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "a\tb\n"
			view := md010Fixed(t, "commonmark", source, md010Options(4, false), tamper(tc.rewrite))
			if string(view.Candidate.Bytes()) != source {
				t.Fatalf("tampered candidate retained: %q", view.Candidate.Bytes())
			}
			decisions := view.Attempts[0].Decisions
			if len(decisions) != 1 || decisions[0].Accepted || len(decisions[0].Reasons) == 0 || decisions[0].Reasons[len(decisions[0].Reasons)-1].Code != "meaning-not-preserved" {
				t.Fatalf("%+v", decisions)
			}
		})
	}
}
