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

func assertStrike(t *testing.T, n *Node, a, b, width int, payload string, segments ...[2]int) {
	t.Helper()
	assertTaskSpan(t, n.Span(), a, b)
	p := n.Properties()
	if n.Kind() != Strike || p.Delimiter == nil || *p.Delimiter != (DelimiterProperties{'~', width}) || p.Literal.Present {
		t.Fatal("strike type/arity", n.spec)
	}
	markers := n.Markers()
	if len(markers) != 2 || markers[0].Role != "opening" || markers[1].Role != "closing" {
		t.Fatal("markers", markers)
	}
	assertTaskSpan(t, markers[0].Source, a, a+width)
	assertTaskSpan(t, markers[1].Source, b-width, b)
	if tableRaw(t, n.Span().source, markers[0].Source) != strings.Repeat("~", width) || tableRaw(t, n.Span().source, markers[1].Source) != strings.Repeat("~", width) {
		t.Fatal("original spelling")
	}
	var raw strings.Builder
	for _, part := range n.Content() {
		raw.WriteString(tableRaw(t, n.Span().source, part.Source))
	}
	if raw.String() != payload {
		t.Fatalf("raw content %q want %q", raw.String(), payload)
	}
	if segments != nil {
		if len(n.Content()) != len(segments) {
			t.Fatal("content segments", n.Content())
		}
		for i, w := range segments {
			assertTaskSpan(t, n.Content()[i].Source, w[0], w[1])
		}
	}
}

func TestStrikePublished491To493(t *testing.T) {
	for _, id := range []int{491, 492, 493} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			input := originalTaskInput(t, id)
			r := parseTableStage(t, input)
			strikes := tableNodes(r.Document.root, Strike)
			switch id {
			case 491:
				if input != "~~Hi~~ Hello, ~there~ world!\n" || len(strikes) != 2 {
					t.Fatal("original 491", input, len(strikes))
				}
				assertStrike(t, strikes[0], 0, 6, 2, "Hi", [2]int{2, 4})
				assertStrike(t, strikes[1], 14, 21, 1, "there", [2]int{15, 20})
				children := r.Document.root.Children()[0].Children()
				if len(children) != 4 || children[1].Properties().Literal != Some(" Hello, ") || children[3].Properties().Literal != Some(" world!") || tableText(strikes[0]) != "Hi" || tableText(strikes[1]) != "there" {
					t.Fatal("ordered retained text", children)
				}
			case 492:
				if len(strikes) != 0 || len(r.Document.root.Children()) != 2 {
					t.Fatal("paragraph boundary")
				}
				paras := r.Document.root.Children()
				assertTaskSpan(t, contentEnvelope(paras[0].Content()), 0, 12)
				assertTaskSpan(t, contentEnvelope(paras[1].Content()), 14, 30)
				if tableText(paras[0]) != "This ~~has a" || tableText(paras[1]) != "new paragraph~~." {
					t.Fatal("unmatched text")
				}
			case 493:
				if len(strikes) != 0 || tableText(r.Document.root) != "This will ~~~not~~~ strike." {
					t.Fatal("triple literal", input)
				}
			}
			assertStrikeDisabled(t, r.Source)
		})
	}
}

func assertStrikeDisabled(t *testing.T, source Source) {
	t.Helper()
	baseline := Parse(context.Background(), source)
	requireTableComplete(t, baseline, string(source.Bytes()))
	staged := parseWithProfile(context.Background(), source, budgetFor(t, DefaultLimits()), gfmInProgressProfile)
	requireTableComplete(t, staged, string(source.Bytes()))
	for _, r := range []ParseResult{Parse(context.Background(), source), ParseWithBudget(context.Background(), source, budgetFor(t, DefaultLimits())), parseWithProfile(context.Background(), source, budgetFor(t, DefaultLimits()), coreProfile)} {
		requireTableComplete(t, r, string(source.Bytes()))
		if !reflect.DeepEqual(r.Document.root, baseline.Document.root) {
			t.Fatal("core tree changed after stage")
		}
		Walk(r.Document.root, func(n *Node) bool {
			if n.Kind() == Strike || n.Kind() == Table || n.Kind() == TableRow || n.Kind() == TableCell || n.Properties().Task != nil {
				t.Error("extension leak", n.Kind())
			}
			return true
		})
	}
}

func TestStrikePairingAndLiteralGrammar(t *testing.T) {
	for _, input := range []string{"~a", "~~a", "a~", "a~~", "~a~~", "~~a~", "~ a~", "~a ~", "~ ~", "~~ ~~", "~\ta~", "~a\t~", "~\u00a0a~", "~a\u00a0~", "~\fa~", "~a\f~", "x ~~~a~~~", "x ~~~~a~~~~", "x ~~~a~", "x ~a~~~", "x ~~a~~~", "x ~~~~a~~", "\\~a\\~", "&#126;a&#126;", "&tilde;a&tilde;", "`~a~`", "<span title='~a~'>", "<!-- ~a~ -->", "<?x ~a~ ?>", "<!X ~a~>", "<![CDATA[~a~]]>", "<https://x.test/~a~>", "[x](/~a~ \"~b~\")", "<script>\n~a~\n</script>\n", "<textarea>\n~a~\n\n~b~\n</textarea>\n", "~a\n\nb~", "~a\r\n \t\r\nb~", "# ~a\n\n~b\n", "- ~a\n- b~\n", "| ~a | b~ |\n| - | - |\n"} {
		t.Run(fmt.Sprintf("literal-%q", input), func(t *testing.T) {
			r := parseTableStage(t, input)
			if len(tableNodes(r.Document.root, Strike)) != 0 {
				t.Fatal("literal acquired strike")
			}
			core := Parse(context.Background(), r.Source)
			if len(tableNodes(r.Document.root, Table)) == 0 && !reflect.DeepEqual(core.Document.root, r.Document.root) {
				t.Fatal("literal lost ordinary tree")
			}
		})
	}
	for _, tc := range []struct {
		input string
		spans [][3]int
		text  string
	}{
		{"~a~", [][3]int{{0, 3, 1}}, "a"}, {"~~a~~", [][3]int{{0, 5, 2}}, "a"}, {"a~b~c", [][3]int{{1, 4, 1}}, "abc"},
		{"~a~~b~", [][3]int{{0, 6, 1}}, "a~~b"}, {"~~a~ b~~", [][3]int{{0, 8, 2}}, "a~ b"},
		{"~a ~~b~ c~~", [][3]int{{0, 7, 1}}, "a ~~b c~~"}, {"~~a ~b~ c~~", [][3]int{{0, 11, 2}, {4, 7, 1}}, "a b c"},
		{"~a ~~b~~ c~", [][3]int{{0, 11, 1}, {3, 8, 2}}, "a b c"},
		{"~a\vb~", [][3]int{{0, 5, 1}}, "a\vb"}, {"a~\u0085b~c", [][3]int{{1, 6, 1}}, "a\u0085bc"},
		{"~£~bravo.", nil, "~£~bravo."}, {"\\~a~b~", [][3]int{{3, 6, 1}}, "~ab"},
		{"\\\\~a~", [][3]int{{2, 5, 1}}, "\\a"}, {"~a\nb~", [][3]int{{0, 5, 1}}, "ab"},
		{"~[a~](/u)", nil, "~a~"}, {"[~a](/u)~", nil, "~a~"},
		{"~[a~](broken", [][3]int{{0, 4, 1}}, "[a](broken"},
		{"~[a](/u) [b](/v)~", [][3]int{{0, 17, 1}}, "a b"},
		{"[~a~](/u) ~b~", [][3]int{{1, 4, 1}, {10, 13, 1}}, "a b"},
	} {
		t.Run(fmt.Sprintf("pair-%q", tc.input), func(t *testing.T) {
			r := parseTableStage(t, tc.input)
			ns := tableNodes(r.Document.root, Strike)
			if len(ns) != len(tc.spans) {
				t.Fatal("pair count", len(ns), tc.spans)
			}
			for i, w := range tc.spans {
				raw := tableRaw(t, r.Source, ns[i].Span())
				assertStrike(t, ns[i], w[0], w[1], w[2], raw[w[2]:len(raw)-w[2]])
			}
			if tableText(r.Document.root) != tc.text {
				t.Fatalf("remaining text %q want %q", tableText(r.Document.root), tc.text)
			}
			assertStrikeDisabled(t, r.Source)
		})
	}
	for _, input := range []string{"~~~\n~x~\n~~~\n", "~~~\n~x~\n", "> ~~~\n> ~x~\n> ~~~\n", "- ~~~\n  ~x~\n  ~~~\n", "    ~a~\n"} {
		r := parseTableStage(t, input)
		if len(tableNodes(r.Document.root, Strike)) != 0 || len(tableNodes(r.Document.root, CodeBlock)) != 1 {
			t.Fatal("ordinary fence/code precedence", input)
		}
		assertStrikeDisabled(t, r.Source)
	}
}

func TestStrikeSharedSyntaxAndSourceConsumers(t *testing.T) {
	input := "[r]: /first \"title\"\n[r]: /later\n\n> - [X] ~~*a* **b** [~ref~][r] ![~~pic~~][r] `~code~` <!-- note --> <i>~c~</i>~~\r\n"
	source := NewSource([]byte(input))
	op := budgetFor(t, DefaultLimits())
	r := parseWithProfile(context.Background(), source, op, gfmInProgressProfile)
	requireTableComplete(t, r, input)
	for kind, w := range map[Kind]int{Strike: 4, Emphasis: 1, Strong: 1, Link: 1, Image: 1, CodeSpan: 1, InlineHTML: 3} {
		if got := len(tableNodes(r.Document.root, kind)); got != w {
			t.Fatal(kind, got, w)
		}
	}
	outer := tableNodes(r.Document.root, Strike)[0]
	if outer.Properties().Delimiter.Width != 2 || !strings.Contains(tableRaw(t, source, outer.Content()[0].Source), "*a* **b**") {
		t.Fatal("raw nested markup")
	}
	for _, kind := range []Kind{Link, Image} {
		n := tableNodes(outer, kind)[0]
		if n.Properties().Link.Destination.Semantic != Some("/first") || n.Properties().Link.Resolution != Available {
			t.Fatal("reference binding")
		}
	}
	constructed, err := NewDocument(r.Document.root, r.Document.references)
	if err != nil {
		t.Fatal(err)
	}
	literal, err := r.Document.LiteralSpans(context.Background(), op)
	if err != nil || len(literal) != 4 {
		t.Fatal("literals", literal, err)
	}
	protected, err := r.Document.ProtectedNodes(context.Background(), op)
	if err != nil || len(protected) != 4 {
		t.Fatal("protection", err)
	}
	comments, err := r.Document.Comments(context.Background(), op)
	if err != nil || len(comments) != 1 || tableRaw(t, source, comments[0].Envelope) != "<!-- note -->" {
		t.Fatal("comments", err)
	}
	for _, d := range []*Document{r.Document, constructed} {
		foreign := budgetFor(t, DefaultLimits())
		if _, err := d.Comments(context.Background(), foreign); err == nil {
			t.Fatal("foreign/manual authority")
		}
		if _, err := d.LiteralSpans(context.Background(), foreign); err == nil {
			t.Fatal("foreign literal authority")
		}
		if _, err := d.ProtectedNodes(context.Background(), foreign); err == nil {
			t.Fatal("foreign protected authority")
		}
	}
	again := parseWithProfile(context.Background(), source, op, gfmInProgressProfile)
	requireTableComplete(t, again, input)
	if !reflect.DeepEqual(r.Document.root, again.Document.root) {
		t.Fatal("consumer mutation")
	}
	for _, input := range []string{"*~a~*", "**~~a~~**", "~~**a **b** c**~~", "~`a`~", "~<span title='~'>a</span>~", "![~~a~~](/u)", "~[a](/u)~", "[~a~](/u)", "~a  \nb~", "~a\\\nb~"} {
		r := parseTableStage(t, input)
		if len(tableNodes(r.Document.root, Strike)) == 0 {
			t.Fatal("shared syntax", input)
		}
		assertStrikeDisabled(t, r.Source)
	}
}

func TestStrikeMappedSourceAndModel(t *testing.T) {
	for _, tc := range []struct {
		input      string
		end, close int
		parts      [][2]int
	}{
		{"> ~~é\n> fin~~\n", 14, 12, [][2]int{{4, 7}, {9, 12}}},
		{"> ~~é\r\n> fin~~\r\n", 15, 13, [][2]int{{4, 8}, {10, 13}}},
		{"> ~~é\r> fin~~\r", 14, 12, [][2]int{{4, 7}, {9, 12}}},
	} {
		r := parseTableStage(t, tc.input)
		ns := tableNodes(r.Document.root, Strike)
		if len(ns) != 1 {
			t.Fatal("mapped strike")
		}
		payload := "é\nfin"
		if strings.Contains(tc.input, "\r\n") {
			payload = "é\r\nfin"
		} else if strings.Contains(tc.input, "\r") {
			payload = "é\rfin"
		}
		assertStrike(t, ns[0], 2, tc.end, 2, payload, tc.parts...)
		if tc.close == 13 {
			idx := NewLineIndex(r.Source)
			for off, w := range map[int]Position{2: {1, 3}, 4: {1, 5}, 6: {1, 6}, 8: {2, 1}, 13: {2, 6}, 15: {2, 8}, 17: {3, 1}} {
				got, err := idx.Position(off)
				if err != nil || got != w {
					t.Fatal(off, got, err)
				}
			}
			for _, off := range []int{5, 7, 16} {
				if _, err := idx.Position(off); err == nil {
					t.Fatal("interior", off)
				}
			}
		}
		assertStrikeDisabled(t, r.Source)
	}
	for _, input := range []string{"> - [x] ~~é\t\x00\r\n>   fin~~\r\n", "- ~a\nlazy~", "- ~a~\n- ~b~\n", "# ~é~", "| ~a\\|b~ | ~~x~~ |\n| - | - |\n| ~c~ | ~d~ | ~excess~ |\n"} {
		r := parseTableStage(t, input)
		if len(tableNodes(r.Document.root, Strike)) == 0 {
			t.Fatal("container/table task context")
		}
		for _, n := range tableNodes(r.Document.root, Strike) {
			for _, m := range n.Markers() {
				if tableRaw(t, r.Source, m.Source) != strings.Repeat("~", n.Properties().Delimiter.Width) {
					t.Fatal("mapped marker")
				}
			}
		}
	}
	r := parseTableStage(t, "~~a *b*~~")
	original := tableNodes(r.Document.root, Strike)[0]
	fresh := func() NodeSpec {
		return NodeSpec{Kind: Strike, Envelope: original.Span(), Markers: original.Markers(), Content: original.Content(), Children: original.Children(), Properties: original.Properties()}
	}
	spec := fresh()
	n, err := NewNode(spec)
	if err != nil {
		t.Fatal(err)
	}
	spec.Markers[0] = Marker{}
	spec.Content[0] = Segment{}
	spec.Properties.Delimiter.Width = 1
	*spec.Children[0] = Node{}
	got := n.Properties()
	got.Delimiter.Marker = '*'
	marks := n.Markers()
	marks[0] = Marker{}
	parts := n.Content()
	parts[0] = Segment{}
	children := n.Children()
	*children[0] = Node{}
	bytes := r.Source.Bytes()
	bytes[0] = '!'
	if !reflect.DeepEqual(n.spec, original.spec) || tableRaw(t, r.Source, n.Span()) != "~~a *b*~~" {
		t.Fatal("immutable ownership")
	}
	for name, mutate := range map[string]func(*NodeSpec){
		"missing": func(s *NodeSpec) { s.Properties.Delimiter = nil }, "width0": func(s *NodeSpec) { s.Properties.Delimiter.Width = 0 }, "width3": func(s *NodeSpec) { s.Properties.Delimiter.Width = 3 }, "marker": func(s *NodeSpec) { s.Properties.Delimiter.Marker = '*' }, "kind": func(s *NodeSpec) { s.Kind = Strong }, "literal": func(s *NodeSpec) { s.Properties.Literal = Some("a b") }, "foreign": func(s *NodeSpec) { s.Markers[0].Source, _ = NewSource(r.Source.Bytes()).Span(0, 2) }, "shift": func(s *NodeSpec) { s.Markers[0].Source, _ = r.Source.Span(1, 3) }, "role": func(s *NodeSpec) { s.Markers[1].Role = "opening" }, "absent-close": func(s *NodeSpec) { s.Markers = s.Markers[:1] }, "payload-marker": func(s *NodeSpec) { s.Content[0].Source = s.Markers[0].Source; s.Content[0].Semantic = Some("~~") }, "child-marker": func(s *NodeSpec) {
			child, _ := NewNode(NodeSpec{Kind: Text, Envelope: s.Markers[0].Source})
			s.Children = []*Node{child}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := fresh()
			mutate(&s)
			if _, err := NewNode(s); err == nil {
				t.Fatal("invalid strike accepted")
			}
		})
	}
}

func TestStrikeApprovedSupplementalWitnesses(t *testing.T) {
	data, err := os.ReadFile("testdata/gfm/profile/expectations.json")
	if err != nil {
		t.Fatal(err)
	}
	var b struct{ Fixtures []struct{ ID, Source string } }
	if err = json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, f := range b.Fixtures {
		switch f.ID {
		case "strike-container-gaps", "strike-nested", "strike-mixed", "strike-unmatched", "strike-escaped", "combined-all-five":
		default:
			continue
		}
		seen++
		t.Run(f.ID, func(t *testing.T) {
			r := parseTableStage(t, f.Source)
			ns := tableNodes(r.Document.root, Strike)
			switch f.ID {
			case "strike-container-gaps":
				if len(ns) != 1 {
					t.Fatal("quote")
				}
				assertStrike(t, ns[0], 2, 14, 2, "é\nfin", [2]int{4, 7}, [2]int{9, 12})
			case "strike-nested":
				if len(ns) != 1 || len(tableNodes(ns[0], Emphasis)) != 1 || len(tableNodes(ns[0], Link)) != 1 {
					t.Fatal("shared children")
				}
				assertStrike(t, ns[0], 0, 15, 2, "*a* [b](/u)")
			case "combined-all-five":
				if len(ns) != 1 || len(taskItems(r.Document.root)) != 1 || len(tableNodes(r.Document.root, Table)) != 1 || len(bareNodes(r.Document.root)) != 1 {
					t.Fatal("private stage only")
				}
				assertStrike(t, ns[0], 6, 19, 2, "www.a.org")
			case "strike-mixed", "strike-unmatched", "strike-escaped":
				if len(ns) != 0 {
					t.Fatal("literal approved witness")
				}
			}
			assertStrikeDisabled(t, r.Source)
		})
	}
	if seen != 6 {
		t.Fatal("missing witnesses", seen)
	}
}

func TestStrikeResourceDepthAndTermination(t *testing.T) {
	input := "> - [x] ~~**[a](/u)**~~\n"
	source := NewSource([]byte(input))
	baseline := budgetFor(t, DefaultLimits())
	requireTableComplete(t, parseWithProfile(context.Background(), source, baseline, gfmInProgressProfile), input)
	for name, used := range map[string]int64{"document-nodes": baseline.nodes, "work-units": baseline.work, "total-nodes": baseline.nodes} {
		for _, delta := range []int64{-1, 0, 1} {
			l := DefaultLimits()
			switch name {
			case "document-nodes":
				l.DocumentNodes = used + delta
			case "total-nodes":
				l.DocumentNodes = used
				l.TotalNodes = used + 1 + delta
			case "work-units":
				l.WorkUnits = used + delta
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
		func(n int) string { return "prefix " + strings.Repeat("~", n) + "x " + strings.Repeat("~", n) },
		func(n int) string { return "prefix " + strings.Repeat("~a ", n) },
		func(n int) string { return "prefix " + strings.Repeat("~a~~b ", n) },
		func(n int) string { return "prefix " + strings.Repeat("~a~ ~~b~~ *c* _d_ ", n) },
		func(n int) string { return "~a " + strings.Repeat("[~b~](/u) [~~c](/v) ", n) + "z~" },
	} {
		var previous int64
		for _, size := range []int{128, 256, 512} {
			input := generate(size)
			op := budgetFor(t, DefaultLimits())
			r := parseWithProfile(context.Background(), NewSource([]byte(input)), op, gfmInProgressProfile)
			requireTableComplete(t, r, input)
			if previous != 0 && op.work > previous*3 {
				t.Fatal("nonproportional work", previous, op.work)
			}
			previous = op.work
			for _, n := range tableNodes(r.Document.root, Strike) {
				if n.Properties().Delimiter.Width > 2 {
					t.Fatal("long run peeled")
				}
			}
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
}

func TestStrikeCancellationAndConcurrentIsolation(t *testing.T) {
	for _, input := range []string{"prefix " + strings.Repeat("~a~ ~~b~~ *c* ", 300), "prefix " + strings.Repeat("~", 8192) + "x", "> - [x] ~~é\x00\r\n>   " + strings.Repeat("payload ", 200) + "fin~~"} {
		source := NewSource([]byte(input))
		for _, deadline := range []bool{false, true} {
			seen := false
			for _, checks := range []int{1, 8, 32, 128, 512, 1024} {
				ctx := &quoteCheckpoint{checkpointContext: checkpointContext{Context: context.Background(), remaining: checks}, deadline: deadline}
				op := budgetFor(t, DefaultLimits())
				r := parseWithProfile(ctx, source, op, gfmInProgressProfile)
				if r.Status == Complete {
					continue
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
				if r.Phases.Inlines == Interrupted && len(tableNodes(r.Document.root, Strike)) != 0 {
					t.Fatal("partial inline publication")
				}
				if _, err := r.Document.ProtectedNodes(context.Background(), op); err == nil {
					t.Fatal("partial authority")
				}
			}
			if !seen {
				t.Fatal("missing interruption")
			}
		}
	}
	source := NewSource([]byte("> - [x] ~~**é** [~a~](/u)~~\n"))
	op := budgetFor(t, DefaultLimits())
	baseline := parseWithProfile(context.Background(), source, budgetFor(t, DefaultLimits()), gfmInProgressProfile)
	requireTableComplete(t, baseline, string(source.Bytes()))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			for j := 0; j < 3; j++ {
				r := parseWithProfile(context.Background(), source, op, gfmInProgressProfile)
				if r.Status != Complete || !reflect.DeepEqual(r.Document.root, baseline.Document.root) {
					t.Error("staged concurrency")
				}
				core := ParseWithBudget(context.Background(), source, op)
				if core.Status != Complete || len(tableNodes(core.Document.root, Strike)) != 0 || len(taskItems(core.Document.root)) != 0 {
					t.Error("core leak")
				}
				independent := parseWithProfile(context.Background(), NewSource(source.Bytes()), budgetFor(t, DefaultLimits()), gfmInProgressProfile)
				if independent.Status != Complete || independent.Source == source || len(tableNodes(independent.Document.root, Strike)) != 2 {
					t.Error("independent source/profile")
				}
			}
		})
	}
	wg.Wait()
	assertStrikeDisabled(t, source)
}

func TestStrikePartialTabAndPairingCheckpoints(t *testing.T) {
	source := NewSource([]byte("~a\n\tb~"))
	span := func(a, b int) Span { s, _ := source.Span(a, b); return s }
	parts := []Segment{{Source: span(0, 3), Transformation: Exact, Semantic: Some("~a\n")}, {Source: span(3, 4), Transformation: PartialTab, Semantic: Some("  ")}, {Source: span(4, 6), Transformation: Exact, Semantic: Some("b~")}}
	ctx := &parserContext{Context: context.Background(), profile: gfmInProgressProfile}
	scanner := inlineScanner{ctx: ctx, text: "~a\n  b~"}
	scanner.opaqueCandidates()
	flat, err := scanner.scan(parts)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := scanner.emphasis(parts, flat)
	if err != nil || len(nodes) != 1 || nodes[0].Kind() != Strike || nodes[0].Content()[1] != parts[1] {
		t.Fatal("partial tab provenance", err, nodes)
	}
	for _, input := range []string{"prefix " + strings.Repeat("~x~ ~~y~~ ", 8) + "suffix", "prefix " + strings.Repeat("~a~~b ", 8) + "z~"} {
		source := NewSource([]byte(input))
		whole, _ := source.Span(0, len(input))
		parts := []Segment{{Source: whole, Transformation: Exact, Semantic: Some(input)}}
		original := inlineScanner{ctx: ctx, text: input}
		original.opaqueCandidates()
		flat, err := original.scan(parts)
		if err != nil {
			t.Fatal(err)
		}
		before := append([]*Node(nil), flat...)
		for _, deadline := range []bool{false, true} {
			observed, completed := 0, false
			for checkpoint := 1; checkpoint < 3000; checkpoint++ {
				cancel := &quoteCheckpoint{checkpointContext: checkpointContext{Context: context.Background(), remaining: checkpoint}, deadline: deadline}
				scanctx := &parserContext{Context: cancel, profile: gfmInProgressProfile}
				s := inlineScanner{ctx: scanctx, text: input, delimiters: append([]inlineDelimiter(nil), original.delimiters...)}
				nodes, err := s.emphasis(parts, flat)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(flat, before) {
					t.Fatal("borrowed nodes changed")
				}
				if interruption(scanctx) != "" {
					observed++
					continue
				}
				if len(nodes) == 0 {
					t.Fatal("lost completed nodes")
				}
				completed = true
				break
			}
			if !completed || observed < 100 {
				t.Fatal("pair/mapping checkpoints", completed, observed)
			}
		}
	}
}

func TestStrikeSelectedStrongAndAdmission(t *testing.T) {
	shape := func(root *Node) string {
		var out strings.Builder
		Walk(root, func(n *Node) bool { out.WriteString(string(n.Kind())); out.WriteByte('/'); return true })
		return out.String()
	}
	for _, id := range []int{398, 426, 434, 435, 436, 473, 474, 475, 477} {
		input := strings.TrimSuffix(originalTaskInput(t, id), "\n")
		core := Parse(context.Background(), NewSource([]byte(input)))
		requireTableComplete(t, core, input)
		if len(core.Document.root.Children()) != 1 || core.Document.root.Children()[0].Kind() != Paragraph {
			t.Fatal("strong witness structure")
		}
		wrapped := parseTableStage(t, "~~"+input+"~~")
		strikes := tableNodes(wrapped.Document.root, Strike)
		if len(strikes) != 1 {
			t.Fatal("wrapped strong")
		}
		if len(tableNodes(strikes[0], Strong)) != len(tableNodes(core.Document.root, Strong)) {
			t.Fatal("selected nested strong count", id)
		}
		var expected, actual strings.Builder
		for _, n := range core.Document.root.Children()[0].Children() {
			expected.WriteString(shape(n))
		}
		for _, n := range strikes[0].Children() {
			actual.WriteString(shape(n))
		}
		if expected.String() != actual.String() {
			t.Fatal("selected core tree flattened", id, expected.String(), actual.String())
		}
	}
	cancel, cancelFunc := context.WithCancel(context.Background())
	cancelFunc()
	l := DefaultLimits()
	l.DocumentBytes = 3
	requireLimit(t, parseWithProfile(cancel, NewSource([]byte("~a~\xff")), budgetFor(t, l), gfmInProgressProfile), "document-bytes")
	r := parseWithProfile(cancel, NewSource([]byte("~\xff~")), budgetFor(t, DefaultLimits()), gfmInProgressProfile)
	if r.Failure == nil || r.Failure.Code != InvalidUTF8 || r.Failure.Location.Start() != 1 || r.Interruption != "" {
		t.Fatal("encoding precedence", r)
	}
	l = DefaultLimits()
	l.WorkUnits = 1
	requireLimit(t, parseWithProfile(cancel, NewSource([]byte("~\xff~")), budgetFor(t, l), gfmInProgressProfile), "work-units")
	requireTableComplete(t, parseWithProfile(context.Background(), NewSource(nil), budgetFor(t, DefaultLimits()), gfmInProgressProfile), "")
	input := "~a~"
	source := NewSource([]byte(input))
	op := budgetFor(t, DefaultLimits())
	requireTableComplete(t, parseWithProfile(context.Background(), source, op, gfmInProgressProfile), input)
	used := op.work
	if err := op.ChargeWork(13); err != nil {
		t.Fatal(err)
	}
	requireTableComplete(t, ParseWithBudget(context.Background(), source, op), input)
	if op.work <= used+13 {
		t.Fatal("cumulative work reset")
	}
}

func TestStrikeMappedPipeDepthAndFinalCheckpoint(t *testing.T) {
	r := parseTableStage(t, "| ~a\\|b~ |\n| - |\n")
	ns := tableNodes(r.Document.root, Strike)
	if len(ns) != 1 {
		t.Fatal("mapped pipe strike")
	}
	assertStrike(t, ns[0], 2, 8, 1, "a\\|b", [2]int{3, 4}, [2]int{4, 6}, [2]int{6, 7})
	if ns[0].Content()[1].Transformation != Escape || tableText(ns[0]) != "a|b" {
		t.Fatal("mapped pipe provenance")
	}
	for _, input := range []string{"£~a~", "~a\vb~", "~~a~b~~", "~a ~~b~~ c~"} {
		r := parseTableStage(t, input)
		if len(tableNodes(r.Document.root, Strike)) == 0 {
			t.Fatal("flanking/nesting", input)
		}
	}
	deep := strings.Repeat("~a ", 300) + "x" + strings.Repeat(" b~", 300)
	source := NewSource([]byte(deep))
	requireLimit(t, parseWithProfile(context.Background(), source, budgetFor(t, DefaultLimits()), gfmInProgressProfile), "syntax-depth")
	l := DefaultLimits()
	l.SyntaxDepth = 512
	r = parseWithProfile(context.Background(), source, budgetFor(t, l), gfmInProgressProfile)
	requireTableComplete(t, r, deep)
	if len(tableNodes(r.Document.root, Strike)) != 300 {
		t.Fatal("raised depth flattened")
	}
	for _, input := range []string{"", "~a~"} {
		source := NewSource([]byte(input))
		for _, deadline := range []bool{false, true} {
			last := ParseResult{}
			completed := false
			for checkpoint := 1; checkpoint < 300; checkpoint++ {
				ctx := &quoteCheckpoint{checkpointContext: checkpointContext{Context: context.Background(), remaining: checkpoint}, deadline: deadline}
				op := budgetFor(t, DefaultLimits())
				r := parseWithProfile(ctx, source, op, gfmInProgressProfile)
				if r.Status == Complete {
					requireTableComplete(t, r, input)
					completed = true
					break
				}
				want := Cancelled
				if deadline {
					want = DeadlineExceeded
				}
				if r.Failure != nil || r.Status != Incomplete || r.AST != Partial || r.Interruption != want || len(r.Uncertainty) != 1 || r.Source != source {
					t.Fatal("checkpoint outcome", r)
				}
				last = r
				if _, err := r.Document.Comments(context.Background(), op); err == nil {
					t.Fatal("unfinished comment authority")
				}
			}
			if !completed || last.Interruption == "" {
				t.Fatal("final publication sweep did not finish")
			}
		}
	}
}
