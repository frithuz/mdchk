package parser_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

var oracleOnce sync.Once
var oracleEntities map[string]string
var oracleError error

func entityOracle(t testing.TB) map[string]string {
	t.Helper()
	oracleOnce.Do(func() {
		var data []byte
		data, oracleError = os.ReadFile("testdata/html5/entities.json")
		if oracleError != nil {
			return
		}
		var entries map[string]struct{ Characters string }
		oracleError = json.Unmarshal(data, &entries)
		oracleEntities = make(map[string]string)
		for key, value := range entries {
			if strings.HasSuffix(key, ";") {
				oracleEntities[key] = value.Characters
			}
		}
	})
	if oracleError != nil {
		t.Fatal(oracleError)
	}
	return oracleEntities
}

// The test oracle uses the independently retained authoritative data and a
// regular expression/strconv, not the production token recognizer or table.
var oracleTokens = regexp.MustCompile(`\\[[:punct:]]|&(?:#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6}|[A-Za-z][A-Za-z0-9]*);`)

func oracleText(text string, entities map[string]string) string {
	text = strings.ReplaceAll(text, "\x00", "\ufffd")
	return oracleTokens.ReplaceAllStringFunc(text, func(token string) string {
		if token[0] == '\\' {
			return token[1:]
		}
		if token[1] != '#' {
			if value, ok := entities[token]; ok {
				return value
			}
			return token
		}
		digits, base := token[2:len(token)-1], 10
		if digits[0] == 'x' || digits[0] == 'X' {
			digits, base = digits[1:], 16
		}
		n, err := strconv.ParseUint(digits, base, 32)
		if err != nil {
			panic(err)
		}
		if n == 0 || !utf8.ValidRune(rune(n)) {
			return "\ufffd"
		}
		return string(rune(n))
	})
}

func assertDecodedParts(t *testing.T, source parser.Source, parts []parser.Segment, ranges []byteRange, entities map[string]string) {
	t.Helper()
	index, offset := 0, ranges[0].start
	for _, part := range parts {
		if index >= len(ranges) || part.Source.Start() != offset || part.Source.End() > ranges[index].end || !part.Semantic.Present {
			t.Fatal("lost original ordered range", part, ranges)
		}
		raw, err := source.Text(part.Source)
		if err != nil {
			t.Fatal(err)
		}
		want := raw
		switch part.Transformation {
		case parser.Normalized:
			want = strings.ReplaceAll(raw, "\x00", "\ufffd")
		case parser.Escape, parser.Entity:
			want = oracleText(raw, entities)
		case parser.Exact:
		default:
			t.Fatal("unexpected inline provenance", part)
		}
		if part.Semantic != parser.Some(want) {
			t.Fatal("incorrect independent contribution", raw, part, want)
		}
		offset = part.Source.End()
		if offset == ranges[index].end {
			index++
			if index < len(ranges) {
				offset = ranges[index].start
			}
		}
	}
	if index != len(ranges) {
		t.Fatal("original source contribution omitted", parts, ranges)
	}
}

func TestDecodingOnePassStructureAndOpaqueBoundaries(t *testing.T) {
	for _, tc := range []struct{ input, literal string }{
		{`\&ouml;`, "&ouml;"}, {"&amp;ouml;", "&ouml;"}, {"&amp;amp;", "&amp;"},
		{`\*foo\*`, "*foo*"}, {`\[foo]`, "[foo]"}, {"\\`foo`", "`foo`"},
		{`\<br/>`, "<br/>"}, {"&#42;foo&#42;", "*foo*"}, {"foo&#10;&#10;bar", "foo\n\nbar"},
		{"&#9;foo", "\tfoo"}, {"&#13;foo", "\rfoo"}, {"é&amp;β", "é&β"},
		{"&notit; &unknown; &amp", "&notit; &unknown; &amp"},
		{"\\\\*", "\\*"}, {"\\x\\", "\\x\\"},
	} {
		t.Run(strconv.Quote(tc.input), func(t *testing.T) {
			source := parser.NewSource([]byte(tc.input))
			r := parser.Parse(context.Background(), source)
			checkStagedTextOutcome(t, r, true)
			block := r.Document.Root().Children()[0]
			children := block.Children()
			if block.Kind() != parser.Paragraph || len(children) != 1 || children[0].Kind() != parser.Text || children[0].Properties().Literal != parser.Some(tc.literal) {
				t.Fatal("decoded bytes became syntax or wrong semantics", r, children)
			}
			assertDecodedParts(t, source, children[0].Content(), []byteRange{{0, len(tc.input)}}, entityOracle(t))
			if conformance.Evaluate(r).Status != "rendered" || string(source.Bytes()) != tc.input || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
				t.Fatal("untruthful outcome/source mutation")
			}
		})
	}
	for _, input := range []string{"\\\\*emphasis*", "<https://example.com/\\[\\>", "text <i title='\\>'>", "x\\\ny", "x\\\r\ny", "[x](/a&amp;b)", "<https://e.test/?a&amp;b>"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		if r.Status != parser.Incomplete || r.Phases.Inlines != parser.Pending || len(r.Uncertainty) != 1 || conformance.Evaluate(r).Status != "pending" {
			t.Fatal("undelivered provider became complete", input, r)
		}
	}
	input := "~~~ &amp;\n\\* &amp;\n~~~\n\n<!-- \\* &amp; -->\n"
	r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
	checkStagedTextOutcome(t, r, true)
	nodes := r.Document.Root().Children()
	if len(nodes) != 2 || nodes[0].Properties().Literal != parser.Some("\\* &amp;\n") || nodes[0].Properties().Code.Info.Semantic != parser.Some("&") || len(nodes[1].Content()) != 1 || nodes[1].Content()[0].Semantic != parser.Some("<!-- \\* &amp; -->\n") || len(nodes[0].Children()) != 0 || len(nodes[1].Children()) != 0 {
		t.Fatal("opaque payload changed", nodes)
	}
}

func TestDecodingIndependentSpansAndConsumerPhases(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, terminal := range []string{"", ending} {
			input := "> - é&amp;β" + ending + ">   \\*\x00" + terminal
			source := parser.NewSource([]byte(input))
			r := parser.Parse(context.Background(), source)
			var text *parser.Node
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() == parser.Text {
					text = n
				}
				return true
			})
			if text == nil || text.Properties().Literal != parser.Some("é&β"+ending+"*\ufffd") {
				t.Fatal("segmented semantics", r, text)
			}
			parts := text.Content()
			wants := []struct {
				a, b     int
				kind     parser.Transformation
				semantic string
			}{
				{4, 6, parser.Exact, "é"}, {6, 11, parser.Entity, "&"}, {11, 13 + len(ending), parser.Exact, "β" + ending},
				{17 + len(ending), 19 + len(ending), parser.Escape, "*"}, {19 + len(ending), 20 + len(ending), parser.Normalized, "\ufffd"},
			}
			if len(parts) != len(wants) {
				t.Fatal("split source oracle", parts)
			}
			for j, w := range wants {
				g := parts[j]
				if g.Source.Start() != w.a || g.Source.End() != w.b || g.Transformation != w.kind || g.Semantic != parser.Some(w.semantic) {
					t.Fatalf("%q part %d = %+v want %+v", input, j, g, w)
				}
			}
			// Returned nested storage is detached; original Source remains intact.
			text.Content()[0].Semantic = parser.Some("changed")
			*text = parser.Node{}
			if string(source.Bytes()) != input || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
				t.Fatal("ownership")
			}
			refInput := "> [a\\!]: /é&amp; " + "\"first\\*" + ending + "> second &NotEqualTilde;\"" + terminal
			ref := parser.Parse(context.Background(), parser.NewSource([]byte(refInput)))
			checkStagedTextOutcome(t, ref, true)
			props := ref.Document.References().Definitions[0].Properties()
			if props.Destination.Semantic != parser.Some("/é&") || props.Title == nil || props.Title.Semantic != parser.Some("first*\nsecond \u2242\u0338") || props.NormalizedLabel != parser.Some(`a\!`) {
				t.Fatal("reference semantics/identity", props)
			}
			for _, part := range props.Title.Segments {
				raw, err := ref.Source.Text(part.Source)
				if err != nil || strings.Contains(raw, "> ") {
					t.Fatal("reference prefix entered component", raw, err)
				}
			}
		}
	}
	for _, input := range []string{"# é&amp;β #\n", "é&amp;β\n===\n"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
		if r.Document.Root().Children()[0].Children()[0].Properties().Literal != parser.Some("é&β") {
			t.Fatal("heading marker entered Text")
		}
	}
	for _, title := range []string{"", ` ""`, ` "&unknown;"`} {
		input := "[a&amp;]: <>" + title + "\n[a&amp;]: /other\n\ntext &amp;\n"
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
		refs := r.Document.References()
		props := refs.Definitions[0].Properties()
		if refs.Lookup["a&amp;"] != 0 || len(refs.Lookup) != 1 || len(refs.Definitions) != 2 || props.Destination.Semantic != parser.Some("") || len(props.Destination.Segments) != 1 || props.Destination.Segments[0].Source.Start() != props.Destination.Segments[0].Source.End() || (props.Title == nil) != (title == "") {
			t.Fatal("empty/absent/duplicate contract", props, refs)
		}
		if title != "" && (!props.Title.Semantic.Present || props.Title.Semantic.Value != strings.Trim(title, " \"")) {
			t.Fatal("empty/literal title", props.Title)
		}
	}
}

func TestDecodingUpstreamAcceptanceAndNewPasses(t *testing.T) {
	data, err := os.ReadFile("testdata/commonmark/0.31.2/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples []struct {
		Example        int
		Markdown, HTML string
	}
	if err = json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	newPasses := map[int]bool{12: true, 24: true, 26: true, 27: true, 29: true, 30: true, 34: true, 38: true, 39: true, 40: true, 65: true, 76: true, 102: true, 563: true, 592: true}
	// Independently counted original ranges and semantic values for every new
	// complete case; no expected position is derived from Parse or its markers.
	type expectedLeaf struct {
		kind parser.Kind
		a, b int
		text string
	}
	leaves := map[int][]expectedLeaf{
		12:  {{parser.Text, 0, 64, "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"}},
		24:  {{parser.CodeBlock, 4, 12, "foo+bar"}},
		26:  {{parser.Text, 0, 25, "# Ӓ Ϡ �"}},
		27:  {{parser.Text, 0, 22, "\" ആ ಫ"}},
		29:  {{parser.Text, 0, 5, "&copy"}},
		30:  {{parser.Text, 0, 14, "&MadeUpEntity;"}},
		34:  {{parser.CodeBlock, 4, 17, "föö"}},
		38:  {{parser.Text, 0, 9, "* foo"}, {parser.Text, 13, 16, "foo"}},
		39:  {{parser.Text, 0, 16, "foo\n\nbar"}},
		40:  {{parser.Text, 0, 7, "\tfoo"}},
		65:  {{parser.Text, 0, 7, "## foo"}},
		76:  {{parser.Text, 4, 12, "foo ###"}, {parser.Text, 16, 24, "foo ###"}, {parser.Text, 27, 33, "foo #"}},
		102: {{parser.Text, 0, 6, "> foo"}},
		563: {{parser.Text, 0, 6, "[foo]"}},
		592: {{parser.Text, 0, 7, "![foo]"}},
	}
	seen := 0
	for _, e := range examples {
		if (e.Example < 12 || e.Example > 41) && !newPasses[e.Example] {
			continue
		}
		seen++
		t.Run(fmt.Sprint(e.Example), func(t *testing.T) {
			r := parser.Parse(context.Background(), parser.NewSource([]byte(e.Markdown)))
			checkCorpusTextOutcome(t, e.Example, e.HTML, r)
			leafIndex := 0
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if newPasses[e.Example] && (n.Kind() == parser.Text || n.Kind() == parser.CodeBlock) {
					if leafIndex >= len(leaves[e.Example]) {
						t.Fatal("extra upstream leaf")
					}
					want := leaves[e.Example][leafIndex]
					leafIndex++
					value := parser.Value{Segments: n.Content(), Semantic: n.Properties().Literal}
					if n.Kind() == parser.CodeBlock {
						value = *n.Properties().Code.Info
					}
					if n.Kind() != want.kind || value.Semantic != parser.Some(want.text) {
						t.Fatal("independent upstream leaf", n, want)
					}
					assertDecodedParts(t, r.Source, value.Segments, []byteRange{{want.a, want.b}}, entityOracle(t))
				}
				if n.Kind() == parser.Text {
					// Raw block payloads independently precede decoded children.
					var raw strings.Builder
					for _, part := range n.Content() {
						s, err := r.Source.Text(part.Source)
						if err != nil {
							t.Fatal(err)
						}
						raw.WriteString(s)
					}
					if n.Properties().Literal != parser.Some(oracleText(raw.String(), entityOracle(t))) {
						t.Fatal("upstream semantic/source oracle", n)
					}
				}
				return true
			})
			if leafIndex != len(leaves[e.Example]) {
				t.Fatal("missing independently expected leaves", leafIndex)
			}
		})
	}
	if seen != 35 {
		t.Fatal("missing unchanged upstream acceptance cases", seen)
	}
}
