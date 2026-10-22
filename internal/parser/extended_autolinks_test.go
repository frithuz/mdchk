package parser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type bareWant struct {
	a, b                       int
	label, destination, prefix string
}

func bareNodes(n *Node) []*Node {
	var out []*Node
	Walk(n, func(n *Node) bool {
		if n.Kind() == Link && n.Properties().Link.Origin == "bare-gfm" {
			out = append(out, n)
		}
		return true
	})
	return out
}
func assertBare(t *testing.T, n *Node, w bareWant) {
	t.Helper()
	assertTaskSpan(t, n.Span(), w.a, w.b)
	p := n.Properties().Link
	if p == nil || p.Origin != "bare-gfm" || p.Label.Semantic != Some(w.label) || p.Destination.Semantic != Some(w.destination) || p.Resolution != Available || p.Title != nil || p.ReferenceLabel != nil || p.NormalizedLabel.Present || len(n.Markers()) != 0 {
		t.Fatalf("bare facts %#v want %#v", p, w)
	}
	if tableText(n) != w.label || len(n.Children()) != 1 || n.Children()[0].Kind() != Text {
		t.Fatal("visible child", n.spec)
	}
	seg := p.Destination.Segments
	if w.prefix != "" {
		if len(seg) < 2 || seg[0].Source.Present() || seg[0].Transformation != Synthetic || seg[0].Semantic != Some(w.prefix) {
			t.Fatal("prefix provenance", seg)
		}
		seg = seg[1:]
	}
	if !reflect.DeepEqual(seg, n.Content()) || !reflect.DeepEqual(p.Label.Segments, n.Content()) {
		t.Fatal("source contribution mismatch")
	}
	for _, s := range seg {
		if !s.Source.Present() || s.Source.source != n.Span().source {
			t.Fatal("source ownership")
		}
	}
}
func assertBareCore(t *testing.T, input string) {
	t.Helper()
	source := NewSource([]byte(input))
	before := Parse(context.Background(), source)
	requireTableComplete(t, before, input)
	stage := parseWithProfile(context.Background(), source, budgetFor(t, DefaultLimits()), gfmInProgressProfile)
	requireTableComplete(t, stage, input)
	for _, r := range []ParseResult{Parse(context.Background(), source), ParseWithBudget(context.Background(), source, budgetFor(t, DefaultLimits())), parseWithProfile(context.Background(), source, budgetFor(t, DefaultLimits()), coreProfile)} {
		requireTableComplete(t, r, input)
		if len(bareNodes(r.Document.root)) != 0 || !reflect.DeepEqual(before.Document.root, r.Document.root) {
			t.Fatal("core isolation")
		}
	}
}
func TestBarePublished622To635(t *testing.T) {
	cases := map[int][]bareWant{
		622: {{0, 18, "www.commonmark.org", "http://www.commonmark.org", "http://"}},
		623: {{6, 29, "www.commonmark.org/help", "http://www.commonmark.org/help", "http://"}},
		624: {{6, 24, "www.commonmark.org", "http://www.commonmark.org", "http://"}, {33, 55, "www.commonmark.org/a.b", "http://www.commonmark.org/a.b", "http://"}},
		625: {{0, 41, "www.google.com/search?q=Markup+(business)", "http://www.google.com/search?q=Markup+(business)", "http://"}, {43, 84, "www.google.com/search?q=Markup+(business)", "http://www.google.com/search?q=Markup+(business)", "http://"}, {89, 130, "www.google.com/search?q=Markup+(business)", "http://www.google.com/search?q=Markup+(business)", "http://"}, {134, 175, "www.google.com/search?q=Markup+(business)", "http://www.google.com/search?q=Markup+(business)", "http://"}},
		626: {{0, 38, "www.google.com/search?q=(business))+ok", "http://www.google.com/search?q=(business))+ok", "http://"}},
		627: {{0, 40, "www.google.com/search?q=commonmark&hl=en", "http://www.google.com/search?q=commonmark&hl=en", "http://"}, {42, 76, "www.google.com/search?q=commonmark", "http://www.google.com/search?q=commonmark", "http://"}},
		628: {{0, 21, "www.commonmark.org/he", "http://www.commonmark.org/he", "http://"}},
		629: {{0, 21, "http://commonmark.org", "http://commonmark.org", ""}, {30, 85, "https://encrypted.google.com/search?q=Markup+(business)", "https://encrypted.google.com/search?q=Markup+(business)", ""}},
		630: {{0, 11, "foo@bar.baz", "mailto:foo@bar.baz", "mailto:"}},
		631: {{40, 62, "hello+xyz@mail.example", "mailto:hello+xyz@mail.example", "mailto:"}},
		632: {{0, 11, "a.b-c_d@a.b", "mailto:a.b-c_d@a.b", "mailto:"}, {13, 24, "a.b-c_d@a.b", "mailto:a.b-c_d@a.b", "mailto:"}},
		633: {{0, 18, "mailto:foo@bar.baz", "mailto:foo@bar.baz", ""}, {20, 38, "mailto:a.b-c_d@a.b", "mailto:a.b-c_d@a.b", ""}, {40, 58, "mailto:a.b-c_d@a.b", "mailto:a.b-c_d@a.b", ""}, {61, 79, "mailto:a.b-c_d@a.b", "mailto:a.b-c_d@a.b", ""}, {124, 140, "xmpp:foo@bar.baz", "xmpp:foo@bar.baz", ""}, {142, 158, "xmpp:foo@bar.baz", "xmpp:foo@bar.baz", ""}},
		634: {{0, 20, "xmpp:foo@bar.baz/txt", "xmpp:foo@bar.baz/txt", ""}, {22, 46, "xmpp:foo@bar.baz/txt@bin", "xmpp:foo@bar.baz/txt@bin", ""}, {48, 76, "xmpp:foo@bar.baz/txt@bin.com", "xmpp:foo@bar.baz/txt@bin.com", ""}},
		635: {{0, 20, "xmpp:foo@bar.baz/txt", "xmpp:foo@bar.baz/txt", ""}},
	}
	count := 0
	for id := 622; id <= 635; id++ {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			input := originalTaskInput(t, id)
			r := parseTableStage(t, input)
			got := bareNodes(r.Document.root)
			want := cases[id]
			if len(got) != len(want) {
				t.Fatalf("got %d want %d: %s", len(got), len(want), input)
			}
			if gotText := tableText(r.Document.root); gotText != strings.ReplaceAll(input, "\n", "") {
				t.Fatalf("published visible text/remainders %q", gotText)
			}
			for i, w := range want {
				assertBare(t, got[i], w)
				count++
			}
			assertBareCore(t, input)
		})
	}
	if count != 28 {
		t.Fatal("independent 28-link matrix", count)
	}
}
func TestBareIntegratedInteractions(t *testing.T) {
	for _, c := range []struct {
		id      int
		w       bareWant
		visible string
	}{
		{611, bareWant{1, 19, "http://foo.bar/baz", "http://foo.bar/baz", ""}, "<http://foo.bar/baz bim>"},
		{615, bareWant{5, 22, "+@bar.example.com", "mailto:+@bar.example.com", "mailto:"}, "<foo\\+@bar.example.com>"},
		{617, bareWant{2, 16, "http://foo.bar", "http://foo.bar", ""}, "< http://foo.bar >"},
		{620, bareWant{0, 18, "http://example.com", "http://example.com", ""}, "http://example.com"},
		{621, bareWant{0, 19, "foo@bar.example.com", "mailto:foo@bar.example.com", "mailto:"}, "foo@bar.example.com"},
	} {
		t.Run(fmt.Sprint(c.id), func(t *testing.T) {
			input := originalTaskInput(t, c.id)
			r := parseTableStage(t, input)
			got := bareNodes(r.Document.root)
			if len(got) != 1 {
				t.Fatal("links", got)
			}
			assertBare(t, got[0], c.w)
			if tableText(r.Document.root) != c.visible {
				t.Fatalf("literal remainder %q", tableText(r.Document.root))
			}
			assertBareCore(t, input)
			if c.id == 615 {
				p := r.Document.root.Children()[0].Children()
				assertTaskSpan(t, p[0].Span(), 0, 5)
				if p[0].Properties().Literal != Some("<foo\\") {
					t.Fatal("raw competing escape prefix")
				}
				core := Parse(context.Background(), r.Source)
				parts := core.Document.root.Children()[0].Children()[0].Content()
				if len(parts) != 3 || parts[1].Transformation != Escape {
					t.Fatal("core escape", parts)
				}
				assertTaskSpan(t, parts[1].Source, 4, 6)
			}
		})
	}
}
func TestBareGrammarAndRemainders(t *testing.T) {
	for _, c := range []struct{ input, label, remainder string }{
		{"www.a.b?!.,:*_~", "www.a.b", "?!.,:*_~"},
		{"www.a.b/a).", "www.a.b/a", ")."},
		{"http://a.b_)", "http://a.b", "_)"},
		{"www.a.b_&x;", "www.a.b", "_&x;"},
		{"www.a.b_)&x;.", "www.a.b", "_)&x;."},
		{"www.a.b_&x;)", "www.a.b", "_&x;)"},
		{"www.a.b/(x)&x;", "www.a.b/(x)", "&x;"},
		{"www.a.b/a(x)&x;)&y;.", "www.a.b/a(x)", "&x;)&y;."},
		{"www.a.b/a&x;&y;.", "www.a.b/a", "&x;&y;."},
		{"www.a.b/a(b)))", "www.a.b/a(b)", "))"},
		{"www.a.b/a)b", "www.a.b/a)b", ""},
		{"www.a.b/a&bogus;", "www.a.b/a", "&bogus;"},
		{"www.a.b/a&;", "www.a.b/a&;", ""},
		{"www.a.b/a&#12;", "www.a.b/a&#12;", ""},
		{"xmpp:a@b.c/", "xmpp:a@b.c", "/"},
		{"xmpp:a@b.c/r.", "xmpp:a@b.c/r.", ""},
		{"xmpp:a@b.c/r/s", "xmpp:a@b.c/r", "/s"},
		{"xmpp:a@b.c/r-abc", "xmpp:a@b.c/r", "-abc"},
		{"mailto:a@b.c/r", "mailto:a@b.c", "/r"},
	} {
		t.Run(c.input, func(t *testing.T) {
			r := parseTableStage(t, c.input)
			links := bareNodes(r.Document.root)
			if len(links) != 1 || links[0].Properties().Link.Label.Semantic != Some(c.label) {
				t.Fatal("link", links)
			}
			if tableText(r.Document.root) != c.label+c.remainder {
				t.Fatal("remainder", tableText(r.Document.root))
			}
			assertTaskSpan(t, links[0].Span(), 0, len(c.label))
			assertBareCore(t, c.input)
		})
	}
	for _, input := range []string{"www.a", "www.", "http://localhost", "http://a..b", "http://a_.b", "http://a.b_c", "http://a_.b_)&x;", "http://a.b_()", "www.a.b_&;", "www.a.b_&#12;", "hello@mail+xyz.example", "a@b.c-", "a@b.c_", "a@b..c", "a@@b.c", "WWW.a.b", "HTTP://a.b", "https:/a.b", "www.é.b", "www.a.é", "xwww.a.b", "x\u00a0www.a.b", "x\u0085www.a.b", "ftp://a.b"} {
		t.Run("invalid/"+input, func(t *testing.T) {
			r := parseTableStage(t, input)
			if len(bareNodes(r.Document.root)) != 0 {
				t.Fatal("invalid candidate accepted", input)
			}
			assertBareCore(t, input)
		})
	}
	for _, input := range []string{"www.a.b", "http://a.b", "https://a.b", "http://a_b.c.d", "www." + strings.Repeat("a", 80) + ".b", "éa@b.c", "x\vwww.a.b", "x\fwww.a.b", "x\twww.a.b"} {
		t.Run("valid/"+input, func(t *testing.T) {
			r := parseTableStage(t, input)
			if len(bareNodes(r.Document.root)) != 1 {
				t.Fatal("valid candidate refused", input)
			}
			assertBareCore(t, input)
		})
	}
	for _, stop := range []string{"\v", "\f", "\t"} {
		r := parseTableStage(t, "www.a.b/p"+stop+"Z")
		links := bareNodes(r.Document.root)
		if len(links) != 1 || links[0].Properties().Link.Destination.Semantic != Some("http://www.a.b/p") {
			t.Fatal("W path stop")
		}
	}
	for _, keep := range []string{"\u00a0", "\u0085", "\x01", "\x7f", "\x00"} {
		input := "www.a.b/p" + keep + "Z"
		r := parseTableStage(t, input)
		links := bareNodes(r.Document.root)
		want := strings.ReplaceAll(input, "\x00", "�")
		if len(links) != 1 || links[0].Properties().Link.Destination.Semantic != Some("http://"+want) {
			t.Fatal("retained path", input, links)
		}
	}
}

func TestBareSharedPriority(t *testing.T) {
	for _, c := range []struct {
		input               string
		bare, links, images int
	}{
		{"[www.a.b](/x) www.c.d", 1, 2, 0},
		{"[http://a.b](/x) [a@b.c](/y)", 0, 2, 0},
		{"[www.a.b][r]\n\n[r]: /first\n[r]: /later\n", 0, 1, 0},
		{"[www.a.b][]\n\n[www.a.b]: /x\n", 0, 1, 0},
		{"[www.a.b]\n\n[www.a.b]: /x\n", 0, 1, 0},
		{"[ www.a.b ][missing] www.c.d", 2, 2, 0},
		{"[ www.a.b ](broken www.c.d", 2, 2, 0},
		{"![ www.a.b ](/image)", 1, 1, 1},
		{"![ www.a.b ][r]\n\n[r]: /image\n", 1, 1, 1},
		{"[![ www.a.b ](/image)](/outer)", 0, 1, 1},
		{"[![ www.a.b ][r]](/outer)\n\n[r]: /image\n", 0, 1, 1},
		{"![a [www.a.b](/inner)](/image)", 0, 1, 1},
		{"![ www.a.b ][missing]", 1, 1, 0},
		{"![![ www.a.b ](/inner)](/outer)", 1, 1, 2},
		{"~~www.a.b~~", 1, 1, 0},
		{"**www.a.b**", 1, 1, 0},
		{"*www.a.b*", 1, 1, 0},
		{"www.a.b/a*b*c", 1, 1, 0},
		{"www.a.b/a~b~c", 1, 1, 0},
		{"www.a.b/a_b_c", 1, 1, 0},
		{"![~~www.a.b~~](/image)", 1, 1, 1},
		{"[![~~www.a.b~~](/image)](/outer)", 0, 1, 1},
	} {
		t.Run(c.input, func(t *testing.T) {
			r := parseTableStage(t, c.input)
			if len(bareNodes(r.Document.root)) != c.bare || len(tableNodes(r.Document.root, Link)) != c.links || len(tableNodes(r.Document.root, Image)) != c.images {
				t.Fatalf("counts bare=%d links=%d images=%d", len(bareNodes(r.Document.root)), len(tableNodes(r.Document.root, Link)), len(tableNodes(r.Document.root, Image)))
			}
			for _, n := range tableNodes(r.Document.root, Link) {
				for _, child := range n.spec.Children {
					if len(tableNodes(child, Link)) != 0 {
						t.Fatal("nested link")
					}
				}
			}
			if c.input == "~~www.a.b~~" || c.input == "![~~www.a.b~~](/image)" {
				strikes := tableNodes(r.Document.root, Strike)
				if len(strikes) != 1 || len(bareNodes(strikes[0])) != 1 {
					t.Fatal("surrounding strike")
				}
			}
			if strings.HasPrefix(c.input, "www.a.b/a") && (len(tableNodes(r.Document.root, Emphasis)) != 0 || len(tableNodes(r.Document.root, Strike)) != 0) {
				t.Fatal("URL interior formatting")
			}
			assertBareCore(t, c.input)
		})
	}
}
func TestBareOpaqueAndGeneratedSyntax(t *testing.T) {
	for _, input := range []string{
		"`www.a.b`", "`` www.a.b ` ``", "    www.a.b\n", "~~~\nwww.a.b\n~~~\n",
		"<a x=\"www.a.b\">", "<!-- www.a.b -->", "<?www.a.b?>", "<![CDATA[www.a.b]]>", "<!ABC www.a.b>",
		"<script>\nwww.a.b\n</script>\n", "> <script>\n> www.a.b\n> </script>\n",
		"[label](http://a.b \"www.c.d\")", "[label][r]\n\n[r]: http://a.b \"www.c.d\"\n",
		"&#119;ww.a.b", "www&#46;a.b", "a&#64;b.c", "http&#58;//a.b", "\\www.a.b", "\\<http://a.b>",
	} {
		t.Run(input, func(t *testing.T) {
			r := parseTableStage(t, input)
			if len(bareNodes(r.Document.root)) != 0 {
				t.Fatal("opaque/generated bare syntax", input)
			}
			assertBareCore(t, input)
		})
	}
	r := parseTableStage(t, "<a>www.a.b</a> www.c.d")
	if len(bareNodes(r.Document.root)) != 1 {
		t.Fatal("raw logical boundary before www remains >; eligible trailing prose")
	}
	r = parseTableStage(t, "<a> www.a.b </a>")
	if len(bareNodes(r.Document.root)) != 1 {
		t.Fatal("ordinary inline-tag prose")
	}
	r = parseTableStage(t, "www.a.b/a\\*b&amp;c")
	links := bareNodes(r.Document.root)
	if len(links) != 1 || links[0].Properties().Link.Label.Semantic != Some("www.a.b/a\\*b&amp;c") || links[0].Properties().Link.Destination.Semantic != Some("http://www.a.b/a\\*b&amp;c") {
		t.Fatal("raw URL payload decoded", links)
	}
}
func TestBareMappedSource(t *testing.T) {
	input := "| www.a.org/a\\|b |\n| - |\n"
	r := parseTableStage(t, input)
	links := bareNodes(r.Document.root)
	if len(links) != 1 {
		t.Fatal("mapped table", links)
	}
	assertBare(t, links[0], bareWant{2, 16, "www.a.org/a|b", "http://www.a.org/a|b", "http://"})
	parts := links[0].Content()
	if len(parts) != 3 || parts[1].Transformation != Escape || parts[1].Semantic != Some("|") {
		t.Fatal("atomic escaped pipe", parts)
	}
	for i, w := range [][2]int{{2, 13}, {13, 15}, {15, 16}} {
		assertTaskSpan(t, parts[i].Source, w[0], w[1])
	}
	for _, c := range []struct {
		input string
		a, b  int
		label string
	}{
		{"> www.a.b/é\r\n", 2, 12, "www.a.b/é"},
		{"- [x] www.a.b\r\n", 6, 13, "www.a.b"},
		{"# www.a.b\n", 2, 9, "www.a.b"},
		{"é\twww.a.b.\n", 3, 10, "www.a.b"},
		{"www.a.b\r", 0, 7, "www.a.b"},
		{"www.a.b", 0, 7, "www.a.b"},
	} {
		r := parseTableStage(t, c.input)
		links := bareNodes(r.Document.root)
		if len(links) != 1 {
			t.Fatal("container source", c.input)
		}
		assertBare(t, links[0], bareWant{c.a, c.b, c.label, "http://" + c.label, "http://"})
		assertBareCore(t, c.input)
	}
	r = parseTableStage(t, "www.a.b/p\x00Z")
	parts = bareNodes(r.Document.root)[0].Content()
	found := false
	for _, p := range parts {
		if p.Transformation == Normalized {
			found = true
			if tableRaw(t, r.Source, p.Source) != "\x00" || p.Semantic != Some("�") {
				t.Fatal("original NUL contribution", p)
			}
		}
	}
	if !found {
		t.Fatal("missing NUL provenance")
	}
	r = parseTableStage(t, "> www.a.b/é\r\n")
	idx := NewLineIndex(r.Source)
	for off, want := range map[int]Position{2: {1, 3}, 10: {1, 11}, 12: {1, 12}, 14: {2, 1}} {
		got, err := idx.Position(off)
		if err != nil || got != want {
			t.Fatal("original scalar position", off, got, err)
		}
	}
	for _, off := range []int{11, 13} {
		if _, err := idx.Position(off); err == nil {
			t.Fatal("UTF8/CRLF interior", off)
		}
	}
	for _, input := range []string{"www.a\n\nb", "| www.a | b |\n| - | - |\n", "- www.a\n- b\n"} {
		r := parseTableStage(t, input)
		if len(bareNodes(r.Document.root)) != 0 {
			t.Fatal("cross-boundary candidate")
		}
	}
}

func TestBareModelOwnershipAndSourceKinds(t *testing.T) {
	input := "https://a.org <https://a.org> [x](https://a.org) [x][r] [r][] [r] www.a.org a@b.c mailto:a@b.c xmpp:a@b.c/r ftp://a.org <ftp://a.org>\n\n[r]: https://a.org\n"
	r := parseTableStage(t, input)
	links := tableNodes(r.Document.root, Link)
	origins := []string{"bare-gfm", "autolink", "inline", "full", "collapsed", "shortcut", "bare-gfm", "bare-gfm", "bare-gfm", "bare-gfm", "autolink"}
	if len(links) != len(origins) {
		t.Fatal("source kinds", len(links))
	}
	for i, n := range links {
		if n.Properties().Link.Origin != origins[i] {
			t.Fatal("origin", i, n.Properties().Link.Origin)
		}
	}
	if _, err := NewDocument(r.Document.root, r.Document.references); err != nil {
		t.Fatal("bare nodes acquired reference association", err)
	}
	original := links[6]
	fresh := func() NodeSpec {
		return NodeSpec{Kind: Link, Envelope: original.Span(), Content: original.Content(), Properties: original.Properties(), Children: original.Children()}
	}
	spec := fresh()
	n, err := NewNode(spec)
	if err != nil {
		t.Fatal(err)
	}
	spec.Content[0] = Segment{}
	spec.Properties.Link.Label.Segments[0] = Segment{}
	spec.Properties.Link.Destination.Segments[0] = Segment{}
	spec.Properties.Link.Origin = "inline"
	*spec.Children[0] = Node{}
	prop := n.Properties()
	prop.Link.Origin = "inline"
	prop.Link.Destination.Segments[0] = Segment{}
	content := n.Content()
	content[0] = Segment{}
	children := n.Children()
	*children[0] = Node{}
	bytes := r.Source.Bytes()
	bytes[0] = '!'
	if !reflect.DeepEqual(n.spec, original.spec) || string(r.Source.Bytes()) != input {
		t.Fatal("mutable owned bare facts")
	}
	for name, change := range map[string]func(*NodeSpec){
		"image": func(s *NodeSpec) { s.Kind = Image }, "unknown-origin": func(s *NodeSpec) { s.Properties.Link.Origin = "bare" }, "unavailable": func(s *NodeSpec) { s.Properties.Link.Resolution = Unavailable },
		"title": func(s *NodeSpec) { v := Value{}; s.Properties.Link.Title = &v }, "reference": func(s *NodeSpec) { v := Value{}; s.Properties.Link.ReferenceLabel = &v }, "binding": func(s *NodeSpec) { s.Properties.Link.NormalizedLabel = Some("r") },
		"angle-marker": func(s *NodeSpec) { s.Markers = []Marker{{Role: "opening", Source: s.Envelope}} }, "missing-visible": func(s *NodeSpec) { s.Content = nil },
		"missing-child": func(s *NodeSpec) { s.Children = nil }, "missing-label": func(s *NodeSpec) { s.Properties.Link.Label.Semantic = Optional[string]{} },
		"foreign": func(s *NodeSpec) {
			s.Content[0].Source, _ = NewSource(r.Source.Bytes()).Span(s.Content[0].Source.Start(), s.Content[0].Source.End())
		},
		"prefix-source": func(s *NodeSpec) { s.Properties.Link.Destination.Segments[0].Source = s.Content[0].Source }, "prefix-kind": func(s *NodeSpec) { s.Properties.Link.Destination.Segments[0].Transformation = Normalized }, "prefix-text": func(s *NodeSpec) { s.Properties.Link.Destination.Segments[0].Semantic = Some("ftp://") },
		"aggregate": func(s *NodeSpec) { s.Properties.Link.Destination.Semantic = Some("http://invented") }, "label-aggregate": func(s *NodeSpec) { s.Properties.Link.Label.Semantic = Some("invented") },
		"shifted": func(s *NodeSpec) { s.Content[0].Source.start++ }, "invented-visible": func(s *NodeSpec) { s.Content[0].Source = Span{}; s.Content[0].Transformation = Synthetic },
	} {
		t.Run(name, func(t *testing.T) {
			s := fresh()
			change(&s)
			if _, err := NewNode(s); err == nil {
				t.Fatal("invalid bare model accepted")
			}
		})
	}
	foreign := NewSource(r.Source.Bytes())
	if _, err := foreign.Text(original.Span()); err == nil {
		t.Fatal("equal-byte foreign snapshot accepted")
	}
	manual, err := NewDocument(r.Document.root, r.Document.references)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manual.LiteralSpans(context.Background(), budgetFor(t, DefaultLimits())); err == nil {
		t.Fatal("manual protection authority")
	}
	assertBareCore(t, input)
	for _, input := range []string{"![www.a.b](/image)", "[www.a.b][missing]"} {
		r := parseTableStage(t, input)
		if len(bareNodes(r.Document.root)) != 0 {
			t.Fatal("invented bracket boundary")
		}
	}
}

func TestBareResourceDepthAndTermination(t *testing.T) {
	input := "> - [x] ~~**www.a.b**~~\n"
	source := NewSource([]byte(input))
	baseline := budgetFor(t, DefaultLimits())
	requireTableComplete(t, parseWithProfile(context.Background(), source, baseline, gfmInProgressProfile), input)
	for name, used := range map[string]int64{"document-nodes": baseline.nodes, "work-units": baseline.work, "total-nodes": baseline.nodes} {
		for _, delta := range []int64{-1, 0, 1} {
			l := DefaultLimits()
			switch name {
			case "document-nodes":
				l.DocumentNodes = used + delta
			case "work-units":
				l.WorkUnits = used + delta
			case "total-nodes":
				l.DocumentNodes = used
				l.TotalNodes = used + 1 + delta
			}
			op := budgetFor(t, l)
			if name == "total-nodes" {
				if err := op.ChargeNodes(1); err != nil {
					t.Fatal(err)
				}
			}
			r := parseWithProfile(context.Background(), source, op, gfmInProgressProfile)
			if delta < 0 {
				requireLimit(t, r, name)
			} else {
				requireTableComplete(t, r, input)
			}
		}
	}
	for _, allowed := range []int64{6, 7, 8} {
		l := DefaultLimits()
		l.SyntaxDepth = allowed
		r := parseWithProfile(context.Background(), source, budgetFor(t, l), gfmInProgressProfile)
		if allowed < 7 {
			requireLimit(t, r, "syntax-depth")
		} else {
			requireTableComplete(t, r, input)
		}
	}
	for _, generate := range []func(int) string{
		func(n int) string { return "http://" + strings.Repeat("www.", n) + "invalid_" },
		func(n int) string { return strings.Repeat("http://", n) + strings.Repeat("a", n) },
		func(n int) string { return "www." + strings.Repeat("a.", n) + "b/p" },
		func(n int) string { return strings.Repeat("a", n) + "@" + strings.Repeat("b", n) + ".c_ valid@b.c" },
		func(n int) string { return strings.Repeat("a@", n) + "b.c valid@b.c" },
		func(n int) string { return strings.Repeat("www.user@example.org \nx\n\n", n) },
		func(n int) string { return strings.Repeat("\\+@b.c ", n) },
		func(n int) string { return "www.a.b/" + strings.Repeat("(", n) + strings.Repeat(")", n*2) + "." },
		func(n int) string { return "www.a.b/a" + strings.Repeat("&x;", n) + ")." },
		func(n int) string { return strings.Repeat("www.a.b_*", n) + strings.Repeat("&x;", n) + ")." },
		func(n int) string { return strings.Repeat("www.a_.b_(", n) + strings.Repeat(")&x;", n) },
		func(n int) string {
			return strings.Repeat("www.a.b [ www.c.d ](/x) ![ ~~www.e.f~~ ](/i) [ www.g.h ][missing] ", n)
		},
	} {
		var previous int64
		for _, size := range []int{128, 256, 512} {
			input := generate(size)
			op := budgetFor(t, DefaultLimits())
			r := parseWithProfile(context.Background(), NewSource([]byte(input)), op, gfmInProgressProfile)
			requireTableComplete(t, r, input)
			if previous != 0 && op.work > previous*3 {
				t.Fatal("superlinear failed candidate/rebuild work", previous, op.work)
			}
			previous = op.work
		}
	}
	l := DefaultLimits()
	l.DocumentNodes = baseline.nodes
	l.TotalNodes = baseline.nodes * 2
	op := budgetFor(t, l)
	requireTableComplete(t, parseWithProfile(context.Background(), source, op, gfmInProgressProfile), input)
	requireTableComplete(t, ParseWithBudget(context.Background(), source, op), input)
	requireLimit(t, parseWithProfile(context.Background(), source, op, gfmInProgressProfile), "total-nodes")
	requireLimit(t, ParseWithBudget(context.Background(), source, op), "total-nodes")
	cancel, stop := context.WithCancel(context.Background())
	stop()
	l = DefaultLimits()
	l.DocumentBytes = 3
	requireLimit(t, parseWithProfile(cancel, NewSource([]byte("www.a.b\xff")), budgetFor(t, l), gfmInProgressProfile), "document-bytes")
	r := parseWithProfile(cancel, NewSource([]byte("a\xff")), budgetFor(t, DefaultLimits()), gfmInProgressProfile)
	if r.Failure == nil || r.Failure.Code != InvalidUTF8 || r.Interruption != "" {
		t.Fatal("encoding precedence")
	}
	l = DefaultLimits()
	l.WorkUnits = 1
	requireLimit(t, parseWithProfile(cancel, NewSource([]byte("a\xff")), budgetFor(t, l), gfmInProgressProfile), "work-units")
}

func TestBareSiblingImageHighWaterProgress(t *testing.T) {
	var previous int64
	for _, size := range []int{512, 1024, 2048} {
		input := strings.Repeat("![ ~~www.a.b~~ *x* ](/i) [ ~www.c.d ] text ", size)
		limits := DefaultLimits()
		limits.DocumentNodes = 1000000
		limits.TotalNodes = 4000000
		op := budgetFor(t, limits)
		r := parseWithProfile(context.Background(), NewSource([]byte(input)), op, gfmInProgressProfile)
		requireTableComplete(t, r, input)
		if len(tableNodes(r.Document.root, Image)) != size || len(bareNodes(r.Document.root)) < size {
			t.Fatal("sibling label recognition")
		}
		if previous != 0 && op.work*10 > previous*26 {
			t.Fatal("retired sibling delimiter history revisited", previous, op.work)
		}
		previous = op.work
	}
}

func TestBareCancellationPublicationAndIsolation(t *testing.T) {
	for _, input := range []string{"", "www.a.b", "![www.user@example.org \r\nx](/i)", "> - [x] ~~www.a.b/p\x00Z~~", strings.Repeat("http://", 300) + strings.Repeat("a", 300), "![ ~~www.a.b~~ ](/i) www.c.d/a&x;&y;.", "| www.a.b/a\\|b |\n| - |\n"} {
		source := NewSource([]byte(input))
		for _, deadline := range []bool{false, true} {
			completed, seen := false, false
			for checkpoint := 1; checkpoint < 3000; checkpoint++ {
				ctx := &quoteCheckpoint{checkpointContext: checkpointContext{Context: context.Background(), remaining: checkpoint}, deadline: deadline}
				op := budgetFor(t, DefaultLimits())
				r := parseWithProfile(ctx, source, op, gfmInProgressProfile)
				if r.Status == Complete {
					requireTableComplete(t, r, input)
					completed = true
					break
				}
				seen = true
				want := Cancelled
				if deadline {
					want = DeadlineExceeded
				}
				if r.Status != Incomplete || r.AST != Partial || r.Failure != nil || r.Interruption != want || len(r.Uncertainty) != 1 || r.Source != source || r.Document.references.LookupAvailability == Available {
					t.Fatal("interruption", r)
				}
				assertTaskSpan(t, r.Uncertainty[0].Region, 0, len(input))
				if r.Phases.Inlines == Interrupted && len(bareNodes(r.Document.root)) != 0 {
					t.Fatal("partial bare publication")
				}
				if _, err := r.Document.ProtectedNodes(context.Background(), op); err == nil {
					t.Fatal("incomplete authority")
				}
			}
			if !completed || !seen {
				t.Fatal("cancellation sweep", completed, seen)
			}
		}
	}
}

func TestBarePostScanCheckpointsAndPartialTab(t *testing.T) {
	input := "prefix **www.a.b/a*b** ![ ~~www.c.d~~ ](/i) <foo\\+@bar.example.com> www.e.f/a&x;&y;."
	source := NewSource([]byte(input))
	whole, _ := source.Span(0, len(input))
	parts := []Segment{{Source: whole, Transformation: Exact, Semantic: Some(input)}}
	ctx := &parserContext{Context: context.Background(), profile: gfmInProgressProfile}
	original := inlineScanner{ctx: ctx, text: input}
	original.opaqueCandidates()
	flat, err := original.scan(parts)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]*Node(nil), flat...)
	for _, deadline := range []bool{false, true} {
		completed, interrupted := false, 0
		for checkpoint := 1; checkpoint < 3000; checkpoint++ {
			cancel := &quoteCheckpoint{checkpointContext: checkpointContext{Context: context.Background(), remaining: checkpoint}, deadline: deadline}
			scanctx := &parserContext{Context: cancel, profile: gfmInProgressProfile}
			images := append([]bareImage(nil), original.images...)
			for i := range images {
				images[i].delimiters = append([]inlineDelimiter(nil), images[i].delimiters...)
			}
			scanner := inlineScanner{ctx: scanctx, text: input, delimiters: append([]inlineDelimiter(nil), original.delimiters...), images: images, failedAngles: original.failedAngles, delimitersLinked: original.delimitersLinked, delimiterHead: original.delimiterHead, delimiterTail: original.delimiterTail}
			nodes, err := scanner.extendedAutolinks(parts, flat)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(flat, before) {
				t.Fatal("borrowed tree changed")
			}
			if interruption(scanctx) != "" {
				interrupted++
				continue
			}
			nodes, err = scanner.emphasis(parts, nodes)
			if err != nil {
				t.Fatal(err)
			}
			if interruption(scanctx) != "" {
				interrupted++
				continue
			}
			count := 0
			for _, n := range nodes {
				count += len(bareNodes(n))
			}
			if count != 4 {
				t.Fatal("completed postscan count", count)
			}
			completed = true
			break
		}
		if !completed || interrupted < 100 {
			t.Fatal("new-phase checkpoints", completed, interrupted)
		}
	}
	source = NewSource([]byte("\twww.a.b"))
	tab, _ := source.Span(0, 1)
	url, _ := source.Span(1, 8)
	parts = []Segment{{Source: tab, Transformation: PartialTab, Semantic: Some("  ")}, {Source: url, Transformation: Exact, Semantic: Some("www.a.b")}}
	scanner := inlineScanner{ctx: ctx, text: "  www.a.b"}
	flat, err = scanner.scan(parts)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := scanner.extendedAutolinks(parts, flat)
	if err != nil || len(nodes) != 2 || nodes[0].Content()[0] != parts[0] || len(bareNodes(nodes[1])) != 1 {
		t.Fatal("partial tab ownership", err, nodes)
	}
	assertTaskSpan(t, nodes[1].Span(), 1, 8)
}

func TestBareConcurrentAndProtection(t *testing.T) {
	input := "> - [x] ~~www.a.b~~ [x][r] ![ www.c.d ](/i) `www.e.f` <!-- mdchk-disable -->\n\n[r]: /first\n[r]: /later\n"
	source := NewSource([]byte(input))
	op := budgetFor(t, DefaultLimits())
	baseline := parseWithProfile(context.Background(), source, budgetFor(t, DefaultLimits()), gfmInProgressProfile)
	requireTableComplete(t, baseline, input)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Go(func() {
			for j := 0; j < 3; j++ {
				r := parseWithProfile(context.Background(), source, op, gfmInProgressProfile)
				if r.Status != Complete || !reflect.DeepEqual(r.Document.root, baseline.Document.root) {
					t.Error("shared staged isolation")
				}
				core := ParseWithBudget(context.Background(), source, op)
				if core.Status != Complete || len(bareNodes(core.Document.root)) != 0 {
					t.Error("shared core leak")
				}
				independent := parseWithProfile(context.Background(), NewSource(source.Bytes()), budgetFor(t, DefaultLimits()), gfmInProgressProfile)
				if independent.Status != Complete || independent.Source == source || len(bareNodes(independent.Document.root)) != 2 {
					t.Error("independent source/profile")
				}
			}
		})
	}
	wg.Wait()
	assertBareCore(t, input)
	r := parseWithProfile(context.Background(), source, op, gfmInProgressProfile)
	requireTableComplete(t, r, input)
	protected, err := r.Document.ProtectedNodes(context.Background(), op)
	if err != nil {
		t.Fatal(err)
	}
	if len(protected) != 2 {
		t.Fatal("transparent bare/protected owners", len(protected))
	}
	comments, err := r.Document.Comments(context.Background(), op)
	if err != nil || len(comments) != 1 {
		t.Fatal("comment traversal", err)
	}
	literals, err := r.Document.LiteralSpans(context.Background(), op)
	if err != nil || len(literals) != 2 {
		t.Fatal("literal inventory", err, len(literals))
	}
	foreign := budgetFor(t, DefaultLimits())
	if _, err := r.Document.ProtectedNodes(context.Background(), foreign); err == nil {
		t.Fatal("foreign operation authority")
	}
	refs := tableNodes(r.Document.root, Link)
	found := false
	for _, n := range refs {
		if n.Properties().Link.Origin == "full" {
			found = true
			if n.Properties().Link.Destination.Semantic != Some("/first") {
				t.Fatal("first definition binding")
			}
		}
	}
	if !found {
		t.Fatal("reference missing")
	}
}

func TestBareApprovedSourceWitnessesAndTrimStarts(t *testing.T) {
	data, err := os.ReadFile("testdata/gfm/profile/expectations.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct{ Fixtures []struct{ ID, Source string } }
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, f := range fixtures.Fixtures {
		var kind Kind
		var owner, payload [2]int
		switch f.ID {
		case "code-protection":
			kind, owner, payload = CodeSpan, [2]int{0, 30}, [2]int{1, 29}
		case "block-code-protection":
			kind, owner, payload = CodeBlock, [2]int{0, 39}, [2]int{4, 35}
		case "raw-protection":
			kind, owner, payload = HTMLBlock, [2]int{0, 64}, [2]int{0, 64}
		case "comment-protection":
			kind, owner, payload = HTMLBlock, [2]int{0, 34}, [2]int{0, 34}
		case "nested-link", "autolink-utf8-tab", "autolink-origins":
		default:
			continue
		}
		seen++
		t.Run(f.ID, func(t *testing.T) {
			source := NewSource([]byte(f.Source))
			op := budgetFor(t, DefaultLimits())
			r := parseWithProfile(context.Background(), source, op, gfmInProgressProfile)
			requireTableComplete(t, r, f.Source)
			if kind != "" {
				if len(bareNodes(r.Document.root)) != 0 {
					t.Fatal("protected bare candidate")
				}
				owners, err := r.Document.ProtectedNodes(context.Background(), op)
				if err != nil || len(owners) != 1 || owners[0].Kind() != kind {
					t.Fatal("actual protected owner", owners, err)
				}
				assertTaskSpan(t, owners[0].Span(), owner[0], owner[1])
				spans, err := r.Document.LiteralSpans(context.Background(), op)
				want := [][2]int{payload}
				if kind == CodeBlock {
					want = [][2]int{{4, 34}, {34, 35}}
				}
				if err != nil || len(spans) != len(want) {
					t.Fatal("actual literal payload", spans, err)
				}
				for i, w := range want {
					assertTaskSpan(t, spans[i], w[0], w[1])
				}
			} else {
				switch f.ID {
				case "nested-link":
					links := tableNodes(r.Document.root, Link)
					if len(links) != 1 || links[0].Properties().Link.Origin != "inline" || links[0].Properties().Link.Destination.Semantic != Some("/x") {
						t.Fatal("explicit priority")
					}
					assertTaskSpan(t, links[0].Span(), 0, 15)
				case "autolink-utf8-tab":
					links := bareNodes(r.Document.root)
					if len(links) != 1 {
						t.Fatal("UTF8/tab candidate")
					}
					assertBare(t, links[0], bareWant{3, 17, "www.a.org/a(b)", "http://www.a.org/a(b)", "http://"})
					if tableText(r.Document.root) != "é\twww.a.org/a(b). &" {
						t.Fatal("decoded suffix once")
					}
					p, err := NewLineIndex(source).Position(3)
					if err != nil || p != (Position{1, 3}) {
						t.Fatal("scalar/tab coordinate", p, err)
					}
				case "autolink-origins":
					links := tableNodes(r.Document.root, Link)
					if len(links) != 3 {
						t.Fatal("explicit origin witness")
					}
					for i, w := range []string{"autolink", "inline", "shortcut"} {
						if links[i].Properties().Link.Origin != w {
							t.Fatal("origin", i)
						}
					}
				}
			}
			assertBareCore(t, f.Source)
		})
	}
	if seen != 7 {
		t.Fatal("approved witness inventory", seen)
	}
	input := "www.a_.b_(www.a.b/p(x))&x;"
	r := parseTableStage(t, input)
	links := bareNodes(r.Document.root)
	if len(links) != 1 {
		t.Fatal("different start paren thresholds", links)
	}
	assertBare(t, links[0], bareWant{10, 22, "www.a.b/p(x)", "http://www.a.b/p(x)", "http://"})
	if tableText(r.Document.root) != input {
		t.Fatal("failed domain and trim remainder preserved")
	}
}

func TestBareSelectedPrefixAssociation(t *testing.T) {
	for _, c := range []struct {
		input string
		w     bareWant
	}{
		{"www.user@example.org\n", bareWant{0, 20, "www.user@example.org", "mailto:www.user@example.org", "mailto:"}},
		{":www.a.b@c.d", bareWant{1, 12, "www.a.b@c.d", "mailto:www.a.b@c.d", "mailto:"}},
		{"[www.a.b@c.d]", bareWant{1, 12, "www.a.b@c.d", "mailto:www.a.b@c.d", "mailto:"}},
		{"![www.a.b@c.d](/i)", bareWant{2, 13, "www.a.b@c.d", "mailto:www.a.b@c.d", "mailto:"}},
		{"é:www.user@example.org\r\n", bareWant{3, 23, "www.user@example.org", "mailto:www.user@example.org", "mailto:"}},
		{"| www.user@example.org | x |\n| - | - |\n", bareWant{2, 22, "www.user@example.org", "mailto:www.user@example.org", "mailto:"}},
		{"www.a.b@c.d", bareWant{0, 11, "www.a.b@c.d", "http://www.a.b@c.d", "http://"}},
		{"www.a.b", bareWant{0, 7, "www.a.b", "http://www.a.b", "http://"}},
	} {
		t.Run(c.input, func(t *testing.T) {
			r := parseTableStage(t, c.input)
			links := bareNodes(r.Document.root)
			if len(links) != 1 {
				t.Fatal("selected form", links)
			}
			assertBare(t, links[0], c.w)
			n := links[0]
			spec := NodeSpec{Kind: n.Kind(), Envelope: n.Span(), Content: n.Content(), Properties: n.Properties(), Children: n.Children()}
			round, err := NewNode(spec)
			if err != nil || !reflect.DeepEqual(round.spec, n.spec) {
				t.Fatal("constructor association round trip", err)
			}
			again := parseWithProfile(context.Background(), r.Source, budgetFor(t, DefaultLimits()), gfmInProgressProfile)
			requireTableComplete(t, again, c.input)
			if !reflect.DeepEqual(again.Document.root, r.Document.root) {
				t.Fatal("repeat association")
			}
			assertBareCore(t, c.input)
		})
	}
	for _, c := range []struct{ input, prefix string }{{"www.a.b", "mailto:"}, {"a@b.c", "http://"}, {"www.user@example.org", "ftp://"}} {
		r := parseTableStage(t, c.input)
		n := bareNodes(r.Document.root)[0]
		spec := NodeSpec{Kind: Link, Envelope: n.Span(), Content: n.Content(), Properties: n.Properties(), Children: n.Children()}
		d := &spec.Properties.Link.Destination
		d.Segments[0].Semantic = Some(c.prefix)
		d.Semantic = Some(c.prefix + spec.Properties.Link.Label.Semantic.Value)
		if _, err := NewNode(spec); err == nil {
			t.Fatal("forged association accepted", c)
		}
	}
	r := parseTableStage(t, "[![www.user@example.org](/i)](/outer)")
	if len(bareNodes(r.Document.root)) != 0 || len(tableNodes(r.Document.root, Link)) != 1 {
		t.Fatal("successful outer link priority")
	}
}

func TestBarePreservesStructuralDiscardedSpace(t *testing.T) {
	for _, c := range []struct {
		input, label, prefix, visible string
		a, b, space, brEnd            int
	}{
		{"www.a.b \nx", "www.a.b", "http://", "www.a.bx", 0, 7, 7, 9},
		{"www.a.b \r\nx", "www.a.b", "http://", "www.a.bx", 0, 7, 7, 10},
		{"www.a.b \rx", "www.a.b", "http://", "www.a.bx", 0, 7, 7, 9},
		{"> www.a.b \n> x", "www.a.b", "http://", "www.a.bx", 2, 9, 9, 11},
		{"> www.a.b \r\n> x", "www.a.b", "http://", "www.a.bx", 2, 9, 9, 12},
		{"![ www.a.b \nx ](/i)", "www.a.b", "http://", " www.a.bx ", 3, 10, 10, 12},
		{"![ www.a.b \r\nx ](/i)", "www.a.b", "http://", " www.a.bx ", 3, 10, 10, 13},
		{"a@b.c \nx", "a@b.c", "mailto:", "a@b.cx", 0, 5, 5, 7},
		{"mailto:a@b.c \r\nx", "mailto:a@b.c", "", "mailto:a@b.cx", 0, 12, 12, 15},
		{"xmpp:a@b.c/r \nx", "xmpp:a@b.c/r", "", "xmpp:a@b.c/rx", 0, 12, 12, 14},
		{"www.user@example.org \nx", "www.user@example.org", "mailto:", "www.user@example.orgx", 0, 20, 20, 22},
		{"www.a.b&amp; \nx", "www.a.b", "http://", "www.a.b&x", 0, 7, 12, 14},
		{"www.a.b &amp;\\* \nx", "www.a.b", "http://", "www.a.b &*x", 0, 7, 15, 17},
		{"www.a.b \x00 \nx", "www.a.b", "http://", "www.a.b �x", 0, 7, 9, 11},
	} {
		t.Run(c.input, func(t *testing.T) {
			r := parseTableStage(t, c.input)
			links := bareNodes(r.Document.root)
			if len(links) != 1 {
				t.Fatal("link", links)
			}
			assertBare(t, links[0], bareWant{c.a, c.b, c.label, c.prefix + c.label, c.prefix})
			core := Parse(context.Background(), r.Source)
			requireTableComplete(t, core, c.input)
			if tableText(r.Document.root) != c.visible || tableText(core.Document.root) != c.visible {
				t.Fatalf("nonlink/alt meaning stage=%q core=%q want=%q", tableText(r.Document.root), tableText(core.Document.root), c.visible)
			}
			breaks := tableNodes(r.Document.root, SoftBreak)
			if len(breaks) != 1 {
				t.Fatal("soft break")
			}
			assertTaskSpan(t, breaks[0].Span(), c.space+1, c.brEnd)
			found := false
			for _, n := range tableNodes(r.Document.root, Text) {
				for _, p := range n.Content() {
					if p.Source.Start() == c.space && p.Source.End() == c.space+1 {
						found = true
						if p.Transformation != Normalized || p.Semantic != Some("") || tableRaw(t, r.Source, p.Source) != " " {
							t.Fatal("discarded source-space contribution", p)
						}
					}
				}
			}
			if !found {
				t.Fatal("discarded space owner missing")
			}
			if c.input == "www.a.b &amp;\\* \nx" {
				texts := tableNodes(r.Document.root, Text)
				var suffix *Node
				for _, n := range texts {
					if n.Span().Start() == 7 {
						suffix = n
					}
				}
				if suffix == nil || len(suffix.Content()) != 4 {
					t.Fatal("one-pass suffix mapping", suffix)
				}
				for i, w := range []struct {
					a, b  int
					k     Transformation
					value string
				}{{7, 8, Exact, " "}, {8, 13, Entity, "&"}, {13, 15, Escape, "*"}, {15, 16, Normalized, ""}} {
					p := suffix.Content()[i]
					assertTaskSpan(t, p.Source, w.a, w.b)
					if p.Transformation != w.k || p.Semantic != Some(w.value) {
						t.Fatal("suffix contribution", i, p)
					}
				}
			}
			assertBareCore(t, c.input)
		})
	}
	for _, c := range []struct {
		input, visible string
		kind           Kind
	}{{"www.a.b  \nx", "www.a.bx", HardBreak}, {"www.a.b\\\nx", "www.a.bx", HardBreak}, {"www.a.b\t\nx", "www.a.b\tx", SoftBreak}} {
		r := parseTableStage(t, c.input)
		core := Parse(context.Background(), r.Source)
		requireTableComplete(t, core, c.input)
		if len(bareNodes(r.Document.root)) != 1 || len(tableNodes(r.Document.root, c.kind)) != 1 || tableText(r.Document.root) != c.visible || tableText(core.Document.root) != c.visible {
			t.Fatal("unchanged hard break/tab semantics", c.input)
		}
	}
}
