package parser_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

type referenceWant struct {
	raw, label, destination string
	title                   *string
	key                     string
}

func refWant(raw, label, destination, key string, title ...string) referenceWant {
	w := referenceWant{raw: raw, label: label, destination: destination, key: key}
	if len(title) > 0 {
		w.title = &title[0]
	}
	return w
}

func referenceRaw(t *testing.T, source parser.Source, value parser.Value) string {
	t.Helper()
	var raw strings.Builder
	for _, segment := range value.Segments {
		s, err := source.Text(segment.Source)
		if err != nil {
			t.Fatal(err)
		}
		raw.WriteString(s)
	}
	return raw.String()
}

func checkReferenceRecords(t *testing.T, r parser.ParseResult, input string, wants []referenceWant) {
	t.Helper()
	refs := r.Document.References()
	if refs.Availability != parser.Available || refs.LookupAvailability != parser.Available || len(refs.Definitions) != len(wants) {
		t.Fatalf("registry = %+v, want %d records", refs, len(wants))
	}
	first := map[string]int{}
	for i, w := range wants {
		p := refs.Definitions[i].Properties()
		start := strings.Index(input, w.raw)
		if start < 0 {
			t.Fatal("bad independent fixture", w.raw)
		}
		if p.Envelope.Start() != start || p.Envelope.End() != start+len(w.raw) {
			t.Fatalf("definition %d envelope [%d,%d), want [%d,%d)", i, p.Envelope.Start(), p.Envelope.End(), start, start+len(w.raw))
		}
		if referenceRaw(t, r.Source, p.Label) != w.label || referenceRaw(t, r.Source, p.Destination) != w.destination || p.NormalizedLabel != parser.Some(w.key) {
			t.Fatalf("definition %d components = %+v", i, p)
		}
		if (p.Title == nil) != (w.title == nil) {
			t.Fatalf("definition %d title presence", i)
		}
		if p.Title != nil && referenceRaw(t, r.Source, *p.Title) != *w.title {
			t.Fatalf("title raw = %q want %q", referenceRaw(t, r.Source, *p.Title), *w.title)
		}
		// Independent marker offsets from the original spelling. Label closers
		// follow the complete raw label, including any escaped closing bracket.
		labelStart := start + strings.Index(w.raw, "[")
		wantMarkers := []struct {
			role       string
			start, end int
		}{{"label-open", labelStart, labelStart + 1}, {"label-close", labelStart + 1 + len(w.label), labelStart + 2 + len(w.label)}, {"definition-colon", labelStart + 2 + len(w.label), labelStart + 3 + len(w.label)}}
		for j, m := range wantMarkers {
			got := p.Markers[j]
			if got.Role != m.role || got.Source.Start() != m.start || got.Source.End() != m.end {
				t.Fatalf("marker %d = %+v want %+v", j, got, m)
			}
		}
		if p.Destination.Semantic != parser.Some(oracleText(w.destination, entityOracle(t))) {
			t.Fatal("destination decoding differs from independent oracle", p.Destination)
		}
		// Component ranges are independently located in the original fixture,
		// rather than inferred from the recognizer's returned markers.
		destination := start + strings.Index(w.raw, w.destination)
		if w.destination == "" {
			destination = start + strings.Index(w.raw, "<>") + 1
		}
		if len(p.Destination.Segments) == 0 || p.Destination.Segments[0].Source.Start() != destination || p.Destination.Segments[len(p.Destination.Segments)-1].Source.End() != destination+len(w.destination) {
			t.Fatal("destination source range", i, p.Destination)
		}
		if w.title != nil {
			title := destination + len(w.destination) + strings.Index(input[destination+len(w.destination):start+len(w.raw)], *w.title)
			segments := p.Title.Segments
			if len(segments) == 0 || segments[0].Source.Start() != title || segments[len(segments)-1].Source.End() != title+len(*w.title) {
				t.Fatal("title source range", i, segments)
			}
		}
		if _, ok := first[w.key]; !ok {
			first[w.key] = i
		}
	}
	if !reflect.DeepEqual(refs.Lookup, first) {
		t.Fatalf("lookup = %v want %v", refs.Lookup, first)
	}
	if string(r.Source.Bytes()) != input {
		t.Fatal("source changed")
	}
}

func TestReferenceUpstreamTargets(t *testing.T) {
	// Literal source spellings and tree expectations are independent of the
	// recognizer, scanner and generated HTML. Exercise every original 192–218.
	wants := map[int][]referenceWant{
		192: {refWant("[foo]: /url \"title\"\n", "foo", "/url", "foo", "title")},
		193: {refWant("   [foo]: \n      /url  \n           'the title'  \n", "foo", "/url", "foo", "the title")},
		194: {refWant("[Foo*bar\\]]:my_(url) 'title (with parens)'\n", "Foo*bar\\]", "my_(url)", "foo*bar\\]", "title (with parens)")},
		195: {refWant("[Foo bar]:\n<my url>\n'title'\n", "Foo bar", "my url", "foo bar", "title")},
		196: {refWant("[foo]: /url '\ntitle\nline1\nline2\n'\n", "foo", "/url", "foo", "\ntitle\nline1\nline2\n")},
		197: nil,
		198: {refWant("[foo]:\n/url\n", "foo", "/url", "foo")}, 199: nil,
		200: {refWant("[foo]: <>\n", "foo", "", "foo")}, 201: nil,
		202: {{raw: "[foo]: /url\\bar\\*baz \"foo\\\"bar\\baz\"\n", label: "foo", destination: "/url\\bar\\*baz", key: "foo", title: stringPointer("foo\\\"bar\\baz")}},
		203: {refWant("[foo]: url\n", "foo", "url", "foo")},
		204: {refWant("[foo]: first\n", "foo", "first", "foo"), refWant("[foo]: second\n", "foo", "second", "foo")},
		205: {refWant("[FOO]: /url\n", "FOO", "/url", "foo")},
		206: {refWant("[ΑΓΩ]: /φου\n", "ΑΓΩ", "/φου", "αγω")},
		207: {refWant("[foo]: /url\n", "foo", "/url", "foo")},
		208: {refWant("[\nfoo\n]: /url\n", "\nfoo\n", "/url", "foo")}, 209: nil,
		210: {refWant("[foo]: /url\n", "foo", "/url", "foo")}, 211: nil, 212: nil, 213: nil,
		214: {refWant("[foo]: /url\n", "foo", "/url", "foo")},
		215: {refWant("[foo]: /url\n", "foo", "/url", "foo")},
		216: {refWant("[foo]: /url\n", "foo", "/url", "foo")},
		217: {refWant("[foo]: /foo-url \"foo\"\n", "foo", "/foo-url", "foo", "foo"), refWant("[bar]: /bar-url\n  \"bar\"\n", "bar", "/bar-url", "bar", "bar"), refWant("[baz]: /baz-url\n", "baz", "/baz-url", "baz")},
		218: {refWant("[foo]: /url\n", "foo", "/url", "foo")},
	}
	data, err := os.ReadFile("testdata/commonmark/0.31.2/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples []struct {
		ID             int `json:"example"`
		Markdown, HTML string
	}
	if err = json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range examples {
		w, ok := wants[e.ID]
		if !ok {
			continue
		}
		seen++
		t.Run(strconv.Itoa(e.ID), func(t *testing.T) {
			r := parser.Parse(context.Background(), parser.NewSource([]byte(e.Markdown)))
			if r.Failure != nil {
				t.Fatal(r.Failure)
			}
			checkReferenceRecords(t, r, e.Markdown, w)
			nodes := r.Document.Root().Children()
			if e.ID == 207 {
				out := conformance.Evaluate(r)
				if r.Status != parser.Complete || r.Phases != (parser.PhaseProgress{Blocks: parser.PhaseComplete, Inlines: parser.PhaseComplete}) || len(nodes) != 0 || out.Status != "rendered" || out.HTML != e.HTML {
					t.Fatal("definition-only recognition incomplete", r, out)
				}
			}
			checkCorpusTextOutcome(t, e.ID, e.HTML, r)
			switch e.ID {
			case 197:
				if len(nodes) != 3 {
					t.Fatal("invalid multiline title consumed prose")
				}
			case 199, 201, 213:
				if len(nodes) != 2 {
					t.Fatal("invalid/noninterrupting definition changed paragraphs")
				}
			case 209:
				if len(nodes) != 1 || nodes[0].Span().Start() != 0 || nodes[0].Span().End() != len(e.Markdown) {
					t.Fatal("invalid same-line title consumed")
				}
			case 210:
				if len(nodes) != 1 || nodes[0].Span().Start() != 12 || nodes[0].Span().End() != 23 || referenceNodeText(nodes[0]) != "\"title\" ok" {
					t.Fatal("next-line title remainder lost", nodes)
				}
			case 211, 212:
				if len(nodes) != 2 || nodes[0].Kind() != parser.CodeBlock {
					t.Fatal("code definition was interpreted")
				}
			case 214:
				if len(nodes) != 2 || nodes[0].Kind() != parser.Heading || nodes[1].Kind() != parser.BlockQuote {
					t.Fatal("surrounding blocks lost")
				}
			case 215:
				if len(nodes) != 2 || nodes[0].Kind() != parser.Heading || nodes[0].Span().Start() != 12 || nodes[0].Span().End() != 20 || referenceNodeText(nodes[0]) != "bar" {
					t.Fatal("Setext remainder wrong")
				}
			case 216:
				if len(nodes) != 1 || nodes[0].Kind() != parser.Paragraph || nodes[0].Span().Start() != 12 || referenceNodeText(nodes[0]) != "===\n[foo]" {
					t.Fatal("definition-only prefix became a heading")
				}
			case 218:
				if len(nodes) != 2 || nodes[1].Kind() != parser.BlockQuote || len(nodes[1].Children()) != 0 {
					t.Fatal("definition-only quote disappeared")
				}
			}
		})
	}
	if seen != 27 {
		t.Fatal("missing upstream targets", seen)
	}
}

func stringPointer(s string) *string { return &s }
func referenceNodeText(n *parser.Node) string {
	var s strings.Builder
	for _, v := range n.Content() {
		s.WriteString(v.Semantic.Value)
	}
	return s.String()
}

func TestReferenceLabelBoundsAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		label, key string
		valid      bool
	}{
		{"", "", false}, {" \t ", "", false}, {"a[b", "", false}, {"a\\]b", "a\\]b", true}, {"a\\[b", "a\\[b", true},
		{strings.Repeat("é", 999), strings.Repeat("é", 999), true}, {strings.Repeat("é", 1000), "", false},
		{strings.Repeat("é", 997) + "\r\nx", strings.Repeat("é", 997) + " x", true},
		{strings.Repeat("é", 998) + "\r\nx", "", false},
		{" Straße\tẞ SS Σςσ ", "strasse ss ss σσσ", true}, {"a\u00a0b", "a\u00a0b", true}, {"a\\!", "a\\!", true}, {"a&amp;", "a&amp;", true},
	} {
		input := "[" + tc.label + "]: /url\n"
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		refs := r.Document.References()
		if r.Failure != nil || refs.LookupAvailability != parser.Available {
			t.Fatal(r)
		}
		if !tc.valid {
			if len(refs.Definitions) != 0 {
				t.Fatal("invalid label accepted", tc.label)
			}
			continue
		}
		checkReferenceRecords(t, r, input, []referenceWant{refWant(input, tc.label, "/url", tc.key)})
	}
	input := "[ẞ]: /first\n> [ss]: /second\n\n[foo\\!]: /escape\n[foo!]: /literal\n[foo&amp;]: /entity\n[foo&]: /amp\n"
	r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
	refs := r.Document.References()
	if len(refs.Definitions) != 6 || len(refs.Lookup) != 5 || refs.Lookup["ss"] != 0 || refs.Lookup["foo\\!"] != 2 || refs.Lookup["foo!"] != 3 || refs.Lookup["foo&amp;"] != 4 || refs.Lookup["foo&"] != 5 {
		t.Fatal("matching identities collapsed", refs)
	}
}

func TestReferenceContainersEndingsAndOwnership(t *testing.T) {
	partialTab := parser.Parse(context.Background(), parser.NewSource([]byte(">\t[a]: /u\n")))
	definition := partialTab.Document.References().Definitions[0].Properties()
	if partialTab.Status != parser.Complete || definition.Envelope.Start() != 1 || definition.Markers[0].Source.Start() != 2 || definition.Label.Segments[0].Source.Start() != 3 || partialTab.Document.Root().Children()[0].Markers()[0].Source.End() != 2 {
		t.Fatal("residual tab lost shared original-byte provenance", definition)
	}
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, terminal := range []string{ending, ""} {
			input := "- > [A]: <my url>" + ending + "  > 'one" + ending + "  > two'" + ending + "- [a]: /second" + terminal
			source := parser.NewSource([]byte(input))
			r := parser.Parse(context.Background(), source)
			if r.Failure != nil || r.Status != parser.Complete {
				t.Fatal(r)
			}
			refs := r.Document.References()
			if len(refs.Definitions) != 2 || refs.Lookup["a"] != 0 {
				t.Fatal(refs)
			}
			first := refs.Definitions[0].Properties()
			if first.Envelope.Start() != 4 || first.Envelope.End() != strings.Index(input, "- [a]") || first.Title == nil || referenceRaw(t, source, *first.Title) != "one"+ending+"two" || first.Title.Semantic != parser.Some("one\ntwo") {
				t.Fatal("container title/source mapping", first)
			}
			for _, m := range first.Markers {
				raw, _ := source.Text(m.Source)
				if !strings.Contains("[]:<>''", raw) || len(raw) != 1 {
					t.Fatal("marker lost source", m, raw)
				}
			}
			list := r.Document.Root().Children()[0]
			if list.Properties().List.Tight != parser.Some(true) || len(list.Children()) != 2 || len(list.Children()[0].Children()[0].Children()) != 0 {
				t.Fatal("empty nested containers lost")
			}
			// Mutate every returned owning container/handle, then compare against a
			// fresh parse on the same immutable snapshot.
			refs.Lookup["a"] = 1
			refs.Definitions[0].Properties().Label.Segments[0].Semantic = parser.Some("bad")
			*refs.Definitions[0] = parser.ReferenceDefinition{}
			refs.Definitions[1] = nil
			if !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
				t.Fatal("accessors changed immutable parse")
			}
		}
	}
	for _, input := range []string{"> [a]: /u\noutside\n", "- [a]: /u\noutside\n"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		nodes := r.Document.Root().Children()
		if r.Failure != nil || len(nodes) != 2 || nodes[1].Kind() != parser.Paragraph || referenceNodeText(nodes[1]) != "outside" {
			t.Fatal("definition-only block acquired lazy outside text", input, r)
		}
	}
	// Reference-looking text cannot interrupt an existing lazy paragraph.
	r := parser.Parse(context.Background(), parser.NewSource([]byte("> text\n[a]: /u\n")))
	if len(r.Document.References().Definitions) != 0 || referenceNodeText(r.Document.Root().Children()[0].Children()[0]) != "text\n[a]: /u" {
		t.Fatal("definition interrupted lazy paragraph")
	}
}

func TestReferenceListBoundaries317(t *testing.T) {
	input := "- a\n- b\n\n  [ref]: /url\n- d\n"
	r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
	list := r.Document.Root().Children()[0]
	if r.Failure != nil || list.Properties().List.Tight != parser.Some(false) || len(list.Children()) != 3 {
		t.Fatal("lost definition boundary/sibling", r)
	}
	checkReferenceRecords(t, r, input, []referenceWant{refWant("[ref]: /url\n", "ref", "/url", "ref")})
	for i, w := range []struct {
		item, paragraph byteRange
		text            string
	}{{byteRange{0, 4}, byteRange{2, 4}, "a"}, {byteRange{4, 23}, byteRange{6, 8}, "b"}, {byteRange{23, 27}, byteRange{25, 27}, "d"}} {
		n := list.Children()[i]
		if n.Span().Start() != w.item.start || n.Span().End() != w.item.end || len(n.Children()) != 1 || n.Children()[0].Span().Start() != w.paragraph.start || n.Children()[0].Span().End() != w.paragraph.end || referenceNodeText(n.Children()[0]) != w.text {
			t.Fatal("317 independent spans", i, n)
		}
	}
}

func TestReferenceSyntaxAndLiteralProtection(t *testing.T) {
	for _, tc := range []struct {
		input              string
		count              int
		destination, title string
		hasTitle           bool
	}{
		{"[a]: a(b(c)d)e\n", 1, "a(b(c)d)e", "", false},
		{"[a]: a(b\n", 0, "", "", false},
		{"[a]: a)b\n", 0, "", "", false},
		{"[a]: <bad\nurl>\n", 0, "", "", false},
		{"[a]: <a<b>\n", 0, "", "", false},
		{"[a]: <> ''\n", 1, "", "", true},
		{"[a]: /u (title)\n", 1, "/u", "title", true},
		{"[a]: /u (a(b))\n", 0, "", "", false},
		{"[a]: /u\n (bad\n", 1, "/u", "", false},
		{"[a]: /u \"bad\n", 0, "", "", false},
		{"[a]: /u \"good\" tail\n", 0, "", "", false},
		{"[a]: /u\n\"good\" tail\n", 1, "/u", "", false},
		{"[a]: \n\n/u\n", 0, "", "", false},
		{"[a]: /\x00u\n", 1, "/\ufffdu", "", false},
		{"<!--\n[a]: /u\n-->\n", 0, "", "", false},
		{"<script>\n[a]: /u\n</script>\n", 0, "", "", false},
		{"    [a]: /u\n", 0, "", "", false},
		{"```\n[a]: /u\n```\n", 0, "", "", false},
	} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(tc.input)))
		if r.Failure != nil {
			t.Fatal(tc.input, r.Failure)
		}
		refs := r.Document.References()
		if len(refs.Definitions) != tc.count {
			t.Fatalf("%q: %d definitions want %d", tc.input, len(refs.Definitions), tc.count)
		}
		if tc.count == 0 {
			continue
		}
		p := refs.Definitions[0].Properties()
		if p.Destination.Semantic != parser.Some(tc.destination) || (p.Title != nil) != tc.hasTitle || p.Title != nil && p.Title.Semantic != parser.Some(tc.title) {
			t.Fatal("component semantics", tc.input, p)
		}
		for _, m := range p.Markers {
			s, err := r.Source.Text(m.Source)
			if err != nil || len(s) != 1 || !strings.Contains("[]:<>()'\"", s) {
				t.Fatal("marker range", m)
			}
		}
	}
	// Empty and definition-only items survive, with local tightness rather than
	// leaking the inner list's looseness to the outer list.
	for _, tc := range []struct {
		input string
		tight bool
	}{
		{"- [a]: /u\n- [b]: /v\n", true},
		{"- [a]: /u\n\n- [b]: /v\n", false},
		{"- > [a]: /u\n- [b]: /v\n", true},
	} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(tc.input)))
		list := r.Document.Root().Children()[0]
		if r.Status != parser.Complete || list.Properties().List.Tight != parser.Some(tc.tight) || len(list.Children()) != 2 || len(r.Document.References().Definitions) != 2 {
			t.Fatal("definition-only item boundary", tc.input, r)
		}
	}
}

func TestReferenceListTightnessIsLocal(t *testing.T) {
	for _, tc := range []struct {
		input        string
		outer, inner bool
	}{
		{"- a\n  - b\n\n    [r]: /u\n  - c\n- d\n", true, false},
		{"- a\n\n  - [r]: /u\n  - c\n- d\n", false, true},
	} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(tc.input)))
		if r.Failure != nil {
			t.Fatal(r.Failure)
		}
		var lists []*parser.Node
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.List {
				lists = append(lists, n)
			}
			return true
		})
		if len(lists) != 2 || lists[0].Properties().List.Tight != parser.Some(tc.outer) || lists[1].Properties().List.Tight != parser.Some(tc.inner) || len(lists[0].Children()) != 2 || len(lists[1].Children()) != 2 {
			t.Fatal("definition boundary leaked inner/outer looseness", tc.input, lists)
		}
		refs := r.Document.References()
		if len(refs.Definitions) != 1 || refs.Lookup["r"] != 0 {
			t.Fatal("nested definition lost", refs)
		}
	}
}
