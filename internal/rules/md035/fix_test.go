package md035_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/md035"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

// md035Corrected validates one document under MD035 with the given style and
// any other enabled rules.
func md035Corrected(t *testing.T, text, style string, producer fix.Producer, enable ...string) fix.FileView {
	t.Helper()
	r := ruletest.Registry(t, rules.Registrations())
	s := md035Settings()
	s["MD035"] = engine.RuleSetting{Options: engine.Options{"style": style}}
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

// md035Producer runs only MD035's part of Corrections.
func md035Producer(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
	analysis, err := c.BoundedAnalysis(ctx)
	if err != nil {
		return nil, err
	}
	var findings []result.Diagnostic
	for _, d := range analysis.Diagnostics {
		if d.Rule == "MD035" {
			findings = append(findings, d)
		}
	}
	return md035.Correction(ctx, c, findings)
}

func TestMD035ProducerGroups(t *testing.T) {
	view := md035Corrected(t, "---\n\n  * * *  \n\n___\n", "consistent", md035Producer)
	if got := string(view.Candidate.Bytes()); got != "---\n\n  ---  \n\n---\n" {
		t.Fatalf("%q", got)
	}
	decisions := view.Attempts[0].Decisions
	if len(decisions) != 2 {
		t.Fatal(decisions)
	}
	for i, want := range []struct {
		id         string
		start, end int
		expected   string
	}{{"MD035-line-3", 7, 12, "* * *"}, {"MD035-line-5", 16, 19, "___"}} {
		d := decisions[i]
		if !d.Accepted || d.Group.ID != want.id || d.Group.Rule != "MD035" || len(d.Group.Edits) != 1 {
			t.Fatalf("%+v", d)
		}
		e := d.Group.Edits[0]
		if e.Span.Start() != want.start || e.Span.End() != want.end || string(e.Expected) != want.expected || string(e.Replacement) != "---" {
			t.Fatalf("%+v", e)
		}
		if d.Group.Anchor != e.Span {
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

func TestMD035Candidates(t *testing.T) {
	for _, tc := range []struct {
		name, source, style, want string
		enable                    []string
	}{
		{"default", "---\n\n***\n", "consistent", "---\n\n---\n", nil},
		{"asterisk dash", "- - -\n", "asterisk", "***\n", nil},
		{"asterisk underscore", "_____\n", "asterisk", "***\n", nil},
		{"underscore", "---\n\n* * *\n", "underscore", "___\n\n___\n", nil},
		{"dash", "***\n\n_ _ _\n", "dash", "---\n\n---\n", nil},
		{"conforming kept", "-----\n\n- - -\n", "dash", "-----\n\n- - -\n", nil},
		{"consistent spaced", "* * *\n\n---\n\n***\n", "consistent", "* * *\n\n* * *\n\n* * *\n", nil},
		{"lf", "***\n\npara\n\n___\n", "dash", "---\n\npara\n\n---\n", nil},
		{"crlf", "---\r\n\r\n***\r\n", "consistent", "---\r\n\r\n---\r\n", nil},
		{"cr", "---\r\r***\r", "consistent", "---\r\r---\r", nil},
		{"indented crlf", "---\r\n\r\n  ***  \r\n", "consistent", "---\r\n\r\n  ---  \r\n", nil},
		{"quote crlf", "---\r\n\r\n> ***\r\n", "consistent", "---\r\n\r\n> ---\r\n", nil},
		{"quote nested", "> ---\n>\n> > ***\n", "consistent", "> ---\n>\n> > ---\n", nil},
		{"list item", "---\n\n- a\n\n  ***\n", "consistent", "---\n\n- a\n\n  ---\n", nil},
		{"unicode before", "é\n\n***\n\n日本\n", "dash", "é\n\n---\n\n日本\n", nil},
		{"unicode after trailing", "***  \n✓\n", "dash", "---  \n✓\n", nil},
		{"fenced", "---\n\n```\n***\n```\n", "consistent", "---\n\n```\n***\n```\n", nil},
		{"indented code", "---\n\n    ***\n", "consistent", "---\n\n    ***\n", nil},
		{"html", "---\n\n<div>\n***\n</div>\n", "consistent", "---\n\n<div>\n***\n</div>\n", nil},
		{"setext guard", "para\n***\n", "dash", "para\n***\n", nil},
		{"list item guard", "* * *\n", "asterisk", "* * *\n", nil},
		{"dash list position", "---\n\n- a\n- - -\n", "consistent", "---\n\n- a\n---\n", nil},
		{"star list position", "***\n\n* a\n* * *\n", "consistent", "***\n\n* a\n***\n", nil},
		{"item setext guard", "---\n\n- a\n  * * *\n", "consistent", "---\n\n- a\n  * * *\n", nil},
		{"spaced list position", "***\n\n- - -\n", "asterisk", "***\n\n***\n", nil},
		{"spaced to dash under paragraph", "para\n\n* * *\n", "dash", "para\n\n---\n", nil},
		{"composition", "a \n\n\n\n---\n\n***", "consistent", "a\n\n---\n\n---\n", []string{"MD009", "MD012", "MD047"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := md035Corrected(t, tc.source, tc.style, rules.Corrections, tc.enable...)
			if got := string(view.Candidate.Bytes()); got != tc.want {
				t.Fatalf("%q, want %q", got, tc.want)
			}
			again := md035Corrected(t, tc.want, tc.style, rules.Corrections, tc.enable...)
			if got := string(again.Candidate.Bytes()); got != tc.want {
				t.Fatalf("second run %q", got)
			}
		})
	}
}

func TestMD035SetextGuardDefers(t *testing.T) {
	view := md035Corrected(t, "para\n***\n", "dash", md035Producer)
	decisions := view.Attempts[0].Decisions
	if len(decisions) != 1 || decisions[0].Accepted || string(view.Candidate.Bytes()) != "para\n***\n" {
		t.Fatalf("%+v", decisions)
	}
}

func TestMD035CheckerRejectsTamperedGroups(t *testing.T) {
	tamper := func(rewrite func(*testing.T, fix.RoundContext, fix.Group) fix.Group) fix.Producer {
		return func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
			groups, err := md035Producer(ctx, c)
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
		name    string
		rewrite func(*testing.T, fix.RoundContext, fix.Group) fix.Group
	}{
		{"replacement", func(_ *testing.T, _ fix.RoundContext, g fix.Group) fix.Group {
			g.Edits[0].Replacement = []byte("----")
			return g
		}},
		{"wider span", func(t *testing.T, c fix.RoundContext, g fix.Group) fix.Group {
			t.Helper()
			span, err := c.Current().Span(g.Edits[0].Span.Start(), g.Edits[0].Span.End()+1)
			if err != nil {
				t.Fatal(err)
			}
			text, err := c.Current().Text(span)
			if err != nil {
				t.Fatal(err)
			}
			g.Edits[0].Span, g.Edits[0].Expected, g.Edits[0].Replacement, g.Anchor = span, []byte(text), []byte("---\n"), span
			return g
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "---\n\n***\n"
			view := md035Corrected(t, source, "consistent", tamper(tc.rewrite))
			if string(view.Candidate.Bytes()) != source {
				t.Fatalf("tampered candidate retained: %q", view.Candidate.Bytes())
			}
			decisions := view.Attempts[0].Decisions
			if len(decisions) != 1 || decisions[0].Accepted {
				t.Fatalf("%+v", decisions)
			}
		})
	}
}
