package md027_test

import (
	"context"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/md027"
)

func md027FixSettings(enabled bool) engine.Settings {
	s := md027Settings()
	s["MD027"] = engine.RuleSetting{Enabled: &enabled, Options: engine.Options{}}
	return s
}

func md027Corrected(t *testing.T, flavor, text string, enabled bool, producer fix.Producer) fix.FileView {
	t.Helper()
	r := registry(t, rules.Registrations())
	selection, err := r.SelectRules(md027FixSettings(enabled), engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: flavor}, nil)
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

func md027Findings(ctx context.Context, c fix.RoundContext) ([]result.Diagnostic, error) {
	analysis, err := c.BoundedAnalysis(ctx)
	if err != nil {
		return nil, err
	}
	var findings []result.Diagnostic
	for _, d := range analysis.Diagnostics {
		if d.Rule == "MD027" && d.FixAvailable {
			findings = append(findings, d)
		}
	}
	return findings, nil
}

// md027Producer runs only MD027's part of Corrections.
func md027Producer(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
	findings, err := md027Findings(ctx, c)
	if err != nil {
		return nil, err
	}
	return md027.Correction(ctx, c, findings)
}

func md027Analysis(t *testing.T, flavor, text string) result.Result {
	t.Helper()
	r := registry(t, rules.Registrations())
	return analyzeHeadings(t, r, text, md027Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
}

func TestMD027FixSpans(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{">   x\n", "> x\n"},
		{"> >   x\n", "> > x\n"},
		{"- >   x\n", "- > x\n"},
		{">   x\r\n>    y\r\n", "> x\r\n> y\r\n"},
		{">   été  x\n", "> été  x\n"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			analysis := md027Analysis(t, "commonmark", tc.source)
			ranges := map[int]result.ByteRange{}
			for _, d := range analysis.Diagnostics {
				if d.Rule == "MD027" {
					if !d.FixAvailable {
						t.Fatal("not fixable", d)
					}
					ranges[d.ByteRange.Start] = d.ByteRange
				}
			}
			view := md027Corrected(t, "commonmark", tc.source, true, md027Producer)
			if got := string(view.Candidate.Bytes()); got != tc.want {
				t.Fatalf("got %q want %q: %+v", got, tc.want, view.Attempts)
			}
			decisions := view.Attempts[0].Decisions
			if len(decisions) != len(ranges) || len(ranges) == 0 {
				t.Fatal(decisions, ranges)
			}
			for _, d := range decisions {
				g := d.Group
				r, ok := ranges[g.Anchor.Start()]
				if !d.Accepted || g.Rule != "MD027" || !strings.HasPrefix(g.ID, "MD027-line-") || !ok || len(g.Edits) != 1 || g.Edits[0].Span != g.Anchor || g.Anchor.End() != r.End || len(g.Edits[0].Replacement) != 0 || strings.Trim(string(g.Edits[0].Expected), " ") != "" {
					t.Fatalf("%+v", d)
				}
				// Exactly one SPACE stays between the marker and the content.
				src := []byte(tc.source)
				if src[r.Start-1] != ' ' || src[r.Start-2] != '>' || src[r.End] == ' ' {
					t.Fatalf("%q %+v", tc.source, r)
				}
			}
			again := md027Corrected(t, "commonmark", tc.want, true, rules.Corrections)
			if string(again.Candidate.Bytes()) != tc.want {
				t.Fatal("not idempotent", string(again.Candidate.Bytes()))
			}
		})
	}
}

func md027Texts(t *testing.T, text string) (kinds []string, texts []string) {
	t.Helper()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithOptions(context.Background(), parser.NewSource([]byte(text)), budget, parser.ParseOptions{})
	if parsed.Document == nil || parsed.Status != parser.Complete {
		t.Fatal("parse", parsed.Status)
	}
	var walk func(n *parser.Node)
	walk = func(n *parser.Node) {
		kinds = append(kinds, string(n.Kind()))
		if n.Kind() == parser.Text {
			texts = append(texts, n.Properties().Literal.Value)
		}
		for _, c := range n.Children() {
			walk(c)
		}
	}
	walk(parsed.Document.Root())
	return kinds, texts
}

func TestMD027ContinuationKeepsParagraph(t *testing.T) {
	source := "> a\n>     Text\n"
	view := md027Corrected(t, "commonmark", source, true, rules.Corrections)
	got := string(view.Candidate.Bytes())
	if got != "> a\n> Text\n" {
		t.Fatalf("%q %+v", got, view.Attempts)
	}
	beforeKinds, beforeTexts := md027Texts(t, source)
	afterKinds, afterTexts := md027Texts(t, got)
	if strings.Join(beforeKinds, ",") != strings.Join(afterKinds, ",") || strings.Join(beforeTexts, "|") != strings.Join(afterTexts, "|") {
		t.Fatal(beforeKinds, afterKinds, beforeTexts, afterTexts)
	}
	if strings.Count(strings.Join(afterKinds, ","), string(parser.Paragraph)) != 1 || strings.Contains(strings.Join(afterKinds, ","), string(parser.CodeBlock)) {
		t.Fatal(afterKinds)
	}
}

func TestMD027ProtectedContextsStayManual(t *testing.T) {
	for _, tc := range []struct{ flavor, source string }{
		{"commonmark", "> a\n   b\n"},
		{"commonmark", "> a `b\n>   c` d\n"},
		{"commonmark", "> a <span\n>   title=\"x\"> b\n"},
		{"commonmark", "> <div>\n>   x\n"},
		{"commonmark", "> \t x\n"},
		{"gfm", "---\n>   x\n---\n\n# T\n"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			for _, d := range md027Analysis(t, tc.flavor, tc.source).Diagnostics {
				if d.Rule == "MD027" && d.FixAvailable {
					t.Fatal("protected finding fixable", d)
				}
			}
			view := md027Corrected(t, tc.flavor, tc.source, true, rules.Corrections)
			if got := string(view.Candidate.Bytes()); got != tc.source {
				t.Fatalf("%q", got)
			}
		})
	}
}

func TestMD027DirectiveRegionStays(t *testing.T) {
	source := ">   a\n\n<!-- mdchk:disable MD027 -->\n\n>   b\n\n<!-- mdchk:enable MD027 -->\n\n>   c\n"
	want := "> a\n\n<!-- mdchk:disable MD027 -->\n\n>   b\n\n<!-- mdchk:enable MD027 -->\n\n> c\n"
	view := md027Corrected(t, "commonmark", source, true, rules.Corrections)
	if got := string(view.Candidate.Bytes()); got != want {
		t.Fatalf("%q %+v", got, view.Attempts)
	}
}

func TestMD027SettingsAndFlavors(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		if got := string(md027Corrected(t, flavor, ">   x\n", true, rules.Corrections).Candidate.Bytes()); got != "> x\n" {
			t.Fatal(flavor, got)
		}
		if got := string(md027Corrected(t, flavor, ">   x\n", false, rules.Corrections).Candidate.Bytes()); got != ">   x\n" {
			t.Fatal("disabled rule edited", flavor, got)
		}
	}
}

func TestMD027CheckerRejectsUnprovenEdits(t *testing.T) {
	for name, tamper := range map[string]func(c fix.RoundContext, g *fix.Group) error{
		"shifted span": func(c fix.RoundContext, g *fix.Group) error {
			s, err := c.Current().Span(g.Anchor.Start()+1, g.Anchor.End())
			g.Anchor, g.Edits[0].Span, g.Edits[0].Expected = s, s, g.Edits[0].Expected[1:]
			return err
		},
		"replacement": func(c fix.RoundContext, g *fix.Group) error {
			g.Edits[0].Replacement = []byte("  ")
			return nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := md027Producer(ctx, c)
				for i := range groups {
					if err == nil && len(groups[i].Edits) == 1 {
						err = tamper(c, &groups[i])
					}
				}
				return groups, err
			}
			view := md027Corrected(t, "commonmark", ">   x\n", true, producer)
			if got := string(view.Candidate.Bytes()); got != ">   x\n" {
				t.Fatalf("%q", got)
			}
		})
	}
}
