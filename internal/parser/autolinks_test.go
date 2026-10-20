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

// Regex oracles express the pinned grammar independently of scanner control flow.
var oracleURI = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]{1,31}:[^\x00-\x20\x7f<>]*$`)
var oracleEmail = regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")

func checkAutolink(t *testing.T, source parser.Source, n *parser.Node, start int, raw string, email bool) {
	t.Helper()
	end := start + len(raw) + 2
	literal := strings.ReplaceAll(raw, "\x00", "\ufffd")
	p := n.Properties()
	if n.Kind() != parser.Link || n.Span().Start() != start || n.Span().End() != end || p.Link == nil || p.Link.Origin != "autolink" || p.Link.Resolution != parser.Available || p.Literal.Present || p.Link.Title != nil || p.Link.NormalizedLabel.Present {
		t.Fatal("autolink kind, envelope or resolution", n, p)
	}
	m := n.Markers()
	if len(m) != 2 || m[0].Role != "destination-open" || m[1].Role != "destination-close" || m[0].Source.Start() != start || m[0].Source.End() != start+1 || m[1].Source.Start() != end-1 || m[1].Source.End() != end {
		t.Fatal("angle delimiter provenance", m)
	}
	for j, want := range []string{"<", ">"} {
		if s, err := source.Text(m[j].Source); err != nil || s != want {
			t.Fatal("angle spelling or source ownership", s, err)
		}
	}
	if p.Link.Label.Semantic != parser.Some(literal) || !reflect.DeepEqual(p.Link.Label.Segments, n.Content()) {
		t.Fatal("label/content semantics", p.Link.Label)
	}
	var bytes, semantic strings.Builder
	offset := start + 1
	for _, part := range n.Content() {
		s, err := source.Text(part.Source)
		if err != nil || part.Source.Start() != offset || part.Source.End() > end-1 || !part.Semantic.Present {
			t.Fatal("lost original address range", part, err)
		}
		want := s
		if part.Transformation == parser.Normalized && s == "\x00" {
			want = "\ufffd"
		} else if part.Transformation != parser.Exact {
			t.Fatal("opaque label was decoded or mapping invented", part)
		}
		if part.Semantic != parser.Some(want) {
			t.Fatal("literal contribution", part, want)
		}
		bytes.WriteString(s)
		semantic.WriteString(want)
		offset = part.Source.End()
	}
	if offset != end-1 || bytes.String() != raw || semantic.String() != literal {
		t.Fatal("payload coverage", bytes.String(), raw)
	}
	dest := p.Link.Destination
	if email {
		if dest.Semantic != parser.Some("mailto:"+literal) || len(dest.Segments) != len(n.Content())+1 || dest.Segments[0].Source.Present() || dest.Segments[0].Transformation != parser.Synthetic || dest.Segments[0].Semantic != parser.Some("mailto:") || !reflect.DeepEqual(dest.Segments[1:], n.Content()) {
			t.Fatal("email prefix fabricated source or changed address", dest)
		}
	} else if !reflect.DeepEqual(dest, p.Link.Label) {
		t.Fatal("URI destination spelling changed", dest)
	}
	children := n.Children()
	if len(children) != 1 || children[0].Kind() != parser.Text || children[0].Span().Start() != start+1 || children[0].Span().End() != end-1 || children[0].Properties().Literal != parser.Some(literal) || !reflect.DeepEqual(children[0].Content(), n.Content()) || len(children[0].Children()) != 0 {
		t.Fatal("opaque visible label child", children)
	}
}

func checkAutolinkProvenance(t *testing.T, source parser.Source, n *parser.Node) {
	t.Helper()
	raw, err := source.Text(n.Span())
	if err != nil || len(raw) < 3 || raw[0] != '<' || raw[len(raw)-1] != '>' {
		t.Fatal("invented autolink source", raw, err)
	}
	label := strings.ReplaceAll(raw[1:len(raw)-1], "\x00", "\ufffd")
	email := !oracleURI.MatchString(label)
	if email && !oracleEmail.MatchString(label) {
		t.Fatal("recognized outside pinned grammar", raw)
	}
	checkAutolink(t, source, n, n.Span().Start(), raw[1:len(raw)-1], email)
}

func TestAutolinkUpstreamAcceptanceAndNewPasses(t *testing.T) {
	// Expected labels and surrounding text are supplied independently of Parse.
	type linkWant struct {
		prefix, label, suffix string
		email                 bool
	}
	wants := map[int]linkWant{
		20:  {label: `https://example.com?find=\*`},
		346: {label: "https://foo.bar.`baz", suffix: "`"},
		480: {prefix: "**a", label: "https://foo.bar/?q=**"},
		481: {prefix: "__a", label: "https://foo.bar/?q=__"},
		526: {prefix: "[foo", label: "https://example.com/?search=](uri)"},
		538: {prefix: "[foo", label: "https://example.com/?search=][ref]"},
		594: {label: "http://foo.bar.baz"},
		595: {label: "https://foo.bar.baz/test?q=hello&id=22&boolean"},
		596: {label: "irc://foo.bar:2233/baz"}, 597: {label: "MAILTO:FOO@BAR.BAZ"},
		598: {label: "a+b+c:d"}, 599: {label: "made-up-scheme://foo,bar"},
		600: {label: "https://../"}, 601: {label: "localhost:5001/foo"},
		603: {label: `https://example.com/\[\`},
		604: {label: "foo@bar.example.com", email: true},
		605: {label: "foo+special@Bar.baz-bar0.com", email: true},
	}
	fallback := map[int]string{602: "<https://foo.bar/baz bim>", 606: "<foo+@bar.example.com>", 607: "<>", 608: "< https://foo.bar >", 609: "<m:abc>", 610: "<foo.bar.baz>", 611: "https://example.com", 612: "foo@bar.example.com", 618: "<33> <__>"}
	data, err := os.ReadFile("testdata/commonmark/0.31.2/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Example        int
		Markdown, HTML string
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range cases {
		w, link := wants[e.Example]
		literal, text := fallback[e.Example]
		if !link && !text {
			continue
		}
		seen++
		t.Run(strconv.Itoa(e.Example), func(t *testing.T) {
			source := parser.NewSource([]byte(e.Markdown))
			r := parser.Parse(context.Background(), source)
			checkStagedTextOutcome(t, r, true)
			nodes := r.Document.Root().Children()[0].Children()
			if link {
				index := 0
				if w.prefix != "" {
					checkInlineNodes(t, source, nodes[:1], []inlineWant{textWant(0, len(w.prefix), w.prefix)})
					index++
				}
				checkAutolink(t, source, nodes[index], len(w.prefix), w.label, w.email)
				index++
				if w.suffix != "" {
					start := len(w.prefix) + len(w.label) + 2
					checkInlineNodes(t, source, nodes[index:], []inlineWant{textWant(start, start+len(w.suffix), w.suffix)})
					index++
				}
				if len(nodes) != index {
					t.Fatal("extra inlines", nodes)
				}
			} else {
				checkInlineNodes(t, source, nodes, []inlineWant{textWant(0, len(e.Markdown)-1, literal)})
			}
			if out := conformance.Evaluate(r); out.Status != "rendered" || out.HTML != e.HTML {
				t.Fatal("actual parser/adapter", out, e.HTML)
			}
			if string(source.Bytes()) != e.Markdown {
				t.Fatal("source mutation")
			}
		})
	}
	if seen != len(wants)+len(fallback) {
		t.Fatal("missing upstream cases", seen)
	}
}

func TestAutolinkGrammarAndFallback(t *testing.T) {
	validURI := []string{"ab:", strings.Repeat("a", 32) + ":", "https://a", "Z9+.-:x", "MAILTO:FOO@BAR.BAZ", "unknown:é/β%20", "ab:![]*_'`&amp;\\", "ab:\x00"}
	validEmail := []string{"a@b", "123@a.b", ".!#$%&'*+/=?^_`{|}~-@x", "a@" + strings.Repeat("x", 63), "a@x-y.Z9"}
	invalid := []string{"a:x", strings.Repeat("a", 33) + ":x", "1a:x", "+a:x", "ab:x y", "ab:x\ty", "ab:x\ny", "ab:x\ry", "ab:x\r\ny", "a@", "@b", "a@-b", "a@b-", "a@b..c", "a@" + strings.Repeat("x", 64), "a@b_c", "a@é", `foo\+@bar.example.com`}
	for c := byte(1); c < 32; c++ {
		invalid = append(invalid, "ab:x"+string(c)+"y")
	}
	invalid = append(invalid, "ab:x\x7fy")
	for _, group := range []struct {
		values      []string
		link, email bool
	}{{validURI, true, false}, {validEmail, true, true}, {invalid, false, false}} {
		for _, label := range group.values {
			t.Run(strconv.Quote(label), func(t *testing.T) {
				input := "x <" + label + ">"
				r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
				if r.Failure != nil || r.Interruption != "" {
					t.Fatal(r)
				}
				count := 0
				parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
					if n.Kind() == parser.Link {
						count++
						checkAutolink(t, r.Source, n, 2, label, group.email)
					}
					return true
				})
				if group.link {
					checkStagedTextOutcome(t, r, true)
					if count != 1 {
						t.Fatal("missing link")
					}
				} else if count != 0 {
					t.Fatal("invalid candidate recognized")
				}
			})
		}
	}
}

func TestAutolinkOpacitySourceAndOwnership(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, prefix := range []string{"", "> ", "> - ", "# "} {
			input := prefix + "é\x00 <ab:β\x00&amp;\\[]> <a@b>" + ending
			source := parser.NewSource([]byte(input))
			r := parser.Parse(context.Background(), source)
			checkStagedTextOutcome(t, r, true)
			links := 0
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() != parser.Link {
					return true
				}
				links++
				start := len(prefix) + 4 // original bytes of é, NUL and space
				if links == 1 {
					checkAutolink(t, source, n, start, "ab:β\x00&amp;\\[]", false)
					pos, err := parser.NewLineIndex(source).Position(n.Span().Start())
					if err != nil || pos.Line != 1 || pos.Column != len(prefix)+4 {
						t.Fatal("original scalar coordinate", pos, err)
					}
				} else {
					checkAutolink(t, source, n, start+len("<ab:β\x00&amp;\\[]> "), "a@b", true)
				}
				if _, err := parser.NewSource([]byte(input)).Text(n.Span()); !errors.Is(err, parser.ErrRange) {
					t.Fatal("foreign source accepted", err)
				}
				p := n.Properties()
				p.Link.Label.Segments[0].Semantic = parser.Some("mutated")
				p.Link.Destination.Segments[0].Semantic = parser.Some("mutated")
				p.Link.Resolution = parser.Unavailable
				n.Markers()[0].Role = "mutated"
				n.Content()[0].Semantic = parser.Some("mutated")
				child := n.Children()[0]
				child.Content()[0].Semantic = parser.Some("mutated")
				*child = parser.Node{}
				*n = parser.Node{}
				return true
			})
			if links != 2 || string(source.Bytes()) != input || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
				t.Fatal("ownership/repeatability", input, links)
			}
		}
		// Both heading styles and lazy/explicit continuations retain original offsets.
		for _, input := range []string{"<ab:x>" + ending + "===", "> before" + ending + "<ab:x>", "> before" + ending + "> <ab:x>", "> - before" + ending + ">   <ab:x>"} {
			r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
			checkStagedTextOutcome(t, r, true)
			count := 0
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() == parser.Link {
					count++
					checkAutolink(t, r.Source, n, strings.Index(input, "<ab:x>"), "ab:x", false)
				}
				return true
			})
			if count != 1 {
				t.Fatal("missing mapped link", input)
			}
		}
	}
	for _, input := range []string{`\<ab:x>`, "&lt;ab:x>", "&#60;ab:x>", "`<ab:x>`", "    <ab:x>", "~~~\n<ab:x>\n~~~", "<!-- <ab:x> -->", "<ab:x\n\n>", "<ab:x\n\n# >"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.Link {
				t.Fatal("escaped/opaque/block-crossing angle became link", input)
			}
			return true
		})
	}
	for _, input := range []string{"x <i><ab:x></i>", "*a* <ab:x>", "[a](b) <ab:x>", "x <!-- <ab:x> -->", "x <?pi> <ab:x>", "x <![CDATA[a]]> <ab:x>", "x </tag> <ab:x>"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		if r.Status != parser.Incomplete || conformance.Evaluate(r).Status != "pending" {
			t.Fatal("lost remaining provider", input, r)
		}
	}
	for _, input := range []string{"x <<ab:x>>", "x <invalid<ab:x>>", "x <ab:unterminated", "x <ab:x>\n\nnext", "x <ab:x><a@b>"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.Link {
				checkAutolinkProvenance(t, r.Source, n)
			}
			return true
		})
	}
}

func TestAutolinkMixedOrdinaryDecoding(t *testing.T) {
	input := "é&amp;<ab:&amp;\\[]> &amp;β"
	r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
	checkStagedTextOutcome(t, r, true)
	nodes := r.Document.Root().Children()[0].Children()
	if len(nodes) != 3 {
		t.Fatal("mixed inline structure", nodes)
	}
	checkInlineNodes(t, r.Source, nodes[:1], []inlineWant{textWant(0, 7, "é&")})
	checkAutolink(t, r.Source, nodes[1], 7, `ab:&amp;\[]`, false)
	start := 7 + len(`<ab:&amp;\[]>`)
	checkInlineNodes(t, r.Source, nodes[2:], []inlineWant{textWant(start, len(input), " &β")})
}

func TestAutolinkActualRendererEscaping(t *testing.T) {
	for _, tc := range []struct{ input, html string }{
		{`<ab:é%20?x=&amp;"'>`, "<p><a href=\"ab:%C3%A9%20?x=&amp;amp;%22'\">ab:é%20?x=&amp;amp;&quot;'</a></p>\n"},
		{`<a&b@c>`, "<p><a href=\"mailto:a&amp;b@c\">a&amp;b@c</a></p>\n"},
	} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(tc.input)))
		out := conformance.Evaluate(r)
		if out.Status != "rendered" || out.HTML != tc.html {
			t.Fatal("escaping boundary", out, tc.html)
		}
	}
}
