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
	"time"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

// These literal ranges are supplied independently of parser/scanner output.
func indentedWant(envelope byteRange, literal string, prefixes []byteRange, content ...byteRange) atxBlockWant {
	w := atxBlockWant{kind: parser.CodeBlock, envelope: envelope, content: content, text: literal}
	for _, prefix := range prefixes {
		w.markers = append(w.markers, markerWant{"indent-prefix", prefix})
	}
	return w
}

func TestIndentedUpstreamTargets(t *testing.T) {
	wants := map[int][]atxBlockWant{
		1:   {indentedWant(byteRange{0, 14}, "foo\tbaz\t\tbim\n", []byteRange{{0, 1}}, byteRange{1, 13}, byteRange{13, 14})},
		2:   {indentedWant(byteRange{0, 16}, "foo\tbaz\t\tbim\n", []byteRange{{0, 3}}, byteRange{3, 15}, byteRange{15, 16})},
		3:   {indentedWant(byteRange{0, 18}, "a\ta\nὐ\ta\n", []byteRange{{0, 4}, {8, 12}}, byteRange{4, 7}, byteRange{7, 8}, byteRange{12, 17}, byteRange{17, 18})},
		8:   {indentedWant(byteRange{0, 13}, "foo\nbar\n", []byteRange{{0, 4}, {8, 9}}, byteRange{4, 7}, byteRange{7, 8}, byteRange{9, 12}, byteRange{12, 13})},
		18:  {indentedWant(byteRange{0, 9}, "\\[\\]\n", []byteRange{{0, 4}}, byteRange{4, 8}, byteRange{8, 9})},
		36:  {indentedWant(byteRange{0, 19}, "f&ouml;f&ouml;\n", []byteRange{{0, 4}}, byteRange{4, 18}, byteRange{18, 19})},
		48:  {indentedWant(byteRange{0, 8}, "***\n", []byteRange{{0, 4}}, byteRange{4, 7}, byteRange{7, 8})},
		107: {indentedWant(byteRange{0, 39}, "a simple\n  indented code block\n", []byteRange{{0, 4}, {13, 17}}, byteRange{4, 12}, byteRange{12, 13}, byteRange{17, 38}, byteRange{38, 39})},
		108: {continuedItemWant(bulletWant(byteRange{0, 8}, byteRange{0, 4}, byteRange{2, 3}, '-', 1, 4, "foo", byteRange{4, 7}), 17, []markerWant{{"item-blank", byteRange{8, 9}}, {"item-indent", byteRange{9, 13}}}, atxParagraphWant(byteRange{4, 8}, "foo", byteRange{4, 7}), atxParagraphWant(byteRange{13, 17}, "bar", byteRange{13, 16}))},
		109: {continuedItemWant(orderedWant(byteRange{0, 8}, byteRange{0, 4}, byteRange{0, 2}, 1, '.', 2, 4, "foo", byteRange{4, 7}), 19, []markerWant{{"item-blank", byteRange{8, 9}}, {"item-indent", byteRange{9, 13}}}, atxParagraphWant(byteRange{4, 8}, "foo", byteRange{4, 7}), bulletWant(byteRange{13, 19}, byteRange{13, 15}, byteRange{13, 14}, '-', 1, 2, "bar", byteRange{15, 18}))},
		110: {indentedWant(byteRange{0, 29}, "<a/>\n*hi*\n\n- one\n", []byteRange{{0, 4}, {9, 13}, {19, 23}}, byteRange{4, 8}, byteRange{8, 9}, byteRange{13, 17}, byteRange{17, 18}, byteRange{18, 19}, byteRange{23, 28}, byteRange{28, 29})},
		111: {indentedWant(byteRange{0, 41}, "chunk1\n\nchunk2\n\n\n\nchunk3\n", []byteRange{{0, 4}, {12, 16}, {23, 25}, {26, 27}, {28, 29}, {30, 34}}, byteRange{4, 10}, byteRange{10, 11}, byteRange{11, 12}, byteRange{16, 22}, byteRange{22, 23}, byteRange{25, 26}, byteRange{27, 28}, byteRange{29, 30}, byteRange{34, 40}, byteRange{40, 41})},
		112: {indentedWant(byteRange{0, 31}, "chunk1\n  \n  chunk2\n", []byteRange{{0, 4}, {11, 15}, {18, 22}}, byteRange{4, 10}, byteRange{10, 11}, byteRange{15, 17}, byteRange{17, 18}, byteRange{22, 30}, byteRange{30, 31})},
		113: {atxParagraphWant(byteRange{0, 12}, "Foo\nbar", byteRange{0, 4}, byteRange{8, 11})},
		114: {indentedWant(byteRange{0, 8}, "foo\n", []byteRange{{0, 4}}, byteRange{4, 7}, byteRange{7, 8}), atxParagraphWant(byteRange{8, 12}, "bar", byteRange{8, 11})},
		115: {atxHeadingWant(1, byteRange{0, 10}, byteRange{0, 1}, byteRange{2, 9}, byteRange{}, "Heading"), indentedWant(byteRange{10, 18}, "foo\n", []byteRange{{10, 14}}, byteRange{14, 17}, byteRange{17, 18}), setextWant(2, byteRange{18, 33}, byteRange{26, 32}, "Heading", byteRange{18, 25}), indentedWant(byteRange{33, 41}, "foo\n", []byteRange{{33, 37}}, byteRange{37, 40}, byteRange{40, 41}), atxThematicWant(byteRange{41, 46}, byteRange{41, 45})},
		116: {indentedWant(byteRange{0, 20}, "    foo\nbar\n", []byteRange{{0, 4}, {12, 16}}, byteRange{4, 11}, byteRange{11, 12}, byteRange{16, 19}, byteRange{19, 20})},
		117: {indentedWant(byteRange{6, 14}, "foo\n", []byteRange{{6, 10}}, byteRange{10, 13}, byteRange{13, 14})},
		118: {indentedWant(byteRange{0, 10}, "foo  \n", []byteRange{{0, 4}}, byteRange{4, 9}, byteRange{9, 10})},
		134: {indentedWant(byteRange{0, 24}, "```\naaa\n```\n", []byteRange{{0, 4}, {8, 12}, {16, 20}}, byteRange{4, 7}, byteRange{7, 8}, byteRange{12, 15}, byteRange{15, 16}, byteRange{20, 23}, byteRange{23, 24})},
		231: {indentedWant(byteRange{0, 32}, "> # Foo\n> bar\n> baz\n", []byteRange{{0, 4}, {12, 16}, {22, 26}}, byteRange{4, 11}, byteRange{11, 12}, byteRange{16, 21}, byteRange{21, 22}, byteRange{26, 31}, byteRange{31, 32})},
		289: {indentedWant(byteRange{0, 97}, "1.  A paragraph\n    with two lines.\n\n        indented code\n\n    > A block quote.\n", []byteRange{{0, 4}, {20, 24}, {45, 49}, {72, 76}}, byteRange{4, 19}, byteRange{19, 20}, byteRange{24, 43}, byteRange{43, 44}, byteRange{44, 45}, byteRange{49, 70}, byteRange{70, 71}, byteRange{71, 72}, byteRange{76, 96}, byteRange{96, 97})},
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
		if seen[e.ID] {
			t.Fatal("duplicate upstream identity", e.ID)
		}
		seen[e.ID] = true
		if e.ID >= 107 && e.ID <= 118 && e.Section != "Indented code blocks" {
			t.Fatal("wrong upstream section", e.ID)
		}
		t.Run(strconv.Itoa(e.ID), func(t *testing.T) {
			complete := e.ID < 107 || e.ID == 107 || e.ID >= 110 && e.ID <= 112 || e.ID >= 116
			r := checkATX(t, e.Markdown, complete, want)
			if complete && conformance.Evaluate(r).HTML != e.HTML {
				t.Fatal("actual parser/adapter differs from original HTML")
			}
		})
	}
	if len(seen) != len(wants) {
		t.Fatal("missing direct upstream target")
	}
}

func TestIndentedLiteralBoundaries(t *testing.T) {
	// Exact LF-backed ranges, including deferred whitespace-only lines and the
	// first dedented block, are independently supplied to the common checker.
	checkATX(t, "    a\n      \n    b\n   \t \n\n", true, []atxBlockWant{
		indentedWant(byteRange{0, 19}, "a\n  \nb\n", []byteRange{{0, 4}, {6, 10}, {13, 17}}, byteRange{4, 5}, byteRange{5, 6}, byteRange{10, 12}, byteRange{12, 13}, byteRange{17, 18}, byteRange{18, 19}),
	})
	checkATX(t, "    a\n  b\n    c\n", false, []atxBlockWant{
		indentedWant(byteRange{0, 6}, "a\n", []byteRange{{0, 4}}, byteRange{4, 5}, byteRange{5, 6}),
		atxParagraphWant(byteRange{6, 16}, "b\nc", byteRange{8, 10}, byteRange{14, 15}),
	})
	checkATX(t, "\t\ta\n\t\t\n\t b\t\n", true, []atxBlockWant{
		indentedWant(byteRange{0, 12}, "\ta\n\t\n b\t\n", []byteRange{{0, 1}, {4, 5}, {7, 8}}, byteRange{1, 3}, byteRange{3, 4}, byteRange{5, 6}, byteRange{6, 7}, byteRange{8, 11}, byteRange{11, 12}),
	})
	for _, prefix := range []string{"    ", "\t", " \t", "   \t"} {
		for _, text := range []string{"***", "# x", "---", "```", "<a/>", "*x* &amp; \\*", "[foo]: /url", "1. x", "> x"} {
			input := prefix + text + "\n"
			checkATX(t, input, true, []atxBlockWant{indentedWant(byteRange{0, len(input)}, text+"\n", []byteRange{{0, len(prefix)}}, byteRange{len(prefix), len(input) - 1}, byteRange{len(input) - 1, len(input)})})
		}
	}
	for _, input := range []string{"   x\n", "a\n\tcode\n", "- item\n\n    code\n"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		if r.Status != parser.Incomplete || r.Failure != nil || r.Document.References().LookupAvailability != parser.Unavailable {
			t.Fatal("unknown/inline input gained completion", input, r)
		}

		if strings.HasPrefix(input, "- item") {
			nodes := ungroupedChildren(r.Document.Root())
			if len(nodes) != 1 || nodes[0].Kind() != parser.ListItem || nodes[0].Span().End() != len(input) || len(nodes[0].Children()) != 2 || nodes[0].Children()[1].Kind() != parser.Paragraph {
				t.Fatal("item-relative indentation was mistaken for code")
			}
		}
	}
}

func TestIndentedNormalizedSourceAndEOF(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r", ""} {
		input := " \té\x00\t " + ending
		original := []byte(input)
		source := parser.NewSource(original)
		r := parser.Parse(context.Background(), source)
		if r.Status != parser.Complete || r.Failure != nil {
			t.Fatal(r)
		}
		n := ungroupedChildren(r.Document.Root())[0]
		if n.Kind() != parser.CodeBlock || n.Span().Start() != 0 || n.Span().End() != len(input) || n.Properties().Literal != parser.Some("é\ufffd\t \n") || len(n.Children()) != 0 {
			t.Fatal("code metadata/literal", n)
		}
		markers := n.Markers()
		if len(markers) != 1 || markers[0].Role != "indent-prefix" || markers[0].Source.Start() != 0 || markers[0].Source.End() != 2 {
			t.Fatal("tab prefix lost original bytes")
		}
		endTransform := parser.Normalized
		if ending == "\n" {
			endTransform = parser.Exact
		} else if ending == "" {
			endTransform = parser.Synthetic
		}
		want := []struct {
			span      byteRange
			transform parser.Transformation
			text      string
		}{
			{byteRange{2, 4}, parser.Exact, "é"}, {byteRange{4, 5}, parser.Normalized, "\ufffd"},
			{byteRange{5, 7}, parser.Exact, "\t "}, {byteRange{7, len(input)}, endTransform, "\n"},
		}
		segments := n.Content()
		if len(segments) != len(want) {
			t.Fatal("missing provenance records", segments)
		}
		foreign := parser.NewSource([]byte(input))
		for j, s := range segments {
			w := want[j]
			if !s.Source.Present() || s.Source.Start() != w.span.start || s.Source.End() != w.span.end || s.Transformation != w.transform || s.Semantic != parser.Some(w.text) {
				t.Fatalf("ending %q segment %d: %+v", ending, j, s)
			}
			raw, err := source.Text(s.Source)
			if err != nil || raw != input[w.span.start:w.span.end] {
				t.Fatal("wrong original slice", raw, err)
			}
			if _, err := foreign.Text(s.Source); !errors.Is(err, parser.ErrRange) {
				t.Fatal("foreign snapshot accepted code range")
			}
		}
		original[0] = '!'
		bytes := source.Bytes()
		bytes[2] = '!'
		n.Content()[0].Semantic = parser.Some("changed")
		n.Markers()[0].Role = "changed"
		n.Properties().Code.Style = "fenced"
		if string(source.Bytes()) != input || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
			t.Fatal("mutation escaped immutable snapshot")
		}
	}
}

func TestIndentedEncodingAndInterruption(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, c := range []context.Context{ctx, expiredContext()} {
		r := parser.Parse(c, parser.NewSource([]byte("    code\n")))
		if r.Status != parser.Incomplete || r.Interruption == "" || r.Document.References().LookupAvailability != parser.Unavailable || len(ungroupedChildren(r.Document.Root())) != 0 || conformance.Evaluate(r).Status != "interrupted" {
			t.Fatal("interrupted code gained recognition", r)
		}
		r = parser.Parse(c, parser.NewSource([]byte("    \xff\n")))
		if r.Status != parser.Failed || r.Failure == nil || r.Failure.Code != parser.InvalidUTF8 || r.Failure.Location.Start() != 4 || r.Interruption != "" {
			t.Fatal("encoding gate lost precedence", r)
		}
	}
}

func expiredContext() context.Context {
	ctx, cancel := context.WithDeadline(context.Background(), time.Time{})
	cancel()
	return ctx
}
