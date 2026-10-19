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

func htmlWant(form, start, end int) atxBlockWant {
	return atxBlockWant{kind: parser.HTMLBlock, htmlForm: form, envelope: byteRange{start, end}, content: []byteRange{{start, end}}}
}

func TestHTMLUpstreamTargets(t *testing.T) {
	// Original-byte envelopes and form identities are independent expected facts.
	// Nil identifies a genuinely unavailable container, not successful recognition.
	wants := map[int][]atxBlockWant{
		21:  {htmlWant(7, 0, 19)},
		31:  {htmlWant(7, 0, 29)},
		148: {htmlWant(6, 0, 33), atxParagraphWant(byteRange{34, 50}, "_world_.\n</pre>", byteRange{34, 43}, byteRange{43, 49}), htmlWant(6, 50, 69)},
		149: {htmlWant(6, 0, 65), atxParagraphWant(byteRange{66, 72}, "okay.", byteRange{66, 71})},
		150: {htmlWant(6, 0, 35)}, 151: {htmlWant(6, 0, 13)},
		152: {htmlWant(6, 0, 18), atxParagraphWant(byteRange{19, 30}, "*Markdown*", byteRange{19, 29}), htmlWant(6, 31, 38)}, 153: {htmlWant(6, 0, 36)},
		154: {htmlWant(6, 0, 40)}, 155: {htmlWant(6, 0, 12), atxParagraphWant(byteRange{13, 19}, "*bar*", byteRange{13, 18})},
		156: {htmlWant(6, 0, 19)}, 157: {htmlWant(6, 0, 15)},
		158: {htmlWant(6, 0, 25)}, 159: {htmlWant(6, 0, 35)},
		160: {htmlWant(6, 0, 39)}, 161: {htmlWant(6, 0, 34)},
		162: {htmlWant(7, 0, 26)}, 163: {htmlWant(7, 0, 27)},
		164: {htmlWant(7, 0, 27)}, 165: {htmlWant(7, 0, 13)},
		166: {htmlWant(7, 0, 19)}, 167: {htmlWant(7, 0, 6), atxParagraphWant(byteRange{7, 13}, "*foo*", byteRange{7, 12}), htmlWant(7, 14, 21)},
		168: {atxParagraphWant(byteRange{0, 17}, "<del>*foo*</del>", byteRange{0, 16})},
		169: {htmlWant(1, 0, 115), atxParagraphWant(byteRange{115, 120}, "okay", byteRange{115, 119})},
		170: {htmlWant(1, 0, 130), atxParagraphWant(byteRange{130, 135}, "okay", byteRange{130, 134})},
		171: {htmlWant(1, 0, 38)},
		172: {htmlWant(1, 0, 68), atxParagraphWant(byteRange{68, 73}, "okay", byteRange{68, 72})},
		173: {htmlWant(1, 0, 31)},
		174: {quoteWant(byteRange{0, 14}, []byteRange{{0, 2}, {8, 10}}, atxBlockWant{kind: parser.HTMLBlock, htmlForm: 6, envelope: byteRange{2, 14}, content: []byteRange{{2, 8}, {10, 14}}, text: "<div>\nfoo\n"}), atxParagraphWant(byteRange{15, 19}, "bar", byteRange{15, 18})},
		175: {continuedItemWant(bulletWant(byteRange{0, 8}, byteRange{0, 2}, byteRange{0, 1}, '-', 1, 2, ""), 8, nil, atxBlockWant{kind: parser.HTMLBlock, htmlForm: 6, envelope: byteRange{2, 8}, content: []byteRange{{2, 8}}, text: "<div>\n"}), bulletWant(byteRange{8, 14}, byteRange{8, 10}, byteRange{8, 9}, '-', 1, 2, "foo", byteRange{10, 13})},
		176: {htmlWant(1, 0, 29), atxParagraphWant(byteRange{29, 35}, "*foo*", byteRange{29, 34})}, 177: {htmlWant(2, 0, 18), atxParagraphWant(byteRange{18, 24}, "*baz*", byteRange{18, 23})},
		178: {htmlWant(1, 0, 31)},
		179: {htmlWant(2, 0, 25), atxParagraphWant(byteRange{25, 30}, "okay", byteRange{25, 29})},
		180: {htmlWant(3, 0, 23), atxParagraphWant(byteRange{23, 28}, "okay", byteRange{23, 27})},
		181: {htmlWant(4, 0, 16)},
		182: {htmlWant(5, 0, 114), atxParagraphWant(byteRange{114, 119}, "okay", byteRange{114, 118})},
		183: {htmlWant(2, 0, 15), indentedWant(byteRange{16, 33}, "<!-- foo -->\n", []byteRange{{16, 20}}, byteRange{20, 32}, byteRange{32, 33})},
		184: {htmlWant(6, 0, 8), indentedWant(byteRange{9, 19}, "<div>\n", []byteRange{{9, 13}}, byteRange{13, 18}, byteRange{18, 19})},
		185: {atxParagraphWant(byteRange{0, 4}, "Foo", byteRange{0, 3}), htmlWant(6, 4, 21)},
		186: {htmlWant(6, 0, 23)},
		187: {atxParagraphWant(byteRange{0, 23}, "Foo\n<a href=\"bar\">\nbaz", byteRange{0, 4}, byteRange{4, 19}, byteRange{19, 22})},
		188: {htmlWant(6, 0, 6), atxParagraphWant(byteRange{7, 26}, "*Emphasized* text.", byteRange{7, 25}), htmlWant(6, 27, 34)}, 189: {htmlWant(6, 0, 32)},
		190: {htmlWant(6, 0, 8), htmlWant(6, 9, 14), htmlWant(6, 15, 29), htmlWant(6, 30, 36), htmlWant(6, 37, 46)},
		191: {htmlWant(6, 0, 8), htmlWant(6, 9, 16), indentedWant(byteRange{17, 45}, "<td>\n  Hi\n</td>\n", []byteRange{{17, 21}, {26, 30}, {35, 39}}, byteRange{21, 25}, byteRange{25, 26}, byteRange{30, 34}, byteRange{34, 35}, byteRange{39, 44}, byteRange{44, 45}), htmlWant(6, 46, 54), htmlWant(6, 55, 64)},
	}
	pending := map[int]bool{148: true, 149: true, 152: true, 155: true, 167: true, 168: true, 169: true, 170: true, 172: true, 174: true, 175: true, 176: true, 177: true, 179: true, 180: true, 182: true, 185: true, 187: true, 188: true}
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
		if seen[e.ID] || e.ID != 21 && e.ID != 31 && e.Section != "HTML blocks" {
			t.Fatal("wrong upstream identity", e.ID)
		}
		seen[e.ID] = true
		t.Run(strconv.Itoa(e.ID), func(t *testing.T) {
			for j := range want {
				if want[j].kind == parser.HTMLBlock {
					want[j].text = e.Markdown[want[j].envelope.start:want[j].envelope.end]
				}
			}
			r := checkATX(t, e.Markdown, !pending[e.ID], want)
			if !pending[e.ID] && conformance.Evaluate(r).HTML != e.HTML {
				t.Fatal("actual HTML differs from unchanged upstream expectation")
			}
		})
	}
	if len(seen) != 46 {
		t.Fatal("not every HTML target and cross-feature 21/31 exercised")
	}
}

func TestHTMLGrammarAndFixedForm(t *testing.T) {
	for _, tt := range []struct {
		input string
		form  int
	}{
		{"<PrE>\n</STYLE>tail", 1}, {"<script\n\n</textarea>\n", 1}, {"<textarea\tbad\n", 1},
		{"<!-->\n", 2}, {"<!--->", 2}, {"<!-- a -->tail", 2}, {"<?\n?>tail", 3},
		{"<!a\n>tail", 4}, {"<![CDATA[\n]]>tail", 5},
		{"<SEARCH/>\n<pre>\n---\n", 6}, {"</DIV whatever\n", 6},
		{"<custom a=unquoted b = 'x > y' _x:y.z-0=\"\" boolean /> \t", 7},
		{"</PRE \t>\t", 7}, {"<scripture>", 7}, {"<a-0 :x =\tvalue>", 7},
	} {
		t.Run(tt.input, func(t *testing.T) {
			w := htmlWant(tt.form, 0, len(tt.input))
			w.text = tt.input
			checkATX(t, tt.input, true, []atxBlockWant{w})
		})
	}
	for _, input := range []string{"<pre/>", "<script/>", "<style/>", "<textarea/>", "<a_b>", "<1a>", "<a x=>", "<a x=\"unclosed>", "<a x='unclosed>", "<a x=`v`>", "<a x=<v>", "<a x=v=z>", "<a x=\"v\"y>", "<a / >", "</a x>", "</a/>", "<a>tail", "<a>\u00a0", "<![cdata[", "<!1>", "<a 0x>", "<diverse bad=>"} {
		t.Run("invalid/"+input, func(t *testing.T) {
			r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
			if r.Status != parser.Incomplete || r.Failure != nil || len(r.Document.Root().Children()) != 1 || r.Document.Root().Children()[0].Kind() != parser.Paragraph {
				t.Fatal("invalid candidate did not fall back to paragraph", r)
			}
		})
	}
	for _, tt := range []struct {
		first, rest string
		form        int
	}{
		{"<pre>", "<div>\n\n</style>tail\n", 1},
		{"<!--", "<script>\n\n-->tail\n", 2},
		{"<?", "<!X>\n\n?>tail\n", 3},
		{"<!X", "\n>tail\n", 4},
		{"<![CDATA[", "<!--\n\n]]>tail\n", 5},
		{"<div>", "<pre>\n```\n", 6},
		{"<custom>", "<!--\n# heading\n", 7},
	} {
		input := tt.first + "\n" + tt.rest + "\n***\n"
		end := len(tt.first) + 1 + len(tt.rest)
		w := htmlWant(tt.form, 0, end)
		w.text = input[:end]
		checkATX(t, input, true, []atxBlockWant{w, atxThematicWant(byteRange{end + 1, end + 5}, byteRange{end + 1, end + 4})})
	}
	for _, form := range []string{"<pre>", "<!--", "<?", "<!A", "<![CDATA[", "<div>"} {
		input := "a\n" + form
		w := htmlWant(map[string]int{"<pre>": 1, "<!--": 2, "<?": 3, "<!A": 4, "<![CDATA[": 5, "<div>": 6}[form], 2, len(input))
		w.text = form
		checkATX(t, input, false, []atxBlockWant{atxParagraphWant(byteRange{0, 2}, "a", byteRange{0, 1}), w})
	}
	checkATX(t, "a\n<x>\n", false, []atxBlockWant{atxParagraphWant(byteRange{0, 6}, "a\n<x>", byteRange{0, 2}, byteRange{2, 5})})
}

func TestHTMLSourceOwnershipEndingsAndIsolation(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, terminal := range []string{ending, ""} {
			input := "  <!--é\x00 &amp; \\*" + ending + "\t[x]: /url" + ending + "<!-- mdchk -->tail  " + terminal
			original := []byte(input)
			source := parser.NewSource(original)
			r := parser.Parse(context.Background(), source)
			if r.Status != parser.Complete || r.Failure != nil || r.Document.References().Availability != parser.Available || len(r.Document.References().Definitions) != 0 || len(r.Document.References().Lookup) != 0 {
				t.Fatal("opaque HTML failed handoff", r)
			}
			n := r.Document.Root().Children()[0]
			segments := n.Content()
			if n.Kind() != parser.HTMLBlock || n.Properties().HTML.BlockForm != parser.Some(2) || n.Span().Start() != 0 || n.Span().End() != len(input) || len(n.Children()) != 0 || len(n.Markers()) != 0 || len(segments) != 3 {
				t.Fatal("HTML metadata", n, segments)
			}
			want := []struct {
				start, end int
				text       string
				transform  parser.Transformation
			}{
				{0, 8, "  <!--é", parser.Exact}, {8, 9, "\ufffd", parser.Normalized}, {9, len(input), input[9:], parser.Exact},
			}
			foreign := parser.NewSource([]byte(input))
			for j, s := range segments {
				w := want[j]
				if s.Source.Start() != w.start || s.Source.End() != w.end || s.Transformation != w.transform || s.Semantic != parser.Some(w.text) {
					t.Fatalf("provenance %d: %+v want %+v", j, s, w)
				}
				if _, err := source.Text(s.Source); err != nil {
					t.Fatal(err)
				}
				if _, err := foreign.Text(s.Source); !errors.Is(err, parser.ErrRange) {
					t.Fatal("foreign span accepted")
				}
			}
			expected := strings.ReplaceAll(input, "\x00", "\ufffd")
			if !strings.HasSuffix(expected, "\n") {
				expected += "\n"
			}
			if got := conformance.Evaluate(r); got.Status != "rendered" || got.HTML != expected {
				t.Fatal("raw spelling/semantic NUL changed", got)
			}
			original[0] = '!'
			source.Bytes()[0] = '!'
			n.Content()[0].Semantic = parser.Some("changed")
			n.Properties().HTML.BlockForm = parser.Some(7)
			if string(source.Bytes()) != input || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
				t.Fatal("mutation escaped frozen graph")
			}
		}
	}
	// A leading structural tab means indented code; tabs inside HTML stay raw.
	for _, input := range []string{"\t<div>\n", "   \t<!-- -->\n"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		if r.Status != parser.Complete || r.Document.Root().Children()[0].Kind() != parser.CodeBlock {
			t.Fatal("code precedence", r)
		}
	}
	for _, input := range []string{"<!--", "<?", "<!A", "<![CDATA[", "<pre>", "<div>", "<custom>"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		if r.Status != parser.Complete || r.Document.Root().Children()[0].Span().End() != len(input) {
			t.Fatal("normal EOF", r)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, c := range []context.Context{ctx, expiredContext()} {
		r := parser.Parse(c, parser.NewSource([]byte("<!--\nopen")))
		if r.Status != parser.Incomplete || r.Interruption == "" || len(r.Document.Root().Children()) != 0 || conformance.Evaluate(r).Status != "interrupted" {
			t.Fatal(r)
		}
		r = parser.Parse(c, parser.NewSource([]byte("<!--\n\xff")))
		if r.Status != parser.Failed || r.Failure == nil || r.Failure.Code != parser.InvalidUTF8 || r.Failure.Location.Start() != 5 || r.Interruption != "" {
			t.Fatal("encoding precedence", r)
		}
	}
}
