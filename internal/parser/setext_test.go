package parser_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

func setextWant(level int, envelope, underline byteRange, text string, content ...byteRange) atxBlockWant {
	return atxBlockWant{kind: parser.Heading, level: level, style: "setext", envelope: envelope,
		markers: []markerWant{{"underline", underline}}, content: content, text: text}
}

func TestSetextUpstreamTargets(t *testing.T) {
	// Literal byte ranges and raw text are independent of parser/scanner output.
	// Nil means explicitly unavailable block classification, not recognition.
	wants := map[int][]atxBlockWant{
		59: {setextWant(2, byteRange{0, 8}, byteRange{4, 7}, "Foo", byteRange{0, 3}), atxParagraphWant(byteRange{8, 12}, "bar", byteRange{8, 11})},
		80: {setextWant(1, byteRange{0, 20}, byteRange{10, 19}, "Foo *bar*", byteRange{0, 9}), setextWant(2, byteRange{21, 41}, byteRange{31, 40}, "Foo *bar*", byteRange{21, 30})},
		81: {setextWant(1, byteRange{0, 19}, byteRange{14, 18}, "Foo *bar\nbaz*", byteRange{0, 9}, byteRange{9, 13})},
		82: {setextWant(1, byteRange{0, 22}, byteRange{17, 21}, "Foo *bar\nbaz*", byteRange{2, 11}, byteRange{11, 15})},
		83: {setextWant(2, byteRange{0, 30}, byteRange{4, 29}, "Foo", byteRange{0, 3}), setextWant(1, byteRange{31, 37}, byteRange{35, 36}, "Foo", byteRange{31, 34})},
		84: {setextWant(2, byteRange{0, 11}, byteRange{7, 10}, "Foo", byteRange{3, 6}), setextWant(2, byteRange{12, 24}, byteRange{18, 23}, "Foo", byteRange{14, 17}), setextWant(1, byteRange{25, 37}, byteRange{33, 36}, "Foo", byteRange{27, 30})},
		85: {indentedWant(byteRange{0, 25}, "Foo\n---\n\nFoo\n", []byteRange{{0, 4}, {8, 12}, {17, 21}}, byteRange{4, 7}, byteRange{7, 8}, byteRange{12, 15}, byteRange{15, 16}, byteRange{16, 17}, byteRange{21, 24}, byteRange{24, 25}), atxThematicWant(byteRange{25, 29}, byteRange{25, 28})},
		86: {setextWant(2, byteRange{0, 18}, byteRange{7, 11}, "Foo", byteRange{0, 3})},
		87: {atxParagraphWant(byteRange{0, 12}, "Foo\n---", byteRange{0, 4}, byteRange{8, 11})},
		88: {atxParagraphWant(byteRange{0, 8}, "Foo\n= =", byteRange{0, 4}, byteRange{4, 7}), atxParagraphWant(byteRange{9, 13}, "Foo", byteRange{9, 12}), {kind: parser.ThematicBreak, envelope: byteRange{13, 19}, markers: []markerWant{{"thematic-marker", byteRange{13, 16}}, {"thematic-marker", byteRange{17, 18}}}}},
		89: {setextWant(2, byteRange{0, 12}, byteRange{6, 11}, "Foo", byteRange{0, 3})},
		90: {setextWant(2, byteRange{0, 10}, byteRange{5, 9}, "Foo\\", byteRange{0, 4})},
		// Invalid fence/tag starts permit ordinary paragraph/Setext progress.
		91:  {setextWant(2, byteRange{0, 10}, byteRange{5, 9}, "`Foo", byteRange{0, 4}), atxParagraphWant(byteRange{10, 12}, "`", byteRange{10, 11}), setextWant(2, byteRange{13, 33}, byteRange{29, 32}, "<a title=\"a lot", byteRange{13, 28}), atxParagraphWant(byteRange{33, 46}, "of dashes\"/>", byteRange{33, 45})},
		92:  {quoteWant(byteRange{0, 6}, []byteRange{{0, 2}}, atxParagraphWant(byteRange{2, 6}, "Foo", byteRange{2, 5})), atxThematicWant(byteRange{6, 10}, byteRange{6, 9})},
		93:  {quoteWant(byteRange{0, 14}, []byteRange{{0, 2}}, atxParagraphWant(byteRange{2, 14}, "foo\nbar\n===", byteRange{2, 6}, byteRange{6, 10}, byteRange{10, 13}))},
		94:  {bulletWant(byteRange{0, 6}, byteRange{0, 2}, byteRange{0, 1}, '-', 1, 2, "Foo", byteRange{2, 5}), atxThematicWant(byteRange{6, 10}, byteRange{6, 9})},
		95:  {setextWant(2, byteRange{0, 12}, byteRange{8, 11}, "Foo\nBar", byteRange{0, 4}, byteRange{4, 7})},
		96:  {atxThematicWant(byteRange{0, 4}, byteRange{0, 3}), setextWant(2, byteRange{4, 12}, byteRange{8, 11}, "Foo", byteRange{4, 7}), setextWant(2, byteRange{12, 20}, byteRange{16, 19}, "Bar", byteRange{12, 15}), atxParagraphWant(byteRange{20, 24}, "Baz", byteRange{20, 23})},
		97:  {atxParagraphWant(byteRange{1, 6}, "====", byteRange{1, 5})},
		98:  {atxThematicWant(byteRange{0, 4}, byteRange{0, 3}), atxThematicWant(byteRange{4, 8}, byteRange{4, 7})},
		99:  {bulletWant(byteRange{0, 6}, byteRange{0, 2}, byteRange{0, 1}, '-', 1, 2, "foo", byteRange{2, 5}), atxThematicWant(byteRange{6, 12}, byteRange{6, 11})},
		100: {indentedWant(byteRange{0, 8}, "foo\n", []byteRange{{0, 4}}, byteRange{4, 7}, byteRange{7, 8}), atxThematicWant(byteRange{8, 12}, byteRange{8, 11})},
		101: {quoteWant(byteRange{0, 6}, []byteRange{{0, 2}}, atxParagraphWant(byteRange{2, 6}, "foo", byteRange{2, 5})), atxThematicWant(byteRange{6, 12}, byteRange{6, 11})},
		102: {setextWant(2, byteRange{0, 14}, byteRange{7, 13}, "\\> foo", byteRange{0, 6})},
		103: {atxParagraphWant(byteRange{0, 4}, "Foo", byteRange{0, 3}), setextWant(2, byteRange{5, 13}, byteRange{9, 12}, "bar", byteRange{5, 8}), atxParagraphWant(byteRange{13, 17}, "baz", byteRange{13, 16})},
		104: {atxParagraphWant(byteRange{0, 8}, "Foo\nbar", byteRange{0, 4}, byteRange{4, 7}), atxThematicWant(byteRange{9, 13}, byteRange{9, 12}), atxParagraphWant(byteRange{14, 18}, "baz", byteRange{14, 17})},
		105: {atxParagraphWant(byteRange{0, 8}, "Foo\nbar", byteRange{0, 4}, byteRange{4, 7}), {kind: parser.ThematicBreak, envelope: byteRange{8, 14}, markers: []markerWant{{"thematic-marker", byteRange{8, 9}}, {"thematic-marker", byteRange{10, 11}}, {"thematic-marker", byteRange{12, 13}}}}, atxParagraphWant(byteRange{14, 18}, "baz", byteRange{14, 17})},
		106: {atxParagraphWant(byteRange{0, 17}, "Foo\nbar\n\\---\nbaz", byteRange{0, 4}, byteRange{4, 8}, byteRange{8, 13}, byteRange{13, 16})},
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
		section := "Setext headings"
		if e.ID == 59 {
			section = "Thematic breaks"
		}
		if seen[e.ID] || e.Section != section {
			t.Fatal("duplicate or wrong-section target", e.ID)
		}
		seen[e.ID] = true
		t.Run(strconv.Itoa(e.ID), func(t *testing.T) {
			r := checkATX(t, e.Markdown, e.ID == 98 || e.ID == 85 || e.ID == 100, want)
			if (e.ID == 98 || e.ID == 85 || e.ID == 100) && conformance.Evaluate(r).HTML != e.HTML {
				t.Fatal("actual supported thematic HTML changed")
			}
		})
	}
	if len(seen) != 28 {
		t.Fatal("not all Setext and existing thematic 59 targets exercised")
	}
}

func TestSetextBoundariesAndProvenance(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, terminal := range []string{ending, ""} {
			input := "  é\x00 \t" + ending + "\tZ \\" + ending + "   -\t " + terminal
			// First line is seven bytes before its ending; the second is four.
			second := 7 + len(ending)
			underline := second + 4 + len(ending)
			checkATX(t, input, false, []atxBlockWant{setextWant(2, byteRange{0, underline + 6 + len(terminal)}, byteRange{underline + 3, underline + 4}, "é\ufffd \t"+ending+"Z \\", byteRange{2, 4}, byteRange{4, 5}, byteRange{5, second}, byteRange{second + 1, second + 4})})
		}
	}
	for _, marker := range []string{"=", "-", "===", "---"} {
		level := 1
		if marker[0] == '-' {
			level = 2
		}
		for _, ending := range []string{"", "\n", "\r\n", "\r"} {
			checkATX(t, "a\n"+marker+ending, false, []atxBlockWant{setextWant(level, byteRange{0, 2 + len(marker) + len(ending)}, byteRange{2, 2 + len(marker)}, "a", byteRange{0, 1})})
		}
	}
	for _, input := range []string{"= =", "--x", "=-", "-="} {
		// Failed non-list/non-thematic candidates continue ordinary text.
		checkATX(t, "a\n"+input, false, []atxBlockWant{atxParagraphWant(byteRange{0, 2 + len(input)}, "a\n"+input, byteRange{0, 2}, byteRange{2, 2 + len(input)})})
	}
	for _, input := range []string{"a\n=\t=", "a\n===x", "a\n---\x00", "a\n---\u00a0", "a\n---\v", "a\n\t---", "a\n   \t=", "a\n    ="} {
		// All are ordinary paragraph continuations, never valid underlines.
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		if r.Status != parser.Incomplete || r.Failure != nil || len(r.Document.Root().Children()) != 1 || r.Document.Root().Children()[0].Kind() != parser.Paragraph || string(r.Source.Bytes()) != input {
			t.Fatal("malformed/deep underline did not remain a paragraph", input, r)
		}
	}
	checkATX(t, "a\n--\t-\nb", false, []atxBlockWant{atxParagraphWant(byteRange{0, 2}, "a", byteRange{0, 1}), {kind: parser.ThematicBreak, envelope: byteRange{2, 7}, markers: []markerWant{{"thematic-marker", byteRange{2, 4}}, {"thematic-marker", byteRange{5, 6}}}}, atxParagraphWant(byteRange{7, 8}, "b", byteRange{7, 8})})
	checkATX(t, "-", false, []atxBlockWant{bulletWant(byteRange{0, 1}, byteRange{0, 1}, byteRange{0, 1}, '-', 1, 2, "")})
	checkATX(t, "- ", false, []atxBlockWant{bulletWant(byteRange{0, 2}, byteRange{0, 2}, byteRange{0, 1}, '-', 1, 2, "")})
	checkATX(t, "a\n- item\n---", false, []atxBlockWant{atxParagraphWant(byteRange{0, 2}, "a", byteRange{0, 1}), bulletWant(byteRange{2, 9}, byteRange{2, 4}, byteRange{2, 3}, '-', 1, 2, "item", byteRange{4, 8}), atxThematicWant(byteRange{9, 12}, byteRange{9, 12})})
	checkATX(t, "a\n```\n---\n", false, []atxBlockWant{
		atxParagraphWant(byteRange{0, 2}, "a", byteRange{0, 1}),
		fencedWant(byteRange{2, 10}, byteRange{2, 5}, byteRange{}, '`', "---\n", "", byteRange{}, nil, byteRange{6, 9}, byteRange{9, 10}),
	})
	checkATX(t, "a\n-\n#\n***\n", false, []atxBlockWant{setextWant(2, byteRange{0, 4}, byteRange{2, 3}, "a", byteRange{0, 1}), atxHeadingWant(1, byteRange{4, 6}, byteRange{4, 5}, byteRange{}, byteRange{}, ""), atxThematicWant(byteRange{6, 10}, byteRange{6, 9})})
	checkATX(t, "a\n\n===\n", false, []atxBlockWant{atxParagraphWant(byteRange{0, 2}, "a", byteRange{0, 1}), atxParagraphWant(byteRange{3, 7}, "===", byteRange{3, 6})})
	checkATX(t, "\u00a0\n=\n", false, []atxBlockWant{setextWant(1, byteRange{0, 5}, byteRange{3, 4}, "\u00a0", byteRange{0, 2})})
	checkATX(t, "a\n=\n> quote\n", false, []atxBlockWant{setextWant(1, byteRange{0, 4}, byteRange{2, 3}, "a", byteRange{0, 1}), quoteWant(byteRange{4, 12}, []byteRange{{4, 6}}, atxParagraphWant(byteRange{6, 12}, "quote", byteRange{6, 11}))})
	checkATX(t, "<a title=\"a lot\n---\nof dashes\"/>\n", false, []atxBlockWant{
		setextWant(2, byteRange{0, 20}, byteRange{16, 19}, "<a title=\"a lot", byteRange{0, 15}),
		atxParagraphWant(byteRange{20, 33}, "of dashes\"/>", byteRange{20, 32}),
	})
}

func TestSetextAccessorsKeepFrozenMetadata(t *testing.T) {
	source := parser.NewSource([]byte("é\x00\n=\n"))
	r := parser.Parse(context.Background(), source)
	n := r.Document.Root().Children()[0]
	n.Markers()[0].Source = parser.Span{}
	n.Content()[0].Semantic = parser.Some("changed")
	n.Properties().Heading.Style = "atx"
	*n = parser.Node{}
	if !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
		t.Fatal("mutating returned metadata changed frozen heading")
	}
}
