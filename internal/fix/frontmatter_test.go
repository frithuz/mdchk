package fix_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
)

func frontmatterSetup(t *testing.T, text string) fixture {
	t.Helper()
	f := setup(t, text, nil, true)
	options, err := parser.NewFrontmatterOptions(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.parsed = parser.ParseWithOptions(context.Background(), f.parsed.Source, f.budget, parser.ParseOptions{Frontmatter: options})
	rules, err := f.registry.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	f.state = engine.ProcessDirectives(context.Background(), f.parsed, policy{rules, true}, f.budget)
	if !f.state.Complete() {
		t.Fatal(f.state.Err())
	}
	return f
}

func TestFrontmatterPlannerProtectsWholeHeader(t *testing.T) {
	text := "\ufeff---\r\ntitle: [broken\r\n---\r\nbody"
	for _, tc := range []struct {
		name       string
		start, end int
	}{
		{"BOM", 0, 3}, {"opener", 3, 6}, {"payload", 8, 13},
		{"closer", strings.LastIndex(text, "---"), strings.LastIndex(text, "---") + 3},
		{"prefix insertion", 0, 0}, {"interior insertion", 3, 3},
		{"crossing", strings.Index(text, "body") - 2, strings.Index(text, "body") + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := frontmatterSetup(t, text)
			p := plan(t, f, group(t, f, "header", edit(t, f, tc.start, tc.end, "changed")))
			if len(accepted(p)) != 0 || !reason(p, "header", "protected-syntax") || string(p.ProvisionalBytes()) != text {
				t.Fatal("opaque header edit accepted", p.Decisions())
			}
		})
	}
	for _, text := range []string{"---\nx: y\n---", "+++\nx = 1\n+++"} {
		f := frontmatterSetup(t, text)
		p := plan(t, f, group(t, f, "ending", edit(t, f, len(text), len(text), "\n")))
		if len(accepted(p)) != 0 || !reason(p, "ending", "protected-syntax") {
			t.Fatal("header-only EOF insertion accepted", p.Decisions())
		}
	}
}

func frontmatterSemanticSetup(t *testing.T, gfm bool) semanticFixture {
	t.Helper()
	f := semanticSetup(t, nil, nil)
	selection, err := f.registry.SelectRules(nil, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	options, err := parser.NewFrontmatterOptions(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	flavor := "commonmark"
	if gfm {
		flavor = "gfm"
	}
	f.plan, err = selection.Plan(engine.DocumentContext{Flavor: flavor, Frontmatter: options}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestFrontmatterCandidateSelectionAndPreservation(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		text := "\ufeff---\r\n<!-- mdchk:disable MD001 -->\r\nbroken: [\r\n---\r\nlast"
		headerBytes := text[:strings.Index(text, "last")]
		calls := 0
		producer := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
			calls++
			header, present := c.Parsed().Document.Frontmatter()
			if !present || header.Format() != "yaml" || header.BodyOffset() != len(headerBytes) {
				t.Fatal("recognition selection lost during reanalysis")
			}
			actual, err := c.Current().Text(header.Span())
			if err != nil || actual != headerBytes || len(c.Directives().Candidates()) != 0 {
				t.Fatal("header bytes/directive opacity lost", actual, err)
			}
			at := bytes.Index(c.Current().Bytes(), []byte("last"))
			if at < 0 {
				return nil, nil
			}
			return []fix.Group{syntheticGroup(t, c, at, at+4, "done")}, nil
		}
		checker := func(context.Context, fix.CheckContext) (fix.Proof, error) { return fix.Preserved, nil }
		out, err := validateSynthetic(t, frontmatterSemanticSetup(t, gfm), text, producer, checker)
		if err != nil {
			t.Fatal(err)
		}
		file, _ := out.File("a.md")
		if !out.Complete() || string(file.Candidate.Bytes()) != headerBytes+"done" || calls < 2 || file.ChangingRounds != 1 {
			t.Fatal("body candidate did not converge with opaque header", file, calls)
		}
		if _, present := file.Parsed().Document.Frontmatter(); !present {
			t.Fatal("final candidate lost header")
		}
	}
}

func TestFrontmatterCandidateCannotIntroduceHeader(t *testing.T) {
	producer := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
		return []fix.Group{syntheticGroup(t, c, 0, 0, "---\nx: y\n---\n")}, nil
	}
	checker := func(context.Context, fix.CheckContext) (fix.Proof, error) { return fix.Preserved, nil }
	out, err := validateSynthetic(t, frontmatterSemanticSetup(t, false), "body", producer, checker)
	if err != nil {
		t.Fatal(err)
	}
	file, _ := out.File("a.md")
	if !out.Complete() || string(file.Candidate.Bytes()) != "body" || len(file.Attempts) == 0 {
		t.Fatal("ordinary correction introduced metadata recognition", file)
	}
	if file.Attempts[0].Decisions[0].Accepted || file.Attempts[0].Decisions[0].Reasons[0].Code != "candidate-preservation-failed" {
		t.Fatal("independent header binding check missing", file.Attempts)
	}
}

func TestFrontmatterCandidateCanInsertAtBodyBoundary(t *testing.T) {
	const header = "---\nx: y\n---\n"
	producer := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
		if strings.HasPrefix(string(c.Current().Bytes()), header+"\n") {
			return nil, nil
		}
		return []fix.Group{syntheticGroup(t, c, len(header), len(header), "\n")}, nil
	}
	checker := func(context.Context, fix.CheckContext) (fix.Proof, error) { return fix.Preserved, nil }
	out, err := validateSynthetic(t, frontmatterSemanticSetup(t, false), header+"body", producer, checker)
	if err != nil {
		t.Fatal(err)
	}
	file, _ := out.File("a.md")
	if !out.Complete() || string(file.Candidate.Bytes()) != header+"\nbody" {
		t.Fatal("body-boundary insertion was treated as opaque metadata", file)
	}
	h, present := file.Parsed().Document.Frontmatter()
	if !present || h.BodyOffset() != len(header) {
		t.Fatal("body-boundary insertion changed header binding", h, present)
	}
}

func TestFrontmatterUnterminatedCandidateWithheld(t *testing.T) {
	calls := 0
	producer := func(context.Context, fix.RoundContext) ([]fix.Group, error) { calls++; return nil, nil }
	text := "---\nx: y"
	out, err := validateSynthetic(t, frontmatterSemanticSetup(t, false), text, producer, nil)
	if err != nil {
		t.Fatal(err)
	}
	file, _ := out.File("a.md")
	if out.Complete() || string(file.Candidate.Bytes()) != text || calls != 0 {
		t.Fatal("unterminated header became writable", file, calls)
	}
	if header, present := file.Parsed().Document.Frontmatter(); !present || header.Closer().Present() || file.Parsed().Status != parser.Incomplete {
		t.Fatal("unterminated record lost", file.Parsed())
	}
}
