package parser_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

type byteRange struct{ start, end int }
type paragraphWant struct {
	envelope byteRange
	content  []byteRange
	text     string
}

func checkParagraphs(t *testing.T, source parser.Source, want []paragraphWant) parser.ParseResult {
	t.Helper()
	r := parser.Parse(context.Background(), source)
	checkStagedTextOutcome(t, r, false)
	refs := r.Document.References()
	if refs.Availability != parser.Available || refs.LookupAvailability != parser.Available || len(refs.Definitions) != 0 || len(refs.Lookup) != 0 {
		t.Fatal("finished block scan lost available empty references")
	}
	nodes := r.Document.Root().Children()
	if len(nodes) != len(want) {
		t.Fatalf("got %d paragraph nodes, want %d", len(nodes), len(want))
	}
	for i, node := range nodes {
		w := want[i] //nolint:gosec // G602: equal node/fixture lengths are checked above.
		if node.Kind() != parser.Paragraph || node.Span().Start() != w.envelope.start || node.Span().End() != w.envelope.end || node.Properties().Literal.Present {
			t.Fatalf("paragraph %d has wrong envelope or invented inlines: %+v", i, node)
		}
		checkInlineText(t, r, node, w.text, w.content)
		if _, err := source.Text(node.Span()); err != nil {
			t.Fatal("paragraph has foreign snapshot")
		}
		var ranges []byteRange
		var text string
		for _, segment := range node.Content() {
			ranges = append(ranges, byteRange{segment.Source.Start(), segment.Source.End()})
			if _, err := source.Text(segment.Source); err != nil {
				t.Fatal("segment has foreign snapshot")
			}
			if !segment.Semantic.Present {
				t.Fatal("source contribution lacks semantic text")
			}
			text += segment.Semantic.Value
		}
		if !reflect.DeepEqual(ranges, w.content) || text != w.text {
			t.Fatalf("paragraph %d content = %v %q, want %v %q", i, ranges, text, w.content, w.text)
		}
	}
	if !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
		t.Fatal("same snapshot produced nondeterministic blocks")
	}
	return r
}

func TestParagraphUpstreamTargets(t *testing.T) {
	// Expected original-byte ranges are independent of parser/scanner output.
	wants := map[int][]paragraphWant{
		219: {{byteRange{0, 4}, []byteRange{{0, 3}}, "aaa"}, {byteRange{5, 9}, []byteRange{{5, 8}}, "bbb"}},
		220: {{byteRange{0, 8}, []byteRange{{0, 4}, {4, 7}}, "aaa\nbbb"}, {byteRange{9, 17}, []byteRange{{9, 13}, {13, 16}}, "ccc\nddd"}},
		221: {{byteRange{0, 4}, []byteRange{{0, 3}}, "aaa"}, {byteRange{6, 10}, []byteRange{{6, 9}}, "bbb"}},
		222: {{byteRange{0, 11}, []byteRange{{2, 6}, {7, 10}}, "aaa\nbbb"}},
		223: {{byteRange{0, 64}, []byteRange{{0, 4}, {17, 21}, {60, 63}}, "aaa\nbbb\nccc"}},
		224: {{byteRange{0, 11}, []byteRange{{3, 7}, {7, 10}}, "aaa\nbbb"}},
		225: nil, // Code and following paragraph are asserted together below.
		226: {{byteRange{0, 18}, []byteRange{{0, 9}, {9, 12}}, "aaa     \nbbb"}},
		227: {{byteRange{4, 8}, []byteRange{{4, 7}}, "aaa"}}, // Heading is asserted separately below.
	}
	data, err := os.ReadFile("testdata/commonmark/0.31.2/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples []struct {
		ID       int    `json:"example"`
		Section  string `json:"section"`
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	seen := make(map[int]bool)
	for _, e := range examples {
		want, target := wants[e.ID]
		if !target {
			continue
		}
		section := "Paragraphs"
		if e.ID == 227 {
			section = "Blank lines"
		}
		if seen[e.ID] || e.Section != section {
			t.Fatalf("duplicate or wrong-section target %d", e.ID)
		}
		seen[e.ID] = true
		t.Run(e.Section+"/"+strconv.Itoa(e.ID), func(t *testing.T) {
			s := parser.NewSource([]byte(e.Markdown))
			if e.ID == 225 {
				checkATX(t, e.Markdown, false, []atxBlockWant{indentedWant(byteRange{0, 8}, "aaa\n", []byteRange{{0, 4}}, byteRange{4, 7}, byteRange{7, 8}), atxParagraphWant(byteRange{8, 12}, "bbb", byteRange{8, 11})})
			} else if e.ID == 227 {
				checkATX(t, e.Markdown, false, []atxBlockWant{
					atxParagraphWant(byteRange{4, 8}, "aaa", byteRange{4, 7}),
					atxHeadingWant(1, byteRange{12, 18}, byteRange{12, 13}, byteRange{14, 17}, byteRange{}, "aaa"),
				})
			} else {
				checkParagraphs(t, s, want)
			}
			if string(s.Bytes()) != e.Markdown {
				t.Fatal("parser changed original upstream bytes")
			}
		})
	}
	if len(seen) != 9 {
		t.Fatal("not every target 219–227 was exercised")
	}
}

func TestParagraphClosureAndProvenance(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  []paragraphWant
	}{
		{"a", []paragraphWant{{byteRange{0, 1}, []byteRange{{0, 1}}, "a"}}},
		{"a\n\n\n", []paragraphWant{{byteRange{0, 2}, []byteRange{{0, 1}}, "a"}}},
		{" \t\n\na\n\n b \t", []paragraphWant{{byteRange{4, 6}, []byteRange{{4, 5}}, "a"}, {byteRange{7, 11}, []byteRange{{8, 9}}, "b"}}},
		{"a\r\nb\r", []paragraphWant{{byteRange{0, 5}, []byteRange{{0, 3}, {3, 4}}, "a\r\nb"}}},
		{"a\rb", []paragraphWant{{byteRange{0, 3}, []byteRange{{0, 2}, {2, 3}}, "a\rb"}}},
		{"a\n \t b\n", []paragraphWant{{byteRange{0, 7}, []byteRange{{0, 2}, {5, 6}}, "a\nb"}}},
		{"a\tb\n", []paragraphWant{{byteRange{0, 4}, []byteRange{{0, 3}}, "a\tb"}}},
		{"\u00a0\n", []paragraphWant{{byteRange{0, 3}, []byteRange{{0, 2}}, "\u00a0"}}},
		{"\u2028\n", []paragraphWant{{byteRange{0, 4}, []byteRange{{0, 3}}, "\u2028"}}},
		{"\ufffd\x00", []paragraphWant{{byteRange{0, 4}, []byteRange{{0, 3}, {3, 4}}, "\ufffd\ufffd"}}},
		{"  é\x00 \t\r\n\tZ \t\r", []paragraphWant{{byteRange{0, 14}, []byteRange{{2, 4}, {4, 5}, {5, 9}, {10, 11}}, "é\ufffd \t\r\nZ"}}},
	} {
		t.Run(tt.input, func(t *testing.T) {
			input := []byte(tt.input)
			s := parser.NewSource(input)
			r := checkParagraphs(t, s, tt.want)
			input[0] = '!'
			if string(s.Bytes()) != tt.input || string(r.Source.Bytes()) != tt.input {
				t.Fatal("parser failed snapshot ownership")
			}
			for _, node := range r.Document.Root().Children() {
				for _, segment := range node.Content() {
					raw, _ := s.Text(segment.Source)
					if raw == "\x00" {
						if segment.Transformation != parser.Normalized || segment.Semantic != parser.Some("\ufffd") {
							t.Fatal("NUL lost explicit normalization provenance")
						}
					} else if segment.Transformation != parser.Exact || segment.Semantic != parser.Some(raw) {
						t.Fatal("unchanged content lost exact provenance")
					}
					if _, err := parser.NewSource([]byte(tt.input)).Text(segment.Source); !errors.Is(err, parser.ErrRange) {
						t.Fatal("independent snapshot accepted paragraph span")
					}
				}
			}
		})
	}
}

func TestEstablishedBlockBoundaries(t *testing.T) {
	checkATX(t, "- list\n", false, []atxBlockWant{bulletWant(byteRange{0, 7}, byteRange{0, 2}, byteRange{0, 1}, '-', 1, 2, "list", byteRange{2, 6})})
	checkATX(t, "1. list\n", false, []atxBlockWant{orderedWant(byteRange{0, 8}, byteRange{0, 3}, byteRange{0, 2}, 1, '.', 1, 3, "list", byteRange{3, 7})})
	// The blank belongs to the item; the following dedented paragraph is outside.
	checkATX(t, "a\n\n- list\n\nb\n", false, []atxBlockWant{atxParagraphWant(byteRange{0, 2}, "a", byteRange{0, 1}), continuedItemWant(bulletWant(byteRange{3, 10}, byteRange{3, 5}, byteRange{3, 4}, '-', 1, 2, "list", byteRange{5, 9}), 11, []markerWant{{"item-blank", byteRange{10, 11}}}), atxParagraphWant(byteRange{11, 13}, "b", byteRange{11, 12})})
}

func TestBlankDocumentsComplete(t *testing.T) {
	for _, input := range []string{"", " ", "\t\n\r\n \r\t", "\r\n\r\n"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		if r.Status != parser.Complete || r.AST != parser.Available || r.Failure != nil || r.Interruption != "" || len(r.Uncertainty) != 0 || len(r.Document.Root().Children()) != 0 || r.Phases != (parser.PhaseProgress{Blocks: parser.PhaseComplete, Inlines: parser.PhaseComplete}) || r.Document.References().LookupAvailability != parser.Available || string(r.Source.Bytes()) != input {
			t.Fatalf("blank document not established complete: %+v", r)
		}
	}
}

func TestParsedParagraphAccessorsRetainFrozenSnapshot(t *testing.T) {
	source := parser.NewSource([]byte("first\n\nsecond\n"))
	r := parser.Parse(context.Background(), source)
	nodes := r.Document.Root().Children()
	segments := nodes[0].Content()
	segments[0].Semantic = parser.Some("changed")
	*nodes[0] = parser.Node{}
	nodes[1] = nil
	bytes := r.Source.Bytes()
	bytes[0] = '!'
	if !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
		t.Fatal("mutating returned bytes, segments or node handles changed frozen parse")
	}
}
