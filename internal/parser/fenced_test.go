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

// All ranges and payloads below are independent original-byte expectations,
// not obtained by rendering, classifying or rescanning parser output.
func fencedWant(envelope, opening, closing byteRange, character byte, literal, info string, infoSpan byteRange, prefixes []byteRange, content ...byteRange) atxBlockWant {
	w := atxBlockWant{kind: parser.CodeBlock, style: "fenced", envelope: envelope,
		fence: character, width: opening.end - opening.start, text: literal, info: info, infoSpan: infoSpan, content: content,
		markers: []markerWant{{"opening", opening}}}
	for _, prefix := range prefixes {
		w.markers = append(w.markers, markerWant{"indent-prefix", prefix})
	}
	if closing != (byteRange{}) {
		w.markers = append(w.markers, markerWant{"closing", closing})
	}
	return w
}

func TestFencedUpstreamTargets(t *testing.T) {
	wants := map[int][]atxBlockWant{
		19:  {fencedWant(byteRange{0, 13}, byteRange{0, 3}, byteRange{9, 12}, '~', "\\[\\]\n", "", byteRange{}, nil, byteRange{4, 8}, byteRange{8, 9})},
		119: {fencedWant(byteRange{0, 13}, byteRange{0, 3}, byteRange{9, 12}, '`', "<\n >\n", "", byteRange{}, nil, byteRange{4, 5}, byteRange{5, 6}, byteRange{6, 8}, byteRange{8, 9})},
		120: {fencedWant(byteRange{0, 13}, byteRange{0, 3}, byteRange{9, 12}, '~', "<\n >\n", "", byteRange{}, nil, byteRange{4, 5}, byteRange{5, 6}, byteRange{6, 8}, byteRange{8, 9})},
		121: {atxParagraphWant(byteRange{0, 10}, "``\nfoo\n``", byteRange{0, 3}, byteRange{3, 7}, byteRange{7, 9})},
		122: {fencedWant(byteRange{0, 16}, byteRange{0, 3}, byteRange{12, 15}, '`', "aaa\n~~~\n", "", byteRange{}, nil, byteRange{4, 7}, byteRange{7, 8}, byteRange{8, 11}, byteRange{11, 12})},
		123: {fencedWant(byteRange{0, 16}, byteRange{0, 3}, byteRange{12, 15}, '~', "aaa\n```\n", "", byteRange{}, nil, byteRange{4, 7}, byteRange{7, 8}, byteRange{8, 11}, byteRange{11, 12})},
		124: {fencedWant(byteRange{0, 20}, byteRange{0, 4}, byteRange{13, 19}, '`', "aaa\n```\n", "", byteRange{}, nil, byteRange{5, 8}, byteRange{8, 9}, byteRange{9, 12}, byteRange{12, 13})},
		125: {fencedWant(byteRange{0, 18}, byteRange{0, 4}, byteRange{13, 17}, '~', "aaa\n~~~\n", "", byteRange{}, nil, byteRange{5, 8}, byteRange{8, 9}, byteRange{9, 12}, byteRange{12, 13})},
		126: {fencedWant(byteRange{0, 4}, byteRange{0, 3}, byteRange{}, '`', "", "", byteRange{}, nil)},
		127: {fencedWant(byteRange{0, 15}, byteRange{0, 5}, byteRange{}, '`', "\n```\naaa\n", "", byteRange{}, nil, byteRange{6, 7}, byteRange{7, 10}, byteRange{10, 11}, byteRange{11, 14}, byteRange{14, 15})},
		128: {quoteWant(byteRange{0, 12}, []byteRange{{0, 2}, {6, 8}}, fencedWant(byteRange{2, 12}, byteRange{2, 5}, byteRange{}, '`', "aaa\n", "", byteRange{}, nil, byteRange{8, 11}, byteRange{11, 12})), atxParagraphWant(byteRange{13, 17}, "bbb", byteRange{13, 16})},
		129: {fencedWant(byteRange{0, 12}, byteRange{0, 3}, byteRange{8, 11}, '`', "\n  \n", "", byteRange{}, nil, byteRange{4, 5}, byteRange{5, 7}, byteRange{7, 8})},
		130: {fencedWant(byteRange{0, 8}, byteRange{0, 3}, byteRange{4, 7}, '`', "", "", byteRange{}, nil)},
		131: {fencedWant(byteRange{0, 18}, byteRange{1, 4}, byteRange{14, 17}, '`', "aaa\naaa\n", "", byteRange{}, []byteRange{{5, 6}}, byteRange{6, 9}, byteRange{9, 10}, byteRange{10, 13}, byteRange{13, 14})},
		132: {fencedWant(byteRange{0, 26}, byteRange{2, 5}, byteRange{22, 25}, '`', "aaa\naaa\naaa\n", "", byteRange{}, []byteRange{{10, 12}}, byteRange{6, 9}, byteRange{9, 10}, byteRange{12, 15}, byteRange{15, 16}, byteRange{16, 19}, byteRange{19, 20})},
		133: {fencedWant(byteRange{0, 35}, byteRange{3, 6}, byteRange{31, 34}, '`', "aaa\n aaa\naaa\n", "", byteRange{}, []byteRange{{7, 10}, {14, 17}, {22, 24}}, byteRange{10, 13}, byteRange{13, 14}, byteRange{17, 21}, byteRange{21, 22}, byteRange{24, 27}, byteRange{27, 28})},
		134: {indentedWant(byteRange{0, 24}, "```\naaa\n```\n", []byteRange{{0, 4}, {8, 12}, {16, 20}}, byteRange{4, 7}, byteRange{7, 8}, byteRange{12, 15}, byteRange{15, 16}, byteRange{20, 23}, byteRange{23, 24})},
		135: {fencedWant(byteRange{0, 14}, byteRange{0, 3}, byteRange{10, 13}, '`', "aaa\n", "", byteRange{}, nil, byteRange{4, 7}, byteRange{7, 8})},
		136: {fencedWant(byteRange{0, 17}, byteRange{3, 6}, byteRange{13, 16}, '`', "aaa\n", "", byteRange{}, nil, byteRange{7, 10}, byteRange{10, 11})},
		137: {fencedWant(byteRange{0, 16}, byteRange{0, 3}, byteRange{}, '`', "aaa\n    ```\n", "", byteRange{}, nil, byteRange{4, 7}, byteRange{7, 8}, byteRange{8, 15}, byteRange{15, 16})},
		138: {atxParagraphWant(byteRange{0, 12}, "``` ```\naaa", byteRange{0, 8}, byteRange{8, 11})},
		139: {fencedWant(byteRange{0, 18}, byteRange{0, 6}, byteRange{}, '~', "aaa\n~~~ ~~\n", "", byteRange{}, nil, byteRange{7, 10}, byteRange{10, 11}, byteRange{11, 17}, byteRange{17, 18})},
		140: {atxParagraphWant(byteRange{0, 4}, "foo", byteRange{0, 3}), fencedWant(byteRange{4, 16}, byteRange{4, 7}, byteRange{12, 15}, '`', "bar\n", "", byteRange{}, nil, byteRange{8, 11}, byteRange{11, 12}), atxParagraphWant(byteRange{16, 20}, "baz", byteRange{16, 19})},
		141: {setextWant(2, byteRange{0, 8}, byteRange{4, 7}, "foo", byteRange{0, 3}), fencedWant(byteRange{8, 20}, byteRange{8, 11}, byteRange{16, 19}, '~', "bar\n", "", byteRange{}, nil, byteRange{12, 15}, byteRange{15, 16}), atxHeadingWant(1, byteRange{20, 26}, byteRange{20, 21}, byteRange{22, 25}, byteRange{}, "baz")},
		142: {fencedWant(byteRange{0, 38}, byteRange{0, 3}, byteRange{34, 37}, '`', "def foo(x)\n  return 3\nend\n", "ruby", byteRange{3, 7}, nil, byteRange{8, 18}, byteRange{18, 19}, byteRange{19, 29}, byteRange{29, 30}, byteRange{30, 33}, byteRange{33, 34})},
		143: {fencedWant(byteRange{0, 65}, byteRange{0, 4}, byteRange{57, 64}, '~', "def foo(x)\n  return 3\nend\n", "ruby startline=3 $%@#$", byteRange{8, 30}, nil, byteRange{31, 41}, byteRange{41, 42}, byteRange{42, 52}, byteRange{52, 53}, byteRange{53, 56}, byteRange{56, 57})},
		144: {fencedWant(byteRange{0, 11}, byteRange{0, 4}, byteRange{6, 10}, '`', "", ";", byteRange{4, 5}, nil)},
		145: {atxParagraphWant(byteRange{0, 15}, "``` aa ```\nfoo", byteRange{0, 11}, byteRange{11, 14})},
		146: {fencedWant(byteRange{0, 23}, byteRange{0, 3}, byteRange{19, 22}, '~', "foo\n", "aa ``` ~~~", byteRange{4, 14}, nil, byteRange{15, 18}, byteRange{18, 19})},
		147: {fencedWant(byteRange{0, 16}, byteRange{0, 3}, byteRange{12, 15}, '`', "``` aaa\n", "", byteRange{}, nil, byteRange{4, 11}, byteRange{11, 12})},
	}
	b, err := os.ReadFile("testdata/commonmark/0.31.2/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples []struct {
		ID                      int `json:"example"`
		Markdown, HTML, Section string
	}
	if err := json.Unmarshal(b, &examples); err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, e := range examples {
		want, ok := wants[e.ID]
		if !ok {
			continue
		}
		section := "Fenced code blocks"
		if e.ID == 19 {
			section = "Backslash escapes"
		}
		if seen[e.ID] || e.Section != section {
			t.Fatal("duplicate/wrong-section upstream identity", e.ID)
		}
		seen[e.ID] = true
		t.Run(strconv.Itoa(e.ID), func(t *testing.T) {
			complete := e.ID != 121 && e.ID != 128 && e.ID != 138 && e.ID != 140 && e.ID != 141 && e.ID != 145
			r := checkATX(t, e.Markdown, complete, want)
			if complete && conformance.Evaluate(r).HTML != e.HTML {
				t.Fatal("actual parser differs from original upstream HTML")
			}
		})
	}
	if len(seen) != 30 {
		t.Fatal("not every fenced-code target and cross-feature 19 was exercised")
	}
}

func TestFencedLiteralIsolationAndBoundaries(t *testing.T) {
	for _, character := range []string{"`", "~"} {
		fence := strings.Repeat(character, 4)
		payload := "[x]: /url\n# title\n- list\n<div> &amp; \\*\n<!-- mdchk -->\n"
		input := fence + "\n" + payload + fence + "  \t\n***\n"
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		if r.Status != parser.Complete || r.Failure != nil || len(r.Document.References().Definitions) != 0 || len(r.Document.References().Lookup) != 0 {
			t.Fatal("literal payload interpreted", r)
		}
		nodes := r.Document.Root().Children()
		if len(nodes) != 2 || nodes[0].Properties().Literal != parser.Some(payload) || len(nodes[0].Children()) != 0 || nodes[1].Kind() != parser.ThematicBreak {
			t.Fatal("literal payload or following dispatch lost")
		}
		if conformance.Evaluate(r).HTML != "<pre><code>[x]: /url\n# title\n- list\n&lt;div&gt; &amp;amp; \\*\n&lt;!-- mdchk --&gt;\n</code></pre>\n<hr />\n" {
			t.Fatal("literal test-renderer escaping changed")
		}
	}
	for _, input := range []string{"```", "~~~   \t", " ```\n", "```\n```", "~~~\n   ~~~~ \t"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		if r.Status != parser.Complete || r.Failure != nil {
			t.Fatal("valid empty/EOF fence rejected", input, r)
		}
		n := r.Document.Root().Children()[0]
		if n.Span().Start() != 0 || n.Span().End() != len(input) || n.Properties().Literal != parser.Some("") || len(n.Content()) != 0 {
			t.Fatal("empty fence metadata", input, n)
		}
		closed := strings.Count(input, "\n") > 0 && (strings.Contains(input, "\n```") || strings.Contains(input, "\n   ~~~~"))
		if (len(n.Markers()) == 2) != closed {
			t.Fatal("missing/fabricated closer", input)
		}
	}
	for _, input := range []string{"~~\n", "`~`\n", "```` a`\n", "\t```\n", "    ~~~\n", "a\n\t```\n"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		for _, n := range r.Document.Root().Children() {
			if n.Properties().Code != nil && n.Properties().Code.Style == "fenced" {
				t.Fatal("invalid opener recognized", input)
			}
		}
	}
}

func TestFencedInfoAvailability(t *testing.T) {
	for _, info := range []string{"go", "a&b", "&x;", "&#;", "&#x;", "&#12345678;", "&#x1234567;", "C:\\path", "end\\", "a\u00a0b", "a&b\"c"} {
		input := "~~~  \t" + info + " \t\n~~~\n"
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		if r.Status != parser.Complete || r.Failure != nil {
			t.Fatal("literal info needlessly deferred", info, r)
		}
		v := r.Document.Root().Children()[0].Properties().Code.Info
		if v == nil || v.Semantic != parser.Some(info) {
			t.Fatal("literal info lost", info, v)
		}
		if info == "a&b\"c" && conformance.Evaluate(r).HTML != "<pre><code class=\"language-a&amp;b&quot;c\"></code></pre>\n" {
			t.Fatal("info word was not escaped as attribute data")
		}
	}
	for _, info := range []string{"\\*", "&amp;", "&#65;", "&#x41;", "&NotEqualTilde;", "&unknown;", "a \\&amp;"} {
		input := "~~~ " + info + "\ncontent\n~~~\n"
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		if r.Status != parser.Complete || r.Phases.Blocks != parser.PhaseComplete || r.Phases.Inlines != parser.PhaseComplete || r.Document.References().LookupAvailability != parser.Available || conformance.Evaluate(r).Status != "rendered" {
			t.Fatal("decoded info did not complete", info, r)
		}
		n := r.Document.Root().Children()[0]
		v := n.Properties().Code.Info
		if v == nil || v.Semantic != parser.Some(oracleText(info, entityOracle(t))) || n.Properties().Literal != parser.Some("content\n") {
			t.Fatal("decoded info or literal payload", info, v)
		}
		var raw strings.Builder
		for _, part := range v.Segments {
			text, err := r.Source.Text(part.Source)
			if err != nil {
				t.Fatal(err)
			}
			raw.WriteString(text)
		}
		if raw.String() != info || v.Segments[0].Source.Start() != 4 || v.Segments[len(v.Segments)-1].Source.End() != 4+len(info) {
			t.Fatal("raw info lost", raw.String())
		}
	}
}

func TestFencedNormalizedSourceOwnershipAndEOF(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, closed := range []bool{false, true} {
			input := "  ~~~ é\x00 \t" + ending + "\té\x00\t " + ending
			if closed {
				input += " ~~~~\t" + ending
			}
			original := []byte(input)
			source := parser.NewSource(original)
			r := parser.Parse(context.Background(), source)
			if r.Status != parser.Complete || r.Failure != nil {
				t.Fatal(r)
			}
			n := r.Document.Root().Children()[0]
			props := n.Properties()
			if n.Span().Start() != 0 || n.Span().End() != len(input) || props.Literal != parser.Some("  é\ufffd\t \n") || props.Code.Info.Semantic != parser.Some("é\ufffd") {
				t.Fatal("normalization lost", props)
			}
			start := 11 + len(ending) // independently counted opener bytes
			transformation := parser.Normalized
			if ending == "\n" {
				transformation = parser.Exact
			}
			want := []struct {
				start, end     int
				transformation parser.Transformation
				text           string
			}{
				{start, start + 1, parser.PartialTab, "  "}, {start + 1, start + 3, parser.Exact, "é"},
				{start + 3, start + 4, parser.Normalized, "\ufffd"}, {start + 4, start + 6, parser.Exact, "\t "},
				{start + 6, start + 6 + len(ending), transformation, "\n"},
			}
			segments := n.Content()
			if len(segments) != len(want) {
				t.Fatal("lost payload provenance", segments)
			}
			foreign := parser.NewSource([]byte(input))
			for j, s := range segments {
				w := want[j]
				if s.Source.Start() != w.start || s.Source.End() != w.end || s.Transformation != w.transformation || s.Semantic != parser.Some(w.text) {
					t.Fatalf("segment %d: %+v want %+v", j, s, w)
				}
				if _, err := source.Text(s.Source); err != nil {
					t.Fatal(err)
				}
				if _, err := foreign.Text(s.Source); !errors.Is(err, parser.ErrRange) {
					t.Fatal("foreign source accepted range")
				}
			}
			markers := n.Markers()
			if markers[0].Source.Start() != 2 || markers[0].Source.End() != 5 || markers[1].Role != "indent-prefix" || markers[1].Source.Start() != start || markers[1].Source.End() != start+1 || (len(markers) == 3) != closed {
				t.Fatal("token/partial-tab markers", markers)
			}
			info := props.Code.Info
			if len(info.Segments) != 2 || info.Segments[0].Source.Start() != 6 || info.Segments[0].Source.End() != 8 || info.Segments[1].Source.Start() != 8 || info.Segments[1].Source.End() != 9 || info.Segments[1].Transformation != parser.Normalized {
				t.Fatal("info provenance", info)
			}
			original[0] = '!'
			source.Bytes()[0] = '!'
			n.Content()[0].Semantic = parser.Some("changed")
			n.Markers()[0].Role = "changed"
			props.Code.Info.Segments[0].Semantic = parser.Some("changed")
			props.Code.Info.Semantic = parser.Some("changed")
			props.Code.Style = "indented"
			if string(source.Bytes()) != input || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
				t.Fatal("mutation escaped immutable graph")
			}
		}
	}
	input := "```\nlast"
	r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
	n := r.Document.Root().Children()[0]
	segments := n.Content()
	if r.Status != parser.Complete || n.Properties().Literal != parser.Some("last\n") || len(segments) != 2 || segments[1].Transformation != parser.Synthetic || segments[1].Source.Start() != 8 || segments[1].Source.End() != 8 || len(n.Markers()) != 1 {
		t.Fatal("unterminated payload EOF provenance", r, segments)
	}
}

func TestFencedEncodingAndInterruption(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, c := range []context.Context{ctx, expiredContext()} {
		r := parser.Parse(c, parser.NewSource([]byte("```\ncode\n")))
		if r.Status != parser.Incomplete || r.Interruption == "" || r.Document.References().LookupAvailability != parser.Unavailable || len(r.Document.Root().Children()) != 0 || conformance.Evaluate(r).Status != "interrupted" {
			t.Fatal("interrupted fence gained recognition", r)
		}
		r = parser.Parse(c, parser.NewSource([]byte("```\n\xff\n")))
		if r.Status != parser.Failed || r.Failure == nil || r.Failure.Code != parser.InvalidUTF8 || r.Failure.Location.Start() != 4 || r.Interruption != "" {
			t.Fatal("encoding precedence lost", r)
		}
	}
}
