package parser_test

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

// These are fixed acceptance identities, independent of the production ledger.
var requiredInlineLinks = []int{
	22, 32, 41, 404, 419, 422, 433, 473, 474,
	482, 483, 484, 485, 486, 487, 488, 489, 490, 491, 492, 493, 494,
	495, 496, 497, 498, 499, 500, 501, 502, 503, 504, 505, 506, 507,
	508, 509, 510, 512, 513, 514, 515, 516, 518, 519, 521, 522, 567,
}
var literalInlineLinks = []int{197, 199, 201, 209, 211, 212, 213, 511, 523, 546, 547, 548, 551, 552}

type inlineTitleWant struct {
	marker byte
	raw    string
}
type inlineLinkWant struct {
	label, destination         string
	angle                      bool
	leading, between, trailing string
	title                      *inlineTitleWant
}

// Compose explicitly supplied syntax, not a recognizer. Every source position is
// calculated from these independent fixture bytes, never the produced tree.
func (w inlineLinkWant) spelling() string {
	destination := w.destination
	if w.angle {
		destination = "<" + destination + ">"
	}
	title := ""
	if w.title != nil {
		close := w.title.marker
		if close == '(' {
			close = ')'
		}
		title = w.between + string(w.title.marker) + w.title.raw + string(close)
	}
	return "[" + w.label + "](" + w.leading + destination + title + w.trailing + ")"
}

func TestInlineLinkApprovedPartitionAndUpstream(t *testing.T) {
	quote := func(raw string) *inlineTitleWant { return &inlineTitleWant{'"', raw} }
	wants := map[int][]inlineLinkWant{
		22:  {{label: "foo", destination: `/bar\*`, between: " ", title: quote(`ti\*tle`)}},
		32:  {{label: "foo", destination: "/f&ouml;&ouml;", between: " ", title: quote("f&ouml;&ouml;")}},
		404: {{label: "bar", destination: "/url"}}, 419: {{label: "*bar*", destination: "/url"}},
		422: {{label: "bar", destination: "/url"}}, 433: {{label: "*bar*", destination: "/url"}},
		473: {{label: "bar*", destination: "/url"}}, 474: {{label: "bar_", destination: "/url"}},
		482: {{label: "link", destination: "/uri", between: " ", title: quote("title")}},
		483: {{label: "link", destination: "/uri"}}, 484: {{destination: "./target.md"}},
		485: {{label: "link"}}, 486: {{label: "link", angle: true}}, 487: {{}},
		489: {{label: "link", destination: "/my uri", angle: true}},
		492: {{label: "a", destination: "b)c", angle: true}},
		495: {{label: "link", destination: `\(foo\)`}},
		496: {{label: "link", destination: "foo(and(bar))"}},
		498: {{label: "link", destination: `foo\(and\(bar\)`}},
		499: {{label: "link", destination: "foo(and(bar)", angle: true}},
		500: {{label: "link", destination: `foo\)\:`}},
		501: {{label: "link", destination: "#fragment"}, {label: "link", destination: "https://example.com#fragment"}, {label: "link", destination: "https://example.com?foo=3#frag"}},
		502: {{label: "link", destination: `foo\bar`}},
		503: {{label: "link", destination: "foo%20b&auml;"}},
		504: {{label: "link", destination: `"title"`}},
		505: {{label: "link", destination: "/url", between: " ", title: quote("title")}, {label: "link", destination: "/url", between: " ", title: &inlineTitleWant{'\'', "title"}}, {label: "link", destination: "/url", between: " ", title: &inlineTitleWant{'(', "title"}}},
		506: {{label: "link", destination: "/url", between: " ", title: quote(`title \"&quot;`)}},
		507: {{label: "link", destination: "/url\u00a0\"title\""}},
		509: {{label: "link", destination: "/url", between: " ", title: &inlineTitleWant{'\'', `title "and" title`}}},
		510: {{label: "link", destination: "/uri", leading: "   ", between: "\n  ", title: quote("title"), trailing: "  "}},
		512: {{label: "link [foo [bar]]", destination: "/uri"}},
		514: {{label: "bar", destination: "/uri"}},
		515: {{label: `link \[bar`, destination: "/uri"}},
		516: {{label: "link *foo **bar** `#`*", destination: "/uri"}},
		518: {{label: "bar", destination: "/uri"}}, 519: {{label: "baz", destination: "/uri"}},
		521: {{label: "foo*", destination: "/uri"}}, 522: {{label: "foo *bar", destination: "baz*"}},
		567: {{label: "foo"}},
	}
	ids := map[int]bool{}
	for _, group := range [][]int{requiredInlineLinks, literalInlineLinks} {
		for _, id := range group {
			if ids[id] {
				t.Fatal("duplicate acceptance identity", id)
			}
			ids[id] = true
		}
	}
	if len(requiredInlineLinks) != 48 || len(literalInlineLinks) != 14 || len(ids) != 62 {
		t.Fatal("acceptance partition changed")
	}
	cases, err := emphasisExamples()
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range cases {
		if !ids[e.ID] {
			continue
		}
		seen++
		t.Run(strconv.Itoa(e.ID), func(t *testing.T) {
			r := parser.Parse(context.Background(), parser.NewSource([]byte(e.Markdown)))
			checkStagedTextOutcome(t, r, true)
			if out := conformance.Evaluate(r); out.Status != "rendered" || out.HTML != e.HTML {
				t.Fatal("actual Parse/Evaluate differs from unchanged upstream", out, e.HTML)
			}
			var links []*parser.Node
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				switch n.Kind() {
				case parser.Link:
					checkLinkProvenance(t, r.Source, n)
					links = append(links, n)
				case parser.Text:
					checkEmphasisText(t, r.Source, n)
				case parser.Emphasis, parser.Strong:
					checkEmphasisProvenance(t, r.Source, n)
				case parser.Paragraph, parser.Heading:
					checkInlineSourceCoverage(t, n)
				case parser.CodeSpan:
					checkCodeProvenance(t, r.Source, n)
				case parser.InlineHTML:
					checkInlineHTMLProvenance(t, r.Source, n)
				case parser.HardBreak, parser.SoftBreak:
					checkBreakProperties(t, r.Source, n, n.Properties().Break.Origin, byteRange{n.Span().Start(), n.Content()[0].Source.Start()})
				}
				return true
			})
			if len(links) != len(wants[e.ID]) {
				t.Fatal("independent Link count", len(links), len(wants[e.ID]))
			}
			for i, want := range wants[e.ID] {
				checkInlineLinkFixture(t, r.Source, links[i], e.Markdown, want)
			}
			if want := map[int][]parser.Kind{
				419: {parser.Link, parser.Emphasis, parser.Text},
				433: {parser.Link, parser.Emphasis, parser.Text},
				516: {parser.Link, parser.Text, parser.Emphasis, parser.Text, parser.Strong, parser.Text, parser.Text, parser.CodeSpan},
			}[e.ID]; want != nil {
				var shape []parser.Kind
				parser.Walk(links[0], func(n *parser.Node) bool { shape = append(shape, n.Kind()); return true })
				if !reflect.DeepEqual(shape, want) {
					t.Fatal("independent formatted-label shape", shape, want)
				}
			}
			if !reflect.DeepEqual(r, parser.Parse(context.Background(), r.Source)) || string(r.Source.Bytes()) != e.Markdown {
				t.Fatal("source/state ownership")
			}
		})
	}
	if seen != 62 {
		t.Fatal("missing required/newly complete example", seen)
	}
}

func checkInlineLinkFixture(t *testing.T, source parser.Source, n *parser.Node, input string, w inlineLinkWant) {
	t.Helper()
	raw := w.spelling()
	start := strings.Index(input, raw)
	if start < 0 || n.Span().Start() != start || n.Span().End() != start+len(raw) {
		t.Fatal("expected exact original link envelope", raw, n.Span())
	}
	p := n.Properties().Link
	if p.Label.Semantic != parser.Some(w.label) || p.Destination.Semantic != parser.Some(oracleText(w.destination, entityOracle(t))) || (p.Title != nil) != (w.title != nil) {
		t.Fatal("independent component semantics", p, w)
	}
	component := func(v parser.Value, a int, text string) {
		if len(v.Segments) == 0 || v.Segments[0].Source.Start() != a || v.Segments[len(v.Segments)-1].Source.End() != a+len(text) {
			t.Fatal("independent component range", v, a, text)
		}
		var original strings.Builder
		for _, part := range v.Segments {
			s, err := source.Text(part.Source)
			if err != nil {
				t.Fatal(err)
			}
			original.WriteString(s)
		}
		if original.String() != text {
			t.Fatal("original component bytes", original.String(), text)
		}
	}
	component(p.Label, start+1, w.label)
	dest := start + len(w.label) + 3 + len(w.leading)
	if w.angle {
		dest++
	}
	component(p.Destination, dest, w.destination)
	type mark struct {
		role string
		pos  int
	}
	marks := []mark{{"label-open", start}, {"label-close", start + 1 + len(w.label)}, {"link-open", start + 2 + len(w.label)}}
	if w.angle {
		marks = append(marks, mark{"destination-open", dest - 1}, mark{"destination-close", dest + len(w.destination)})
	}
	if w.title != nil {
		title := dest + len(w.destination) + len(w.between) + 1
		if w.angle {
			title++
		}
		component(*p.Title, title, w.title.raw)
		marks = append(marks, mark{"title-open", title - 1}, mark{"title-close", title + len(w.title.raw)})
		if p.Title.Semantic != parser.Some(oracleText(w.title.raw, entityOracle(t))) {
			t.Fatal("independent title", p.Title)
		}
	}
	marks = append(marks, mark{"link-close", start + len(raw) - 1})
	if len(n.Markers()) != len(marks) {
		t.Fatal("independent marker count", n.Markers(), marks)
	}
	for i, want := range marks {
		m := n.Markers()[i]
		if m.Role != want.role || m.Source.Start() != want.pos || m.Source.End() != want.pos+1 {
			t.Fatal("independent original delimiter range", m, want)
		}
	}
}

func checkLinkProvenance(t *testing.T, source parser.Source, n *parser.Node) {
	if origin := n.Properties().Link.Origin; origin == "full" || origin == "collapsed" || origin == "shortcut" {
		checkReferenceLinkProvenance(t, source, n)
		return
	}
	t.Helper()
	if n.Properties().Link.Origin == "autolink" {
		checkAutolinkProvenance(t, source, n)
		return
	}
	p := n.Properties().Link
	if n.Kind() != parser.Link || p.Origin != "inline" || p.Resolution != parser.Available || p.NormalizedLabel.Present || n.Properties().Literal.Present {
		t.Fatal("inline Link properties", p)
	}
	m := map[string]parser.Span{}
	for _, marker := range n.Markers() {
		if m[marker.Role].Present() || marker.Source.End()-marker.Source.Start() != 1 {
			t.Fatal("duplicate or non-scalar marker", marker)
		}
		m[marker.Role] = marker.Source
	}
	for role, want := range map[string]string{"label-open": "[", "label-close": "]", "link-open": "(", "link-close": ")"} {
		raw, err := source.Text(m[role])
		if err != nil || raw != want {
			t.Fatal("original link delimiter", role, raw, err)
		}
	}
	if m["label-open"].Start() != n.Span().Start() || m["link-close"].End() != n.Span().End() || m["label-close"].End() != m["link-open"].Start() || !reflect.DeepEqual(n.Content(), p.Label.Segments) {
		t.Fatal("inline label/envelope contract")
	}
	value := func(v parser.Value, decoded bool) string {
		if len(v.Segments) == 0 || !v.Semantic.Present {
			t.Fatal("missing known component", v)
		}
		var original, semantic strings.Builder
		previous := n.Span().Start()
		for _, part := range v.Segments {
			raw, err := source.Text(part.Source)
			if err != nil || part.Source.Start() < previous || part.Source.End() > n.Span().End() {
				t.Fatal("component lost source ownership/order", part)
			}
			want := strings.ReplaceAll(raw, "\x00", "\ufffd")
			if decoded {
				want = oracleText(strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n"), entityOracle(t))
			}
			if part.Transformation == parser.PartialTab {
				if raw != "\t" || strings.Trim(part.Semantic.Value, " ") != "" || len(part.Semantic.Value) < 1 || len(part.Semantic.Value) > 3 {
					t.Fatal("partial tab mapping", part)
				}
				want = part.Semantic.Value
			} else if part.Transformation != parser.Exact && part.Transformation != parser.Normalized && (!decoded || part.Transformation != parser.Escape && part.Transformation != parser.Entity) {
				t.Fatal("invented component transformation", part)
			}
			if part.Semantic != parser.Some(want) {
				t.Fatal("one-pass component interpretation", part, want)
			}
			original.WriteString(raw)
			semantic.WriteString(want)
			previous = part.Source.End()
		}
		if v.Semantic != parser.Some(semantic.String()) {
			t.Fatal("component semantic concatenation", v)
		}
		return original.String()
	}
	value(p.Label, false)
	dest := value(p.Destination, true)
	if p.Label.Segments[0].Source.Start() != m["label-open"].End() || p.Label.Segments[len(p.Label.Segments)-1].Source.End() != m["label-close"].Start() {
		t.Fatal("label boundary/empty provenance")
	}
	if m["destination-open"].Present() {
		for role, want := range map[string]string{"destination-open": "<", "destination-close": ">"} {
			raw, err := source.Text(m[role])
			if err != nil || raw != want {
				t.Fatal("angle delimiter", role, raw)
			}
		}
		if strings.ContainsAny(oracleTokens.ReplaceAllString(dest, ""), "<>\r\n") {
			t.Fatal("invalid angle destination", dest)
		}
	} else {
		// Independent raw-escape removal plus rune validation, not the producer's
		// candidate scanner. Entity-produced whitespace/parentheses remain data.
		plain := oracleTokens.ReplaceAllStringFunc(strings.ReplaceAll(dest, "\x00", "\ufffd"), func(token string) string {
			if token[0] == '\\' {
				return ""
			}
			return token
		})
		depth := 0
		for _, r := range plain {
			if r <= ' ' || r == 127 || r == '<' && depth == 0 && plain[0] == '<' {
				t.Fatal("invalid bare destination", dest)
			}
			if r == '(' {
				depth++
			} else if r == ')' {
				depth--
				if depth < 0 {
					t.Fatal("unbalanced raw destination", dest)
				}
			}
		}
		if depth != 0 {
			t.Fatal("unbalanced raw destination", dest)
		}
	}
	if p.Title != nil {
		raw := value(*p.Title, true)
		open, e1 := source.Text(m["title-open"])
		close, e2 := source.Text(m["title-close"])
		if e1 != nil || e2 != nil || !(open == "\"" && close == "\"" || open == "'" && close == "'" || open == "(" && close == ")") {
			t.Fatal("title delimiters", open, close)
		}
		plain := oracleTokens.ReplaceAllStringFunc(raw, func(token string) string {
			if token[0] == '\\' {
				return ""
			}
			return token
		})
		if strings.Contains(plain, close) || open == "(" && strings.Contains(plain, "(") {
			t.Fatal("unescaped title delimiter", raw)
		}
	}
	checkInlineSourceCoverage(t, n)
	for _, child := range n.Children() {
		parser.Walk(child, func(child *parser.Node) bool {
			if child.Kind() == parser.Link {
				t.Fatal("Link contains another Link")
			}
			return true
		})
		if child.Span().Start() < m["label-open"].End() || child.Span().End() > m["label-close"].Start() {
			t.Fatal("label child crosses bracket boundary")
		}
	}
}

func TestInlineLinkMappingOwnershipAndOpacity(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, terminal := range []string{"", ending} {
			for _, input := range []string{
				"[é\x00 *β*](/a&amp;b \"ti\\\"tle\")" + terminal,
				"# [é\x00](<> '') #" + terminal,
				"[é\x00](url)" + ending + "===" + terminal,
				"> - [é\x00" + ending + ">   *β*](<a b>" + ending + ">   \"x" + ending + ">   y\")" + terminal,
				"> [a" + ending + "lazy](b)" + terminal,
				"*[a](b) [*c*](d)*" + terminal,
			} {
				source := parser.NewSource([]byte(input))
				r := parser.Parse(context.Background(), source)
				checkStagedTextOutcome(t, r, true)
				count := 0
				parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
					if n.Kind() == parser.Link {
						count++
						checkLinkProvenance(t, source, n)
						if strings.Count(input, "[") == 1 {
							if n.Span().Start() != strings.Index(input, "[") || n.Span().End() != strings.LastIndex(input, ")")+1 || n.Markers()[1].Source.Start() != strings.Index(input, "]") {
								t.Fatal("independently located mapped envelope/brackets", n.Span())
							}
						}
						p := n.Properties()
						p.Link.Destination.Segments[0].Semantic = parser.Some("changed")
						p.Link.Label.Segments[0].Source = parser.Span{}
						if p.Link.Title != nil {
							p.Link.Title.Semantic = parser.Some("changed")
						}
						n.Markers()[0].Source = parser.Span{}
						n.Content()[0].Semantic = parser.Some("changed")
						if children := n.Children(); len(children) != 0 {
							*children[0] = parser.Node{}
						}
						*n = parser.Node{}
					}
					return true
				})
				if count == 0 || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) || string(source.Bytes()) != input {
					t.Fatal("missing link or mutable snapshot", input)
				}
			}
		}
	}
	for _, input := range []string{`\[a](b)`, `&#91;a](b)`, `&lbrack;a](b)`, "`[a](b)`", "    [a](b)", "~~~\n[a](b)\n~~~", "<!-- [a](b) -->", "x <a v='[a](b)'>", "[a\n\n](b)", "# [a\n\n](b)", "[a\n\n# ](b)"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.Link {
				t.Fatal("generated/opaque link", input)
			}
			return true
		})
	}
	for _, tt := range []struct{ input, html string }{
		{"[a <ab:x>](b)", "<p>[a <a href=\"ab:x\">ab:x</a>](b)</p>\n"},
		{"[a](bad title) [b](ok)", "<p>[a](bad title) <a href=\"ok\">b</a></p>\n"},
		{`\![a](b)`, "<p>!<a href=\"b\">a</a></p>\n"},
		{"[a]() [b](<> '')", "<p><a href=\"\">a</a> <a href=\"\" title=\"\">b</a></p>\n"},
	} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(tt.input)))
		if out := conformance.Evaluate(r); out.Status != "rendered" || out.HTML != tt.html {
			t.Fatal("boundary/recovery", tt.input, out, tt.html)
		}
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.Link {
				checkLinkProvenance(t, r.Source, n)
			}
			return true
		})
	}
	for _, input := range []string{"![a](b)", "[![a](b)](c)"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		if r.Status != parser.Incomplete || conformance.Evaluate(r).Status != "pending" {
			t.Fatal("remaining reference/image interpretation falsely complete", input)
		}
	}
	// ASCII whitespace is structural; other Unicode whitespace is destination data.
	for _, r := range []rune{'\u00a0', '\u2003', '\u2028'} {
		if !unicode.IsSpace(r) {
			t.Fatal("bad whitespace fixture")
		}
		input := "[a](b" + string(r) + "c)"
		parsed := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, parsed, true)
		checkLinkProvenance(t, parsed.Source, parsed.Document.Root().Children()[0].Children()[0])
	}
}
