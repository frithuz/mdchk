package parser_test

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

func TestNestedEmphasisMappedOwnershipAndOpacity(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, fixture := range []struct{ prefix, raw, logical, suffix string }{
			{"> - ", "é\x00" + ending + ">   β", "é\ufffd" + ending + "β", ""},
			{"> ", "é\x00" + ending + "β", "é\ufffd" + ending + "β", ""},
			{"# ", "é\x00 β", "é\ufffd β", " #"},
			{"", "é\x00 β", "é\ufffd β", ending + "==="},
			{"", "a &amp; `*` <i title='*'> <ab:*>", "a &amp; `*` <i title='*'> <ab:*>", ""},
			{"", `\* &amp; &#42;`, `\* &amp; &#42;`, ""},
			{"\\\\", "emphasis", "emphasis", ""},
			{"", "a", "a", " <ab:x>"},
			{"", "x", "x", " <a>"},
		} {
			input := fixture.prefix + "***" + fixture.raw + "***" + fixture.suffix
			t.Run(strconv.Quote(input), func(t *testing.T) {
				source := parser.NewSource([]byte(input))
				r := parser.Parse(context.Background(), source)
				checkStagedTextOutcome(t, r, true)
				var nested []*parser.Node
				parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
					switch n.Kind() {
					case parser.Paragraph, parser.Heading:
						checkInlineSourceCoverage(t, n)
					case parser.Emphasis, parser.Strong:
						checkEmphasisProvenance(t, source, n)
						nested = append(nested, n)
					}
					return true
				})
				if len(nested) != 2 || nested[0].Kind() != parser.Emphasis || nested[1].Kind() != parser.Strong || len(nested[0].Children()) != 1 || nested[0].Children()[0].Kind() != parser.Strong {
					t.Fatal("outer Emphasis / inner Strong", nested)
				}
				start, end := len(fixture.prefix), len(fixture.prefix)+6+len(fixture.raw)
				for depth, n := range nested {
					width := depth + 1
					m := n.Markers()
					if n.Span().Start() != start+depth || n.Span().End() != end-depth || m[0].Source.Start() != start+depth || m[0].Source.End() != start+depth+width || m[1].Source.Start() != end-depth-width || m[1].Source.End() != end-depth || n.Properties().Delimiter.Width != width {
						t.Fatal("independent original nested markers", depth, n.Span(), m)
					}
					var raw strings.Builder
					for _, part := range n.Content() {
						raw.WriteString(part.Semantic.Value)
					}
					want := fixture.logical
					if depth == 0 {
						want = "**" + want + "**"
					}
					if raw.String() != want {
						t.Fatal("raw parent/child Content", depth, raw.String(), want)
					}
					// Mutate every accessor layer through both parent and nested handles.
					m[0].Role = "changed"
					n.Properties().Delimiter.Width = 99
					n.Content()[0].Semantic = parser.Some("changed")
					children := n.Children()
					*children[0] = parser.Node{}
					children[0] = nil
				}
				if string(source.Bytes()) != input || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
					t.Fatal("nested accessor/source isolation or call state")
				}
				if out := conformance.Evaluate(r); out.Status != "rendered" || !strings.Contains(out.HTML, "<em><strong>") {
					t.Fatal("nested parse did not reach adapter", out)
				}
			})
		}
	}
}

func TestNestedEmphasisLongRunsAndIndependentMarkers(t *testing.T) {
	for _, depth := range []int{1, 32, 512} {
		input := strings.Repeat("*", depth*2) + "x" + strings.Repeat("*", depth*2)
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
		nodes := r.Document.Root().Children()[0].Children()
		for level := 0; level < depth; level++ {
			if len(nodes) != 1 {
				t.Fatal("long-run child count", level, len(nodes))
			}
			n := nodes[0]
			m, content := n.Markers(), n.Content()
			start, end := level*2, len(input)-level*2
			if n.Kind() != parser.Strong || n.Span().Start() != start || n.Span().End() != end || n.Properties().Delimiter.Width != 2 || m[0].Source.Start() != start || m[0].Source.End() != start+2 || m[1].Source.Start() != end-2 || m[1].Source.End() != end {
				t.Fatal("long-run independent nested intervals", level, n)
			}
			if len(content) != 1 || content[0].Source.Start() != start+2 || content[0].Source.End() != end-2 || content[0].Semantic != parser.Some(input[start+2:end-2]) {
				t.Fatal("nested payload fragmented at inner markers", level, content)
			}
			nodes = n.Children()
		}
		if len(nodes) != 1 || nodes[0].Kind() != parser.Text || nodes[0].Span().Start() != depth*2 || nodes[0].Span().End() != depth*2+1 || nodes[0].Properties().Literal != parser.Some("x") {
			t.Fatal("deepest original text", nodes)
		}
	}
}

func TestNestedEmphasisFixtureConsumerAndBlockIsolation(t *testing.T) {
	// Exercises the block-fixture oracle with its independently counted upstream
	// marker table, not a parser-generated expected tree.
	source := parser.NewSource([]byte("***foo***\n"))
	r := parser.Parse(context.Background(), source)
	checkStagedTextOutcome(t, r, true)
	checkInlineText(t, r, r.Document.Root().Children()[0], "***foo***", []byteRange{{0, 9}})
	for _, input := range []string{"***open\n\nclose***", "- ***open\n- close***", "    ***code***", "~~~\n***code***\n~~~", "<!-- ***raw*** -->"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.Emphasis || n.Kind() == parser.Strong {
				t.Fatal("nested block/opacity boundary", input)
			}
			return true
		})
	}
}
