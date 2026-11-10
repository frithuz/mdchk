package md004_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

func bulletCorrected(t *testing.T, text, marker string, producer fix.Producer, ids ...string) (fix.FileView, fix.Validated) {
	t.Helper()
	settings := ruletest.Settings(append(ids, "MD004")...)
	enabled := true
	settings["MD004"] = engine.RuleSetting{Enabled: &enabled, Options: engine.Options{"marker": marker}}
	r := registry(t, rules.Registrations())
	selection, err := r.SelectRules(settings, engine.ValidationProviders{})
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

// blockShape renders a parse's block kinds, nesting and list facts, so a
// reparsed candidate can be compared with its source.
func blockShape(t *testing.T, text string) string {
	t.Helper()
	ctx := context.Background()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithOptions(ctx, parser.NewSource([]byte(text)), budget, engine.DocumentContext{Flavor: "commonmark"}.ParseOptions())
	var b strings.Builder
	var walk func(*parser.Node, int)
	walk = func(n *parser.Node, depth int) {
		b.WriteString(strings.Repeat(" ", depth) + string(n.Kind()))
		if p := n.Properties().List; p != nil {
			b.WriteString(" ordered=" + map[bool]string{true: "y", false: "n"}[p.Ordered] + " tight=" + map[bool]string{true: "y", false: "n"}[p.Tight.Value])
		}
		b.WriteString("\n")
		switch n.Kind() {
		case parser.DocumentKind, parser.BlockQuote, parser.List, parser.ListItem:
			for _, c := range n.Children() {
				walk(c, depth+1)
			}
		default:
		}
	}
	walk(parsed.Document.Root(), 0)
	return b.String()
}

func TestBulletMarkerFixturesReparseWithSameStructure(t *testing.T) {
	data, err := os.ReadFile("testdata/md004_corrections.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Source, Candidate, Config string
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		marker := "consistent"
		switch tc.Config {
		case "asterisk", "plus", "dash":
			marker = tc.Config
		case "":
		default:
			// Configuration-dependent fixtures run through the production command.
			continue
		}
		t.Run(tc.Name, func(t *testing.T) {
			view, out := bulletCorrected(t, tc.Source, marker, rules.Corrections)
			got := string(view.Candidate.Bytes())
			if !out.Complete() || got != tc.Candidate {
				t.Fatalf("got %q complete %v attempts %+v", got, out.Complete(), view.Attempts)
			}
			if blockShape(t, tc.Source) != blockShape(t, got) {
				t.Fatalf("structure changed:\n%s\n%s", blockShape(t, tc.Source), blockShape(t, got))
			}
			if tc.Source != got && view.ChangingRounds != 1 {
				t.Fatal("rounds", view.ChangingRounds)
			}
			for i := range len(got) {
				if got[i] != tc.Source[i] && !strings.ContainsRune("-*+", rune(got[i])) {
					t.Fatalf("byte %d changed to %q", i, got[i])
				}
			}
		})
	}
}

func TestBulletMarkerEligibility(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		eligible     []bool
	}{
		{"separated list", "- a\n\ntext\n\n* b\n", []bool{true}},
		{"adjacent lists", "- a\n* b\n", []bool{false}},
		{"blank separated lists", "- a\n\n* b\n", []bool{false}},
		{"thematic break", "- a\n\ntext\n\n* - -\n", []bool{false}},
		{"whole list", "- a\n\ntext\n\n* b\n* c\n", []bool{true, true}},
		{"directive disabled", "- a\n\ntext\n\n* b\n\n<!-- mdchk:disable MD004 -->\n\n- x\n\ntext\n\n* c\n", []bool{true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, _ := bulletCorrected(t, tc.source, "consistent", func(context.Context, fix.RoundContext) ([]fix.Group, error) { return nil, nil })
			var got []bool
			for _, d := range view.Analysis.Diagnostics {
				if d.Rule == "MD004" {
					if d.ByteRange.End-d.ByteRange.Start != 1 || !strings.ContainsRune("*+-", rune(tc.source[d.ByteRange.Start])) {
						t.Fatal("span", d.ByteRange)
					}
					got = append(got, d.FixAvailable)
				}
			}
			if len(got) != len(tc.eligible) {
				t.Fatal(got)
			}
			for i := range got {
				if got[i] != tc.eligible[i] {
					t.Fatal(got)
				}
			}
		})
	}
}

func TestBulletMarkerCheckerRejectsForgedChanges(t *testing.T) {
	source := "- a\n\ntext\n\n* b\n"
	for _, tc := range []struct {
		name  string
		alter func(*fix.Group, fix.RoundContext)
	}{
		{"wrong replacement", func(g *fix.Group, _ fix.RoundContext) { g.Edits[0].Replacement = []byte("+") }},
		{"non-marker byte", func(g *fix.Group, c fix.RoundContext) {
			s, _ := c.Current().Span(5, 6)
			g.Anchor = s
			g.Edits[0] = fix.Edit{Span: s, Expected: []byte("t"), Replacement: []byte("-")}
		}},
		{"merge lists", func(g *fix.Group, c fix.RoundContext) {
			// Turning the paragraph into list content would merge the lists.
			s, _ := c.Current().Span(5, 9)
			g.Edits[0] = fix.Edit{Span: s, Expected: []byte("text"), Replacement: []byte("- tx")}
		}},
		{"unknown rule", func(g *fix.Group, _ fix.RoundContext) { g.Rule = "MD005" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			altered := false
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := rules.Corrections(ctx, c)
				for i := range groups {
					if groups[i].Rule == "MD004" {
						tc.alter(&groups[i], c)
						altered = true
					}
				}
				return groups, err
			}
			view, out := bulletCorrected(t, source, "consistent", producer)
			if !altered || !out.Complete() || string(view.Candidate.Bytes()) != source {
				t.Fatalf("forged correction accepted: %q %+v", view.Candidate.Bytes(), view.Attempts)
			}
		})
	}
}

func TestBulletMarkerCheckerRejectsMergingEdit(t *testing.T) {
	// A forged group proposing the deferred edit is still refused on reparse.
	for _, source := range []string{"- a\n* b\n", "- a\n\n* b\n", "- a\n\ntext\n\n* - -\n"} {
		t.Run(source, func(t *testing.T) {
			producer := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
				at := strings.LastIndex(source, "* ")
				span, _ := c.Current().Span(at, at+1)
				return []fix.Group{{ID: "forged", Rule: "MD004", Provenance: "forged", Anchor: span, Edits: []fix.Edit{{Span: span, Expected: []byte("*"), Replacement: []byte("-")}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}}}}, nil
			}
			view, out := bulletCorrected(t, source, "consistent", producer)
			if !out.Complete() || string(view.Candidate.Bytes()) != source {
				t.Fatalf("merging correction accepted: %q", view.Candidate.Bytes())
			}
		})
	}
}

func TestBulletMarkerComposesWithOtherFixers(t *testing.T) {
	source := "##  Title  ##\n\n- a   \n\ntext\n\n> * c\n>\n> para\n\n+ d"
	want := "## Title ##\n\n- a\n\ntext\n\n> - c\n>\n> para\n\n- d\n"
	view, out := bulletCorrected(t, source, "consistent", rules.Corrections, "MD009", "MD019", "MD021", "MD047")
	if !out.Complete() || string(view.Candidate.Bytes()) != want || view.ChangingRounds != 1 || len(view.Attempts) != 2 {
		t.Fatalf("%q complete %v rounds %d attempts %+v", view.Candidate.Bytes(), out.Complete(), view.ChangingRounds, view.Attempts)
	}
	if len(view.Analysis.Diagnostics) != 0 {
		t.Fatal(view.Analysis.Diagnostics)
	}
	again, _ := bulletCorrected(t, want, "consistent", rules.Corrections, "MD009", "MD019", "MD021", "MD047")
	if string(again.Candidate.Bytes()) != want || again.ChangingRounds != 0 {
		t.Fatal("second run changed", again.ChangingRounds)
	}
}

func TestBulletMarkerDefersMD047WhileWaiting(t *testing.T) {
	// A merge-deferred bullet leaves no edit, so the MD047 group is proposed.
	view, _ := bulletCorrected(t, "- a\n* b", "consistent", rules.Corrections, "MD047")
	if string(view.Candidate.Bytes()) != "- a\n* b\n" {
		t.Fatalf("%q", view.Candidate.Bytes())
	}
	found := false
	for _, d := range view.Analysis.Diagnostics {
		found = found || d.Rule == "MD004" && !d.FixAvailable
	}
	if !found {
		t.Fatal(view.Analysis.Diagnostics)
	}
}
