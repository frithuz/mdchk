package parser

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func attributeFixture(t *testing.T, raw string) (*Attributes, LineIndex) {
	t.Helper()
	index := newLineIndex(context.Background(), NewSource([]byte(raw)))
	a, n := scanAttributes(context.Background(), index.semanticText(0, len(raw)).Segments, 0)
	if a == nil || n != len(raw) {
		t.Fatalf("attribute scan %q: %+v end%d", raw, a, n)
	}
	return a, index
}
func assertByteSpan(t *testing.T, s Span, start, end int, raw string) {
	t.Helper()
	if !s.Present() || s.Start() != start || s.End() != end {
		t.Fatalf("span [%d,%d) want [%d,%d)", s.Start(), s.End(), start, end)
	}
	got, err := s.source.Text(s)
	if err != nil || got != raw {
		t.Fatalf("source %q (%v) want %q", got, err, raw)
	}
}
func assertValueRange(t *testing.T, v Value, start, end int, raw, semantic string) {
	t.Helper()
	if len(v.Segments) != 1 {
		t.Fatalf("value segments %+v", v)
	}
	assertByteSpan(t, v.Segments[0].Source, start, end, raw)
	if v.Semantic != Some(semantic) {
		t.Fatalf("semantic %+v want %q", v.Semantic, semantic)
	}
}
func TestPandocAttributesOrderedProjection(t *testing.T) {
	raw := `{#a id="b&amp;c" class="x y" .x k="&amp;" k=&amp; #d}`
	a, _ := attributeFixture(t, raw)
	if a.ID != Some("d") || !reflect.DeepEqual(a.Classes, []string{"x", "y", "x"}) || !reflect.DeepEqual(a.KeyValues, []AttributeKeyValue{{"k", "&"}, {"k", "&amp;"}}) {
		t.Fatalf("projection %+v", a)
	}
	ranges := [][2]int{{1, 3}, {4, 16}, {17, 28}, {29, 31}, {32, 41}, {42, 49}, {50, 52}}
	// The independent ranges below are literal coordinates, not searches through
	// output or lengths inferred from decoded values.
	if len(a.Occurrences) != 7 {
		t.Fatal(a.Occurrences)
	}
	for i, r := range ranges {
		o := a.Occurrences[i]
		if len(o.Raw.Segments) != 1 || o.Raw.Segments[0].Source.Start() != r[0] || o.Raw.Segments[0].Source.End() != r[1] {
			t.Fatalf("occurrence %d %+v expected %v", i, o, r)
		}
	}
}
func TestPandocAttributesGrammar(t *testing.T) {
	tests := []struct {
		raw     string
		id      Optional[string]
		classes []string
		kvs     []AttributeKeyValue
	}{
		{`{}`, Optional[string]{}, nil, nil},
		{`{#a id=b #c .x class="y z" k=1 k=2 -}`, Some("c"), []string{"x", "y", "z", "unnumbered"}, []AttributeKeyValue{{"k", "1"}, {"k", "2"}}},
		{`{id=}`, Some(""), nil, nil}, {`{id=""}`, Some(""), nil, nil},
		{`{#9 .É k="a\}b" z=a\}b}`, Some("9"), []string{"É"}, []AttributeKeyValue{{"k", "a}b"}, {"z", "a}b"}}},
		{`{k=" leading"}`, Optional[string]{}, nil, nil}, // checked separately: unquoted fallback stops at space => invalid
	}
	for _, tt := range tests[:len(tests)-1] {
		t.Run(tt.raw, func(t *testing.T) {
			a, _ := attributeFixture(t, tt.raw)
			if a.ID != tt.id || !reflect.DeepEqual(a.Classes, tt.classes) || !reflect.DeepEqual(a.KeyValues, tt.kvs) {
				t.Fatalf("%+v", a)
			}
		})
	}
	for _, raw := range []string{`{python}`, `{.9}`, `{#}`, `{k=" leading"}`, "{#a\n\n#b}", `{#a`, "{#é}"} {
		index := newLineIndex(context.Background(), NewSource([]byte(raw)))
		a, _ := scanAttributes(context.Background(), index.semanticText(0, len(raw)).Segments, 0)
		if a != nil {
			t.Fatalf("accepted malformed %q %+v", raw, a)
		}
	}
	a, _ := attributeFixture(t, "{#é\n .x k='a\nb' k=\"&notit;\"}")
	if a.ID != Some("é") || !reflect.DeepEqual(a.KeyValues, []AttributeKeyValue{{"k", "a b"}, {"k", "&notit;"}}) {
		t.Fatal(a)
	}
}
func TestPandocAttributesImmutableAndValidation(t *testing.T) {
	a, index := attributeFixture(t, `{#a #b .x k=1 k=2}`)
	spec := NodeSpec{Kind: Heading, Envelope: index.span(0, index.source.Len()), Properties: Properties{Heading: &HeadingProperties{Level: 1, Style: "atx"}, Attributes: a}}
	n, err := NewNode(spec)
	if err != nil {
		t.Fatal(err)
	}
	a.Occurrences[0].Raw.Segments[0].Source = Span{}
	a.Classes[0] = "changed"
	a.KeyValues[0].Value = "changed"
	a.ID = Some("changed")
	p := n.Properties()
	p.Attributes.Occurrences[0].Value.Segments[0].Source = Span{}
	p.Attributes.Classes[0] = "changed"
	if n.Properties().Attributes.ID != Some("b") || n.Properties().Attributes.Classes[0] != "x" {
		t.Fatal("aliased attributes")
	}
	for _, mutate := range []func(*NodeSpec){
		func(s *NodeSpec) { s.Properties.Attributes.ID = Some("a") },
		func(s *NodeSpec) { s.Properties.Attributes.Occurrences[0].Raw.Segments[0].Source = Span{} },
		func(s *NodeSpec) {
			foreign := NewSource([]byte(`{#a #b .x k=1 k=2}`))
			s.Properties.Attributes.Raw.Segments[0].Source.source = foreign
		},
		func(s *NodeSpec) { s.Properties.Attributes.Raw.Segments[0].Source.end-- },
		func(s *NodeSpec) { s.Kind = Paragraph },
	} {
		s := n.spec
		s.Properties = n.Properties()
		mutate(&s)
		if _, err := NewNode(s); err == nil {
			t.Fatal("invalid supplied metadata accepted")
		}
	}
}
func TestPandocFenceSourceAndLanguage(t *testing.T) {
	input := "``` {.python #one #two}\nx\n```\n"
	n := pandocBlocks(t, input).Document.Root().Children()[0]
	assertByteSpan(t, n.Span(), 0, 30, input)
	assertByteSpan(t, n.Markers()[0].Source, 0, 3, "```")
	assertByteSpan(t, n.Markers()[1].Source, 26, 29, "```")
	a := n.Properties().Attributes
	assertValueRange(t, n.Properties().PandocFence.Info, 3, 23, " {.python #one #two}", " {.python #one #two}")
	assertValueRange(t, a.Raw, 4, 23, "{.python #one #two}", "{.python #one #two}")
	assertValueRange(t, a.Occurrences[0].Raw, 5, 12, ".python", ".python")
	assertValueRange(t, a.Occurrences[1].Raw, 13, 17, "#one", "#one")
	assertValueRange(t, a.Occurrences[2].Raw, 18, 22, "#two", "#two")
	if a.ID != Some("two") || n.Properties().PandocFence.Language != Some("python") || n.Properties().Literal != Some("x") {
		t.Fatal(n.Properties())
	}
	assertByteSpan(t, n.Content()[0].Source, 24, 25, "x")
	assertByteSpan(t, n.Content()[1].Source, 25, 26, "\n")
	for _, tt := range []struct{ info, lang string }{{"C++", "c++"}, {"c++", "cpp"}, {"objective-c", "objectivec"}, {"Python", "python"}, {"{.Python}", "Python"}, {"{#id}", ""}, {"{k=v}", ""}, {"go {.other}", "go"}, {"{}", ""}} {
		n := pandocBlocks(t, "```"+tt.info+"\n``` ").Document.Root().Children()[0]
		p := n.Properties()
		if n.Kind() != CodeBlock || p.PandocFence.Language.Present != (tt.lang != "") || p.PandocFence.Language.Value != tt.lang || p.Literal != Some("") {
			t.Fatalf("%q %+v", tt.info, p)
		}
	}
}
func TestPandocRawFenceSource(t *testing.T) {
	input := "```{=latex}\n\\alpha [^hidden]\n```\n"
	n := pandocBlocks(t, input).Document.Root().Children()[0]
	assertByteSpan(t, n.Span(), 0, 33, input)
	assertByteSpan(t, n.Markers()[1].Source, 29, 32, "```")
	if n.Kind() != RawBlock || n.Properties().PandocFence.Language.Present || n.Properties().Literal != Some("\\alpha [^hidden]") {
		t.Fatal(n.spec)
	}
	assertValueRange(t, *n.Properties().PandocFence.RawFormat, 5, 10, "latex", "latex")
	assertByteSpan(t, n.Content()[0].Source, 12, 28, "\\alpha [^hidden]")
}
func TestPandocFenceCompleteGrammarAndFallback(t *testing.T) {
	for _, info := range []string{"{python}", "go extra", "{#a", "{=}", "{=html} {.x}"} {
		input := "```" + info + "\n# heading\n\n- item\n\n```go\nx\n```\n"
		r := pandocBlocks(t, input)
		children := r.Document.Root().Children()
		if children[0].Kind() != Paragraph || children[len(children)-1].Kind() != CodeBlock || children[len(children)-1].Properties().PandocFence.Language != Some("go") {
			t.Fatalf("fallback %q %v", info, pandocKinds(r.Document.Root()))
		}
	}
	for _, input := range []string{"```go\nx\n", "> prose\n```{python}\nnext\n", "- prose\n```go extra\nnext\n"} {
		r := pandocBlocks(t, input)
		Walk(r.Document.Root(), func(n *Node) bool {
			if n.Kind() == CodeBlock || n.Kind() == RawBlock {
				t.Fatalf("unrecognized fence %q", input)
			}
			return true
		})
	}
	for _, input := range []string{"```\n```", "```\nx\n```", "````\na\n```\nb\n`````\n"} {
		r := pandocBlocks(t, input)
		n := r.Document.Root().Children()[0]
		if n.Kind() != CodeBlock {
			t.Fatal(input)
		}
		if len(n.Children()) != 0 {
			t.Fatal("literal payload acquired children")
		}
	}
	for _, input := range []string{"prose\n~~~\nx\n~~~\n", "prose\n ```\nx\n ```\n"} {
		r := pandocBlocks(t, input)
		if got := pandocKinds(r.Document.Root()); !reflect.DeepEqual(got, []Kind{DocumentKind, Paragraph}) {
			t.Fatal(got)
		}
	}
}
func TestPandocHeadingAttributesSource(t *testing.T) {
	input := "# H {#first #last}\n"
	n := pandocBlocks(t, input).Document.Root().Children()[0]
	assertByteSpan(t, n.Span(), 0, 19, input)
	assertByteSpan(t, n.Content()[0].Source, 2, 3, "H")
	a := n.Properties().Attributes
	assertValueRange(t, a.Raw, 4, 18, "{#first #last}", "{#first #last}")
	assertValueRange(t, a.Occurrences[0].Raw, 5, 11, "#first", "#first")
	assertValueRange(t, a.Occurrences[1].Raw, 12, 17, "#last", "#last")
	if a.ID != Some("last") {
		t.Fatal(a)
	}
	for _, tt := range []struct {
		input, text string
		id          Optional[string]
	}{{"# t ## {#a}\n", "t", Some("a")}, {"t {#a}\n---\n", "t", Some("a")}, {"# t \\{#a}\n", "t \\{#a}", Optional[string]{}}, {"# `{#a}`\n", "`{#a}`", Optional[string]{}}} {
		n := pandocBlocks(t, tt.input).Document.Root().Children()[0]
		a := n.Properties().Attributes
		if tt.id.Present && (a == nil || a.ID != tt.id) || !tt.id.Present && a != nil {
			t.Fatal(n.spec)
		}
		var text strings.Builder
		for _, p := range n.Content() {
			text.WriteString(p.Semantic.Value)
		}
		if text.String() != tt.text {
			t.Fatal(text.String())
		}
	}
}
func TestPandocNativeDivNestingSource(t *testing.T) {
	input := ":::: {#o}\n::: inner\ntext\n:::\n::::\n"
	root := pandocBlocks(t, input).Document.Root()
	outer := root.Children()[0]
	inner := outer.Children()[0]
	if outer.Kind() != Div || inner.Kind() != Div || len(root.Children()) != 1 || len(outer.Children()) != 1 || len(inner.Children()) != 1 || inner.Children()[0].Kind() != Paragraph {
		t.Fatal(pandocKinds(root))
	}
	assertByteSpan(t, outer.Span(), 0, 34, input)
	assertByteSpan(t, inner.Span(), 10, 29, "::: inner\ntext\n:::\n")
	assertByteSpan(t, outer.Markers()[0].Source, 0, 4, "::::")
	assertByteSpan(t, outer.Markers()[1].Source, 29, 33, "::::")
	assertByteSpan(t, inner.Markers()[0].Source, 10, 13, ":::")
	assertByteSpan(t, inner.Markers()[1].Source, 25, 28, ":::")
	assertValueRange(t, outer.Properties().Attributes.Raw, 5, 9, "{#o}", "{#o}")
	assertValueRange(t, inner.Properties().Attributes.Raw, 14, 19, "inner", "inner")
	assertByteSpan(t, inner.Children()[0].Content()[0].Source, 20, 24, "text")
	if outer.Properties().Div.Origin != "fenced-native" || outer.Properties().Attributes.ID != Some("o") || !reflect.DeepEqual(inner.Properties().Attributes.Classes, []string{"inner"}) {
		t.Fatal(outer.spec, inner.spec)
	}
	for _, input := range []string{"::::: {} :::\n::: in\n::: deep\nx\n:::\n:::\n:::\n", "::: {}\n*child*\n", "::: {}\n```{=html}\n::: fake\n```\n::: \n"} {
		r := pandocBlocks(t, input)
		if r.Document.Root().Children()[0].Kind() != Div {
			t.Fatal(input)
		}
	}
	if pandocBlocks(t, ":::\n").Document.Root().Children()[0].Kind() != Paragraph {
		t.Fatal("bare closer became opener")
	}
}

func TestPandocMultilineOriginalContainerSegments(t *testing.T) {
	for _, input := range []string{
		"``` {#é\n .X k='a\nb'}\nx\n```\n",
		"> ``` {#é\r\n> .X k='a\r\n> b'}\r\n> x\r\n> ```\r\n",
		"::: {#é\n .X}\nchild\n:::\n",
		"# H {#é\n .X}\nnext\n",
	} {
		r := pandocBlocks(t, input)
		root := r.Document.Root()
		var owner *Node
		Walk(root, func(n *Node) bool {
			if n.Properties().Attributes != nil {
				if owner != nil {
					t.Fatal("duplicate owner")
				}
				owner = n
			}
			return true
		})
		if owner == nil || owner.Properties().Attributes.ID != Some("é") || !reflect.DeepEqual(owner.Properties().Attributes.Classes, []string{"X"}) {
			t.Fatalf("multiline %q %v", input, pandocKinds(root))
		}
		a := owner.Properties().Attributes
		for _, o := range a.Occurrences {
			for _, s := range o.Raw.Segments {
				raw, _ := r.Source.Text(s.Source)
				if strings.Contains(raw, "> ") {
					t.Fatal("stripped quote prefix in occurrence")
				}
				if !validPandocSpan(owner.Span(), s.Source) {
					t.Fatal("invalid source")
				}
			}
		}
		if strings.HasPrefix(input, "> ") {
			if root.Children()[0].Kind() != BlockQuote || owner.Kind() != CodeBlock || len(a.Raw.Segments) < 3 {
				t.Fatal("segmented quote ownership")
			}
			// Original CRLF and quote-gap coordinates, independently counted.
			assertByteSpan(t, a.Occurrences[0].Raw.Segments[0].Source, 7, 10, "#é")
			assertByteSpan(t, a.Occurrences[1].Raw.Segments[0].Source, 14, 16, ".X")
			if a.KeyValues[0].Value != "a b" {
				t.Fatal(a.KeyValues)
			}
		}
	}
}
func TestPandocDivLiteralAndContainerOwnership(t *testing.T) {
	input := "::: outer\n::: inner\n```{=html}\n::: fake\n# {#fake}\n```\n:::\n:::\nafter\n"
	root := pandocBlocks(t, input).Document.Root()
	if len(root.Children()) != 2 {
		t.Fatal(pandocKinds(root))
	}
	outer := root.Children()[0]
	inner := outer.Children()[0]
	if outer.Kind() != Div || len(outer.Children()) != 1 || inner.Kind() != Div || len(inner.Children()) != 1 || inner.Children()[0].Kind() != RawBlock || inner.Children()[0].Properties().Literal != Some("::: fake\n# {#fake}") || root.Children()[1].Kind() != Paragraph {
		t.Fatal(pandocKinds(root))
	}
	for _, input := range []string{
		"> ::: outer\r\n> ::: inner\r\n> *x*\r\n> :::\r\n> :::\r\n",
		"- ::: outer\n  ::: inner\n  *x*\n  :::\n  :::\n",
		"::: outer\n<pre>\n::: fake\n</pre>\n\n:::\n",
		"::: outer\n<script>\n::: fake\n</script>\n:::\n",
	} {
		r := pandocBlocks(t, input)
		divs := 0
		Walk(r.Document.Root(), func(n *Node) bool {
			if n.Kind() == Div {
				divs++
				if n.Properties().Div.Origin != "fenced-native" {
					t.Fatal("origin")
				}
			}
			return true
		})
		want := 2
		if strings.Contains(input, "<") {
			want = 1
		}
		if divs != want {
			t.Fatalf("div count %q: %v", input, pandocKinds(r.Document.Root()))
		}
	}
}
func TestPandocOwnedMetadataFlavorIsolation(t *testing.T) {
	for _, input := range []string{"```{=html}\nx\n```\n", "``` {.Python #a #b}\nx\n```\n", "::: {#a}\nx\n:::\n", "# H {#a}\n"} {
		for _, profile := range []parserProfile{coreProfile, gfmInProgressProfile} {
			budget, _ := NewOperationBudget(DefaultLimits())
			r := parseWithProfile(context.Background(), NewSource([]byte(input)), budget, profile)
			if r.Failure != nil || r.Interruption != "" {
				t.Fatal(r)
			}
			Walk(r.Document.Root(), func(n *Node) bool {
				p := n.Properties()
				if n.Kind() == RawBlock || n.Kind() == Div || p.Attributes != nil || p.PandocFence != nil || p.Div != nil {
					t.Fatalf("profile leak %v", n.spec)
				}
				return true
			})
		}
	}
}

func TestPandocOwnedMetadataRejectsCoordinatedFalseFacts(t *testing.T) {
	for _, input := range []string{"```go {.x}\nx\n```\n", "```{=html}\nx\n```\n", "::: actual\n:::\n"} {
		n := pandocBlocks(t, input).Document.Root().Children()[0]
		spec := n.spec
		spec.Properties = n.Properties()
		if spec.Properties.Div != nil {
			a := spec.Properties.Attributes
			a.Raw.Semantic = Some("fake")
			a.Classes = []string{"fake"}
			a.Occurrences[0].Raw.Semantic = Some("fake")
			a.Occurrences[0].Value.Semantic = Some("fake")
		} else if spec.Kind == RawBlock {
			spec.Properties.PandocFence.RawFormat.Semantic = Some("latex")
		} else {
			spec.Properties.PandocFence.Language = Some("fake")
		}
		if _, err := NewNode(spec); err == nil {
			t.Fatal("false semantic projection accepted")
		}
	}
	source := NewSource([]byte("{#a DROP #b}"))
	index := LineIndex{source: source, ctx: context.Background()}
	parts := append(index.semanticText(0, 4).Segments, index.semanticText(9, 12).Segments...)
	a, _ := scanAttributes(context.Background(), parts, 0)
	if a == nil {
		t.Fatal("test setup")
	}
	if _, err := NewNode(NodeSpec{Kind: Heading, Envelope: index.span(0, source.Len()), Properties: Properties{Heading: &HeadingProperties{Level: 1, Style: "atx"}, Attributes: a}}); err == nil {
		t.Fatal("ordinary attribute bytes lost through fake gap")
	}
}
func TestPandocOwnedLimitsRollbackAndAuthority(t *testing.T) {
	inputs := []string{
		"# closed\n\n``` {" + strings.Repeat("#a k=\"&amp;\" ", 200) + "}\n" + strings.Repeat("payload\n", 30) + "```\n",
		"# closed\n\n" + strings.Repeat("::: {}\n", 20) + "body\n" + strings.Repeat(":::\n", 20),
		"# closed\n\n" + strings.Repeat("```go extra\n", 100) + "\n# visible\n",
	}
	for _, input := range inputs {
		for _, field := range []string{"work", "nodes", "depth"} {
			limits := DefaultLimits()
			switch field {
			case "work":
				limits.WorkUnits = 2000
			case "nodes":
				limits.DocumentNodes = 40
			case "depth":
				limits.SyntaxDepth = 2
			}
			budget, _ := NewOperationBudget(limits)
			r := parseWithProfile(context.Background(), NewSource([]byte(input)), budget, pandocBlocksProfile)
			// A malformed fallback need not build nested nodes; its work and node
			// controls still must be charged, while depth is relevant to real Divs.
			if field == "depth" && !strings.Contains(input, "::: ") {
				continue
			}
			if field == "nodes" && strings.Contains(input, "go extra") {
				continue
			}
			if r.Interruption != LimitExceededReason || r.Limit == nil || r.Status != Incomplete || r.Document.parsedComplete {
				t.Fatalf("%s %+v", field, r)
			}
		}
		cancelled := 0
		for point := 1; point < 75; point++ {
			budget, _ := NewOperationBudget(DefaultLimits())
			ctx := &checkpointContext{Context: context.Background(), remaining: point}
			r := parseWithProfile(ctx, NewSource([]byte(input)), budget, pandocBlocksProfile)
			if r.Interruption == Cancelled {
				cancelled++
				if r.Document.parsedComplete || r.Document.References().Availability != Unavailable {
					t.Fatal("cancelled authority")
				}
			}
		}
		if cancelled < 10 {
			t.Fatal("cancellation checkpoints missing")
		}
	}
	budget, _ := NewOperationBudget(DefaultLimits())
	source := NewSource([]byte("::: {#a}\n```go\nx\n```\n:::\n"))
	first := parseWithProfile(context.Background(), source, budget, pandocBlocksProfile)
	work, nodes := budget.work, budget.nodes
	second := parseWithProfile(context.Background(), source, budget, pandocBlocksProfile)
	if first.Failure != nil || second.Failure != nil || budget.work != 2*work || budget.nodes != 2*nodes {
		t.Fatalf("cumulative charges %d/%d -> %d/%d", work, nodes, budget.work, budget.nodes)
	}
	if _, err := second.Document.LiteralSpans(context.Background(), budget); err == nil {
		t.Fatal("partial literal consumer authority")
	}
	if _, err := second.Document.ProtectedNodes(context.Background(), budget); err == nil {
		t.Fatal("partial protection authority")
	}
	fresh := pandocBlocks(t, ":::\n")
	if fresh.Document.Root().Children()[0].Kind() != Paragraph {
		t.Fatal("Div state leaked")
	}
}

func TestPandocFenceDelimiterPayloadAndProjection(t *testing.T) {
	for _, tt := range []struct {
		input, literal       string
		closeStart, closeEnd int
	}{
		{"```\n```", "", 4, 7},
		{"```\nx\n```", "x", 6, 9},
		{"````\na\n```\n~~~\n```` junk\nb\n`````\n", "a\n```\n~~~\n```` junk\nb", 27, 32},
	} {
		n := pandocBlocks(t, tt.input).Document.Root().Children()[0]
		if n.Properties().Literal != Some(tt.literal) {
			t.Fatal(n.Properties().Literal)
		}
		assertByteSpan(t, n.Span(), 0, len(tt.input), tt.input)
		m := n.Markers()
		assertByteSpan(t, m[len(m)-1].Source, tt.closeStart, tt.closeEnd, tt.input[tt.closeStart:tt.closeEnd])
		for _, s := range n.Content() {
			if !s.Source.Present() || s.Source.End() > len(tt.input) {
				t.Fatal("invented bytes")
			}
		}
	}
	for spaces := 0; spaces <= 4; spaces++ {
		input := strings.Repeat(" ", spaces) + "```go\nx\n```\n"
		n := pandocBlocks(t, input).Document.Root().Children()[0]
		if spaces < 4 && n.Kind() != CodeBlock || spaces == 4 && n.Properties().Code.Style != "indented" {
			t.Fatalf("indent %d %v", spaces, n.spec)
		}
	}
	for _, tt := range []struct {
		info    string
		classes []string
	}{{"go {.X .X}", []string{"go", "X", "X"}}, {"{.Python}", []string{"Python"}}, {"C++", []string{"c++"}}, {"Objective-C", []string{"objective-c"}}} {
		n := pandocBlocks(t, "```"+tt.info+"\nx\n```\n").Document.Root().Children()[0]
		if !reflect.DeepEqual(n.Properties().PandocFence.Classes, tt.classes) {
			t.Fatal(n.Properties())
		}
	}
}
func TestPandocNativeDivEOFAndFrameBoundaries(t *testing.T) {
	input := "::::: outer\n::: inner\n:::: deepest\nx\n:::\n::::\n:::\n"
	root := pandocBlocks(t, input).Document.Root()
	outer := root.Children()[0]
	inner := outer.Children()[0]
	deep := inner.Children()[0]
	if len(root.Children()) != 1 || len(outer.Children()) != 1 || len(inner.Children()) != 1 || len(deep.Children()) != 1 || outer.Kind() != Div || inner.Kind() != Div || deep.Kind() != Div || deep.Children()[0].Kind() != Paragraph {
		t.Fatal(pandocKinds(root))
	}
	assertByteSpan(t, outer.Span(), 0, 50, input)
	assertByteSpan(t, inner.Span(), 12, 46, input[12:46])
	assertByteSpan(t, deep.Span(), 22, 41, input[22:41])
	unclosed := "::: {}\ntext"
	n := pandocBlocks(t, unclosed).Document.Root().Children()[0]
	assertByteSpan(t, n.Span(), 0, 11, unclosed)
	if len(n.Markers()) != 1 || len(n.Children()) != 1 {
		t.Fatal("invented physical Div closer")
	}
	for _, input := range []string{"::: {}\n> child\n:::\nafter\n", "::: {}\n- child\n:::\nafter\n"} {
		root := pandocBlocks(t, input).Document.Root()
		if len(root.Children()) != 2 || root.Children()[0].Kind() != Div || root.Children()[1].Kind() != Paragraph {
			t.Fatal(pandocKinds(root))
		}
		div := root.Children()[0]
		if len(div.Children()) != 1 || div.Children()[0].Span().End() != strings.Index(input, ":::\nafter") {
			t.Fatal("outer closing source belongs to child")
		}
	}
}
func TestPandocAttributeWideCostSaturation(t *testing.T) {
	const max64 = int64(^uint64(0) >> 1)
	product := pandocWork(0, max64, 2)
	if product != max64 || pandocWork(64, product, 16) != max64 {
		t.Fatal("attribute reservation product overflow")
	}
}

func TestPandocMultilineOpenerPhysicalBackslash(t *testing.T) {
	input := "``` {k=abc\\\n}\nx\n```\n"
	n := pandocBlocks(t, input).Document.Root().Children()[0]
	if n.Kind() != CodeBlock || !reflect.DeepEqual(n.Properties().Attributes.KeyValues, []AttributeKeyValue{{"k", "abc\\"}}) || n.Properties().Literal != Some("x") {
		t.Fatal(n.spec)
	}
	a := n.Properties().Attributes
	assertByteSpan(t, n.Span(), 0, 20, input)
	assertValueRange(t, a.Occurrences[0].Raw, 5, 11, "k=abc\\", "k=abc\\")
	assertByteSpan(t, n.Markers()[1].Source, 16, 19, "```")
}

func TestPandocOpenerUnquotedLiteralQuotes(t *testing.T) {
	for _, value := range []string{`ab"cd`, `ab'cd`} {
		for _, input := range []string{"``` {k=" + value + "}\npayload\n```\n", "# H {k=" + value + "}\n", "::: {k=" + value + "}\nchild\n:::\n"} {
			n := pandocBlocks(t, input).Document.Root().Children()[0]
			if n.Properties().Attributes == nil || !reflect.DeepEqual(n.Properties().Attributes.KeyValues, []AttributeKeyValue{{"k", value}}) {
				t.Fatal(n.spec)
			}
			if n.Kind() == CodeBlock && n.Properties().Literal != Some("payload") {
				t.Fatal("opener consumed literal payload")
			}
		}
	}
	for _, input := range []string{"# H {k=abc\\\n}\nnext\n", "::: {k=abc\\\n}\nchild\n:::\n"} {
		n := pandocBlocks(t, input).Document.Root().Children()[0]
		if n.Properties().Attributes == nil || !reflect.DeepEqual(n.Properties().Attributes.KeyValues, []AttributeKeyValue{{"k", "abc\\"}}) {
			t.Fatal(n.spec)
		}
	}
}

func TestPandocHeadingEscapedClosingHash(t *testing.T) {
	for _, input := range []string{"# H \\# {#a}\n", "# H \\#\n"} {
		n := pandocBlocks(t, input).Document.Root().Children()[0]
		if got := newAttributeInput(context.Background(), n.Content()).text; got != "H \\#" {
			t.Fatalf("escaped closing hash text %q", got)
		}
		assertByteSpan(t, n.Content()[0].Source, 2, 6, "H \\#")
		if strings.Contains(input, "{#a}") {
			if n.Properties().Attributes == nil || n.Properties().Attributes.ID != Some("a") {
				t.Fatal(n.spec)
			}
		} else if n.Properties().Attributes != nil {
			t.Fatal("false attributes")
		}
		if len(n.Markers()) != 1 {
			t.Fatal("escaped hash became closer")
		}
	}
	n := pandocBlocks(t, "# H \\## {#a}\n").Document.Root().Children()[0]
	if newAttributeInput(context.Background(), n.Content()).text != "H \\#" {
		t.Fatal("escaped first hash in terminal run")
	}
	assertByteSpan(t, n.Markers()[1].Source, 6, 7, "#")
}
func TestPandocAttributeNumericReferences(t *testing.T) {
	a, _ := attributeFixture(t, `{a="&#000000065;" b="&#x000000041;" c="&#999999999999999999999999;" d="&#0;" e=&#000000065; f="&#xD800;"}`)
	want := []AttributeKeyValue{{"a", "A"}, {"b", "A"}, {"c", "�"}, {"d", "�"}, {"e", "&#000000065;"}, {"f", "�"}}
	if !reflect.DeepEqual(a.KeyValues, want) {
		t.Fatal(a.KeyValues)
	}
	for _, input := range []string{"``` {k=\"&#000000065;\"}\nx\n```\n", "# H {k=\"&#000000065;\"}\n", "::: {k=\"&#000000065;\"}\n:::\n"} {
		n := pandocBlocks(t, input).Document.Root().Children()[0]
		if !reflect.DeepEqual(n.Properties().Attributes.KeyValues, []AttributeKeyValue{{"k", "A"}}) {
			t.Fatal(n.spec)
		}
	}
}

func TestPandocHeadingClosingHashEscapeParity(t *testing.T) {
	for _, tt := range []struct {
		raw, text string
		closing   bool
	}{
		{`# H \\# {#a}`, `H \\`, true},
		{`# H \\\# {#a}`, `H \\\#`, false},
		{`# H ### {#a}`, `H`, true},
	} {
		n := pandocBlocks(t, tt.raw+"\n").Document.Root().Children()[0]
		if newAttributeInput(context.Background(), n.Content()).text != tt.text {
			t.Fatalf("hash parity %q %+v", tt.raw, n.spec)
		}
		if (len(n.Markers()) == 2) != tt.closing {
			t.Fatal("hash parity markers")
		}
		if n.Properties().Attributes.ID != Some("a") {
			t.Fatal("lost attributes")
		}
	}
}

func TestPandocBareDivClassAdjacentNormalizedSource(t *testing.T) {
	input := "::: 0\x00\nbody\n:::\n"
	n := pandocBlocks(t, input).Document.Root().Children()[0]
	if n.Kind() != Div {
		t.Fatal("native Div")
	}
	assertByteSpan(t, n.Span(), 0, len(input), input)
	a := n.Properties().Attributes
	if a == nil || !a.BareClass || len(a.Classes) != 1 || a.Classes[0] != "0\ufffd" || a.Raw.Semantic != Some("0\ufffd") || len(a.Raw.Segments) != 2 {
		t.Fatal("normalized effective class")
	}
	assertByteSpan(t, a.Raw.Segments[0].Source, 4, 5, "0")
	assertByteSpan(t, a.Raw.Segments[1].Source, 5, 6, "\x00")
	if a.Raw.Segments[1].Transformation != Normalized || a.Raw.Segments[1].Semantic != Some("\ufffd") {
		t.Fatal("NUL provenance")
	}
	if _, err := NewNode(n.spec); err != nil {
		t.Fatal(err)
	}
	good := pandocBlocks(t, "::: abc\n:::\n").Document.Root().Children()[0]
	spec := good.spec
	spec.Properties = good.Properties()
	attrs := spec.Properties.Attributes
	index := LineIndex{source: spec.Envelope.source, ctx: context.Background()}
	parts := append(index.semanticText(4, 5).Segments, index.semanticText(6, 7).Segments...)
	fake := Value{Segments: parts, Semantic: Some("ac")}
	attrs.Raw = fake
	attrs.Classes = []string{"ac"}
	attrs.Occurrences[0].Raw = fake
	attrs.Occurrences[0].Value = fake
	if _, err := NewNode(spec); err == nil {
		t.Fatal("ordinary bare-class bytes lost through false source gap")
	}
}
