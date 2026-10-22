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

func taskItems(root *Node) []*Node {
	var out []*Node
	Walk(root, func(n *Node) bool {
		if n.Kind() == ListItem && n.spec.Properties.Task != nil {
			out = append(out, n)
		}
		return true
	})
	return out
}
func assertTaskSpan(t *testing.T, got Span, start, end int) {
	t.Helper()
	if !got.Present() || got.Start() != start || got.End() != end {
		t.Fatalf("span = [%d,%d), want [%d,%d)", got.Start(), got.End(), start, end)
	}
}
func assertTask(t *testing.T, item *Node, itemStart, itemEnd, paraStart, paraEnd, opening, checkStart, checkEnd, closing, separatorEnd int, spelling, payload string) {
	t.Helper()
	assertTaskSpan(t, item.Span(), itemStart, itemEnd)
	if item.Kind() != ListItem || len(item.spec.Children) == 0 || item.spec.Children[0].Kind() != Paragraph {
		t.Fatal("core item/first paragraph lost")
	}
	para := item.spec.Children[0]
	p := item.Properties().Task
	if p == nil || !sameTask(p, para.Properties().Task) || p.Checked != (spelling == "x" || spelling == "X") || p.CheckSpelling != spelling {
		t.Fatalf("state/association: %+v", p)
	}
	assertTaskSpan(t, para.Span(), paraStart, paraEnd)
	assertTaskSpan(t, p.Paragraph, paraStart, paraEnd)
	assertTaskSpan(t, p.Marker, opening, closing+1)
	assertTaskSpan(t, p.Opening, opening, opening+1)
	assertTaskSpan(t, p.Check, checkStart, checkEnd)
	assertTaskSpan(t, p.Closing, closing, closing+1)
	assertTaskSpan(t, p.Separator, closing+1, separatorEnd)
	if tableText(para) != payload {
		t.Fatalf("payload %q want %q", tableText(para), payload)
	}
	if tableRaw(t, item.Span().source, p.Check) != spelling {
		t.Fatal("check provenance")
	}
	for _, part := range para.Content() {
		if part.Source.Start() < separatorEnd {
			t.Fatal("task syntax leaked into logical Content")
		}
	}
}
func originalTaskInput(t *testing.T, id int) string {
	t.Helper()
	data, err := os.ReadFile("testdata/gfm/0.29-gfm/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Example  int
		Markdown string
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if c.Example == id {
			return c.Markdown
		}
	}
	t.Fatal("original absent")
	return ""
}
func TestTaskPublished279(t *testing.T) {
	input := originalTaskInput(t, 279)
	if input != "- [ ] foo\n- [x] bar\n" {
		t.Fatal("immutable input changed")
	}
	r := parseTableStage(t, input)
	items := taskItems(r.Document.root)
	if len(items) != 2 {
		t.Fatal("two actual tasks required")
	}
	assertTask(t, items[0], 0, 10, 2, 10, 2, 3, 4, 4, 6, " ", "foo")
	assertTask(t, items[1], 10, 20, 12, 20, 12, 13, 14, 14, 16, "x", "bar")
	list := r.Document.root.spec.Children[0]
	if list.Kind() != List || list.spec.Properties.List.Tight != Some(true) {
		t.Fatal("tight core list lost")
	}
	for i, item := range items {
		marker := item.Markers()[1]
		assertTaskSpan(t, marker.Source, i*10, i*10+1)
		if marker.Role != "item-marker" || tableRaw(t, r.Source, marker.Source) != "-" {
			t.Fatal("core list token changed")
		}
	}
	assertTaskDisabled(t, r.Source)
}
func TestTaskPublished280(t *testing.T) {
	input := originalTaskInput(t, 280)
	if input != "- [x] foo\n  - [ ] bar\n  - [x] baz\n- [ ] bim\n" {
		t.Fatal("immutable input changed")
	}
	r := parseTableStage(t, input)
	items := taskItems(r.Document.root)
	if len(items) != 4 {
		t.Fatal("four actual tasks required")
	}
	assertTask(t, items[0], 0, 34, 2, 10, 2, 3, 4, 4, 6, "x", "foo")
	assertTask(t, items[1], 12, 22, 14, 22, 14, 15, 16, 16, 18, " ", "bar")
	assertTask(t, items[2], 24, 34, 26, 34, 26, 27, 28, 28, 30, "x", "baz")
	assertTask(t, items[3], 34, 44, 36, 44, 36, 37, 38, 38, 40, " ", "bim")
	outer := r.Document.root.spec.Children[0]
	nested := items[0].spec.Children[1]
	assertTaskSpan(t, outer.Span(), 0, 44)
	assertTaskSpan(t, nested.Span(), 12, 34)
	if nested.Kind() != List || len(nested.spec.Children) != 2 || len(outer.spec.Children) != 2 || len(items[0].spec.Children) != 2 {
		t.Fatal("nested core structure changed")
	}
	var indents []Span
	for _, m := range items[0].Markers() {
		if m.Role == "item-indent" {
			indents = append(indents, m.Source)
		}
	}
	if len(indents) != 2 {
		t.Fatalf("outer continuation evidence: %+v", items[0].Markers())
	}
	assertTaskSpan(t, indents[0], 10, 12)
	assertTaskSpan(t, indents[1], 22, 24)
	assertTaskDisabled(t, r.Source)
}
func assertTaskDisabled(t *testing.T, source Source) {
	t.Helper()
	baseline := Parse(context.Background(), source)
	requireTableComplete(t, baseline, string(source.Bytes()))
	staged := parseWithProfile(context.Background(), source, budgetFor(t, DefaultLimits()), gfmInProgressProfile)
	requireTableComplete(t, staged, string(source.Bytes()))
	assertTaskBlockContext(t, baseline.Document.root, staged.Document.root)
	for _, core := range []ParseResult{Parse(context.Background(), source), ParseWithBudget(context.Background(), source, budgetFor(t, DefaultLimits())), parseWithProfile(context.Background(), source, budgetFor(t, DefaultLimits()), coreProfile)} {
		requireTableComplete(t, core, string(source.Bytes()))
		if !reflect.DeepEqual(core.Document.root, baseline.Document.root) {
			t.Fatal("public/default core changed")
		}
		Walk(core.Document.root, func(n *Node) bool {
			if n.Properties().Task != nil {
				t.Error("task profile leaked")
			}
			return true
		})
	}
}
func TestTaskMappedSourceAndPositions(t *testing.T) {
	r := parseTableStage(t, "> - [X] é\r\n")
	item := taskItems(r.Document.root)[0]
	assertTask(t, item, 2, 12, 4, 12, 4, 5, 6, 6, 8, "X", "é")
	assertTaskSpan(t, r.Document.root.spec.Children[0].Span(), 0, 12)
	para := item.spec.Children[0]
	if len(para.Content()) != 1 {
		t.Fatal("logical payload mapping")
	}
	assertTaskSpan(t, para.Content()[0].Source, 8, 10)
	index := NewLineIndex(r.Source)
	for offset, want := range map[int]Position{5: {1, 6}, 8: {1, 9}, 10: {1, 10}, 12: {2, 1}} {
		got, err := index.Position(offset)
		if err != nil || got != want {
			t.Fatal(offset, got, err)
		}
	}
	for _, offset := range []int{9, 11} {
		if _, err := index.Position(offset); err == nil {
			t.Fatal("interior accepted", offset)
		}
	}
	for _, tc := range []struct {
		input                                                                            string
		itemStart, itemEnd, paraStart, open, checkA, checkB, close, sepEnd, textA, textB int
		spelling, payload                                                                string
		segments                                                                         [][2]int
	}{
		{"- [\n  ] foo\n", 0, 12, 2, 2, 3, 4, 6, 8, 8, 11, "\n", "foo", [][2]int{{2, 4}, {6, 7}}},
		{"> - [\r\n>   ] é\r\n", 2, 17, 4, 4, 5, 7, 11, 13, 13, 15, "\r\n", "é", [][2]int{{4, 7}, {11, 12}}},
		{"- [x]\r\n  é\r\n", 0, 13, 2, 2, 3, 4, 4, 7, 9, 11, "x", "é", [][2]int{{2, 5}}},
	} {
		t.Run(fmt.Sprintf("%q", tc.input), func(t *testing.T) {
			r := parseTableStage(t, tc.input)
			items := taskItems(r.Document.root)
			if len(items) != 1 {
				t.Fatal("mapped task absent")
			}
			assertTask(t, items[0], tc.itemStart, tc.itemEnd, tc.paraStart, tc.itemEnd, tc.open, tc.checkA, tc.checkB, tc.close, tc.sepEnd, tc.spelling, tc.payload)
			p := items[0].Properties().Task
			if len(p.Segments) != len(tc.segments) {
				t.Fatal("segments", p.Segments)
			}
			for i, w := range tc.segments {
				assertTaskSpan(t, p.Segments[i], w[0], w[1])
			}
			content := items[0].spec.Children[0].Content()
			if len(content) != 1 {
				t.Fatal("payload/break mapping", content)
			}
			assertTaskSpan(t, content[0].Source, tc.textA, tc.textB)
			assertTaskDisabled(t, r.Source)
		})
	}
}
func TestTaskGrammarAndEmptyContent(t *testing.T) {
	for _, check := range []string{" ", "\t", "\v", "\f", "x", "X"} {
		for _, separator := range []string{" ", "\t", "\v", "\f", "\n", "\r", "\r\n"} {
			t.Run(fmt.Sprintf("%q/%q", check, separator), func(t *testing.T) {
				input := "- [" + check + "]" + separator + "foo\n"
				if strings.ContainsAny(separator, "\r\n") {
					input = "- [" + check + "]" + separator + "  foo\n"
				}
				r := parseTableStage(t, input)
				items := taskItems(r.Document.root)
				if len(items) != 1 {
					t.Fatal("ASCII grammar not recognized", input)
				}
				p := items[0].Properties().Task
				if p.CheckSpelling != check || p.Checked != (check == "x" || check == "X") || tableRaw(t, r.Source, p.Separator) != separator || tableText(items[0].spec.Children[0]) != "foo" {
					t.Fatal("grammar mapping/state", p)
				}
			})
		}
	}
	for _, ending := range []string{"", "\n", "\r", "\r\n"} {
		for _, suffix := range []string{"", " ", "\t"} {
			input := "- [x]" + suffix + ending
			r := parseTableStage(t, input)
			items := taskItems(r.Document.root)
			if ending == "" && suffix == "" {
				if len(items) != 0 {
					t.Fatal("EOF fabricated separator")
				}
				continue
			}
			if len(items) != 1 {
				t.Fatal("terminal original separator lost", input)
			}
			para := items[0].spec.Children[0]
			if len(para.Content()) != 0 || len(para.Children()) != 0 {
				t.Fatal("known-empty task payload lost", input)
			}
		}
	}
	for _, spacing := range []string{"  ", " \t ", "\t\t"} {
		r := parseTableStage(t, "- [x]"+spacing+"foo\n")
		item := taskItems(r.Document.root)[0]
		para := item.spec.Children[0]
		var logical string
		for _, p := range para.Content() {
			logical += p.Semantic.Value
		}
		if logical != spacing[1:]+"foo" {
			t.Fatal("extra whitespace discarded", logical)
		}
		if item.Properties().Task.Separator.End() != 6 {
			t.Fatal("consumed more than first separator")
		}
	}
	for _, input := range []string{"- [\n  ] foo\n", "- [\r  ] foo\r", "- [\r\n  ] foo\r\n", "- [\n] foo\n", "> - [\n] foo\n"} {
		r := parseTableStage(t, input)
		if len(taskItems(r.Document.root)) != 1 {
			t.Fatal("logical/lazy newline task", input)
		}
	}
}
func TestTaskContextAndOrdinaryCore(t *testing.T) {
	for _, input := range []string{
		"[x] prose\n", "- prefix [x] later\n", "-   \t[x] code\n", "- ordinary\n\n  [x] later\n", "- # heading\n\n  [x] later\n", "- > [x] prose\n", "- > quote\n\n  [x] later\n", "- ```\n  [x] code\n  ```\n\n  [x] later\n", "-     [x] code\n\n  [x] later\n", "- <div>\n  [x] html\n\n  [x] later\n",
		"- [z] foo\n", "- [xx] foo\n", "- [] foo\n", "- [ x] foo\n", "- [x ] foo\n", "- [x foo\n", "- [x]foo\n", "- [\u00a0] foo\n", "- [x]\u00a0foo\n", "- [\u2003] foo\n", "- \\[x] foo\n", "- &#91;x] foo\n", "- [&#120;] foo\n", "- [x&#93; foo\n", "- [\n\n  ] foo\n", "```\n- [x] code\n```\n", "    - [x] code\n", "<script>\n- [x] html\n</script>\n",
	} {
		t.Run(fmt.Sprintf("%q", input), func(t *testing.T) {
			r := parseTableStage(t, input)
			if len(taskItems(r.Document.root)) != 0 {
				t.Fatal("invalid context recognized")
			}
			core := Parse(context.Background(), r.Source)
			if !reflect.DeepEqual(core.Document.root, r.Document.root) {
				t.Fatal("invalid task changed ordinary core tree")
			}
		})
	}
	for _, prefix := range []string{"- ", "+ ", "* ", "1. ", "2) ", "001. ", "-    ", "-\t", "> - ", "1. > - ", "-\n  ", "   - "} {
		input := prefix + "[X] foo\n"
		r := parseTableStage(t, input)
		items := taskItems(r.Document.root)
		if len(items) != 1 || tableText(items[0].spec.Children[0]) != "foo" {
			t.Fatal("list/padding context", input)
		}
	}
	r := parseTableStage(t, "- a | b\n  - | -\n\n  [x] later\n")
	if len(tableNodes(r.Document.root, Table)) != 1 || len(taskItems(r.Document.root)) != 0 {
		t.Fatal("first table must prevent later paragraph task")
	}
	r = parseTableStage(t, "- [ ] first\n\n  next\n")
	item := taskItems(r.Document.root)[0]
	assertTaskSpan(t, item.Span(), 0, 20)
	assertTaskSpan(t, item.spec.Children[0].Span(), 2, 12)
	if len(item.Children()) != 2 || r.Document.root.spec.Children[0].Properties().List.Tight != Some(false) {
		t.Fatal("loose core structure")
	}
	for _, input := range []string{"- [x] first\n  next\n", "- [x] first\nlazy\n", "> - [x] first\n>   next\n"} {
		r := parseTableStage(t, input)
		item := taskItems(r.Document.root)[0]
		if len(tableNodes(item, SoftBreak)) != 1 || (tableText(item.spec.Children[0]) != "firstnext" && tableText(item.spec.Children[0]) != "firstlazy") {
			t.Fatal("continuation content")
		}
	}
}

func TestTaskSharedInlinesReferencesAndProtection(t *testing.T) {
	input := "[r]: /first \"title\"\n[r]: /later\n[x]: /check\n\n- [x] &amp; *em* **strong** `code` [inline](/i) ![pic](/p) [ref][r] ![ref][r] <https://e.test> <!-- note -->\n- [ ] [label]: /body\n\n| h |\n| - |\n| [v][a\\|b] |\n\n[a|b]: /table\n"
	source := NewSource([]byte(input))
	op := budgetFor(t, DefaultLimits())
	r := parseWithProfile(context.Background(), source, op, gfmInProgressProfile)
	requireTableComplete(t, r, input)
	items := taskItems(r.Document.root)
	if len(items) != 2 || len(tableNodes(r.Document.root, Table)) != 1 {
		t.Fatal("table/task coexistence")
	}
	for kind, count := range map[Kind]int{Emphasis: 1, Strong: 1, CodeSpan: 1, InlineHTML: 1, Link: 4, Image: 2} {
		if len(tableNodes(r.Document.root, kind)) != count {
			t.Fatal("shared inline kind", kind, len(tableNodes(r.Document.root, kind)))
		}
	}
	code := tableNodes(r.Document.root, CodeSpan)[0]
	if tableRaw(t, source, code.Span()) != "`code`" || code.Properties().Literal != Some("code") {
		t.Fatal("code provenance")
	}
	em := tableNodes(r.Document.root, Emphasis)[0]
	if tableRaw(t, source, em.Span()) != "*em*" || tableText(em) != "em" {
		t.Fatal("emphasis provenance")
	}
	for _, kind := range []Kind{Link, Image} {
		for _, n := range tableNodes(r.Document.root, kind) {
			p := n.Properties().Link
			if p.Origin == "full" && p.NormalizedLabel == Some("r") && p.Destination.Semantic != Some("/first") {
				t.Fatal("first definition binding")
			}
		}
	}
	if len(r.Document.references.Definitions) != 4 || tableText(items[1].spec.Children[0]) != "[label]: /body" {
		t.Fatal("task suffix reparsed as definition")
	}
	if _, err := NewDocument(r.Document.root, r.Document.references); err != nil {
		t.Fatal("actual bindings invalid", err)
	}
	literals, err := r.Document.LiteralSpans(context.Background(), op)
	if err != nil || len(literals) != 2 {
		t.Fatal("literal authority", err, literals)
	}
	protected, err := r.Document.ProtectedNodes(context.Background(), op)
	if err != nil || len(protected) != 2 {
		t.Fatal("protected authority", err)
	}
	comments, err := r.Document.Comments(context.Background(), op)
	if err != nil || len(comments) != 1 {
		t.Fatal("comments authority", err)
	}
	foreign := budgetFor(t, DefaultLimits())
	if _, err := r.Document.LiteralSpans(context.Background(), foreign); err == nil {
		t.Fatal("foreign literal authority")
	}
	if _, err := r.Document.ProtectedNodes(context.Background(), foreign); err == nil {
		t.Fatal("foreign protected authority")
	}
	if _, err := r.Document.Comments(context.Background(), foreign); err == nil {
		t.Fatal("foreign comment authority")
	}
	again := parseWithProfile(context.Background(), source, op, gfmInProgressProfile)
	requireTableComplete(t, again, input)
	if !reflect.DeepEqual(r.Document.root, again.Document.root) || r.Source != source {
		t.Fatal("source/tree mutation")
	}
	r = parseTableStage(t, "- [r]: /u\n  [x] [r]\n")
	if len(taskItems(r.Document.root)) != 1 || len(r.Document.references.Definitions) != 1 || len(tableNodes(r.Document.root, Link)) != 1 {
		t.Fatal("first remaining direct paragraph after definition")
	}
	r = parseTableStage(t, "- [x] foo\n\n[x]: /u\n")
	if len(tableNodes(r.Document.root, Link)) != 0 {
		t.Fatal("task marker became reference use")
	}
	r = parseTableStage(t, "---\n- [x] ~~gone~~ www.example.com <title>x</title>\n")
	if r.Document.root.spec.Children[0].Kind() != ThematicBreak || len(taskItems(r.Document.root)) != 1 || len(bareNodes(r.Document.root)) != 1 {
		t.Fatal("private stage scope/frontmatter")
	}
}

func TestTaskModelOwnershipAndValidation(t *testing.T) {
	r := parseTableStage(t, "> - [\r\n>   ] é\r\n")
	item := taskItems(r.Document.root)[0]
	para := item.spec.Children[0]
	spec := para.spec
	spec.Content = para.Content()
	spec.Properties = para.Properties()
	original := spec.Properties.Task
	n, err := NewNode(spec)
	if err != nil {
		t.Fatal(err)
	}
	original.Segments[0] = Span{}
	original.Checked = true
	original.CheckSpelling = "x"
	spec.Content[0] = Segment{}
	if n.Properties().Task.Checked || n.Properties().Task.CheckSpelling != "\r\n" || !n.Properties().Task.Segments[0].Present() {
		t.Fatal("constructor alias")
	}
	got := n.Properties()
	got.Task.Segments[0] = Span{}
	got.Task.Check = Span{}
	if !n.Properties().Task.Segments[0].Present() || !n.Properties().Task.Check.Present() {
		t.Fatal("accessor alias")
	}
	bytes := r.Source.Bytes()
	bytes[4] = '!'
	if tableRaw(t, r.Source, n.Properties().Task.Opening) != "[" {
		t.Fatal("source alias")
	}
	for name, mutate := range map[string]func(*NodeSpec){
		"foreign":       func(s *NodeSpec) { s.Properties.Task.Check, _ = NewSource(r.Source.Bytes()).Span(5, 7) },
		"checked":       func(s *NodeSpec) { s.Properties.Task.Checked = true },
		"spelling":      func(s *NodeSpec) { s.Properties.Task.CheckSpelling = "x" },
		"separator":     func(s *NodeSpec) { s.Properties.Task.Separator = Span{} },
		"shift":         func(s *NodeSpec) { s.Properties.Task.Opening, _ = r.Source.Span(5, 6) },
		"gap-as-marker": func(s *NodeSpec) { s.Properties.Task.Segments = []Span{s.Properties.Task.Marker} },
		"half-crlf":     func(s *NodeSpec) { s.Properties.Task.Check, _ = r.Source.Span(6, 7) },
		"half-crlf-consistent": func(s *NodeSpec) {
			s.Properties.Task.Check, _ = r.Source.Span(6, 7)
			s.Properties.Task.CheckSpelling = "\n"
			s.Properties.Task.Segments = []Span{s.Properties.Task.Opening, s.Properties.Task.Check, s.Properties.Task.Closing}
		},
		"unrelated-kind":      func(s *NodeSpec) { s.Kind = Heading },
		"different-paragraph": func(s *NodeSpec) { s.Properties.Task.Paragraph, _ = r.Source.Span(5, 17) },
	} {
		t.Run(name, func(t *testing.T) {
			s := para.spec
			s.Properties = para.Properties()
			mutate(&s)
			if _, err := NewNode(s); err == nil {
				t.Fatal("invalid task model accepted")
			}
		})
	}
	// A contained range alone cannot masquerade as the actual first paragraph.
	for _, mode := range []string{"later", "missing", "different", "nested"} {
		s := item.spec
		s.Properties = item.Properties()
		switch mode {
		case "later":
			ordinary, _ := NewNode(NodeSpec{Kind: Paragraph, Envelope: para.Span()})
			s.Children = []*Node{ordinary, para}
		case "missing":
			s.Properties.Task = nil
		case "different":
			s.Properties.Task.CheckSpelling = "X"
		case "nested":
			quote, _ := NewNode(NodeSpec{Kind: BlockQuote, Envelope: para.Span(), Children: []*Node{para}})
			s.Children = []*Node{quote}
		}
		if _, err := NewNode(s); err == nil {
			t.Fatal("wrong first child accepted", mode)
		}
	}
	s := item.spec
	s.Properties = item.Properties()
	s.Children = item.Children()
	rebuilt, err := NewNode(s)
	if err != nil {
		t.Fatal(err)
	}
	*s.Children[0] = Node{}
	if rebuilt.Children()[0].Kind() != Paragraph {
		t.Fatal("child handle alias")
	}
}

func TestTaskResourceBudgetsAndDepth(t *testing.T) {
	input := "> - [X] **a**\n>   next\n"
	source := NewSource([]byte(input))
	baseline := budgetFor(t, DefaultLimits())
	requireTableComplete(t, parseWithProfile(context.Background(), source, baseline, gfmInProgressProfile), input)
	for name, used := range map[string]int64{"document-nodes": baseline.nodes, "work-units": baseline.work} {
		for _, delta := range []int64{-1, 0, 1} {
			l := DefaultLimits()
			if name == "document-nodes" {
				l.DocumentNodes = used + delta
			} else {
				l.WorkUnits = used + delta
			}
			r := parseWithProfile(context.Background(), source, budgetFor(t, l), gfmInProgressProfile)
			if delta < 0 {
				requireLimit(t, r, name)
			} else {
				requireTableComplete(t, r, input)
			}
		}
	}
	for _, allowed := range []int64{4, 5, 6} {
		l := DefaultLimits()
		l.SyntaxDepth = allowed
		r := parseWithProfile(context.Background(), source, budgetFor(t, l), gfmInProgressProfile)
		if allowed < 5 {
			requireLimit(t, r, "syntax-depth")
		} else {
			requireTableComplete(t, r, input)
		}
	}
	for _, input := range []string{strings.Repeat("- [x] a\n", 1000), "- [x] " + strings.Repeat(" ", 20000) + "a\n", "- [" + strings.Repeat(" ", 20000) + "] a\n", "- [x] " + strings.Repeat("\x00", 1000) + "\n"} {
		l := DefaultLimits()
		l.DocumentNodes = 10
		requireLimit(t, parseWithProfile(context.Background(), NewSource([]byte(input)), budgetFor(t, l), gfmInProgressProfile), "document-nodes")
		l = DefaultLimits()
		l.WorkUnits = int64(len(input) * 3)
		requireLimit(t, parseWithProfile(context.Background(), NewSource([]byte(input)), budgetFor(t, l), gfmInProgressProfile), "work-units")
	}
	l := DefaultLimits()
	l.DocumentNodes = baseline.nodes
	l.TotalNodes = baseline.nodes * 2
	op := budgetFor(t, l)
	requireTableComplete(t, parseWithProfile(context.Background(), source, op, gfmInProgressProfile), input)
	requireTableComplete(t, ParseWithBudget(context.Background(), source, op), input)
	requireLimit(t, parseWithProfile(context.Background(), source, op, gfmInProgressProfile), "total-nodes")
	requireLimit(t, ParseWithBudget(context.Background(), source, op), "total-nodes")
	op = budgetFor(t, DefaultLimits())
	requireTableComplete(t, parseWithProfile(context.Background(), source, op, gfmInProgressProfile), input)
	work := op.work
	if err := op.ChargeWork(13); err != nil || op.work != work+13 {
		t.Fatal("adapter charge")
	}
	requireTableComplete(t, ParseWithBudget(context.Background(), source, op), input)
	if op.work <= work+13 {
		t.Fatal("cumulative work reset")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	l = DefaultLimits()
	l.DocumentBytes = 3
	requireLimit(t, parseWithProfile(cancelled, NewSource([]byte("abc\xff")), budgetFor(t, l), gfmInProgressProfile), "document-bytes")
	r := parseWithProfile(cancelled, NewSource([]byte("\xff")), budgetFor(t, l), gfmInProgressProfile)
	if r.Failure == nil || r.Failure.Code != InvalidUTF8 {
		t.Fatal("encoding before cancellation")
	}
	requireTableComplete(t, parseWithProfile(context.Background(), NewSource(nil), budgetFor(t, DefaultLimits()), gfmInProgressProfile), "")
}

func TestTaskCancellationAndConcurrentIsolation(t *testing.T) {
	for _, input := range []string{"- [\r\n  ] " + strings.Repeat("*a* `b` ", 500) + "\n", strings.Repeat("- [X] a\n", 1000), "- [x] " + strings.Repeat("\x00", 4000) + "\n", "- [" + strings.Repeat(" ", 20000) + "] a\n"} {
		source := NewSource([]byte(input))
		seen := false
		for _, checks := range []int{1, 8, 16, 32, 64, 128, 256, 512, 1024} {
			ctx := &checkpointContext{Context: context.Background(), remaining: checks}
			r := parseWithProfile(ctx, source, budgetFor(t, DefaultLimits()), gfmInProgressProfile)
			if r.Status == Complete {
				continue
			}
			seen = true
			if r.Status != Incomplete || r.AST != Partial || r.Failure != nil || r.Interruption != Cancelled || len(r.Uncertainty) != 1 || r.Document.references.LookupAvailability == Available || r.Source != source {
				t.Fatal("bad task interruption", r)
			}
			assertTaskSpan(t, r.Uncertainty[0].Region, 0, len(input))
			if r.Phases.Inlines == Interrupted && len(tableNodes(r.Document.root, CodeSpan)) != 0 {
				t.Fatal("unfinished inline tree published")
			}
			if _, err := r.Document.ProtectedNodes(context.Background(), budgetFor(t, DefaultLimits())); err == nil {
				t.Fatal("partial protection authority")
			}
		}
		if !seen {
			t.Fatal("cancellation unexercised")
		}
	}
	input := "> - [X] **a**\n>   - [ ] `b`\n"
	source := NewSource([]byte(input))
	baseline := parseWithProfile(context.Background(), source, budgetFor(t, DefaultLimits()), gfmInProgressProfile)
	requireTableComplete(t, baseline, input)
	shared := budgetFor(t, DefaultLimits())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			for j := 0; j < 3; j++ {
				r := parseWithProfile(context.Background(), source, shared, gfmInProgressProfile)
				if r.Status != Complete || !reflect.DeepEqual(r.Document.root, baseline.Document.root) {
					t.Error("staged concurrency")
				}
				core := ParseWithBudget(context.Background(), source, shared)
				if core.Status != Complete || len(taskItems(core.Document.root)) != 0 {
					t.Error("core concurrency/profile leak")
				}
				independent := Parse(context.Background(), source)
				if independent.Status != Complete || len(taskItems(independent.Document.root)) != 0 {
					t.Error("independent core leak")
				}
			}
		})
	}
	wg.Wait()
	assertTaskDisabled(t, source)
}

// Pair original structural source facts, while payload/inline interpretation is
// intentionally different on task paragraphs. Literal ranges above remain the
// independent source oracle for the published and mapped witnesses.
func assertTaskBlockContext(t *testing.T, core, staged *Node) {
	t.Helper()
	if core.Kind() != staged.Kind() || core.Span() != staged.Span() || !reflect.DeepEqual(core.Markers(), staged.Markers()) || !reflect.DeepEqual(core.Properties().List, staged.Properties().List) {
		t.Fatal("task changed core block/container ownership")
	}
	if core.Kind() == Paragraph || core.Kind() == Heading || core.Kind() == CodeBlock || core.Kind() == HTMLBlock {
		return
	}
	if len(core.spec.Children) != len(staged.spec.Children) {
		t.Fatal("task changed block child order/count")
	}
	for i, child := range core.spec.Children {
		assertTaskBlockContext(t, child, staged.spec.Children[i])
	}
}

// Exercise the selected authored source bytes as real syntax. Integrated HTML
// remains deliberately untested until the all-five adapter exists.
func TestTaskApprovedSupplementalWitnesses(t *testing.T) {
	data, err := os.ReadFile("testdata/gfm/profile/expectations.json")
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct{ Fixtures []struct{ ID, Source string } }
	if err = json.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, fixture := range bundle.Fixtures {
		switch fixture.ID {
		case "task-uppercase-crlf", "task-loose", "task-invalid", "task-nonlist", "combined-all-five":
		default:
			continue
		}
		seen++
		t.Run(fixture.ID, func(t *testing.T) {
			r := parseTableStage(t, fixture.Source)
			items := taskItems(r.Document.root)
			switch fixture.ID {
			case "task-uppercase-crlf":
				if len(items) != 1 {
					t.Fatal("uppercase task absent")
				}
				assertTask(t, items[0], 2, 12, 4, 12, 4, 5, 6, 6, 8, "X", "é")
			case "task-loose":
				if len(items) != 1 {
					t.Fatal("loose task absent")
				}
				assertTask(t, items[0], 0, 20, 2, 12, 2, 3, 4, 4, 6, " ", "first")
				if r.Document.root.spec.Children[0].Properties().List.Tight != Some(false) || len(items[0].spec.Children) != 2 {
					t.Fatal("loose source structure")
				}
			case "task-invalid", "task-nonlist":
				if len(items) != 0 {
					t.Fatal("ordinary fixture recognized")
				}
				core := Parse(context.Background(), r.Source)
				if !reflect.DeepEqual(core.Document.root, r.Document.root) {
					t.Fatal("ordinary fixture core changed")
				}
			case "combined-all-five":
				if len(items) != 1 || len(tableNodes(r.Document.root, Table)) != 1 || len(bareNodes(r.Document.root)) != 1 {
					t.Fatal("implemented stage scope")
				}
				assertTask(t, items[0], 0, 21, 2, 20, 2, 3, 4, 4, 6, "x", "www.a.org")
				html := tableNodes(r.Document.root, InlineHTML)
				if len(html) != 1 {
					t.Fatal("raw HTML lost")
				}
				assertTaskSpan(t, html[0].Span(), 35, 42)
				if tableRaw(t, r.Source, html[0].Span()) != "<title>" {
					t.Fatal("premature filter/source rewrite")
				}
			}
			core := Parse(context.Background(), r.Source)
			requireTableComplete(t, core, fixture.Source)
			if len(taskItems(core.Document.root)) != 0 || len(tableNodes(core.Document.root, Table)) != 0 {
				t.Fatal("private stage leaked")
			}
		})
	}
	if seen != 5 {
		t.Fatal("approved witnesses absent", seen)
	}
}
