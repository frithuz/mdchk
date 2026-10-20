package parser_test

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

type referenceUseWant struct {
	start                   int
	use, origin, label, key string
	definition              int
}

func TestReferenceLinkApprovedPartitionAndUpstream(t *testing.T) {
	wants := map[int][]referenceUseWant{
		23:  {{0, "[foo]", "shortcut", "foo", "foo", 0}},
		33:  {{0, "[foo]", "shortcut", "foo", "foo", 0}},
		192: {{21, "[foo]", "shortcut", "foo", "foo", 0}},
		193: {{50, "[foo]", "shortcut", "foo", "foo", 0}},
		194: {{44, "[Foo*bar\\]]", "shortcut", "Foo*bar\\]", "Foo*bar\\]", 0}},
		195: {{29, "[Foo bar]", "shortcut", "Foo bar", "Foo bar", 0}},
		196: {{35, "[foo]", "shortcut", "foo", "foo", 0}},
		198: {{13, "[foo]", "shortcut", "foo", "foo", 0}},
		200: {{11, "[foo]", "shortcut", "foo", "foo", 0}},
		202: {{37, "[foo]", "shortcut", "foo", "foo", 0}},
		203: {{0, "[foo]", "shortcut", "foo", "foo", 0}},
		204: {{0, "[foo]", "shortcut", "foo", "foo", 0}},
		205: {{13, "[Foo]", "shortcut", "Foo", "Foo", 0}},
		206: {{19, "[αγω]", "shortcut", "αγω", "αγω", 0}},
		214: {{2, "[Foo]", "shortcut", "Foo", "Foo", 0}},
		215: {{20, "[foo]", "shortcut", "foo", "foo", 0}},
		216: {{16, "[foo]", "shortcut", "foo", "foo", 0}},
		217: {{63, "[foo]", "shortcut", "foo", "foo", 0}, {70, "[bar]", "shortcut", "bar", "bar", 1}, {77, "[baz]", "shortcut", "baz", "baz", 2}},
		218: {{0, "[foo]", "shortcut", "foo", "foo", 0}},
		527: {{0, "[foo][bar]", "full", "foo", "bar", 0}},
		528: {{0, "[link [foo [bar]]][ref]", "full", "link [foo [bar]]", "ref", 0}},
		529: {{0, "[link \\[bar][ref]", "full", "link \\[bar", "ref", 0}},
		530: {{0, "[link *foo **bar** `#`*][ref]", "full", "link *foo **bar** `#`*", "ref", 0}},
		532: {{17, "[ref]", "shortcut", "ref", "ref", 0}},
		533: {{10, "[baz][ref]", "full", "baz", "ref", 0}, {22, "[ref]", "shortcut", "ref", "ref", 0}},
		534: {{1, "[foo*][ref]", "full", "foo*", "ref", 0}},
		535: {{0, "[foo *bar][ref]", "full", "foo *bar", "ref", 0}},
		539: {{0, "[foo][BaR]", "full", "foo", "BaR", 0}},
		540: {{0, "[ẞ]", "shortcut", "ẞ", "ẞ", 0}},
		541: {{19, "[Baz][Foo bar]", "full", "Baz", "Foo bar", 0}},
		542: {{6, "[bar]", "shortcut", "bar", "bar", 0}},
		543: {{6, "[bar]", "shortcut", "bar", "bar", 0}},
		544: {{28, "[bar][foo]", "full", "bar", "foo", 0}},
		545: {},
		549: {{0, "[foo][ref\\[]", "full", "foo", "ref\\[", 0}},
		550: {{15, "[bar\\\\]", "shortcut", "bar\\\\", "bar\\\\", 0}},
		553: {{0, "[foo][]", "collapsed", "foo", "foo", 0}},
		554: {{0, "[*foo* bar][]", "collapsed", "*foo* bar", "*foo* bar", 0}},
		555: {{0, "[Foo][]", "collapsed", "Foo", "Foo", 0}},
		556: {{0, "[foo]", "shortcut", "foo", "foo", 0}},
		557: {{0, "[foo]", "shortcut", "foo", "foo", 0}},
		558: {{0, "[*foo* bar]", "shortcut", "*foo* bar", "*foo* bar", 0}},
		559: {{1, "[*foo* bar]", "shortcut", "*foo* bar", "*foo* bar", 0}},
		560: {{6, "[foo]", "shortcut", "foo", "foo", 0}},
		561: {{0, "[Foo]", "shortcut", "Foo", "Foo", 0}},
		562: {{0, "[foo]", "shortcut", "foo", "foo", 0}},
		564: {{15, "[foo*]", "shortcut", "foo*", "foo*", 0}},
		565: {{0, "[foo][bar]", "full", "foo", "bar", 1}},
		566: {{0, "[foo][]", "collapsed", "foo", "foo", 0}},
		568: {{0, "[foo]", "shortcut", "foo", "foo", 0}},
		569: {{5, "[bar][baz]", "full", "bar", "baz", 0}},
		570: {{0, "[foo][bar]", "full", "foo", "bar", 1}, {10, "[baz]", "shortcut", "baz", "baz", 0}},
		571: {{5, "[bar][baz]", "full", "bar", "baz", 0}},
		593: {{2, "[foo]", "shortcut", "foo", "foo", 0}},
	}
	seen := 0
	cases, err := emphasisExamples()
	if err != nil {
		t.Fatal(err)
	}
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
			var uses []*parser.Node
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() == parser.Link {
					checkLinkProvenance(t, r.Source, n)
					if n.Properties().Link.Origin != "inline" && n.Properties().Link.Origin != "autolink" {
						uses = append(uses, n)
					}
				}
				return true
			})
			if len(uses) != len(expected) {
				t.Fatal("reference count", len(uses), len(expected))
			}
			for i, w := range expected {
				n := uses[i]
				p := n.Properties().Link
				if raw, _ := r.Source.Text(n.Span()); raw != w.use || n.Span().Start() != w.start || n.Span().End() != w.start+len(w.use) || p.Origin != w.origin || p.Label.Semantic != parser.Some(w.label) || p.NormalizedLabel != parser.Some(referenceKeyOracle(t, w.key)) {
					t.Fatal("independent use/component identity", n, p, w)
				}
				refs := r.Document.References()
				if refs.Lookup[p.NormalizedLabel.Value] != w.definition {
					t.Fatal("independent selected-definition index", w)
				}
				marks := n.Markers()
				if marks[0].Source.Start() != w.start || marks[1].Source.Start() != w.start+len(w.label)+1 {
					t.Fatal("independent visible bracket ranges", marks, w)
				}
				if w.origin != "shortcut" {
					rawKey := w.key
					if w.origin == "collapsed" {
						rawKey = ""
					}
					if p.ReferenceLabel == nil || p.ReferenceLabel.Semantic != parser.Some(rawKey) || p.ReferenceLabel.Segments[0].Source.Start() != w.start+len(w.label)+3 || p.ReferenceLabel.Segments[len(p.ReferenceLabel.Segments)-1].Source.End() != w.start+len(w.use)-1 {
						t.Fatal("independent identifier ranges", p, w)
					}
				}
			}
		})
	}
	if seen != 54 {
		t.Fatal("missing independent acceptance cases", seen)
	}
}

// The oracle reads the retained Unicode source, independently of the generated
// production folding table. It performs no escape/entity interpretation.
var referenceFolding = sync.OnceValues(func() (map[rune]string, error) {
	data, err := os.ReadFile("testdata/unicode/17.0.0/CaseFolding.txt")
	if err != nil {
		return nil, err
	}
	m := map[rune]string{}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Split(strings.SplitN(line, "#", 2)[0], ";")
		if len(f) < 3 || (strings.TrimSpace(f[1]) != "C" && strings.TrimSpace(f[1]) != "F") {
			continue
		}
		from, err := strconv.ParseInt(strings.TrimSpace(f[0]), 16, 32)
		if err != nil {
			return nil, err
		}
		var to strings.Builder
		for _, v := range strings.Fields(f[2]) {
			r, err := strconv.ParseInt(v, 16, 32)
			if err != nil {
				return nil, err
			}
			to.WriteRune(rune(r))
		}
		m[rune(from)] = to.String()
	}
	return m, nil
})

func referenceKeyOracle(t *testing.T, raw string) string {
	t.Helper()
	fold, err := referenceFolding()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, r := range strings.ReplaceAll(raw, "\x00", "\ufffd") {
		if v, ok := fold[r]; ok {
			b.WriteString(v)
		} else {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.FieldsFunc(b.String(), func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' }), " ")
}

func checkReferenceRawValue(t *testing.T, source parser.Source, n *parser.Node, v parser.Value) string {
	t.Helper()
	var logical strings.Builder
	previous := n.Span().Start()
	if !v.Semantic.Present || len(v.Segments) == 0 {
		t.Fatal("unmapped raw reference component", v)
	}
	for _, part := range v.Segments {
		raw, err := source.Text(part.Source)
		if err != nil || part.Source.Start() < previous || part.Source.End() > n.Span().End() {
			t.Fatal("raw reference ownership", part, err)
		}
		want := strings.ReplaceAll(raw, "\x00", "\ufffd")
		if part.Transformation == parser.PartialTab {
			if raw != "\t" || len(part.Semantic.Value) < 1 || len(part.Semantic.Value) > 3 || strings.Trim(part.Semantic.Value, " ") != "" {
				t.Fatal("reference partial tab", part)
			}
			want = part.Semantic.Value
		} else if part.Transformation != parser.Exact && part.Transformation != parser.Normalized {
			t.Fatal("decoded reference identity", part)
		}
		if part.Semantic != parser.Some(want) {
			t.Fatal("reference logical spelling", part, want)
		}
		logical.WriteString(want)
		previous = part.Source.End()
	}
	if v.Semantic != parser.Some(logical.String()) {
		t.Fatal("reference component concatenation", v)
	}
	return logical.String()
}

func checkReferenceLinkProvenance(t *testing.T, source parser.Source, n *parser.Node) {
	t.Helper()
	p := n.Properties().Link
	if p == nil || p.Resolution != parser.Available || !p.NormalizedLabel.Present || !reflect.DeepEqual(n.Content(), p.Label.Segments) {
		t.Fatal("reference metadata", p)
	}
	markers := n.Markers()
	count := 4
	if p.Origin == "shortcut" {
		count = 2
	}
	if len(markers) != count {
		t.Fatal("reference marker count", markers)
	}
	for i, m := range markers {
		role, want := "label-open", "["
		switch i {
		case 1:
			role, want = "label-close", "]"
		case 2:
			role = "reference-open"
		case 3:
			role, want = "reference-close", "]"
		}
		raw, err := source.Text(m.Source)
		if err != nil || m.Role != role || raw != want || m.Source.End() != m.Source.Start()+1 {
			t.Fatal("reference delimiter", m, raw)
		}
	}
	if markers[0].Source.Start() != n.Span().Start() || markers[count-1].Source.End() != n.Span().End() {
		t.Fatal("reference envelope", n)
	}
	raw := checkReferenceRawValue(t, source, n, p.Label)
	if p.Label.Segments[0].Source.Start() != markers[0].Source.End() || p.Label.Segments[len(p.Label.Segments)-1].Source.End() != markers[1].Source.Start() {
		t.Fatal("reference label boundaries")
	}
	if p.Origin == "shortcut" {
		if p.ReferenceLabel != nil {
			t.Fatal("invented shortcut identifier")
		}
	} else {
		if p.ReferenceLabel == nil || markers[1].Source.End() != markers[2].Source.Start() {
			t.Fatal("missing/separated identifier")
		}
		key := checkReferenceRawValue(t, source, n, *p.ReferenceLabel)
		if p.ReferenceLabel.Segments[0].Source.Start() != markers[2].Source.End() || p.ReferenceLabel.Segments[len(p.ReferenceLabel.Segments)-1].Source.End() != markers[3].Source.Start() {
			t.Fatal("reference key boundaries")
		}
		if p.Origin == "full" {
			raw = key
		} else if p.Origin != "collapsed" || key != "" {
			t.Fatal("reference form", p)
		}
	}
	if p.NormalizedLabel != parser.Some(referenceKeyOracle(t, raw)) {
		t.Fatal("raw-key folding", raw, p)
	}
	// Reacquire the same immutable snapshot's registry. The independent fixture
	// table above supplies expected indices; this shared fuzz oracle also verifies
	// first-selection against every raw definition label and checks projections.
	refs := parser.Parse(context.Background(), source).Document.References()
	selected := -1
	for i, d := range refs.Definitions {
		if referenceKeyOracle(t, d.Properties().Label.Semantic.Value) == p.NormalizedLabel.Value {
			selected = i
			break
		}
	}
	if selected < 0 || refs.Lookup[p.NormalizedLabel.Value] != selected {
		t.Fatal("reference has no local first definition", p)
	}
	d := refs.Definitions[selected].Properties()
	projection := func(v parser.Value, want parser.Value) {
		if v.Semantic != want.Semantic || len(v.Segments) != 1 || v.Segments[0].Source.Present() || v.Segments[0].Transformation != parser.Synthetic || v.Segments[0].Semantic != v.Semantic {
			t.Fatal("false use-site projection", v, want)
		}
		// Decode original definition spelling independently of the production decoder.
		var text strings.Builder
		for _, s := range want.Segments {
			original, err := source.Text(s.Source)
			if err != nil {
				t.Fatal(err)
			}
			text.WriteString(original)
		}
		normalized := strings.ReplaceAll(strings.ReplaceAll(text.String(), "\r\n", "\n"), "\r", "\n")
		if v.Semantic != parser.Some(oracleText(normalized, entityOracle(t))) {
			t.Fatal("definition semantic oracle", v, normalized)
		}
	}
	projection(p.Destination, d.Destination)
	if (p.Title == nil) != (d.Title == nil) {
		t.Fatal("reference title presence")
	}
	if p.Title != nil {
		projection(*p.Title, *d.Title)
	}
	checkInlineSourceCoverage(t, n)
	for _, c := range n.Children() {
		parser.Walk(c, func(child *parser.Node) bool {
			if child.Kind() == parser.Link {
				t.Fatal("nested reference Link")
			}
			return true
		})
	}
}

func TestReferenceLinkRawIdentityAndLimits(t *testing.T) {
	for _, tt := range []struct {
		use, definition string
		match           bool
	}{
		{"ẞ", "SS", true}, {"Iİı", "ii\u0307ı", true}, {" a\tb ", "A b", true}, {"a\r\nb", "A b", true},
		{`a\!`, "a!", false}, {`a\!`, `a\!`, true}, {"a&amp;", "a&", false}, {"a&amp;", "a&amp;", true},
		{"a\u00a0b", "a b", false}, {"a\u00a0b", "a\u00a0b", true}, {"\u00a0", "\u00a0", true},
		{strings.Repeat("é", 999), strings.Repeat("é", 999), true},
		{strings.Repeat(" ", 998) + "a", "a", true}, {strings.Repeat(" ", 999) + "a", "a", false},
		{"a\\]b", "a\\]b", true}, {"a[b", "a[b", false}, {" \t ", "unused", false},
	} {
		for _, origin := range []string{"full", "collapsed", "shortcut"} {
			use := "[" + tt.use + "]"
			if origin == "full" {
				use = "[display][" + tt.use + "]"
			} else if origin == "collapsed" {
				use += "[]"
			}
			input := use + "\n\n[" + tt.definition + "]: /uri\n"
			t.Run(origin+"/"+strconv.Itoa(len(tt.use)), func(t *testing.T) {
				r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
				checkStagedTextOutcome(t, r, true)
				count := 0
				parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
					if n.Kind() == parser.Link {
						count++
						checkLinkProvenance(t, r.Source, n)
						if n.Properties().Link.Origin != origin {
							t.Fatal("wrong form", n.Properties())
						}
					}
					return true
				})
				want := 0
				if tt.match {
					want = 1
				}
				if count != want {
					t.Fatal("raw normalization/eligibility", origin, tt.use, count, want)
				}
			})
		}
	}
	// The visible text of a full reference is not subject to identifier length.
	input := "[" + strings.Repeat("é", 1000) + "][key]\n\n[key]: /uri\n"
	r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
	checkStagedTextOutcome(t, r, true)
	if n := r.Document.Root().Children()[0].Children()[0]; n.Kind() != parser.Link || n.Properties().Link.Origin != "full" {
		t.Fatal("bounded visible text as identifier", n)
	}
}

func TestReferenceLinkMappedOwnershipAndIsolation(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, terminal := range []string{"", ending} {
			for _, text := range []string{
				"[é\x00 *β*][KEY] [key][] [key]",
				"# [é\x00][key] #", ">\t- [é\x00][key]", "> [é\x00" + ending + ">\tβ][key]", "[é\x00][key]" + ending + "===",
				"> - [é\x00" + ending + ">   *β*][k" + ending + ">   ey]",
				"> [a" + ending + "lazy][key]",
			} {
				key := "key"
				if strings.Contains(text, "[k"+ending) {
					key = "k ey"
				}
				input := text + ending + ending + "> [" + key + "]: /a&amp;b \"\"" + ending + "[" + key + "]: /second" + terminal
				source := parser.NewSource([]byte(input))
				r := parser.Parse(context.Background(), source)
				checkStagedTextOutcome(t, r, true)
				count := 0
				parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
					if n.Kind() != parser.Link {
						return true
					}
					count++
					checkLinkProvenance(t, source, n)
					p := n.Properties().Link
					refs := r.Document.References()
					d := refs.Definitions[refs.Lookup[p.NormalizedLabel.Value]].Properties()
					if p.Destination.Semantic != parser.Some("/a&b") || p.Title == nil || p.Title.Semantic != parser.Some("") || d.Destination.Segments[0].Source.Start() != strings.Index(input, "/a&amp;b") {
						t.Fatal("definition provenance and first selection", p, d)
					}
					if n.Span().Start() != strings.Index(input, "[") && count == 1 {
						t.Fatal("mapped use start")
					}
					p.Label.Segments[0].Source = parser.Span{}
					p.Destination.Segments[0].Semantic = parser.Some("bad")
					if p.ReferenceLabel != nil {
						p.ReferenceLabel.Segments[0].Source = parser.Span{}
						*p.ReferenceLabel = parser.Value{}
					}
					if p.Title != nil {
						p.Title.Semantic = parser.Some("bad")
					}
					*n = parser.Node{}
					return true
				})
				if count == 0 || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) || string(source.Bytes()) != input {
					t.Fatal("mapped identity/immutability", input)
				}
			}
		}
	}
	for _, use := range []string{`\[a]`, "&#91;a]", "`[a]`", "    [a]", "~~~\n[a]\n~~~", "<!-- [a] -->", "x <i title='[a]'>"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(use+"\n\n[a]: /uri\n")))
		checkStagedTextOutcome(t, r, true)
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.Link {
				t.Fatal("opaque/generated reference", use)
			}
			return true
		})
	}
	// Unrelated definitions prove misses literal, and per-call state cannot resolve
	// a key from a preceding document. Images still keep the parse incomplete.
	for _, input := range []string{"[absent][missing]\n\n[other]: /u\n", "[absent][]\n\n[other]: /u\n", "[key]"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.Link {
				t.Fatal("unresolved/cross-call reference", input)
			}
			return true
		})
	}
}

func TestReferenceLinkIdenticalDuplicateDefinitions(t *testing.T) {
	const use = "[visible][KEY] [key][] [key]"
	const definitions = "[key]: /same \"same\"\n[KEY]: /same \"same\"\n"
	for _, forward := range []bool{false, true} {
		input := definitions + "\n" + use
		if forward {
			input = use + "\n\n" + definitions
		}
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
		refs := r.Document.References()
		if len(refs.Definitions) != 2 || refs.Lookup["key"] != 0 || refs.Definitions[0].Properties().Envelope.Start() != strings.Index(input, "[key]:") || refs.Definitions[1].Properties().Envelope.Start() != strings.Index(input, "[KEY]:") {
			t.Fatal("equal values obscured source identity", refs)
		}
		count := 0
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.Link {
				count++
				checkLinkProvenance(t, r.Source, n)
				p := n.Properties().Link
				d := refs.Definitions[refs.Lookup[p.NormalizedLabel.Value]].Properties()
				if d.Destination.Segments[0].Source.Start() != strings.Index(input, "/same") || p.Destination.Semantic != parser.Some("/same") {
					t.Fatal("selected later equal-valued definition", p, d)
				}
			}
			return true
		})
		if count != 3 {
			t.Fatal("missing reference form", count)
		}
	}
}
