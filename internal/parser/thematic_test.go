package parser_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

type blockWant struct {
	kind     parser.Kind
	envelope byteRange
	markers  []byteRange
}

func checkThematicBlocks(t *testing.T, input string, complete bool, want []blockWant) parser.ParseResult {
	t.Helper()
	bytes := []byte(input)
	source := parser.NewSource(bytes)
	r := parser.Parse(context.Background(), source)
	if r.Failure != nil || r.Interruption != "" || r.Document == nil || string(r.Source.Bytes()) != input {
		t.Fatalf("unexpected parse outcome: %+v", r)
	}
	refs := r.Document.References()
	if complete {
		if r.Status != parser.Complete || r.AST != parser.Available || len(r.Uncertainty) != 0 || r.Phases != (parser.PhaseProgress{Blocks: parser.PhaseComplete, Inlines: parser.PhaseComplete}) || refs.Availability != parser.Available || refs.LookupAvailability != parser.Available {
			t.Fatalf("thematic-only input did not complete: %+v", r)
		}
	} else if r.Status != parser.Incomplete || r.AST != parser.Partial || len(r.Uncertainty) != 1 || r.Uncertainty[0].Reason != parser.SyntaxUnimplemented || r.Uncertainty[0].Region.Start() != 0 || r.Uncertainty[0].Region.End() != source.Len() || r.Phases != (parser.PhaseProgress{Blocks: parser.Pending, Inlines: parser.NotStarted}) || refs.Availability != parser.Unavailable || refs.LookupAvailability != parser.Unavailable {
		t.Fatalf("mixed/unknown source gained false completeness: %+v", r)
	}
	if len(refs.Definitions) != 0 || len(refs.Lookup) != 0 {
		t.Fatal("invented reference records")
	}
	root := r.Document.Root()
	if root.Span().Start() != 0 || root.Span().End() != len(input) {
		t.Fatal("root does not account for the original snapshot envelope")
	}
	nodes := ungroupedChildren(root)
	if len(nodes) != len(want) {
		t.Fatalf("got %d blocks, want %d", len(nodes), len(want))
	}
	foreign := parser.NewSource([]byte(input))
	for i, n := range nodes {
		w := want[i]
		if n.Kind() != w.kind || n.Span().Start() != w.envelope.start || n.Span().End() != w.envelope.end {
			t.Fatalf("block %d = %s [%d,%d), want %+v", i, n.Kind(), n.Span().Start(), n.Span().End(), w)
		}
		if _, err := source.Text(n.Span()); err != nil {
			t.Fatal("foreign node envelope", err)
		}
		if _, err := foreign.Text(n.Span()); !errors.Is(err, parser.ErrRange) {
			t.Fatal("independent snapshot accepted node envelope")
		}
		var ranges []byteRange
		for markerIndex, m := range n.Markers() {
			ranges = append(ranges, byteRange{m.Source.Start(), m.Source.End()})
			text, err := source.Text(m.Source)
			role := "thematic-marker"
			if n.Kind() == parser.CodeBlock {
				role = "indent-prefix"
			}
			if n.Kind() == parser.Heading {
				role = "underline"
			}
			if n.Kind() == parser.ListItem {
				role = "item-prefix"
				if markerIndex == 1 {
					role = "item-marker"
				}
			}
			if err != nil || m.Role != role || len(text) == 0 {
				t.Fatal("marker lost original spelling/role", m, err)
			}
			for j := 0; n.Kind() != parser.ListItem && role != "indent-prefix" && j < len(text); j++ {
				if text[j] != text[0] || text[j] != '*' && text[j] != '-' && text[j] != '_' {
					t.Fatal("marker range contains non-marker bytes", text)
				}
			}
			if _, err := foreign.Text(m.Source); !errors.Is(err, parser.ErrRange) {
				t.Fatal("independent snapshot accepted marker range")
			}
		}
		if !reflect.DeepEqual(ranges, w.markers) {
			t.Fatalf("block %d markers = %v, want %v", i, ranges, w.markers)
		}
		if n.Kind() == parser.ThematicBreak && (len(n.Content()) != 0 || len(n.Children()) != 0 || !reflect.DeepEqual(n.Properties(), parser.Properties{})) {
			t.Fatal("break invented semantic payload or inline children")
		}
	}
	if len(bytes) > 0 {
		bytes[0] = '!'
	}
	copy := r.Source.Bytes()
	if len(copy) > 0 {
		copy[0] = '!'
	}
	if string(source.Bytes()) != input || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
		t.Fatal("source mutation or repeat parse changed the immutable result")
	}
	return r
}

func TestThematicUpstreamTargets(t *testing.T) {
	// Independent expectations over the unmodified upstream inputs. Marker ranges
	// are maximal contiguous runs, excluding indentation/inter-marker whitespace.
	wants := map[int][]blockWant{
		11: {{parser.ThematicBreak, byteRange{0, 7}, []byteRange{{0, 1}, {2, 3}, {4, 5}}}},
		43: {{parser.ThematicBreak, byteRange{0, 4}, []byteRange{{0, 3}}}, {parser.ThematicBreak, byteRange{4, 8}, []byteRange{{4, 7}}}, {parser.ThematicBreak, byteRange{8, 12}, []byteRange{{8, 11}}}},
		44: {{parser.Paragraph, byteRange{0, 4}, nil}},
		45: {{parser.Paragraph, byteRange{0, 4}, nil}}, // Equals without content is a paragraph.
		46: {{parser.Paragraph, byteRange{0, 9}, nil}},
		47: {{parser.ThematicBreak, byteRange{0, 5}, []byteRange{{1, 4}}}, {parser.ThematicBreak, byteRange{5, 11}, []byteRange{{7, 10}}}, {parser.ThematicBreak, byteRange{11, 18}, []byteRange{{14, 17}}}},
		48: {{parser.CodeBlock, byteRange{0, 8}, []byteRange{{0, 4}}}},
		49: {{parser.Paragraph, byteRange{0, 12}, nil}},
		50: {{parser.ThematicBreak, byteRange{0, 38}, []byteRange{{0, 37}}}},
		51: {{parser.ThematicBreak, byteRange{0, 7}, []byteRange{{1, 2}, {3, 4}, {5, 6}}}},
		52: {{parser.ThematicBreak, byteRange{0, 20}, []byteRange{{1, 3}, {5, 6}, {7, 9}, {10, 11}, {12, 14}, {15, 16}, {17, 19}}}},
		53: {{parser.ThematicBreak, byteRange{0, 22}, []byteRange{{0, 1}, {6, 7}, {13, 14}, {20, 21}}}},
		54: {{parser.ThematicBreak, byteRange{0, 12}, []byteRange{{0, 1}, {2, 3}, {4, 5}, {6, 7}}}},
		55: {{parser.Paragraph, byteRange{0, 10}, nil}, {parser.Paragraph, byteRange{11, 19}, nil}, {parser.Paragraph, byteRange{20, 28}, nil}},
		56: {{parser.Paragraph, byteRange{0, 5}, nil}},
		57: {{parser.ListItem, byteRange{0, 6}, []byteRange{{0, 2}, {0, 1}}}, {parser.ThematicBreak, byteRange{6, 10}, []byteRange{{6, 9}}}, {parser.ListItem, byteRange{10, 16}, []byteRange{{10, 12}, {10, 11}}}},
		58: {{parser.Paragraph, byteRange{0, 4}, nil}, {parser.ThematicBreak, byteRange{4, 8}, []byteRange{{4, 7}}}, {parser.Paragraph, byteRange{8, 12}, nil}},
		59: {{parser.Heading, byteRange{0, 8}, []byteRange{{4, 7}}}, {parser.Paragraph, byteRange{8, 12}, nil}},
		60: {{parser.ListItem, byteRange{0, 6}, []byteRange{{0, 2}, {0, 1}}}, {parser.ThematicBreak, byteRange{6, 12}, []byteRange{{6, 7}, {8, 9}, {10, 11}}}, {parser.ListItem, byteRange{12, 18}, []byteRange{{12, 14}, {12, 13}}}},
		61: {{parser.ListItem, byteRange{0, 6}, []byteRange{{0, 2}, {0, 1}}}, {parser.ListItem, byteRange{6, 14}, []byteRange{{6, 8}, {6, 7}}}},
		98: {{parser.ThematicBreak, byteRange{0, 4}, []byteRange{{0, 3}}}, {parser.ThematicBreak, byteRange{4, 8}, []byteRange{{4, 7}}}},
	}
	data, err := os.ReadFile("testdata/commonmark/0.31.2/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples []struct {
		ID       int    `json:"example"`
		Section  string `json:"section"`
		Markdown string `json:"markdown"`
		HTML     string `json:"html"`
	}
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	seen := make(map[int]bool)
	for _, e := range examples {
		want, target := wants[e.ID]
		if !target {
			continue
		}
		section := "Thematic breaks"
		if e.ID == 11 {
			section = "Tabs"
		} else if e.ID == 98 {
			section = "Setext headings"
		}
		if seen[e.ID] || e.Section != section {
			t.Fatal("duplicate or wrong-section target", e.ID)
		}
		seen[e.ID] = true
		t.Run(strconv.Itoa(e.ID), func(t *testing.T) {
			complete := e.ID == 11 || e.ID == 43 || e.ID == 47 || e.ID == 48 || e.ID >= 50 && e.ID <= 54 || e.ID == 98
			r := checkThematicBlocks(t, e.Markdown, complete, want)
			got := conformance.Evaluate(r)
			if complete {
				if got.Status != "rendered" || got.HTML != e.HTML || got.Reason != "" {
					t.Fatalf("actual parser/adapter lost upstream HTML: %+v, want %q", got, e.HTML)
				}
			} else if got.Status != "pending" || got.Reason != "syntax-unimplemented" || got.HTML != "" {
				t.Fatalf("later-provider outcome is not pending: %+v", got)
			}
			if e.ID == 49 {
				checkParagraphs(t, r.Source, []paragraphWant{{byteRange{0, 12}, []byteRange{{0, 4}, {8, 11}}, "Foo\n***"}})
			}
			if e.ID == 58 {
				for i, text := range map[int]string{0: "Foo", 2: "bar"} {
					content := r.Document.Root().Children()[i].Content()
					if len(content) != 1 || content[0].Semantic != parser.Some(text) {
						t.Fatal("paragraph interruption lost logical source content")
					}
				}
			}
		})
	}
	if len(seen) != 21 {
		t.Fatal("not every original example 43–61 and supported case 11/98 was exercised")
	}
}

func TestThematicBoundariesAndPrecedence(t *testing.T) {
	for _, tt := range []struct {
		input    string
		complete bool
		want     []blockWant
	}{
		{"***", true, []blockWant{{parser.ThematicBreak, byteRange{0, 3}, []byteRange{{0, 3}}}}},
		{"   *** \t\r\n", true, []blockWant{{parser.ThematicBreak, byteRange{0, 10}, []byteRange{{3, 6}}}}},
		{"*\t*\t*\t\r", true, []blockWant{{parser.ThematicBreak, byteRange{0, 7}, []byteRange{{0, 1}, {2, 3}, {4, 5}}}}},
		{" \n***\r\n \t\r___", true, []blockWant{{parser.ThematicBreak, byteRange{2, 7}, []byteRange{{2, 5}}}, {parser.ThematicBreak, byteRange{10, 13}, []byteRange{{10, 13}}}}},
		{"Foo\n - - -\nbar\n", false, []blockWant{{parser.Paragraph, byteRange{0, 4}, nil}, {parser.ThematicBreak, byteRange{4, 11}, []byteRange{{5, 6}, {7, 8}, {9, 10}}}, {parser.Paragraph, byteRange{11, 15}, nil}}},
		{"é\n* * *\r\nz", false, []blockWant{{parser.Paragraph, byteRange{0, 3}, nil}, {parser.ThematicBreak, byteRange{3, 10}, []byteRange{{3, 4}, {5, 6}, {7, 8}}}, {parser.Paragraph, byteRange{10, 11}, nil}}},
		{"Foo\n--- \t\n", false, []blockWant{{parser.Heading, byteRange{0, 10}, []byteRange{{4, 7}}}}},
		{"Foo\n  ---\rbar", false, []blockWant{{parser.Heading, byteRange{0, 10}, []byteRange{{6, 9}}}, {parser.Paragraph, byteRange{10, 13}, nil}}},
		{"***\nFoo\n---\n", false, []blockWant{{parser.ThematicBreak, byteRange{0, 4}, []byteRange{{0, 3}}}, {parser.Heading, byteRange{4, 12}, []byteRange{{8, 11}}}}},
	} {
		t.Run(tt.input, func(t *testing.T) { checkThematicBlocks(t, tt.input, tt.complete, tt.want) })
	}
	for _, input := range []string{"+++", "===", "**", "--", "__", "*-*", "***x", "***\x00", "***\u00a0", "***\v", "***\f", "\u00a0***", "***é"} {
		t.Run(input, func(t *testing.T) {
			r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
			if r.Status != parser.Incomplete || r.Failure != nil || string(r.Source.Bytes()) != input {
				t.Fatal("negative candidate changed source or completed", r)
			}
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() == parser.ThematicBreak {
					t.Fatal("invalid thematic candidate recognized")
				}
				return true
			})
		})
	}
}

func TestThematicAccessorsKeepFrozenMarkers(t *testing.T) {
	source := parser.NewSource([]byte("* * *\n"))
	r := parser.Parse(context.Background(), source)
	nodes := r.Document.Root().Children()
	markers := nodes[0].Markers()
	markers[0].Role = "changed"
	markers[0].Source = parser.Span{}
	*nodes[0] = parser.Node{}
	if !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
		t.Fatal("returned node/marker mutation changed the frozen parse")
	}
}
