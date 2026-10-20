package parser_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

func breakWant(start, ending, end int, origin, raw string) inlineWant {
	kind := parser.SoftBreak
	if origin != "line-ending" {
		kind = parser.HardBreak
	}
	return inlineWant{kind: kind, span: byteRange{start, end}, opening: byteRange{start, ending},
		literal: raw, payload: []byteRange{{ending, end}}, origin: origin}
}

func checkBreakProperties(t *testing.T, source parser.Source, n *parser.Node, origin string, marker byteRange) {
	t.Helper()
	p, m := n.Properties(), n.Markers()
	if p.Break == nil || p.Break.Origin != origin || p.Literal.Present || p.Delimiter != nil || len(n.Children()) != 0 {
		t.Fatal("incorrect break properties", p)
	}
	if origin == "line-ending" {
		if n.Kind() != parser.SoftBreak || len(m) != 0 {
			t.Fatal("soft break acquired a marker", n)
		}
	} else {
		if n.Kind() != parser.HardBreak || len(m) != 1 || m[0].Role != "hard-break-marker" || m[0].Source.Start() != marker.start || m[0].Source.End() != marker.end {
			t.Fatal("hard-break marker range", m, marker)
		}
		raw, err := source.Text(m[0].Source)
		if err != nil || (origin == "backslash" && raw != "\\") || (origin == "spaces" && (len(raw) < 2 || strings.Trim(raw, " ") != "")) {
			t.Fatal("invented hard-break spelling", raw, err)
		}
	}
	var raw strings.Builder
	for _, part := range n.Content() {
		text, err := source.Text(part.Source)
		if err != nil || part.Transformation != parser.Exact || part.Semantic != parser.Some(text) {
			t.Fatal("break lost its original ending", part, err)
		}
		raw.WriteString(text)
	}
	if s := raw.String(); s != "\n" && s != "\r\n" && s != "\r" {
		t.Fatal("not one physical ending", s)
	}
}

var fixtureEndings = regexp.MustCompile("\r\n|\r|\n")

func oracleNormalizedText(t *testing.T, source parser.Source, part parser.Segment) string {
	t.Helper()
	raw, err := source.Text(part.Source)
	if err != nil {
		t.Fatal(err)
	}
	if raw == " " && part.Semantic == parser.Some("") {
		bytes := source.Bytes()
		end := part.Source.End()
		if end >= len(bytes) || (bytes[end] != '\r' && bytes[end] != '\n') {
			t.Fatal("trimmed space was not before a physical ending", part)
		}
		return ""
	}
	return strings.ReplaceAll(raw, "\x00", "\ufffd")
}

// Advance old block fixture expectations from their independent original-byte
// ranges, not from the parser's new inline nodes. Regex-selected physical endings
// and a trailing-backslash parity oracle are separate from production dispatch.
func fixtureBreakInlines(t *testing.T, source parser.Source, ranges []byteRange) []inlineWant {
	t.Helper()
	var raw []byte
	var offsets []int
	bytes := source.Bytes()
	for _, r := range ranges {
		raw = append(raw, bytes[r.start:r.end]...)
		for i := r.start; i < r.end; i++ {
			offsets = append(offsets, i)
		}
	}
	mapped := func(a, b int) []byteRange {
		var out []byteRange
		for i := a; i < b; i++ {
			if len(out) == 0 || out[len(out)-1].end != offsets[i] {
				out = append(out, byteRange{offsets[i], offsets[i] + 1})
			} else {
				out[len(out)-1].end++
			}
		}
		return out
	}
	var wants []inlineWant
	start := 0
	text := func(end int, trim bool) {
		if end == start {
			return
		}
		payload := mapped(start, end)
		literal := string(raw[start:end])
		if trim {
			literal = strings.TrimSuffix(literal, " ")
		}
		wants = append(wants, inlineWant{kind: parser.Text, span: byteRange{payload[0].start, payload[len(payload)-1].end},
			literal: oracleText(literal, entityOracle(t)), payload: payload})
	}
	for _, match := range fixtureEndings.FindAllIndex(raw, -1) {
		a, b := match[0], match[1]
		marker, origin := a, "line-ending"
		for marker > start && raw[marker-1] == ' ' {
			marker--
		}
		if a-marker >= 2 {
			origin = "spaces"
		} else {
			marker = a
			slashes := 0
			for i := a - 1; i >= start && raw[i] == '\\'; i-- {
				slashes++
			}
			if slashes%2 != 0 {
				marker, origin = a-1, "backslash"
			}
		}
		text(marker, origin == "line-ending" && a > start && raw[a-1] == ' ')
		wants = append(wants, breakWant(offsets[marker], offsets[a], offsets[b-1]+1, origin, string(raw[a:b])))
		start = b
	}
	text(len(raw), false)
	return wants
}

func TestBreakOriginsMappingAndCoordinates(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, tc := range []struct {
			spelling, literal, origin string
			marker                    int
		}{
			{"", "é\ufffd", "line-ending", 0},
			{" ", "é\ufffd", "line-ending", 0},
			{"  ", "é\ufffd", "spaces", 2},
			{"       ", "é\ufffd", "spaces", 7},
			{"\\", "é\ufffd", "backslash", 1},
			{"\\\\", "é\ufffd\\", "line-ending", 0},
			{"\\\\\\", "é\ufffd\\", "backslash", 1},
			{"\t ", "é\ufffd\t", "line-ending", 0},
			{"\t  ", "é\ufffd\t", "spaces", 2},
			{"  \t", "é\ufffd  \t", "line-ending", 0},
			{"&#32;&#32;", "é\ufffd  ", "line-ending", 0},
			{"&#92;", "é\ufffd\\", "line-ending", 0},
			{"&#10;", "é\ufffd\n", "line-ending", 0},
		} {
			t.Run(strconv.Quote(tc.spelling+ending), func(t *testing.T) {
				input := "> - é\x00" + tc.spelling + ending + ">   β"
				source := parser.NewSource([]byte(input))
				r := parser.Parse(context.Background(), source)
				checkStagedTextOutcome(t, r, true)
				var leaf *parser.Node
				parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
					if n.Kind() == parser.Paragraph {
						leaf = n
					}
					return true
				})
				endingStart := 7 + len(tc.spelling)
				textEnd := endingStart - tc.marker
				w := textWant(4, textEnd, tc.literal)
				checkInlineNodes(t, source, leaf.Children(), []inlineWant{w,
					breakWant(textEnd, endingStart, endingStart+len(ending), tc.origin, ending),
					textWant(endingStart+len(ending)+4, len(input), "β")})
				index := parser.NewLineIndex(source)
				before, err := index.Position(endingStart)
				if err != nil || before.Line != 1 || before.Column != 7+len(tc.spelling) {
					t.Fatal("ending coordinate is not an original scalar position", before, err)
				}
				after, err := index.Position(endingStart + len(ending))
				if err != nil || after.Line != 2 || after.Column != 1 {
					t.Fatal("complete ending coordinate", after, err)
				}
				br := leaf.Children()[1]
				br.Content()[0].Semantic = parser.Some("mutated")
				br.Properties().Break.Origin = "mutated"
				if tc.marker > 0 {
					br.Markers()[0].Role = "mutated"
				}
				*br = parser.Node{}
				if string(source.Bytes()) != input || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
					t.Fatal("break accessor aliasing, mutation or call-state leakage")
				}
			})
		}
	}
}

func TestBreakUpstreamAcceptance(t *testing.T) {
	data, err := os.ReadFile("testdata/commonmark/0.31.2/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples []struct {
		Example        int
		Markdown, HTML string
	}
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	wants := map[int][]inlineWant{
		633: {textWant(0, 3, "foo"), breakWant(3, 5, 6, "spaces", "\n"), textWant(6, 9, "baz")},
		634: {textWant(0, 3, "foo"), breakWant(3, 4, 5, "backslash", "\n"), textWant(5, 8, "baz")},
		635: {textWant(0, 3, "foo"), breakWant(3, 10, 11, "spaces", "\n"), textWant(11, 14, "baz")},
		636: {textWant(0, 3, "foo"), breakWant(3, 5, 6, "spaces", "\n"), textWant(11, 14, "bar")},
		637: {textWant(0, 3, "foo"), breakWant(3, 4, 5, "backslash", "\n"), textWant(10, 13, "bar")},
		644: {textWant(0, 4, "foo\\")}, 645: {textWant(0, 3, "foo")},
		646: {textWant(4, 8, "foo\\")}, 647: {textWant(4, 7, "foo")},
		648: {textWant(0, 3, "foo"), breakWant(3, 3, 4, "line-ending", "\n"), textWant(4, 7, "baz")},
		649: {textWant(0, 4, "foo"), breakWant(4, 4, 5, "line-ending", "\n"), textWant(6, 9, "baz")},
	}
	seen := 0
	for _, e := range examples {
		if e.Example < 633 || e.Example > 649 {
			continue
		}
		seen++
		t.Run(strconv.Itoa(e.Example), func(t *testing.T) {
			r := parser.Parse(context.Background(), parser.NewSource([]byte(e.Markdown)))
			pending := e.Example == 638 || e.Example == 639 || e.Example == 642 || e.Example == 643
			checkStagedTextOutcome(t, r, !pending)
			out := conformance.Evaluate(r)
			if pending {
				if r.Status != parser.Incomplete || out.Status != "pending" || out.HTML != "" {
					t.Fatal("missing emphasis/HTML provider became complete", out)
				}
			} else if out.Status != "rendered" || out.HTML != e.HTML {
				t.Fatal("upstream output", out, e.HTML)
			}
			if w, ok := wants[e.Example]; ok {
				checkInlineNodes(t, r.Source, r.Document.Root().Children()[0].Children(), w)
			}
			if string(r.Source.Bytes()) != e.Markdown {
				t.Fatal("source mutation")
			}
		})
	}
	if seen != 17 {
		t.Fatal("missing upstream identity", seen)
	}
}

func TestBreakBlockBoundariesAndOpaqueContexts(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, input := range []string{
			"foo\\", "foo  ", "foo\\" + ending, "foo  " + ending,
			"### foo\\" + ending, "### foo  " + ending,
			"foo\\" + ending + "===" + ending,
			"foo\\" + ending + ending + "bar",
			"    code  " + ending + "    code\\" + ending,
			"~~~" + ending + "code  " + ending + "code\\" + ending + "~~~",
			"<!-- a  " + ending + "b\\" + ending + "c -->",
			"`code  " + ending + "span`",
		} {
			r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
			checkStagedTextOutcome(t, r, true)
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() == parser.SoftBreak || n.Kind() == parser.HardBreak {
					t.Fatal("terminal/opaque source acquired a break", input, n)
				}
				return true
			})
		}
		for _, prefix := range []string{"", "> ", "> - "} {
			continuation := prefix
			if prefix == "> - " {
				continuation = ">   "
			}
			input := prefix + "a  " + ending + continuation + "b" + ending + continuation + "==="
			r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
			checkStagedTextOutcome(t, r, true)
			var headings, breaks int
			parser.Walk(r.Document.Root(), func(n *parser.Node) bool {
				if n.Kind() == parser.Heading {
					headings++
					checkInlineNodes(t, r.Source, n.Children(), []inlineWant{
						textWant(len(prefix), len(prefix)+1, "a"),
						breakWant(len(prefix)+1, len(prefix)+3, len(prefix)+3+len(ending), "spaces", ending),
						textWant(len(prefix)+3+len(ending)+len(continuation), len(prefix)+4+len(ending)+len(continuation), "b"),
					})
				}
				if n.Kind() == parser.HardBreak {
					breaks++
				}
				return true
			})
			if headings != 1 || breaks != 1 {
				t.Fatal("multiline heading lost its break", input, r)
			}
		}
		// Lazy continuation drops no real source bytes and adds no synthetic prefix.
		input := "> a\\" + ending + "b"
		r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
		checkStagedTextOutcome(t, r, true)
		leaf := r.Document.Root().Children()[0].Children()[0]
		checkInlineNodes(t, r.Source, leaf.Children(), []inlineWant{textWant(2, 3, "a"),
			breakWant(3, 4, 4+len(ending), "backslash", ending), textWant(4+len(ending), len(input), "b")})
	}
}
