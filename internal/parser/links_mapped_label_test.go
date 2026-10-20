package parser_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

// Hosted fuzzing found [\n ](): the continuation indentation is a source gap,
// not label payload. A label containing a soft break is not an empty label.
func TestMappedLabelClosingBracketAfterSourceGap(t *testing.T) {
	type fixture struct {
		name, input, label                      string
		kind                                    parser.Kind
		labelRanges, payloadRanges, childRanges []byteRange
		children                                []parser.Kind
		html                                    string
	}
	fixtures := []fixture{
		{"native-CI-regression", "[\n ]()", "\n", parser.Link, []byteRange{{1, 2}}, []byteRange{{0, 2}, {3, 6}}, []byteRange{{1, 2}}, []parser.Kind{parser.SoftBreak}, "<p><a href=\"\">\n</a></p>\n"},
		{"empty-label", "[]()", "", parser.Link, []byteRange{{1, 1}}, []byteRange{{0, 4}}, nil, nil, "<p><a href=\"\"></a></p>\n"},
		{"image-break-label", "![\n ]()", "\n", parser.Image, []byteRange{{2, 3}}, []byteRange{{0, 3}, {4, 7}}, []byteRange{{2, 3}}, []parser.Kind{parser.SoftBreak}, "<p><img src=\"\" alt=\"\n\" /></p>\n"},
		{"quote-prefix-gap", "> [\n> ]()", "\n", parser.Link, []byteRange{{3, 4}}, []byteRange{{2, 4}, {6, 9}}, []byteRange{{3, 4}}, []parser.Kind{parser.SoftBreak}, "<blockquote>\n<p><a href=\"\">\n</a></p>\n</blockquote>\n"},
		{"quote-item-prefix-gap", "> - [\n>   ]()", "\n", parser.Link, []byteRange{{5, 6}}, []byteRange{{4, 6}, {10, 13}}, []byteRange{{5, 6}}, []parser.Kind{parser.SoftBreak}, "<blockquote>\n<ul>\n<li><a href=\"\">\n</a></li>\n</ul>\n</blockquote>\n"},
		{"opaque-HTML-not-empty", "[<i></i>\n ]()", "<i></i>\n", parser.Link, []byteRange{{1, 9}}, []byteRange{{0, 9}, {10, 13}}, []byteRange{{1, 4}, {4, 8}, {8, 9}}, []parser.Kind{parser.InlineHTML, parser.InlineHTML, parser.SoftBreak}, "<p><a href=\"\"><i></i>\n</a></p>\n"},
		{"opaque-code-and-break", "[`x`\n ]()", "`x`\n", parser.Link, []byteRange{{1, 5}}, []byteRange{{0, 5}, {6, 9}}, []byteRange{{1, 4}, {4, 5}}, []parser.Kind{parser.CodeSpan, parser.SoftBreak}, "<p><a href=\"\"><code>x</code>\n</a></p>\n"},
		{"reference-primary-label-gap", "[a\n ][k]\n\n[k]: /u\n", "a\n", parser.Link, []byteRange{{1, 3}}, []byteRange{{0, 3}, {4, 8}}, []byteRange{{1, 2}, {2, 3}}, []parser.Kind{parser.Text, parser.SoftBreak}, "<p><a href=\"/u\">a\n</a></p>\n"},
		{"reference-key-gap", "[x][k\n ]\n\n[k]: /u\n", "x", parser.Link, []byteRange{{1, 2}}, []byteRange{{0, 6}, {7, 8}}, []byteRange{{1, 2}}, []parser.Kind{parser.Text}, "<p><a href=\"/u\">x</a></p>\n"},
	}
	for _, ending := range []string{"\r\n", "\r"} {
		input := "[" + ending + "\t]()"
		end := 1 + len(ending)
		fixtures = append(fixtures, fixture{"physical-ending-" + ending, input, ending, parser.Link, []byteRange{{1, end}}, []byteRange{{0, end}, {end + 1, len(input)}}, []byteRange{{1, end}}, []parser.Kind{parser.SoftBreak}, "<p><a href=\"\">\n</a></p>\n"})
	}
	bounds := func(parts []parser.Segment) []byteRange {
		var out []byteRange
		for _, p := range parts {
			out = append(out, byteRange{p.Source.Start(), p.Source.End()})
		}
		return out
	}
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			source := parser.NewSource([]byte(f.input))
			r := parser.Parse(context.Background(), source)
			checkStagedTextOutcome(t, r, true)
			var block, link *parser.Node
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() == parser.Paragraph {
					if block != nil {
						t.Fatal("unexpected second paragraph")
					}
					block = n
				}
				if n.Kind() == f.kind {
					if link != nil {
						t.Fatal("unexpected second label node")
					}
					link = n
				}
				return true
			})
			if block == nil || link == nil {
				t.Fatal("missing paragraph/link")
			}
			label := link.Properties().Link.Label
			if label.Semantic != parser.Some(f.label) || !reflect.DeepEqual(bounds(label.Segments), f.labelRanges) || !reflect.DeepEqual(bounds(block.Content()), f.payloadRanges) {
				t.Fatal("independent logical label/source-gap facts", label, bounds(block.Content()))
			}
			var kinds []parser.Kind
			var ranges []byteRange
			for _, child := range link.Children() {
				kinds = append(kinds, child.Kind())
				ranges = append(ranges, byteRange{child.Span().Start(), child.Span().End()})
				if child.Kind() == parser.CodeSpan && child.Properties().Literal != parser.Some("x") {
					t.Fatal("code label interpretation")
				}
			}
			if !reflect.DeepEqual(kinds, f.children) || !reflect.DeepEqual(ranges, f.childRanges) {
				t.Fatal("independent interpreted label facts", kinds, ranges)
			}
			checkLinkProvenance(t, source, link, r.Document.Root())
			if out := conformance.Evaluate(r); out.Status != "rendered" || out.HTML != f.html {
				t.Fatal("independent rendered label", out, f.html)
			}
			if string(source.Bytes()) != f.input {
				t.Fatal("original bytes changed")
			}
		})
	}
}

func TestBracketSourceOracleRejectsMissingPayloadAndIncludedPrefix(t *testing.T) {
	source := parser.NewSource([]byte("[\n ]()"))
	span := func(a, b int) parser.Span {
		s, e := source.Span(a, b)
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	parts := []parser.Segment{{Source: span(0, 2)}, {Source: span(3, 6)}}
	opening, closing := span(0, 1), span(3, 4)
	value := func(a, b int, raw string) parser.Value {
		return parser.Value{Semantic: parser.Some(raw), Segments: []parser.Segment{{Source: span(a, b), Transformation: parser.Exact, Semantic: parser.Some(raw)}}}
	}
	if !bracketValueCoversPayload(parts, value(1, 2, "\n"), opening, closing) {
		t.Fatal("valid mapped break label rejected")
	}
	for _, bad := range []parser.Value{value(1, 1, ""), value(1, 3, "\n "), value(2, 3, " ")} {
		if bracketValueCoversPayload(parts, bad, opening, closing) {
			t.Fatal("oracle accepted missing payload or included source gap", bad)
		}
	}
	emptySource := parser.NewSource([]byte("[]()"))
	all, _ := emptySource.Span(0, 4)
	open, _ := emptySource.Span(0, 1)
	close, _ := emptySource.Span(1, 2)
	point, _ := emptySource.Span(1, 1)
	empty := parser.Value{Semantic: parser.Some(""), Segments: []parser.Segment{{Source: point, Transformation: parser.Exact, Semantic: parser.Some("")}}}
	if !bracketValueCoversPayload([]parser.Segment{{Source: all}}, empty, open, close) {
		t.Fatal("true empty label point rejected")
	}
	wrong, _ := emptySource.Span(2, 2)
	empty.Segments[0].Source = wrong
	if bracketValueCoversPayload([]parser.Segment{{Source: all}}, empty, open, close) {
		t.Fatal("oracle accepted wrong empty-label point")
	}
}
