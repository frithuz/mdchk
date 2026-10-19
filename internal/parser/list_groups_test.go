package parser_test

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

// Earlier item/leaf fixtures continue to check every independently specified
// source fact through the new List wrappers. TestListGroupingUpstreamTargets
// separately asserts complete List ancestry and metadata without this projection.
func flattenListGroups(nodes []*parser.Node) []*parser.Node {
	var items []*parser.Node
	for _, n := range nodes {
		children := n.Children()
		validGroup := n.Kind() == parser.List && len(children) > 0
		for _, c := range children {
			validGroup = validGroup && c.Kind() == parser.ListItem
		}
		if validGroup {
			items = append(items, children...)
		} else {
			items = append(items, n)
		}
	}
	return items
}

func TestListGroupingEarlierConsumers(t *testing.T) {
	type wantGroup struct {
		path               []int
		a, b, start, items int
		marker             byte
		tight              bool
	}
	wants := map[int][]wantGroup{
		57:  {{[]int{0}, 0, 6, 0, 1, '-', true}, {[]int{2}, 10, 16, 0, 1, '-', true}},
		60:  {{[]int{0}, 0, 6, 0, 1, '*', true}, {[]int{2}, 12, 18, 0, 1, '*', true}},
		61:  {{[]int{0}, 0, 14, 0, 2, '-', true}},
		94:  {{[]int{0}, 0, 6, 0, 1, '-', true}},
		99:  {{[]int{0}, 0, 6, 0, 1, '-', true}},
		108: {{[]int{0}, 0, 17, 0, 1, '-', false}},
		109: {{[]int{0}, 0, 19, 1, 1, '.', false}, {[]int{0, 0, 1}, 13, 19, 0, 1, '-', true}},
		175: {{[]int{0}, 0, 14, 0, 2, '-', true}},
		235: {{[]int{0, 0}, 2, 8, 0, 1, '-', true}, {[]int{1}, 8, 14, 0, 1, '-', true}},
	}
	data, err := os.ReadFile("testdata/commonmark/0.31.2/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples []struct {
		ID       int `json:"example"`
		Markdown string
	}
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range examples {
		groups, ok := wants[e.ID]
		if !ok {
			continue
		}
		seen++
		t.Run(strconv.Itoa(e.ID), func(t *testing.T) {
			r := parser.Parse(context.Background(), parser.NewSource([]byte(e.Markdown)))
			if r.Failure != nil || r.Status != parser.Incomplete {
				t.Fatal("wrong staged result", r)
			}
			count := 0
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() == parser.List {
					count++
				}
				return true
			})
			if count != len(groups) {
				t.Fatal("unexpected list ancestry")
			}
			for _, w := range groups {
				n := r.Document.Root()
				for _, j := range w.path {
					children := n.Children()
					if j >= len(children) {
						t.Fatal("missing ancestry", w.path)
					}
					n = children[j]
				}
				p := n.Properties().List
				if n.Kind() != parser.List || n.Span().Start() != w.a || n.Span().End() != w.b || len(n.Children()) != w.items || p.Tight != parser.Some(w.tight) {
					t.Fatal("wrong list ancestry, envelope or tightness", w)
				}
				if w.start != 0 {
					if !p.Ordered || p.Start != parser.Some(w.start) || p.Delimiter != parser.Some(w.marker) {
						t.Fatal("wrong ordered metadata")
					}
				} else if p.Ordered || p.Bullet != parser.Some(w.marker) {
					t.Fatal("wrong bullet metadata")
				}
			}
		})
	}
	if seen != len(wants) {
		t.Fatal("missing earlier input")
	}
}

func ungroupedChildren(n *parser.Node) []*parser.Node { return flattenListGroups(n.Children()) }

func bulletGroup(a, b int, bullet byte, tight bool, items ...listTreeWant) listTreeWant {
	return listTreeWant{kind: parser.List, span: byteRange{a, b}, children: items,
		list: &parser.ListProperties{Bullet: parser.Some(bullet), Tight: parser.Some(tight)}}
}

func orderedGroup(a, b, start int, delimiter byte, tight bool, items ...listTreeWant) listTreeWant {
	w := bulletGroup(a, b, '-', tight, items...)
	w.list = &parser.ListProperties{Ordered: true, Start: parser.Some(start), Delimiter: parser.Some(delimiter), Tight: parser.Some(tight)}
	return w
}

func TestListGroupingUpstreamTargets(t *testing.T) {
	// Independently specified original-byte trees, not trees produced by Parse
	// or inferred from renderer output. Existing item fixtures separately retain
	// marker/prefix/indent facts; these fixtures assert the new ancestry too.
	p, i, q, c := paragraphTree, itemTree, quoteTree, codeTree
	u, o := bulletGroup, orderedGroup
	html := func(a, b int, text string) listTreeWant {
		return listTreeWant{kind: parser.HTMLBlock, span: byteRange{a, b}, text: text, content: []byteRange{{a, b}}}
	}
	wants := map[int][]listTreeWant{
		301: {u(0, 12, '-', true, i(0, 6, p(2, 6, "foo", byteRange{2, 5})), i(6, 12, p(8, 12, "bar", byteRange{8, 11}))), u(12, 18, '+', true, i(12, 18, p(14, 18, "baz", byteRange{14, 17})))},
		302: {o(0, 14, 1, '.', true, i(0, 7, p(3, 7, "foo", byteRange{3, 6})), i(7, 14, p(10, 14, "bar", byteRange{10, 13}))), o(14, 21, 3, ')', true, i(14, 21, p(17, 21, "baz", byteRange{17, 20})))},
		303: {p(0, 4, "Foo", byteRange{0, 3}), u(4, 16, '-', true, i(4, 10, p(6, 10, "bar", byteRange{6, 9})), i(10, 16, p(12, 16, "baz", byteRange{12, 15})))},
		304: {p(0, 68, "The number of windows in my house is\n14.  The number of doors is 6.", byteRange{0, 37}, byteRange{37, 67})},
		305: {p(0, 37, "The number of windows in my house is", byteRange{0, 36}), o(37, 67, 1, '.', true, i(37, 67, p(41, 67, "The number of doors is 6.", byteRange{41, 66})))},
		306: {u(0, 21, '-', false, i(0, 7, p(2, 6, "foo", byteRange{2, 5})), i(7, 15, p(9, 13, "bar", byteRange{9, 12})), i(15, 21, p(17, 21, "baz", byteRange{17, 20})))},
		307: {u(0, 36, '-', true, i(0, 36, p(2, 6, "foo", byteRange{2, 5}), u(8, 36, '-', true, i(8, 36, p(10, 14, "bar", byteRange{10, 13}), u(18, 36, '-', false, i(18, 36, p(20, 24, "baz", byteRange{20, 23}), p(32, 36, "bim", byteRange{32, 35})))))))},
		308: {u(0, 13, '-', true, i(0, 6, p(2, 6, "foo", byteRange{2, 5})), i(6, 13, p(8, 12, "bar", byteRange{8, 11}))), html(13, 22, "<!-- -->\n"), u(23, 35, '-', true, i(23, 29, p(25, 29, "baz", byteRange{25, 28})), i(29, 35, p(31, 35, "bim", byteRange{31, 34})))},
		309: {u(0, 31, '-', false, i(0, 22, p(4, 8, "foo", byteRange{4, 7}), p(13, 21, "notcode", byteRange{13, 20})), i(22, 31, p(26, 30, "foo", byteRange{26, 29}))), html(31, 40, "<!-- -->\n"), c(41, 50, "code\n", byteRange{45, 49}, byteRange{49, 50})},
		310: {u(0, 37, '-', true, i(0, 4, p(2, 4, "a", byteRange{2, 3})), i(4, 9, p(7, 9, "b", byteRange{7, 8})), i(9, 15, p(13, 15, "c", byteRange{13, 14})), i(15, 22, p(20, 22, "d", byteRange{20, 21})), i(22, 28, p(26, 28, "e", byteRange{26, 27})), i(28, 33, p(31, 33, "f", byteRange{31, 32})), i(33, 37, p(35, 37, "g", byteRange{35, 36})))},
		311: {o(0, 22, 1, '.', false, i(0, 6, p(3, 5, "a", byteRange{3, 4})), i(6, 14, p(11, 13, "b", byteRange{11, 12})), i(14, 22, p(20, 22, "c", byteRange{20, 21})))},
		312: {u(0, 30, '-', true, i(0, 4, p(2, 4, "a", byteRange{2, 3})), i(4, 9, p(7, 9, "b", byteRange{7, 8})), i(9, 15, p(13, 15, "c", byteRange{13, 14})), i(15, 30, p(20, 30, "d\n- e", byteRange{20, 22}, byteRange{26, 29})))},
		313: {o(0, 14, 1, '.', false, i(0, 6, p(3, 5, "a", byteRange{3, 4})), i(6, 14, p(11, 13, "b", byteRange{11, 12}))), c(14, 23, "3. c\n", byteRange{18, 22}, byteRange{22, 23})},
		314: {u(0, 13, '-', false, i(0, 4, p(2, 4, "a", byteRange{2, 3})), i(4, 9, p(6, 8, "b", byteRange{6, 7})), i(9, 13, p(11, 13, "c", byteRange{11, 12})))},
		315: {u(0, 11, '*', false, i(0, 4, p(2, 4, "a", byteRange{2, 3})), i(4, 6), i(7, 11, p(9, 11, "c", byteRange{9, 10})))},
		316: {u(0, 17, '-', false, i(0, 4, p(2, 4, "a", byteRange{2, 3})), i(4, 13, p(6, 8, "b", byteRange{6, 7}), p(11, 13, "c", byteRange{11, 12})), i(13, 17, p(15, 17, "d", byteRange{15, 16})))},
		317: {u(0, 23, '-', false, i(0, 4, p(2, 4, "a", byteRange{2, 3})), i(4, 23, p(6, 8, "b", byteRange{6, 7})))},
		318: {u(0, 26, '-', true, i(0, 4, p(2, 4, "a", byteRange{2, 3})), i(4, 22, c(6, 22, "b\n\n\n", byteRange{12, 13}, byteRange{13, 14}, byteRange{14, 15}, byteRange{15, 16})), i(22, 26, p(24, 26, "c", byteRange{24, 25})))},
		319: {u(0, 21, '-', true, i(0, 17, p(2, 4, "a", byteRange{2, 3}), u(6, 17, '-', false, i(6, 17, p(8, 10, "b", byteRange{8, 9}), p(15, 17, "c", byteRange{15, 16})))), i(17, 21, p(19, 21, "d", byteRange{19, 20})))},
		320: {u(0, 18, '*', true, i(0, 14, p(2, 4, "a", byteRange{2, 3}), q(6, 14, p(8, 10, "b", byteRange{8, 9}))), i(14, 18, p(16, 18, "c", byteRange{16, 17})))},
		321: {u(0, 30, '-', true, i(0, 26, p(2, 4, "a", byteRange{2, 3}), q(6, 10, p(8, 10, "b", byteRange{8, 9})), c(12, 26, "c\n", byteRange{18, 19}, byteRange{19, 20})), i(26, 30, p(28, 30, "d", byteRange{28, 29})))},
		322: {u(0, 4, '-', true, i(0, 4, p(2, 4, "a", byteRange{2, 3})))},
		323: {u(0, 10, '-', true, i(0, 10, p(2, 4, "a", byteRange{2, 3}), u(6, 10, '-', true, i(6, 10, p(8, 10, "b", byteRange{8, 9})))))},
		324: {o(0, 29, 1, '.', false, i(0, 29, c(3, 21, "foo\n", byteRange{10, 13}, byteRange{13, 14}), p(25, 29, "bar", byteRange{25, 28})))},
		325: {u(0, 21, '*', false, i(0, 21, p(2, 6, "foo", byteRange{2, 5}), u(8, 15, '*', true, i(8, 15, p(10, 14, "bar", byteRange{10, 13}))), p(17, 21, "baz", byteRange{17, 20})))},
		326: {u(0, 33, '-', false, i(0, 17, p(2, 4, "a", byteRange{2, 3}), u(6, 17, '-', true, i(6, 10, p(8, 10, "b", byteRange{8, 9})), i(12, 17, p(14, 16, "c", byteRange{14, 15})))), i(17, 33, p(19, 21, "d", byteRange{19, 20}), u(23, 33, '-', true, i(23, 27, p(25, 27, "e", byteRange{25, 26})), i(29, 33, p(31, 33, "f", byteRange{31, 32})))))},
	}
	// The definition provider is absent: the recognized prefix is retained, but
	// the unexamined suffix cannot establish final list tightness or a third item.
	wants[317][0].list.Tight = parser.Optional[bool]{}
	markers := map[int][]byteRange{
		301: {{0, 1}, {6, 7}, {12, 13}}, 302: {{0, 2}, {7, 9}, {14, 16}}, 303: {{4, 5}, {10, 11}}, 304: {}, 305: {{37, 39}},
		306: {{0, 1}, {7, 8}, {15, 16}}, 307: {{0, 1}, {8, 9}, {18, 19}}, 308: {{0, 1}, {6, 7}, {23, 24}, {29, 30}}, 309: {{0, 1}, {22, 23}},
		310: {{0, 1}, {5, 6}, {11, 12}, {18, 19}, {24, 25}, {29, 30}, {33, 34}}, 311: {{0, 2}, {8, 10}, {17, 19}}, 312: {{0, 1}, {5, 6}, {11, 12}, {18, 19}}, 313: {{0, 2}, {8, 10}},
		314: {{0, 1}, {4, 5}, {9, 10}}, 315: {{0, 1}, {4, 5}, {7, 8}}, 316: {{0, 1}, {4, 5}, {13, 14}}, 317: {{0, 1}, {4, 5}}, 318: {{0, 1}, {4, 5}, {22, 23}},
		319: {{0, 1}, {6, 7}, {17, 18}}, 320: {{0, 1}, {14, 15}}, 321: {{0, 1}, {26, 27}}, 322: {{0, 1}}, 323: {{0, 1}, {6, 7}}, 324: {{0, 2}}, 325: {{0, 1}, {8, 9}},
		326: {{0, 1}, {6, 7}, {12, 13}, {17, 18}, {23, 24}, {29, 30}},
	}
	data, err := os.ReadFile("testdata/commonmark/0.31.2/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples []struct {
		ID                      int `json:"example"`
		Markdown, HTML, Section string
	}
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, e := range examples {
		if e.ID < 301 || e.ID > 326 {
			continue
		}
		if seen[e.ID] || e.Section != "Lists" {
			t.Fatal("wrong upstream identity", e.ID)
		}
		seen[e.ID] = true
		t.Run(strconv.Itoa(e.ID), func(t *testing.T) {
			r := checkListTrees(t, e.Markdown, wants[e.ID])
			checkGroupedItemMarkers(t, r, markers[e.ID])
			if r.Status != parser.Incomplete || r.AST != parser.Partial || conformance.Evaluate(r).Status != "pending" {
				t.Fatal("later reference/inline providers invented", r)
			}
		})
	}
	if len(seen) != 26 || len(wants) != 26 {
		t.Fatal("incomplete Lists coverage")
	}
}

func checkGroupedItemMarkers(t *testing.T, r parser.ParseResult, want []byteRange) {
	t.Helper()
	i := 0
	parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
		if n.Kind() != parser.ListItem {
			return true
		}
		if i >= len(want) {
			t.Fatal("unexpected item marker")
		}
		m, p := n.Markers(), n.Properties().List
		w := want[i]
		if len(m) < 2 || m[1].Role != "item-marker" || m[1].Source.Start() != w.start || m[1].Source.End() != w.end {
			t.Fatal("original marker range", i, m, w)
		}
		raw, err := r.Source.Text(m[1].Source)
		if err != nil || p.MarkerWidth != parser.Some(w.end-w.start) || p.Prefix == nil {
			t.Fatal("lost item facts")
		}
		if p.Ordered {
			start, err := strconv.Atoi(raw[:len(raw)-1])
			if err != nil || p.Start != parser.Some(start) || p.Delimiter != parser.Some(raw[len(raw)-1]) {
				t.Fatal("lost ordinal spelling")
			}
		} else if p.Bullet != parser.Some(raw[0]) {
			t.Fatal("lost bullet spelling")
		}
		for _, s := range p.Prefix.Segments {
			if _, err := r.Source.Text(s.Source); err != nil {
				t.Fatal("prefix ownership", err)
			}
		}
		i++
		return true
	})
	if i != len(want) {
		t.Fatal("missing item markers", i, len(want))
	}
}

func TestListGroupingCompleteSources(t *testing.T) {
	for _, tt := range []struct {
		input, html string
		want        []listTreeWant
	}{
		{"*\n", "<ul>\n<li></li>\n</ul>\n", []listTreeWant{bulletGroup(0, 2, '*', true, itemTree(0, 2))}},
		{"-\t\tfoo\n", "<ul>\n<li>\n<pre><code>  foo\n</code></pre>\n</li>\n</ul>\n", []listTreeWant{bulletGroup(0, 7, '-', true, itemTree(0, 7, codeTree(1, 7, "  foo\n", byteRange{2, 3}, byteRange{3, 6}, byteRange{6, 7})))}},
	} {
		r := checkListTrees(t, tt.input, tt.want)
		got := conformance.Evaluate(r)
		if r.Status != parser.Complete || r.AST != parser.Available || r.Phases != (parser.PhaseProgress{Blocks: parser.PhaseComplete, Inlines: parser.PhaseComplete}) || len(r.Uncertainty) != 0 || r.Document.References().LookupAvailability != parser.Available || got.Status != "rendered" || got.HTML != tt.html {
			t.Fatal("wrong complete-list result", r, got)
		}
	}
}

func TestListBlankBoundaryOwnership(t *testing.T) {
	for _, tt := range []struct {
		input string
		tight []bool
	}{
		{"- ```\n\n- #\n", []bool{true}}, // Blank is still inside the unclosed fence.
		{"- <pre>\n\n- #\n", []bool{true}},
		{"- > #\n  >\n- #\n", []bool{true}},
		{"- > #\n\n- #\n", []bool{false}},
		{"-\n\n- #\n", []bool{false}},
		{"> -\n>\n> - #\n", []bool{false}},
		{"- #\n\n", []bool{true}},
		{"-\n\n", []bool{true}},
		{"- #\n  - #\n\n  #\n", []bool{false, true}},
	} {
		for _, ending := range []string{"\n", "\r\n", "\r"} {
			for _, eof := range []bool{false, true} {
				input := strings.ReplaceAll(tt.input, "\n", ending)
				if eof {
					input = strings.TrimSuffix(input, ending)
				}
				t.Run(strconv.Quote(input), func(t *testing.T) {
					r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
					if r.Failure != nil || r.Status != parser.Complete || conformance.Evaluate(r).Status != "rendered" {
						t.Fatal("inline-free list incomplete", r)
					}
					i := 0
					parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
						if n.Kind() == parser.List {
							if i >= len(tt.tight) || n.Properties().List.Tight != parser.Some(tt.tight[i]) {
								t.Fatal("blank crossed structural boundary", i, n.Properties().List)
							}
							i++
						}
						return true
					})
					if i != len(tt.tight) || string(r.Source.Bytes()) != input {
						t.Fatal("lost structure or source")
					}
				})
			}
		}
	}
}

func TestListOrderedStartAndReferenceBoundaries(t *testing.T) {
	r := parser.Parse(context.Background(), parser.NewSource([]byte("003. a\n9. b\n7) c\n")))
	lists := r.Document.Root().Children()
	if len(lists) != 2 || lists[0].Properties().List.Start != parser.Some(3) || lists[1].Properties().List.Start != parser.Some(7) {
		t.Fatal("lost initial starts or delimiter split")
	}
	checkGroupedItemMarkers(t, r, []byteRange{{0, 4}, {7, 9}, {12, 14}})
	for _, input := range []string{"- [r]: /url\n- unseen\n", "- > [r]: /url\n- unseen\n", "- #\n  - [r]: /url\n- unseen\n"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		if r.Status != parser.Incomplete || conformance.Evaluate(r).Status != "pending" {
			t.Fatal("reference suffix invented")
		}
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.List && n.Properties().List.Tight.Present {
				t.Fatal("partial reference list acquired final tightness")
			}
			return true
		})
	}
}

func FuzzListGroupingBoundaries(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 0, 4}, false, false, uint8(0))
	f.Add([]byte{1, 1, 0, 0, 1}, true, true, uint8(1))
	f.Fuzz(func(t *testing.T, choices []byte, ordered, quoted bool, lineEnding uint8) {
		if len(choices) == 0 {
			return
		}
		if len(choices) > 32 {
			choices = choices[:32]
		}
		ending := []string{"\n", "\r\n", "\r"}[lineEnding%3]
		prefix := ""
		if quoted {
			prefix = "> "
		}
		type group struct {
			marker       byte
			start, count int
			tight        bool
		}
		var groups []group
		var input strings.Builder
		for j, choice := range choices {
			marker := "-+*"[choice%3]
			spelling := string(marker)
			if ordered {
				marker = ".)"[choice%2]
				spelling = strconv.Itoa(j+2) + string(marker)
			}
			if len(groups) == 0 || groups[len(groups)-1].marker != marker {
				groups = append(groups, group{marker: marker, start: j + 2, tight: true})
			} else if choices[j-1]&4 != 0 {
				groups[len(groups)-1].tight = false
			}
			groups[len(groups)-1].count++
			input.WriteString(prefix + spelling + " #" + ending)
			if choice&4 != 0 {
				input.WriteString(prefix + ending)
			}
		}
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input.String())))
		if r.Failure != nil || r.Status != parser.Complete || conformance.Evaluate(r).Status != "rendered" {
			t.Fatal("generated inline-free groups incomplete", r)
		}
		nodes := r.Document.Root().Children()
		if quoted {
			nodes = nodes[0].Children()
		}
		if len(nodes) != len(groups) {
			t.Fatal("wrong group count", len(nodes), len(groups))
		}
		for j, want := range groups {
			n := nodes[j]
			p := n.Properties().List
			if n.Kind() != parser.List || p == nil || p.Ordered != ordered || p.Tight != parser.Some(want.tight) || len(n.Children()) != want.count {
				t.Fatal("wrong grouping or tightness", j, p, want)
			}
			if ordered && (p.Start != parser.Some(want.start) || p.Delimiter != parser.Some(want.marker)) || !ordered && p.Bullet != parser.Some(want.marker) {
				t.Fatal("wrong list type/start")
			}
			for _, item := range n.Children() {
				if item.Kind() != parser.ListItem || len(item.Children()) != 1 || item.Children()[0].Kind() != parser.Heading {
					t.Fatal("lost item or leaf")
				}
			}
		}
		if string(r.Source.Bytes()) != input.String() {
			t.Fatal("source changed")
		}
	})
}
