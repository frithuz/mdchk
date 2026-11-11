package md089_test

import (
	"context"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/md089"
)

func md089FixSettings(enabled bool) engine.Settings {
	s := md089Settings()
	s["MD089"] = engine.RuleSetting{Enabled: &enabled, Options: engine.Options{}}
	return s
}

func md089Corrected(t *testing.T, flavor, text string, producer fix.Producer) fix.FileView {
	t.Helper()
	r := registry(t, rules.Registrations())
	selection, err := r.SelectRules(md089FixSettings(true), engine.ValidationProviders{})
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

// md089Producer runs only MD089's part of Corrections.
func md089Producer(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
	analysis, err := c.BoundedAnalysis(ctx)
	if err != nil {
		return nil, err
	}
	var findings []result.Diagnostic
	for _, d := range analysis.Diagnostics {
		if d.Rule == "MD089" && d.FixAvailable {
			findings = append(findings, d)
		}
	}
	return md089.Correction(ctx, c, findings)
}

func md089Analysis(t *testing.T, flavor, text string) result.Result {
	t.Helper()
	r := registry(t, rules.Registrations())
	return analyzeHeadings(t, r, text, md089Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
}

// md089Shape returns the node kinds and Text values of a GFM parse, with every
// SPACE removed from the Text values.
func md089Shape(t *testing.T, text string) (string, string) {
	t.Helper()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithOptions(context.Background(), parser.NewSource([]byte(text)), budget, engine.DocumentContext{Flavor: "gfm"}.ParseOptions())
	if parsed.Document == nil || parsed.Status != parser.Complete {
		t.Fatal("parse", parsed.Status)
	}
	var kinds, texts []string
	var walk func(n *parser.Node)
	walk = func(n *parser.Node) {
		kinds = append(kinds, string(n.Kind()))
		if n.Kind() == parser.Text {
			texts = append(texts, strings.ReplaceAll(n.Properties().Literal.Value, " ", ""))
		}
		for _, c := range n.Children() {
			walk(c)
		}
	}
	walk(parsed.Document.Root())
	return strings.Join(kinds, ","), strings.Join(texts, "|")
}

func TestMD089FixInsertions(t *testing.T) {
	for _, tc := range []struct {
		source, want string
		offsets      []int
	}{
		{"版本2\n", "版本 2\n", []int{6}},
		{"中A\n", "中 A\n", []int{3}},
		{"A中\n", "A 中\n", []int{1}},
		{"A中2b版\n", "A 中 2b 版\n", []int{1, 4, 6}},
		{"中A\r\nB中\r\n", "中 A\r\nB 中\r\n", []int{3, 7}},
		{"\U00020000A\n", "\U00020000 A\n", []int{4}},
		{"中é\n", "中 é\n", []int{3}},
		{"中Ａ\n", "中 Ａ\n", []int{3}},
	} {
		t.Run(tc.source, func(t *testing.T) {
			var offsets []int
			for _, d := range md089Analysis(t, "gfm", tc.source).Diagnostics {
				if d.Rule == "MD089" {
					if !d.FixAvailable {
						t.Fatal("not fixable", d)
					}
					offsets = append(offsets, d.ByteRange.Start)
				}
			}
			view := md089Corrected(t, "gfm", tc.source, md089Producer)
			if got := string(view.Candidate.Bytes()); got != tc.want {
				t.Fatalf("got %q want %q: %+v", got, tc.want, view.Attempts)
			}
			decisions := view.Attempts[0].Decisions
			if len(decisions) != len(tc.offsets) || len(offsets) != len(tc.offsets) {
				t.Fatal(decisions, offsets)
			}
			for i, d := range decisions {
				g := d.Group
				if !d.Accepted || g.Rule != "MD089" || !strings.HasPrefix(g.ID, "MD089-") || len(g.Edits) != 1 || g.Edits[0].Span != g.Anchor || g.Anchor.Start() != tc.offsets[i] || g.Anchor.End() != tc.offsets[i] || offsets[i] != tc.offsets[i] || len(g.Edits[0].Expected) != 0 || string(g.Edits[0].Replacement) != " " {
					t.Fatalf("%+v", d)
				}
			}
			for _, d := range md089Analysis(t, "gfm", tc.want).Diagnostics {
				if d.Rule == "MD089" {
					t.Fatal("finding after fix", d)
				}
			}
			again := md089Corrected(t, "gfm", tc.want, rules.Corrections)
			if string(again.Candidate.Bytes()) != tc.want {
				t.Fatal("not idempotent", string(again.Candidate.Bytes()))
			}
		})
	}
}

func TestMD089FixBarriersGiveNoEdit(t *testing.T) {
	for _, source := range []string{"中́A\n", "中︀A\n", "Á中\n"} {
		t.Run(source, func(t *testing.T) {
			for _, d := range md089Analysis(t, "gfm", source).Diagnostics {
				if d.Rule == "MD089" {
					t.Fatal("barrier reported", d)
				}
			}
			if got := string(md089Corrected(t, "gfm", source, rules.Corrections).Candidate.Bytes()); got != source {
				t.Fatalf("%q", got)
			}
		})
	}
}

func TestMD089FixProtectedContentStays(t *testing.T) {
	for _, source := range []string{
		"<https://x/中A>\n",
		"https://example.com/中A\n",
		"`中A`\n",
		"[l](/中A \"中A\")\n",
		"```\n中A\n```\n",
		"<span title=\"中A\">x</span>\n",
		"<div>\n中A\n</div>\n",
		"---\ntitle: 中A\n---\n\nx\n",
		"+++\ntitle = \"中A\"\n+++\n\nx\n",
	} {
		t.Run(source, func(t *testing.T) {
			view := md089Corrected(t, "gfm", source, rules.Corrections)
			if got := string(view.Candidate.Bytes()); got != source {
				t.Fatalf("%q", got)
			}
			for _, a := range view.Attempts {
				for _, d := range a.Decisions {
					if d.Group.Rule == "MD089" {
						t.Fatalf("protected content got a group: %+v", d)
					}
				}
			}
		})
	}
}

func TestMD089FixContainers(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"- 中A\n", "- 中 A\n"},
		{"> 中A\n", "> 中 A\n"},
		{"[中A](/x)\n", "[中 A](/x)\n"},
		{"![中A](/x.png)\n", "![中 A](/x.png)\n"},
		{"| a | b |\n| - | - |\n| 中A | x |\n", "| a | b |\n| - | - |\n| 中 A | x |\n"},
		{"[中A][r]\n\n[r]: /u\n", "[中 A][r]\n\n[r]: /u\n"},
		{"- [ ] 中A\n", "- [ ] 中 A\n"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			got := string(md089Corrected(t, "gfm", tc.source, rules.Corrections).Candidate.Bytes())
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
			beforeKinds, beforeTexts := md089Shape(t, tc.source)
			afterKinds, afterTexts := md089Shape(t, got)
			if beforeKinds != afterKinds || beforeTexts != afterTexts {
				t.Fatal(beforeKinds, afterKinds, beforeTexts, afterTexts)
			}
			for _, d := range md089Analysis(t, "gfm", got).Diagnostics {
				if d.Rule == "MD089" {
					t.Fatal("finding after fix", d)
				}
			}
		})
	}
}

// Heading text derives an anchor and a collapsed or shortcut label selects a
// definition, so those points stay manual and never block a safe sibling.
func TestMD089NamingTextStaysManual(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"# 版本2\n\nSee [v](#版本2).\n", "# 版本2\n\nSee [v](#版本2).\n"},
		{"中A\n===\n", "中A\n===\n"},
		{"Safe 中A here.\n\nSee [中A].\n\n[中A]: /u\n", "Safe 中 A here.\n\nSee [中A].\n\n[中A]: /u\n"},
		{"x 中A [中A][] y\n\n[中A]: /u\n", "x 中 A [中A][] y\n\n[中A]: /u\n"},
		{"![中A]\n\n[中A]: /i.png\n", "![中A]\n\n[中A]: /i.png\n"},
		{"Note 中A[^中A].\n\n[^中A]: /n\n", "Note 中 A[^中A].\n\n[^中A]: /n\n"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			fixable := 0
			for _, d := range md089Analysis(t, "gfm", tc.source).Diagnostics {
				if d.Rule == "MD089" && d.FixAvailable {
					fixable++
				}
			}
			view := md089Corrected(t, "gfm", tc.source, rules.Corrections)
			if got := string(view.Candidate.Bytes()); got != tc.want {
				t.Fatalf("got %q want %q: %+v", got, tc.want, view.Attempts)
			}
			accepted := 0
			for _, a := range view.Attempts {
				for _, d := range a.Decisions {
					if d.Group.Rule == "MD089" && !d.Accepted {
						t.Fatalf("deferred group: %+v", d)
					}
					if d.Group.Rule == "MD089" {
						accepted++
					}
				}
			}
			if accepted != fixable {
				t.Fatal("fix_available disagrees with fix", accepted, fixable)
			}
			manual := 0
			for _, d := range view.Analysis.Diagnostics {
				if d.Rule == "MD089" {
					manual++
					if d.FixAvailable {
						t.Fatal("manual finding fixable", d)
					}
				}
			}
			if manual == 0 {
				t.Fatal("manual finding missing", view.Analysis.Diagnostics)
			}
		})
	}
}

func TestMD089FixDirectiveRegionStays(t *testing.T) {
	source := "中A\n\n<!-- mdchk:disable MD089 -->\n\n中B\n\n<!-- mdchk:enable MD089 -->\n\n中C\n"
	want := "中 A\n\n<!-- mdchk:disable MD089 -->\n\n中B\n\n<!-- mdchk:enable MD089 -->\n\n中 C\n"
	view := md089Corrected(t, "gfm", source, rules.Corrections)
	if got := string(view.Candidate.Bytes()); got != want {
		t.Fatalf("%q %+v", got, view.Attempts)
	}
}

func TestMD089FixDisabledRuleLeavesFile(t *testing.T) {
	r := registry(t, rules.Registrations())
	selection, err := r.SelectRules(md089FixSettings(false), engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: "gfm"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	v, err := fix.NewValidator(r, rules.CorrectionCheckers())
	if err != nil {
		t.Fatal(err)
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	out, err := v.Validate(context.Background(), []fix.Target{{Path: "doc.md", Source: parser.NewSource([]byte("中A\n")), Plan: plan, Produce: rules.Corrections}}, fix.ValidationOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	if view, _ := out.File("doc.md"); string(view.Candidate.Bytes()) != "中A\n" {
		t.Fatal("disabled rule edited", string(view.Candidate.Bytes()))
	}
}

func TestMD089CheckerRejectsUnprovenEdits(t *testing.T) {
	for name, tamper := range map[string]func(c fix.RoundContext, g *fix.Group) error{
		"shifted span": func(c fix.RoundContext, g *fix.Group) error {
			s, err := c.Current().Span(g.Anchor.Start()-3, g.Anchor.Start()-3)
			g.Anchor, g.Edits[0].Span = s, s
			return err
		},
		"two spaces": func(c fix.RoundContext, g *fix.Group) error {
			g.Edits[0].Replacement = []byte("  ")
			return nil
		},
		"fullwidth space": func(c fix.RoundContext, g *fix.Group) error {
			g.Edits[0].Replacement = []byte("　")
			return nil
		},
		"no-break space": func(c fix.RoundContext, g *fix.Group) error {
			g.Edits[0].Replacement = []byte(" ")
			return nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				groups, err := md089Producer(ctx, c)
				for i := range groups {
					if err == nil && len(groups[i].Edits) == 1 {
						err = tamper(c, &groups[i])
					}
				}
				return groups, err
			}
			view := md089Corrected(t, "gfm", "x 中A\n", producer)
			if got := string(view.Candidate.Bytes()); got != "x 中A\n" {
				t.Fatalf("%q", got)
			}
			for _, a := range view.Attempts {
				for _, d := range a.Decisions {
					if d.Accepted {
						t.Fatalf("tampered group accepted: %+v", d)
					}
				}
			}
		})
	}
}

// A rejected group is deferred and its finding stays reported; a document
// opening with an unrecognized block gets no group and fix_available false.
func TestMD089DeferredFindingsStayReported(t *testing.T) {
	producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
		groups, err := md089Producer(ctx, c)
		for i := range groups {
			groups[i].Edits[0].Replacement = []byte("  ")
		}
		return groups, err
	}
	view := md089Corrected(t, "gfm", "中A\n", producer)
	decisions := view.Attempts[0].Decisions
	if got := string(view.Candidate.Bytes()); got != "中A\n" || len(decisions) != 1 || decisions[0].Accepted || len(decisions[0].Reasons) == 0 {
		t.Fatalf("%q %+v", got, decisions)
	}
	if len(view.Analysis.Diagnostics) != 1 || view.Analysis.Diagnostics[0].Rule != "MD089" {
		t.Fatal(view.Analysis.Diagnostics)
	}
	source := "---\n中A\n---\n\n中B\n"
	guarded := md089Corrected(t, "gfm", source, rules.Corrections)
	if got := string(guarded.Candidate.Bytes()); got != source || len(guarded.Analysis.Diagnostics) == 0 {
		t.Fatalf("%q %+v", got, guarded.Analysis.Diagnostics)
	}
	for _, d := range guarded.Analysis.Diagnostics {
		if d.Rule != "MD089" || d.FixAvailable {
			t.Fatal(d)
		}
	}
}
