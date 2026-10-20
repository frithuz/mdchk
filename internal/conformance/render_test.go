package conformance

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

// These are constructed source-bound fixtures, never claimed parser outputs.
type fixture struct {
	t *testing.T
	s parser.Source
	e parser.Span
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	s := parser.NewSource([]byte("&amp;é<raw>\n"))
	e, err := s.Span(0, s.Len())
	if err != nil {
		t.Fatal(err)
	}
	return fixture{t, s, e}
}

func known(s string) parser.Optional[string] {
	return parser.Optional[string]{Present: true, Value: s}
}
func semantic(s string) parser.Value {
	return parser.Value{Semantic: known(s), Segments: []parser.Segment{{Transformation: parser.MappingUnavailable}}}
}
func (f fixture) node(kind parser.Kind, properties parser.Properties, children ...*parser.Node) *parser.Node {
	f.t.Helper()
	spec := parser.NodeSpec{Kind: kind, Envelope: f.e, Properties: properties, Children: children}
	if properties.Literal.Present {
		spec.Content = []parser.Segment{{Transformation: parser.MappingUnavailable}}
	}
	n, err := parser.NewNode(spec)
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}
func (f fixture) text(s string) *parser.Node {
	return f.node(parser.Text, parser.Properties{Literal: known(s)})
}
func (f fixture) doc(children ...*parser.Node) *parser.Document {
	f.t.Helper()
	d, err := parser.NewDocument(f.node(parser.DocumentKind, parser.Properties{}, children...), parser.ReferenceCollection{Availability: parser.Available, LookupAvailability: parser.Available})
	if err != nil {
		f.t.Fatal(err)
	}
	return d
}
func (f fixture) check(want string, children ...*parser.Node) {
	f.t.Helper()
	d := f.doc(children...)
	before := string(d.Source().Bytes())
	for range 2 {
		got, err := RenderDocument(d)
		if err != nil || got != want {
			f.t.Fatalf("render = %q, %v; want %q", got, err, want)
		}
	}
	if string(d.Source().Bytes()) != before {
		f.t.Fatal("render changed source")
	}
}

func TestConstructedCoreHTML(t *testing.T) {
	f := newFixture(t)
	f.check("<h2>é &amp; &lt; &gt; &quot; '</h2>\n", f.node(parser.Heading, parser.Properties{Heading: &parser.HeadingProperties{Level: 2, Style: "atx"}}, f.text("é & < > \" '")))
	f.check("<p><em>a</em><strong>b</strong><code>&amp; &lt;</code>\n<br />\n</p>\n",
		f.node(parser.Paragraph, parser.Properties{},
			f.node(parser.Emphasis, parser.Properties{}, f.text("a")),
			f.node(parser.Strong, parser.Properties{}, f.text("b")),
			f.node(parser.CodeSpan, parser.Properties{Literal: known("& <")}),
			f.node(parser.SoftBreak, parser.Properties{}), f.node(parser.HardBreak, parser.Properties{})))
	f.check("<blockquote>\n<hr />\n<p></p>\n</blockquote>\n", f.node(parser.BlockQuote, parser.Properties{},
		f.node(parser.ThematicBreak, parser.Properties{}), f.node(parser.Paragraph, parser.Properties{}, f.text(""))))
	info := semantic(" go&\"\tignored")
	f.check("<pre><code class=\"language-go&amp;&quot;\">&lt;x&gt;\n</code></pre>\n",
		f.node(parser.CodeBlock, parser.Properties{Code: &parser.CodeProperties{Style: "fenced", Info: &info}, Literal: known("<x>\n")}))
	f.check("<pre><code></code></pre>\n", f.node(parser.CodeBlock, parser.Properties{Code: &parser.CodeProperties{Style: "indented"}, Literal: known("")}))
}

func TestConstructedLinksImagesAndLists(t *testing.T) {
	f := newFixture(t)
	title := semantic("a \"title\" &")
	link := parser.Properties{Link: &parser.LinkProperties{Origin: "inline", Resolution: parser.Available, Destination: semantic("/f ä%20?x=1&y=\"<"), Title: &title}}
	f.check("<p><a href=\"/f%20%C3%A4%20?x=1&amp;y=%22%3C\" title=\"a &quot;title&quot; &amp;\">link</a></p>\n",
		f.node(parser.Paragraph, parser.Properties{}, f.node(parser.Link, link, f.text("link"))))
	image := parser.Properties{Link: &parser.LinkProperties{Origin: "inline", Resolution: parser.Available, Destination: semantic("url")}}
	f.check("<img src=\"url\" alt=\"é &amp;code\n\" />", f.node(parser.Image, image,
		f.node(parser.Emphasis, parser.Properties{}, f.text("é &")),
		f.node(parser.CodeSpan, parser.Properties{Literal: known("code")}), f.node(parser.HardBreak, parser.Properties{})))
	paragraph := f.node(parser.Paragraph, parser.Properties{}, f.text("a"))
	item := f.node(parser.ListItem, parser.Properties{}, paragraph)
	for _, tight := range []bool{false, true} {
		props := parser.Properties{List: &parser.ListProperties{Ordered: true, Start: parser.Optional[int]{Present: true, Value: 3}, Tight: parser.Optional[bool]{Present: true, Value: tight}}}
		want := "<ol start=\"3\">\n<li>\n<p>a</p>\n</li>\n</ol>\n"
		if tight {
			want = "<ol start=\"3\">\n<li>a</li>\n</ol>\n"
		}
		f.check(want, f.node(parser.List, props, item))
	}
	bullet := parser.Properties{List: &parser.ListProperties{Tight: parser.Optional[bool]{Present: true, Value: true}}}
	f.check("<ul>\n<li>a\n<ul>\n<li>a</li>\n</ul>\n</li>\n</ul>\n",
		f.node(parser.List, bullet, f.node(parser.ListItem, parser.Properties{}, paragraph, f.node(parser.List, bullet, item))))
}

func TestConstructedMixedListTightnessAndQuotes(t *testing.T) {
	f := newFixture(t)
	paragraph := func(s string) *parser.Node { return f.node(parser.Paragraph, parser.Properties{}, f.text(s)) }
	list := func(tight bool, children ...*parser.Node) *parser.Node {
		return f.node(parser.List, parser.Properties{List: &parser.ListProperties{Tight: parser.Some(tight)}},
			f.node(parser.ListItem, parser.Properties{}, children...))
	}
	quote := f.node(parser.BlockQuote, parser.Properties{}, paragraph("q"))
	for _, tt := range []struct {
		outer, inner bool
		want         string
	}{
		{true, false, "<ul>\n<li>a\n<ul>\n<li>\n<p>b</p>\n</li>\n</ul>\n<blockquote>\n<p>q</p>\n</blockquote>\n</li>\n</ul>\n"},
		{false, true, "<ul>\n<li>\n<p>a</p>\n<ul>\n<li>b</li>\n</ul>\n<blockquote>\n<p>q</p>\n</blockquote>\n</li>\n</ul>\n"},
	} {
		n := list(tt.outer, paragraph("a"), list(tt.inner, paragraph("b")), quote)
		before := 0
		parser.Walk(n, func(n *parser.Node) bool {
			if n.Kind() == parser.Paragraph {
				before++
			}
			return true
		})
		f.check(tt.want, n)
		after := 0
		parser.Walk(n, func(n *parser.Node) bool {
			if n.Kind() == parser.Paragraph {
				after++
			}
			return true
		})
		if before != 3 || after != before {
			t.Fatal("rendering removed paragraphs")
		}
	}
}

func TestOriginalSegmentsAndSemanticValues(t *testing.T) {
	s := parser.NewSource([]byte("&amp; <i>skip</i>"))
	span := func(a, b int) parser.Span {
		v, err := s.Span(a, b)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	f := fixture{t, s, span(0, s.Len())}
	text, err := parser.NewNode(parser.NodeSpec{Kind: parser.Text, Envelope: span(0, 5), Properties: parser.Properties{Literal: known("&")}, Content: []parser.Segment{{Source: span(0, 5), Transformation: parser.Entity, Semantic: known("&")}}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := parser.NewNode(parser.NodeSpec{Kind: parser.InlineHTML, Envelope: span(6, 17), Content: []parser.Segment{{Source: span(6, 9), Transformation: parser.Exact}, {Source: span(13, 17), Transformation: parser.Exact}}})
	if err != nil {
		t.Fatal(err)
	}
	f.check("&amp;<i></i>", text, raw) // Neither envelope bytes nor entity spelling become semantic text.
	rawBlock, err := parser.NewNode(parser.NodeSpec{Kind: parser.HTMLBlock, Envelope: raw.Span(), Content: raw.Content()})
	if err != nil {
		t.Fatal(err)
	}
	f.check("<i></i>\n", rawBlock)
}

func TestMissingSemanticsRefusePartialOutput(t *testing.T) {
	f := newFixture(t)
	for _, empty := range []*parser.Document{nil, new(parser.Document)} {
		if output, err := RenderDocument(empty); err == nil || output != "" {
			t.Fatal("missing document/root rendered successfully", output, err)
		}
	}
	unknown := parser.Value{}
	for _, bad := range []*parser.Node{
		f.node(parser.Text, parser.Properties{}),
		f.node(parser.Heading, parser.Properties{}, f.text("x")),
		f.node(parser.List, parser.Properties{List: &parser.ListProperties{}}),
		f.node(parser.CodeSpan, parser.Properties{}),
		f.node(parser.HTMLBlock, parser.Properties{}),
		f.node(parser.Link, parser.Properties{Link: &parser.LinkProperties{Origin: "inline", Resolution: parser.Unavailable}}),
		f.node(parser.Image, parser.Properties{Link: &parser.LinkProperties{Origin: "inline", Resolution: parser.Available, Destination: unknown}}),
		f.node(parser.CodeBlock, parser.Properties{Literal: known("x"), Code: &parser.CodeProperties{Style: "fenced", Info: &unknown}}),
	} {
		if got, err := RenderDocument(f.doc(f.text("already buffered"), bad)); err == nil || got != "" {
			t.Errorf("%s: exposed %q with error %v", bad.Kind(), got, err)
		}
	}
}

func TestRawHTMLNULNormalizationPreservesOriginalSource(t *testing.T) {
	const original = "<i title=\"a\x00b�&amp;\\*\">"
	const want = "<i title=\"a�b�&amp;\\*\">"
	s := parser.NewSource([]byte(original))
	e, err := s.Span(0, s.Len())
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := parser.NewLineIndex(s).SemanticText(e)
	if err != nil {
		t.Fatal(err)
	}
	// Independent original-byte facts distinguish literal U+FFFD from NUL's
	// normalized one-byte source. Neither semantic length can determine spans.
	if len(normalized.Segments) != 3 || normalized.Segments[1].Transformation != parser.Normalized || normalized.Segments[1].Source.Start() != 11 || normalized.Segments[1].Source.End() != 12 || normalized.Segments[1].Semantic != known("�") {
		t.Fatal("unexpected scanner NUL provenance", normalized)
	}
	literalRange, err := s.Span(13, 16)
	if err != nil {
		t.Fatal(err)
	}
	if text, err := s.Text(literalRange); err != nil || text != "�" {
		t.Fatal("literal replacement character lost its source identity", text, err)
	}
	for _, kind := range []parser.Kind{parser.InlineHTML, parser.HTMLBlock} {
		for _, segments := range [][]parser.Segment{
			{{Source: e, Transformation: parser.Exact}},
			normalized.Segments,
		} {
			t.Run(string(kind)+"/"+string(segments[0].Transformation)+"/"+string(rune(len(segments)+'0')), func(t *testing.T) {
				n, err := parser.NewNode(parser.NodeSpec{Kind: kind, Envelope: e, Content: segments})
				if err != nil {
					t.Fatal(err)
				}
				expected := want
				if kind == parser.HTMLBlock {
					expected += "\n"
				}
				f := fixture{t, s, e}
				f.check(expected, n) // Also checks repeatability and source bytes.
				if string(s.Bytes()) != original || s.Bytes()[11] != 0 || !reflect.DeepEqual(n.Content(), segments) {
					t.Fatal("source bytes or source segment changed")
				}
			})
		}
	}
}

func TestRawHTMLRefusesUnknownTransformations(t *testing.T) {
	f := newFixture(t)
	for _, transformation := range []parser.Transformation{parser.Normalized, parser.Entity, parser.Escape, parser.PartialTab, parser.Synthetic, parser.MappingUnavailable} {
		for _, semantic := range []parser.Optional[string]{{}, known("unrelated rewrite")} {
			for _, kind := range []parser.Kind{parser.InlineHTML, parser.HTMLBlock} {
				n, err := parser.NewNode(parser.NodeSpec{Kind: kind, Envelope: f.e, Content: []parser.Segment{{Source: f.e, Transformation: transformation, Semantic: semantic}}})
				if err != nil {
					t.Fatal(err)
				}
				if output, err := RenderDocument(f.doc(f.text("buffered"), n)); err == nil || output != "" {
					t.Errorf("%s/%s exposed %q, error %v", kind, transformation, output, err)
				}
			}
		}
	}
}

func TestActualParseOutcomeBoundary(t *testing.T) {
	for _, tt := range []struct {
		input, status, reason, html string
	}{
		{"", "rendered", "", ""}, {"[foo]: /url\n", "rendered", "", ""},
		{" \t\r\n", "rendered", "", ""}, {"\xff", "error", "invalid-utf8", ""},
		{"hello\n", "rendered", "", "<p>hello</p>\n"},
		{"aaa     \nbbb     \n", "rendered", "", "<p>aaa<br />\nbbb</p>\n"},
		{"*aaa*", "rendered", "", "<p><em>aaa</em></p>\n"},
		{"***aaa***", "rendered", "", "<p><em><strong>aaa</strong></em></p>\n"},
		{"[aaa](b)", "pending", "syntax-unimplemented", ""},
	} {
		got := Evaluate(parser.Parse(context.Background(), parser.NewSource([]byte(tt.input))))
		if got.Status != tt.status || got.Reason != tt.reason || got.HTML != tt.html {
			t.Fatalf("input %q: %+v", tt.input, got)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	deadline, stop := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer stop()
	for _, tt := range []struct {
		ctx    context.Context
		reason string
	}{{cancelled, "cancelled"}, {deadline, "deadline-exceeded"}} {
		r := Evaluate(parser.Parse(tt.ctx, parser.NewSource([]byte("x"))))
		if r.Status != "interrupted" || r.Reason != tt.reason || r.HTML != "" {
			t.Fatal(r)
		}
	}
	if r := Evaluate(parser.Parse(cancelled, parser.NewSource([]byte("\xff")))); r.Status != "error" || r.Reason != "invalid-utf8" {
		t.Fatal("encoding failure was hidden by interruption", r)
	}
	if r := Evaluate(parser.ParseResult{Status: parser.Complete, AST: parser.Available}); r.Status != "error" {
		t.Fatal("nil tree accepted as complete", r)
	}
}

func TestRawHTMLRejectsInvalidResidualTabs(t *testing.T) {
	for _, tt := range []struct {
		input  string
		offset int
		spaces parser.Optional[string]
		kind   parser.Kind
	}{
		{"\t<div>", 0, known("    "), parser.HTMLBlock},
		{"\t<div>", 0, known("x"), parser.HTMLBlock},
		{"\t<div>", 0, parser.Optional[string]{}, parser.HTMLBlock},
		{"x\t<div>", 1, known("  "), parser.HTMLBlock},
		{"\t<div>", 0, known("  "), parser.InlineHTML},
	} {
		source := parser.NewSource([]byte(tt.input))
		envelope, _ := source.Span(0, source.Len())
		tab, _ := source.Span(tt.offset, tt.offset+1)
		tail, _ := source.Span(tt.offset+1, source.Len())
		var content []parser.Segment
		if tt.offset != 0 {
			prefix, _ := source.Span(0, tt.offset)
			content = append(content, parser.Segment{Source: prefix, Transformation: parser.Exact})
		}
		content = append(content, parser.Segment{Source: tab, Transformation: parser.PartialTab, Semantic: tt.spaces}, parser.Segment{Source: tail, Transformation: parser.Exact})
		html, err := parser.NewNode(parser.NodeSpec{Kind: tt.kind, Envelope: envelope, Content: content})
		if err != nil {
			t.Fatal(err)
		}
		root, err := parser.NewNode(parser.NodeSpec{Kind: parser.DocumentKind, Envelope: envelope, Children: []*parser.Node{html}})
		if err != nil {
			t.Fatal(err)
		}
		doc, err := parser.NewDocument(root, parser.ReferenceCollection{Availability: parser.Available, LookupAvailability: parser.Available})
		if err != nil {
			t.Fatal(err)
		}
		if output, err := RenderDocument(doc); err == nil || output != "" {
			t.Fatal("invalid residual tab emitted HTML", tt, output, err)
		}
	}
}
