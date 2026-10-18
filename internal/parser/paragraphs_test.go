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
	if r.Status != parser.Incomplete || r.AST != parser.Partial || r.Failure != nil || r.Interruption != "" || r.Phases != (parser.PhaseProgress{Blocks: parser.Pending, Inlines: parser.NotStarted}) || len(r.Uncertainty) != 1 || r.Uncertainty[0].Region.Start() != 0 || r.Uncertainty[0].Region.End() != source.Len() || r.Uncertainty[0].Reason != parser.SyntaxUnimplemented {
		t.Fatalf("staged syntax claimed completion: %+v", r)
	}
	refs := r.Document.References()
	if refs.Availability != parser.Unavailable || refs.LookupAvailability != parser.Unavailable || len(refs.Definitions) != 0 || len(refs.Lookup) != 0 {
		t.Fatal("partial block work claimed known-empty references")
	}
	nodes := r.Document.Root().Children()
	if len(nodes) != len(want) {
		t.Fatalf("got %d paragraph nodes, want %d", len(nodes), len(want))
	}
	for i, node := range nodes {
		w := want[i]
		if node.Kind() != parser.Paragraph || node.Span().Start() != w.envelope.start || node.Span().End() != w.envelope.end || len(node.Children()) != 0 || node.Properties().Literal.Present {
			t.Fatalf("paragraph %d has wrong envelope or invented inlines: %+v", i, node)
		}
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
		225: nil, // #10: unknown indented code; its suffix remains source-only.
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
			if e.ID == 227 {
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

func TestPendingBlockBoundaries(t *testing.T) {
	for _, input := range []string{"    code\n", "\tcode\n", "   \tcode\n", "```\na\n```\n", "> quote\n", "- list\n", "1. list\n", "[foo]: /url\n", "<div>\na\n", "aaa\n---\n", "aaa\n===\n"} {
		t.Run(input, func(t *testing.T) { checkParagraphs(t, parser.NewSource([]byte(input)), nil) })
	}
	// Once a block boundary is unknown, its whole suffix is left uninterpreted.
	checkParagraphs(t, parser.NewSource([]byte("a\n\n> quote\n\nb\n")), []paragraphWant{{byteRange{0, 2}, []byteRange{{0, 1}}, "a"}})
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
