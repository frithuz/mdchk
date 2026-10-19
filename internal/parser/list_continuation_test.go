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

// These trees are independent expectations over unchanged upstream bytes. The
// constructors only assemble supplied ranges, never classify Markdown or inspect
// parser/renderer output. Marker metadata is checked separately by the retained
// opening-prefix tests and the explicit continuation-provenance fixtures below.
type listTreeWant struct {
	kind     parser.Kind
	span     byteRange
	text     string
	content  []byteRange
	children []listTreeWant
	list     *parser.ListProperties
}

func itemTree(a, b int, children ...listTreeWant) listTreeWant {
	return listTreeWant{kind: parser.ListItem, span: byteRange{a, b}, children: children}
}
func quoteTree(a, b int, children ...listTreeWant) listTreeWant {
	return listTreeWant{kind: parser.BlockQuote, span: byteRange{a, b}, children: children}
}
func paragraphTree(a, b int, text string, content ...byteRange) listTreeWant {
	return listTreeWant{kind: parser.Paragraph, span: byteRange{a, b}, text: text, content: content}
}
func codeTree(a, b int, text string, content ...byteRange) listTreeWant {
	return listTreeWant{kind: parser.CodeBlock, span: byteRange{a, b}, text: text, content: content}
}
func headingTree(a, b int, text string, content ...byteRange) listTreeWant {
	return listTreeWant{kind: parser.Heading, span: byteRange{a, b}, text: text, content: content}
}

func checkListTrees(t *testing.T, input string, want []listTreeWant) parser.ParseResult {
	t.Helper()
	source := parser.NewSource([]byte(input))
	r := parser.Parse(context.Background(), source)
	if r.Failure != nil || r.Interruption != "" || r.Document == nil || string(source.Bytes()) != input {
		t.Fatal(r)
	}
	foreign := parser.NewSource([]byte(input))
	checkSpan := func(s parser.Span, w byteRange) {
		t.Helper()
		if !s.Present() || s.Start() != w.start || s.End() != w.end {
			t.Fatalf("span [%d,%d) want %+v", s.Start(), s.End(), w)
		}
		if _, err := source.Text(s); err != nil {
			t.Fatal("wrong source owner", err)
		}
		if _, err := foreign.Text(s); err == nil {
			t.Fatal("foreign source accepted span")
		}
	}
	// Older continuation fixtures independently retain their item/leaf facts;
	// grouping fixtures below assert the new List ancestry without flattening.
	explicitGroups := false
	for _, w := range want {
		explicitGroups = explicitGroups || w.kind == parser.List
	}
	var check func([]*parser.Node, []listTreeWant)
	check = func(nodes []*parser.Node, wants []listTreeWant) {
		t.Helper()
		if !explicitGroups {
			nodes = flattenListGroups(nodes)
		}
		if len(nodes) != len(wants) {
			t.Fatalf("child count %d want %d", len(nodes), len(wants))
		}
		for i, n := range nodes {
			w := wants[i]
			if n.Kind() != w.kind {
				t.Fatalf("node %d kind %s want %s", i, n.Kind(), w.kind)
			}
			checkSpan(n.Span(), w.span)
			segments := n.Content()
			if len(segments) != len(w.content) {
				t.Fatalf("%s [%d,%d) segments %d want %d", n.Kind(), w.span.start, w.span.end, len(segments), len(w.content))
			}
			text := ""
			for j, s := range segments {
				checkSpan(s.Source, w.content[j])
				text += s.Semantic.Value
			}
			if text != w.text {
				t.Fatalf("%s content %q want %q", n.Kind(), text, w.text)
			}
			if n.Kind() == parser.ListItem {
				p := n.Properties().List
				if p == nil || p.Prefix == nil || p.Tight.Present {
					t.Fatal("fabricated item completion", r)
				}
			}
			if w.list != nil && !reflect.DeepEqual(n.Properties().List, w.list) {
				t.Fatalf("list properties %+v want %+v", n.Properties().List, w.list)
			}
			if n.Kind() == parser.CodeBlock && n.Properties().Literal != parser.Some(w.text) {
				t.Fatal("literal/source mismatch")
			}
			if n.Kind() == parser.Paragraph || n.Kind() == parser.Heading {
				checkInlineText(t, r, n, w.text, w.content)
			} else {
				check(n.Children(), w.children)
			}
		}
	}
	checkSpan(r.Document.Root().Span(), byteRange{0, len(input)})
	check(r.Document.Root().Children(), want)
	return r
}

func TestListContinuationUpstreamTargets(t *testing.T) {
	wants := map[int][]listTreeWant{
		253: {paragraphTree(0, 28, "A paragraph\nwith two lines.", byteRange{0, 12}, byteRange{12, 27}), codeTree(29, 47, "indented code\n", byteRange{33, 46}, byteRange{46, 47}), quoteTree(48, 65, paragraphTree(50, 65, "A block quote.", byteRange{50, 64}))},
		254: {itemTree(0, 81, paragraphTree(4, 36, "A paragraph\nwith two lines.", byteRange{4, 16}, byteRange{20, 35}), codeTree(41, 59, "indented code\n", byteRange{45, 58}, byteRange{58, 59}), quoteTree(64, 81, paragraphTree(66, 81, "A block quote.", byteRange{66, 80})))},
		255: {itemTree(0, 7, paragraphTree(2, 6, "one", byteRange{2, 5})), paragraphTree(7, 12, "two", byteRange{8, 11})},
		256: {itemTree(0, 13, paragraphTree(2, 6, "one", byteRange{2, 5}), paragraphTree(9, 13, "two", byteRange{9, 12}))},
		257: {itemTree(0, 11, paragraphTree(6, 10, "one", byteRange{6, 9})), codeTree(11, 20, " two\n", byteRange{15, 19}, byteRange{19, 20})},
		258: {itemTree(0, 21, paragraphTree(6, 10, "one", byteRange{6, 9}), paragraphTree(17, 21, "two", byteRange{17, 20}))},
		259: {quoteTree(0, 29, quoteTree(5, 29, itemTree(7, 29, paragraphTree(11, 15, "one", byteRange{11, 14}), paragraphTree(25, 29, "two", byteRange{25, 28}))))},
		260: {quoteTree(0, 22, quoteTree(1, 22, itemTree(2, 11, paragraphTree(4, 8, "one", byteRange{4, 7})), paragraphTree(18, 22, "two", byteRange{18, 21})))},
		261: {paragraphTree(0, 5, "-one", byteRange{0, 4}), paragraphTree(6, 12, "2.two", byteRange{6, 11})},
		262: {itemTree(0, 14, paragraphTree(2, 6, "foo", byteRange{2, 5}), paragraphTree(10, 14, "bar", byteRange{10, 13}))},
		263: {itemTree(0, 53, paragraphTree(4, 8, "foo", byteRange{4, 7}), codeTree(13, 33, "bar\n", byteRange{21, 24}, byteRange{24, 25}), paragraphTree(38, 42, "baz", byteRange{38, 41}), quoteTree(47, 53, paragraphTree(49, 53, "bam", byteRange{49, 52})))},
		264: {itemTree(0, 29, paragraphTree(2, 6, "Foo", byteRange{2, 5}), codeTree(9, 29, "bar\n\n\nbaz\n", byteRange{13, 16}, byteRange{16, 17}, byteRange{17, 18}, byteRange{18, 19}, byteRange{25, 28}, byteRange{28, 29}))},
		265: {itemTree(0, 14, paragraphTree(11, 14, "ok", byteRange{11, 13}))},
		266: {paragraphTree(0, 19, "1234567890. not ok", byteRange{0, 18})},
		267: {itemTree(0, 6, paragraphTree(3, 6, "ok", byteRange{3, 5}))},
		268: {itemTree(0, 8, paragraphTree(5, 8, "ok", byteRange{5, 7}))},
		269: {paragraphTree(0, 11, "-1. not ok", byteRange{0, 10})},
		270: {itemTree(0, 17, paragraphTree(2, 6, "foo", byteRange{2, 5}), codeTree(9, 17, "bar\n", byteRange{13, 16}, byteRange{16, 17}))},
		271: {itemTree(0, 27, paragraphTree(7, 11, "foo", byteRange{7, 10}), codeTree(19, 27, "bar\n", byteRange{23, 26}, byteRange{26, 27}))},
		273: {itemTree(0, 53, codeTree(3, 21, "indented code\n", byteRange{7, 20}, byteRange{20, 21}), paragraphTree(25, 35, "paragraph", byteRange{25, 34}), codeTree(39, 53, "more code\n", byteRange{43, 52}, byteRange{52, 53}))},
		274: {itemTree(0, 54, codeTree(3, 22, " indented code\n", byteRange{7, 21}, byteRange{21, 22}), paragraphTree(26, 36, "paragraph", byteRange{26, 35}), codeTree(40, 54, "more code\n", byteRange{44, 53}, byteRange{53, 54}))},
		275: {paragraphTree(0, 7, "foo", byteRange{3, 6}), paragraphTree(8, 12, "bar", byteRange{8, 11})},
		276: {itemTree(0, 10, paragraphTree(5, 9, "foo", byteRange{5, 8})), paragraphTree(10, 16, "bar", byteRange{12, 15})},
		277: {itemTree(0, 15, paragraphTree(3, 7, "foo", byteRange{3, 6}), paragraphTree(11, 15, "bar", byteRange{11, 14}))},
		278: {itemTree(0, 8, paragraphTree(4, 8, "foo", byteRange{4, 7})), itemTree(8, 28, codeTree(12, 28, "bar\n", byteRange{18, 21}, byteRange{21, 22})), itemTree(28, 40, codeTree(32, 40, "baz\n", byteRange{36, 39}, byteRange{39, 40}))},
		279: {itemTree(0, 11, paragraphTree(7, 11, "foo", byteRange{7, 10}))},
		280: {itemTree(0, 2), paragraphTree(3, 9, "foo", byteRange{5, 8})},
		281: {itemTree(0, 6, paragraphTree(2, 6, "foo", byteRange{2, 5})), itemTree(6, 8), itemTree(8, 14, paragraphTree(10, 14, "bar", byteRange{10, 13}))},
		282: {itemTree(0, 6, paragraphTree(2, 6, "foo", byteRange{2, 5})), itemTree(6, 11), itemTree(11, 17, paragraphTree(13, 17, "bar", byteRange{13, 16}))},
		283: {itemTree(0, 7, paragraphTree(3, 7, "foo", byteRange{3, 6})), itemTree(7, 10), itemTree(10, 17, paragraphTree(13, 17, "bar", byteRange{13, 16}))},
		284: {itemTree(0, 2)},
		285: {paragraphTree(0, 6, "foo\n*", byteRange{0, 4}, byteRange{4, 5}), paragraphTree(7, 14, "foo\n1.", byteRange{7, 11}, byteRange{11, 13})},
		286: {itemTree(0, 85, paragraphTree(5, 38, "A paragraph\nwith two lines.", byteRange{5, 17}, byteRange{22, 37}), codeTree(44, 62, "indented code\n", byteRange{48, 61}, byteRange{61, 62}), quoteTree(68, 85, paragraphTree(70, 85, "A block quote.", byteRange{70, 84})))},
		287: {itemTree(0, 89, paragraphTree(6, 40, "A paragraph\nwith two lines.", byteRange{6, 18}, byteRange{24, 39}), codeTree(47, 65, "indented code\n", byteRange{51, 64}, byteRange{64, 65}), quoteTree(72, 89, paragraphTree(74, 89, "A block quote.", byteRange{74, 88})))},
		288: {itemTree(0, 93, paragraphTree(7, 42, "A paragraph\nwith two lines.", byteRange{7, 19}, byteRange{26, 41}), codeTree(50, 68, "indented code\n", byteRange{54, 67}, byteRange{67, 68}), quoteTree(76, 93, paragraphTree(78, 93, "A block quote.", byteRange{78, 92})))},
		289: {codeTree(0, 97, "1.  A paragraph\n    with two lines.\n\n        indented code\n\n    > A block quote.\n", byteRange{4, 19}, byteRange{19, 20}, byteRange{24, 43}, byteRange{43, 44}, byteRange{44, 45}, byteRange{49, 70}, byteRange{70, 71}, byteRange{71, 72}, byteRange{76, 96}, byteRange{96, 97})},
		290: {itemTree(0, 83, paragraphTree(6, 34, "A paragraph\nwith two lines.", byteRange{6, 18}, byteRange{18, 33}), codeTree(41, 59, "indented code\n", byteRange{45, 58}, byteRange{58, 59}), quoteTree(66, 83, paragraphTree(68, 83, "A block quote.", byteRange{68, 82})))},
		291: {itemTree(0, 38, paragraphTree(6, 38, "A paragraph\nwith two lines.", byteRange{6, 18}, byteRange{22, 37}))},
		292: {quoteTree(0, 34, itemTree(2, 34, quoteTree(5, 34, paragraphTree(7, 34, "Blockquote\ncontinued here.", byteRange{7, 18}, byteRange{18, 33}))))},
		293: {quoteTree(0, 36, itemTree(2, 36, quoteTree(5, 36, paragraphTree(7, 36, "Blockquote\ncontinued here.", byteRange{7, 18}, byteRange{20, 35}))))},
		294: {itemTree(0, 36, paragraphTree(2, 6, "foo", byteRange{2, 5}), itemTree(8, 36, paragraphTree(10, 14, "bar", byteRange{10, 13}), itemTree(18, 36, paragraphTree(20, 24, "baz", byteRange{20, 23}), itemTree(30, 36, paragraphTree(32, 36, "boo", byteRange{32, 35})))))},
		295: {itemTree(0, 6, paragraphTree(2, 6, "foo", byteRange{2, 5})), itemTree(6, 13, paragraphTree(9, 13, "bar", byteRange{9, 12})), itemTree(13, 21, paragraphTree(17, 21, "baz", byteRange{17, 20})), itemTree(21, 30, paragraphTree(26, 30, "boo", byteRange{26, 29}))},
		296: {itemTree(0, 18, paragraphTree(4, 8, "foo", byteRange{4, 7}), itemTree(12, 18, paragraphTree(14, 18, "bar", byteRange{14, 17})))},
		297: {itemTree(0, 8, paragraphTree(4, 8, "foo", byteRange{4, 7})), itemTree(8, 17, paragraphTree(13, 17, "bar", byteRange{13, 16}))},
		298: {itemTree(0, 8, itemTree(2, 8, paragraphTree(4, 8, "foo", byteRange{4, 7})))},
		299: {itemTree(0, 12, itemTree(3, 12, itemTree(5, 12, paragraphTree(8, 12, "foo", byteRange{8, 11}))))},
		300: {itemTree(0, 8, headingTree(2, 8, "Foo", byteRange{4, 7})), itemTree(8, 26, headingTree(10, 20, "Bar", byteRange{10, 13}), paragraphTree(22, 26, "baz", byteRange{22, 25}))},
	}
	// Introductory code/paragraph case 272 has its own existing independent
	// literal/source assertion; still execute it here to account for every ID.
	wants[272] = []listTreeWant{codeTree(0, 18, "indented code\n", byteRange{4, 17}, byteRange{17, 18}), paragraphTree(19, 29, "paragraph", byteRange{19, 28}), codeTree(30, 44, "more code\n", byteRange{34, 43}, byteRange{43, 44})}
	data, err := os.ReadFile("testdata/commonmark/0.31.2/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples []struct {
		ID                      int `json:"example"`
		Markdown, HTML, Section string
	}
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, e := range examples {
		if e.ID < 253 || e.ID > 300 {
			continue
		}
		if seen[e.ID] || e.Section != "List items" {
			t.Fatal("wrong upstream identity")
		}
		seen[e.ID] = true
		t.Run(strconv.Itoa(e.ID), func(t *testing.T) {
			w, ok := wants[e.ID]
			if !ok {
				t.Fatal("unaccounted case")
			}
			r := checkListTrees(t, e.Markdown, w)
			checkCorpusTextOutcome(t, e.ID, e.HTML, r)
		})
	}
	if len(seen) != 48 || len(wants) != 48 {
		t.Fatal("incomplete upstream inventory")
	}
}

func TestListContinuationBoundaries(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  []listTreeWant
	}{
		{"- first\ncontinued\n", []listTreeWant{itemTree(0, 18, paragraphTree(2, 18, "first\ncontinued", byteRange{2, 8}, byteRange{8, 17}))}},
		{"- first\n  continued\n", []listTreeWant{itemTree(0, 20, paragraphTree(2, 20, "first\ncontinued", byteRange{2, 8}, byteRange{10, 19}))}},
		{"- first\n- sibling\n", []listTreeWant{itemTree(0, 8, paragraphTree(2, 8, "first", byteRange{2, 7})), itemTree(8, 18, paragraphTree(10, 18, "sibling", byteRange{10, 17}))}},
		{"- first\n\n# later\n", []listTreeWant{itemTree(0, 9, paragraphTree(2, 8, "first", byteRange{2, 7})), headingTree(9, 17, "later", byteRange{11, 16})}},
		{"- first\n  [x]: /url\n", []listTreeWant{itemTree(0, 20, paragraphTree(2, 20, "first\n[x]: /url", byteRange{2, 8}, byteRange{10, 19}))}},
		{"- foo\n2. bar\n", []listTreeWant{itemTree(0, 13, paragraphTree(2, 13, "foo\n2. bar", byteRange{2, 6}, byteRange{6, 12}))}},
		{"1. foo\n2. bar\n", []listTreeWant{itemTree(0, 7, paragraphTree(3, 7, "foo", byteRange{3, 6})), itemTree(7, 14, paragraphTree(10, 14, "bar", byteRange{10, 13}))}},
		{"- foo\n+\n", []listTreeWant{itemTree(0, 8, paragraphTree(2, 8, "foo\n+", byteRange{2, 6}, byteRange{6, 7}))}},
	} {
		t.Run(tt.input, func(t *testing.T) { checkListTrees(t, tt.input, tt.want) })
	}
}

func TestListContinuationSourceImmutability(t *testing.T) {
	input := []byte("- one\n  > two\n  > three\n- four\n")
	source := parser.NewSource(input)
	r := parser.Parse(context.Background(), source)
	parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
		children := n.Children()
		if len(children) > 0 {
			children[0] = nil
		}
		markers := n.Markers()
		if len(markers) > 0 {
			markers[0].Source = parser.Span{}
		}
		segments := n.Content()
		if len(segments) > 0 {
			segments[0].Semantic = parser.Some("changed")
		}
		props := n.Properties()
		if props.List != nil {
			props.List.ContentIndent = parser.Some(99)
			if props.List.Prefix != nil {
				props.List.Prefix.Segments[0].Source = parser.Span{}
			}
		}
		return true
	})
	input[0] = '!'
	copy := source.Bytes()
	copy[0] = '!'
	if !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
		t.Fatal("borrowed mutable storage crossed model boundary")
	}
}

func TestListContinuationLeafBoundaries(t *testing.T) {
	// Each unclosed HTML form stops at the item's actual dedent. Payload markers
	// are opaque even when they look like nested items, quotes or directives.
	for form, opening := range []string{"<pre>", "<!--", "<?x", "<!A", "<![CDATA[", "<div>", "<custom>"} {
		input := "- " + opening + "\n  - > opaque\noutside\n"
		t.Run(opening, func(t *testing.T) {
			r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
			if r.Failure != nil || r.Status != parser.Complete || conformance.Evaluate(r).Status != "rendered" {
				t.Fatal(r)
			}
			nodes := ungroupedChildren(r.Document.Root())
			end := 2 + len(opening) + 1 + len("  - > opaque\n")
			if len(nodes) != 2 || nodes[0].Kind() != parser.ListItem || nodes[0].Span().End() != end || nodes[1].Kind() != parser.Paragraph || nodes[1].Span().Start() != end {
				t.Fatal("dedented line swallowed", nodes)
			}
			children := nodes[0].Children()
			if len(children) != 1 || children[0].Kind() != parser.HTMLBlock || children[0].Properties().HTML.BlockForm != parser.Some(form+1) || children[0].Span().Start() != 2 || children[0].Span().End() != end {
				t.Fatal("opaque HTML boundary changed", children)
			}
			content := children[0].Content()
			if len(content) != 2 {
				t.Fatal("lost separated raw lines", content)
			}
			assertSegment(t, r.Source, content[0], 2, 3+len(opening), parser.Exact, opening+"\n", opening+"\n")
			assertSegment(t, r.Source, content[1], 5+len(opening), end, parser.Exact, "- > opaque\n", "- > opaque\n")
		})
	}
	for _, opening := range []string{"```", "~~~"} {
		input := "- " + opening + "\n  - > opaque\noutside\n"
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		nodes := ungroupedChildren(r.Document.Root())
		if r.Failure != nil || len(nodes) != 2 || nodes[0].Kind() != parser.ListItem || nodes[0].Span().End() != 19 || nodes[1].Span().Start() != 19 {
			t.Fatal("unclosed fence crossed dedent", r)
		}
		code := nodes[0].Children()[0]
		if code.Kind() != parser.CodeBlock || code.Properties().Code.Style != "fenced" || code.Properties().Literal != parser.Some("- > opaque\n") || code.Span().Start() != 2 || code.Span().End() != 19 {
			t.Fatal("fence payload interpreted", code)
		}
	}
}

func TestListContinuationPartialTabsAndEndings(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, terminal := range []string{ending, ""} {
			// The item starts one column after the quote. Effective padding
			// consumes one column of byte 4; code indentation leaves three
			// columns of byte 5. Continuation shares tabs at both boundaries.
			input := ">  -\t\tX\x00" + ending + ">\t\t\tY" + terminal
			source := parser.NewSource([]byte(input))
			r := parser.Parse(context.Background(), source)
			if r.Failure != nil || r.Status != parser.Complete || conformance.Evaluate(r).Status != "rendered" {
				t.Fatal(r)
			}
			q := ungroupedChildren(r.Document.Root())[0]
			item := ungroupedChildren(q)[0]
			code := item.Children()[0]
			if q.Kind() != parser.BlockQuote || item.Kind() != parser.ListItem || len(item.Children()) != 1 || code.Kind() != parser.CodeBlock || code.Properties().Literal != parser.Some("   X\ufffd\n   Y\n") {
				t.Fatal("partial-tab code or item ownership", r)
			}
			if item.Span().Start() != 2 || item.Span().End() != len(input) || code.Span().Start() != 4 || code.Span().End() != len(input) || string(source.Bytes()) != input {
				t.Fatal("original byte envelopes")
			}
			prefix := item.Properties().List.Prefix
			if item.Properties().List.ContentIndent != parser.Some(3) || len(prefix.Segments) != 3 {
				t.Fatal("effective prefix facts")
			}
			assertSegment(t, source, prefix.Segments[2], 4, 5, parser.PartialTab, "\t", " ")
			segments := code.Content()
			if len(segments) != 7 {
				t.Fatal("lost code contributions", segments)
			}
			assertSegment(t, source, segments[0], 5, 6, parser.PartialTab, "\t", "   ")
			assertSegment(t, source, segments[1], 6, 7, parser.Exact, "X", "X")
			assertSegment(t, source, segments[2], 7, 8, parser.Normalized, "\x00", "\ufffd")
			start := 8 + len(ending)
			markers := item.Markers()
			if len(markers) != 3 || markers[2].Role != "item-indent" || markers[2].Source.Start() != start+1 || markers[2].Source.End() != start+3 {
				t.Fatal("lost consumed continuation tab bytes", markers)
			}
			if q.Markers()[1].Source.Start() != start || q.Markers()[1].Source.End() != start+2 {
				t.Fatal("quote/item shared original tab")
			}
			assertSegment(t, source, segments[4], start+3, start+4, parser.PartialTab, "\t", "   ")
			assertSegment(t, source, segments[5], start+4, start+5, parser.Exact, "Y", "Y")
			for _, index := range []int{3, 6} {
				seg := segments[index]
				want := parser.Exact
				raw := ending
				if index == 6 {
					raw = terminal
				}
				if raw == "" {
					want = parser.Synthetic
				} else if raw != "\n" {
					want = parser.Normalized
				}
				if seg.Transformation != want || seg.Semantic != parser.Some("\n") {
					t.Fatal("ending provenance", seg)
				}
				got, _ := source.Text(seg.Source)
				if got != raw {
					t.Fatal("ending selected wrong original bytes", got, raw)
				}
			}
		}
	}
}

func TestListContinuationBlankMarkers(t *testing.T) {
	input := "- first\n  \n  second\n\n- next\n"
	r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
	if r.Failure != nil {
		t.Fatal(r)
	}
	items := ungroupedChildren(r.Document.Root())
	markers := items[0].Markers()
	want := []markerWant{{"item-prefix", byteRange{0, 2}}, {"item-marker", byteRange{0, 1}}, {"item-indent", byteRange{8, 10}}, {"item-blank", byteRange{8, 11}}, {"item-indent", byteRange{11, 13}}, {"item-blank", byteRange{20, 21}}}
	if len(items) != 2 || items[0].Span().End() != 21 || items[1].Span().Start() != 21 || len(markers) != len(want) {
		t.Fatal("lost item/blank boundaries", items, markers)
	}
	for j, m := range markers {
		if m.Role != want[j].role || m.Source.Start() != want[j].span.start || m.Source.End() != want[j].span.end {
			t.Fatal("blank/indent evidence", m, want[j])
		}
	}
}

func TestListContinuationParagraphEndings(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, terminal := range []string{ending, ""} {
			input := "- é" + ending + "  β\x00" + terminal
			source := parser.NewSource([]byte(input))
			r := parser.Parse(context.Background(), source)
			if r.Failure != nil || r.Status != parser.Incomplete {
				t.Fatal(r)
			}
			item := ungroupedChildren(r.Document.Root())[0]
			paragraph := item.Children()[0]
			if item.Span().End() != len(input) || paragraph.Kind() != parser.Paragraph || paragraph.Span().Start() != 2 || paragraph.Span().End() != len(input) {
				t.Fatal("multibyte/endings ownership")
			}
			segments := paragraph.Content()
			if len(segments) != 3 {
				t.Fatal(segments)
			}
			start := 4 + len(ending)
			assertSegment(t, source, segments[0], 2, start, parser.Exact, "é"+ending, "é"+ending)
			assertSegment(t, source, segments[1], start+2, start+4, parser.Exact, "β", "β")
			assertSegment(t, source, segments[2], start+4, start+5, parser.Normalized, "\x00", "\ufffd")
			markers := item.Markers()
			if len(markers) != 3 || markers[2].Role != "item-indent" || markers[2].Source.Start() != start || markers[2].Source.End() != start+2 {
				t.Fatal("original ending/indent boundary", markers)
			}
		}
	}
}

func FuzzListContinuationOwnership(f *testing.F) {
	f.Add("first", "second", uint8(0), false, false)
	f.Add("longer", "continuation", uint8(3), true, true)
	f.Fuzz(func(t *testing.T, a, b string, quotes uint8, ordered, lazy bool) {
		// Generate only paragraph text, so the oracle is independent of Markdown
		// classification. Vary container depth, marker width and omitted prefixes.
		prose := func(raw string) string {
			if len(raw) > 128 {
				raw = raw[:128]
			}
			out := []byte(raw)
			for i := range out {
				out[i] = 'a' + out[i]%26
			}
			if len(out) == 0 {
				return "x"
			}
			return string(out)
		}
		a, b = prose(a), prose(b)
		depth := int(quotes % 8)
		prefix := strings.Repeat("> ", depth)
		marker, sibling := "- ", "- "
		if ordered {
			marker, sibling = "123. ", "4. "
		}
		continuation := prefix + strings.Repeat(" ", len(marker))
		if lazy {
			continuation = ""
		}
		first := prefix + marker + a + "\n"
		second := continuation + b + "\n"
		input := first + second + prefix + sibling + "sibling\n"
		end := len(first) + len(second)
		last := end + len(prefix) + len(sibling)
		want := []listTreeWant{
			itemTree(len(prefix), end, paragraphTree(len(prefix)+len(marker), end, a+"\n"+b, byteRange{len(prefix) + len(marker), len(first)}, byteRange{len(first) + len(continuation), end - 1})),
			itemTree(end+len(prefix), len(input), paragraphTree(last, len(input), "sibling", byteRange{last, len(input) - 1})),
		}
		for d := depth - 1; d >= 0; d-- {
			want = []listTreeWant{quoteTree(2*d, len(input), want...)}
		}
		checkListTrees(t, input, want)
	})
}
