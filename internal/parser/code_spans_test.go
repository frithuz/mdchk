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

type inlineWant struct {
	kind                   parser.Kind
	span, opening, closing byteRange
	literal                string
	payload                []byteRange
	origin                 string
}

func codeWant(start, openEnd, closeStart, end int, literal string, payload ...byteRange) inlineWant {
	return inlineWant{kind: parser.CodeSpan, span: byteRange{start, end}, opening: byteRange{start, openEnd}, closing: byteRange{closeStart, end}, literal: literal, payload: payload}
}
func textWant(start, end int, literal string) inlineWant {
	return inlineWant{kind: parser.Text, span: byteRange{start, end}, literal: literal, payload: []byteRange{{start, end}}}
}
func withInlines(w atxBlockWant, inlines ...inlineWant) atxBlockWant {
	w.inlines = inlines
	return w
}

// The fixtures supply original offsets and semantics independently. A provider
// may split ranges further for transformations, but cannot omit a trimmed byte,
// include a prefix/delimiter, decode opaque code or invent a source contribution.
func checkInlineNodes(t *testing.T, source parser.Source, nodes []*parser.Node, wants []inlineWant) {
	t.Helper()
	if len(nodes) != len(wants) {
		t.Fatalf("inline count %d want %d", len(nodes), len(wants))
	}
	for i, w := range wants {
		n := nodes[i]
		literal := parser.Some(w.literal)
		if w.kind == parser.SoftBreak || w.kind == parser.HardBreak || w.kind == parser.InlineHTML {
			literal = parser.Optional[string]{}
		}
		if n.Kind() != w.kind || n.Span().Start() != w.span.start || n.Span().End() != w.span.end || n.Properties().Literal != literal || len(n.Children()) != 0 {
			t.Fatalf("inline %d: kind=%s span=%v literal=%+v want %+v", i, n.Kind(), n.Span(), n.Properties().Literal, w)
		}
		if w.kind == parser.CodeSpan {
			p := n.Properties().Delimiter
			m := n.Markers()
			if p == nil || p.Marker != '`' || p.Width != w.opening.end-w.opening.start || len(m) != 2 || m[0].Role != "opening" || m[1].Role != "closing" || m[0].Source.Start() != w.opening.start || m[0].Source.End() != w.opening.end || m[1].Source.Start() != w.closing.start || m[1].Source.End() != w.closing.end {
				t.Fatal("delimiter provenance", p, m, w)
			}
		} else if w.kind == parser.SoftBreak || w.kind == parser.HardBreak {
			checkBreakProperties(t, source, n, w.origin, w.opening)
		} else if w.kind == parser.InlineHTML {
			checkInlineHTMLProvenance(t, source, n)
		} else if n.Properties().Delimiter != nil || len(n.Markers()) != 0 {
			t.Fatal("Text acquired code markers")
		}
		index, offset := 0, w.payload[0].start
		var semantic strings.Builder
		for _, part := range n.Content() {
			if index >= len(w.payload) || part.Source.Start() != offset || part.Source.End() > w.payload[index].end || !part.Semantic.Present {
				t.Fatal("lost/foreign payload bytes", part, w)
			}
			raw, err := source.Text(part.Source)
			if err != nil {
				t.Fatal(err)
			}
			if part.Transformation == parser.Exact && part.Semantic.Value != raw {
				t.Fatal("semantic change without provenance", part)
			}
			if w.kind == parser.CodeSpan && part.Transformation != parser.Exact && part.Transformation != parser.Normalized && part.Transformation != parser.PartialTab {
				t.Fatal("code payload decoded", part)
			}
			semantic.WriteString(part.Semantic.Value)
			offset = part.Source.End()
			if offset == w.payload[index].end {
				index++
				if index < len(w.payload) {
					offset = w.payload[index].start
				}
			}
		}
		if index != len(w.payload) || semantic.String() != w.literal {
			t.Fatal("payload coverage or contributions", n.Content(), w)
		}
	}
}

func TestCodeSpanUpstreamAcceptanceAndNewPasses(t *testing.T) {
	// Independently counted original-byte ranges. Text and code are separate
	// assertions; HTML agreement alone cannot establish source preservation.
	wants := map[int][]inlineWant{
		17:  {codeWant(0, 2, 8, 10, `\[\`+"`", byteRange{2, 8})},
		35:  {codeWant(0, 1, 14, 15, "f&ouml;&ouml;", byteRange{1, 14})},
		121: {codeWant(0, 2, 7, 9, "foo", byteRange{2, 3}, byteRange{3, 7})},
		327: {codeWant(0, 1, 3, 4, "hi", byteRange{1, 3}), textWant(4, 7, "lo`")},
		328: {codeWant(0, 1, 4, 5, "foo", byteRange{1, 4})},
		329: {codeWant(0, 2, 13, 15, "foo ` bar", byteRange{2, 13})},
		330: {codeWant(0, 1, 5, 6, "``", byteRange{1, 5})},
		331: {codeWant(0, 1, 7, 8, " `` ", byteRange{1, 7})},
		332: {codeWant(0, 1, 3, 4, " a", byteRange{1, 3})},
		333: {codeWant(0, 1, 6, 7, "\u00a0b\u00a0", byteRange{1, 6})},
		334: {codeWant(0, 1, 3, 4, "\u00a0", byteRange{1, 3}), breakWant(4, 4, 5, "line-ending", "\n"), codeWant(5, 6, 8, 9, "  ", byteRange{6, 8})},
		335: {codeWant(0, 2, 17, 19, "foo bar   baz", byteRange{2, 3}, byteRange{3, 7}, byteRange{7, 13}, byteRange{13, 17})},
		336: {codeWant(0, 2, 8, 10, "foo ", byteRange{2, 3}, byteRange{3, 8})},
		337: {codeWant(0, 1, 15, 16, "foo   bar  baz", byteRange{1, 12}, byteRange{12, 15})},
		338: {codeWant(0, 1, 5, 6, "foo\\", byteRange{1, 5}), textWant(6, 10, "bar`")},
		339: {codeWant(0, 2, 9, 11, "foo`bar", byteRange{2, 9})},
		340: {codeWant(0, 1, 13, 14, "foo `` bar", byteRange{1, 13})},
		341: {textWant(0, 4, "*foo"), codeWant(4, 5, 6, 7, "*", byteRange{5, 6})},
		342: {textWant(0, 7, "[not a "), codeWant(7, 8, 18, 19, "link](/foo", byteRange{8, 18}), textWant(19, 20, ")")},
		343: {codeWant(0, 1, 10, 11, `<a href="`, byteRange{1, 10}), textWant(11, 14, "\">`")},
		345: {codeWant(0, 1, 18, 19, "<https://foo.bar.", byteRange{1, 18}), textWant(19, 24, "baz>`")},
		347: {textWant(0, 8, "```foo``")}, 348: {textWant(0, 4, "`foo")},
		349: {textWant(0, 4, "`foo"), codeWant(4, 6, 9, 11, "bar", byteRange{6, 9})},
		525: {textWant(0, 4, "[foo"), codeWant(4, 5, 12, 13, "](/uri)", byteRange{5, 12})},
		537: {textWant(0, 4, "[foo"), codeWant(4, 5, 11, 12, "][ref]", byteRange{5, 11})},
		640: {codeWant(0, 1, 12, 13, "code   span", byteRange{1, 8}, byteRange{8, 12})},
		641: {codeWant(0, 1, 11, 12, "code\\ span", byteRange{1, 7}, byteRange{7, 11})},
	}
	data, err := os.ReadFile("testdata/commonmark/0.31.2/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples []struct {
		Example        int
		Markdown, HTML string
	}
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range examples {
		if _, ok := wants[e.Example]; !ok && e.Example != 344 && e.Example != 346 {
			continue
		}
		seen++
		t.Run(strconv.Itoa(e.Example), func(t *testing.T) {
			source := parser.NewSource([]byte(e.Markdown))
			r := parser.Parse(context.Background(), source)
			checkStagedTextOutcome(t, r, true)
			if w, ok := wants[e.Example]; ok {
				checkInlineNodes(t, source, r.Document.Root().Children()[0].Children(), w)
			}
			out := conformance.Evaluate(r)
			if out.Status != "rendered" || out.HTML != e.HTML {
				t.Fatal(out, e.HTML)
			}
			if string(source.Bytes()) != e.Markdown {
				t.Fatal("source mutation")
			}
		})
	}
	if seen != len(wants)+2 {
		t.Fatal("missing unchanged upstream example", seen)
	}
}

func TestCodeSpanSegmentedOwnershipAndNormalization(t *testing.T) {
	for _, e := range []string{"\n", "\r\n", "\r"} {
		for _, terminal := range []string{"", e} {
			input := "> - ` é\x00" + e + ">   β `" + terminal
			source := parser.NewSource([]byte(input))
			r := parser.Parse(context.Background(), source)
			checkStagedTextOutcome(t, r, true)
			leaf := r.Document.Root().Children()[0].Children()[0].Children()[0].Children()[0]
			checkInlineNodes(t, source, leaf.Children(), []inlineWant{codeWant(4, 5, 16+len(e), 17+len(e), "é\ufffd β", byteRange{5, 9 + len(e)}, byteRange{13 + len(e), 16 + len(e)})})
			n := leaf.Children()[0]
			n.Content()[0].Semantic = parser.Some("changed")
			n.Markers()[0].Role = "changed"
			n.Properties().Delimiter.Width = 99
			*n = parser.Node{}
			if string(source.Bytes()) != input || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
				t.Fatal("aliased mutable model or state")
			}
		}
	}
	for _, tc := range []struct {
		input, literal string
		start, end     int
	}{
		{"# `é` #\n", "é", 2, 6}, {"`é`\n==\n", "é", 0, 4},
		{"> `a\nb`", "a b", 2, 7},
		{"`  `", "  ", 0, 4}, {"` `", " ", 0, 3}, {"`\n`", " ", 0, 3},
		{"` a `", "a", 0, 5}, {"` a`", " a", 0, 4}, {"`a `", "a ", 0, 4},
		{"`\ta\t`", "\ta\t", 0, 5}, {"` \u00a0 `", "\u00a0", 0, 6},
	} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(tc.input)))
		checkStagedTextOutcome(t, r, true)
		var code *parser.Node
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.CodeSpan {
				code = n
			}
			return true
		})
		if code == nil || code.Span().Start() != tc.start || code.Span().End() != tc.end || code.Properties().Literal != parser.Some(tc.literal) {
			t.Fatal(tc, code)
		}
	}
}

func TestCodeSpanMixedDecodingAndBlockIsolation(t *testing.T) {
	for _, input := range []string{
		"\\`foo`", "&#96;foo`", "\\`foo\\`", "&grave;foo&grave;",
	} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
		for _, n := range r.Document.Root().Children()[0].Children() {
			if n.Kind() != parser.Text {
				t.Fatal("generated or escaped opener became code", input)
			}
		}
	}
	input := "é&amp;`&amp;\\*`&amp;β"
	r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
	checkStagedTextOutcome(t, r, true)
	checkInlineNodes(t, r.Source, r.Document.Root().Children()[0].Children(), []inlineWant{
		textWant(0, 7, "é&"), codeWant(7, 8, 15, 16, "&amp;\\*", byteRange{8, 15}), textWant(16, 23, "&β"),
	})
	for _, input := range []string{"`one\n\ntwo`", "- `one\n- two`", "# `one\n\ntwo`"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.CodeSpan {
				t.Fatal("code state crossed block", input)
			}
			return true
		})
	}

}

// This oracle operates on original source bytes, independently of the production
// normalizer. Partial-tab expansions have their own exact fixture above.
func oracleCode(raw string) string {
	raw = strings.ReplaceAll(raw, "\x00", "\ufffd")
	raw = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(raw)
	if strings.HasPrefix(raw, " ") && strings.HasSuffix(raw, " ") && strings.Trim(raw, " ") != "" {
		return raw[1 : len(raw)-1]
	}
	return raw
}

func checkCodeProvenance(t *testing.T, source parser.Source, n *parser.Node) {
	t.Helper()
	var raw, semantic strings.Builder
	hasPartialTab := false
	for _, part := range n.Content() {
		text, err := source.Text(part.Source)
		if err != nil || !part.Semantic.Present {
			t.Fatal("foreign/unknown code contribution", part, err)
		}
		if part.Transformation == parser.PartialTab {
			hasPartialTab = true
		}
		if part.Transformation != parser.Exact && part.Transformation != parser.Normalized && part.Transformation != parser.PartialTab {
			t.Fatal("decoded code", part)
		}
		if part.Transformation == parser.Exact && part.Semantic.Value != text {
			t.Fatal("false identity", part)
		}
		raw.WriteString(text)
		semantic.WriteString(part.Semantic.Value)
	}
	if n.Properties().Literal != parser.Some(semantic.String()) {
		t.Fatal("detached literal")
	}
	if !hasPartialTab && semantic.String() != oracleCode(raw.String()) {
		t.Fatal("independent opaque normalization", raw.String(), semantic.String())
	}
	m, p := n.Markers(), n.Properties().Delimiter
	if len(m) != 2 || p == nil || p.Width < 1 {
		t.Fatal("missing delimiters")
	}
	for _, marker := range m {
		text, err := source.Text(marker.Source)
		if err != nil || text != strings.Repeat("`", p.Width) {
			t.Fatal("original delimiters", text, err)
		}
	}
}

func TestCodeSpanRepeatedRunWidths(t *testing.T) {
	const count = 1000
	input := "start " + strings.Repeat("`a` ``b`c`` ", count) + "`unmatched"
	r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
	checkStagedTextOutcome(t, r, true)
	index := 0
	for _, n := range r.Document.Root().Children()[0].Children() {
		if n.Kind() != parser.CodeSpan {
			continue
		}
		want := "a"
		if index%2 == 1 {
			want = "b`c"
		}
		if n.Properties().Literal != parser.Some(want) {
			t.Fatal("reused wrong closer", index, n)
		}
		checkCodeProvenance(t, r.Source, n)
		index++
	}
	if index != 2*count {
		t.Fatal("lost run matches", index)
	}
}
