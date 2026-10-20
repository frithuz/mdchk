package parser_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

type atxBlockWant struct {
	kind     parser.Kind
	envelope byteRange
	level    int
	style    string
	markers  []markerWant
	content  []byteRange
	text     string
	fence    byte
	width    int
	info     string
	infoSpan byteRange
	htmlForm int
	list     *parser.ListProperties
	children []atxBlockWant
	inlines  []inlineWant
}
type markerWant struct {
	role string
	span byteRange
}

// All ranges passed to these fixture constructors are independent, original-byte
// expectations. They never inspect parser/scanner output or search source text.
func atxHeadingWant(level int, envelope, opening, content, closing byteRange, text string) atxBlockWant {
	w := atxBlockWant{kind: parser.Heading, envelope: envelope, level: level, text: text,
		markers: []markerWant{{"opening", opening}}}
	if content != (byteRange{}) {
		w.content = []byteRange{content}
	}
	if closing != (byteRange{}) {
		w.markers = append(w.markers, markerWant{"closing", closing})
	}
	return w
}
func atxParagraphWant(envelope byteRange, text string, content ...byteRange) atxBlockWant {
	return atxBlockWant{kind: parser.Paragraph, envelope: envelope, content: content, text: text}
}
func atxThematicWant(envelope, marker byteRange) atxBlockWant {
	return atxBlockWant{kind: parser.ThematicBreak, envelope: envelope, markers: []markerWant{{"thematic-marker", marker}}}
}

func checkATX(t *testing.T, input string, complete bool, want []atxBlockWant) parser.ParseResult {
	t.Helper()
	original := []byte(input)
	source := parser.NewSource(original)
	r := parser.Parse(context.Background(), source)
	if r.Failure != nil || r.Interruption != "" || r.Document == nil || string(r.Source.Bytes()) != input {
		t.Fatalf("unexpected parse result: %+v", r)
	}
	refs := r.Document.References()
	checkStagedTextOutcome(t, r, complete)
	if len(refs.Definitions) != 0 || len(refs.Lookup) != 0 {
		t.Fatal("invented references")
	}
	root := r.Document.Root()
	if root.Span().Start() != 0 || root.Span().End() != len(input) {
		t.Fatal("wrong root envelope")
	}
	nodes := ungroupedChildren(root)
	if len(nodes) != len(want) {
		t.Fatalf("got %d blocks, want %d: %q", len(nodes), len(want), input)
	}
	foreign := parser.NewSource([]byte(input))
	checkRange := func(span parser.Span, w byteRange) {
		t.Helper()
		if !span.Present() || span.Start() != w.start || span.End() != w.end {
			t.Fatalf("range [%d,%d), want %+v", span.Start(), span.End(), w)
		}
		if _, err := source.Text(span); err != nil {
			t.Fatal("span has wrong owner", err)
		}
		if _, err := foreign.Text(span); !errors.Is(err, parser.ErrRange) {
			t.Fatal("independent source accepted span")
		}
	}
	var checkNodes func([]*parser.Node, []atxBlockWant)
	checkNodes = func(nodes []*parser.Node, want []atxBlockWant) {
		if len(nodes) != len(want) {
			t.Fatalf("got %d nested blocks, want %d", len(nodes), len(want))
		}
		for i, n := range nodes {
			w := want[i]
			if n.Kind() != w.kind {
				t.Fatalf("block %d wrong kind or invented inline children", i)
			}
			checkRange(n.Span(), w.envelope)
			props := parser.Properties{}
			if w.kind == parser.Heading {
				style := w.style
				if style == "" {
					style = "atx"
				}
				props.Heading = &parser.HeadingProperties{Level: w.level, Style: style}
			}
			if w.kind == parser.CodeBlock {
				props.Code = &parser.CodeProperties{Style: "indented"}
				if w.style == "fenced" {
					info := parser.Value{Semantic: parser.Some(w.info)}
					if w.infoSpan != (byteRange{}) {
						span, err := source.Span(w.infoSpan.start, w.infoSpan.end)
						if err != nil {
							t.Fatal(err)
						}
						info.Segments = []parser.Segment{{Source: span, Transformation: parser.Exact, Semantic: parser.Some(w.info)}}
					}
					props.Code = &parser.CodeProperties{Style: "fenced", Fence: parser.Some(w.fence), Width: parser.Some(w.width), Info: &info}
				}
				props.Literal = parser.Some(w.text)
			}
			if w.kind == parser.HTMLBlock {
				props.HTML = &parser.HTMLProperties{BlockForm: parser.Some(w.htmlForm)}
			}
			if w.list != nil {
				p := *w.list
				prefix, marker := w.markers[0].span, w.markers[1].span
				value := parser.Value{Semantic: parser.Some("")}
				for _, r := range []byteRange{{prefix.start, marker.start}, marker, {marker.end, prefix.end}} {
					if r.start == r.end {
						continue
					}
					span, err := source.Span(r.start, r.end)
					if err != nil {
						t.Fatal(err)
					}
					raw, _ := source.Text(span)
					value.Segments = append(value.Segments, parser.Segment{Source: span, Transformation: parser.Exact, Semantic: parser.Some(raw)})
					value.Semantic.Value += raw
				}
				p.Prefix = &value
				props.List = &p
			}
			if !reflect.DeepEqual(n.Properties(), props) {
				t.Fatalf("block %d properties: %+v", i, n.Properties())
			}
			markers := n.Markers()
			if len(markers) != len(w.markers) {
				t.Fatalf("block %d markers: %v, want %v", i, markers, w.markers)
			}
			for j, m := range markers {
				if m.Role != w.markers[j].role {
					t.Fatal("wrong marker role", m.Role)
				}
				checkRange(m.Source, w.markers[j].span)
			}
			content := n.Content()
			if len(content) != len(w.content) {
				t.Fatalf("block %d content count: %d want %d", i, len(content), len(w.content))
			}
			var text string
			for j, s := range content {
				checkRange(s.Source, w.content[j])
				raw, _ := source.Text(s.Source)
				if raw == "\x00" {
					if s.Transformation != parser.Normalized || s.Semantic != parser.Some("\ufffd") {
						t.Fatal("NUL provenance lost")
					}
				} else if s.Transformation != parser.Exact || s.Semantic != parser.Some(raw) {
					t.Fatal("exact provenance lost", s)
				}
				text += s.Semantic.Value
			}
			if text != w.text {
				t.Fatalf("block %d content %q, want %q", i, text, w.text)
			}
			if n.Kind() == parser.Paragraph || n.Kind() == parser.Heading {
				if w.inlines != nil {
					checkInlineNodes(t, source, n.Children(), w.inlines)
				} else {
					checkInlineText(t, r, n, w.text, w.content)
				}
			} else {
				checkNodes(ungroupedChildren(n), w.children)
			}
		}
	}
	checkNodes(nodes, want)
	outcome := conformance.Evaluate(r)
	if r.Status == parser.Complete {
		if outcome.Status != "rendered" {
			t.Fatal(outcome)
		}
	} else if outcome.Status != "pending" || outcome.HTML != "" {
		t.Fatal(outcome)
	}

	if len(original) > 0 {
		original[0] = '!'
	}
	copy := r.Source.Bytes()
	if len(copy) > 0 {
		copy[0] = '!'
	}
	if string(source.Bytes()) != input || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
		t.Fatal("source mutation or nondeterministic parse")
	}
	return r
}

func TestATXUpstreamTargets(t *testing.T) {
	// Includes the pre-existing blank-line target affected by heading dispatch.
	wants := map[int][]atxBlockWant{
		62: {
			atxHeadingWant(1, byteRange{0, 6}, byteRange{0, 1}, byteRange{2, 5}, byteRange{}, "foo"),
			atxHeadingWant(2, byteRange{6, 13}, byteRange{6, 8}, byteRange{9, 12}, byteRange{}, "foo"),
			atxHeadingWant(3, byteRange{13, 21}, byteRange{13, 16}, byteRange{17, 20}, byteRange{}, "foo"),
			atxHeadingWant(4, byteRange{21, 30}, byteRange{21, 25}, byteRange{26, 29}, byteRange{}, "foo"),
			atxHeadingWant(5, byteRange{30, 40}, byteRange{30, 35}, byteRange{36, 39}, byteRange{}, "foo"),
			atxHeadingWant(6, byteRange{40, 51}, byteRange{40, 46}, byteRange{47, 50}, byteRange{}, "foo"),
		},
		63:  {atxParagraphWant(byteRange{0, 12}, "####### foo", byteRange{0, 11})},
		64:  {atxParagraphWant(byteRange{0, 8}, "#5 bolt", byteRange{0, 7}), atxParagraphWant(byteRange{9, 18}, "#hashtag", byteRange{9, 17})},
		65:  {atxParagraphWant(byteRange{0, 8}, "\\## foo", byteRange{0, 7})},
		66:  {atxHeadingWant(1, byteRange{0, 20}, byteRange{0, 1}, byteRange{2, 19}, byteRange{}, "foo *bar* \\*baz\\*")},
		67:  {atxHeadingWant(1, byteRange{0, 44}, byteRange{0, 1}, byteRange{19, 22}, byteRange{}, "foo")},
		68:  {atxHeadingWant(3, byteRange{0, 9}, byteRange{1, 4}, byteRange{5, 8}, byteRange{}, "foo"), atxHeadingWant(2, byteRange{9, 18}, byteRange{11, 13}, byteRange{14, 17}, byteRange{}, "foo"), atxHeadingWant(1, byteRange{18, 27}, byteRange{21, 22}, byteRange{23, 26}, byteRange{}, "foo")},
		69:  {indentedWant(byteRange{0, 10}, "# foo\n", []byteRange{{0, 4}}, byteRange{4, 9}, byteRange{9, 10})},
		70:  {atxParagraphWant(byteRange{0, 14}, "foo\n# bar", byteRange{0, 4}, byteRange{8, 13})},
		71:  {atxHeadingWant(2, byteRange{0, 10}, byteRange{0, 2}, byteRange{3, 6}, byteRange{7, 9}, "foo"), atxHeadingWant(3, byteRange{10, 29}, byteRange{12, 15}, byteRange{18, 21}, byteRange{25, 28}, "bar")},
		72:  {atxHeadingWant(1, byteRange{0, 41}, byteRange{0, 1}, byteRange{2, 5}, byteRange{6, 40}, "foo"), atxHeadingWant(5, byteRange{41, 54}, byteRange{41, 46}, byteRange{47, 50}, byteRange{51, 53}, "foo")},
		73:  {atxHeadingWant(3, byteRange{0, 17}, byteRange{0, 3}, byteRange{4, 7}, byteRange{8, 11}, "foo")},
		74:  {atxHeadingWant(3, byteRange{0, 14}, byteRange{0, 3}, byteRange{4, 13}, byteRange{}, "foo ### b")},
		75:  {atxHeadingWant(1, byteRange{0, 7}, byteRange{0, 1}, byteRange{2, 6}, byteRange{}, "foo#")},
		76:  {atxHeadingWant(3, byteRange{0, 13}, byteRange{0, 3}, byteRange{4, 12}, byteRange{}, "foo \\###"), atxHeadingWant(2, byteRange{13, 25}, byteRange{13, 15}, byteRange{16, 24}, byteRange{}, "foo #\\##"), atxHeadingWant(1, byteRange{25, 34}, byteRange{25, 26}, byteRange{27, 33}, byteRange{}, "foo \\#")},
		77:  {atxThematicWant(byteRange{0, 5}, byteRange{0, 4}), atxHeadingWant(2, byteRange{5, 12}, byteRange{5, 7}, byteRange{8, 11}, byteRange{}, "foo"), atxThematicWant(byteRange{12, 17}, byteRange{12, 16})},
		78:  {atxParagraphWant(byteRange{0, 8}, "Foo bar", byteRange{0, 7}), atxHeadingWant(1, byteRange{8, 14}, byteRange{8, 9}, byteRange{10, 13}, byteRange{}, "baz"), atxParagraphWant(byteRange{14, 22}, "Bar foo", byteRange{14, 21})},
		79:  {atxHeadingWant(2, byteRange{0, 4}, byteRange{0, 2}, byteRange{}, byteRange{}, ""), atxHeadingWant(1, byteRange{4, 6}, byteRange{4, 5}, byteRange{}, byteRange{}, ""), atxHeadingWant(3, byteRange{6, 14}, byteRange{6, 9}, byteRange{}, byteRange{10, 13}, "")},
		227: {atxParagraphWant(byteRange{4, 8}, "aaa", byteRange{4, 7}), atxHeadingWant(1, byteRange{12, 18}, byteRange{12, 13}, byteRange{14, 17}, byteRange{}, "aaa")},
	}
	data, err := os.ReadFile("testdata/commonmark/0.31.2/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples []struct {
		ID       int    `json:"example"`
		Section  string `json:"section"`
		Markdown string `json:"markdown"`
		HTML     string `json:"html"`
	}
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, e := range examples {
		want, ok := wants[e.ID]
		if !ok {
			continue
		}
		section := "ATX headings"
		if e.ID == 227 {
			section = "Blank lines"
		}
		if seen[e.ID] || e.Section != section {
			t.Fatal("duplicate or wrong-section target", e.ID)
		}
		seen[e.ID] = true
		t.Run(strconv.Itoa(e.ID), func(t *testing.T) {
			r := checkATX(t, e.Markdown, e.ID == 79 || e.ID == 69, want)
			if (e.ID == 79 || e.ID == 69) && conformance.Evaluate(r).HTML != e.HTML {
				t.Fatal("actual supported HTML differs from upstream")
			}
		})
	}
	if len(seen) != 19 {
		t.Fatal("not all ATX and existing 227 targets exercised")
	}
}

func TestATXBoundariesAndProvenance(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r", ""} {
		t.Run("ending/"+strconv.Quote(ending), func(t *testing.T) {
			checkATX(t, "  ## é ##"+ending, false, []atxBlockWant{
				atxHeadingWant(2, byteRange{0, 10 + len(ending)}, byteRange{2, 4}, byteRange{5, 7}, byteRange{8, 10}, "é"),
			})
		})
	}
	for _, tt := range []struct {
		input    string
		complete bool
		want     []atxBlockWant
	}{
		{"#", true, []atxBlockWant{atxHeadingWant(1, byteRange{0, 1}, byteRange{0, 1}, byteRange{}, byteRange{}, "")}},
		{"   ### \t", true, []atxBlockWant{atxHeadingWant(3, byteRange{0, 8}, byteRange{3, 6}, byteRange{}, byteRange{}, "")}},
		{"#\tfoo\t##\t\r\n", false, []atxBlockWant{atxHeadingWant(1, byteRange{0, 11}, byteRange{0, 1}, byteRange{2, 5}, byteRange{6, 8}, "foo")}},
		{"  ## é\x00 ##\r\n", false, []atxBlockWant{{kind: parser.Heading, envelope: byteRange{0, 13}, level: 2, markers: []markerWant{{"opening", byteRange{2, 4}}, {"closing", byteRange{9, 11}}}, content: []byteRange{{5, 7}, {7, 8}}, text: "é\ufffd"}}},
		{"# \u00a0 #", false, []atxBlockWant{atxHeadingWant(1, byteRange{0, 6}, byteRange{0, 1}, byteRange{2, 4}, byteRange{5, 6}, "\u00a0")}},
		{"# foo #\u00a0", false, []atxBlockWant{atxHeadingWant(1, byteRange{0, 9}, byteRange{0, 1}, byteRange{2, 9}, byteRange{}, "foo #\u00a0")}},
		{"# foo\u00a0#", false, []atxBlockWant{atxHeadingWant(1, byteRange{0, 8}, byteRange{0, 1}, byteRange{2, 8}, byteRange{}, "foo\u00a0#")}},
		{"# foo# ##", false, []atxBlockWant{atxHeadingWant(1, byteRange{0, 9}, byteRange{0, 1}, byteRange{2, 6}, byteRange{7, 9}, "foo#")}},
		{"# \\##", false, []atxBlockWant{atxHeadingWant(1, byteRange{0, 5}, byteRange{0, 1}, byteRange{2, 5}, byteRange{}, "\\##")}},
		{"# \\\\ ##", false, []atxBlockWant{atxHeadingWant(1, byteRange{0, 7}, byteRange{0, 1}, byteRange{2, 4}, byteRange{5, 7}, "\\\\")}},
		{"a\n#no\nb", false, []atxBlockWant{atxParagraphWant(byteRange{0, 7}, "a\n#no\nb", byteRange{0, 2}, byteRange{2, 6}, byteRange{6, 7})}},
		{"a\n\n# h\n\nb\n", false, []atxBlockWant{atxParagraphWant(byteRange{0, 2}, "a", byteRange{0, 1}), atxHeadingWant(1, byteRange{3, 7}, byteRange{3, 4}, byteRange{5, 6}, byteRange{}, "h"), atxParagraphWant(byteRange{8, 10}, "b", byteRange{8, 9})}},
		{"***\n# h\n", false, []atxBlockWant{atxThematicWant(byteRange{0, 4}, byteRange{0, 3}), atxHeadingWant(1, byteRange{4, 8}, byteRange{4, 5}, byteRange{6, 7}, byteRange{}, "h")}},
		{"#\n \t\r\n***\r### ###", true, []atxBlockWant{atxHeadingWant(1, byteRange{0, 2}, byteRange{0, 1}, byteRange{}, byteRange{}, ""), atxThematicWant(byteRange{6, 10}, byteRange{6, 9}), atxHeadingWant(3, byteRange{10, 17}, byteRange{10, 13}, byteRange{}, byteRange{14, 17}, "")}},
		{"#\n> quote\n#\n", false, []atxBlockWant{atxHeadingWant(1, byteRange{0, 2}, byteRange{0, 1}, byteRange{}, byteRange{}, ""), quoteWant(byteRange{2, 10}, []byteRange{{2, 4}}, atxParagraphWant(byteRange{4, 10}, "quote", byteRange{4, 9})), atxHeadingWant(1, byteRange{10, 12}, byteRange{10, 11}, byteRange{}, byteRange{}, "")}},
		{"#\na\n---\n", false, []atxBlockWant{atxHeadingWant(1, byteRange{0, 2}, byteRange{0, 1}, byteRange{}, byteRange{}, ""), setextWant(2, byteRange{2, 8}, byteRange{4, 7}, "a", byteRange{2, 3})}},
		{"#\n    #\n", true, []atxBlockWant{atxHeadingWant(1, byteRange{0, 2}, byteRange{0, 1}, byteRange{}, byteRange{}, ""), indentedWant(byteRange{2, 8}, "#\n", []byteRange{{2, 6}}, byteRange{6, 7}, byteRange{7, 8})}},
		{"# h\n#\n", false, []atxBlockWant{atxHeadingWant(1, byteRange{0, 4}, byteRange{0, 1}, byteRange{2, 3}, byteRange{}, "h"), atxHeadingWant(1, byteRange{4, 6}, byteRange{4, 5}, byteRange{}, byteRange{}, "")}},
		{"#\nx\n", false, []atxBlockWant{atxHeadingWant(1, byteRange{0, 2}, byteRange{0, 1}, byteRange{}, byteRange{}, ""), atxParagraphWant(byteRange{2, 4}, "x", byteRange{2, 3})}},
		{"a\n    # h", false, []atxBlockWant{atxParagraphWant(byteRange{0, 9}, "a\n# h", byteRange{0, 2}, byteRange{6, 9})}},
	} {
		t.Run(tt.input, func(t *testing.T) { checkATX(t, tt.input, tt.complete, tt.want) })
	}
	// These inputs cannot be ATX headings. Hash-prefixed failed candidates take
	// the paragraph path; indented_test.go checks four-column starts as literal code.
	for _, input := range []string{"#######", "####### x", "#x", "#\u00a0x", "#\vx", "#\fx", "\\# x"} {
		t.Run("negative/"+input, func(t *testing.T) {
			r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
			if r.Failure != nil || string(r.Source.Bytes()) != input {
				t.Fatal("negative input changed or completed", r)
			}
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() == parser.Heading {
					t.Fatal("negative opener became heading")
				}
				return true
			})
		})
	}
}

func TestATXAccessorsKeepFrozenMetadata(t *testing.T) {
	source := parser.NewSource([]byte("## é ##\n"))
	r := parser.Parse(context.Background(), source)
	n := r.Document.Root().Children()[0]
	markers := n.Markers()
	markers[0].Role = "changed"
	markers[1].Source = parser.Span{}
	content := n.Content()
	content[0].Source = parser.Span{}
	content[0].Semantic = parser.Some("changed")
	props := n.Properties()
	props.Heading.Level = 6
	*n = parser.Node{}
	if !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
		t.Fatal("returned metadata mutation changed frozen heading")
	}
}
