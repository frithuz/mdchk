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

func parseTableStage(t *testing.T, input string) ParseResult {
	t.Helper()
	r := parseWithProfile(context.Background(), NewSource([]byte(input)), budgetFor(t, DefaultLimits()), gfmInProgressProfile)
	requireTableComplete(t, r, input)
	return r
}
func requireTableComplete(t *testing.T, r ParseResult, input string) {
	t.Helper()
	if r.Status != Complete || r.AST != Available || r.Failure != nil || r.Phases != (PhaseProgress{PhaseComplete, PhaseComplete}) || len(r.Uncertainty) != 0 || r.Document == nil || r.Document.references.Availability != Available || r.Document.references.LookupAvailability != Available || string(r.Source.Bytes()) != input || r.Document.Source() != r.Source {
		t.Fatalf("not complete/source intact: %+v", r)
	}
}
func tableNodes(root *Node, kind Kind) []*Node {
	var out []*Node
	Walk(root, func(n *Node) bool {
		if n.Kind() == kind {
			out = append(out, n)
		}
		return true
	})
	return out
}
func tableText(n *Node) string {
	if n.spec.Properties.Literal.Present {
		return n.spec.Properties.Literal.Value
	}
	var b strings.Builder
	for _, child := range n.spec.Children {
		b.WriteString(tableText(child))
	}
	return b.String()
}
func tableRaw(t *testing.T, source Source, span Span) string {
	t.Helper()
	raw, err := source.Text(span)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Original fixtures supply only immutable inputs. Every expected semantic row,
// source count, following block and alignment below is independently authored.
// This executes a private syntax stage, not the future full-profile adapter.
func TestTablePublished198To205(t *testing.T) {
	data, err := os.ReadFile("testdata/gfm/0.29-gfm/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var originals []struct {
		Example  int
		Markdown string
	}
	if err := json.Unmarshal(data, &originals); err != nil {
		t.Fatal(err)
	}
	type want struct {
		width     int
		cells     [][]string
		alignment []TableAlignment
		after     Kind
	}
	expected := map[int]want{
		198: {2, [][]string{{"foo", "bar"}, {"baz", "bim"}}, []TableAlignment{AlignNone, AlignNone}, ""},
		199: {2, [][]string{{"abc", "defghi"}, {"bar", "baz"}}, []TableAlignment{AlignCenter, AlignRight}, ""},
		200: {1, [][]string{{"f|oo"}, {"b | az"}, {"b | im"}}, []TableAlignment{AlignNone}, ""},
		201: {2, [][]string{{"abc", "def"}, {"bar", "baz"}}, []TableAlignment{AlignNone, AlignNone}, BlockQuote},
		202: {2, [][]string{{"abc", "def"}, {"bar", "baz"}, {"bar"}}, []TableAlignment{AlignNone, AlignNone}, Paragraph},
		203: {},
		204: {2, [][]string{{"abc", "def"}, {"bar"}, {"bar", "baz", "boo"}}, []TableAlignment{AlignNone, AlignNone}, ""},
		205: {2, [][]string{{"abc", "def"}}, []TableAlignment{AlignNone, AlignNone}, ""},
	}
	seen := 0
	for _, original := range originals {
		w, ok := expected[original.Example]
		if !ok {
			continue
		}
		seen++
		t.Run(fmt.Sprint(original.Example), func(t *testing.T) {
			r := parseTableStage(t, original.Markdown)
			tables := tableNodes(r.Document.root, Table)
			if w.width == 0 {
				if len(tables) != 0 || len(r.Document.root.spec.Children) != 1 || r.Document.root.spec.Children[0].Kind() != Paragraph || r.Document.root.spec.Children[0].Span().End() != len(original.Markdown) {
					t.Fatal("mismatch lost ordinary paragraph")
				}
				core := Parse(context.Background(), r.Source)
				if !reflect.DeepEqual(core.Document.root, r.Document.root) {
					t.Fatal("mismatch changed core interpretation")
				}
				return
			}
			if len(tables) != 1 {
				t.Fatalf("tables=%d result=%+v", len(tables), r)
			}
			table := tables[0]
			if table.spec.Properties.Table.Width != w.width || !reflect.DeepEqual(table.spec.Properties.Table.Alignments, w.alignment) {
				t.Fatal(table.Properties())
			}
			rows := table.spec.Children
			if len(rows) != len(w.cells)+1 {
				t.Fatal("row count", len(rows))
			}
			logical := 0
			for i, row := range rows {
				role := "body"
				if i == 0 {
					role = "header"
				}
				if i == 1 {
					role = "delimiter"
				}
				props := row.spec.Properties.Row
				if props.Role != role || props.RawCells != len(row.spec.Children) {
					t.Fatal("row role/count", props)
				}
				if i == 1 {
					continue
				}
				if props.Padding != max(0, w.width-len(w.cells[logical])) || len(row.spec.Children) != len(w.cells[logical]) {
					t.Fatal("source cell count/padding", props)
				}
				for j, cell := range row.spec.Children {
					if tableText(cell) != w.cells[logical][j] {
						t.Fatalf("row%d cell%d got %q want %q", i, j, tableText(cell), w.cells[logical][j])
					}
				}
				logical++
			}
			top := r.Document.root.spec.Children
			if w.after != "" {
				if len(top) != 2 || top[1].Kind() != w.after {
					t.Fatal("following block ownership", top)
				}
			} else if len(top) != 1 {
				t.Fatal("unexpected following block")
			}
			// Every source descendant and property belongs to the original snapshot.
			Walk(table, func(n *Node) bool {
				if n.Span().source != r.Source {
					t.Fatal("rewritten source")
				}
				for _, s := range n.Content() {
					if s.Source.source != r.Source {
						t.Fatal("foreign payload")
					}
				}
				return true
			})
			if original.Example == 198 {
				for i, row := range rows {
					if row.Span().Start() != i*14 || row.Span().End() != (i+1)*14 || len(row.spec.Properties.Row.Separators) != 3 {
						t.Fatal("published198 row/separators", row.Span())
					}
				}
				if tableRaw(t, r.Source, rows[0].spec.Children[0].Span()) != " foo " {
					t.Fatal("lost raw whitespace")
				}
			}
			if original.Example == 199 {
				markers := rows[1].spec.Children[0].Markers()
				for i, w := range []struct {
					role string
					a, b int
				}{{"left-colon", 17, 18}, {"dash-run", 18, 19}, {"right-colon", 19, 20}} {
					if markers[i].Role != w.role || markers[i].Source.Start() != w.a || markers[i].Source.End() != w.b {
						t.Fatal("precise alignment markers", markers)
					}
				}
				if rows[1].spec.Properties.Row.LeadingPipe.Present() || rows[2].spec.Properties.Row.TrailingPipe.Present() {
					t.Fatal("invented outer pipes")
				}
			}
			if original.Example == 200 {
				code := tableNodes(table, CodeSpan)
				strong := tableNodes(table, Strong)
				if len(code) != 1 || code[0].Span().Start() != 26 || code[0].Span().End() != 30 || tableRaw(t, r.Source, code[0].Content()[0].Source) != "\\|" || code[0].spec.Properties.Literal != Some("|") || len(strong) != 1 || strong[0].Span().Start() != 40 || strong[0].Span().End() != 46 || tableText(strong[0]) != "|" {
					t.Fatal("escaped code/strong source")
				}
				core := Parse(context.Background(), r.Source)
				codes := tableNodes(core.Document.root, CodeSpan)
				if len(codes) != 1 || codes[0].spec.Properties.Literal != Some("\\|") {
					t.Fatal("table code semantics leaked into core")
				}
			}
			if original.Example == 201 && table.Span().End() != 42 {
				t.Fatal("swallowed quote")
			}
			if original.Example == 202 {
				row := rows[3]
				if row.Span().Start() != 42 || row.Span().End() != 46 || row.spec.Properties.Row.RawCells != 1 || row.spec.Properties.Row.Padding != 1 {
					t.Fatal("pipe-free body row missing")
				}
			}
			if original.Example == 204 {
				short, long := rows[2], rows[3]
				if short.Span().Start() != 28 || short.Span().End() != 36 || short.spec.Children[0].Span().Start() != 29 || short.spec.Children[0].Span().End() != 34 || short.spec.Properties.Row.Padding != 1 {
					t.Fatal("short original row")
				}
				excess := long.spec.Children[2]
				if long.Span().Start() != 36 || long.Span().End() != 56 || excess.Span().Start() != 49 || excess.Span().End() != 54 || tableRaw(t, r.Source, excess.Span()) != " boo " {
					t.Fatal("lost excess source")
				}
				for i, pipe := range long.spec.Properties.Row.Separators {
					if pipe.Start() != []int{36, 42, 48, 54}[i] || pipe.End() != pipe.Start()+1 {
						t.Fatal("actual pipe ranges")
					}
				}
			}
			// Public entry points and the private zero profile remain exactly core-only,
			// including after a staged parse; no ambient or retained profile state.
			public := Parse(context.Background(), r.Source)
			for _, disabled := range []ParseResult{ParseWithBudget(context.Background(), r.Source, budgetFor(t, DefaultLimits())), parseWithProfile(context.Background(), r.Source, budgetFor(t, DefaultLimits()), coreProfile)} {
				requireTableComplete(t, disabled, original.Markdown)
				if len(tableNodes(disabled.Document.root, Table)) != 0 || !reflect.DeepEqual(disabled.Document.root, public.Document.root) {
					t.Fatal("disabled isolation")
				}
			}
		})
	}
	if seen != 8 {
		t.Fatal("missing published cases", seen)
	}
}

func TestTablePipeSplittingAndSharedInline(t *testing.T) {
	for _, tc := range []struct {
		body  string
		count int
		code  string
	}{
		{"| `a|b` | c |", 3, ""}, {"| `a\\|b` | c |", 2, "a|b"},
		{"| **a|b** | c |", 3, ""}, {"| **a\\|b** | c |", 2, ""},
		{"| &#124; | c |", 2, ""}, {"| a\\| | c |", 2, ""},
	} {
		t.Run(tc.body, func(t *testing.T) {
			r := parseTableStage(t, "| a | b |\n| - | - |\n"+tc.body+"\n")
			row := tableNodes(r.Document.root, TableRow)[2]
			if len(row.spec.Children) != tc.count {
				t.Fatal("split after inline recognition", len(row.spec.Children))
			}
			codes := tableNodes(row, CodeSpan)
			if tc.code != "" {
				if len(codes) != 1 || tableText(codes[0]) != tc.code {
					t.Fatal("code pipe semantics")
				}
			} else if len(codes) != 0 {
				t.Fatal("code crossed raw separator")
			}
		})
	}
	input := "| h | h |\n| - | - |\n| *[a\\|b][r]* &amp;copy; | ![i](u\\|v) | `x\\|y` <!-- c --> |\n\n[r]: /dest \"title\"\n"
	source := NewSource([]byte(input))
	op := budgetFor(t, DefaultLimits())
	r := parseWithProfile(context.Background(), source, op, gfmInProgressProfile)
	requireTableComplete(t, r, input)
	links, images := tableNodes(r.Document.root, Link), tableNodes(r.Document.root, Image)
	if len(links) != 1 || len(images) != 1 || tableText(links[0]) != "a|b" || links[0].spec.Properties.Link.Destination.Semantic != Some("/dest") || !referenceProjection(links[0].spec.Properties.Link.Destination) || images[0].spec.Properties.Link.Destination.Semantic != Some("u|v") {
		t.Fatalf("shared link/reference decoding links=%d images=%d link=%+v image=%+v text=%q", len(links), len(images), links[0].Properties().Link, images[0].Properties().Link, tableText(links[0]))
	}
	row := tableNodes(r.Document.root, TableRow)[2]
	if len(row.spec.Children) != 3 || !strings.Contains(tableText(row.spec.Children[0]), "&copy;") {
		t.Fatal("excess or one-pass entity semantics")
	}
	refs := r.Document.references
	if len(refs.Definitions) != 1 || tableRaw(t, source, refs.Definitions[0].spec.Destination.Segments[0].Source) != "/dest" {
		t.Fatal("reference provenance")
	}
	literal, err := r.Document.LiteralSpans(context.Background(), op)
	if err != nil {
		t.Fatal(err)
	}
	var raw strings.Builder
	for _, s := range literal {
		raw.WriteString(tableRaw(t, source, s))
	}
	if !strings.Contains(raw.String(), "x\\|y") || !strings.Contains(raw.String(), "<!-- c -->") {
		t.Fatal("excess literal protection missing", raw.String())
	}
	protected, err := r.Document.ProtectedNodes(context.Background(), op)
	if err != nil || len(protected) != 2 {
		t.Fatal("protection traversal", len(protected), err)
	}
	comments, err := r.Document.Comments(context.Background(), op)
	if err != nil || len(comments) != 1 {
		t.Fatal("comment traversal", comments, err)
	}
	if _, err := r.Document.ProtectedNodes(context.Background(), budgetFor(t, DefaultLimits())); err == nil {
		t.Fatal("foreign operation acquired authority")
	}
}

func TestTableBoundariesContainersAndFallback(t *testing.T) {
	for name, input := range map[string]string{
		"quote":      "> | a |\n> | - |\n> | é |\n",
		"list":       "- | a |\n  | - |\n  | é |\n",
		"list-quote": "- > | a |\n  > | - |\n  > | é |\n",
	} {
		t.Run(name, func(t *testing.T) {
			r := parseTableStage(t, input)
			tables := tableNodes(r.Document.root, Table)
			if len(tables) != 1 || tableText(tables[0]) != "aé" {
				t.Fatal("nested table")
			}
			if name == "quote" {
				table := tables[0]
				if table.Span().Start() != 2 || table.Span().End() != 25 {
					t.Fatal("table envelope", table.Span())
				}
				for i, row := range table.spec.Children {
					if row.Span().Start() != []int{2, 10, 18}[i] || row.Span().End() != []int{8, 16, 25}[i] {
						t.Fatal("logical row/prefix ownership", row.Span())
					}
				}
				cell := table.spec.Children[2].spec.Children[0]
				if cell.Span().Start() != 19 || cell.Span().End() != 23 || tableRaw(t, r.Source, cell.Span()) != " é " {
					t.Fatal("UTF8 raw cell")
				}
			}
		})
	}
	for _, suffix := range []struct {
		text string
		kind Kind
	}{{"\nnext\n", Paragraph}, {"> quote\n", BlockQuote}, {"# heading\n", Heading}, {"```\nx\n```\n", CodeBlock}, {"<div>\nx\n", HTMLBlock}, {"- item\n", List}, {"2. item\n", List}, {"---\n", ThematicBreak}} {
		t.Run(suffix.text, func(t *testing.T) {
			r := parseTableStage(t, "| a |\n| - |\n"+suffix.text)
			top := r.Document.root.spec.Children
			if len(top) != 2 || top[0].Kind() != Table || top[0].Span().End() != 12 || top[1].Kind() != suffix.kind {
				t.Fatal("following block swallowed", top)
			}
		})
	}
	for _, input := range []string{"> | a |\n> | - |\noutside\n", "- | a |\n  | - |\noutside\n", "> > | a |\n> > | - |\n> outside\n"} {
		r := parseTableStage(t, input)
		table := tableNodes(r.Document.root, Table)
		if len(table) != 1 || len(table[0].spec.Children) != 2 || strings.Contains(tableText(table[0]), "outside") || len(tableNodes(r.Document.root, Paragraph)) != 1 {
			t.Fatal("table borrowed lazy container continuation")
		}
	}
	for _, input := range []string{
		"| a | b |\n| - |\n| c |\n", "| a |\n| - | - |\n", "| a |\n| : |\n", "| a |\n| - x |\n", "| a |\n| - - |\n", "| a |\n| -:- |\n", "| a | b |\n| - \\| - |\n",
		"| a |\n\n| - |\n", "[r]: u\n| - |\n", "[r]:\n|-|\n", "[a|\n|a|\n|-|\n]: /u\n", "ordinary\n| a |\n| - |\n", "> | a |\n| - |\n",
		"```\n| a |\n| - |\n```\n", "    | a |\n    | - |\n", "<div>\n| a |\n| - |\n", "<!--\n| a |\n| - |\n-->\n",
	} {
		t.Run(input, func(t *testing.T) {
			r := parseTableStage(t, input)
			core := Parse(context.Background(), r.Source)
			if len(tableNodes(r.Document.root, Table)) != 0 || !reflect.DeepEqual(r.Document.root, core.Document.root) {
				t.Fatal("invalid/protected candidate changed core structure")
			}
		})
	}
	r := parseTableStage(t, "[r]: /u\n| a |\n| - |\n")
	if len(tableNodes(r.Document.root, Table)) != 1 || len(r.Document.references.Definitions) != 1 {
		t.Fatal("leading definition extraction")
	}
}

func TestTableRawFieldsEndingsAndImmutability(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r", ""} {
		for _, row := range []string{"a|b", "|a|b", "a|b|", "|a|b|", "|a||", "||b|", "a||b", "| a\\| | b |"} {
			input := "| h | h |\n| :- | -: |\n" + row + ending
			r := parseTableStage(t, input)
			table := tableNodes(r.Document.root, Table)[0]
			body := table.spec.Children[2]
			if body.Span().End() != len(input) {
				t.Fatal("row lost ending")
			}
			for _, cell := range body.spec.Children {
				if strings.ContainsAny(tableRaw(t, r.Source, cell.Span()), "\r\n") {
					t.Fatal("ending in cell")
				}
			}
			if ending == "\r\n" {
				index := NewLineIndex(r.Source)
				if _, err := index.Position(len(input) - 1); err == nil {
					t.Fatal("CRLF interior accepted")
				}
			}
		}
	}
	r := parseTableStage(t, "|h|h|\n|-|-|\n||é\t|\n")
	row := tableNodes(r.Document.root, TableRow)[2]
	if !row.spec.Children[0].Span().Present() || row.spec.Children[0].Span().Start() != row.spec.Children[0].Span().End() || row.spec.Properties.Row.Padding != 0 {
		t.Fatal("empty source field confused with padding")
	}
	unicodeCell := row.spec.Children[1]
	index := NewLineIndex(r.Source)
	pos, err := index.Position(unicodeCell.Span().End())
	if err != nil || pos.Column != 5 {
		t.Fatal("Unicode scalar/tab1 column", pos, err)
	}
	if _, err := index.Position(unicodeCell.Span().Start() + 1); err == nil {
		t.Fatal("UTF8 interior accepted")
	}
	bytes := []byte("|a\\|b|\n|:-:|\n|c|\n")
	source := NewSource(bytes)
	op := budgetFor(t, DefaultLimits())
	original := string(bytes)
	bytes[1] = 'z'
	r = parseWithProfile(context.Background(), source, op, gfmInProgressProfile)
	requireTableComplete(t, r, original)
	table := tableNodes(r.Document.root, Table)[0]
	before := table.Properties()
	copyProps := table.Properties()
	copyProps.Table.Alignments[0] = AlignNone
	copyProps.Table.Width = 999
	row = table.Children()[0]
	props := row.Properties()
	props.Row.Separators[0] = Span{}
	props.Row.Role = "changed"
	children := row.Children()
	childProps := children[0].Properties()
	childProps.Cell.EscapedPipes[0] = Span{}
	*children[0] = Node{}
	markers := table.Children()[1].Children()[0].Markers()
	markers[0].Source = Span{}
	content := row.Children()[0].Content()
	content[0].Semantic = Some("changed")
	returnedBytes := source.Bytes()
	returnedBytes[0] = 'x'
	if !reflect.DeepEqual(table.Properties(), before) || row.Properties().Row.Role != "header" || !row.Properties().Row.Separators[0].Present() || !row.Children()[0].Properties().Cell.EscapedPipes[0].Present() || string(source.Bytes()) != original {
		t.Fatal("mutable model/source escaped")
	}
	foreign := NewSource([]byte(original))
	if _, err := foreign.Text(table.Span()); err == nil {
		t.Fatal("foreign identical snapshot read")
	}
	spec := table.spec
	spec.Envelope = Span{source: foreign, start: 0, end: foreign.Len(), present: true}
	if _, err := NewNode(spec); err == nil {
		t.Fatal("foreign child admitted")
	}
	// Constructor snapshots nested property input as well as accessor output.
	spec = table.spec
	spec.Properties = table.Properties()
	node, err := NewNode(spec)
	if err != nil {
		t.Fatal(err)
	}
	spec.Properties.Table.Alignments[0] = AlignRight
	if node.Properties().Table.Alignments[0] != AlignCenter {
		t.Fatal("retained constructor input")
	}
}

func TestTableModelRejectsInvalidFacts(t *testing.T) {
	r := parseTableStage(t, "|a|b|\n|:-|-:|\n|c|\n")
	table := tableNodes(r.Document.root, Table)[0]
	for _, mutate := range []func(*NodeSpec){
		func(s *NodeSpec) { s.Properties.Table.Width = 0 },
		func(s *NodeSpec) { s.Properties.Table.Alignments = s.Properties.Table.Alignments[:1] },
		func(s *NodeSpec) { s.Properties.Table.Alignments[0] = "unknown" },
		func(s *NodeSpec) { s.Children[0], s.Children[1] = s.Children[1], s.Children[0] },
	} {
		spec := table.spec
		spec.Properties = table.Properties()
		spec.Children = table.Children()
		mutate(&spec)
		if _, err := NewNode(spec); err == nil {
			t.Fatal("invalid table admitted")
		}
	}
	row := table.spec.Children[2]
	for _, mutate := range []func(*NodeSpec){
		func(s *NodeSpec) { s.Properties.Row.RawCells = -1 },
		func(s *NodeSpec) { s.Properties.Row.Padding = -1 },
		func(s *NodeSpec) { s.Properties.Row.Role = "delimiter" },
		func(s *NodeSpec) { s.Properties.Row.Separators[0] = Span{} },
		func(s *NodeSpec) {
			s.Properties.Row.LeadingPipe = Span{source: r.Source, start: 1, end: 2, present: true}
		},
	} {
		spec := row.spec
		spec.Properties = row.Properties()
		mutate(&spec)
		if _, err := NewNode(spec); err == nil {
			t.Fatal("invalid row admitted")
		}
	}
}

func TestTableResourceBudgets(t *testing.T) {
	input := "> | **a** | b |\n> | :- | -: |\n> | `x\\|y` |\n"
	source := NewSource([]byte(input))
	baseline := budgetFor(t, DefaultLimits())
	r := parseWithProfile(context.Background(), source, baseline, gfmInProgressProfile)
	requireTableComplete(t, r, input)
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
	for _, input := range []string{
		"|" + strings.Repeat("a|", 2000) + "\n|" + strings.Repeat("-|", 2000) + "\n",
		"|a|b|\n|-|-|\n" + strings.Repeat("|x|\n", 2000),
		strings.Repeat("|a|b|\n| -x | - |\n\n", 500),
		"|a|\n|-|\n|" + strings.Repeat("\\|", 4000) + "|\n",
	} {
		l := DefaultLimits()
		l.DocumentNodes = 100
		r := parseWithProfile(context.Background(), NewSource([]byte(input)), budgetFor(t, l), gfmInProgressProfile)
		requireLimit(t, r, "document-nodes")
		l = DefaultLimits()
		l.WorkUnits = int64(len(input) * 3)
		r = parseWithProfile(context.Background(), NewSource([]byte(input)), budgetFor(t, l), gfmInProgressProfile)
		requireLimit(t, r, "work-units")
	}
	l := DefaultLimits()
	l.DocumentNodes = baseline.nodes
	l.TotalNodes = baseline.nodes * 2
	op := budgetFor(t, l)
	requireTableComplete(t, parseWithProfile(context.Background(), source, op, gfmInProgressProfile), input)
	core := ParseWithBudget(context.Background(), source, op)
	requireTableComplete(t, core, input)
	requireLimit(t, parseWithProfile(context.Background(), source, op, gfmInProgressProfile), "total-nodes")
	requireLimit(t, ParseWithBudget(context.Background(), source, op), "total-nodes")
	op = budgetFor(t, DefaultLimits())
	requireTableComplete(t, parseWithProfile(context.Background(), source, op, gfmInProgressProfile), input)
	work := op.work
	if err := op.ChargeWork(13); err != nil || op.work != work+13 {
		t.Fatal("adapter charge lost")
	}
	requireTableComplete(t, ParseWithBudget(context.Background(), source, op), input)
	if op.work <= work+13 {
		t.Fatal("core retry reset work")
	}
	for _, allowed := range []int64{3, 4, 5} {
		l := DefaultLimits()
		l.SyntaxDepth = allowed
		r := parseWithProfile(context.Background(), NewSource([]byte("> | *a* |\n> | - |\n")), budgetFor(t, l), gfmInProgressProfile)
		if allowed < 5 {
			requireLimit(t, r, "syntax-depth")
		} else if r.Status != Complete {
			t.Fatal("mixed depth rejected", r)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	l = DefaultLimits()
	l.DocumentBytes = 3
	requireLimit(t, parseWithProfile(cancelled, NewSource([]byte("abc\xff")), budgetFor(t, l), gfmInProgressProfile), "document-bytes")
	r = parseWithProfile(cancelled, NewSource([]byte("\xff")), budgetFor(t, l), gfmInProgressProfile)
	if r.Failure == nil || r.Failure.Code != InvalidUTF8 {
		t.Fatal("encoding/cancel precedence")
	}
}

func TestTableCancellationAndConcurrentIsolation(t *testing.T) {
	input := "|a|b|\n|-|-|\n|`" + strings.Repeat("a\\|", 300) + "`| **x** |\n"
	source := NewSource([]byte(input))
	baseline := parseTableStage(t, input)
	for _, checks := range []int{1, 10, 30, 100, 300, 1000, 3000} {
		ctx := &checkpointContext{Context: context.Background(), remaining: checks}
		r := parseWithProfile(ctx, source, budgetFor(t, DefaultLimits()), gfmInProgressProfile)
		if r.Status == Complete {
			continue
		}
		if r.Status != Incomplete || r.AST != Partial || r.Interruption != Cancelled || r.Failure != nil || len(r.Uncertainty) != 1 || r.Document.references.Availability == Available {
			t.Fatal("bad cancelled table", r)
		}
		if r.Phases.Inlines == Interrupted && len(tableNodes(r.Document.root, CodeSpan)) != 0 {
			t.Fatal("unfinished inline tree published")
		}
	}
	shared := budgetFor(t, DefaultLimits())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			for j := 0; j < 3; j++ {
				r := parseWithProfile(context.Background(), source, shared, gfmInProgressProfile)
				if r.Status != Complete || !reflect.DeepEqual(r.Document.root, baseline.Document.root) {
					t.Error("concurrent staged parse changed")
				}
				core := Parse(context.Background(), source)
				if len(tableNodes(core.Document.root, Table)) != 0 {
					t.Error("staged profile leaked")
				}
			}
		})
	}
	wg.Wait()
}

func TestTableApprovedSupplementalSourceWitnesses(t *testing.T) {
	data, err := os.ReadFile("testdata/gfm/profile/expectations.json")
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct{ Fixtures []struct{ ID, Source string } }
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, fixture := range bundle.Fixtures {
		if fixture.ID != "table-code-pipe-crlf" && fixture.ID != "table-nested-quote" {
			continue
		}
		seen++
		t.Run(fixture.ID, func(t *testing.T) {
			r := parseTableStage(t, fixture.Source)
			table := tableNodes(r.Document.root, Table)[0]
			if fixture.ID == "table-code-pipe-crlf" {
				row := table.spec.Children[2]
				if row.Span().Start() != 22 || row.Span().End() != 39 || row.Properties().Row.RawCells != 3 || row.Properties().Row.Padding != 0 {
					t.Fatal("CRLF original row")
				}
				for i, w := range []struct {
					a, b          int
					raw, semantic string
				}{{23, 26, " `x", "`x"}, {27, 30, "y` ", "y`"}, {31, 36, " é\t ", "é"}} {
					cell := row.spec.Children[i]
					if cell.Span().Start() != w.a || cell.Span().End() != w.b || tableRaw(t, r.Source, cell.Span()) != w.raw || tableText(cell) != w.semantic {
						t.Fatal("independent excess/UTF8/tab cell", i)
					}
				}
				cell := table.spec.Children[3].spec.Children[0]
				if cell.Span().Start() != 40 || cell.Span().End() != 41 || tableRaw(t, r.Source, cell.Span()) != " " || tableText(cell) != "" {
					t.Fatal("empty whitespace source field")
				}
			} else {
				content := table.Content()
				if len(content) != 3 {
					t.Fatal("missing mapped row segments")
				}
				for i, part := range content {
					if part.Source.Start() != []int{2, 10, 18}[i] || part.Source.End() != []int{8, 16, 25}[i] {
						t.Fatal("prefix leaked into logical source")
					}
				}
			}
			core := Parse(context.Background(), r.Source)
			for _, disabled := range []ParseResult{ParseWithBudget(context.Background(), r.Source, budgetFor(t, DefaultLimits())), parseWithProfile(context.Background(), r.Source, budgetFor(t, DefaultLimits()), coreProfile)} {
				requireTableComplete(t, disabled, fixture.Source)
				if !reflect.DeepEqual(disabled.Document.root, core.Document.root) || len(tableNodes(disabled.Document.root, Table)) != 0 {
					t.Fatal("supplemental disabled isolation")
				}
			}
		})
	}
	if seen != 2 {
		t.Fatal("missing approved witnesses")
	}
}

func TestTableMappedPipesOpaqueAndBoundaryCases(t *testing.T) {
	input := "| h |\n| - |\n| <a title=\"x\\|y\"> `a\\|b` <https://a/\\|b> &vert; \\&amp; |\n"
	r := parseTableStage(t, input)
	html := tableNodes(r.Document.root, InlineHTML)
	code := tableNodes(r.Document.root, CodeSpan)
	link := tableNodes(r.Document.root, Link)
	if len(html) != 1 || len(code) != 1 || len(link) != 1 || tableRaw(t, r.Source, html[0].Span()) != "<a title=\"x\\|y\">" || tableText(code[0]) != "a|b" || link[0].Properties().Link.Destination.Semantic != Some("https://a/|b") {
		t.Fatal("mapped pipe opaque/shared scanner")
	}
	var semantic strings.Builder
	for _, part := range html[0].Content() {
		semantic.WriteString(part.Semantic.Value)
	}
	if semantic.String() != "<a title=\"x|y\">" {
		t.Fatal("raw HTML lost semantic mapping", semantic.String())
	}
	if !strings.Contains(tableText(tableNodes(r.Document.root, TableRow)[2]), "| &amp;") {
		t.Fatal("generated text was rescanned")
	}
	for _, input := range []string{"| a |\n-\n", "| a |\n:-\n", "| a |\n-:\n", "| a |\n:-:\n"} {
		r := parseTableStage(t, input)
		if len(tableNodes(r.Document.root, Table)) != 1 {
			t.Fatal("one-dash/one-column delimiter")
		}
	}
	// These controls pin mdchk's explicitly recorded odd/even interpretation.
	// GFM 4.10 does not unequivocally mandate this ordering for longer runs;
	// core punctuation-escape grouping motivates the approved choice.
	for run := 1; run <= 4; run++ {
		body := "| `a" + strings.Repeat("\\", run) + "|b` | c |"
		r := parseTableStage(t, "| a | b |\n| - | - |\n"+body+"\n")
		row := tableNodes(r.Document.root, TableRow)[2]
		want := 3
		if run%2 == 1 {
			want = 2
		}
		if row.Properties().Row.RawCells != want {
			t.Fatal("backslash-run separator", run)
		}
		if run%2 == 1 {
			codes := tableNodes(row, CodeSpan)
			if len(codes) != 1 || tableText(codes[0]) != "a"+strings.Repeat("\\", run-1)+"|b" {
				t.Fatal("code backslash-run mapping", run)
			}
		}
	}
}

func TestTableReferenceTitleProbeRemainsLinear(t *testing.T) {
	for _, n := range []int{100, 200} {
		input := "[r]: /u \"title\n" + strings.Repeat("|a|b|\n|-|-|\n", n) + "end\"\n"
		r := parseTableStage(t, input)
		core := Parse(context.Background(), r.Source)
		if !reflect.DeepEqual(r.Document.root, core.Document.root) || !reflect.DeepEqual(r.Document.references, core.Document.references) {
			t.Fatal("failed candidate changed multiline definition")
		}
		op := budgetFor(t, DefaultLimits())
		r = parseWithProfile(context.Background(), r.Source, op, gfmInProgressProfile)
		if r.Status != Complete || op.work > int64(len(input))*300 {
			t.Fatal("repeated title probes rescanned growing prefix", n, op.work)
		}
	}
}

func TestTableWhitespaceAndMappingCancellation(t *testing.T) {
	for name, payload := range map[string]string{
		"trailing":     "x" + strings.Repeat(" ", 16384),
		"leading":      strings.Repeat(" ", 16384) + "x",
		"cell":         "|" + strings.Repeat(" ", 16384) + "x|",
		"escapes":      "|" + strings.Repeat("\\|", 4096) + "|",
		"nul-mappings": "|" + strings.Repeat("\x00", 4096) + "|",
	} {
		t.Run(name, func(t *testing.T) {
			source := NewSource([]byte("|h|\n|-|\n" + payload + "\n"))
			sawInterrupted := false
			for _, checks := range []int{8, 32, 64, 96, 128, 192, 256, 384} {
				ctx := &checkpointContext{Context: context.Background(), remaining: checks}
				r := parseWithProfile(ctx, source, budgetFor(t, DefaultLimits()), gfmInProgressProfile)
				if r.Status == Complete {
					continue
				}
				sawInterrupted = true
				if r.Status != Incomplete || r.Interruption != Cancelled || r.Failure != nil || r.Document.references.LookupAvailability == Available {
					t.Fatal("bad whitespace/mapping interruption", r)
				}
			}
			if !sawInterrupted {
				t.Fatal("cancellation did not exercise real table")
			}
		})
	}
}

func TestTableMappedReferenceIdentifiers(t *testing.T) {
	for _, tc := range []struct {
		name, use string
		kind      Kind
		origin    string
	}{
		{"full-link", "[x][a\\|b]", Link, "full"},
		{"full-image", "![x][a\\|b]", Image, "full"},
		{"shortcut-link", "[a\\|b]", Link, "shortcut"},
		{"collapsed-link", "[a\\|b][]", Link, "collapsed"},
		{"shortcut-image", "![a\\|b]", Image, "shortcut"},
		{"collapsed-image", "![a\\|b][]", Image, "collapsed"},
	} {
		for _, nested := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/nested=%v", tc.name, nested), func(t *testing.T) {
				input := "| h |\n| - |\n| " + tc.use + " |\n\n[a|b]: /u\n"
				if nested {
					input = "> - | é |\r\n>   | - |\r\n>   | ok | " + tc.use + " |\r\n>\r\n> [a|b]: /u\r\n"
				}
				source := NewSource([]byte(input))
				op := budgetFor(t, DefaultLimits())
				r := parseWithProfile(context.Background(), source, op, gfmInProgressProfile)
				requireTableComplete(t, r, input)
				nodes := tableNodes(r.Document.root, tc.kind)
				if len(nodes) != 1 {
					t.Fatal("missing mapped reference", nodes)
				}
				node := nodes[0]
				p := node.Properties().Link
				if p.Origin != tc.origin || p.NormalizedLabel != Some("a|b") || p.Destination.Semantic != Some("/u") || !referenceProjection(p.Destination) {
					t.Fatal("wrong lookup/projection", p)
				}
				label := p.Label
				if tc.origin == "full" {
					if p.ReferenceLabel == nil {
						t.Fatal("missing full lookup label")
					}
					label = *p.ReferenceLabel
				} else if tc.origin == "collapsed" {
					if p.ReferenceLabel == nil || !referenceIdentifier(*p.ReferenceLabel, true) {
						t.Fatal("collapsed source identifier")
					}
				}
				if label.Semantic != Some("a|b") {
					t.Fatal("mapped lookup-label semantics", label)
				}
				start := strings.Index(input, "a\\|b")
				if start < 0 {
					t.Fatal("bad input")
				}
				if contentEnvelope(label.Segments).Start() != start || contentEnvelope(label.Segments).End() != start+4 || tableRaw(t, source, contentEnvelope(label.Segments)) != "a\\|b" {
					t.Fatal("lost original identifier source", label)
				}
				escaped := 0
				for _, part := range label.Segments {
					if part.Transformation == Escape {
						escaped++
						if part.Source.Start() != start+1 || part.Source.End() != start+3 || tableRaw(t, source, part.Source) != "\\|" || part.Semantic != Some("|") {
							t.Fatal("wrong mapped escape", part)
						}
					}
				}
				if escaped != 1 {
					t.Fatal("missing escaped pipe provenance")
				}
				rows := tableNodes(r.Document.root, TableRow)
				body := rows[2]
				if nested && (body.Properties().Row.RawCells != 2 || tableNodes(r.Document.root, Table)[0].Properties().Table.Width != 1 || len(tableNodes(body.Children()[1], tc.kind)) != 1) {
					t.Fatal("reference lost from excess source cell")
				}
				refs := r.Document.References()
				if len(refs.Definitions) != 1 || refs.Lookup["a|b"] != 0 || refs.Definitions[0].Properties().NormalizedLabel != Some("a|b") {
					t.Fatal("definition binding", refs)
				}
				if _, err := NewDocument(r.Document.Root(), refs); err != nil {
					t.Fatal("completed reference binding rejected", err)
				}
				if _, err := r.Document.ProtectedNodes(context.Background(), op); err != nil {
					t.Fatal("completed protection authority", err)
				}
				repeat := parseWithProfile(context.Background(), source, budgetFor(t, DefaultLimits()), gfmInProgressProfile)
				requireTableComplete(t, repeat, input)
				if !reflect.DeepEqual(r.Document.root, repeat.Document.root) {
					t.Fatal("repeat changed mapped source")
				}
			})
		}
	}
}

func TestTableMappedReferenceConstructorAndCoreControls(t *testing.T) {
	input := "| h |\n| - |\n| [x][a\\|b] |\n\n[a|b]: /u\n[a\\|b]: /raw\n[a|b]: /later\n"
	source := NewSource([]byte(input))
	r := parseWithProfile(context.Background(), source, budgetFor(t, DefaultLimits()), gfmInProgressProfile)
	requireTableComplete(t, r, input)
	link := tableNodes(r.Document.root, Link)[0]
	if link.Properties().Link.Destination.Semantic != Some("/u") || r.Document.References().Lookup["a|b"] != 0 || len(r.Document.References().Definitions) != 3 {
		t.Fatal("first definition binding changed")
	}
	if _, err := NewNode(link.spec); err != nil {
		t.Fatal("permitted mapped identifier rejected", err)
	}
	escape := -1
	for i, part := range link.Properties().Link.ReferenceLabel.Segments {
		if part.Transformation == Escape {
			escape = i
		}
	}
	if escape < 0 {
		t.Fatal("missing mapped identifier")
	}
	foreign := NewSource(source.Bytes())
	original := link.Properties().Link.ReferenceLabel.Segments[escape].Source
	foreignSpan, err := foreign.Span(original.Start(), original.End())
	if err != nil {
		t.Fatal(err)
	}
	wrongRaw, err := source.Span(link.Span().Start(), link.Span().Start()+2)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*LinkProperties){
		"absent-source":        func(p *LinkProperties) { p.ReferenceLabel.Segments[escape].Source = Span{} },
		"foreign-source":       func(p *LinkProperties) { p.ReferenceLabel.Segments[escape].Source = foreignSpan },
		"wrong-original-bytes": func(p *LinkProperties) { p.ReferenceLabel.Segments[escape].Source = wrongRaw },
		"wrong-pipe-value":     func(p *LinkProperties) { p.ReferenceLabel.Segments[escape].Semantic = Some("!") },
		"absent-pipe-value":    func(p *LinkProperties) { p.ReferenceLabel.Segments[escape].Semantic = Optional[string]{} },
		"entity-mapping":       func(p *LinkProperties) { p.ReferenceLabel.Segments[escape].Transformation = Entity },
		"synthetic-mapping":    func(p *LinkProperties) { p.ReferenceLabel.Segments[escape].Transformation = Synthetic },
		"forged-aggregate":     func(p *LinkProperties) { p.ReferenceLabel.Semantic = Some("unrelated") },
		"not-collapsed-empty":  func(p *LinkProperties) { p.Origin = "collapsed" },
	} {
		t.Run(name, func(t *testing.T) {
			spec := link.spec
			spec.Properties = link.Properties()
			mutate(spec.Properties.Link)
			if _, err := NewNode(spec); err == nil {
				t.Fatal("forged mapped reference admitted")
			}
		})
	}
	for _, mutate := range []func(*ReferenceCollection){
		func(refs *ReferenceCollection) { refs.Lookup["a|b"] = 2 },
		func(refs *ReferenceCollection) {
			refs.Definitions = Parse(context.Background(), foreign).Document.References().Definitions
		},
	} {
		refs := r.Document.References()
		mutate(&refs)
		if _, err := NewDocument(r.Document.Root(), refs); err == nil {
			t.Fatal("mapped reference accepted later/foreign definition")
		}
	}
	// A source-backed ordinary core identifier remains raw: its backslash is
	// part of the lookup key, selecting /raw rather than the table's /u binding.
	for _, core := range []ParseResult{Parse(context.Background(), source), ParseWithBudget(context.Background(), source, budgetFor(t, DefaultLimits())), parseWithProfile(context.Background(), source, budgetFor(t, DefaultLimits()), coreProfile)} {
		requireTableComplete(t, core, input)
		nodes := tableNodes(core.Document.root, Link)
		if len(nodes) != 1 {
			t.Fatal("core raw-label reference missing")
		}
		p := nodes[0].Properties().Link
		if p.NormalizedLabel != Some("a\\|b") || p.ReferenceLabel.Semantic != Some("a\\|b") || p.Destination.Semantic != Some("/raw") || len(tableNodes(core.Document.root, Table)) != 0 {
			t.Fatal("table mapping leaked into core", p)
		}
		for _, part := range p.ReferenceLabel.Segments {
			if part.Transformation == Escape {
				t.Fatal("ordinary raw identifier decoded")
			}
		}
	}
	// The same mapped pipe remains usable by the adjacent shared inline producer
	// paths; their source-bound values do not become reference identifiers.
	controls := parseTableStage(t, "|h|\n|-|\n|[x](u\\|v) ![x](u \"a\\|b\")|\n")
	links, images := tableNodes(controls.Document.root, Link), tableNodes(controls.Document.root, Image)
	if len(links) != 1 || len(images) != 1 || links[0].Properties().Link.Destination.Semantic != Some("u|v") || images[0].Properties().Link.Title.Semantic != Some("a|b") {
		t.Fatal("adjacent shared producer changed")
	}
}
