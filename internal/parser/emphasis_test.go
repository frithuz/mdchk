package parser_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

type emphasisMark struct{ open, width, close int }

// Independently counted original-byte opening/closing positions. Empty entries
// in requiredEmphasis are literal/opaque cases, never omitted acceptance cases.
var emphasisMarks = map[int][]emphasisMark{
	404: {{0, 1, 16}}, 419: {{0, 1, 18}, {6, 1, 10}},
	422: {{0, 2, 17}}, 433: {{0, 2, 19}, {7, 1, 11}},
	15: {{2, 1, 11}}, 37: {{14, 1, 18}}, 56: {{1, 1, 3}}, 66: {{6, 1, 10}},
	80: {{4, 1, 8}, {25, 1, 29}}, 81: {{4, 1, 12}}, 82: {{6, 1, 14}},
	148: {{34, 1, 40}}, 152: {{19, 1, 28}}, 155: {{13, 1, 17}}, 167: {{7, 1, 11}}, 168: {{5, 1, 9}},
	176: {{29, 1, 33}}, 177: {{18, 1, 22}}, 188: {{7, 1, 18}}, 638: {{0, 1, 10}}, 639: {{0, 1, 9}},
	350: {{0, 1, 8}}, 355: {{3, 1, 7}}, 356: {{1, 1, 3}}, 357: {{0, 1, 8}}, 364: {{4, 1, 10}},
	370: {{0, 1, 4}}, 376: {{0, 1, 12}}, 377: {{0, 1, 6}}, 378: {{0, 2, 9}}, 381: {{3, 2, 8}}, 382: {{0, 2, 9}},
	390: {{4, 2, 11}}, 396: {{0, 2, 5}}, 402: {{0, 2, 15}}, 403: {{0, 2, 7}}, 405: {{0, 1, 8}}, 412: {{0, 1, 9}}, 423: {{0, 2, 9}},
	437: {{4, 1, 7}}, 438: {{4, 1, 6}}, 440: {{4, 2, 8}}, 441: {{4, 2, 7}}, 442: {{1, 1, 5}}, 443: {{0, 1, 4}},
	444: {{1, 2, 6}}, 445: {{3, 1, 7}}, 446: {{0, 2, 5}}, 447: {{0, 1, 4}}, 449: {{4, 1, 7}}, 450: {{4, 1, 6}},
	452: {{4, 2, 8}}, 453: {{4, 2, 7}}, 454: {{1, 1, 5}}, 455: {{0, 1, 4}}, 456: {{1, 2, 6}}, 457: {{3, 1, 7}},
	458: {{0, 2, 5}}, 459: {{0, 1, 4}}, 460: {{0, 2, 5}}, 462: {{0, 2, 5}}, 469: {{0, 1, 9}}, 471: {{6, 2, 15}}, 472: {{5, 1, 13}},
	478: {{0, 1, 6}}, 479: {{0, 1, 6}},
	369: {{0, 1, 8}, {2, 1, 6}}, 373: {{0, 1, 8}, {2, 1, 6}},
	389: {{0, 2, 19}, {7, 2, 12}}, 393: {{0, 1, 10}, {2, 2, 7}},
	394: {{0, 2, 72}, {16, 1, 41}, {49, 1, 70}}, 395: {{0, 2, 17}, {7, 1, 11}},
	399: {{0, 1, 10}, {2, 2, 7}}, 406: {{0, 1, 16}, {5, 2, 10}},
	407: {{0, 1, 14}, {5, 1, 9}}, 408: {{0, 1, 10}, {1, 1, 5}},
	409: {{0, 1, 10}, {5, 1, 9}}, 410: {{0, 1, 16}, {5, 2, 10}},
	411: {{0, 1, 14}, {4, 2, 9}}, 413: {{0, 1, 12}, {1, 2, 6}},
	414: {{0, 1, 12}, {5, 2, 10}}, 415: {{0, 1, 11}, {4, 2, 9}},
	416: {{3, 1, 11}, {4, 2, 9}}, 417: {{3, 2, 16}, {5, 2, 14}, {7, 2, 12}},
	418: {{0, 1, 26}, {5, 2, 20}, {11, 1, 15}},
	424: {{0, 2, 15}, {6, 1, 10}}, 425: {{0, 2, 17}, {6, 2, 11}},
	426: {{0, 2, 13}, {2, 2, 7}}, 427: {{0, 2, 13}, {6, 2, 11}},
	428: {{0, 2, 15}, {6, 1, 10}}, 429: {{0, 2, 13}, {5, 1, 9}},
	430: {{0, 2, 11}, {2, 1, 6}}, 431: {{0, 2, 11}, {6, 1, 10}},
	432: {{0, 2, 27}, {6, 1, 22}, {11, 2, 16}},
	461: {{0, 1, 6}, {1, 1, 5}}, 463: {{0, 1, 6}, {1, 1, 5}},
	464: {{0, 2, 9}, {2, 2, 7}}, 465: {{0, 2, 9}, {2, 2, 7}},
	466: {{0, 2, 13}, {2, 2, 11}, {4, 2, 9}}, 467: {{0, 1, 8}, {1, 2, 6}},
	468: {{0, 1, 12}, {1, 2, 10}, {3, 2, 8}}, 470: {{0, 1, 25}, {5, 2, 19}},
}
var requiredEmphasis = []int{
	350, 351, 352, 353, 354, 355, 356, 357, 358, 359, 360, 361, 362, 363,
	364, 365, 366, 367, 368, 370, 371, 372, 374, 375, 376, 377, 378, 379,
	380, 381, 382, 383, 384, 385, 386, 387, 388, 390, 391, 392, 396, 397,
	398, 400, 401, 402, 403, 405, 412, 420, 421, 423, 434, 435, 436, 437,
	438, 439, 440, 441, 442, 443, 444, 445, 446, 447, 448, 449, 450, 451,
	452, 453, 454, 455, 456, 457, 458, 459, 460, 462, 469, 471, 472, 475,
	476, 477, 478, 479, 480, 481,
}
var nestedEmphasis = []int{
	369, 373, 389, 393, 394, 395, 399, 406, 407, 408, 409, 410, 411, 413,
	414, 415, 416, 417, 418, 424, 425, 426, 427, 428, 429, 430, 431, 432,
	461, 463, 464, 465, 466, 467, 468, 470,
}
var linkEmphasis = []int{
	404, 419, 422, 433, 473, 474,
}
var crossEmphasis = []int{
	15, 37, 55, 56, 66, 80, 81, 82, 148, 152, 155, 167, 168, 176,
	177, 188, 638, 639,
}

type emphasisExample struct {
	ID             int `json:"example"`
	Markdown, HTML string
}

var emphasisExamples = sync.OnceValues(func() ([]emphasisExample, error) {
	data, err := os.ReadFile("testdata/commonmark/0.31.2/examples.json")
	if err != nil {
		return nil, err
	}
	var cases []emphasisExample
	err = json.Unmarshal(data, &cases)
	return cases, err
})

func TestEmphasisApprovedPartitionAndUpstream(t *testing.T) {
	ownership := map[int]string{}
	for _, group := range []struct {
		name string
		ids  []int
	}{{"required", requiredEmphasis}, {"nested", nestedEmphasis}, {"links", linkEmphasis}} {
		for _, id := range group.ids {
			if ownership[id] != "" {
				t.Fatal("duplicate partition", id)
			}
			ownership[id] = group.name
		}
	}
	if len(requiredEmphasis) != 90 || len(nestedEmphasis) != 36 || len(linkEmphasis) != 6 || len(ownership) != 132 {
		t.Fatal("changed approved partition")
	}
	for id := 350; id <= 481; id++ {
		if ownership[id] == "" {
			t.Fatal("missing partition", id)
		}
	}
	for _, id := range crossEmphasis {
		ownership[id] = "required"
	}
	cases, err := emphasisExamples()
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range cases {
		_, ok := ownership[e.ID]
		if !ok {
			continue
		}
		seen++
		t.Run(strconv.Itoa(e.ID), func(t *testing.T) {
			source := parser.NewSource([]byte(e.Markdown))
			r := parser.Parse(context.Background(), source)
			out := conformance.Evaluate(r)

			checkStagedTextOutcome(t, r, true)
			if out.Status != "rendered" || out.HTML != e.HTML {
				t.Fatal("unchanged upstream HTML", out, e.HTML)
			}
			var marks []emphasisMark
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				switch n.Kind() {
				case parser.Emphasis, parser.Strong:
					checkEmphasisProvenance(t, source, n)
					m := n.Markers()
					marks = append(marks, emphasisMark{m[0].Source.Start(), n.Properties().Delimiter.Width, m[1].Source.Start()})
				case parser.Paragraph, parser.Heading:
					checkInlineSourceCoverage(t, n)
				case parser.Text:
					checkEmphasisText(t, source, n)
				case parser.CodeSpan:
					checkCodeProvenance(t, source, n)
				case parser.InlineHTML:
					checkInlineHTMLProvenance(t, source, n)
				case parser.Link, parser.Image:
					checkLinkProvenance(t, source, n)
				case parser.HardBreak, parser.SoftBreak:
					checkBreakProperties(t, source, n, n.Properties().Break.Origin, byteRange{n.Span().Start(), n.Content()[0].Source.Start()})
				}
				return true
			})
			if !reflect.DeepEqual(marks, emphasisMarks[e.ID]) {
				t.Fatal("independent consumed marker positions", marks, emphasisMarks[e.ID])
			}
			if string(source.Bytes()) != e.Markdown {
				t.Fatal("source changed")
			}
		})
	}
	if seen != 150 {
		t.Fatal("missing section/cross-section fixtures", seen)
	}
}

func checkEmphasisText(t *testing.T, source parser.Source, n *parser.Node) {
	t.Helper()
	var literal strings.Builder
	for _, part := range n.Content() {
		raw, err := source.Text(part.Source)
		if err != nil {
			t.Fatal(err)
		}
		want := raw
		switch part.Transformation {
		case parser.Exact:
		case parser.Normalized:
			want = oracleNormalizedText(t, source, part)
		case parser.Escape, parser.Entity:
			want = oracleText(raw, entityOracle(t))
		case parser.PartialTab:
			if raw != "\t" || strings.Trim(part.Semantic.Value, " ") != "" || len(part.Semantic.Value) < 1 || len(part.Semantic.Value) > 3 {
				t.Fatal("partial tab", part)
			}
			want = part.Semantic.Value
		default:
			t.Fatal("unknown text transformation", part)
		}
		if part.Semantic != parser.Some(want) {
			t.Fatal("text provenance", part, want)
		}
		literal.WriteString(want)
	}
	if n.Properties().Literal != parser.Some(literal.String()) {
		t.Fatal("text literal", n)
	}
}

func checkEmphasisProvenance(t *testing.T, source parser.Source, n *parser.Node) {
	t.Helper()
	p := n.Properties().Delimiter
	m := n.Markers()
	width := 1
	if n.Kind() == parser.Strong {
		width = 2
	}
	if p == nil || p.Width != width || (p.Marker != '*' && p.Marker != '_') || len(m) != 2 || m[0].Role != "opening" || m[1].Role != "closing" || n.Properties().Literal.Present {
		t.Fatal("emphasis shape", n)
	}
	for _, marker := range m {
		raw, err := source.Text(marker.Source)
		if err != nil || raw != strings.Repeat(string(p.Marker), width) {
			t.Fatal("original marker", marker, err)
		}
	}
	if n.Span().Start() != m[0].Source.Start() || n.Span().End() != m[1].Source.End() || m[0].Source.End() >= m[1].Source.Start() {
		t.Fatal("emphasis envelope", n)
	}
	var rawPayload, children strings.Builder
	previous := m[0].Source.End()
	for _, part := range n.Content() {
		raw, err := source.Text(part.Source)
		if err != nil || part.Source.Start() < previous || part.Source.End() > m[1].Source.Start() {
			t.Fatal("payload bounds", part, err)
		}
		want := strings.ReplaceAll(raw, "\x00", "\ufffd")
		if part.Transformation == parser.PartialTab {
			want = part.Semantic.Value
		} else if part.Transformation != parser.Exact && part.Transformation != parser.Normalized {
			t.Fatal("container payload decoded", part)
		}
		if part.Semantic != parser.Some(want) {
			t.Fatal("raw logical content", part)
		}
		rawPayload.WriteString(want)
		previous = part.Source.End()
	}
	previous = m[0].Source.End()
	for _, child := range n.Children() {
		if child.Span().Start() < previous || child.Span().End() > m[1].Source.Start() {
			t.Fatal("nested child order", child)
		}
		// Reconstruct raw logical payload, including opaque delimiters and break markers,
		// by intersecting the parent's independently source-backed Content ranges.
		for _, part := range n.Content() {
			a, b := max(child.Span().Start(), part.Source.Start()), min(child.Span().End(), part.Source.End())
			if a >= b {
				continue
			}
			span, _ := source.Span(a, b)
			raw, err := source.Text(span)
			if err != nil {
				t.Fatal(err)
			}
			if part.Transformation == parser.PartialTab {
				children.WriteString(part.Semantic.Value)
			} else {
				children.WriteString(strings.ReplaceAll(raw, "\x00", "\ufffd"))
			}
		}
		switch child.Kind() {
		case parser.Emphasis, parser.Strong:
			checkEmphasisProvenance(t, source, child)
		case parser.Text:
			checkEmphasisText(t, source, child)
		case parser.CodeSpan:
			checkCodeProvenance(t, source, child)
		case parser.InlineHTML:
			checkInlineHTMLProvenance(t, source, child)
		case parser.Link, parser.Image:
			checkLinkProvenance(t, source, child)
		case parser.HardBreak, parser.SoftBreak:
			checkBreakProperties(t, source, child, child.Properties().Break.Origin, byteRange{child.Span().Start(), child.Content()[0].Source.Start()})
		default:
			t.Fatal("unverified emphasis child", child.Kind())
		}
		previous = child.Span().End()
	}
	if rawPayload.String() != children.String() || len(n.Children()) == 0 {
		t.Fatal("children lost/duplicated original payload", rawPayload.String(), children.String())
	}
}

// Old block fixtures retain their own independent envelopes and logical ranges.
// Apply only the explicitly counted markers above to their expected leaf lists;
// this helper contains no flanking, delimiter search or production-parser oracle.
func fixturePairWants(t *testing.T, source parser.Source, wants []inlineWant) []inlineWant {
	t.Helper()
	for _, w := range wants {
		if w.kind == parser.Emphasis || w.kind == parser.Strong {
			return wants
		}
	}
	if len(wants) == 0 {
		return wants
	}
	cases, err := emphasisExamples()
	if err != nil {
		t.Fatal(err)
	}
	var marks []emphasisMark
	for _, e := range cases {
		if e.Markdown == string(source.Bytes()) {
			marks = emphasisMarks[e.ID]
			break
		}
	}
	var out []inlineWant
	remaining := append([]inlineWant(nil), wants...)
	take := func(end int) []inlineWant {
		var result []inlineWant
		for len(remaining) > 0 {
			w := remaining[0]
			if w.span.start >= end {
				break
			}
			if w.span.end <= end {
				result = append(result, w)
				remaining = remaining[1:]
				continue
			}
			if w.kind != parser.Text {
				t.Fatal("fixture cut opaque inline", w)
			}
			slice := func(a, b int) inlineWant {
				var ranges []byteRange
				var raw strings.Builder
				for _, r := range w.payload {
					lo, hi := max(a, r.start), min(b, r.end)
					if lo < hi {
						ranges = append(ranges, byteRange{lo, hi})
						raw.Write(source.Bytes()[lo:hi])
					}
				}
				literal := oracleText(raw.String(), entityOracle(t))
				if b == w.span.end {
					var whole strings.Builder
					for _, r := range w.payload {
						whole.Write(source.Bytes()[r.start:r.end])
					}
					if strings.TrimSuffix(oracleText(whole.String(), entityOracle(t)), " ") == w.literal {
						literal = strings.TrimSuffix(literal, " ")
					}
				}
				return inlineWant{kind: parser.Text, span: byteRange{a, b}, literal: literal, payload: ranges}
			}
			result = append(result, slice(w.span.start, end))
			remaining[0] = slice(end, w.span.end)
			break
		}
		return result
	}
	var rawRanges []byteRange
	for _, w := range wants {
		rawRanges = append(rawRanges, w.payload...)
		for _, r := range []byteRange{w.opening, w.closing} {
			if r.start < r.end {
				rawRanges = append(rawRanges, r)
			}
		}
	}
	sort.Slice(rawRanges, func(i, j int) bool { return rawRanges[i].start < rawRanges[j].start })
	var logical []byteRange
	for _, r := range rawRanges {
		if len(logical) > 0 && r.start <= logical[len(logical)-1].end {
			logical[len(logical)-1].end = max(logical[len(logical)-1].end, r.end)
		} else {
			logical = append(logical, r)
		}
	}
	var selected []emphasisMark
	for _, m := range marks {
		if m.open >= wants[0].span.start && m.close+m.width <= wants[len(wants)-1].span.end {
			selected = append(selected, m)
		}
	}
	// The independent marker table is pre-order. Recursion consumes just the
	// known fixture intervals; it performs no classification or pair selection.
	index := 0
	var build func(int) []inlineWant
	build = func(end int) []inlineWant {
		var result []inlineWant
		for index < len(selected) && selected[index].open < end {
			m := selected[index]
			index++
			result = append(result, take(m.open)...)
			take(m.open + m.width)
			children := build(m.close)
			take(m.close + m.width)
			kind := parser.Emphasis
			if m.width == 2 {
				kind = parser.Strong
			}
			var payload []byteRange
			var raw strings.Builder
			for _, r := range logical {
				a, b := max(m.open+m.width, r.start), min(m.close, r.end)
				if a < b {
					payload = append(payload, byteRange{a, b})
					raw.Write(source.Bytes()[a:b])
				}
			}
			result = append(result, inlineWant{kind: kind, span: byteRange{m.open, m.close + m.width},
				opening: byteRange{m.open, m.open + m.width}, closing: byteRange{m.close, m.close + m.width},
				literal: strings.ReplaceAll(raw.String(), "\x00", "\ufffd"), payload: payload, children: children})
		}
		return append(result, take(end)...)
	}
	out = build(wants[len(wants)-1].span.end)
	return append(out, remaining...)
}

func TestEmphasisMappedContentOwnershipAndOpacity(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, input := range []string{
			"> - **é\x00" + ending + ">   β**", "> *é\x00" + ending + "β*", "# **é\x00 β** #", "**é\x00 β**" + ending + "===",
			"*a &amp; `*` <i title='*'> <ab:*>*", "\\*literal\\* &#42;literal&#42; **real**",
		} {
			t.Run(strconv.Quote(input), func(t *testing.T) {
				source := parser.NewSource([]byte(input))
				r := parser.Parse(context.Background(), source)
				checkStagedTextOutcome(t, r, true)
				count := 0
				parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
					if n.Kind() != parser.Emphasis && n.Kind() != parser.Strong {
						return true
					}
					count++
					checkEmphasisProvenance(t, source, n)
					marker := "**"
					if n.Kind() == parser.Emphasis {
						marker = "*"
					}
					start := strings.Index(input, marker)
					if strings.HasPrefix(input, "\\*") {
						start = strings.Index(input, "**real")
					}
					end := strings.LastIndex(input, marker)
					if n.Span().Start() != start || n.Span().End() != end+len(marker) {
						t.Fatal("mapped original envelope", n.Span(), start, end)
					}
					if strings.HasPrefix(input, ">") {
						var payload strings.Builder
						for _, part := range n.Content() {
							payload.WriteString(part.Semantic.Value)
						}
						if payload.String() != "é\ufffd"+ending+"β" {
							t.Fatal("container prefixes entered logical payload", payload.String())
						}
					}
					if strings.HasPrefix(input, "> -") {
						parts := n.Content()
						if len(parts) < 3 || parts[0].Source.Start() != 6 || parts[len(parts)-1].Source.Start() != 13+len(ending) {
							t.Fatal("container gap/NUL mapping", parts)
						}
					}
					// Detach every mutable accessor layer, including opaque child handles.
					original := n.Children()
					children := n.Children()
					children[0] = nil
					m := n.Markers()
					m[0].Role = "mutated"
					p := n.Properties()
					p.Delimiter.Width = 9
					content := n.Content()
					content[0].Semantic = parser.Some("changed")
					*original[0] = parser.Node{}
					return true
				})
				if count != 1 || string(source.Bytes()) != input || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
					t.Fatal("state/ownership", r, count)
				}
			})
		}
	}
	for _, input := range []string{"*open\n\nclose*", "- *open\n- close*", "    *code*", "~~~\n**code**\n~~~", "<!-- *raw* -->"} {
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
		parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
			if n.Kind() == parser.Emphasis || n.Kind() == parser.Strong {
				t.Fatal("opaque/block-crossing pair", input)
			}
			return true
		})
	}
}

func TestEmphasisManyFlatPairsAndUnmatchedRuns(t *testing.T) {
	for _, count := range []int{64, 512, 4096} {
		input := "before " + strings.Repeat("*x* **y** ", count) + "after"
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
		nodes := r.Document.Root().Children()[0].Children()
		pairs := 0
		for _, n := range nodes {
			if n.Kind() != parser.Emphasis && n.Kind() != parser.Strong {
				continue
			}
			group, within := pairs/2, 0
			if pairs%2 == 1 {
				within = 4
			}
			width := 1
			if pairs%2 == 1 {
				width = 2
			}
			start := len("before ") + group*10 + within
			if n.Span().Start() != start || n.Span().End() != start+2*width+1 {
				t.Fatal("flat pair source order", n.Span(), start)
			}
			checkEmphasisProvenance(t, r.Source, n)
			pairs++
		}
		if pairs != 2*count {
			t.Fatal("flat pair count", pairs, count)
		}
		// Many incompatible marker classes and a long unmatched maximal run.
		for _, text := range []string{strings.Repeat("a_b_c * ", count), "a " + strings.Repeat("*", count) + " ", strings.Repeat("a**b_c* ", count)} {
			parsed := parser.Parse(context.Background(), parser.NewSource([]byte(text)))
			checkStagedTextOutcome(t, parsed, true)
			parser.Walk(parsed.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() == parser.Text {
					checkEmphasisText(t, parsed.Source, n)
				}
				return true
			})
		}
	}
}

// Every original logical byte must belong to exactly one ordered direct child.
// Envelopes may cross stripped prefix gaps, but may not overlap or omit payload.
func checkInlineSourceCoverage(t *testing.T, block *parser.Node) {
	t.Helper()
	children := block.Children()
	previous := -1
	for _, n := range children {
		if n.Span().Start() < previous {
			t.Fatal("overlapping inline envelopes", n)
		}
		previous = n.Span().End()
	}
	index := 0
	for _, part := range block.Content() {
		for offset := part.Source.Start(); offset < part.Source.End(); offset++ {
			for index < len(children) && children[index].Span().End() <= offset {
				index++
			}
			if index == len(children) || children[index].Span().Start() > offset {
				t.Fatal("unowned original logical byte", offset)
			}
		}
	}
}
