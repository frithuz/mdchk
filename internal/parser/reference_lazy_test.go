package parser_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

func TestReferenceComponentsRequireContainerPrefixes(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, tc := range []struct {
			first, outside, marked, key string
			firstStart, outsidePrefix   int
		}{
			{"> [a]:", "/u", "> /u", "a", 2, 0},
			{"> [a", "b]: /u", "> b]: /u", "a b", 2, 0},
			{"> [a]: /u \"first", "second\"", "> second\"", "a", 2, 0},
			{"- [a]:", "/u", "  /u", "a", 2, 0},
			{"> - [a]:", "> /u", ">   /u", "a", 4, 2},
			{"- > [a]:", "  /u", "  > /u", "a", 4, 2},
		} {
			input := tc.first + ending + tc.outside + ending
			r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
			if r.Failure != nil || r.Status != parser.Incomplete || r.Phases != (parser.PhaseProgress{Blocks: parser.PhaseComplete, Inlines: parser.Pending}) || conformance.Evaluate(r).Status != "pending" || len(r.Document.References().Definitions) != 0 {
				t.Fatalf("outside line became invisible definition: %q %+v", input, r)
			}
			var paragraphs []*parser.Node
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() == parser.Paragraph {
					paragraphs = append(paragraphs, n)
				}
				return true
			})
			if len(paragraphs) != 2 {
				t.Fatalf("container boundary lost: %q %d paragraphs", input, len(paragraphs))
			}
			root := r.Document.Root().Children()
			if tc.outsidePrefix == 0 {
				if len(root) != 2 || root[1].Kind() != parser.Paragraph || root[0].Span().End() != len(tc.first)+len(ending) {
					t.Fatal("outside paragraph remained in unmatched container", input, root)
				}
			} else {
				if len(root) != 1 {
					t.Fatal("matched outer container split", input, root)
				}
				siblings := root[0].Children()
				if root[0].Kind() == parser.List {
					siblings = siblings[0].Children()
				}
				if len(siblings) != 2 || siblings[1].Kind() != parser.Paragraph || siblings[0].Span().End() != len(tc.first)+len(ending) {
					t.Fatal("partial container match lost outside ownership", input, siblings)
				}
			}
			for i, w := range []struct {
				start, end int
				text       string
			}{{tc.firstStart, len(tc.first) + len(ending), tc.first[tc.firstStart:]}, {len(tc.first) + len(ending) + tc.outsidePrefix, len(input), tc.outside[tc.outsidePrefix:]}} {
				n := paragraphs[i]
				if n.Span().Start() != w.start || n.Span().End() != w.end || referenceNodeText(n) != w.text || len(n.Content()) != 1 || n.Content()[0].Source.Start() != w.start || n.Content()[0].Source.End() != w.end-len(ending) {
					t.Fatal("paragraph ownership/source", input, i, n)
				}
			}
			marked := tc.first + ending + tc.marked + ending
			r = parser.Parse(context.Background(), parser.NewSource([]byte(marked)))
			refs := r.Document.References()
			selected, exists := refs.Lookup[tc.key]
			if r.Failure != nil || r.Status != parser.Complete || conformance.Evaluate(r).Status != "rendered" || len(refs.Definitions) != 1 || !exists || selected != 0 {
				t.Fatal("properly prefixed definition lost", marked, r)
			}
			if string(r.Source.Bytes()) != marked {
				t.Fatal("source changed")
			}
			definition := refs.Definitions[0].Properties()
			if definition.Destination.Semantic != parser.Some("/u") {
				t.Fatal("prefixed destination", definition)
			}
			if strings.Contains(tc.first, "first") && (definition.Title == nil || definition.Title.Semantic != parser.Some("first\nsecond")) {
				t.Fatal("prefixed title lost original logical contents", definition)
			}
		}
	}
}

func TestReferenceLazyProseAndOptionalTitleFallback(t *testing.T) {
	for _, input := range []string{"> text\ncontinued\n", "> [a\nordinary words\n", "> [a]: not a definition\ncontinued\n"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		root := r.Document.Root().Children()
		if r.Failure != nil || len(r.Document.References().Definitions) != 0 || len(root) != 1 || root[0].Kind() != parser.BlockQuote || len(root[0].Children()) != 1 || referenceNodeText(root[0].Children()[0]) != strings.TrimSuffix(input[2:], "\n") {
			t.Fatal("genuine lazy prose split", input, r)
		}
	}
	input := "> [a]: /u\n> \"title\noutside\"\n"
	r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
	root := r.Document.Root().Children()
	refs := r.Document.References()
	if r.Failure != nil || len(refs.Definitions) != 1 || refs.Definitions[0].Properties().Title != nil || len(root) != 2 || referenceNodeText(root[0].Children()[0]) != "\"title" || referenceNodeText(root[1]) != "outside\"" {
		t.Fatal("lazy optional title replaced destination-only fallback", r)
	}
	input = "> [a]: /u\n> prose\ncontinued\n"
	r = parser.Parse(context.Background(), parser.NewSource([]byte(input)))
	root = r.Document.Root().Children()
	if len(root) != 1 || len(r.Document.References().Definitions) != 1 || referenceNodeText(root[0].Children()[0]) != "prose\ncontinued" {
		t.Fatal("post-definition prose lost laziness", r)
	}
}

type referenceWorkContext struct {
	context.Context
	calls int
}

func (c *referenceWorkContext) Err() error { c.calls++; return nil }

func referenceTitleInput(n int, marked, closed bool) string {
	line := "continuation text\n"
	if marked {
		line = "> " + line
	}
	input := "> [x]: /url \"\n" + strings.Repeat(line, n)
	if closed {
		if marked {
			input += "> "
		}
		input += "\"\n"
	}
	return input
}

func TestReferenceLazyTitleWorkIsLinear(t *testing.T) {
	for _, tc := range []struct {
		name           string
		marked, closed bool
		definitions    int
	}{{"lazy-closed", false, true, 0}, {"lazy-unfinished", false, false, 0}, {"prefixed-closed", true, true, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			previous := 0
			for _, n := range []int{1000, 2000, 4000, 8000} {
				ctx := &referenceWorkContext{Context: context.Background()}
				input := referenceTitleInput(n, tc.marked, tc.closed)
				r := parser.Parse(ctx, parser.NewSource([]byte(input)))
				if r.Failure != nil || len(r.Document.References().Definitions) != tc.definitions || string(r.Source.Bytes()) != input {
					t.Fatal("title semantics/source", n, r)
				}
				if previous != 0 && ctx.calls > previous*3 {
					t.Fatalf("doubling title repeats growing prefix: n=%d calls=%d previous=%d", n, ctx.calls, previous)
				}
				t.Logf("lines=%d context checks=%d", n, ctx.calls)
				previous = ctx.calls
			}
		})
	}
}

func BenchmarkReferenceContainerTitles(b *testing.B) {
	for _, tc := range []struct {
		name           string
		marked, closed bool
		definitions    int
	}{{"lazy-closed", false, true, 0}, {"lazy-unfinished", false, false, 0}, {"prefixed-closed", true, true, 1}} {
		for _, n := range []int{1000, 2000, 4000, 8000} {
			b.Run(tc.name+"/"+strconv.Itoa(n), func(b *testing.B) {
				source := parser.NewSource([]byte(referenceTitleInput(n, tc.marked, tc.closed)))
				b.ReportAllocs()
				b.SetBytes(int64(source.Len()))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					r := parser.Parse(context.Background(), source)
					if r.Failure != nil || len(r.Document.References().Definitions) != tc.definitions {
						b.Fatal("title semantics", r)
					}
				}
			})
		}
	}
}
