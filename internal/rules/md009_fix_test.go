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

func md009CorrectionSetup(t *testing.T, flavor string, md047 bool, options engine.Options) (*fix.Validator, *engine.ExecutionPlan) {
	t.Helper()
	r := registry(t, Registrations())
	s := md009Settings()
	s["MD009"] = engine.RuleSetting{Options: options}
	if md047 {
		on := true
		s["MD047"] = engine.RuleSetting{Enabled: &on}
	}
	selection, err := r.SelectRules(s, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: flavor}, nil)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := fix.NewValidator(r, CorrectionCheckers())
	if err != nil {
		t.Fatal(err)
	}
	return validator, plan
}

func md009Corrected(t *testing.T, flavor, text string, producer fix.Producer) fix.FileView {
	t.Helper()
	v, p := md009CorrectionSetup(t, flavor, false, engine.Options{})
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	out, err := v.Validate(context.Background(), []fix.Target{{Path: "doc.md", Source: parser.NewSource([]byte(text)), Plan: p, Produce: producer}}, fix.ValidationOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	view, ok := out.File("doc.md")
	if !ok {
		t.Fatal("missing file")
	}
	return view
}

// md009Producer runs only MD009's part of Corrections.
func md009Producer(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
	analysis, err := c.BoundedAnalysis(ctx)
	if err != nil {
		return nil, err
	}
	var findings []result.Diagnostic
	for _, d := range analysis.Diagnostics {
		if d.Rule == "MD009" {
			findings = append(findings, d)
		}
	}
	return trailingWhitespaceGroups(ctx, c, findings)
}

func md009Walk(n *parser.Node, visit func(*parser.Node)) {
	visit(n)
	for _, c := range n.Children() {
		md009Walk(c, visit)
	}
}

func md009Parse(t *testing.T, flavor, text string) *parser.Document {
	t.Helper()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithOptions(context.Background(), parser.NewSource([]byte(text)), budget, parser.ParseOptions{GFM: flavor == "gfm"})
	if parsed.Document == nil || parsed.Status != parser.Complete {
		t.Fatal("parse", parsed.Status)
	}
	return parsed.Document
}

func md009Kinds(t *testing.T, flavor, text string) []parser.Kind {
	t.Helper()
	var kinds []parser.Kind
	md009Walk(md009Parse(t, flavor, text).Root(), func(n *parser.Node) { kinds = append(kinds, n.Kind()) })
	return kinds
}

func TestMD009ProducerGroups(t *testing.T) {
	view := md009Corrected(t, "commonmark", "a \nb   \nc\n\n```\n```\n", md009Producer)
	if got := string(view.Candidate.Bytes()); got != "a\nb  \nc\n\n```\n```\n" {
		t.Fatalf("%q", got)
	}
	decisions := view.Attempts[0].Decisions
	if len(decisions) != 2 {
		t.Fatal(decisions)
	}
	for i, want := range []struct {
		id          string
		start       int
		replacement string
	}{{"MD009-line-1", 1, ""}, {"MD009-line-2", 4, "  "}} {
		d := decisions[i]
		if !d.Accepted || d.Group.ID != want.id || d.Group.Rule != "MD009" || d.Group.Anchor.Start() != want.start || len(d.Group.Edits) != 1 || string(d.Group.Edits[0].Replacement) != want.replacement || d.Group.Edits[0].Span != d.Group.Anchor {
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

func TestMD009ProducerWithholdsWithReason(t *testing.T) {
	view := md009Corrected(t, "commonmark", "a\\ \nb\n\nc \nd\n", md009Producer)
	if got := string(view.Candidate.Bytes()); got != "a\\ \nb\n\nc\nd\n" {
		t.Fatalf("%q", got)
	}
	found := false
	for _, d := range view.Attempts[0].Decisions {
		if d.Group.ID == "MD009-line-1" {
			found = true
			if d.Accepted || len(d.Group.Edits) != 0 || len(d.Reasons) != 1 || d.Reasons[0].Code != "meaning-not-proven" {
				t.Fatalf("%+v", d)
			}
		}
	}
	if !found {
		t.Fatal(view.Attempts)
	}
}

func TestMD009CandidateKeepsParsedStructure(t *testing.T) {
	for _, tc := range []struct {
		flavor, source, want string
		hardBreaks           int
	}{
		{"commonmark", "a   \nb\n", "a  \nb\n", 1},
		{"commonmark", "a\t  \nb\n", "a  \nb\n", 1},
		{"commonmark", "a  \t\nb\n", "a\nb\n", 0},
		{"commonmark", "> \n", ">\n", 0},
		{"commonmark", "> a   \n> b\n", "> a  \n> b\n", 1},
		{"commonmark", "- a\n  \n  b\n", "- a\n\n  b\n", 0},
		{"commonmark", "> - a\n>   \n>   b\n", "> - a\n>\n>   b\n", 0},
		{"gfm", "- [ ] \n- x\n", "- [ ]\n- x\n", 0},
		{"gfm", "| a | \n|---| \n| b | \n", "| a |\n|---|\n| b |\n", 0},
	} {
		t.Run(tc.source, func(t *testing.T) {
			view := md009Corrected(t, tc.flavor, tc.source, md009Producer)
			if got := string(view.Candidate.Bytes()); got != tc.want {
				t.Fatalf("got %q want %q: %+v", got, tc.want, view.Attempts)
			}
			before, after := md009Kinds(t, tc.flavor, tc.source), md009Kinds(t, tc.flavor, tc.want)
			if strings.Join(kindNames(before), ",") != strings.Join(kindNames(after), ",") {
				t.Fatal(before, after)
			}
			hard := 0
			for _, k := range after {
				if k == parser.HardBreak {
					hard++
				}
			}
			if hard != tc.hardBreaks {
				t.Fatal("hard breaks", hard)
			}
		})
	}
}

func kindNames(kinds []parser.Kind) []string {
	out := make([]string, len(kinds))
	for i, k := range kinds {
		out[i] = string(k)
	}
	return out
}

func TestMD009CandidateKeepsTasksAndLinks(t *testing.T) {
	task := func(n *parser.Node) bool { return n.Kind() == parser.Paragraph && n.Properties().Task != nil }
	var tasks int
	md009Walk(md009Parse(t, "gfm", "- [ ]\n- x\n").Root(), func(n *parser.Node) {
		if task(n) {
			tasks++
		}
	})
	if tasks != 1 {
		t.Fatal("task lost", tasks)
	}
	view := md009Corrected(t, "commonmark", "[a \nb]: c\n\n[a b]\n", md009Producer)
	if got := string(view.Candidate.Bytes()); got != "[a\nb]: c\n\n[a b]\n" {
		t.Fatal(got)
	}
	resolved := false
	md009Walk(md009Parse(t, "commonmark", string(view.Candidate.Bytes())).Root(), func(n *parser.Node) {
		if n.Kind() == parser.Link && n.Properties().Link.Resolution == parser.Available {
			resolved = true
		}
	})
	if !resolved {
		t.Fatal("link no longer resolves")
	}
}

// A tampered group must fail the independent checker whatever its producer claims.
func TestMD009CheckerRejectsTamperedGroups(t *testing.T) {
	tamper := func(rewrite func(*testing.T, fix.RoundContext, fix.Group) fix.Group) fix.Producer {
		return func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
			groups, err := md009Producer(ctx, c)
			if err != nil {
				return nil, err
			}
			for i := range groups {
				groups[i] = rewrite(t, c, groups[i])
			}
			return groups, nil
		}
	}
	replaced := func(replacement string) func(*testing.T, fix.RoundContext, fix.Group) fix.Group {
		return func(_ *testing.T, _ fix.RoundContext, g fix.Group) fix.Group {
			g.Edits[0].Replacement = []byte(replacement)
			return g
		}
	}
	for _, tc := range []struct {
		name, source string
		rewrite      func(*testing.T, fix.RoundContext, fix.Group) fix.Group
	}{
		{"wrong replacement", "a \n\nb\n", replaced("\t")},
		{"added hard break", "a \nb\n", replaced("  ")},
		{"removed hard break", "a   \nb\n", replaced("")},
		{"wrong span", "a   \n\nb\n", func(t *testing.T, c fix.RoundContext, g fix.Group) fix.Group {
			t.Helper()
			span, err := c.Current().Span(g.Anchor.Start(), g.Anchor.End()-1)
			if err != nil {
				t.Fatal(err)
			}
			text, _ := c.Current().Text(span)
			g.Edits[0].Span = span
			g.Edits[0].Expected = []byte(text)
			return g
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := md009Corrected(t, "commonmark", tc.source, tamper(tc.rewrite))
			if string(view.Candidate.Bytes()) != tc.source {
				t.Fatalf("tampered candidate retained: %q", view.Candidate.Bytes())
			}
			decisions := view.Attempts[0].Decisions
			if len(decisions) != 1 || decisions[0].Accepted || len(decisions[0].Reasons) == 0 || decisions[0].Reasons[len(decisions[0].Reasons)-1].Code != "meaning-not-preserved" {
				t.Fatalf("%+v", decisions)
			}
		})
	}
}

func TestMD009FixEligibility(t *testing.T) {
	r := registry(t, Registrations())
	for _, tc := range []struct {
		name, flavor, source string
		md047, want          bool
	}{
		{"ordinary", "commonmark", "a \n", false, true},
		{"eof with md047", "commonmark", "a ", true, false},
		{"eof without md047", "commonmark", "a ", false, true},
		{"line with md047", "commonmark", "a \nb\n", true, true},
		{"odd backslash", "commonmark", "a\\ \nb\n", false, false},
		{"even backslash", "commonmark", "a\\\\ \nb\n", false, true},
		{"title", "commonmark", "[a](b \"t \nu\")\n", false, false},
		{"reference title", "commonmark", "[a]: b \"t \nu\"\n", false, false},
		{"eof task", "gfm", "- [x] ", false, false},
		{"eof literal task", "commonmark", "- [x] ", false, true},
		{"mid-file task", "gfm", "- [x] \n- b\n", false, true},
		{"task continuation", "gfm", "- [x] \n  b\n", false, false},
		{"task lazy continuation", "gfm", "- [x] \nb\n", false, false},
		{"task text continuation", "gfm", "- [x] a \n  b\n", false, true},
		{"unrecognized block payload", "commonmark", "---\nt: y \n---\n\nb\n", false, false},
		{"unrecognized block body", "commonmark", "---\nt: y\n---\n\na \nb\n", false, false},
		{"unrecognized toml block body", "gfm", "+++\nt = 1\n+++\n\na \nb\n", false, false},
		{"unrecognized block eof without md047", "commonmark", "---\nt: y\n---\n\na ", false, false},
		{"paired thematic breaks", "commonmark", "---\n\na \n\n---\n", false, false},
		{"opener without closer", "commonmark", "---\n\na \nb\n", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := md009Settings()
			if tc.md047 {
				on := true
				s["MD047"] = engine.RuleSetting{Enabled: &on}
			}
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			selection, err := r.SelectRules(s, engine.ValidationProviders{})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := selection.Plan(engine.DocumentContext{Flavor: tc.flavor}, nil)
			if err != nil {
				t.Fatal(err)
			}
			in, err := plan.NewInput("doc.md", []byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			out, err := plan.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Budget: budget})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, d := range out.Diagnostics {
				if d.Rule == "MD009" {
					found = true
					if d.FixAvailable != tc.want {
						t.Fatalf("%+v", d)
					}
				}
			}
			if !found {
				t.Fatal(out.Diagnostics)
			}
		})
	}
}

func TestOnlyImplementedFixes(t *testing.T) {
	for _, m := range engine.Catalog() {
		if (m.Fix == engine.ImplementedFix) != (m.ID == "MD003" || m.ID == "MD005" || m.ID == "MD009" || m.ID == "MD019" || m.ID == "MD021" || m.ID == "MD022" || m.ID == "MD023" || m.ID == "MD029" || m.ID == "MD032" || m.ID == "MD047" || m.ID == "MD063") {
			t.Errorf("%s fix = %s", m.ID, m.Fix)
		}
	}
}
