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

// These proposals come from no production rule: Plan protects the document by
// edit shape alone, so a new producer that skips eligibility is still stopped.
func TestPlannerProtectsDocumentOpeningWithUnrecognizedBlock(t *testing.T) {
	block := "---\n#  x\n---\n\nbody"
	for _, tc := range []struct {
		name, text, replacement string
		start, end              int
		want                    bool
	}{
		{"replacement in block", block, " ", 5, 7, false},
		{"replacement in body", block, "text", len(block) - 4, len(block), false},
		{"replacement at eof", block, "\n", len(block) - 1, len(block), false},
		{"prefix insertion", block, "\n", 0, 0, false},
		{"interior line ending", block, "\n", 14, 14, false},
		{"spaces removed at eof", block + "  ", "", len(block), len(block) + 2, false},
		{"spaces and ending removed at eof", block + " \n", "", len(block), len(block) + 2, false},
		{"ending deleted before eof", block + "\n\nx", "", len(block), len(block) + 1, false},
		{"ending replaced at eof", block + "\n\n", "\n", len(block), len(block) + 2, false},
		{"text inserted at eof", block, "x", len(block), len(block), false},
		{"two endings inserted at eof", block, "\n\n", len(block), len(block), false},
		{"ending inserted at eof", block, "\n", len(block), len(block), true},
		{"crlf inserted at eof", block, "\r\n", len(block), len(block), true},
		{"cr inserted at eof", block, "\r", len(block), len(block), true},
		{"ending inserted after closer", "---\nt: y\n---", "\n", 12, 12, true},
		{"excess endings deleted", block + "\n\n\n", "", len(block) + 1, len(block) + 3, true},
		{"excess crlf deleted", block + "\r\n\r\n", "", len(block) + 2, len(block) + 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t, tc.text, nil, true)
			if !f.parsed.Document.UnrecognizedBlock() {
				t.Fatal("fixture is not an unrecognized block")
			}
			p := plan(t, f, group(t, f, "new", edit(t, f, tc.start, tc.end, tc.replacement)))
			if (len(accepted(p)) == 1) != tc.want || reason(p, "new", "protected-syntax") == tc.want {
				t.Fatal("wrong decision", p.Decisions())
			}
			if !tc.want && string(p.ProvisionalBytes()) != tc.text {
				t.Fatal("deferred edit changed bytes")
			}
		})
	}
	// Bytes that are all line endings up to EOF still can't split a CRLF: the shared
	// boundary check rejects the span before the terminal shape is considered.
	for _, tc := range []struct {
		name, text string
		start      int
	}{
		{"final crlf", block + "\r\n", len(block) + 1},
		{"last of excess crlf", block + "\r\n\r\n", len(block) + 3},
		{"from inside first crlf", block + "\r\n\r\n", len(block) + 1},
	} {
		t.Run("deletion splitting "+tc.name, func(t *testing.T) {
			f := setup(t, tc.text, nil, true)
			if !f.parsed.Document.UnrecognizedBlock() {
				t.Fatal("fixture is not an unrecognized block")
			}
			p := plan(t, f, group(t, f, "new", edit(t, f, tc.start, len(tc.text), "")))
			if len(accepted(p)) != 0 || !reason(p, "new", "invalid-span") || string(p.ProvisionalBytes()) != tc.text {
				t.Fatal("CRLF interior deletion accepted", p.Decisions())
			}
		})
	}
	for _, text := range []string{"---\n\nbody", "\n---\nx\n---\nbody", "---\nx\n+++\nbody"} {
		f := setup(t, text, nil, true)
		at := strings.Index(text, "body")
		p := plan(t, f, group(t, f, "new", edit(t, f, at, at+4, "text")))
		if len(accepted(p)) != 1 {
			t.Fatal("document without a complete leading block protected", text, p.Decisions())
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
