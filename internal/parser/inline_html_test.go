package parser_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

// An independent declarative grammar oracle, not the production scanner's
// state machine or terminator index. Matches the first complete raw construct.
var oracleInlineHTML = func() *regexp.Regexp {
	ending := `(?:\r\n|\r|\n)`
	space := `[ \t]*(?:` + ending + `[ \t]*)?`
	separator := `(?:[ \t]+(?:` + ending + `[ \t]*)?|` + ending + `[ \t]*)`
	name := `[A-Za-z][A-Za-z0-9-]*`
	value := "(?:[^ \\t\\r\\n\"'=<>`]+|'[^']*'|\"[^\"]*\")"
	attribute := separator + `[A-Za-z_:][A-Za-z0-9_.:-]*(?:` + space + `=` + space + value + `)?`
	return regexp.MustCompile(`(?s)^(?:<` + name + `(?:` + attribute + `)*` + space + `/?` + `>|</` + name + space + `>|<!--(?:>|->|.*?-->)|<\?.*?\?>|<![A-Za-z][^>]*>|<!\[CDATA\[.*?\]\]>)`)
}()

func htmlInlineWant(start, end int, raw string) inlineWant {
	return inlineWant{kind: parser.InlineHTML, span: byteRange{start, end}, literal: raw, payload: []byteRange{{start, end}}}
}

func checkInlineHTMLProvenance(t *testing.T, source parser.Source, n *parser.Node) {
	t.Helper()
	if n.Kind() != parser.InlineHTML || len(n.Children()) != 0 || n.Properties().Literal.Present || n.Properties().HTML == nil || n.Properties().HTML.BlockForm.Present {
		t.Fatal("raw inline identity/opacity", n)
	}
	parts := n.Content()
	var logical strings.Builder
	last := n.Span().Start()
	for _, part := range parts {
		raw, err := source.Text(part.Source)
		if err != nil || part.Source.Start() < last || part.Source.End() > n.Span().End() || !part.Semantic.Present {
			t.Fatal("foreign or unordered raw contribution", part, err)
		}
		want := raw
		switch part.Transformation {
		case parser.Exact:
		case parser.Normalized:
			if raw != "\x00" {
				t.Fatal("unexpected HTML normalization", part)
			}
			want = "\ufffd"

		default:
			t.Fatal("raw HTML was decoded", part)
		}
		if part.Semantic.Value != want {
			t.Fatal("raw semantics differ", raw, part)
		}
		logical.WriteString(want)
		last = part.Source.End()
	}
	if len(parts) == 0 || parts[0].Source.Start() != n.Span().Start() || last != n.Span().End() || oracleInlineHTML.FindString(logical.String()) != logical.String() {
		t.Fatal("raw source does not establish pinned grammar/envelope", logical.String(), n.Span())
	}
	markers := n.Markers()
	if strings.HasPrefix(logical.String(), "<!--") {
		width := 3
		if logical.String() == "<!-->" {
			width = 1
		} else if logical.String() == "<!--->" {
			width = 2
		}
		if len(markers) != 2 || markers[0].Role != "html-comment-open" || markers[1].Role != "html-comment-close" || markers[0].Source.Start() != n.Span().Start() || markers[0].Source.End() != n.Span().Start()+4 || markers[1].Source.Start() != n.Span().End()-width || markers[1].Source.End() != n.Span().End() {
			t.Fatal("confirmed comment boundaries", markers)
		}
		if raw, err := source.Text(markers[0].Source); err != nil || raw != "<!--" {
			t.Fatal("invented opener", raw, err)
		}
	} else if len(markers) != 0 {
		t.Fatal("noncomment acquired comment context", markers)
	}
}

func TestInlineHTMLUpstreamAcceptanceAndNewPasses(t *testing.T) {
	// Independently counted original HTML-node byte envelopes. Empty lists assert
	// malformed literal fallback, not just agreement with an HTML renderer.
	wants := map[int][]byteRange{
		187: {{4, 18}}, 344: {{0, 12}}, 475: {{1, 27}}, 476: {{2, 15}}, 477: {{2, 15}},
		524: {{5, 24}}, 536: {{5, 24}},
		613: {{0, 3}, {3, 8}, {8, 13}}, 614: {{0, 4}, {4, 9}},
		615: {{0, 6}, {6, 22}}, 616: {{0, 63}}, 617: {{4, 38}},
		618: {}, 619: {}, 620: {}, 621: {}, 622: {}, 623: {{0, 4}, {4, 11}}, 624: {},
		625: {{4, 48}}, 626: {{4, 9}, {23, 29}}, 627: {{4, 21}}, 628: {{4, 23}},
		629: {{4, 19}}, 630: {{4, 21}}, 631: {{4, 17}}, 632: {},
		642: {{0, 20}}, 643: {{0, 19}},
	}
	data, err := os.ReadFile("testdata/commonmark/0.31.2/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples []struct {
		Example        int
		Markdown, HTML string
	}
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range examples {
		want, ok := wants[e.Example]
		if !ok {
			continue
		}
		seen++
		t.Run(strconv.Itoa(e.Example), func(t *testing.T) {
			source := parser.NewSource([]byte(e.Markdown))
			r := parser.Parse(context.Background(), source)
			checkStagedTextOutcome(t, r, true)
			index := 0
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				switch n.Kind() {
				case parser.InlineHTML:
					checkInlineHTMLProvenance(t, source, n)
					if index >= len(want) || n.Span().Start() != want[index].start || n.Span().End() != want[index].end {
						t.Fatal("independent HTML byte envelope", n.Span(), want)
					}
					index++
				case parser.Text:
					var expected strings.Builder
					for _, part := range n.Content() {
						raw, err := source.Text(part.Source)
						if err != nil {
							t.Fatal(err)
						}
						expected.WriteString(oracleText(raw, entityOracle(t)))
					}
					if n.Properties().Literal != parser.Some(expected.String()) {
						t.Fatal("rejected/surrounding text lost ordinary decoding", n)
					}
				case parser.CodeSpan, parser.Link, parser.Emphasis, parser.Strong, parser.Image, parser.HardBreak:
					t.Fatal("raw interior was redispatched", n.Kind())
				}
				return true
			})
			if index != len(want) || string(source.Bytes()) != e.Markdown || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
				t.Fatal("HTML count, source mutation or state leak", index, want)
			}
			if out := conformance.Evaluate(r); out.Status != "rendered" || out.HTML != e.HTML {
				t.Fatal("actual Parse/Evaluate differs from unchanged upstream", out, e.HTML)
			}
		})
	}
	if seen != len(wants) {
		t.Fatal("missing upstream inputs", seen)
	}
}

func TestInlineHTMLGrammarOpacityAndFallback(t *testing.T) {
	valid := []string{`<i title='\>'>`, `<a>`, `</a >`, `<a/>`, `<a-0 :x.y-z_1=bare boolean />`, `<a x="<!-- mdchk:disable MD001 -->">`, `<a x='&amp;\*[]` + "`" + `**'>`, `<!-->`, `<!--->`, `<!-- a -- b -->`, `<?pi?>`, `<!a b>`, `<![CDATA[>&<]]>`}
	invalid := []string{`<a /bar>`, `<a / >`, `</a/>`, `</a x>`, `<a 0x>`, `<a x=>`, `<a x="a"y>`, `<a x='open>`, `<a x=` + "`x`>", `<a x=a=b>`, "<a\f>", "<a\v>", `<_a>`, `<1a>`, `<!1>`, `<![cdata[a]]>`, `<?pi>`, `<!--open`, `<![CDATA[open`}
	for _, group := range []struct {
		values []string
		html   bool
	}{{valid, true}, {invalid, false}} {
		for _, raw := range group.values {
			t.Run(raw, func(t *testing.T) {
				input := "x " + raw
				r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
				if r.Failure != nil || r.Interruption != "" {
					t.Fatal(r)
				}
				count := 0
				parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
					if n.Kind() == parser.InlineHTML {
						count++
						checkInlineHTMLProvenance(t, r.Source, n)
					}
					return true
				})
				if group.html && (count != 1 || r.Status != parser.Complete) || !group.html && count != 0 {
					t.Fatal("grammar/fallback", count, r)
				}
			})
		}
	}
	for _, input := range []string{`x \<a>`, `x &lt;a>`, `x &#60;a>`, "x `<a x='<!-->'>`", "    <a>"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.InlineHTML {
				t.Fatal("generated/opaque opener recognized", input)
			}
			return true
		})
	}
	for _, input := range []string{`***x*** <a>`, `[x](a) <!-->`, `![x](a) <?p?>`} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		if r.Status != parser.Incomplete || conformance.Evaluate(r).Status != "pending" {
			t.Fatal("outside unsupported provider falsely complete", input, r)
		}
	}
	// Failed lookahead must not skip earlier quote/terminator matches at a later
	// candidate, and failed comments must not consume a subsequent valid tag.
	for _, input := range []string{`x <a b='x <i>' c="unterminated <b>`, `x <!--open <b>`, `x <?bad <b>`, `x <![CDATA[bad <b>`} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
		count := 0
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.InlineHTML {
				count++
				checkInlineHTMLProvenance(t, r.Source, n)
			}
			return true
		})
		if count == 0 {
			t.Fatal("rejected candidate hid later valid HTML", input)
		}
	}
}

func TestInlineHTMLMappedCommentsOwnershipAndEndings(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, prefix := range []string{"", "> ", "> - "} {
			continuation := prefix
			if prefix == "> - " {
				continuation = ">   "
			}
			input := prefix + "x <!--é\x00 &amp; \\*[]`" + ending + continuation + "tail -->z"
			source := parser.NewSource([]byte(input))
			r := parser.Parse(context.Background(), source)
			checkStagedTextOutcome(t, r, true)
			count := 0
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() != parser.InlineHTML {
					return true
				}
				count++
				checkInlineHTMLProvenance(t, source, n)
				if n.Span().Start() != len(prefix)+2 || n.Span().End() != len(input)-1 {
					t.Fatal("mapped envelope", n.Span())
				}
				var logical strings.Builder
				for _, part := range n.Content() {
					logical.WriteString(part.Semantic.Value)
				}
				want := "<!--é\ufffd &amp; \\*[]`" + ending + "tail -->"
				if logical.String() != want {
					t.Fatal("prefix entered raw logical content", logical.String(), want)
				}
				foreign := parser.NewSource([]byte(input))
				if _, err := foreign.Text(n.Span()); !errors.Is(err, parser.ErrRange) {
					t.Fatal("foreign source accepted", err)
				}
				n.Content()[0].Semantic = parser.Some("changed")
				n.Markers()[1].Role = "changed"
				n.Properties().HTML.BlockForm = parser.Some(1)
				*n = parser.Node{}
				return true
			})
			if count != 1 || string(source.Bytes()) != input || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
				t.Fatal("ownership/isolation", count)
			}
			if out := conformance.Evaluate(r); out.Status != "rendered" || !strings.Contains(out.HTML, "<!--é\ufffd &amp; \\*[]`"+ending+"tail -->") {
				t.Fatal("mapped serialization", out)
			}
		}
		for _, input := range []string{"# x <!--> #", "x <!-->" + ending + "===", "x <a" + ending + " b =" + ending + " 'v'>"} {
			r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
			checkStagedTextOutcome(t, r, true)
			count := 0
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() == parser.InlineHTML {
					count++
					checkInlineHTMLProvenance(t, r.Source, n)
				}
				return true
			})
			if count != 1 {
				t.Fatal("heading/multiline HTML missing", input)
			}
		}
	}
}

func TestInlineHTMLBlockBoundariesAndContinuationTabs(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		form int
	}{{"<script>", 1}, {"<!--x-->", 2}, {"<?x?>", 3}, {"<!A>", 4}, {"<![CDATA[x]]>", 5}, {"<div>", 6}, {"<custom>", 7}} {
		block := parser.Parse(context.Background(), parser.NewSource([]byte(tt.raw)))
		checkStagedTextOutcome(t, block, true)
		n := block.Document.Root().Children()[0]
		if n.Kind() != parser.HTMLBlock || n.Properties().HTML.BlockForm != parser.Some(tt.form) || len(n.Children()) != 0 {
			t.Fatal("block precedence", tt, n)
		}
		inline := parser.Parse(context.Background(), parser.NewSource([]byte("x "+tt.raw)))
		checkStagedTextOutcome(t, inline, true)
		n = inline.Document.Root().Children()[0].Children()[1]
		checkInlineHTMLProvenance(t, inline.Source, n)
	}
	// The final '>' opens a quote at the block stage; inline lookahead cannot
	// consume it across that boundary even though closing-tag grammar allows LF.
	r := parser.Parse(context.Background(), parser.NewSource([]byte("x </a\n>")))
	checkStagedTextOutcome(t, r, true)
	if nodes := r.Document.Root().Children(); len(nodes) != 2 || nodes[1].Kind() != parser.BlockQuote {
		t.Fatal("inline recognition crossed block boundary", r)
	}
	for _, input := range []string{"> - x <!--a\n> \t\tb\t -->", "> x <a title='a\n>\t\tb\t'>"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
		tabs := 0
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.InlineHTML {
				checkInlineHTMLProvenance(t, r.Source, n)
				for _, part := range n.Content() {
					if part.Transformation == parser.PartialTab {
						t.Fatal("paragraph leaked structural residual tab")
					}
					raw, _ := r.Source.Text(part.Source)
					tabs += strings.Count(raw, "\t")
				}
			}
			return true
		})
		if tabs == 0 {
			t.Fatal("fixture did not retain internal literal tab", input)
		}
		if out := conformance.Evaluate(r); out.Status != "rendered" {
			t.Fatal("raw inline tab serialization", out)
		}
	}
}
