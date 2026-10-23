package parser

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func pandocBlocks(t *testing.T, input string) ParseResult {
	t.Helper()
	budget, _ := NewOperationBudget(DefaultLimits())
	r := parseWithProfile(context.Background(), NewSource([]byte(input)), budget, pandocBlocksProfile)
	if r.Failure != nil || r.Interruption != "" || r.Phases.Blocks != PhaseComplete || r.Phases.Inlines != Pending || r.Status != Incomplete || r.AST != Partial {
		t.Fatalf("block-stage result: %+v", r)
	}
	if r.Document.parsedComplete || r.Document.References().Availability != Unavailable {
		t.Fatal("block stage certified full semantics")
	}
	return r
}
func pandocKinds(root *Node) []Kind {
	var kinds []Kind
	Walk(root, func(n *Node) bool { kinds = append(kinds, n.Kind()); return true })
	return kinds
}
func TestPandocBlockShapes(t *testing.T) {
	tests := []struct {
		input string
		kinds []Kind
	}{
		{"prose\n# heading\n> quote\n- item\n", []Kind{DocumentKind, Paragraph}},
		{"# heading\n\n> quote\n\n- item\n", []Kind{DocumentKind, Heading, BlockQuote, Paragraph, List, ListItem, Paragraph}},
		{"Term\n: first\n\n  next paragraph\n", []Kind{DocumentKind, DefinitionList, DefinitionItem, DefinitionTerm, DefinitionBody, Paragraph, Paragraph}},
		{"| a\n  wrapped\n| b\n|\n", []Kind{DocumentKind, LineBlock, LineBlockLine, LineBlockLine, LineBlockLine}},
		{"a. one\nb. two\n", []Kind{DocumentKind, List, ListItem, Paragraph, ListItem, Paragraph}},
		{"* one\n+ two\n- three\n", []Kind{DocumentKind, List, ListItem, Paragraph, ListItem, Paragraph, ListItem, Paragraph}},
	}
	for _, tt := range tests {
		t.Run(strings.ReplaceAll(tt.input, "\n", "/"), func(t *testing.T) {
			r := pandocBlocks(t, tt.input)
			if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, tt.kinds) {
				t.Fatalf("kinds=%v want=%v", got, tt.kinds)
			}
		})
	}
}
func TestPandocMarkerFamilies(t *testing.T) {
	tests := []struct {
		raw, style, delim string
		start             int
		decimal           string
	}{
		{"9.", "decimal", "period", 9, "9"}, {"(12)", "decimal", "two-parens", 12, "12"}, {"9)", "decimal", "one-paren", 9, "9"},
		{"a.", "lower-alpha", "period", 1, ""}, {"B)", "upper-alpha", "one-paren", 2, ""}, {"iv.", "lower-roman", "period", 4, ""}, {"IV)", "upper-roman", "one-paren", 4, ""},
		{"#.", "default", "default", 1, ""}, {"(@bird)", "example", "two-parens", 1, ""}, {"(@)", "example", "two-parens", 1, ""},
		{"123456789012345678901.", "decimal", "period", 1, "123456789012345678901"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			input := tt.raw + "  é\r\n"
			r := pandocBlocks(t, input)
			children := r.Document.Root().Children()
			if len(children) != 1 || children[0].Kind() != List {
				t.Fatalf("not list: %v", pandocKinds(r.Document.Root()))
			}
			item := children[0].Children()[0]
			x := item.Properties().PandocList
			if x == nil || x.Style != tt.style || x.Delimiter != tt.delim || x.Start != tt.start {
				t.Fatalf("metadata %+v", x)
			}
			if got, _ := r.Source.Text(x.Marker); got != tt.raw {
				t.Fatalf("marker %q", got)
			}
			if x.Marker.Start() != 0 || x.Marker.End() != len(tt.raw) || x.MarkerColumn != 0 || x.ContentColumn != len(tt.raw)+2 {
				t.Fatalf("positions %+v", x)
			}
			if tt.decimal == "" {
				if x.Decimal.Present() {
					t.Fatal("nondecimal correction authority")
				}
			} else {
				if got, _ := r.Source.Text(x.Decimal); got != tt.decimal {
					t.Fatalf("decimal %q", got)
				}
			}
			if got, _ := r.Source.Text(x.Separator); got != "  " {
				t.Fatalf("separator %q", got)
			}
		})
	}
}
func TestPandocTaskTerminalIsolation(t *testing.T) {
	for _, ending := range []string{"", "\n", "\r\n"} {
		for _, check := range []string{"x", "X", " "} {
			input := "- [" + check + "]" + ending
			r := pandocBlocks(t, input)
			item := r.Document.Root().Children()[0].Children()[0]
			x := item.Properties().PandocTask
			if x == nil || x.Separator.Present() || !x.InlinePending || x.Checked != (check != " ") {
				t.Fatalf("terminal metadata %+v input %q", x, input)
			}
			core := Parse(context.Background(), NewSource([]byte(input)))
			if core.Status != Complete {
				t.Fatal("core isolation")
			}
			budget, _ := NewOperationBudget(DefaultLimits())
			gfm := ParseGFMWithBudget(context.Background(), NewSource([]byte(input)), budget)
			if ending == "" && gfm.Document.Root().Children()[0].Children()[0].Properties().Task != nil {
				t.Fatal("GFM terminal rule changed")
			}
		}
	}
}

func TestPandocExampleState(t *testing.T) {
	input := "(@é) one\n\ntext\n\n(@) two\n\ntext\n\n(7@鳥) seven\n\ntext\n\n(@é) reuse\n\ntext\n\n(@) eight\n"
	r := pandocBlocks(t, input)
	var starts []int
	var labels []string
	Walk(r.Document.Root(), func(n *Node) bool {
		if n.Kind() == ListItem {
			x := n.Properties().PandocList
			if x.Style == "example" {
				starts = append(starts, x.Start)
				label, _ := r.Source.Text(x.Label)
				labels = append(labels, label)
			}
		}
		return true
	})
	if !reflect.DeepEqual(starts, []int{1, 2, 7, 1, 8}) || !reflect.DeepEqual(labels, []string{"é", "", "鳥", "é", ""}) {
		t.Fatalf("examples %v %v", starts, labels)
	}
}
func TestPandocNestedAndFallback(t *testing.T) {
	cases := []struct {
		input        string
		lists, items int
	}{
		{"- outer\n  2. ordinary\n", 1, 1}, {"- outer\n  1. nested\n  2. sibling\n", 2, 3},
		{"B. prose\n", 0, 0}, {"B.  list\n", 1, 1}, {"p. 42\n", 0, 0}, {"(@bad-) ordinary\n", 0, 0},
		{"i. one\nii. two\nc. hundred\n#. next\n", 1, 4},
		{"a. one\nb. two\ni. nine\n", 1, 3},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			r := pandocBlocks(t, tt.input)
			lists, items := 0, 0
			Walk(r.Document.Root(), func(n *Node) bool {
				if n.Kind() == List {
					lists++
				}
				if n.Kind() == ListItem {
					items++
				}
				return true
			})
			if lists != tt.lists || items != tt.items {
				t.Fatalf("lists/items %d/%d want%d/%d kinds%v", lists, items, tt.lists, tt.items, pandocKinds(r.Document.Root()))
			}
		})
	}
}

func TestPandocRetainedBlockFixtures(t *testing.T) {
	// Original #237 inputs run through the real block lifecycle. These explicit
	// block-stage shapes do not claim any deferred inline, HTML, table or full
	// profile conformance and never rewrite the all-profile NOT-RUN ledger.
	root := "testdata/pandoc/3.12.1/"
	data, err := os.ReadFile(root + "expectations.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct{ ID, Extension, Dimension, Input string }
	}
	if err = json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	item := []Kind{List, ListItem, Paragraph}
	task := []Kind{List, ListItem, Paragraph, ListItem, Paragraph, ListItem, Paragraph}
	bases := map[string][]Kind{
		"blank_before_blockquote": {Paragraph}, "blank_before_header": {Paragraph}, "space_in_atx_header": {Heading},
		"definition_lists": {DefinitionList, DefinitionItem, DefinitionTerm, DefinitionBody, Paragraph},
		"line_blocks":      {LineBlock, LineBlockLine, LineBlockLine}, "fancy_lists": item, "startnum": item,
		"example_lists": {List, ListItem, Paragraph, Paragraph}, "task_lists": task,
	}
	count := 0
	for _, c := range corpus.Cases {
		base, selected := bases[c.Extension]
		extra := c.ID == "list-markers-original" || c.ID == "tasks-original-crlf"
		if !selected && !extra {
			continue
		}
		count++
		t.Run(c.ID, func(t *testing.T) {
			input, err := os.ReadFile(root + c.Input)
			if err != nil {
				t.Fatal(err)
			}
			r := pandocBlocks(t, string(input))
			want := append([]Kind{DocumentKind}, base...)
			switch c.Dimension {
			case "fallback":
				want = []Kind{DocumentKind, Paragraph}
				switch c.Extension {
				case "blank_before_header":
					want = []Kind{DocumentKind, Paragraph, Heading}
				case "blank_before_blockquote":
					want = []Kind{DocumentKind, Paragraph, BlockQuote, Paragraph}
				case "task_lists":
					want = append([]Kind{DocumentKind}, item...)
				}
			case "escape":
				want = []Kind{DocumentKind, Paragraph}
			case "protected":
				want = []Kind{DocumentKind, CodeBlock}
			case "nested":
				want = append([]Kind{DocumentKind, BlockQuote}, base...)
			case "unicode":
				want = append([]Kind{DocumentKind, Paragraph}, base...)
			case "precedence":
				want = append([]Kind{DocumentKind, CodeBlock}, base...)
			}
			if c.ID == "list-markers-original" {
				want = []Kind{DocumentKind, List, ListItem, Paragraph, List, ListItem, Paragraph, List, ListItem, Paragraph, List, ListItem, Paragraph, Paragraph}
			}
			if c.ID == "tasks-original-crlf" {
				want = append([]Kind{DocumentKind, BlockQuote}, task...)
			}
			if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, want) {
				t.Fatalf("actual block kinds %v want %v", got, want)
			}
			Walk(r.Document.Root(), func(n *Node) bool {
				if !validPandocSpan(r.Document.Root().Span(), n.Span()) {
					t.Fatal("invalid node coordinates")
				}
				for _, part := range n.Content() {
					if part.Source.Present() && !validPandocSpan(n.Span(), part.Source) {
						t.Fatal("invalid payload coordinates")
					}
				}
				return true
			})
			if string(r.Source.Bytes()) != string(input) {
				t.Fatal("source changed")
			}
		})
	}
	if count != 74 {
		t.Fatalf("actual block fixture accounting %d want74 (72 selected-switch plus2 loss-sensitive)", count)
	}
}

func TestPandocPriorityAndContainers(t *testing.T) {
	tests := []struct {
		input string
		want  []Kind
	}{
		{"- term\n: definition\n", []Kind{DocumentKind, List, ListItem, Paragraph, Paragraph}},
		{"1. term\n: definition\n", []Kind{DocumentKind, List, ListItem, Paragraph, Paragraph}},
		{"| line\n: definition\n", []Kind{DocumentKind, LineBlock, LineBlockLine, Paragraph}},
		{"Outer\n: term\n\n  Inner\n  : nested\n", []Kind{DocumentKind, DefinitionList, DefinitionItem, DefinitionTerm, DefinitionBody, Paragraph, DefinitionList, DefinitionItem, DefinitionTerm, DefinitionBody, Paragraph}},
		{"Term\n: first\n~ second\n\nNext\n\n: third\n", []Kind{DocumentKind, DefinitionList, DefinitionItem, DefinitionTerm, DefinitionBody, Paragraph, DefinitionBody, Paragraph, DefinitionItem, DefinitionTerm, DefinitionBody, Paragraph}},
		{"> prose\n> > continuation\n", []Kind{DocumentKind, BlockQuote, Paragraph}},
		{"a\nb\n---\n", []Kind{DocumentKind, Paragraph}},
		{"a\n---\n", []Kind{DocumentKind, Heading}},
		{"a\n===\n", []Kind{DocumentKind, Heading}},
		{"####### ordinary\n", []Kind{DocumentKind, Paragraph}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			r := pandocBlocks(t, tt.input)
			if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got%v want%v", got, tt.want)
			}
		})
	}
}
func TestPandocTabsAndPayloads(t *testing.T) {
	for _, input := range []string{"-\té\r\n\tcontinued\r\n", ">\t-\tΩ\r\n>\t  lazy\r\n", "-     indented\n", "(@長いlabel) example\n\n    continuation\n"} {
		t.Run(input, func(t *testing.T) {
			r := pandocBlocks(t, input)
			Walk(r.Document.Root(), func(n *Node) bool {
				for _, part := range n.Content() {
					if part.Source.Present() && !validPandocSpan(n.Span(), part.Source) {
						t.Fatal("payload alignment")
					}
				}
				if x := n.Properties().PandocList; x != nil && n.Kind() == ListItem {
					if _, err := NewNode(n.spec); err != nil {
						t.Fatal("valid typed model rejected", err)
					}
				}
				return true
			})
		})
	}
	r := pandocBlocks(t, "|   é\r\n  continued\r\n|\r\n")
	line := r.Document.Root().Children()[0].Children()[0]
	p := line.Properties().PandocLine
	if p.Indentation.Semantic != Some("\u00a0\u00a0") || line.Content()[0].Source.Start() != 4 || line.Content()[0].Source.End() != 6 || line.Span().Start() != 0 || line.Span().End() != 21 {
		t.Fatalf("line ownership %+v %+v", p, line.spec)
	}
}

func TestPandocEOFMarkersAndNumberBound(t *testing.T) {
	for _, input := range []string{"-", "1.", "(@)", "B.", "-     ", "1.\t"} {
		r := pandocBlocks(t, input)
		if r.Document.Root().Children()[0].Kind() != List {
			t.Fatal("EOF list marker lost", input)
		}
	}
	maxInt := int(^uint(0) >> 1)
	input := "(" + strconv.Itoa(maxInt) + "@last) maximum\n\ntext\n\n(@) exhausted\n"
	budget, _ := NewOperationBudget(DefaultLimits())
	r := parseWithProfile(context.Background(), NewSource([]byte(input)), budget, pandocBlocksProfile)
	if r.Failure != nil || r.Status != Incomplete || r.Phases.Blocks != Pending || r.Document.parsedComplete {
		t.Fatalf("counter boundary %+v", r)
	}
}

func TestPandocModelValidationAndOwnership(t *testing.T) {
	r := pandocBlocks(t, "(12)\t[X] é\r\n")
	list := r.Document.Root().Children()[0]
	item := list.Children()[0]
	paragraph := item.Children()[0]
	tests := []struct {
		name   string
		node   *Node
		mutate func(*NodeSpec)
	}{
		{"wrong style", item, func(s *NodeSpec) { s.Properties.PandocList.Style = "lower-roman" }},
		{"wrong delimiter", item, func(s *NodeSpec) { s.Properties.PandocList.Delimiter = "period" }},
		{"wrong numbering", item, func(s *NodeSpec) { s.Properties.PandocList.Start = 13 }},
		{"lost decimal", item, func(s *NodeSpec) { s.Properties.PandocList.Decimal = Span{} }},
		{"wrong separator", item, func(s *NodeSpec) { s.Properties.PandocList.Separator = s.Properties.PandocList.Decimal }},
		{"wrong column", item, func(s *NodeSpec) { s.Properties.PandocList.ContentColumn++ }},
		{"wrong source marker column", item, func(s *NodeSpec) { s.Properties.PandocList.SourceMarkerColumn++ }},
		{"wrong source content column", item, func(s *NodeSpec) { s.Properties.PandocList.SourceContentColumn++ }},
		{"false partial tab", item, func(s *NodeSpec) {
			s.Properties.PandocList.SeparatorPartialTab = !s.Properties.PandocList.SeparatorPartialTab
		}},
		{"wrong prefix", item, func(s *NodeSpec) { s.Properties.List.Prefix.Semantic = Some("fake") }},
		{"foreign marker", item, func(s *NodeSpec) {
			src := NewSource(r.Source.Bytes())
			s.Properties.PandocList.Marker, _ = src.Span(0, 4)
		}},
		{"wrong kind", item, func(s *NodeSpec) { s.Kind = Paragraph }},
		{"missing task association", item, func(s *NodeSpec) { s.Properties.PandocTask = nil }},
		{"task no children", item, func(s *NodeSpec) { s.Children = nil }},
		{"task wrong state", paragraph, func(s *NodeSpec) { s.Properties.PandocTask.Checked = false }},
		{"task wrong spelling", paragraph, func(s *NodeSpec) { s.Properties.PandocTask.CheckSpelling = "x" }},
		{"task false separator", paragraph, func(s *NodeSpec) { s.Properties.PandocTask.Separator = s.Properties.PandocTask.Check }},
		{"task fake brackets", paragraph, func(s *NodeSpec) { s.Properties.PandocTask.Marker.start++ }},
		{"task lost mapping", paragraph, func(s *NodeSpec) { s.Properties.PandocTask.Segments = nil }},
		{"task false semantic complete", paragraph, func(s *NodeSpec) { s.Properties.PandocTask.InlinePending = false }},
		{"nil child", list, func(s *NodeSpec) { s.Children[0] = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.node.spec
			s.Properties = cloneProperties(s.Properties)
			s.Children = cloneNodes(s.Children)
			tt.mutate(&s)
			if _, err := NewNode(s); err == nil {
				t.Fatal("contradictory supplied facts accepted")
			}
		})
	}
	before := item.Properties()
	copy := item.Properties()
	copy.PandocList.Style = "evil"
	copy.PandocTask.Segments[0] = Span{}
	copy.List.Prefix.Segments[0].Source = Span{}
	if !reflect.DeepEqual(before, item.Properties()) {
		t.Fatal("returned metadata aliases immutable node")
	}
	spec := item.spec
	spec.Properties = cloneProperties(spec.Properties)
	n, err := NewNode(spec)
	if err != nil {
		t.Fatal(err)
	}
	spec.Properties.PandocTask.Segments[0] = Span{}
	spec.Properties.PandocList.Style = "evil"
	if !reflect.DeepEqual(before, n.Properties()) {
		t.Fatal("constructor retained caller metadata")
	}
}
func TestPandocLimitsAndCancellation(t *testing.T) {
	input := "# closed\n\nTerm\n: - [X] first\n  - second\n\n(@bird) example\n\n    continuation\n\n| é\n  wrapped\n"
	for _, field := range []string{"bytes", "work", "nodes", "depth"} {
		t.Run(field, func(t *testing.T) {
			limits := DefaultLimits()
			switch field {
			case "bytes":
				limits.DocumentBytes = 8
			case "work":
				limits.WorkUnits = 200
			case "nodes":
				limits.DocumentNodes = 10
			case "depth":
				limits.SyntaxDepth = 2
			}
			budget, _ := NewOperationBudget(limits)
			r := parseWithProfile(context.Background(), NewSource([]byte(input)), budget, pandocBlocksProfile)
			if r.Status != Incomplete || r.Limit == nil || r.Interruption != LimitExceededReason || r.Document.parsedComplete {
				t.Fatalf("limit result %+v", r)
			}
		})
	}
	cancelled := 0
	for point := 1; point < 40; point++ {
		budget, _ := NewOperationBudget(DefaultLimits())
		ctx := &checkpointContext{Context: context.Background(), remaining: point}
		r := parseWithProfile(ctx, NewSource([]byte(input)), budget, pandocBlocksProfile)
		if r.Interruption == Cancelled {
			cancelled++
			if r.Document.parsedComplete || r.Document.References().Availability != Unavailable || r.Phases.Inlines == PhaseComplete {
				t.Fatalf("interrupted stage certified %+v", r)
			}
			for _, n := range r.Document.Root().Children() {
				if n.Span().End() > len(input) {
					t.Fatal("invalid closed prefix")
				}
			}
		}
	}
	if cancelled < 10 {
		t.Fatalf("cancellation controls did not reach enough checkpoints: %d", cancelled)
	}
	budget, _ := NewOperationBudget(DefaultLimits())
	r := parseWithProfile(context.Background(), NewSource([]byte("# heading\n")), budget, pandocBlocksProfile)
	if _, err := r.Document.TaskSources(context.Background(), budget); err == nil {
		t.Fatal("partial stage obtained complete consumer authority")
	}
	bad := parseWithProfile(context.Background(), NewSource([]byte{0xff}), budget, pandocBlocksProfile)
	if bad.Failure == nil || bad.Failure.Code != InvalidUTF8 {
		t.Fatal("strict UTF8 bypassed")
	}
}

func FuzzPandocBlockSourceProperties(f *testing.F) {
	for _, input := range []string{"# `x`{#inline} {#heading}\n", "# <https://example.com>{#inline}\n", "# <@>{#heading}\n", "> # `x`{#inline\r\n> .X} {#heading}\r\n", "``` {k=\"a}\nb\" .X}\nx\n```\n", "``` {k=\"a}\n .X}\nx\n```\n"} {
		f.Add(input)
	}
	for _, input := range []string{"``` {.X #a #b k='&amp;'}\nx\n```\n", "> ``` {#é\r\n> .X}\r\n> x\r\n> ```\r\n", "::: {}\n::: inner\n```{=html}\n::: fake\n```\n:::\n:::\n", "# H {#a\n .X}\n", "```{python}\n# heading\n\n- item\n"} {
		f.Add(input)
	}
	for _, input := range []string{"(12.  é\n", "- # heading\n(2. prose\n", "(9@bad. prose\n\n(@) valid\n", "> - # heading\r\n> (2. prose\r\n"} {
		f.Add(input)
	}
	for _, input := range []string{"- | text\n wrapped\n", "- # heading\nordinary\n", "Term\n: # heading\nordinary\n", "- ***\nordinary\n", "- # heading\n\n  # second\nordinary\n", "- # heading\n  ```\nbody\n```\n"} {
		f.Add(input)
	}
	for _, input := range []string{"p.  42\n", "p. \t42\r\n", "(p. 42\n", "> p.  42\r\n", "p. 42\n", "p.\t42\n"} {
		f.Add(input)
	}
	for _, input := range []string{"> 1. first\n2. second\n", "> | text\n wrapped\n", "B.\tpayload\r\n", "(@) one\n\n(999999999999999999999999999999@label) two\n", "one\n ```\nbody\n ```\n", "> heading\n===\n", "- heading\n===\n", "> Term\n>\n: body\n"} {
		f.Add(input)
	}
	for _, input := range []string{"Term\n: - [X] é\r\n  - second\n", "(@bird) example\n\n    continued\n\n(@bird) reuse\n", "|   line\n  wrap\n|\n", "> prose\n> > continuation\n", "i. one\nii. two\n#. next\n", "-", "> \tcode\n", "Term\n: [x] body\n", "one\ntwo\n---\n"} {
		f.Add(input)
	}
	for _, input := range []string{"# [$a]b$]{#inline}\n", "# [<i title=']'>x</i>]{#inline}\n", "# <http:a<b>{#inline}\n", "# [<i title='{#wrong}\nx'>y</i>]{#inline}\n", "# [`x {#wrong}\ny`]{#inline}\n", "> # <a\r\n> # <a\r\n"} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 8192 {
			t.Skip()
		}
		source := NewSource([]byte(input))
		limits := DefaultLimits()
		limits.WorkUnits = 2000000
		limits.DocumentNodes = 30000
		limits.TotalNodes = 100000
		budget, _ := NewOperationBudget(limits)
		r := parseWithProfile(context.Background(), source, budget, pandocBlocksProfile)
		if string(r.Source.Bytes()) != input {
			t.Fatal("source changed")
		}
		if r.Failure != nil {
			if r.Failure.Code != InvalidUTF8 {
				t.Fatalf("model failure %+v input%q", r.Failure, input)
			}
			return
		}
		if r.Document.parsedComplete || r.Status == Complete {
			t.Fatal("partial profile certified")
		}
		Walk(r.Document.Root(), func(n *Node) bool {
			if !validPandocSpan(r.Document.Root().Span(), n.Span()) {
				t.Fatal("invalid span")
			}
			if _, err := NewNode(n.spec); err != nil {
				t.Fatalf("published invalid node: %s %v", n.Kind(), err)
			}
			return true
		})
	})
}

func TestPandocDeferredInlinePayloadSurvives(t *testing.T) {
	input := "[a]: /url\n[^note]: body\n"
	r := pandocBlocks(t, input)
	children := r.Document.Root().Children()
	if len(children) != 1 || children[0].Kind() != Paragraph {
		t.Fatal("pending inline/reference provider consumed block payload")
	}
	var raw strings.Builder
	for _, part := range children[0].Content() {
		text, _ := r.Source.Text(part.Source)
		raw.WriteString(text)
	}
	if raw.String() != strings.TrimSuffix(input, "\n") {
		t.Fatalf("payload lost %q", raw.String())
	}
	for _, input := range []string{"> Term\n> : body\n", "> Term\n>\n> : body\n"} {
		r := pandocBlocks(t, input)
		item := r.Document.Root().Children()[0].Children()[0].Children()[0]
		want := !strings.Contains(input, ">\n")
		if item.Properties().Definition.Tight != want {
			t.Fatal("container prefix confused blank-line ownership")
		}
	}
}

func TestPandocCompleteDelimiterMatrix(t *testing.T) {
	families := []struct {
		body, style string
		start       int
	}{{"12", "decimal", 12}, {"b", "lower-alpha", 2}, {"B", "upper-alpha", 2}, {"iv", "lower-roman", 4}, {"IV", "upper-roman", 4}, {"#", "default", 1}, {"@bird", "example", 1}}
	for _, family := range families {
		for _, delim := range []string{"period", "one-paren", "two-parens"} {
			raw := family.body + "."
			if delim == "one-paren" {
				raw = family.body + ")"
			}
			if delim == "two-parens" {
				raw = "(" + family.body + ")"
			}
			t.Run(raw, func(t *testing.T) {
				r := pandocBlocks(t, raw+"  payload\n")
				x := r.Document.Root().Children()[0].Children()[0].Properties().PandocList
				wantDelim := delim
				if family.style == "default" && delim == "period" {
					wantDelim = "default"
				}
				if x.Style != family.style || x.Delimiter != wantDelim || x.Start != family.start {
					t.Fatalf("family %+v", x)
				}
			})
		}
	}
}
func TestPandocWhitespaceTasksAndExactLabels(t *testing.T) {
	for _, input := range []string{"- [   ] text\n", "- [\t] text\n", "> - [ \n>   ] text\n", "1. [X]\n", "(@bird) [x]\n"} {
		t.Run(input, func(t *testing.T) {
			r := pandocBlocks(t, input)
			found := 0
			Walk(r.Document.Root(), func(n *Node) bool {
				if n.Kind() == ListItem && n.Properties().PandocTask != nil {
					found++
					x := n.Properties().PandocTask
					if len(x.CheckSegments) == 0 {
						t.Fatal("lost raw check fragments")
					}
					if _, err := NewNode(n.spec); err != nil {
						t.Fatal(err)
					}
				}
				return true
			})
			if found != 1 {
				t.Fatal("literal task ownership missing", pandocKinds(r.Document.Root()))
			}
		})
	}
	input := "(@e\u0301) invalid\n\n(@é) valid\n\n(@) next\n"
	r := pandocBlocks(t, input)
	var starts []int
	Walk(r.Document.Root(), func(n *Node) bool {
		if n.Kind() == ListItem {
			starts = append(starts, n.Properties().PandocList.Start)
		}
		return true
	})
	if !reflect.DeepEqual(starts, []int{1, 2}) {
		t.Fatal("invalid label changed numbering", starts)
	}
}

func TestPandocOriginalTaskSourceOffsets(t *testing.T) {
	input := "> - [X] é\r\n> - [x] yes\r\n> - [ ] no"
	r := pandocBlocks(t, input)
	items := r.Document.Root().Children()[0].Children()[0].Children()
	wants := []struct {
		start, end, markerStart, markerEnd, taskStart, taskEnd, separatorEnd int
		checked                                                              bool
	}{{2, 12, 2, 3, 4, 7, 8, true}, {14, 25, 14, 15, 16, 19, 20, true}, {27, 35, 27, 28, 29, 32, 33, false}}
	if len(items) != len(wants) {
		t.Fatal("lost original task items")
	}
	for i, w := range wants {
		x := items[i].Properties()
		if items[i].Span().Start() != w.start || items[i].Span().End() != w.end || x.PandocList.Marker.Start() != w.markerStart || x.PandocList.Marker.End() != w.markerEnd || x.PandocTask.Marker.Start() != w.taskStart || x.PandocTask.Marker.End() != w.taskEnd || x.PandocTask.Separator.Start() != w.taskEnd || x.PandocTask.Separator.End() != w.separatorEnd || x.PandocTask.Checked != w.checked {
			t.Fatalf("original source facts lost at%d: %+v %+v", i, x.PandocList, x.PandocTask)
		}
	}
}

func TestPandocDefaultAndSelectedNumberFamilies(t *testing.T) {
	cases := []struct {
		input, style string
		starts       []int
		decimal      []bool
	}{
		{"#. first\n2. second\n#. third\n", "default", []int{1, 2, 1}, []bool{false, true, false}},
		{"i. first\n#. second\nii. third\n", "lower-roman", []int{1, 1, 2}, []bool{false, false, false}},
		{"(@first) first\n(#) second\n(@next) third\n", "example", []int{1, 1, 2}, []bool{false, false, false}},
	}
	for _, tt := range cases {
		t.Run(tt.style, func(t *testing.T) {
			r := pandocBlocks(t, tt.input)
			children := r.Document.Root().Children()
			if len(children) != 1 || children[0].Kind() != List {
				t.Fatal("compatible marker family split")
			}
			items := children[0].Children()
			if len(items) != len(tt.starts) {
				t.Fatal("lost sibling")
			}
			for i, n := range items {
				x := n.Properties().PandocList
				if x.Style != tt.style || x.Start != tt.starts[i] || x.Decimal.Present() != tt.decimal[i] {
					t.Fatalf("marker family%d %+v", i, x)
				}
			}
		})
	}
}

func TestPandocBlankPipeClassificationIsBudgeted(t *testing.T) {
	for _, suffix := range []string{strings.Repeat("\t", 4096), strings.Repeat("\t", 4096) + "notblank"} {
		limits := DefaultLimits()
		limits.WorkUnits = 100
		budget, _ := NewOperationBudget(limits)
		source := NewSource([]byte("|" + suffix + "\n"))
		ctx := &parserContext{Context: context.Background(), budget: &parseBudget{operation: budget}, profile: pandocBlocksProfile}
		index := LineIndex{ctx: ctx, source: source}
		p := blockParser{ctx: ctx, index: index}
		line := blockLine{physicalLine: physicalLine{start: 0, end: 1 + len(suffix), next: 2 + len(suffix)}, first: '|', contentStart: 0, cursor: Indentation{index: index, end: 1 + len(suffix)}}
		func() {
			defer func() {
				stop, ok := recover().(budgetStop)
				if !ok || stop.exceeded.Name != "work-units" {
					t.Fatal("blank-pipe suffix scanned without reserving work")
				}
			}()
			p.pandocLineStart(line)
		}()
	}
}

func TestPandocParagraphBoundariesAreNotCoreInterruption(t *testing.T) {
	cases := []struct {
		input   string
		pandoc  []Kind
		coreTop []Kind
	}{
		{"one\ntwo\n---\n", []Kind{DocumentKind, Paragraph}, []Kind{Heading}},
		{"one\n***\n", []Kind{DocumentKind, Paragraph}, []Kind{Paragraph, ThematicBreak}},
		{"one\n\n***\n", []Kind{DocumentKind, Paragraph, ThematicBreak}, []Kind{Paragraph, ThematicBreak}},
		{"one\n~~~\nbody\n~~~\n", []Kind{DocumentKind, Paragraph}, []Kind{Paragraph, CodeBlock}},
		{"one\n\n~~~\nbody\n~~~\n", []Kind{DocumentKind, Paragraph, CodeBlock}, []Kind{Paragraph, CodeBlock}},
		{"one\n```\nbody\n```\n", []Kind{DocumentKind, Paragraph, CodeBlock}, []Kind{Paragraph, CodeBlock}},
		{"- prose\n  * * *\n", []Kind{DocumentKind, List, ListItem, Paragraph}, []Kind{List}},
		{"- prose\n\n  * * *\n", []Kind{DocumentKind, List, ListItem, Paragraph, ThematicBreak}, []Kind{List}},
		{" one\n---\n", []Kind{DocumentKind, Heading}, []Kind{Heading}},
		{"one\n ---\n", []Kind{DocumentKind, Paragraph}, []Kind{Heading}},
		{" # heading\n", []Kind{DocumentKind, Paragraph}, []Kind{Heading}},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			r := pandocBlocks(t, tt.input)
			if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, tt.pandoc) {
				t.Fatalf("Pandoc%v want%v", got, tt.pandoc)
			}
			for _, gfm := range []bool{false, true} {
				budget, _ := NewOperationBudget(DefaultLimits())
				source := NewSource([]byte(tt.input))
				core := ParseWithBudget(context.Background(), source, budget)
				if gfm {
					budget, _ = NewOperationBudget(DefaultLimits())
					core = ParseGFMWithBudget(context.Background(), source, budget)
				}
				var got []Kind
				for _, n := range core.Document.Root().Children() {
					got = append(got, n.Kind())
				}
				if !reflect.DeepEqual(got, tt.coreTop) {
					t.Fatalf("core/GFM isolation%v want%v", got, tt.coreTop)
				}
			}
		})
	}
}
func TestPandocReaderTabFramesRetainOriginalSource(t *testing.T) {
	cases := []struct {
		input     string
		want      []Kind
		coreInner Kind
	}{
		{"> \tcode\n", []Kind{DocumentKind, BlockQuote, CodeBlock}, Paragraph},
		{">\tcode\n", []Kind{DocumentKind, BlockQuote, CodeBlock}, Paragraph},
		{"> \t- item\n", []Kind{DocumentKind, BlockQuote, CodeBlock}, List},
		{"- outer\n\n  \tcode\n", []Kind{DocumentKind, List, ListItem, Paragraph, CodeBlock}, Paragraph},
		{"> - outer\n>\n>   \tcode\n", []Kind{DocumentKind, BlockQuote, List, ListItem, Paragraph, CodeBlock}, Paragraph},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			r := pandocBlocks(t, tt.input)
			if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("reader tab frame%v want%v", got, tt.want)
			}
			if string(r.Source.Bytes()) != tt.input {
				t.Fatal("source changed")
			}
			for _, gfm := range []bool{false, true} {
				budget, _ := NewOperationBudget(DefaultLimits())
				source := NewSource([]byte(tt.input))
				core := ParseWithBudget(context.Background(), source, budget)
				if gfm {
					budget, _ = NewOperationBudget(DefaultLimits())
					core = ParseGFMWithBudget(context.Background(), source, budget)
				}
				found := false
				Walk(core.Document.Root(), func(n *Node) bool {
					if n.Kind() == tt.coreInner {
						found = true
					}
					return true
				})
				if !found {
					t.Fatal("core/GFM tab interpretation changed")
				}
			}
		})
	}
	r := pandocBlocks(t, "> -\tΩ\r\n")
	item := r.Document.Root().Children()[0].Children()[0].Children()[0]
	x := item.Properties().PandocList
	if x.MarkerColumn != 0 || x.ContentColumn != 4 || x.SourceMarkerColumn != 2 || x.SourceContentColumn != 4 || x.Marker.Start() != 2 || x.Marker.End() != 3 || x.Separator.Start() != 3 || x.Separator.End() != 4 {
		t.Fatalf("reader/physical frames conflated %+v", x)
	}
}
func TestPandocDefinitionBodyTaskOwnership(t *testing.T) {
	for _, input := range []string{"Term\n: [x] body\n", "Term\n\n: [ ]", "Term\n: [X]\n", "Term\n: - [x] nested\n"} {
		t.Run(input, func(t *testing.T) {
			r := pandocBlocks(t, input)
			body := r.Document.Root().Children()[0].Children()[0].Children()[1]
			nested := strings.Contains(input, ": -")
			if (body.Properties().PandocTask != nil) == nested {
				t.Fatal("definition task first-direct-child ownership wrong")
			}
			if !nested {
				if body.Children()[0].Properties().PandocTask == nil {
					t.Fatal("definition task paragraph association missing")
				}
			}
		})
	}
}

func TestPandocLineAndDefinitionMetadataIsolation(t *testing.T) {
	r := pandocBlocks(t, "|   é\n")
	line := r.Document.Root().Children()[0].Children()[0]
	before := line.Properties()
	copy := line.Properties()
	copy.PandocLine.Indentation.Segments[0].Source = Span{}
	if !reflect.DeepEqual(before, line.Properties()) {
		t.Fatal("line indentation alias")
	}
	s := line.spec
	s.Properties = cloneProperties(s.Properties)
	s.Properties.PandocLine.Indentation.Semantic = Some("spaces")
	if _, err := NewNode(s); err == nil {
		t.Fatal("invented indentation projection accepted")
	}
	r = pandocBlocks(t, "Term\n: body\n")
	item := r.Document.Root().Children()[0].Children()[0]
	copy = item.Properties()
	copy.Definition.Tight = false
	if !item.Properties().Definition.Tight {
		t.Fatal("definition property alias")
	}
	s = item.spec
	s.Properties = cloneProperties(s.Properties)
	s.Properties.Definition.Tight = false
	if _, err := NewNode(s); err == nil {
		t.Fatal("false compactness accepted")
	}
}

func TestPandocBlankPipeLineSemantics(t *testing.T) {
	for _, raw := range []string{"|\n", "|  \n", "|\t\n", "| \t\r\n"} {
		t.Run(raw, func(t *testing.T) {
			r := pandocBlocks(t, raw)
			line := r.Document.Root().Children()[0].Children()[0]
			x := line.Properties().PandocLine
			if !x.Blank || x.Indentation.Semantic != Some("") || len(line.Content()) != 0 {
				t.Fatalf("blank pipe acquired semantic indentation %+v", x)
			}
			if line.Span().Start() != 0 || line.Span().End() != len(raw) {
				t.Fatal("raw blank-pipe extent lost")
			}
		})
	}
	r := pandocBlocks(t, "|  \n wrapped\n")
	if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, []Kind{DocumentKind, LineBlock, LineBlockLine, Paragraph}) {
		t.Fatal("blank pipe took a continuation", got)
	}
	r = pandocBlocks(t, "|   text\n wrapped\n")
	line := r.Document.Root().Children()[0].Children()[0]
	if line.Properties().PandocLine.Blank || line.Properties().PandocLine.Indentation.Semantic != Some("\u00a0\u00a0") || len(r.Document.Root().Children()) != 1 {
		t.Fatal("nonempty line indentation/wrap lost")
	}
	r = pandocBlocks(t, "|\ttext\n")
	if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, []Kind{DocumentKind, Paragraph}) {
		t.Fatal("non-space pipe opener recognized", got)
	}
}

func TestPandocExpandedTabLineContinuation(t *testing.T) {
	input := "- | text\n\twrapped\n"
	r := pandocBlocks(t, input)
	if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, []Kind{DocumentKind, List, ListItem, LineBlock, LineBlockLine}) {
		t.Fatal("expanded parent-tab spaces lost continuation", got)
	}
	line := r.Document.Root().Children()[0].Children()[0].Children()[0].Children()[0]
	parts := line.Content()
	if len(parts) != 4 || parts[0].Source.Start() != 4 || parts[0].Source.End() != 8 || parts[1].Source.Start() != 8 || parts[1].Source.End() != 9 || parts[1].Semantic != Some(" ") || parts[2].Transformation != PartialTab || parts[2].Source.Start() != 9 || parts[2].Source.End() != 10 || parts[2].Semantic != Some(" ") || parts[3].Source.Start() != 10 || parts[3].Source.End() != 17 {
		t.Fatalf("partial-tab provenance %+v", parts)
	}
	if line.Markers()[1].Role != "line-continuation" || line.Markers()[1].Source != parts[2].Source {
		t.Fatal("expanded tab must retain shared original prefix/payload byte")
	}
	r = pandocBlocks(t, "- outer\n\n\t| text\n")
	for _, n := range r.Document.Root().Children()[0].Children()[0].Children() {
		if n.Kind() == LineBlock {
			t.Fatal("residual leading indentation became pipe opening")
		}
	}
	r = pandocBlocks(t, "-  \t| text\n")
	if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, []Kind{DocumentKind, List, ListItem, LineBlock, LineBlockLine}) {
		t.Fatal("fully consumed tab prevented valid pipe opening", got)
	}
}

func TestPandocDefinitionLookaheadRestoresNestedReaderFrame(t *testing.T) {
	input := "- > 1. Term\n\t>\n\t>\t: body\n"
	r := pandocBlocks(t, input)
	want := []Kind{DocumentKind, List, ListItem, BlockQuote, List, ListItem, DefinitionList, DefinitionItem, DefinitionTerm, DefinitionBody, Paragraph}
	if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, want) {
		t.Fatalf("lookahead reader frames %v want%v", got, want)
	}
	var term, body *Node
	Walk(r.Document.Root(), func(n *Node) bool {
		if n.Kind() == DefinitionTerm {
			term = n
		}
		if n.Kind() == DefinitionBody {
			body = n
		}
		return true
	})
	if term.Span().Start() != 7 || term.Span().End() != 12 || body.Span().Start() != 17 || body.Span().End() != 25 {
		t.Fatalf("nested source envelopes term%+v body%+v", term.Span(), body.Span())
	}
	x := body.Properties().PandocList
	if x.MarkerColumn != 1 || x.SourceMarkerColumn != 8 || x.ContentColumn != 3 || x.SourceContentColumn != 10 || x.Marker.Start() != 18 || x.Marker.End() != 19 {
		t.Fatalf("nested source/reader frames %+v", x)
	}
}

func TestPandocQuoteExtractionContext(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		for _, payload := range []string{"first", "é"} {
			input := "> 1. " + payload + ending + "2. second" + ending
			r := pandocBlocks(t, input)
			want := []Kind{DocumentKind, BlockQuote, List, ListItem, Paragraph, ListItem, Paragraph}
			if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, want) {
				t.Fatalf("%q: %v", input, got)
			}
			quote := r.Document.Root().Children()[0]
			list := quote.Children()[0]
			if quote.Span().End() != len(input) || list.Span().End() != len(input) || len(quote.Markers()) != 1 {
				t.Fatal("quote/list envelope or invented prefix")
			}
			second := list.Children()[1]
			marker := second.Properties().PandocList.Marker
			start := len("> 1. " + payload + ending)
			if marker.Start() != start || marker.End() != start+2 {
				t.Fatal("second marker source", marker)
			}
			input = "> | " + payload + ending + " wrapped" + ending
			r = pandocBlocks(t, input)
			want = []Kind{DocumentKind, BlockQuote, LineBlock, LineBlockLine}
			if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, want) {
				t.Fatalf("wrap %q %v", input, got)
			}
			quote = r.Document.Root().Children()[0]
			line := quote.Children()[0].Children()[0]
			if quote.Span().End() != len(input) || line.Span().End() != len(input) || len(quote.Markers()) != 1 {
				t.Fatal("wrapped ownership")
			}
			parts := line.Content()
			prefix := len("> | " + payload + ending)
			if len(parts) != 3 || parts[0].Source.Start() != 4 || parts[0].Source.End() != 4+len(payload) || parts[1].Source.Start() != 4+len(payload) || parts[1].Source.End() != prefix || parts[2].Source.Start() != prefix+1 || parts[2].Source.End() != len(input)-len(ending) {
				t.Fatalf("wrap mapping %+v", parts)
			}
		}
	}
	tests := []struct {
		input string
		want  []Kind
	}{
		{"> # heading\nordinary\n", []Kind{DocumentKind, BlockQuote, Heading, Paragraph}},
		{"> ---\nordinary\n", []Kind{DocumentKind, BlockQuote, ThematicBreak, Paragraph}},
		{"> Term\n>\n: body\n", []Kind{DocumentKind, BlockQuote, DefinitionList, DefinitionItem, DefinitionTerm, DefinitionBody, Paragraph}},
		{"> 1. first\n\n2. second\n", []Kind{DocumentKind, BlockQuote, List, ListItem, Paragraph, List, ListItem, Paragraph}},
		{"> | text\n\n wrapped\n", []Kind{DocumentKind, BlockQuote, LineBlock, LineBlockLine, Paragraph}},
		{"- > 1. first\n  2. second\n", []Kind{DocumentKind, List, ListItem, BlockQuote, List, ListItem, Paragraph, Paragraph}},
		{"- > | text\n   wrapped\n", []Kind{DocumentKind, List, ListItem, BlockQuote, LineBlock, LineBlockLine}},
		{"- > # title\n  plain\n", []Kind{DocumentKind, List, ListItem, BlockQuote, Heading, Paragraph}},
	}
	for _, tt := range tests {
		r := pandocBlocks(t, tt.input)
		if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("context %q got %v want %v", tt.input, got, tt.want)
		}
	}
	// Core/GFM retain the non-one marker as paragraph continuation, not a sibling.
	for _, profile := range []parserProfile{coreProfile, gfmInProgressProfile} {
		budget, _ := NewOperationBudget(DefaultLimits())
		r := parseWithProfile(context.Background(), NewSource([]byte("> 1. first\n2. second\n")), budget, profile)
		if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, []Kind{DocumentKind, BlockQuote, List, ListItem, Paragraph, Text, SoftBreak, Text}) {
			t.Fatal("core/GFM quote behavior changed", got)
		}
	}
}

func TestPandocCapitalTabLogicalGuard(t *testing.T) {
	for _, marker := range []string{"B.", "I.", "V.", "X.", "C."} {
		for _, prefix := range []string{"", "> ", "- "} {
			if prefix == "- " && marker != "I." {
				continue
			}
			input := prefix + marker + "\tpayload\r\n"
			r := pandocBlocks(t, input)
			var found *Node
			Walk(r.Document.Root(), func(n *Node) bool {
				if n.Kind() == ListItem && n.Properties().PandocList.Style != "bullet" {
					found = n
				}
				return true
			})
			if found == nil {
				t.Fatalf("valid logical columns rejected %q", input)
			}
			p := found.Properties().PandocList
			if p.Decimal.Present() || p.SourceMarkerColumn != len(prefix) || p.SourceContentColumn != map[bool]int{true: 4, false: 8}[prefix == ""] || p.Separator.Start() != len(prefix)+len(marker) || p.Separator.End() != len(prefix)+len(marker)+1 || p.SeparatorColumns != 2 || p.ContentColumn != 4 {
				t.Fatalf("columns %q %+v", input, p)
			}
		}
	}
	for _, input := range []string{" B.\tpayload\n", " I.\tpayload\n", "B. payload\n", ">  B.\tpayload\n"} {
		r := pandocBlocks(t, input)
		Walk(r.Document.Root(), func(n *Node) bool {
			if n.Kind() == List {
				t.Fatalf("one-column guard accepted %q", input)
			}
			return true
		})
	}
}

func TestPandocOverflowResetIsAbsent(t *testing.T) {
	huge := "999999999999999999999999999999"
	input := "(@) one\n\ntext\n\n(" + huge + "@label) two\n\ntext\n\n(@) three\n\ntext\n\n(@label) reuse\n"
	r := pandocBlocks(t, input)
	var starts []int
	Walk(r.Document.Root(), func(n *Node) bool {
		if n.Kind() == ListItem {
			x := n.Properties().PandocList
			starts = append(starts, x.Start)
			if x.Reset.Present {
				t.Fatal("overflow gained effective reset")
			}
			if _, err := NewNode(n.spec); err != nil {
				t.Fatal(err)
			}
		}
		return true
	})
	if !reflect.DeepEqual(starts, []int{1, 2, 3, 2}) {
		t.Fatal(starts)
	}
	for _, prefix := range []string{"0", "12", strconv.Itoa(int(^uint(0) >> 1))} {
		r := pandocBlocks(t, "("+prefix+"@label) text\n")
		p := r.Document.Root().Children()[0].Children()[0].Properties().PandocList
		n, _ := strconv.Atoi(prefix)
		if p.Reset != Some(n) || p.Start != n {
			t.Fatal("valid reset changed", p)
		}
	}
}

func TestPandocConstructorSourceAssociation(t *testing.T) {
	for _, input := range []string{"- body\n", "12. body\n", "(@label) body\n"} {
		r := pandocBlocks(t, input)
		s := r.Document.Root().Children()[0].Children()[0].spec
		s.Properties = cloneProperties(s.Properties)
		s.Kind = DefinitionBody
		s.Properties.List = nil
		if _, err := NewNode(s); !errors.Is(err, ErrModel) {
			t.Fatal("nondefinition body accepted", input, err)
		}
	}
	for _, marker := range []string{":", "~"} {
		r := pandocBlocks(t, "Term\n"+marker+" body\n")
		body := r.Document.Root().Children()[0].Children()[0].Children()[1]
		if _, err := NewNode(body.spec); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{"(12@12)", "12@12)", "12@12.", "(12@é)", "@é)", "(@)"} {
		r := pandocBlocks(t, raw+" text\n")
		n := r.Document.Root().Children()[0].Children()[0]
		s := n.spec
		s.Properties = cloneProperties(s.Properties)
		if _, err := NewNode(s); err != nil {
			t.Fatal("valid label rejected", raw, err)
		}
		x := s.Properties.PandocList
		good := x.Label
		if good.Start() != strings.Index(raw, "@")+1 {
			t.Fatal("parser label anchor")
		}
		x.Label, _ = r.Source.Span(0, 0)
		if _, err := NewNode(s); !errors.Is(err, ErrModel) {
			t.Fatal("wrong occurrence accepted", raw, err)
		}
		if strings.Contains(raw, "12@12") {
			x.Label, _ = r.Source.Span(strings.Index(raw, "12"), strings.Index(raw, "12")+2)
			if _, err := NewNode(s); !errors.Is(err, ErrModel) {
				t.Fatal("reset occurrence accepted")
			}
		}
	}
	r := pandocBlocks(t, "[foo x] tail\n")
	s := r.Document.Root().Children()[0].spec
	span := func(a, b int) Span { x, _ := r.Source.Span(a, b); return x }
	s.Properties.PandocTask = &PandocTaskProperties{Marker: span(0, 7), Check: span(5, 6), Separator: span(7, 8), Segments: []Span{span(0, 1), span(5, 7)}, CheckSegments: []Span{span(5, 6)}, Checked: true, CheckSpelling: "x", InlinePending: true}
	if _, err := NewNode(s); !errors.Is(err, ErrModel) {
		t.Fatal("ordinary task bytes disappeared", err)
	}
}

func TestPandocWorkCostsStayWide(t *testing.T) {
	max32 := int64(math.MaxInt32) / 16
	for _, tt := range []struct {
		base  int64
		terms []int64
		want  int64
	}{
		{64, []int64{40 << 20, 64}, 64 + 64*(40<<20)},
		{64, []int64{max32, 32}, 64 + 32*max32},
		{64, []int64{max32, 64, max32, 8}, 64 + 72*max32},
		{0, []int64{3 * max32, 8}, 24 * max32},
		{64, []int64{max32, 8, max32, 8, max32, 8}, 64 + 24*max32},
		{0, []int64{math.MaxInt64/8 + 1, 8}, math.MaxInt64},
		{64, []int64{math.MaxInt64 / 8, 8}, math.MaxInt64},
		{-1, nil, math.MaxInt64}, {0, []int64{-1, 8}, math.MaxInt64}, {0, []int64{1, -8}, math.MaxInt64}, {0, []int64{1}, math.MaxInt64},
	} {
		if got := pandocWork(tt.base, tt.terms...); got != tt.want || got < 0 {
			t.Fatalf("wide cost %+v got %d", tt, got)
		}
	}
	cost := pandocWork(64, max32, 64, max32, 8)
	for _, delta := range []int64{-1, 0, 1} {
		l := DefaultLimits()
		l.WorkUnits = cost + delta
		op := budgetFor(t, l)
		ctx := &parserContext{Context: context.Background(), budget: &parseBudget{operation: op}}
		stopped := false
		func() {
			defer func() {
				if v := recover(); v != nil {
					stop, ok := v.(budgetStop)
					if !ok {
						panic(v)
					}
					if stop.exceeded.Observed != cost {
						t.Fatal("wrong wide observed", stop)
					}
					stopped = true
				}
			}()
			meterPandocWork(ctx, cost)
		}()
		if stopped != (delta < 0) {
			t.Fatal("wide budget boundary", delta, stopped)
		}
		if !stopped && op.work != cost {
			t.Fatal("work undercharged", op.work)
		}
	}
	for _, cost := range []int64{-1, pandocWork(0, math.MaxInt64, 64)} {
		op := budgetFor(t, DefaultLimits())
		op.work = 17
		ctx := &parserContext{Context: context.Background(), budget: &parseBudget{operation: op}}
		stopped := false
		func() {
			defer func() {
				if v := recover(); v != nil {
					if _, ok := v.(budgetStop); !ok {
						panic(v)
					}
					stopped = true
				}
			}()
			meterPandocWork(ctx, cost)
		}()
		if !stopped || op.work != 17 {
			t.Fatal("invalid cost refunded work")
		}
	}
}

func TestPandocReaderBacktickCursorAndOuterSetext(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		for indent := 1; indent <= 3; indent++ {
			input := "one" + ending + strings.Repeat(" ", indent) + "```" + ending + "body" + ending + strings.Repeat(" ", indent) + "```" + ending
			r := pandocBlocks(t, input)
			children := r.Document.Root().Children()
			if len(children) != 1 || children[0].Kind() != Paragraph || children[0].Span().End() != len(input) || len(children[0].Markers()) != 0 {
				t.Fatal("indented inline-looking fence owns block", input, pandocKinds(r.Document.Root()))
			}
		}
	}
	for _, input := range []string{"one\n```\nbody\n```\n", "one\n\n ```\nbody\n ```\n"} {
		r := pandocBlocks(t, input)
		if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, []Kind{DocumentKind, Paragraph, CodeBlock}) {
			t.Fatal("fence control", got)
		}
	}
	for _, ending := range []string{"\n", "\r\n"} {
		for _, text := range []string{"> heading", "> é", "| line", "1. ordered", "(@bird) example"} {
			input := text + ending + "===" + ending
			r := pandocBlocks(t, input)
			children := r.Document.Root().Children()
			if len(children) != 1 || children[0].Kind() != Heading {
				t.Fatal("outer Setext priority", input, pandocKinds(r.Document.Root()))
			}
			n := children[0]
			m := n.Markers()
			parts := n.Content()
			if n.Span().End() != len(input) || len(m) != 1 || m[0].Role != "underline" || m[0].Source.Start() != len(text+ending) || m[0].Source.End() != len(text+ending)+3 || len(parts) != 1 || parts[0].Source.Start() != 0 || parts[0].Source.End() != len(text) {
				t.Fatal("outer heading source", n.spec)
			}
		}
	}
	for _, tt := range []struct {
		input string
		want  []Kind
	}{
		{"> heading\n> ===\n", []Kind{DocumentKind, BlockQuote, Heading}},
		{"- heading\n===\n", []Kind{DocumentKind, List, ListItem, Heading}},
		{"```\n===\n```\n", []Kind{DocumentKind, CodeBlock}},
		{"| heading\n===\n", []Kind{DocumentKind, Heading}},
	} {
		r := pandocBlocks(t, tt.input)
		if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("precedence control %q %v", tt.input, got)
		}
	}
}

func TestPandocRepairedPrecedenceExactSourceAndIsolation(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		for _, tt := range []struct {
			first, underline string
			contentStart     int
			want             []Kind
		}{
			{"- é", "===", 2, []Kind{DocumentKind, List, ListItem, Heading}},
			{"- > é", "  ===", 2, []Kind{DocumentKind, List, ListItem, Heading}},
			{"> - é", "> ===", 4, []Kind{DocumentKind, BlockQuote, List, ListItem, Heading}},
		} {
			input := tt.first + ending + tt.underline + ending
			r := pandocBlocks(t, input)
			if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("nested heading %q %v", input, got)
			}
			var heading *Node
			Walk(r.Document.Root(), func(n *Node) bool {
				if n.Kind() == Heading {
					heading = n
				}
				if n.Kind() == ListItem {
					for _, m := range n.Markers() {
						if m.Role == "item-indent" && !strings.HasPrefix(tt.underline, "  ") {
							t.Fatal("invented missing item prefix")
						}
					}
				}
				return true
			})
			m := heading.Markers()[0].Source
			start := len(tt.first+ending) + strings.Index(tt.underline, "=")
			if m.Start() != start || m.End() != start+3 || heading.Span().Start() != tt.contentStart || heading.Span().End() != len(input) {
				t.Fatal("heading exact ownership", heading.spec)
			}
		}
	}
	// These core/GFM decisions remain distinct from the selected reader guards.
	for _, profile := range []parserProfile{coreProfile, gfmInProgressProfile} {
		for _, input := range []string{"one\n ```\nbody\n ```\n", "> heading\n===\n", "B.\tpayload\n"} {
			budget, _ := NewOperationBudget(DefaultLimits())
			r := parseWithProfile(context.Background(), NewSource([]byte(input)), budget, profile)
			children := r.Document.Root().Children()
			switch input[0] {
			case 'o':
				if len(children) != 2 || children[1].Kind() != CodeBlock {
					t.Fatal("core/GFM fence isolation")
				}
			case '>':
				if children[0].Kind() != BlockQuote {
					t.Fatal("core/GFM outer quote isolation")
				}
			case 'B':
				if children[0].Kind() != Paragraph {
					t.Fatal("core/GFM alpha isolation")
				}
			}
		}
	}
}

func TestPandocInapplicableSourceAuthorityRejected(t *testing.T) {
	for _, input := range []string{"- body\n", "Term\n: body\n", "Term\n~ body\n"} {
		r := pandocBlocks(t, input)
		Walk(r.Document.Root(), func(n *Node) bool {
			p := n.Properties().PandocList
			if p == nil || n.Kind() == List {
				return true
			}
			for _, field := range []string{"decimal", "label"} {
				s := n.spec
				s.Properties = cloneProperties(s.Properties)
				if field == "decimal" {
					s.Properties.PandocList.Decimal = p.Marker
				} else {
					s.Properties.PandocList.Label = p.Marker
				}
				if _, err := NewNode(s); !errors.Is(err, ErrModel) {
					t.Fatal("inapplicable source authority", input, field, err)
				}
			}
			return true
		})
	}
}

func TestPandocPageReferenceOriginalCursor(t *testing.T) {
	cases := []struct {
		prefix, marker, separator                        string
		markerColumn, contentColumn, sourceContentColumn int
	}{
		{"", "p.", "  ", 0, 4, 4}, {"", "p.", " \t", 0, 4, 4},
		{"", "p.", "\t ", 0, 5, 5}, {"", "(p)", " ", 0, 4, 4}, {"", "(p)", "\t", 0, 4, 4},
		{" ", "p.", "  ", 1, 5, 5}, {"> ", "p.", "  ", 0, 4, 6},
		{"> ", "p.", " \t", 0, 4, 8}, {"> ", "(p)", " ", 0, 4, 6},
		{">  ", "p.", "  ", 1, 5, 7},
	}
	for _, ending := range []string{"\n", "\r\n"} {
		for _, payload := range []string{"42", "42é"} {
			for _, tt := range cases {
				input := tt.prefix + tt.marker + tt.separator + payload + ending
				t.Run(input, func(t *testing.T) {
					r := pandocBlocks(t, input)
					want := []Kind{DocumentKind, List, ListItem, Paragraph}
					if strings.HasPrefix(tt.prefix, ">") {
						want = []Kind{DocumentKind, BlockQuote, List, ListItem, Paragraph}
					}
					if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, want) {
						t.Fatalf("page-boundary kinds %v want %v", got, want)
					}
					var item *Node
					Walk(r.Document.Root(), func(n *Node) bool {
						if n.Kind() == ListItem {
							item = n
						}
						return true
					})
					wantDelimiter := "period"
					if tt.marker == "(p)" {
						wantDelimiter = "two-parens"
					}
					x := item.Properties().PandocList
					markerStart := len(tt.prefix)
					sepStart := markerStart + len(tt.marker)
					payloadStart := sepStart + len(tt.separator)
					if x.Style != "lower-alpha" || x.MarkerStyle != "lower-alpha" || x.Delimiter != wantDelimiter || x.Start != 16 || x.Decimal.Present() || x.Label.Present() || x.Reset.Present {
						t.Fatalf("wrong marker facts %+v", x)
					}
					if x.Marker.Start() != markerStart || x.Marker.End() != sepStart || x.Separator.Start() != sepStart || x.Separator.End() != payloadStart || x.MarkerColumn != tt.markerColumn || x.ContentColumn != tt.contentColumn || x.SourceMarkerColumn != markerStart || x.SourceContentColumn != tt.sourceContentColumn || x.SeparatorColumns != tt.contentColumn-tt.markerColumn-len(tt.marker) || x.SeparatorPartialTab {
						t.Fatalf("wrong original/reader spans %+v", x)
					}
					para := item.Children()[0]
					parts := para.Content()
					if len(parts) != 1 || parts[0].Source.Start() != payloadStart || parts[0].Source.End() != payloadStart+len(payload) || para.Span().End() != len(input) || item.Span().End() != len(input) {
						t.Fatal("payload ownership", para.spec)
					}
					if raw, _ := r.Source.Text(x.Marker); raw != tt.marker {
						t.Fatal("marker spelling", raw)
					}
					if raw, _ := r.Source.Text(x.Separator); raw != tt.separator {
						t.Fatal("separator spelling", raw)
					}
					if string(r.Source.Bytes()) != input {
						t.Fatal("source changed")
					}
					if _, err := NewNode(item.spec); err != nil {
						t.Fatal("actual model rejected", err)
					}
					// Neither core nor GFM recognizes alphabetic list markers.
					for _, profile := range []parserProfile{coreProfile, gfmInProgressProfile} {
						budget, _ := NewOperationBudget(DefaultLimits())
						core := parseWithProfile(context.Background(), NewSource([]byte(input)), budget, profile)
						Walk(core.Document.Root(), func(n *Node) bool {
							if n.Kind() == List {
								t.Fatal("core/GFM isolation")
							}
							return true
						})
					}
				})
			}
		}
	}
	for _, ending := range []string{"\n", "\r\n"} {
		for _, prefix := range []string{"", " ", "> "} {
			for _, separator := range []string{" ", "\t"} {
				input := prefix + "p." + separator + "42" + ending
				r := pandocBlocks(t, input)
				want := []Kind{DocumentKind, Paragraph}
				if strings.HasPrefix(prefix, ">") {
					want = []Kind{DocumentKind, BlockQuote, Paragraph}
				}
				if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, want) {
					t.Fatalf("exact exclusion lost %q %v", input, got)
				}
			}
		}
	}
}

func TestPandocOrderedMarkerRequiresPunctuation(t *testing.T) {
	// Build otherwise consistent explicit metadata over original bare text. The
	// malicious legacy delimiter equals the final nonpunctuation byte as well.
	for _, tt := range []struct {
		raw, style string
		start      int
	}{
		{"1", "decimal", 1}, {"(1", "decimal", 1}, {"b", "lower-alpha", 2}, {"(b", "lower-alpha", 2},
		{"I", "upper-roman", 1}, {"(I", "upper-roman", 1}, {"iv", "lower-roman", 4},
		{"#", "default", 1}, {"(#", "default", 1}, {"@bird", "example", 1}, {"(@bird", "example", 1},
		{"12@12", "example", 12}, {"(12@12", "example", 12}, {"@é", "example", 1},
	} {
		t.Run(tt.raw, func(t *testing.T) {
			source := NewSource([]byte(tt.raw + " text\n"))
			span := func(a, b int) Span {
				s, err := source.Span(a, b)
				if err != nil {
					t.Fatal(err)
				}
				return s
			}
			marker := span(0, len(tt.raw))
			width := len([]rune(tt.raw))
			indent := width + 1
			if tt.style == "example" {
				indent = 4
			}
			delim := "period"
			if tt.style == "default" {
				delim = "default"
			}
			p := &PandocListProperties{Style: tt.style, MarkerStyle: tt.style, Delimiter: delim, Start: tt.start, Marker: marker, ContentColumn: width, SourceContentColumn: width, ContinuationIndent: indent}
			start := 0
			if strings.HasPrefix(tt.raw, "(") {
				start = 1
			}
			if tt.style == "decimal" {
				p.Decimal = span(start, len(tt.raw))
			}
			if at := strings.IndexByte(tt.raw, '@'); at >= 0 {
				p.Label = span(at+1, len(tt.raw))
				if at > start {
					p.Reset = Some(12)
				}
			}
			for _, legacy := range []byte{tt.raw[len(tt.raw)-1], '.', ')'} {
				spec := NodeSpec{Kind: ListItem, Envelope: span(0, source.Len()), Markers: []Marker{{Role: "item-marker", Source: marker}}, Properties: Properties{PandocList: p, List: &ListProperties{Ordered: true, Start: Some(tt.start), Delimiter: Some(legacy), MarkerWidth: Some(width), Padding: Some(1), ContentIndent: Some(indent), Prefix: &Value{Segments: []Segment{{Source: marker, Transformation: Exact, Semantic: Some(tt.raw)}}, Semantic: Some(tt.raw)}}}}
				if _, err := NewNode(spec); !errors.Is(err, ErrModel) {
					t.Fatalf("bare ordered marker accepted raw=%q legacy=%q err=%v", tt.raw, legacy, err)
				}
			}
		})
	}
	for _, raw := range []string{"12.", "12)", "(12)", "b.", "b)", "(b)", "IV.", "IV)", "(IV)", "#.", "#)", "(#)", "@bird.", "@bird)", "(@bird)", "(12@12)", "(@é)", "(@)"} {
		r := pandocBlocks(t, raw+"  body\r\n")
		item := r.Document.Root().Children()[0].Children()[0]
		n, err := NewNode(item.spec)
		if err != nil {
			t.Fatal("actual punctuated model rejected", raw, err)
		}
		before := n.Properties()
		copy := item.Properties()
		copy.PandocList.Delimiter = "evil"
		if !reflect.DeepEqual(before, n.Properties()) {
			t.Fatal("constructor/accessor alias")
		}
	}
}

// Direct ownership matters here: preorder kinds are identical when the final
// paragraph has incorrectly escaped its item.
func TestPandocItemExtractionOwnsNonparagraphContinuations(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		for _, payload := range []string{"heading", "é"} {
			for _, prefix := range []string{"- ", "1. ", "Term" + ending + ": ", "Term" + ending + "~ "} {
				input := prefix + "# " + payload + ending + "ordinary" + ending
				r := pandocBlocks(t, input)
				root := r.Document.Root()
				if len(root.Children()) != 1 {
					t.Fatalf("detached root %q", input)
				}
				container := root.Children()[0]
				owner := container.Children()[0]
				ownerStart := 0
				if strings.HasPrefix(prefix, "Term") {
					owner = owner.Children()[1]
					ownerStart = len("Term" + ending)
				}
				firstStart := len(prefix)
				secondStart := len(prefix + "# " + payload + ending)
				children := owner.Children()
				if owner.Span().Start() != ownerStart || owner.Span().End() != len(input) || container.Span().End() != len(input) || len(children) != 2 || children[0].Kind() != Heading || children[1].Kind() != Paragraph {
					t.Fatalf("wrong direct owner %q %+v", input, owner.spec)
				}
				if children[0].Span().Start() != firstStart || children[0].Span().End() != secondStart || children[1].Span().Start() != secondStart || children[1].Span().End() != len(input) {
					t.Fatal("direct child spans")
				}
				for _, m := range owner.Markers() {
					if m.Role == "item-indent" {
						t.Fatal("invented indentation for unprefixed continuation")
					}
				}
				part := children[1].Content()
				if len(part) != 1 || part[0].Source.Start() != secondStart || part[0].Source.End() != len(input)-len(ending) {
					t.Fatal("ordinary original payload")
				}
			}
			input := "- ***" + ending + "ordinary" + ending
			r := pandocBlocks(t, input)
			roots := r.Document.Root().Children()
			if len(roots) != 1 || len(roots[0].Children()) != 1 {
				t.Fatal("thematic owner")
			}
			owner := roots[0].Children()[0]
			if owner.Span().End() != len(input) || len(owner.Children()) != 2 || owner.Children()[0].Kind() != ThematicBreak || owner.Children()[1].Kind() != Paragraph {
				t.Fatal("thematic direct children")
			}
			for _, indent := range []string{" ", "   ", "\t"} {
				input := "- | " + payload + ending + indent + "wrapped" + ending
				r := pandocBlocks(t, input)
				roots := r.Document.Root().Children()
				if len(roots) != 1 {
					t.Fatal("wrap outside item", input)
				}
				item := roots[0].Children()[0]
				if len(item.Children()) != 1 || item.Children()[0].Kind() != LineBlock {
					t.Fatal("wrong line owner", input)
				}
				block := item.Children()[0]
				if len(block.Children()) != 1 {
					t.Fatal("split logical line")
				}
				line := block.Children()[0]
				if roots[0].Span().End() != len(input) || item.Span().End() != len(input) || block.Span().Start() != 2 || block.Span().End() != len(input) || line.Span().Start() != 2 || line.Span().End() != len(input) {
					t.Fatal("full logical extents")
				}
				parts := line.Content()
				start := len("- | " + payload + ending)
				if len(parts) < 3 || parts[0].Source.Start() != 4 || parts[0].Source.End() != 4+len(payload) || parts[1].Source.Start() != 4+len(payload) || parts[1].Source.End() != start || parts[1].Semantic != Some(" ") {
					t.Fatal("physical ending mapping", parts)
				}
				indentMarkers := 0
				for _, m := range item.Markers() {
					if m.Role == "item-indent" {
						indentMarkers++
						if indent == " " {
							t.Fatal("partial failed indent was consumed")
						}
						if m.Source.Start() != start || m.Source.End() != start+len(indent)-1 && indent != "\t" {
							t.Fatal("actual item prefix", m)
						}
					}
				}
				if indent == " " && indentMarkers != 0 || indent != " " && indentMarkers != 1 {
					t.Fatal("prefix count")
				}
				continuation := line.Markers()[1].Source
				if indent == "\t" {
					if continuation.Start() != start || continuation.End() != start+1 || parts[2].Transformation != PartialTab || parts[2].Source != continuation {
						t.Fatal("shared expanded tab provenance", line.spec)
					}
				} else {
					expected := start
					if indent == "   " {
						expected += 2
					}
					if continuation.Start() != expected || continuation.End() != expected+1 || parts[2].Source.Start() != expected+1 || parts[2].Source.End() != len(input)-len(ending) {
						t.Fatal("original wrap source", line.spec)
					}
				}
			}
		}
	}
}

func TestPandocItemExtractionGroupBoundaries(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		for _, input := range []string{"- # first\n\n  # second\nordinary\n", "Term\n: # first\n\n  # second\nordinary\n", "- # first\n  ```\nbody\n```\n", "- # first\n\n  ```\nbody\n```\n", "- # first\n\n  prose\n```\nbody\n```\n", "- \n\n  text\nordinary\n"} {
			input = strings.ReplaceAll(input, "\n", ending)
			r := pandocBlocks(t, input)
			roots := r.Document.Root().Children()
			if len(roots) != 1 {
				t.Fatalf("continuation group detached %q", input)
			}
			owner := roots[0].Children()[0]
			if strings.HasPrefix(input, "Term") {
				owner = owner.Children()[1]
			}
			if owner.Span().End() != len(input) {
				t.Fatal("continuation owner end", input)
			}
			if owner.Children()[len(owner.Children())-1].Span().End() != len(input) {
				t.Fatal("last direct child not owned")
			}
		}
		for _, input := range []string{"- # first\n\nordinary\n", "- # first\n\n ordinary\n", "- # first\n\n  # second\n\nordinary\n", "- prose\n ```\nbody\n ```\n", "- # first\n```\nbody\n```\n", "- | text\n\n wrapped\n"} {
			input = strings.ReplaceAll(input, "\n", ending)
			r := pandocBlocks(t, input)
			roots := r.Document.Root().Children()
			if len(roots) != 2 || roots[0].Kind() != List || roots[0].Span().End() != roots[1].Span().Start() {
				t.Fatalf("boundary swallowed %q %+v", input, r.Document.Root().spec)
			}
		}
		for _, marker := range []string{"1.", "a.", "iv.", "#.", "(@new)", ":", "~"} {
			input := "- # first" + ending + marker + " next" + ending
			r := pandocBlocks(t, input)
			roots := r.Document.Root().Children()
			if len(roots) != 2 || roots[0].Children()[0].Span().End() != len("- # first"+ending) {
				t.Fatal("new marker family swallowed", input)
			}
		}
		input := "- # first" + ending + "- next" + ending
		r := pandocBlocks(t, input)
		roots := r.Document.Root().Children()
		if len(roots) != 1 || len(roots[0].Children()) != 2 || roots[0].Children()[0].Span().End() != len("- # first"+ending) {
			t.Fatal("sibling swallowed")
		}
		input = "- | text" + ending + "  wrapped" + ending
		r = pandocBlocks(t, input)
		item := r.Document.Root().Children()[0].Children()[0]
		if len(item.Children()) != 2 || item.Children()[0].Kind() != LineBlock || item.Children()[1].Kind() != Paragraph || item.Children()[1].Span().Start() != len("- | text"+ending)+2 {
			t.Fatal("removed indentation invented a logical wrap")
		}
	}
}

func TestPandocItemExtractionEnclosingContextAndState(t *testing.T) {
	for _, input := range []string{"> - # é\n> ordinary\n", "- - # é\n  ordinary\n", "Term\n: - # é\n  ordinary\n"} {
		r := pandocBlocks(t, input)
		root := r.Document.Root()
		if len(root.Children()) != 1 || root.Children()[0].Span().End() != len(input) {
			t.Fatal("enclosing owner escaped")
		}
		var headingParent *Node
		Walk(root, func(n *Node) bool {
			if n.Kind() == ListItem && len(n.Children()) == 2 && n.Children()[0].Kind() == Heading {
				headingParent = n
			}
			return true
		})
		if headingParent == nil || headingParent.Span().End() != len(input) || headingParent.Children()[1].Kind() != Paragraph || headingParent.Children()[1].Span().End() != len(input) {
			t.Fatal("nested direct ownership", input)
		}
	}
	r := pandocBlocks(t, "- - # heading\n  + next\n")
	outer := r.Document.Root().Children()[0].Children()[0]
	if len(outer.Children()) != 1 || outer.Children()[0].Kind() != List || len(outer.Children()[0].Children()) != 2 {
		t.Fatal("nested sibling boundary")
	}
	input := "(@first) # heading\n(@second) next\n\n(@third) third\n"
	r = pandocBlocks(t, input)
	var starts []int
	Walk(r.Document.Root(), func(n *Node) bool {
		if n.Kind() == ListItem {
			starts = append(starts, n.Properties().PandocList.Start)
		}
		return true
	})
	if !reflect.DeepEqual(starts, []int{1, 2, 3}) {
		t.Fatal("extraction/lookahead mutated numbering", starts)
	}
	for _, profile := range []parserProfile{coreProfile, gfmInProgressProfile} {
		for _, input := range []string{"- # heading\nordinary\n", "Term\n: # heading\nordinary\n", "- | text\n wrapped\n"} {
			budget, _ := NewOperationBudget(DefaultLimits())
			r := parseWithProfile(context.Background(), NewSource([]byte(input)), budget, profile)
			if strings.HasPrefix(input, "- #") && len(r.Document.Root().Children()) != 2 {
				t.Fatal("core/GFM ownership changed")
			}
			if r.Document.parsedComplete == false {
				t.Fatal("core/GFM completion changed")
			}
		}
	}
	input = "- # closed\n\n  | é\n wrapped\n\n  Term\n  : # nested\nordinary\n"
	cancelled := 0
	for point := 1; point <= 40; point++ {
		budget, _ := NewOperationBudget(DefaultLimits())
		ctx := &checkpointContext{Context: context.Background(), remaining: point}
		r := parseWithProfile(ctx, NewSource([]byte(input)), budget, pandocBlocksProfile)
		if r.Interruption == Cancelled {
			cancelled++
		}
		if r.Document.parsedComplete || r.Phases.Inlines == PhaseComplete || string(r.Source.Bytes()) != input {
			t.Fatal("interrupted extraction authority/source")
		}
	}
	if cancelled < 10 {
		t.Fatal("cancellation coverage")
	}
}

func TestPandocItemExtractionEOFAndTildeBoundary(t *testing.T) {
	for _, input := range []string{"- # heading\nordinary", "Term\n: # é\nordinary", "- | é\n wrapped", "- # first\n  ~~~\nbody\n~~~\n"} {
		r := pandocBlocks(t, input)
		roots := r.Document.Root().Children()
		if len(roots) != 1 || roots[0].Span().End() != len(input) {
			t.Fatal("EOF/tilde extraction end", input)
		}
		owner := roots[0].Children()[0]
		if strings.HasPrefix(input, "Term") {
			owner = owner.Children()[1]
		}
		if owner.Span().End() != len(input) || owner.Children()[len(owner.Children())-1].Span().End() != len(input) {
			t.Fatal("EOF owner end")
		}
	}
	input := "- prose\n~~~\nbody\n~~~\n"
	r := pandocBlocks(t, input)
	roots := r.Document.Root().Children()
	if len(roots) != 2 || roots[0].Children()[0].Span().End() != 8 || roots[1].Kind() != CodeBlock {
		t.Fatal("raw tilde fence boundary")
	}
}

func TestPandocDefinitionContinuationSourceFormula(t *testing.T) {
	cases := []struct {
		leading, separator, payload                        string
		markerColumn, contentColumn, indent, sourceContent int
		partial                                            bool
	}{
		{"", " ", "body", 0, 2, 2, 2, false}, {"", "\t", "body", 0, 4, 4, 4, false},
		{"", "   ", "[X] body", 0, 4, 4, 4, false},
		{"", " \t", "é", 0, 4, 4, 4, false}, {" ", "  ", "body", 1, 4, 4, 4, false},
		{"   ", " ", "body", 3, 5, 5, 5, false}, {"", "", "", 0, 1, 2, 1, false},
		{"", "\t", "", 0, 4, 4, 4, false}, {"", " ", "[x]", 0, 2, 2, 2, false},
		{"", "\t\t", "body", 0, 2, 2, 1, true}, {" ", "\t\t", "body", 1, 3, 3, 2, true},
	}
	for _, ending := range []string{"\n", "\r\n"} {
		for _, marker := range []string{":", "~"} {
			for _, tt := range cases {
				for _, blank := range []bool{false, true} {
					input := "Term" + ending
					if blank {
						input += ending
					}
					bodyStart := len(input)
					input += tt.leading + marker + tt.separator + tt.payload + ending
					t.Run(input, func(t *testing.T) {
						r := pandocBlocks(t, input)
						roots := r.Document.Root().Children()
						if len(roots) != 1 || roots[0].Kind() != DefinitionList {
							t.Fatal("body structure", pandocKinds(r.Document.Root()))
						}
						body := roots[0].Children()[0].Children()[1]
						x := body.Properties().PandocList
						if body.Kind() != DefinitionBody || body.Properties().List != nil || x.Style != "definition" || x.ContinuationIndent != tt.indent || x.MarkerColumn != tt.markerColumn || x.ContentColumn != tt.contentColumn || x.SourceMarkerColumn != len(tt.leading) || x.SourceContentColumn != tt.sourceContent || x.SeparatorPartialTab != tt.partial {
							t.Fatalf("definition source facts %+v", x)
						}
						markerStart := bodyStart + len(tt.leading)
						if x.Marker.Start() != markerStart || x.Marker.End() != markerStart+1 {
							t.Fatal("marker range")
						}
						if raw, _ := r.Source.Text(x.Marker); raw != marker {
							t.Fatal("marker source")
						}
						if tt.separator == "" {
							if x.Separator.Present() {
								t.Fatal("invented terminal separator")
							}
						} else {
							end := markerStart + 1 + len(tt.separator)
							if tt.partial {
								end = markerStart + 2
							}
							if x.Separator.Start() != markerStart+1 || x.Separator.End() != end {
								t.Fatal("separator range", x.Separator)
							}
						}
						spec := body.spec
						spec.Properties = cloneProperties(spec.Properties)
						valid, err := NewNode(spec)
						if err != nil {
							t.Fatal("actual body rejected", err)
						}
						before := valid.Properties()
						spec.Properties.PandocList.ContinuationIndent = 99
						if !reflect.DeepEqual(before, valid.Properties()) {
							t.Fatal("constructor input alias")
						}
						returned := valid.Properties()
						returned.PandocList.ContentColumn++
						if !reflect.DeepEqual(before, valid.Properties()) {
							t.Fatal("accessor alias")
						}
						for _, bad := range []int{0, 1, tt.indent - 1, tt.indent + 1, 99} {
							spec := body.spec
							spec.Properties = cloneProperties(spec.Properties)
							spec.Properties.PandocList.ContinuationIndent = bad
							if _, err := NewNode(spec); !errors.Is(err, ErrModel) {
								t.Fatal("false continuation accepted", bad, err)
							}
						}
						for _, field := range []string{"marker", "content", "separator", "leading", "container", "physical-marker", "physical-content"} {
							spec := body.spec
							spec.Properties = cloneProperties(spec.Properties)
							p := spec.Properties.PandocList
							switch field {
							case "marker":
								p.MarkerColumn++
							case "content":
								p.ContentColumn++
							case "separator":
								p.SeparatorColumns++
							case "leading":
								p.LeadingColumns++
							case "container":
								p.ContainerColumn++
							case "physical-marker":
								p.SourceMarkerColumn++
							case "physical-content":
								p.SourceContentColumn++
							}
							if _, err := NewNode(spec); !errors.Is(err, ErrModel) {
								t.Fatal("false body column accepted", field, err)
							}
						}
					})
				}
			}
		}
	}
}

func TestPandocContinuationFormulaNestedAndExceptions(t *testing.T) {
	for _, input := range []string{"> Term\r\n> :\tbody\r\n", "- Term\n  ~\tbody\n", "- > Term\n  > :\tbody\n", "- > 1. Term\n\t>\n\t>\t: body\n", "Term\n: [X] body\n", "Term\n~ [ ]\n", "Term\n:\t[ ] é\n", "Term\n:"} {
		r := pandocBlocks(t, input)
		found := 0
		Walk(r.Document.Root(), func(body *Node) bool {
			if body.Kind() != DefinitionBody {
				return true
			}
			found++
			x := body.Properties().PandocList
			want := x.LeadingColumns + 1 + max(1, x.SeparatorColumns)
			if x.ContinuationIndent != want || body.Properties().List != nil {
				t.Fatal("nested body formula")
			}
			if _, err := NewNode(body.spec); err != nil {
				t.Fatal(err)
			}
			for _, bad := range []int{1, 99} {
				s := body.spec
				s.Properties = cloneProperties(s.Properties)
				s.Properties.PandocList.ContinuationIndent = bad
				if _, err := NewNode(s); !errors.Is(err, ErrModel) {
					t.Fatal("nested false indentation accepted", input)
				}
			}
			return true
		})
		if found == 0 {
			t.Fatal("missing body", input)
		}
	}
	for _, input := range []string{"- body\n", "(12) body\n"} {
		r := pandocBlocks(t, input)
		item := r.Document.Root().Children()[0].Children()[0]
		s := item.spec
		s.Properties = cloneProperties(s.Properties)
		s.Properties.PandocList.ContinuationIndent = 99
		s.Properties.List.ContentIndent = Some(99)
		if _, err := NewNode(s); !errors.Is(err, ErrModel) {
			t.Fatal("ordinary item formula weakened")
		}
	}
	r := pandocBlocks(t, "(@longlabel) body\n")
	list := r.Document.Root().Children()[0]
	item := list.Children()[0]
	if item.Properties().PandocList.ContinuationIndent != 4 {
		t.Fatal("example exception")
	}
	if _, err := NewNode(item.spec); err != nil {
		t.Fatal(err)
	}
	s := item.spec
	s.Properties = cloneProperties(s.Properties)
	s.Properties.PandocList.ContinuationIndent = 13
	s.Properties.List.ContentIndent = Some(13)
	if _, err := NewNode(s); !errors.Is(err, ErrModel) {
		t.Fatal("example ordinary formula accepted")
	}
	group := list.spec
	group.Properties = cloneProperties(group.Properties)
	group.Properties.PandocList.ContinuationIndent = 4
	if _, err := NewNode(group); !errors.Is(err, ErrModel) {
		t.Fatal("grouped list item facts accepted")
	}
}

func TestPandocActualOpenedPeriodFallback(t *testing.T) {
	for _, marker := range []string{"(12.", "(b.", "(B.", "(iv.", "(IV.", "(#.", "(@bird.", "(12@12.", "(p.", "(@é."} {
		for _, ending := range []string{"\n", "\r\n", ""} {
			for _, prefix := range []string{"", "> ", "- ", "> - ", "-\t", "> -\t"} {
				input := prefix + marker + "  é" + ending
				t.Run(input, func(t *testing.T) {
					r := pandocBlocks(t, input)
					roots := r.Document.Root().Children()
					if len(roots) != 1 || roots[0].Span().End() != len(input) {
						t.Fatal("fallback root extent")
					}
					n := roots[0]
					if strings.HasPrefix(prefix, ">") {
						if n.Kind() != BlockQuote || len(n.Children()) != 1 {
							t.Fatal("quote owner")
						}
						n = n.Children()[0]
					}
					if strings.Contains(prefix, "-") {
						if n.Kind() != List || len(n.Children()) != 1 {
							t.Fatal("outer bullet owner")
						}
						item := n.Children()[0]
						if item.Properties().PandocList.Style != "bullet" || len(item.Children()) != 1 {
							t.Fatal("false nested marker")
						}
						n = item.Children()[0]
					}
					if n.Kind() != Paragraph || n.Span().Start() != len(prefix) || n.Span().End() != len(input) || len(n.Markers()) != 0 || n.Properties().PandocList != nil || n.Properties().PandocTask != nil {
						t.Fatalf("actual malformed marker published facts %+v", n.spec)
					}
					parts := n.Content()
					if len(parts) != 1 || parts[0].Source.Start() != len(prefix) || parts[0].Source.End() != len(input)-len(ending) || parts[0].Semantic != Some(marker+"  é") {
						t.Fatal("complete original fallback payload", parts)
					}
					if string(r.Source.Bytes()) != input {
						t.Fatal("source changed")
					}
					Walk(r.Document.Root(), func(n *Node) bool {
						if p := n.Properties().PandocList; p != nil && (p.Style != "bullet" || p.Decimal.Present() || p.Reset.Present || p.Label.Present()) {
							t.Fatal("false recognized metadata", p)
						}
						return true
					})
				})
			}
		}
	}
}

func TestPandocBroadMarkerStopsButActualFallsBack(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		for _, marker := range []string{"(2.", "(b.", "(IV.", "(#.", "(@bird.", "(12@12.", "(p."} {
			for _, opener := range []string{"- # heading", "1. # heading", "Term" + ending + ": # heading"} {
				first := opener + ending
				input := first + marker + " prose" + ending
				r := pandocBlocks(t, input)
				roots := r.Document.Root().Children()
				if len(roots) != 2 || roots[0].Span().End() != len(first) || roots[1].Kind() != Paragraph || roots[1].Span().Start() != len(first) || roots[1].Span().End() != len(input) {
					t.Fatal("broad boundary versus actual ownership", input, pandocKinds(r.Document.Root()))
				}
				owner := roots[0].Children()[0]
				if owner.Kind() == DefinitionItem {
					owner = owner.Children()[1]
				}
				if owner.Span().End() != len(first) {
					t.Fatal("first item swallowed broad stopping form")
				}
			}
			input := "> - # heading" + ending + "> " + marker + " prose" + ending
			r := pandocBlocks(t, input)
			quote := r.Document.Root().Children()[0]
			children := quote.Children()
			if len(r.Document.Root().Children()) != 1 || quote.Kind() != BlockQuote || len(children) != 2 || children[0].Kind() != List || children[0].Children()[0].Span().End() != len("> - # heading"+ending) || children[1].Kind() != Paragraph || children[1].Span().Start() != len("> - # heading"+ending+"> ") {
				t.Fatal("quoted boundary", input)
			}
			input = "- > # heading" + ending + "  " + marker + " prose" + ending
			r = pandocBlocks(t, input)
			item := r.Document.Root().Children()[0].Children()[0]
			children = item.Children()
			if len(r.Document.Root().Children()) != 1 || item.Span().End() != len(input) || len(children) != 2 || children[0].Kind() != BlockQuote || children[0].Span().End() != len("- > # heading"+ending) || children[1].Kind() != Paragraph {
				t.Fatal("enclosing item/quote boundary", input)
			}
		}
	}
	// Broad endline guards also interrupt an existing item's paragraph. Actual
	// malformed source then remains ordinary outside that item, rather than sibling.
	for _, first := range []string{"1. prose\n", "a. prose\n", "(@first) prose\n"} {
		input := first + "(2. outside\n\n2. valid\n"
		r := pandocBlocks(t, input)
		roots := r.Document.Root().Children()
		if len(roots) != 3 || roots[0].Kind() != List || roots[0].Children()[0].Span().End() != len(first) || roots[1].Kind() != Paragraph || roots[1].Span().Start() != len(first) || roots[2].Kind() != List {
			t.Fatal("existing style/paragraph boundary", input)
		}
	}
	r := pandocBlocks(t, "prose\n(2. ordinary\n")
	if roots := r.Document.Root().Children(); len(roots) != 1 || roots[0].Kind() != Paragraph || roots[0].Span().End() != len("prose\n(2. ordinary\n") {
		t.Fatal("root paragraph interruption changed")
	}
}

func TestPandocRejectedOpenedExampleHasNoState(t *testing.T) {
	for _, bad := range []string{"(9@bad.", "(@bad.", "(999999999999999999999999999999@bad."} {
		input := "(@first) valid\n\n" + bad + " rejected\n\n(@) second\n\n(@bad) third\n\n(@first) reuse\n\n(@) fourth\n"
		r := pandocBlocks(t, input)
		var starts []int
		var labels []string
		Walk(r.Document.Root(), func(n *Node) bool {
			if n.Kind() == ListItem {
				x := n.Properties().PandocList
				starts = append(starts, x.Start)
				label, _ := r.Source.Text(x.Label)
				labels = append(labels, label)
			}
			return true
		})
		if !reflect.DeepEqual(starts, []int{1, 2, 3, 1, 4}) || !reflect.DeepEqual(labels, []string{"first", "", "bad", "first", ""}) {
			t.Fatal("rejected actual marker changed example counter/labels", bad, starts, labels)
		}
	}
}

func TestPandocOpenedPeriodSuppliedModelRejected(t *testing.T) {
	for _, tt := range []struct {
		raw, style string
		start      int
	}{
		{"(12.", "decimal", 12}, {"(b.", "lower-alpha", 2}, {"(B.", "upper-alpha", 2}, {"(iv.", "lower-roman", 4}, {"(IV.", "upper-roman", 4}, {"(#.", "default", 1},
		{"(@bird.", "example", 1}, {"(12@12.", "example", 12}, {"(p.", "lower-alpha", 16}, {"(@é.", "example", 1},
	} {
		source := NewSource([]byte(tt.raw + "  é\r\n"))
		sp := func(a, b int) Span {
			s, e := source.Span(a, b)
			if e != nil {
				t.Fatal(e)
			}
			return s
		}
		width := len([]rune(tt.raw))
		indent := width + 2
		if tt.style == "example" {
			indent = 4
		}
		delim := "period"
		if tt.style == "default" {
			delim = "default"
		}
		marker := sp(0, len(tt.raw))
		p := &PandocListProperties{Style: tt.style, MarkerStyle: tt.style, Delimiter: delim, Start: tt.start, Marker: marker, Separator: sp(len(tt.raw), len(tt.raw)+2), ContentColumn: width + 2, SourceContentColumn: width + 2, SeparatorColumns: 2, ContinuationIndent: indent}
		if tt.style == "decimal" {
			p.Decimal = sp(1, len(tt.raw)-1)
		}
		if at := strings.IndexByte(tt.raw, '@'); at >= 0 {
			p.Label = sp(at+1, len(tt.raw)-1)
			if at > 1 {
				p.Reset = Some(12)
			}
		}
		spec := NodeSpec{Kind: ListItem, Envelope: sp(0, source.Len()), Markers: []Marker{{Role: "item-marker", Source: marker}}, Properties: Properties{PandocList: p, List: &ListProperties{Ordered: true, Start: Some(tt.start), Delimiter: Some(byte('.')), MarkerWidth: Some(width), Padding: Some(2), ContentIndent: Some(indent), Prefix: &Value{Segments: []Segment{{Source: sp(0, len(tt.raw)+2), Transformation: Exact, Semantic: Some(tt.raw + "  ")}}, Semantic: Some(tt.raw + "  ")}}}}
		if _, err := NewNode(spec); !errors.Is(err, ErrModel) {
			t.Fatal("unsupported actual ordered spelling accepted", tt.raw, err)
		}
	}
}

func TestPandocActualMarkerBacktracksToHigherPriorityBlocks(t *testing.T) {
	r := pandocBlocks(t, "(2. title\n===\n")
	roots := r.Document.Root().Children()
	if len(roots) != 1 || roots[0].Kind() != Heading || roots[0].Content()[0].Semantic != Some("(2. title") || roots[0].Properties().PandocList != nil {
		t.Fatal("Setext precedence over actual fallback")
	}
	r = pandocBlocks(t, "(2. term\n: body\n")
	roots = r.Document.Root().Children()
	if len(roots) != 1 || roots[0].Kind() != DefinitionList || roots[0].Children()[0].Children()[0].Content()[0].Semantic != Some("(2. term") {
		t.Fatal("definition fallback term")
	}
	for _, input := range []string{"12. one\n#. two\n13. three\n", "a) one\n#) two\nb) three\n", "(@bird) one\n(#) two\n(@bird) reuse\n"} {
		r := pandocBlocks(t, input)
		roots := r.Document.Root().Children()
		if len(roots) != 1 || roots[0].Kind() != List || len(roots[0].Children()) != 3 {
			t.Fatal("selected-style/default siblings changed", input)
		}
		for _, item := range roots[0].Children() {
			if _, err := NewNode(item.spec); err != nil {
				t.Fatal(err)
			}
		}
	}
}
