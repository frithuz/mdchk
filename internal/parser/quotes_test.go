package parser_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

func quoteWant(envelope byteRange, prefixes []byteRange, children ...atxBlockWant) atxBlockWant {
	w := atxBlockWant{kind: parser.BlockQuote, envelope: envelope, children: children}
	for _, prefix := range prefixes {
		w.markers = append(w.markers, markerWant{"quote-prefix", prefix})
	}
	return w
}

func TestQuoteUpstreamTargets(t *testing.T) {
	// Hand-written original byte ranges and tree structure, independent of Parse,
	// the HTML renderer and the scanner. All 25 retained examples are exercised.
	wants := map[int][]atxBlockWant{
		228: {quoteWant(byteRange{0, 20}, []byteRange{{0, 2}, {8, 10}, {14, 16}}, atxHeadingWant(1, byteRange{2, 8}, byteRange{2, 3}, byteRange{4, 7}, byteRange{}, "Foo"), atxParagraphWant(byteRange{10, 20}, "bar\nbaz", byteRange{10, 14}, byteRange{16, 19}))},
		229: {quoteWant(byteRange{0, 18}, []byteRange{{0, 1}, {7, 8}, {12, 14}}, atxHeadingWant(1, byteRange{1, 7}, byteRange{1, 2}, byteRange{3, 6}, byteRange{}, "Foo"), atxParagraphWant(byteRange{8, 18}, "bar\nbaz", byteRange{8, 12}, byteRange{14, 17}))},
		230: {quoteWant(byteRange{0, 27}, []byteRange{{0, 5}, {11, 16}, {20, 23}}, atxHeadingWant(1, byteRange{5, 11}, byteRange{5, 6}, byteRange{7, 10}, byteRange{}, "Foo"), atxParagraphWant(byteRange{16, 27}, "bar\nbaz", byteRange{16, 20}, byteRange{23, 26}))},
		231: {indentedWant(byteRange{0, 32}, "> # Foo\n> bar\n> baz\n", []byteRange{{0, 4}, {12, 16}, {22, 26}}, byteRange{4, 11}, byteRange{11, 12}, byteRange{16, 21}, byteRange{21, 22}, byteRange{26, 31}, byteRange{31, 32})},
		232: {quoteWant(byteRange{0, 18}, []byteRange{{0, 2}, {8, 10}}, atxHeadingWant(1, byteRange{2, 8}, byteRange{2, 3}, byteRange{4, 7}, byteRange{}, "Foo"), atxParagraphWant(byteRange{10, 18}, "bar\nbaz", byteRange{10, 14}, byteRange{14, 17}))},
		233: {quoteWant(byteRange{0, 16}, []byteRange{{0, 2}, {10, 12}}, atxParagraphWant(byteRange{2, 16}, "bar\nbaz\nfoo", byteRange{2, 6}, byteRange{6, 10}, byteRange{12, 15}))},
		234: {quoteWant(byteRange{0, 6}, []byteRange{{0, 2}}, atxParagraphWant(byteRange{2, 6}, "foo", byteRange{2, 5})), atxThematicWant(byteRange{6, 10}, byteRange{6, 9})},
		235: {quoteWant(byteRange{0, 8}, []byteRange{{0, 2}})}, // List suffix still unavailable.
		236: {quoteWant(byteRange{0, 10}, []byteRange{{0, 2}}, indentedWant(byteRange{2, 10}, "foo\n", []byteRange{{2, 6}}, byteRange{6, 9}, byteRange{9, 10})), indentedWant(byteRange{10, 18}, "bar\n", []byteRange{{10, 14}}, byteRange{14, 17}, byteRange{17, 18})},
		237: {quoteWant(byteRange{0, 6}, []byteRange{{0, 2}}, fencedWant(byteRange{2, 6}, byteRange{2, 5}, byteRange{}, '`', "", "", byteRange{}, nil)), atxParagraphWant(byteRange{6, 10}, "foo", byteRange{6, 9}), fencedWant(byteRange{10, 14}, byteRange{10, 13}, byteRange{}, '`', "", "", byteRange{}, nil)},
		238: {quoteWant(byteRange{0, 16}, []byteRange{{0, 2}}, atxParagraphWant(byteRange{2, 16}, "foo\n- bar", byteRange{2, 6}, byteRange{10, 15}))},
		239: {quoteWant(byteRange{0, 2}, []byteRange{{0, 1}})},
		240: {quoteWant(byteRange{0, 9}, []byteRange{{0, 1}, {2, 4}, {6, 8}})},
		241: {quoteWant(byteRange{0, 12}, []byteRange{{0, 1}, {2, 4}, {8, 10}}, atxParagraphWant(byteRange{4, 8}, "foo", byteRange{4, 7}))},
		242: {quoteWant(byteRange{0, 6}, []byteRange{{0, 2}}, atxParagraphWant(byteRange{2, 6}, "foo", byteRange{2, 5})), quoteWant(byteRange{7, 13}, []byteRange{{7, 9}}, atxParagraphWant(byteRange{9, 13}, "bar", byteRange{9, 12}))},
		243: {quoteWant(byteRange{0, 12}, []byteRange{{0, 2}, {6, 8}}, atxParagraphWant(byteRange{2, 12}, "foo\nbar", byteRange{2, 6}, byteRange{8, 11}))},
		244: {quoteWant(byteRange{0, 14}, []byteRange{{0, 2}, {6, 7}, {8, 10}}, atxParagraphWant(byteRange{2, 6}, "foo", byteRange{2, 5}), atxParagraphWant(byteRange{10, 14}, "bar", byteRange{10, 13}))},
		245: {atxParagraphWant(byteRange{0, 4}, "foo", byteRange{0, 3}), quoteWant(byteRange{4, 10}, []byteRange{{4, 6}}, atxParagraphWant(byteRange{6, 10}, "bar", byteRange{6, 9}))},
		246: {quoteWant(byteRange{0, 6}, []byteRange{{0, 2}}, atxParagraphWant(byteRange{2, 6}, "aaa", byteRange{2, 5})), atxThematicWant(byteRange{6, 10}, byteRange{6, 9}), quoteWant(byteRange{10, 16}, []byteRange{{10, 12}}, atxParagraphWant(byteRange{12, 16}, "bbb", byteRange{12, 15}))},
		247: {quoteWant(byteRange{0, 10}, []byteRange{{0, 2}}, atxParagraphWant(byteRange{2, 10}, "bar\nbaz", byteRange{2, 6}, byteRange{6, 9}))},
		248: {quoteWant(byteRange{0, 6}, []byteRange{{0, 2}}, atxParagraphWant(byteRange{2, 6}, "bar", byteRange{2, 5})), atxParagraphWant(byteRange{7, 11}, "baz", byteRange{7, 10})},
		249: {quoteWant(byteRange{0, 8}, []byteRange{{0, 2}, {6, 7}}, atxParagraphWant(byteRange{2, 6}, "bar", byteRange{2, 5})), atxParagraphWant(byteRange{8, 12}, "baz", byteRange{8, 11})},
		250: {quoteWant(byteRange{0, 14}, []byteRange{{0, 2}}, quoteWant(byteRange{2, 14}, []byteRange{{2, 4}}, quoteWant(byteRange{4, 14}, []byteRange{{4, 6}}, atxParagraphWant(byteRange{6, 14}, "foo\nbar", byteRange{6, 10}, byteRange{10, 13}))))},
		251: {quoteWant(byteRange{0, 20}, []byteRange{{0, 1}, {8, 10}, {14, 15}}, quoteWant(byteRange{1, 20}, []byteRange{{1, 2}, {15, 16}}, quoteWant(byteRange{2, 20}, []byteRange{{2, 4}}, atxParagraphWant(byteRange{4, 20}, "foo\nbar\nbaz", byteRange{4, 8}, byteRange{10, 14}, byteRange{16, 19}))))},
		252: {quoteWant(byteRange{0, 11}, []byteRange{{0, 2}}, indentedWant(byteRange{2, 11}, "code\n", []byteRange{{2, 6}}, byteRange{6, 10}, byteRange{10, 11})), quoteWant(byteRange{12, 26}, []byteRange{{12, 14}}, atxParagraphWant(byteRange{14, 26}, "not code", byteRange{17, 25}))},
	}
	data, err := os.ReadFile("testdata/commonmark/0.31.2/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples []struct {
		ID                      int `json:"example"`
		Markdown, HTML, Section string
	}
	if err = json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, e := range examples {
		want, ok := wants[e.ID]
		if !ok {
			continue
		}
		if seen[e.ID] || e.Section != "Block quotes" {
			t.Fatal("wrong upstream identity", e.ID)
		}
		seen[e.ID] = true
		t.Run(strconv.Itoa(e.ID), func(t *testing.T) {
			complete := e.ID == 231 || e.ID == 236 || e.ID == 239 || e.ID == 240
			r := checkATX(t, e.Markdown, complete, want)
			if complete && conformance.Evaluate(r).HTML != e.HTML {
				t.Fatal("supported quote HTML differs from unchanged upstream")
			}
		})
	}
	if len(seen) != 25 {
		t.Fatal("not every original quote example exercised")
	}
}

func TestQuotePartialTabUpstream6(t *testing.T) {
	data, err := os.ReadFile("testdata/commonmark/0.31.2/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples []struct {
		ID             int `json:"example"`
		Markdown, HTML string
	}
	if err = json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	for _, e := range examples {
		if e.ID == 6 {
			if e.Markdown != ">\t\tfoo\n" {
				t.Fatal("upstream input changed")
			}
			source := parser.NewSource([]byte(e.Markdown))
			r := parser.Parse(context.Background(), source)
			if r.Status != parser.Complete || r.Failure != nil || conformance.Evaluate(r).HTML != e.HTML {
				t.Fatal(r)
			}
			q := r.Document.Root().Children()[0]
			code := q.Children()[0]
			if q.Kind() != parser.BlockQuote || q.Span().Start() != 0 || q.Span().End() != 7 || len(q.Markers()) != 1 || q.Markers()[0].Source.Start() != 0 || q.Markers()[0].Source.End() != 2 {
				t.Fatal("quote prefix mapping", q)
			}
			if code.Kind() != parser.CodeBlock || code.Span().Start() != 1 || code.Span().End() != 7 || code.Properties().Literal != parser.Some("  foo\n") || code.Markers()[0].Source.Start() != 1 || code.Markers()[0].Source.End() != 3 {
				t.Fatal("code tab mapping", code)
			}
			segments := code.Content()
			if len(segments) != 3 || segments[0].Source.Start() != 2 || segments[0].Source.End() != 3 || segments[0].Transformation != parser.PartialTab || segments[0].Semantic != parser.Some("  ") || segments[1].Source.Start() != 3 || segments[1].Source.End() != 6 || segments[2].Source.Start() != 6 || segments[2].Source.End() != 7 {
				t.Fatal("residual tab provenance", segments)
			}
			for _, seg := range segments {
				if _, err = source.Text(seg.Source); err != nil {
					t.Fatal(err)
				}
			}
			return
		}
	}
	t.Fatal("upstream example 6 missing")
}

func TestQuoteLeafBoundariesAndOpaquePayload(t *testing.T) {
	forms := []string{"<pre>", "<!--", "<?", "<!X", "<![CDATA[", "<div>", "<custom>"}
	for i, opener := range forms {
		t.Run(strconv.Itoa(i+1), func(t *testing.T) {
			input := "> " + opener + "\n> [ref]: /url\n> > literal\n***\n"
			r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
			nodes := r.Document.Root().Children()
			if r.Status != parser.Complete || r.Failure != nil || len(nodes) != 2 || nodes[0].Kind() != parser.BlockQuote || nodes[1].Kind() != parser.ThematicBreak {
				t.Fatal("container boundary swallowed outside line", r)
			}
			html := nodes[0].Children()[0]
			if html.Kind() != parser.HTMLBlock || html.Properties().HTML.BlockForm != parser.Some(i+1) || len(html.Children()) != 0 {
				t.Fatal("HTML form/payload parsed", html)
			}
			firstEnd := len(opener) + 3
			secondEnd := firstEnd + len("> [ref]: /url\n")
			thirdEnd := secondEnd + len("> > literal\n")
			ranges := []byteRange{{2, firstEnd}, {firstEnd + 2, secondEnd}, {secondEnd + 2, thirdEnd}}
			segments := html.Content()
			if len(segments) != len(ranges) {
				t.Fatal("HTML logical line inventory", segments)
			}
			for j, segment := range segments {
				if segment.Source.Start() != ranges[j].start || segment.Source.End() != ranges[j].end || segment.Transformation != parser.Exact {
					t.Fatal("HTML original source range", j, segment)
				}
			}
			var raw string
			for _, s := range segments {
				raw += s.Semantic.Value
			}
			if raw != opener+"\n[ref]: /url\n> literal\n" || html.Span().Start() != 2 || html.Span().End() != len(input)-4 || len(r.Document.References().Definitions) != 0 {
				t.Fatal("HTML prefix or opaque payload", raw, html)
			}
			if out := conformance.Evaluate(r); out.Status != "rendered" || !strings.Contains(out.HTML, raw+"</blockquote>\n<hr />\n") {
				t.Fatal(out)
			}
		})
	}
	// Existing outer markers must match before a closing fence can be considered;
	// remaining > bytes in marked code are opaque payload, not nested quotes.
	checkATX(t, "> ```\n> > x\n> ```\n***\n", true, []atxBlockWant{
		quoteWant(byteRange{0, 18}, []byteRange{{0, 2}, {6, 8}, {12, 14}}, fencedWant(byteRange{2, 18}, byteRange{2, 5}, byteRange{14, 17}, '`', "> x\n", "", byteRange{}, nil, byteRange{8, 11}, byteRange{11, 12})), atxThematicWant(byteRange{18, 22}, byteRange{18, 21}),
	})
}

func TestQuoteSetextAndProvenance(t *testing.T) {
	checkATX(t, "> a\n---", false, []atxBlockWant{quoteWant(byteRange{0, 4}, []byteRange{{0, 2}}, atxParagraphWant(byteRange{2, 4}, "a", byteRange{2, 3})), atxThematicWant(byteRange{4, 7}, byteRange{4, 7})})
	checkATX(t, "> a\n===", false, []atxBlockWant{quoteWant(byteRange{0, 7}, []byteRange{{0, 2}}, atxParagraphWant(byteRange{2, 7}, "a\n===", byteRange{2, 4}, byteRange{4, 7}))})
	checkATX(t, "> a\n> =\n", false, []atxBlockWant{quoteWant(byteRange{0, 8}, []byteRange{{0, 2}, {4, 6}}, setextWant(1, byteRange{2, 8}, byteRange{6, 7}, "a", byteRange{2, 3}))})
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, terminal := range []string{ending, ""} {
			input := "> é\x00  " + ending + "> Z\t " + terminal
			firstEnd := 7 + len(ending)
			lastEnd := firstEnd + 5 + len(terminal)
			checkATX(t, input, false, []atxBlockWant{quoteWant(byteRange{0, lastEnd}, []byteRange{{0, 2}, {firstEnd, firstEnd + 2}}, atxParagraphWant(byteRange{2, lastEnd}, "é\ufffd  "+ending+"Z", byteRange{2, 4}, byteRange{4, 5}, byteRange{5, firstEnd}, byteRange{firstEnd + 2, firstEnd + 3}))})
		}
	}
	source := parser.NewSource([]byte("> > x\n> y\nz\n"))
	r := parser.Parse(context.Background(), source)
	q := r.Document.Root().Children()[0]
	q.Markers()[0].Role = "changed"
	q.Children()[0].Children()[0].Content()[0].Semantic = parser.Some("changed")
	*q = parser.Node{}
	bytes := r.Source.Bytes()
	bytes[0] = '!'
	if !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
		t.Fatal("mutable accessor escaped snapshot ownership")
	}
	foreign := parser.NewSource(source.Bytes())
	span := r.Document.Root().Children()[0].Children()[0].Children()[0].Content()[0].Source
	if _, err := foreign.Text(span); !errors.Is(err, parser.ErrRange) {
		t.Fatal("foreign snapshot accepted quote span")
	}
	for _, input := range []string{"> text\n", "> # title\n", "> ``` &amp;\n", "> [ref]: /url\n"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		if r.Status != parser.Incomplete || r.Document.References().LookupAvailability != parser.Unavailable || conformance.Evaluate(r).Status != "pending" {
			t.Fatal("unavailable provider gained completion", r)
		}
	}
}

func TestQuoteHTMLResidualTabAndEndings(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, terminal := range []string{ending, ""} {
			input := ">\t<div>" + ending + ">\tX\x00" + terminal
			source := parser.NewSource([]byte(input))
			r := parser.Parse(context.Background(), source)
			if r.Failure != nil || r.Status != parser.Complete {
				t.Fatal(r)
			}
			q := r.Document.Root().Children()[0]
			html := q.Children()[0]
			var text string
			var partials, nuls int
			for _, s := range html.Content() {
				raw, err := source.Text(s.Source)
				if err != nil {
					t.Fatal(err)
				}
				if s.Transformation == parser.PartialTab {
					partials++
					if raw != "\t" || s.Semantic != parser.Some("  ") {
						t.Fatal("residual tab mapping", s)
					}
				}
				if raw == "\x00" {
					nuls++
					if s.Transformation != parser.Normalized || s.Semantic != parser.Some("\ufffd") {
						t.Fatal("NUL mapping", s)
					}
				}
				text += s.Semantic.Value
			}
			want := "  <div>" + ending + "  X\ufffd" + terminal
			if partials != 2 || nuls != 1 || text != want || html.Span().Start() != 1 || html.Span().End() != len(input) || len(q.Markers()) != 2 {
				t.Fatal("logical HTML lost provenance", text, html)
			}
			if !strings.HasSuffix(want, "\n") {
				want += "\n"
			}
			out := conformance.Evaluate(r)
			if out.Status != "rendered" || out.HTML != "<blockquote>\n"+want+"</blockquote>\n" {
				t.Fatal("HTML consumer rejected valid residual tab", out)
			}
		}
	}
}

func TestQuoteCodeAndHeadingTransitions(t *testing.T) {
	checkATX(t, "    a\n> quote\n    b\n", false, []atxBlockWant{
		indentedWant(byteRange{0, 6}, "a\n", []byteRange{{0, 4}}, byteRange{4, 5}, byteRange{5, 6}),
		quoteWant(byteRange{6, 20}, []byteRange{{6, 8}}, atxParagraphWant(byteRange{8, 20}, "quote\nb", byteRange{8, 14}, byteRange{18, 19})),
	})
	// A quote with an empty heading has no paragraph eligible for lazy continuation.
	checkATX(t, "> #\ntext\n", false, []atxBlockWant{
		quoteWant(byteRange{0, 4}, []byteRange{{0, 2}}, atxHeadingWant(1, byteRange{2, 4}, byteRange{2, 3}, byteRange{}, byteRange{}, "")),
		atxParagraphWant(byteRange{4, 9}, "text", byteRange{4, 8}),
	})
}

func TestQuoteCodeBlankAndNormalizedPayload(t *testing.T) {
	checkATX(t, ">     a\n>\n>     b\n>\n", true, []atxBlockWant{quoteWant(byteRange{0, 20}, []byteRange{{0, 2}, {8, 9}, {10, 12}, {18, 19}}, indentedWant(byteRange{2, 18}, "a\n\nb\n", []byteRange{{2, 6}, {12, 16}}, byteRange{6, 7}, byteRange{7, 8}, byteRange{9, 10}, byteRange{16, 17}, byteRange{17, 18}))})
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, terminal := range []string{ending, ""} {
			input := "> ```" + ending + "> é\x00" + terminal
			source := parser.NewSource([]byte(input))
			r := parser.Parse(context.Background(), source)
			if r.Status != parser.Complete || r.Failure != nil {
				t.Fatal(r)
			}
			code := r.Document.Root().Children()[0].Children()[0]
			if code.Properties().Literal != parser.Some("é\ufffd\n") || code.Span().Start() != 2 || code.Span().End() != len(input) || len(code.Markers()) != 1 {
				t.Fatal("unclosed quoted fence payload", code)
			}
			segments := code.Content()
			last := segments[len(segments)-1]
			want := parser.Exact
			if terminal == "" {
				want = parser.Synthetic
			} else if ending != "\n" {
				want = parser.Normalized
			}
			if last.Transformation != want || last.Source.Start() != len(input)-len(terminal) || last.Source.End() != len(input) || last.Semantic != parser.Some("\n") {
				t.Fatal("code ending provenance", last)
			}
			if conformance.Evaluate(r).HTML != "<blockquote>\n<pre><code>é\ufffd\n</code></pre>\n</blockquote>\n" {
				t.Fatal("normalized quote payload HTML")
			}
		}
	}
}
