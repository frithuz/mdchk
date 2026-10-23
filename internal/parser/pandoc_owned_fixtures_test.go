package parser

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Independently authored block-stage expectations for the immutable #237 inputs.
// Templates below specify full trees and all owned source ranges. Transforming
// these expectations for quote/CRLF/prefix dimensions does not inspect Go output.
type owned239Node struct {
	kind                                     Kind
	start, end, contentStart, contentEnd     int
	semantic                                 string
	openStart, openEnd, closeStart, closeEnd int
	language, rawFormat                      string
	groupStart, groupEnd                     int
	occurrences                              [][2]int
	id                                       string
	classes                                  []string
	children                                 []owned239Node
}

func node239(kind Kind, start, end, cs, ce int, semantic string) owned239Node {
	return owned239Node{kind: kind, start: start, end: end, contentStart: cs, contentEnd: ce, semantic: semantic}
}
func fixture239(extension string) (string, []owned239Node) {
	var input string
	var nodes []owned239Node
	switch extension {
	case "backtick_code_blocks":
		input = "before\n``` text\n[^hidden]\n```\nafter\n"
		n := node239(CodeBlock, 7, 30, 16, 26, "[^hidden]")
		n.openStart = 7
		n.openEnd = 10
		n.closeStart = 26
		n.closeEnd = 29
		n.language = "text"
		nodes = []owned239Node{node239(Paragraph, 0, 7, 0, 6, "before"), n, node239(Paragraph, 30, 36, 30, 35, "after")}
	case "fenced_code_blocks":
		input = "~~~ text\npayload\n~~~\n"
		n := node239(CodeBlock, 0, 21, 9, 17, "payload")
		n.openEnd = 3
		n.closeStart = 17
		n.closeEnd = 20
		n.language = "text"
		nodes = []owned239Node{n}
	case "fenced_code_attributes":
		input = "``` {.python #one #two}\nx\n```\n"
		n := node239(CodeBlock, 0, 30, 24, 26, "x")
		n.openEnd = 3
		n.closeStart = 26
		n.closeEnd = 29
		n.language = "python"
		n.groupStart = 4
		n.groupEnd = 23
		n.occurrences = [][2]int{{5, 12}, {13, 17}, {18, 22}}
		n.id = "two"
		n.classes = []string{"python"}
		nodes = []owned239Node{n}
	case "header_attributes":
		input = "# title {#one #two}\n"
		n := node239(Heading, 0, 20, 2, 7, "title")
		n.openEnd = 1
		n.groupStart = 8
		n.groupEnd = 19
		n.occurrences = [][2]int{{9, 13}, {14, 18}}
		n.id = "two"
		nodes = []owned239Node{n}
	case "fenced_divs":
		input = "::: {#one #two .box}\n\n*child*\n\n:::\n"
		n := node239(Div, 0, 35, 0, 0, "")
		n.openEnd = 3
		n.closeStart = 31
		n.closeEnd = 34
		n.groupStart = 4
		n.groupEnd = 20
		n.occurrences = [][2]int{{5, 9}, {10, 14}, {15, 19}}
		n.id = "two"
		n.classes = []string{"box"}
		n.children = []owned239Node{node239(Paragraph, 22, 30, 22, 29, "*child*")}
		nodes = []owned239Node{n}
	case "raw_attribute":
		input = "```{=latex}\n\\alpha [^hidden]\n```\n"
		n := node239(RawBlock, 0, 33, 12, 29, "\\alpha [^hidden]")
		n.openEnd = 3
		n.closeStart = 29
		n.closeEnd = 32
		n.rawFormat = "latex"
		nodes = []owned239Node{n}
	}
	return input, nodes
}
func offset239(n owned239Node, delta int) owned239Node {
	n.start += delta
	n.end += delta
	if n.contentEnd > n.contentStart {
		n.contentStart += delta
		n.contentEnd += delta
	}
	if n.openEnd > n.openStart {
		n.openStart += delta
		n.openEnd += delta
	}
	if n.closeEnd > n.closeStart {
		n.closeStart += delta
		n.closeEnd += delta
	}
	if n.groupEnd > n.groupStart {
		n.groupStart += delta
		n.groupEnd += delta
		copy := make([][2]int, len(n.occurrences))
		for i, r := range n.occurrences {
			copy[i] = [2]int{r[0] + delta, r[1] + delta}
		}
		n.occurrences = copy
	}
	n.children = append([]owned239Node(nil), n.children...)
	for i := range n.children {
		n.children[i] = offset239(n.children[i], delta)
	}
	return n
}
func protected239(input string) owned239Node {
	n := node239(CodeBlock, 0, len(input)+10, 5, len(input)+5, strings.TrimSuffix(input, "\n"))
	n.openEnd = 4
	n.closeStart = 5 + len(input)
	n.closeEnd = 9 + len(input)
	return n
}
func TestPandocOwnedFenceAttributeDivFixtures(t *testing.T) {
	const root = "testdata/pandoc/3.12.1/"
	data, err := os.ReadFile(root + "expectations.json")
	if err != nil {
		t.Fatal(err)
	}
	var collection struct {
		Cases []struct{ ID, Extension, Dimension, Input string }
	}
	if err := json.Unmarshal(data, &collection); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, c := range collection.Cases {
		base, nodes := fixture239(c.Extension)
		if base == "" {
			continue
		}
		counts[c.Extension]++
		t.Run(c.ID, func(t *testing.T) {
			input := base
			quoted, crlf := false, false
			switch c.Dimension {
			case "fallback":
				switch c.Extension {
				case "backtick_code_blocks":
					input = "`` text\nbody\n"
				case "fenced_code_blocks":
					input = "~~ text\nbody\n"
				case "fenced_code_attributes":
					input = "``` {#only}\nx\n```\n"
					n := node239(CodeBlock, 0, 19, 12, 14, "x")
					n.openEnd = 3
					n.closeStart = 14
					n.closeEnd = 17
					n.end = 18
					n.groupStart = 4
					n.groupEnd = 11
					n.occurrences = [][2]int{{5, 10}}
					n.id = "only"
					nodes = []owned239Node{n}
				case "header_attributes":
					input = "# title {broken\n"
				case "fenced_divs":
					input = ":: {#box}\n"
				case "raw_attribute":
					input = "```{python}\nx\n```\n"
				}
				if c.Extension != "fenced_code_attributes" {
					kind, cs := Paragraph, 0
					if c.Extension == "header_attributes" {
						kind = Heading
						cs = 2
					}
					nodes = []owned239Node{node239(kind, 0, len(input), cs, len(input)-1, input[cs:len(input)-1])}
					if kind == Heading {
						nodes[0].openEnd = 1
					}
				}
			case "escape":
				input = "\\" + strings.Split(base, "\n")[0] + "\n"
				if c.Extension == "backtick_code_blocks" {
					input = "\\``` text\n"
				}
				nodes = []owned239Node{node239(Paragraph, 0, len(input), 0, len(input)-1, input[:len(input)-1])}
			case "protected":
				nodes = []owned239Node{protected239(base)}
				input = "~~~~\n" + base + "~~~~\n"
			case "precedence":
				n := protected239(base)
				delta := len(base) + 11
				for i := range nodes {
					nodes[i] = offset239(nodes[i], delta)
				}
				nodes = append([]owned239Node{n}, nodes...)
				input = "~~~~\n" + base + "~~~~\n\n" + base
			case "unicode":
				delta := len("é é\tΩ\n\n")
				for i := range nodes {
					nodes[i] = offset239(nodes[i], delta)
				}
				nodes = append([]owned239Node{node239(Paragraph, 0, delta-1, 0, delta-2, "é é\tΩ")}, nodes...)
				input = "é é\tΩ\n\n" + base
			case "nested":
				quoted = true
			case "crlf":
				crlf = true
			case "loss-sensitive":
				switch c.ID {
				case "raw-ids-heading":
					input = "# H {#first #last}\n"
					n := node239(Heading, 0, 19, 2, 3, "H")
					n.openEnd = 1
					n.groupStart = 4
					n.groupEnd = 18
					n.occurrences = [][2]int{{5, 11}, {12, 17}}
					n.id = "last"
					nodes = []owned239Node{n}
				case "raw-ids-block-code":
					input = "``` {#first #last}\nx\n```\n"
					n := node239(CodeBlock, 0, 25, 19, 21, "x")
					n.openEnd = 3
					n.closeStart = 21
					n.closeEnd = 24
					n.groupStart = 4
					n.groupEnd = 18
					n.occurrences = [][2]int{{5, 11}, {12, 17}}
					n.id = "last"
					nodes = []owned239Node{n}
				case "raw-ids-div":
					input = "::: {#first #last}\n\nchild\n\n:::\n"
					n := node239(Div, 0, 31, 0, 0, "")
					n.openEnd = 3
					n.closeStart = 27
					n.closeEnd = 30
					n.groupStart = 4
					n.groupEnd = 18
					n.occurrences = [][2]int{{5, 11}, {12, 17}}
					n.id = "last"
					n.children = []owned239Node{node239(Paragraph, 20, 26, 20, 25, "child")}
					nodes = []owned239Node{n}
				case "protected-raw-format":
					input = "```{=latex}\n[^x] \\alpha\n```\nordinary é\n"
					n := node239(RawBlock, 0, 28, 12, 24, "[^x] \\alpha")
					n.openEnd = 3
					n.closeStart = 24
					n.closeEnd = 27
					n.rawFormat = "latex"
					nodes = []owned239Node{n, node239(Paragraph, 28, 40, 28, 39, "ordinary é")}
				default:
					t.Fatal("unaccounted loss fixture", c.ID)
				}
			}
			// Map each logical byte independently before parsing. The byte inventory is
			// an input transformation, never an inference from semantic parser values.
			starts := make([]int, len(input)+1)
			ends := make([]int, len(input)+1)
			var physical strings.Builder
			for i := 0; i < len(input); i++ {
				ends[i] = physical.Len()
				if quoted && (i == 0 || input[i-1] == '\n') {
					physical.WriteString("> ")
				}
				starts[i] = physical.Len()
				if crlf && input[i] == '\n' {
					physical.WriteByte('\r')
				}
				physical.WriteByte(input[i])
			}
			starts[len(input)] = physical.Len()
			ends[len(input)] = physical.Len()
			expectedInput := physical.String()
			actualInput, err := os.ReadFile(root + c.Input)
			if err != nil {
				t.Fatal(err)
			}
			if string(actualInput) != expectedInput {
				t.Fatalf("authored fixture template mismatch: got %q want %q", actualInput, expectedInput)
			}
			r := pandocBlocks(t, expectedInput)
			children := r.Document.Root().Children()
			if quoted {
				if len(children) != 1 || children[0].Kind() != BlockQuote {
					t.Fatal("quote ownership")
				}
				assertByteSpan(t, children[0].Span(), 0, len(expectedInput), expectedInput)
				children = children[0].Children()
			}
			if len(children) != len(nodes) {
				t.Fatalf("direct children %v want %+v", pandocKinds(r.Document.Root()), nodes)
			}
			var check func(*Node, owned239Node)
			rangeCheck := func(v Value, start, end int) {
				t.Helper()
				var gotRaw strings.Builder
				var gotRanges, wantRanges [][2]int
				add := func(dst *[][2]int, start, end int) {
					if start == end {
						return
					}
					if len(*dst) > 0 && (*dst)[len(*dst)-1][1] == start {
						(*dst)[len(*dst)-1][1] = end
					} else {
						*dst = append(*dst, [2]int{start, end})
					}
				}
				previous := -1
				for _, s := range v.Segments {
					if s.Source.Start() < previous {
						t.Fatal("source reordered")
					}
					previous = s.Source.End()
					add(&gotRanges, s.Source.Start(), s.Source.End())
					gotRaw.WriteString(expectedInput[s.Source.Start():s.Source.End()])
				}
				var wantRaw strings.Builder
				for i := start; i < end; i++ {
					stop := ends[i+1]
					if stop < starts[i] {
						stop = starts[i] + 1
					}
					add(&wantRanges, starts[i], stop)
					wantRaw.WriteString(expectedInput[starts[i]:stop])
				}
				if !reflect.DeepEqual(gotRanges, wantRanges) {
					t.Fatalf("source ranges %v want %v", gotRanges, wantRanges)
				}
				if gotRaw.String() != wantRaw.String() {
					t.Fatalf("raw source got%q want%q", gotRaw.String(), wantRaw.String())
				}
			}
			check = func(actual *Node, w owned239Node) {
				t.Helper()
				if actual.Kind() != w.kind {
					t.Fatalf("kind %v want%v", actual.Kind(), w.kind)
				}
				assertByteSpan(t, actual.Span(), starts[w.start], ends[w.end], expectedInput[starts[w.start]:ends[w.end]])
				p := actual.Properties()
				if w.contentEnd > w.contentStart {
					rangeCheck(Value{Segments: actual.Content()}, w.contentStart, w.contentEnd)
					var semantic strings.Builder
					for _, s := range actual.Content() {
						semantic.WriteString(s.Semantic.Value)
					}
					if semantic.String() != w.semantic {
						t.Fatalf("semantic %q want%q", semantic.String(), w.semantic)
					}
				}
				if w.openEnd > w.openStart {
					markers := actual.Markers()
					if len(markers) != 1+boolInt239(w.closeEnd > w.closeStart) {
						t.Fatalf("markers %+v", markers)
					}
					assertByteSpan(t, markers[0].Source, starts[w.openStart], ends[w.openEnd], input[w.openStart:w.openEnd])
					if w.closeEnd > w.closeStart {
						assertByteSpan(t, markers[1].Source, starts[w.closeStart], ends[w.closeEnd], input[w.closeStart:w.closeEnd])
					}
				}
				if w.groupEnd > w.groupStart {
					a := p.Attributes
					if a == nil || a.ID != Some(w.id) || !reflect.DeepEqual(a.Classes, w.classes) || len(a.Occurrences) != len(w.occurrences) {
						t.Fatalf("attributes %+v want %+v", a, w)
					}
					rangeCheck(a.Raw, w.groupStart, w.groupEnd)
					for i, v := range w.occurrences {
						rangeCheck(a.Occurrences[i].Raw, v[0], v[1])
					}
				} else if p.Attributes != nil {
					t.Fatal("false attributes")
				}
				if w.kind == CodeBlock || w.kind == RawBlock {
					if p.PandocFence == nil || p.PandocFence.Language.Present != (w.language != "") || p.PandocFence.Language.Value != w.language || p.Literal != Some(w.semantic) {
						t.Fatal(p)
					}
					if w.rawFormat != "" {
						if p.PandocFence.RawFormat == nil || p.PandocFence.RawFormat.Semantic != Some(w.rawFormat) {
							t.Fatal(p)
						}
					} else if p.PandocFence.RawFormat != nil {
						t.Fatal("false raw format")
					}
				}
				if w.kind == Div && (p.Div == nil || p.Div.Origin != "fenced-native") {
					t.Fatal("div origin")
				}
				children := actual.Children()
				if len(children) != len(w.children) {
					t.Fatal("direct child count")
				}
				for i, c := range w.children {
					check(children[i], c)
				}
			}
			for i, w := range nodes {
				check(children[i], w)
			}
		})
	}
	if !reflect.DeepEqual(counts, map[string]int{"backtick_code_blocks": 8, "fenced_code_blocks": 8, "fenced_code_attributes": 9, "header_attributes": 9, "fenced_divs": 9, "raw_attribute": 9}) {
		t.Fatalf("owned fixture accounting %+v", counts)
	}
}
func boolInt239(v bool) int {
	if v {
		return 1
	}
	return 0
}
