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

// These helpers only assemble independently supplied source ranges/column facts.
// They never classify Markdown or inspect parser/scanner/renderer output.
func bulletWant(envelope, prefix, marker byteRange, bullet byte, padding, indent int, text string, content ...byteRange) atxBlockWant {
	w := atxBlockWant{kind: parser.ListItem, envelope: envelope,
		markers: []markerWant{{"item-prefix", prefix}, {"item-marker", marker}},
		list:    &parser.ListProperties{Bullet: parser.Some(bullet), MarkerWidth: parser.Some(1), Padding: parser.Some(padding), ContentIndent: parser.Some(indent)}}
	if len(content) != 0 {
		w.children = []atxBlockWant{atxParagraphWant(byteRange{prefix.end, envelope.end}, text, content...)}
	}
	return w
}

func orderedWant(envelope, prefix, marker byteRange, start int, delimiter byte, padding, indent int, text string, content ...byteRange) atxBlockWant {
	w := bulletWant(envelope, prefix, marker, '-', padding, indent, text, content...)
	w.list.Ordered, w.list.Bullet = true, parser.Optional[byte]{}
	w.list.Start, w.list.Delimiter = parser.Some(start), parser.Some(delimiter)
	w.list.MarkerWidth = parser.Some(marker.end - marker.start)
	return w
}

// Continue an independently specified item with explicit original-byte boundary
// markers and block children. No source classification happens in this helper.
func continuedItemWant(w atxBlockWant, end int, markers []markerWant, children ...atxBlockWant) atxBlockWant {
	w.envelope.end = end
	w.markers = append(w.markers, markers...)
	if children != nil {
		w.children = children
	}
	return w
}

func TestListMarkerUpstreamTargets(t *testing.T) {
	// Retain independently specified opening marker/padding facts. The separate
	// TestListContinuationUpstreamTargets asserts full trees and source spans for
	// all 48 inputs; separate grouping tests assert List ancestry and tightness.
	wants := map[int][]atxBlockWant{
		254: {orderedWant(byteRange{0, 16}, byteRange{0, 4}, byteRange{0, 2}, 1, '.', 2, 4, "A paragraph", byteRange{4, 15})},
		255: {bulletWant(byteRange{0, 6}, byteRange{0, 2}, byteRange{0, 1}, '-', 1, 2, "one", byteRange{2, 5})},
		256: {bulletWant(byteRange{0, 6}, byteRange{0, 2}, byteRange{0, 1}, '-', 1, 2, "one", byteRange{2, 5})},
		257: {bulletWant(byteRange{0, 10}, byteRange{0, 6}, byteRange{1, 2}, '-', 4, 6, "one", byteRange{6, 9})},
		258: {bulletWant(byteRange{0, 10}, byteRange{0, 6}, byteRange{1, 2}, '-', 4, 6, "one", byteRange{6, 9})},
		259: {quoteWant(byteRange{0, 15}, []byteRange{{0, 5}}, quoteWant(byteRange{5, 15}, []byteRange{{5, 7}}, orderedWant(byteRange{7, 15}, byteRange{7, 11}, byteRange{7, 9}, 1, '.', 2, 4, "one", byteRange{11, 14})))},
		260: {quoteWant(byteRange{0, 8}, []byteRange{{0, 1}}, quoteWant(byteRange{1, 8}, []byteRange{{1, 2}}, bulletWant(byteRange{2, 8}, byteRange{2, 4}, byteRange{2, 3}, '-', 1, 2, "one", byteRange{4, 7})))},
		261: {atxParagraphWant(byteRange{0, 5}, "-one", byteRange{0, 4}), atxParagraphWant(byteRange{6, 12}, "2.two", byteRange{6, 11})},
		262: {bulletWant(byteRange{0, 6}, byteRange{0, 2}, byteRange{0, 1}, '-', 1, 2, "foo", byteRange{2, 5})},
		263: {orderedWant(byteRange{0, 8}, byteRange{0, 4}, byteRange{0, 2}, 1, '.', 2, 4, "foo", byteRange{4, 7})},
		264: {bulletWant(byteRange{0, 6}, byteRange{0, 2}, byteRange{0, 1}, '-', 1, 2, "Foo", byteRange{2, 5})},
		265: {orderedWant(byteRange{0, 14}, byteRange{0, 11}, byteRange{0, 10}, 123456789, '.', 1, 11, "ok", byteRange{11, 13})},
		266: {atxParagraphWant(byteRange{0, 19}, "1234567890. not ok", byteRange{0, 18})},
		267: {orderedWant(byteRange{0, 6}, byteRange{0, 3}, byteRange{0, 2}, 0, '.', 1, 3, "ok", byteRange{3, 5})},
		268: {orderedWant(byteRange{0, 8}, byteRange{0, 5}, byteRange{0, 4}, 3, '.', 1, 5, "ok", byteRange{5, 7})},
		269: {atxParagraphWant(byteRange{0, 11}, "-1. not ok", byteRange{0, 10})},
		270: {bulletWant(byteRange{0, 6}, byteRange{0, 2}, byteRange{0, 1}, '-', 1, 2, "foo", byteRange{2, 5})},
		271: {orderedWant(byteRange{0, 11}, byteRange{0, 7}, byteRange{2, 5}, 10, '.', 2, 7, "foo", byteRange{7, 10})},
		273: {orderedWant(byteRange{0, 21}, byteRange{0, 3}, byteRange{0, 2}, 1, '.', 1, 3, "    indented code", byteRange{3, 20})},
		274: {orderedWant(byteRange{0, 22}, byteRange{0, 3}, byteRange{0, 2}, 1, '.', 1, 3, "     indented code", byteRange{3, 21})},
		275: {atxParagraphWant(byteRange{0, 7}, "foo", byteRange{3, 6}), atxParagraphWant(byteRange{8, 12}, "bar", byteRange{8, 11})},
		276: {bulletWant(byteRange{0, 9}, byteRange{0, 5}, byteRange{0, 1}, '-', 4, 5, "foo", byteRange{5, 8})},
		277: {bulletWant(byteRange{0, 7}, byteRange{0, 3}, byteRange{0, 1}, '-', 2, 3, "foo", byteRange{3, 6})},
		278: {bulletWant(byteRange{0, 2}, byteRange{0, 1}, byteRange{0, 1}, '-', 1, 2, "")},
		279: {bulletWant(byteRange{0, 5}, byteRange{0, 4}, byteRange{0, 1}, '-', 1, 2, "")},
		280: {bulletWant(byteRange{0, 2}, byteRange{0, 1}, byteRange{0, 1}, '-', 1, 2, "")},
		281: {bulletWant(byteRange{0, 6}, byteRange{0, 2}, byteRange{0, 1}, '-', 1, 2, "foo", byteRange{2, 5})},
		282: {bulletWant(byteRange{0, 6}, byteRange{0, 2}, byteRange{0, 1}, '-', 1, 2, "foo", byteRange{2, 5})},
		283: {orderedWant(byteRange{0, 7}, byteRange{0, 3}, byteRange{0, 2}, 1, '.', 1, 3, "foo", byteRange{3, 6})},
		284: {bulletWant(byteRange{0, 2}, byteRange{0, 1}, byteRange{0, 1}, '*', 1, 2, "")},
		285: {atxParagraphWant(byteRange{0, 6}, "foo\n*", byteRange{0, 4}, byteRange{4, 5}), atxParagraphWant(byteRange{7, 14}, "foo\n1.", byteRange{7, 11}, byteRange{11, 13})},
		286: {orderedWant(byteRange{0, 17}, byteRange{0, 5}, byteRange{1, 3}, 1, '.', 2, 5, "A paragraph", byteRange{5, 16})},
		287: {orderedWant(byteRange{0, 18}, byteRange{0, 6}, byteRange{2, 4}, 1, '.', 2, 6, "A paragraph", byteRange{6, 17})},
		288: {orderedWant(byteRange{0, 19}, byteRange{0, 7}, byteRange{3, 5}, 1, '.', 2, 7, "A paragraph", byteRange{7, 18})},
		290: {orderedWant(byteRange{0, 18}, byteRange{0, 6}, byteRange{2, 4}, 1, '.', 2, 6, "A paragraph", byteRange{6, 17})},
		291: {orderedWant(byteRange{0, 18}, byteRange{0, 6}, byteRange{2, 4}, 1, '.', 2, 6, "A paragraph", byteRange{6, 17})},
		292: {quoteWant(byteRange{0, 18}, []byteRange{{0, 2}}, orderedWant(byteRange{2, 18}, byteRange{2, 5}, byteRange{2, 4}, 1, '.', 1, 3, "> Blockquote", byteRange{5, 17}))},
		293: {quoteWant(byteRange{0, 18}, []byteRange{{0, 2}}, orderedWant(byteRange{2, 18}, byteRange{2, 5}, byteRange{2, 4}, 1, '.', 1, 3, "> Blockquote", byteRange{5, 17}))},
		294: {bulletWant(byteRange{0, 6}, byteRange{0, 2}, byteRange{0, 1}, '-', 1, 2, "foo", byteRange{2, 5})},
		295: {bulletWant(byteRange{0, 6}, byteRange{0, 2}, byteRange{0, 1}, '-', 1, 2, "foo", byteRange{2, 5})},
		296: {orderedWant(byteRange{0, 8}, byteRange{0, 4}, byteRange{0, 3}, 10, ')', 1, 4, "foo", byteRange{4, 7})},
		297: {orderedWant(byteRange{0, 8}, byteRange{0, 4}, byteRange{0, 3}, 10, ')', 1, 4, "foo", byteRange{4, 7})},
		298: {bulletWant(byteRange{0, 8}, byteRange{0, 2}, byteRange{0, 1}, '-', 1, 2, "- foo", byteRange{2, 7})},
		299: {orderedWant(byteRange{0, 12}, byteRange{0, 3}, byteRange{0, 2}, 1, '.', 1, 3, "- 2. foo", byteRange{3, 11})},
		300: {bulletWant(byteRange{0, 8}, byteRange{0, 2}, byteRange{0, 1}, '-', 1, 2, "# Foo", byteRange{2, 7})},
	}
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
		if e.Section != "List items" || seen[e.ID] {
			t.Fatal("wrong original identity", e.ID)
		}
		seen[e.ID] = true
		t.Run(strconv.Itoa(e.ID), func(t *testing.T) {
			if e.ID == 253 || e.ID == 272 || e.ID == 289 {
				checkListIntroductoryCases(t, e.ID, e.Markdown, e.HTML)
				return
			}
			want, ok := wants[e.ID]
			if !ok {
				t.Fatal("unaccounted original case")
			}
			// Non-item negative cases retain their original paragraph assertions.
			w := want[0]
			for w.kind == parser.BlockQuote {
				w = w.children[0]
			}
			if w.kind != parser.ListItem {
				checkATX(t, e.Markdown, false, want)
				return
			}
			r := parser.Parse(context.Background(), parser.NewSource([]byte(e.Markdown)))
			var item *parser.Node
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if item == nil && n.Kind() == parser.ListItem {
					item = n
				}
				return true
			})
			if r.Failure != nil || item == nil {
				t.Fatal("lost opening marker", r)
			}
			m, props := item.Markers(), item.Properties().List
			if props == nil || props.Ordered != w.list.Ordered || props.Start != w.list.Start || props.Delimiter != w.list.Delimiter || props.Bullet != w.list.Bullet || props.MarkerWidth != w.list.MarkerWidth || props.Padding != w.list.Padding || props.ContentIndent != w.list.ContentIndent || props.Tight.Present || props.Prefix == nil {
				t.Fatal("opening properties changed", props)
			}
			for j := range w.markers {
				if m[j].Role != w.markers[j].role || m[j].Source.Start() != w.markers[j].span.start || m[j].Source.End() != w.markers[j].span.end {
					t.Fatal("opening marker source changed", m)
				}
			}
		})
	}
	if len(seen) != 48 || len(wants) != 45 {
		t.Fatal("not all 253–300 cases asserted")
	}
}

func checkListIntroductoryCases(t *testing.T, id int, input, html string) {
	t.Helper()
	source := parser.NewSource([]byte(input))
	r := parser.Parse(context.Background(), source)
	if r.Failure != nil || r.Document == nil || string(r.Source.Bytes()) != input {
		t.Fatal(r)
	}
	var kinds []parser.Kind
	parser.Walk(r.Document.Root(), func(n *parser.Node) bool { kinds = append(kinds, n.Kind()); return true })
	if id == 289 {
		if !reflect.DeepEqual(kinds, []parser.Kind{parser.DocumentKind, parser.CodeBlock}) || r.Status != parser.Complete || conformance.Evaluate(r).HTML != html {
			t.Fatal("four-column list-looking code changed", r)
		}
		n := ungroupedChildren(r.Document.Root())[0]
		if n.Span().Start() != 0 || n.Span().End() != len(input) || n.Properties().Literal != parser.Some("1.  A paragraph\n    with two lines.\n\n        indented code\n\n    > A block quote.\n") {
			t.Fatal("code source/literal", n)
		}
		return
	}
	if id == 253 {
		checkATX(t, input, false, []atxBlockWant{
			atxParagraphWant(byteRange{0, 28}, "A paragraph\nwith two lines.", byteRange{0, 12}, byteRange{12, 27}),
			indentedWant(byteRange{29, 47}, "indented code\n", []byteRange{{29, 33}}, byteRange{33, 46}, byteRange{46, 47}),
			quoteWant(byteRange{48, 65}, []byteRange{{48, 50}}, atxParagraphWant(byteRange{50, 65}, "A block quote.", byteRange{50, 64})),
		})
	} else {
		checkATX(t, input, false, []atxBlockWant{
			indentedWant(byteRange{0, 18}, "indented code\n", []byteRange{{0, 4}}, byteRange{4, 17}, byteRange{17, 18}),
			atxParagraphWant(byteRange{19, 29}, "paragraph", byteRange{19, 28}),
			indentedWant(byteRange{30, 44}, "more code\n", []byteRange{{30, 34}}, byteRange{34, 43}, byteRange{43, 44}),
		})
	}
}

func TestListMarkerTabProvenance(t *testing.T) {
	type contribution struct {
		start, end     int
		transformation parser.Transformation
		text           string
	}
	for _, tt := range []struct {
		input                  string
		width, padding, indent int
		prefix, marker         byteRange
		consumed, content      []contribution
	}{
		{"-\tfoo\n", 1, 3, 4, byteRange{0, 2}, byteRange{0, 1}, []contribution{{0, 1, parser.Exact, "-"}, {1, 2, parser.PartialTab, "   "}}, []contribution{{2, 5, parser.Exact, "foo"}}},
		{"1.\tfoo\n", 2, 2, 4, byteRange{0, 3}, byteRange{0, 2}, []contribution{{0, 2, parser.Exact, "1."}, {2, 3, parser.PartialTab, "  "}}, []contribution{{3, 6, parser.Exact, "foo"}}},
		{"12.\tfoo\n", 3, 1, 4, byteRange{0, 4}, byteRange{0, 3}, []contribution{{0, 3, parser.Exact, "12."}, {3, 4, parser.PartialTab, " "}}, []contribution{{4, 7, parser.Exact, "foo"}}},
		{"123.\tfoo\n", 4, 4, 8, byteRange{0, 5}, byteRange{0, 4}, []contribution{{0, 4, parser.Exact, "123."}, {4, 5, parser.PartialTab, "    "}}, []contribution{{5, 8, parser.Exact, "foo"}}},
		{"-\t\tfoo\n", 1, 1, 2, byteRange{0, 2}, byteRange{0, 1}, []contribution{{0, 1, parser.Exact, "-"}, {1, 2, parser.PartialTab, " "}}, []contribution{{1, 2, parser.PartialTab, "  "}, {2, 6, parser.Exact, "\tfoo"}}},
		{"> -\tfoo\n", 1, 1, 2, byteRange{2, 4}, byteRange{2, 3}, []contribution{{2, 3, parser.Exact, "-"}, {3, 4, parser.PartialTab, " "}}, []contribution{{4, 7, parser.Exact, "foo"}}},
		{" >\t-\tfoo\n", 1, 3, 5, byteRange{2, 5}, byteRange{3, 4}, []contribution{{2, 3, parser.PartialTab, " "}, {3, 4, parser.Exact, "-"}, {4, 5, parser.PartialTab, "   "}}, []contribution{{5, 8, parser.Exact, "foo"}}},
		{"*\t \t\n", 1, 1, 2, byteRange{0, 4}, byteRange{0, 1}, []contribution{{0, 1, parser.Exact, "*"}, {1, 2, parser.PartialTab, "   "}, {2, 3, parser.Exact, " "}, {3, 4, parser.PartialTab, "   "}}, nil},
	} {
		t.Run(tt.input, func(t *testing.T) {
			source := parser.NewSource([]byte(tt.input))
			r := parser.Parse(context.Background(), source)
			var items []*parser.Node
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() == parser.ListItem {
					items = append(items, n)
				}
				return true
			})
			if r.Failure != nil || r.Status != parser.Complete || len(items) != 1 || conformance.Evaluate(r).Status != "rendered" {
				t.Fatal("supported tabbed text", r)
			}

			n := items[0]
			props := n.Properties().List
			if props == nil || props.MarkerWidth != parser.Some(tt.width) || props.Padding != parser.Some(tt.padding) || props.ContentIndent != parser.Some(tt.indent) || props.Tight.Present {
				t.Fatal("wrong structural facts", props)
			}
			markers := n.Markers()
			if len(markers) != 2 || markers[0].Role != "item-prefix" || markers[1].Role != "item-marker" || markers[0].Source.Start() != tt.prefix.start || markers[0].Source.End() != tt.prefix.end || markers[1].Source.Start() != tt.marker.start || markers[1].Source.End() != tt.marker.end {
				t.Fatal("wrong original token ranges", markers)
			}
			check := func(got []parser.Segment, want []contribution) string {
				t.Helper()
				if len(got) != len(want) {
					t.Fatalf("contributions %v want %v", got, want)
				}
				var text string
				for i, s := range got {
					w := want[i]
					if s.Source.Start() != w.start || s.Source.End() != w.end || s.Transformation != w.transformation || s.Semantic != parser.Some(w.text) {
						t.Fatal("lost original tab contribution", s, w)
					}
					if _, err := source.Text(s.Source); err != nil {
						t.Fatal(err)
					}
					if _, err := parser.NewSource([]byte(tt.input)).Text(s.Source); !errors.Is(err, parser.ErrRange) {
						t.Fatal("foreign snapshot accepted contribution")
					}
					text += w.text
				}
				return text
			}
			if props.Prefix == nil || props.Prefix.Semantic != parser.Some(check(props.Prefix.Segments, tt.consumed)) {
				t.Fatal("prefix semantic mapping")
			}
			if len(tt.content) != 0 {
				child := n.Children()[0]
				if strings.Contains(tt.input, "\t\tfoo") {
					// Indented code consumes four of the six residual columns;
					// the second original tab contributes two remaining spaces.
					if child.Kind() != parser.CodeBlock || child.Properties().Literal != parser.Some("  foo\n") {
						t.Fatal("padding did not open code")
					}
					check(child.Content(), []contribution{{2, 3, parser.PartialTab, "  "}, {3, 6, parser.Exact, "foo"}, {6, 7, parser.Exact, "\n"}})
				} else {
					check(child.Content(), tt.content)
				}
			} else if len(n.Children()) != 0 {
				t.Fatal("empty item acquired children")
			}
			if strings.HasPrefix(tt.input, " >") {
				q := ungroupedChildren(r.Document.Root())[0]
				if q.Markers()[0].Source.End() != 3 || n.Span().Start() != 2 {
					t.Fatal("shared quote/item partial-tab byte lost")
				}
			}
		})
	}
}

func TestListMarkerLongEmptyPadding(t *testing.T) {
	for _, count := range []int{1024, 16384, 65536} {
		for _, padding := range []string{"\t", " "} {
			t.Run(strconv.Itoa(count)+"/"+strconv.Quote(padding), func(t *testing.T) {
				input := "-" + strings.Repeat(padding, count) + "\n"
				source := parser.NewSource([]byte(input))
				r := parser.Parse(context.Background(), source)
				if r.Status != parser.Complete || r.Failure != nil || r.Document.References().LookupAvailability != parser.Available || conformance.Evaluate(r).Status != "rendered" {
					t.Fatal("empty list did not complete", r.Status)
				}
				nodes := ungroupedChildren(r.Document.Root())
				if len(nodes) != 1 || nodes[0].Kind() != parser.ListItem || len(nodes[0].Children()) != 0 || len(nodes[0].Content()) != 0 {
					t.Fatal("empty item acquired content/structure")
				}
				n := nodes[0]
				p := n.Properties().List
				if p.MarkerWidth != parser.Some(1) || p.Padding != parser.Some(1) || p.ContentIndent != parser.Some(2) || p.Tight.Present {
					t.Fatal("actual blank padding changed effective defaults", p)
				}
				if n.Span().End() != len(input) || n.Markers()[0].Source.End() != len(input)-1 || string(source.Bytes()) != input {
					t.Fatal("padding bytes/ranges lost")
				}
				if padding == " " {
					if len(p.Prefix.Segments) != 2 || p.Prefix.Semantic != parser.Some("-"+strings.Repeat(" ", count)) {
						t.Fatal("space run not retained")
					}
					assertSegment(t, source, p.Prefix.Segments[1], 1, count+1, parser.Exact, strings.Repeat(" ", count), strings.Repeat(" ", count))
					return
				}
				if len(p.Prefix.Segments) != count+1 || p.Prefix.Semantic != parser.Some("-"+strings.Repeat(" ", 4*count-1)) {
					t.Fatal("tab contributions or aggregate changed")
				}
				for i, segment := range p.Prefix.Segments[1:] {
					columns := "    "
					if i == 0 {
						columns = "   "
					}
					assertSegment(t, source, segment, i+1, i+2, parser.PartialTab, "\t", columns)
				}
			})
		}
	}
	const count = 1024
	input := " >\t-" + strings.Repeat("\t ", count) + "\n"
	source := parser.NewSource([]byte(input))
	r := parser.Parse(context.Background(), source)
	q := ungroupedChildren(r.Document.Root())[0]
	item := ungroupedChildren(q)[0]
	p := item.Properties().List
	if r.Failure != nil || r.Status != parser.Complete || q.Markers()[0].Source.End() != 3 || item.Span().Start() != 2 || p.ContentIndent != parser.Some(3) || p.Prefix.Semantic != parser.Some(" -"+strings.Repeat(" ", 4*count)) || len(p.Prefix.Segments) != 2*count+2 {
		t.Fatal("mixed long prefix lost quote phase or source")
	}
	assertSegment(t, source, p.Prefix.Segments[0], 2, 3, parser.PartialTab, "\t", " ")
	for i := 0; i < count; i++ {
		assertSegment(t, source, p.Prefix.Segments[2+2*i], 4+2*i, 5+2*i, parser.PartialTab, "\t", "   ")
		assertSegment(t, source, p.Prefix.Segments[3+2*i], 5+2*i, 6+2*i, parser.Exact, " ", " ")
	}
}

func TestListMarkerParagraphInterruption(t *testing.T) {
	for _, tt := range []struct {
		candidate  string
		interrupts bool
	}{
		{"* item", true}, {"+ item", true}, {"- item", true}, {"1. item", true}, {"01) item", true},
		{"0. item", false}, {"2) item", false}, {"*", false}, {"+ \t", false}, {"1.", false},
		{"-one", false}, {"2.two", false}, {"1234567890. x", false}, {"-1. x", false},
		{"１. x", false}, {"1.\u00a0x", false}, {"+\u00a0x", false}, {"*x*", false},
	} {
		for _, quote := range []string{"", "> "} {
			input := quote + "foo\n" + tt.candidate + "\n"
			t.Run(input, func(t *testing.T) {
				r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
				if r.Failure != nil {
					t.Fatal(r)
				}
				nodes := ungroupedChildren(r.Document.Root())
				if tt.interrupts {
					if len(nodes) != 2 || nodes[1].Kind() != parser.ListItem || nodes[1].Span().Start() != len(quote)+4 || nodes[1].Span().End() != len(input) {
						t.Fatal("inconsistent ordinary/quoted interruption", nodes)
					}
					if quote != "" {
						nodes = nodes[0].Children()
					}
					if nodes[0].Kind() != parser.Paragraph || nodes[0].Span().End() != len(quote)+4 {
						t.Fatal("interrupted paragraph lost source")
					}
				} else {
					if len(nodes) != 1 {
						t.Fatal("nonmarker split paragraph", nodes)
					}
					if quote != "" {
						nodes = nodes[0].Children()
					}
					if len(nodes) != 1 || nodes[0].Kind() != parser.Paragraph || nodes[0].Span().End() != len(input) {
						t.Fatal("noninterrupting item lost lazy paragraph")
					}
					var content string
					for _, segment := range nodes[0].Content() {
						content += segment.Semantic.Value
					}
					if content != "foo\n"+strings.TrimRight(tt.candidate, " \t") {
						t.Fatal("paragraph fallback lost text", content)
					}
				}
			})
		}
	}
}

func TestListMarkerNumericAndPaddingBoundaries(t *testing.T) {
	for _, marker := range []string{"-", "+", "*", "0.", "1.", "1)", "01)", "999999999.", "000000000)"} {
		for leading := 0; leading <= 4; leading++ {
			for spaces := 1; spaces <= 6; spaces++ {
				input := strings.Repeat(" ", leading) + marker + strings.Repeat(" ", spaces) + "x\n"
				t.Run(input, func(t *testing.T) {
					source := parser.NewSource([]byte(input))
					r := parser.Parse(context.Background(), source)
					if r.Failure != nil || string(r.Source.Bytes()) != input {
						t.Fatal(r)
					}
					nodes := ungroupedChildren(r.Document.Root())
					if len(nodes) != 1 {
						t.Fatal("lost item or code block", nodes)
					}
					if leading == 4 {
						if nodes[0].Kind() != parser.CodeBlock || r.Status != parser.Complete {
							t.Fatal("four leading columns became list")
						}
						return
					}
					n := nodes[0]
					p := n.Properties().List
					padding := spaces
					if spaces > 4 {
						padding = 1
					}
					wantStatus := parser.Complete
					if n.Kind() != parser.ListItem || r.Status != wantStatus || p == nil || p.MarkerWidth != parser.Some(len(marker)) || p.Padding != parser.Some(padding) || p.ContentIndent != parser.Some(leading+len(marker)+padding) || p.Tight.Present {
						t.Fatal("wrong marker width/effective padding", p)
					}
					raw, _ := source.Text(n.Markers()[1].Source)
					if raw != marker || n.Markers()[1].Source.Start() != leading || n.Markers()[0].Source.End() != leading+len(marker)+padding {
						t.Fatal("original marker/prefix ranges", n.Markers())
					}
					var content string
					child := n.Children()[0]
					for _, seg := range child.Content() {
						content += seg.Semantic.Value
					}
					wantContent := "x"
					if spaces > 4 {
						wantContent = strings.Repeat(" ", spaces-padding-4) + "x\n"
					}
					if content != wantContent {
						t.Fatal("padding swallowed content", content)
					}
				})
			}
		}
	}
	for _, input := range []string{"-one", "2.two", "1234567890. text", "0000000000) text", "-1. text", "１２. text", "1:\ttext", "1.\u00a0text", "*\vtext", "+\ftext", "\u00a0- x"} {
		t.Run(input, func(t *testing.T) {
			r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
			if r.Failure != nil || len(ungroupedChildren(r.Document.Root())) != 1 || ungroupedChildren(r.Document.Root())[0].Kind() != parser.Paragraph {
				t.Fatal("negative marker did not retain paragraph", r)
			}
		})
	}
}

func TestListMarkerBytesEndingsAndUnavailableSuffix(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r", ""} {
		input := "003. é\x00 \t" + ending
		bytes := []byte(input)
		source := parser.NewSource(bytes)
		r := parser.Parse(context.Background(), source)
		if r.Failure != nil || r.Document.References().LookupAvailability != parser.Available {
			t.Fatal(r)
		}
		n := ungroupedChildren(r.Document.Root())[0]
		if n.Kind() != parser.ListItem || n.Span().End() != len(input) || n.Properties().List.Start != parser.Some(3) || n.Properties().List.Tight.Present || len(n.Children()) != 1 {
			t.Fatal("false item completion")
		}
		segments := n.Children()[0].Content()
		if len(segments) != 2 {
			t.Fatal("NUL source mapping", segments)
		}
		assertSegment(t, source, segments[0], 5, 7, parser.Exact, "é", "é")
		assertSegment(t, source, segments[1], 7, 8, parser.Normalized, "\x00", "\ufffd")
		props := n.Properties()
		props.List.Padding = parser.Some(4)
		props.List.Prefix.Segments[0].Source = parser.Span{}
		props.List.Prefix.Semantic = parser.Some("changed")
		markers := n.Markers()
		markers[0].Role = "changed"
		bytes[0] = '!'
		copy := source.Bytes()
		copy[0] = '!'
		if !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) || string(source.Bytes()) != input {
			t.Fatal("list accessors/source not immutable")
		}
	}
	checkATX(t, "003.", true, []atxBlockWant{orderedWant(byteRange{0, 4}, byteRange{0, 4}, byteRange{0, 4}, 3, '.', 1, 5, "")})
	checkATX(t, "   -\n", true, []atxBlockWant{bulletWant(byteRange{0, 5}, byteRange{0, 4}, byteRange{3, 4}, '-', 1, 5, "")})
	for _, input := range []string{"```\n- item\n```\n", "<!--\n1. item\n-->\n", "<div>\n* item\n"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		if r.Status != parser.Complete || r.Failure != nil {
			t.Fatal(r)
		}
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.ListItem {
				t.Fatal("opaque payload became item")
			}
			return true
		})
	}
}

func FuzzListMarkerSourceFacts(f *testing.F) {
	for _, input := range []string{"- [a][K] [k][] [K]\n\n[k]: /a&amp;b \"title\"\n", "- [a *b*](uri)\n", "> - [é\x00\r\n>   **β**](<> '')", "- [bad](a b) [good](ok)\n", "- _a **b *c* d** e_\n", "> - ***é\x00\r\n>   β***", "- *x* **y**\n", "> - ***left**\r\n", "- ***nested***\n", "- x <i>text</i>\n", "> - x <!-->\n", "- item\n", "003.\tfoo\n", "-\t\tfoo\n", " >\t-\tfoo\n", "foo\n1.\n", "1234567890. x\n", "*\t \t\n", "- é\x00\r\n", "\xff"} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 4096 {
			return
		} // A bounded smoke target, not full work metering.
		source := parser.NewSource([]byte(input))
		r := parser.Parse(context.Background(), source)
		if string(r.Source.Bytes()) != input || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
			t.Fatal("source loss or nondeterminism")
		}
		if r.Failure != nil {
			if r.Failure.Code != parser.InvalidUTF8 {
				t.Fatal("valid source violated model invariant", r.Failure)
			}
			return
		}
		if r.Status == parser.Complete && (r.AST != parser.Available || len(r.Uncertainty) != 0 || conformance.Evaluate(r).Status != "rendered") {
			t.Fatal("completed source lacks an established renderable outcome")
		}
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if r.Status == parser.Complete && (n.Kind() == parser.Paragraph || n.Kind() == parser.Heading) && len(n.Content()) != 0 {
				if len(n.Children()) == 0 {
					t.Fatal("completed inline work lacks children")
				}
				checkInlineSourceCoverage(t, n)
				for _, child := range n.Children() {
					switch child.Kind() {
					case parser.Text, parser.CodeSpan:
						if !child.Properties().Literal.Present {
							t.Fatal("completed inline lacks literal semantics")
						}
					case parser.Emphasis, parser.Strong:
						checkEmphasisProvenance(t, source, child)
					case parser.InlineHTML:
						checkInlineHTMLProvenance(t, source, child)
					case parser.Link:
						checkLinkProvenance(t, source, child)
					case parser.SoftBreak, parser.HardBreak:
						checkBreakProperties(t, source, child, child.Properties().Break.Origin,
							byteRange{child.Span().Start(), child.Content()[0].Source.Start()})
					default:
						t.Fatal("unknown completed inline", child.Kind())
					}
				}
			}
			if n.Kind() != parser.ListItem {
				return true
			}
			if len(n.Content()) != 0 {
				t.Fatal("items acquired raw payload")
			}
			p := n.Properties().List
			if p == nil || p.Tight.Present || p.Prefix == nil {
				t.Fatal("missing item prefix or invented tightness")
			}
			markers := n.Markers()
			if len(markers) < 2 {
				t.Fatal("missing original marker")
			}
			raw, err := source.Text(markers[1].Source)
			if err != nil || len(raw) != p.MarkerWidth.Value {
				t.Fatal("marker width/source disagreement", raw, p)
			}
			if p.Ordered {
				value, err := strconv.ParseUint(raw[:len(raw)-1], 10, 32)
				if err != nil || uint64(p.Start.Value) != value || raw[len(raw)-1] != p.Delimiter.Value {
					t.Fatal("ordinal differs from original spelling", raw, p)
				}
			} else if len(raw) != 1 || raw[0] != p.Bullet.Value {
				t.Fatal("bullet differs from original spelling", raw, p)
			}
			for _, segment := range append(p.Prefix.Segments, n.Content()...) {
				raw, err := source.Text(segment.Source)
				if err != nil || segment.Source.Start() < n.Span().Start() || segment.Source.End() > n.Span().End() {
					t.Fatal("item contribution lost source ownership")
				}
				if segment.Transformation == parser.PartialTab && (raw != "\t" || len(segment.Semantic.Value) < 1 || len(segment.Semantic.Value) > 4 || strings.Trim(segment.Semantic.Value, " ") != "") {
					t.Fatal("invalid shared-tab contribution")
				}
			}
			return true
		})
	})
}
