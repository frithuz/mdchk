package parser_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

// Block fixtures retain their independent byte/marker/structure oracles. Their
// former "not inline-free" flag no longer implies pending text; dedicated inline
// fixtures and the complete corpus ledger assert exactly which input completes.
func checkStagedTextOutcome(t *testing.T, r parser.ParseResult, mustComplete bool) {
	t.Helper()
	if r.Failure != nil || r.Interruption != "" || r.Document == nil {
		t.Fatal(r)
	}
	refs := r.Document.References()
	if refs.Availability != parser.Available || refs.LookupAvailability != parser.Available {
		t.Fatal("completed block scan lost references", refs)
	}
	if mustComplete || r.Status == parser.Complete {
		if r.Status != parser.Complete || r.AST != parser.Available || len(r.Uncertainty) != 0 || r.Phases != (parser.PhaseProgress{Blocks: parser.PhaseComplete, Inlines: parser.PhaseComplete}) {
			t.Fatal("complete text lacks affirmative handoff", r)
		}
	} else if r.Status != parser.Incomplete || r.AST != parser.Partial || len(r.Uncertainty) != 1 || r.Uncertainty[0].Reason != parser.SyntaxUnimplemented || r.Uncertainty[0].Region.Start() != 0 || r.Uncertainty[0].Region.End() != r.Source.Len() || r.Phases.Blocks != parser.PhaseComplete || r.Phases.Inlines != parser.Pending {
		t.Fatal("pending syntax lacks uncertainty", r)
	}
}

func checkInlineText(t *testing.T, r parser.ParseResult, block *parser.Node, literal string, ranges []byteRange) {
	t.Helper()
	children := block.Children()
	if r.Phases.Inlines == parser.NotStarted || len(ranges) == 0 {
		if len(children) != 0 {
			t.Fatal("inline work before its handoff", children)
		}
		return
	}
	var raw strings.Builder
	bytes := r.Source.Bytes()
	for _, bounds := range ranges {
		raw.Write(bytes[bounds.start:bounds.end])
	}
	if oracleText(raw.String(), entityOracle(t)) != oracleText(literal, entityOracle(t)) {
		t.Fatal("block fixture logical bytes differ", raw.String(), literal)
	}
	wants := fixtureBreakInlines(t, r.Source, ranges)
	checkInlineNodes(t, r.Source, children, wants)
	for i, want := range wants {
		if want.kind == parser.Text {
			assertDecodedParts(t, r.Source, children[i].Content(), want.payload, entityOracle(t))
		}
	}
}

func checkCorpusTextOutcome(t *testing.T, id int, html string, r parser.ParseResult) {
	t.Helper()
	data, err := os.ReadFile("testdata/commonmark/0.31.2/foundation.json")
	if err != nil {
		t.Fatal(err)
	}
	var ledger struct {
		Supported []int `json:"supported_html_examples"`
	}
	if err := json.Unmarshal(data, &ledger); err != nil {
		t.Fatal(err)
	}
	supported := false
	for _, n := range ledger.Supported {
		supported = supported || n == id
	}
	out := conformance.Evaluate(r)
	if supported {
		if out.Status != "rendered" || out.HTML != html {
			t.Fatalf("example %d: %+v want %q", id, out, html)
		}
	} else if out.Status != "pending" || out.HTML != "" {
		t.Fatalf("example %d: unsupported render %+v", id, out)
	}
}

func TestInlineLiteralAndProviderBoundaries(t *testing.T) {
	for _, text := range []string{"hello", "Foo χρῆν", "Multiple     spaces", "a * b", "a _ b", "a ` b", "a [ b", "a ] b", "a < b", "a & b", "a \\b", "~~strike~~", "é\x00"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(text)))
		checkStagedTextOutcome(t, r, true)
		literal := text
		if text == "é\x00" {
			literal = "é\ufffd"
		}
		if children := r.Document.Root().Children()[0].Children(); len(children) != 1 || children[0].Properties().Literal != parser.Some(literal) {
			t.Fatal(text, children)
		}
	}
	for _, text := range []string{"*a*", "a_b_c", "**a**", "[a](b)", "[a]", "![a](b)", "<a@b.c>", "<123@a.b>", "<https://a>", "<em>x</em>"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(text)))
		if r.Status != parser.Incomplete || r.Phases.Inlines != parser.Pending || len(r.Uncertainty) != 1 || conformance.Evaluate(r).Status != "pending" {
			t.Fatal("missing provider became complete", text, r)
		}
	}
}

func TestInlineSegmentedSourceAndDetachedAccessors(t *testing.T) {
	for _, tt := range []struct {
		input, literal string
		ranges         []byteRange
	}{
		{"> a\r\n> b", "a\r\nb", []byteRange{{2, 5}, {7, 8}}},
		{"> a\nb", "a\nb", []byteRange{{2, 4}, {4, 5}}},
		{"> - x\r\n>   y", "x\r\ny", []byteRange{{4, 7}, {11, 12}}},
		{"# é #\n", "é", []byteRange{{2, 4}}},
		{"é\n==\n", "é", []byteRange{{0, 2}}},
		{"a\x00β", "a\ufffdβ", []byteRange{{0, 1}, {1, 2}, {2, 4}}},
	} {
		t.Run(strconv.Quote(tt.input), func(t *testing.T) {
			source := parser.NewSource([]byte(tt.input))
			r := parser.Parse(context.Background(), source)
			blocks := 0
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() != parser.Paragraph && n.Kind() != parser.Heading {
					return true
				}
				blocks++
				checkInlineText(t, r, n, tt.literal, tt.ranges)
				child := n.Children()[0]
				child.Content()[0].Semantic = parser.Some("mutated")
				*child = parser.Node{}
				return true
			})
			if blocks != 1 || string(source.Bytes()) != tt.input || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
				t.Fatal("Text ownership or repeatability", r)
			}
		})
	}
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		input := "> a" + ending + "> b" + ending
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		paragraph := r.Document.Root().Children()[0].Children()[0]
		checkInlineText(t, r, paragraph, "a"+ending+"b", []byteRange{{2, 3 + len(ending)}, {5 + len(ending), 6 + len(ending)}})
		if r.Status != parser.Complete {
			t.Fatal("break provider did not complete")
		}
	}
}

func TestInlineUpstreamTargets(t *testing.T) {
	// Expected original byte slices and semantic text are independent of Parse.
	type leaf struct {
		text string
		span byteRange
	}
	wants := map[int][]leaf{
		4:   {{"foo", byteRange{4, 7}}, {"bar", byteRange{10, 13}}},
		5:   {{"foo", byteRange{2, 5}}},
		9:   {{"foo", byteRange{3, 6}}, {"bar", byteRange{12, 15}}, {"baz", byteRange{20, 23}}},
		10:  {{"Foo", byteRange{2, 5}}},
		13:  {{"\\\t\\A\\a\\ \\3\\φ\\«", byteRange{0, 16}}},
		42:  {{"`one", byteRange{2, 6}}, {"two`", byteRange{9, 13}}},
		327: {{"lo`", byteRange{4, 7}}},
		347: {{"```foo``", byteRange{0, 8}}}, 348: {{"`foo", byteRange{0, 4}}},
		365: {{"_foo*", byteRange{0, 5}}},
		420: {{"** is not an empty emphasis", byteRange{0, 27}}},
		421: {{"**** is not an empty strong emphasis", byteRange{0, 36}}},
		434: {{"__ is not an empty emphasis", byteRange{0, 27}}},
		435: {{"____ is not an empty strong emphasis", byteRange{0, 36}}},
		436: {{"foo ***", byteRange{0, 7}}}, 439: {{"foo *****", byteRange{0, 9}}},
		448: {{"foo ___", byteRange{0, 7}}}, 451: {{"foo _____", byteRange{0, 9}}},
		611: {{"https://example.com", byteRange{0, 19}}},
		612: {{"foo@bar.example.com", byteRange{0, 19}}},
		644: {{"foo\\", byteRange{0, 4}}}, 645: {{"foo", byteRange{0, 3}}},
		646: {{"foo\\", byteRange{4, 8}}}, 647: {{"foo", byteRange{4, 7}}},
		650: {{"hello $.;'there", byteRange{0, 15}}},
		651: {{"Foo χρῆν", byteRange{0, 13}}},
		652: {{"Multiple     spaces", byteRange{0, 19}}},
	}
	data, err := os.ReadFile("testdata/commonmark/0.31.2/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		ID             int `json:"example"`
		Markdown, HTML string
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range cases {
		leaves, ok := wants[e.ID]
		if !ok {
			continue
		}
		seen++
		t.Run(strconv.Itoa(e.ID), func(t *testing.T) {
			r := parser.Parse(context.Background(), parser.NewSource([]byte(e.Markdown)))
			index := 0
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() != parser.Text {
					return true
				}
				if index >= len(leaves) {
					t.Fatal("extra Text")
				}
				w := leaves[index]
				index++
				if n.Span().Start() != w.span.start || n.Span().End() != w.span.end || n.Properties().Literal != parser.Some(w.text) {
					t.Fatalf("Text %d: %+v want %+v", index, n, w)
				}
				if len(n.Content()) != 1 || n.Content()[0].Source.Start() != w.span.start || n.Content()[0].Source.End() != w.span.end || n.Content()[0].Transformation != parser.Exact {
					t.Fatal("Text source provenance", n.Content())
				}
				return true
			})
			if index != len(leaves) {
				t.Fatal("missing Text", index)
			}
			if got := conformance.Evaluate(r); got.Status != "rendered" || got.HTML != e.HTML {
				t.Fatal(got, e.HTML)
			}
		})
	}
	if seen != len(wants) {
		t.Fatal("missing unchanged upstream targets")
	}
}

func TestInlineOpaqueBlocksAndLocalState(t *testing.T) {
	input := "> ```\n> *text* &amp;\n> ```\n>\n> <!-- *text* -->\n"
	r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
	checkStagedTextOutcome(t, r, true)
	opaque := 0
	parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
		if n.Kind() == parser.CodeBlock || n.Kind() == parser.HTMLBlock {
			opaque++
			if len(n.Children()) != 0 {
				t.Fatal("opaque block acquired inlines")
			}
		}
		return true
	})
	if opaque != 2 {
		t.Fatal("missing protected payloads")
	}
	for _, input := range []string{"*open\n\nclose*", "- `one\n- two`", "[open\n\nclose]"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
	}
}

func FuzzInlineTextProvenance(f *testing.F) {
	entities := entityOracle(f)
	for _, input := range []string{"hello", "> a\r\n> b", "# é\x00 #\n", "[a](b)", "`unmatched", "*a*", "&amp;", "é&amp;β", `\&ouml;`, "&NotEqualTilde;", "&#0;", "&#128;", "&#xD800;", "` x `", "a`&amp;\\*`b", "> - `a\r\n>   b`", "`foo``bar``", "`é\x00`", "a  \nb", "a \r\nb", "a\\\rb", "> - é\x00  \r\n>   β", "&#10;\n&#92;"} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 8192 {
			t.Skip()
		}
		source := parser.NewSource([]byte(input))
		r := parser.Parse(context.Background(), source)
		if string(source.Bytes()) != input || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
			t.Fatal("source mutation or nondeterministic state")
		}
		if r.Failure != nil {
			if r.Failure.Code != parser.InvalidUTF8 {
				t.Fatal(r.Failure)
			}
			return
		}
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.CodeSpan {
				checkCodeProvenance(t, source, n)
				return true
			}
			if n.Kind() == parser.SoftBreak || n.Kind() == parser.HardBreak {
				marker := byteRange{n.Span().Start(), n.Content()[0].Source.Start()}
				checkBreakProperties(t, source, n, n.Properties().Break.Origin, marker)
				return true
			}
			if n.Kind() != parser.Text {
				return true
			}
			var literal strings.Builder
			for _, part := range n.Content() {
				raw, err := source.Text(part.Source)
				if err != nil || !part.Semantic.Present {
					t.Fatal("foreign/unmapped Text", part, err)
				}
				want := raw
				if part.Transformation == parser.Normalized {
					want = oracleNormalizedText(t, source, part)
				} else if part.Transformation == parser.Escape || part.Transformation == parser.Entity {
					want = oracleText(raw, entities)
				} else if part.Transformation != parser.Exact {
					t.Fatal("invented transform", part)
				}
				if part.Semantic.Value != want {
					t.Fatal("original-byte mapping differs", part, raw)
				}
				literal.WriteString(want)
			}
			if n.Properties().Literal != parser.Some(literal.String()) {
				t.Fatal("literal not derived from source contributions")
			}
			return true
		})
	})
}
