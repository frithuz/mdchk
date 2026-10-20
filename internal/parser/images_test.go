package parser_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

func checkImageMarker(t *testing.T, source parser.Source, n *parser.Node) {
	t.Helper()
	marks := n.Markers()
	if len(marks) < 3 || marks[0].Role != "image-marker" || marks[0].Source.Start() != n.Span().Start() || marks[0].Source.End() != n.Span().Start()+1 || marks[1].Role != "label-open" || marks[1].Source.Start() != n.Span().Start()+1 {
		t.Fatal("image envelope/marker", n.Span(), marks)
	}
	if raw, err := source.Text(marks[0].Source); err != nil || raw != "!" {
		t.Fatal("image marker source", raw, err)
	}
}

// These literal fixtures identify every unchanged Images case plus image-owned
// Links cases independently of the coverage ledger and production recognizer.
func TestImageUpstreamAcceptance(t *testing.T) {
	type want struct {
		start                                  int
		raw, origin, label, destination, title string
		hasTitle                               bool
	}
	wants := map[int][]want{
		517: {{1, "![moon](moon.jpg)", "inline", "moon", "moon.jpg", "", false}},
		520: {{0, "![[[foo](uri1)](uri2)](uri3)", "inline", "[[foo](uri1)](uri2)", "uri3", "", false}},
		531: {{1, "![moon](moon.jpg)", "inline", "moon", "moon.jpg", "", false}},
		572: {{0, "![foo](/url \"title\")", "inline", "foo", "/url", "title", true}},
		573: {{0, "![foo *bar*]", "shortcut", "foo *bar*", "train.jpg", "train & tracks", true}},
		574: {{0, "![foo ![bar](/url)](/url2)", "inline", "foo ![bar](/url)", "/url2", "", false}, {6, "![bar](/url)", "inline", "bar", "/url", "", false}},
		575: {{0, "![foo [bar](/url)](/url2)", "inline", "foo [bar](/url)", "/url2", "", false}},
		576: {{0, "![foo *bar*][]", "collapsed", "foo *bar*", "train.jpg", "train & tracks", true}},
		577: {{0, "![foo *bar*][foobar]", "full", "foo *bar*", "train.jpg", "train & tracks", true}},
		578: {{0, "![foo](train.jpg)", "inline", "foo", "train.jpg", "", false}},
		579: {{3, "![foo bar](/path/to/train.jpg  \"title\"   )", "inline", "foo bar", "/path/to/train.jpg", "title", true}},
		580: {{0, "![foo](<url>)", "inline", "foo", "url", "", false}},
		581: {{0, "![](/url)", "inline", "", "/url", "", false}},
		582: {{0, "![foo][bar]", "full", "foo", "/url", "", false}},
		583: {{0, "![foo][bar]", "full", "foo", "/url", "", false}},
		584: {{0, "![foo][]", "collapsed", "foo", "/url", "title", true}},
		585: {{0, "![*foo* bar][]", "collapsed", "*foo* bar", "/url", "title", true}},
		586: {{0, "![Foo][]", "collapsed", "Foo", "/url", "title", true}},
		587: {{0, "![foo]", "shortcut", "foo", "/url", "title", true}},
		588: {{0, "![foo]", "shortcut", "foo", "/url", "title", true}},
		589: {{0, "![*foo* bar]", "shortcut", "*foo* bar", "/url", "title", true}},
		590: {},
		591: {{0, "![Foo]", "shortcut", "Foo", "/url", "title", true}},
		592: {}, 593: {},
	}
	inlineWants := map[int][]inlineLinkWant{
		517: {{label: "moon", destination: "moon.jpg"}},
		520: {{label: "[[foo](uri1)](uri2)", destination: "uri3"}},
		531: {{label: "moon", destination: "moon.jpg"}},
		572: {{label: "foo", destination: "/url", between: " ", title: &inlineTitleWant{'"', "title"}}},
		574: {{label: "foo ![bar](/url)", destination: "/url2"}, {label: "bar", destination: "/url"}},
		575: {{label: "foo [bar](/url)", destination: "/url2"}},
		578: {{label: "foo", destination: "train.jpg"}},
		579: {{label: "foo bar", destination: "/path/to/train.jpg", between: "  ", title: &inlineTitleWant{'"', "title"}, trailing: "   "}},
		580: {{label: "foo", destination: "url", angle: true}},
		581: {{destination: "/url"}},
	}
	cases, err := emphasisExamples()
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range cases {
		expected, ok := wants[e.ID]
		if !ok {
			continue
		}
		seen++
		t.Run(fmt.Sprint(e.ID), func(t *testing.T) {
			r := parser.Parse(context.Background(), parser.NewSource([]byte(e.Markdown)))
			checkStagedTextOutcome(t, r, true)
			if out := conformance.Evaluate(r); out.Status != "rendered" || out.HTML != e.HTML {
				t.Fatal(out, e.HTML)
			}
			var images []*parser.Node
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() == parser.Image {
					images = append(images, n)
					checkLinkProvenance(t, r.Source, n)
				}
				if n.Kind() == parser.Link {
					checkLinkProvenance(t, r.Source, n)
				}
				if n.Kind() == parser.Emphasis || n.Kind() == parser.Strong {
					checkEmphasisProvenance(t, r.Source, n)
				}
				return true
			})
			if len(images) != len(expected) {
				t.Fatal("independent Image count", len(images), len(expected))
			}
			for i, w := range expected {
				n := images[i]
				p := n.Properties().Link
				raw, _ := r.Source.Text(n.Span())
				if n.Span().Start() != w.start || n.Span().End() != w.start+len(w.raw) || raw != w.raw || p.Origin != w.origin || p.Label.Semantic != parser.Some(w.label) || p.Destination.Semantic != parser.Some(w.destination) || (p.Title != nil) != w.hasTitle {
					t.Fatal("independent image components", n, p, w)
				}
				if w.hasTitle && p.Title.Semantic != parser.Some(w.title) {
					t.Fatal("independent title", p.Title, w)
				}
				marks := n.Markers()
				if marks[1].Source.Start() != w.start+1 || marks[2].Source.Start() != w.start+2+len(w.label) {
					t.Fatal("independent label markers", marks, w)
				}
				if w.origin == "inline" {
					checkInlineLinkFixture(t, r.Source, n, e.Markdown, inlineWants[e.ID][i])
				} else {
					key := w.label
					if e.ID == 577 {
						key = "foobar"
					}
					if e.ID == 582 || e.ID == 583 {
						key = "bar"
					}
					if p.NormalizedLabel != parser.Some(referenceKeyOracle(t, key)) {
						t.Fatal("independent image key", p, key)
					}
					if w.origin != "shortcut" {
						rawKey := key
						if w.origin == "collapsed" {
							rawKey = ""
						}
						if p.ReferenceLabel == nil || p.ReferenceLabel.Semantic != parser.Some(rawKey) {
							t.Fatal("independent identifier", p)
						}
					}
				}
			}
			shapeWants := map[int][]parser.Kind{
				517: {parser.Link, parser.Image, parser.Text},
				520: {parser.Image, parser.Text, parser.Link, parser.Text, parser.Text},
				531: {parser.Link, parser.Image, parser.Text},
				573: {parser.Image, parser.Text, parser.Emphasis, parser.Text},
				574: {parser.Image, parser.Text, parser.Image, parser.Text},
				575: {parser.Image, parser.Text, parser.Link, parser.Text},
			}
			if want := shapeWants[e.ID]; want != nil {
				var shape []parser.Kind
				for _, n := range r.Document.Root().Children()[0].Children() {
					parser.Walk(n, func(n *parser.Node) bool { shape = append(shape, n.Kind()); return true })
				}
				if !reflect.DeepEqual(shape, want) {
					t.Fatal("independent nested AST", shape, want)
				}
			}
		})
	}
	if seen != 25 {
		t.Fatal("missing acceptance cases", seen)
	}
}

func TestImageSemanticAltAndActivation(t *testing.T) {
	for _, tt := range []struct {
		input, html   string
		images, links int
	}{
		{"![*a* **b** `&amp;` &amp; <i>c</i> ![d](x) [e](y) <a@b>](z)", "<p><img src=\"z\" alt=\"a b &amp;amp; &amp; c d e a@b\" /></p>\n", 2, 2},
		{"[![a](b)](c)", "<p><a href=\"c\"><img src=\"b\" alt=\"a\" /></a></p>\n", 1, 1},
		{"![a [b](c) ![d](e)](f)", "<p><img src=\"f\" alt=\"a b d\" /></p>\n", 2, 1},
		{"![a <ab:x>](b)", "<p><img src=\"b\" alt=\"a ab:x\" /></p>\n", 1, 1},
		{"![a](<> '') ![]()", "<p><img src=\"\" alt=\"a\" title=\"\" /> <img src=\"\" alt=\"\" /></p>\n", 2, 0},
		{"![a][missing]\n\n[a]: /a", "<p>![a][missing]</p>\n", 0, 0},
		{"![a] [a][] [a]\n\n[a]: /a", "<p><img src=\"/a\" alt=\"a\" /> <a href=\"/a\">a</a> <a href=\"/a\">a</a></p>\n", 1, 2},
		{"![ẞ][SS] ![ẞ][] ![ẞ]\n\n[ss]: /first \"\"\n[ẞ]: /second", "<p><img src=\"/first\" alt=\"ẞ\" title=\"\" /> <img src=\"/first\" alt=\"ẞ\" title=\"\" /> <img src=\"/first\" alt=\"ẞ\" title=\"\" /></p>\n", 3, 0},
		{`\![a](b) !\[a](b) ![a](bad title)`, "<p>!<a href=\"b\">a</a> ![a](b) ![a](bad title)</p>\n", 0, 1},
		{"![`![a](b)` &excl;[a](b)](c)", "<p><img src=\"c\" alt=\"![a](b) !a\" /></p>\n", 1, 1},
	} {
		t.Run(tt.input, func(t *testing.T) {
			r := parser.Parse(context.Background(), parser.NewSource([]byte(tt.input)))
			checkStagedTextOutcome(t, r, true)
			out := conformance.Evaluate(r)
			if out.HTML != tt.html || out.Status != "rendered" {
				t.Fatal(out, tt.html)
			}
			images, links := 0, 0
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() == parser.Image {
					images++
					checkLinkProvenance(t, r.Source, n)
				}
				if n.Kind() == parser.Link {
					links++
					checkLinkProvenance(t, r.Source, n)
				}
				return true
			})
			if images != tt.images || links != tt.links {
				t.Fatal("node counts", images, links, tt)
			}
		})
	}
}

func TestImageMappedSourceAndOwnership(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, terminal := range []string{"", ending} {
			for _, input := range []string{
				"> - ![é\x00 *β*" + ending + ">   tail](</a&amp;b> \"ti\\\"tle\")" + terminal,
				"# ![é\x00](b) #" + terminal,
				"![é\x00](b)" + ending + "===" + terminal,
				"> ![é" + ending + "lazy](b)" + terminal,
				">\t ![a" + ending + ">\t b][K]" + ending + ">" + ending + "> [k]: /uri \"title\"" + terminal,
				"![a  " + ending + "b\\" + ending + "c" + ending + "d](b)" + terminal,
			} {
				r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
				checkStagedTextOutcome(t, r, true)
				count := 0
				parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
					switch n.Kind() {
					case parser.Image:
						count++
						checkLinkProvenance(t, r.Source, n)
						before := n.Properties()
						p := n.Properties()
						p.Link.Label.Segments[0].Semantic = parser.Some("changed")
						marks := n.Markers()
						marks[0].Role = "changed"
						children := n.Children()
						if len(children) > 0 {
							children[0] = nil
						}
						if !reflect.DeepEqual(before, n.Properties()) || n.Markers()[0].Role != "image-marker" {
							t.Fatal("mutable image accessors")
						}
					case parser.Emphasis, parser.Strong:
						checkEmphasisProvenance(t, r.Source, n)
					case parser.SoftBreak, parser.HardBreak:
						checkBreakProperties(t, r.Source, n, n.Properties().Break.Origin, byteRange{n.Span().Start(), n.Content()[0].Source.Start()})
					}
					return true
				})
				if count != 1 {
					t.Fatal("missing mapped image", input, count)
				}
				if !reflect.DeepEqual(r, parser.Parse(context.Background(), r.Source)) || string(r.Source.Bytes()) != input {
					t.Fatal("image state/source mutation")
				}
				if strings.Contains(input, "![a  ") {
					if got := conformance.Evaluate(r).HTML; got != "<p><img src=\"b\" alt=\"a\nb\nc\nd\" /></p>\n" {
						t.Fatal("semantic break alt", got)
					}
				}
			}
		}
	}
}

func TestImageOpaqueAndGeneratedMarkers(t *testing.T) {
	for _, input := range []string{"`![a](b)`", "    ![a](b)", "~~~\n![a](b)\n~~~", "<!-- ![a](b) -->", "x <i title='![a](b)'>", "&excl;\\[a](b)", "!&#91;a](b)"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.Image {
				t.Fatal("opaque/generated image", input, n)
			}
			return true
		})
	}
}
