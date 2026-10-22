package parser

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type filteredActual struct {
	owner *Node
	fact  HTMLFilteredTag
}

func filteredActuals(root *Node) []filteredActual {
	var out []filteredActual
	Walk(root, func(n *Node) bool {
		if p := n.Properties().HTML; p != nil {
			for _, f := range p.FilteredTags {
				out = append(out, filteredActual{n, f})
			}
		}
		return true
	})
	return out
}
func assertFiltered(t *testing.T, r ParseResult, want []int) []filteredActual {
	t.Helper()
	got := filteredActuals(r.Document.root)
	if len(got) != len(want) {
		t.Fatalf("affected count %d want %v: %q", len(got), want, r.Source.content)
	}
	for i, angle := range want {
		g := got[i]
		assertTaskSpan(t, g.fact.Affected, angle, angle+1)
		if text, err := r.Source.Text(g.fact.Affected); err != nil || text != "<" {
			t.Fatal("original angle", text, err)
		}
		if g.fact.Prefix.Segments[0].Source.source != r.Source || g.owner.Span().source != r.Source {
			t.Fatal("foreign metadata")
		}
	}
	return got
}
func TestFilteredPublishedAndAdapted(t *testing.T) {
	for _, c := range []struct {
		id                                      int
		angles, tagEnds, ownerStarts, ownerEnds []int
	}{
		{657, []int{9, 17, 46, 68}, []int{16, 24, 51, 73}, []int{9, 17, 31, 31}, []int{16, 24, 108, 108}},
		{140, []int{0, 120}, []int{31, 129}, []int{0, 0}, []int{130, 130}},
		{141, []int{0, 59}, []int{25, 67}, []int{0, 0}, []int{68, 68}},
		{142, []int{0}, []int{25}, []int{0}, []int{31}},
		{145, []int{0, 20}, []int{7, 28}, []int{0, 0}, []int{29, 29}},
		{147, []int{0, 13}, []int{8, 22}, []int{0, 0}, []int{31, 31}},
	} {
		t.Run(fmt.Sprint(c.id), func(t *testing.T) {
			input := originalTaskInput(t, c.id)
			r := parseTableStage(t, input)
			got := assertFiltered(t, r, c.angles)
			for i, g := range got {
				assertTaskSpan(t, g.owner.Span(), c.ownerStarts[i], c.ownerEnds[i])
				if g.fact.Tag == nil {
					t.Fatal("missing complete actual tag")
				}
				assertTaskSpan(t, contentEnvelope(g.fact.Tag.Segments), c.angles[i], c.tagEnds[i])
				for _, part := range g.fact.Tag.Segments {
					raw, err := r.Source.Text(part.Source)
					if err != nil || raw != part.Semantic.Value {
						t.Fatal("raw tag", part, err)
					}
				}
			}
			if c.id == 657 && len(tableNodes(r.Document.root, InlineHTML)) != 4 {
				t.Fatal("ordinary strong/em lost")
			}
			if c.id == 140 || c.id == 141 || c.id == 145 {
				if len(r.Document.root.spec.Children) < 2 || r.Document.root.spec.Children[1].Kind() != Paragraph {
					t.Fatal("following prose lost")
				}
			}
			assertFilteredCore(t, input)
		})
	}
}
func assertFilteredCore(t *testing.T, input string) {
	t.Helper()
	source := NewSource([]byte(input))
	baseline := Parse(context.Background(), source)
	for _, r := range []ParseResult{Parse(context.Background(), source), ParseWithBudget(context.Background(), source, budgetFor(t, DefaultLimits())), parseWithProfile(context.Background(), source, budgetFor(t, DefaultLimits()), coreProfile), parseWithProfile(context.Background(), source, budgetFor(t, DefaultLimits()), parserProfile(0))} {
		requireTableComplete(t, r, input)
		if len(filteredActuals(r.Document.root)) != 0 || !reflect.DeepEqual(baseline.Document.root, r.Document.root) {
			t.Fatal("core changed")
		}
	}
}
func TestFilteredApprovedWitnessesAndCombined(t *testing.T) {
	raw, err := os.ReadFile("testdata/gfm/profile/expectations.json")
	if err != nil {
		t.Fatal(err)
	}
	var data struct{ Fixtures []struct{ ID, Source string } }
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	seen := 0
	// Literal offsets below are independent of fixture projected facts/output.
	wants := map[string][]int{"modern-textarea": {0, 28}, "filter-title": {0, 14}, "filter-textarea": {0, 17}, "filter-style": {0, 14}, "filter-xmp": {0, 12}, "filter-iframe": {0, 15}, "filter-noembed": {0, 16}, "filter-noframes": {0, 17}, "filter-script": {0, 15}, "filter-plaintext": {0, 18}, "filter-nearmisses": {}, "combined-all-five": {35}, "comment-protection": {}, "code-protection": {}}
	for _, f := range data.Fixtures {
		want, ok := wants[f.ID]
		if !ok {
			continue
		}
		seen++
		t.Run(f.ID, func(t *testing.T) {
			r := parseTableStage(t, f.Source)
			got := assertFiltered(t, r, want)
			if f.ID == "combined-all-five" {
				if len(tableNodes(r.Document.root, ListItem)) == 0 || len(tableNodes(r.Document.root, Table)) != 1 || len(tableNodes(r.Document.root, Strike)) != 1 || len(bareNodes(r.Document.root)) != 1 {
					t.Fatal("combined syntax")
				}
				assertTaskSpan(t, got[0].owner.Span(), 35, 42)
			}
			if f.ID == "modern-textarea" && (len(r.Document.references.Definitions) != 0 || len(r.Document.root.spec.Children) != 1) {
				t.Fatal("textarea opacity")
			}
			assertFilteredCore(t, f.Source)
		})
	}
	if seen < 12 {
		t.Fatal("missing approved inputs", seen)
	}
}
func TestFilteredExactBoundariesAndEndpoints(t *testing.T) {
	// Deliberately independent nine-name list and boundary outcomes.
	for _, name := range []string{"title", "textarea", "style", "xmp", "iframe", "noembed", "noframes", "script", "plaintext"} {
		for _, slash := range []string{"", "/"} {
			for _, b := range []struct {
				suffix     string
				match, tag bool
			}{
				{">", true, true}, {"/>", true, slash == ""}, {" x='y'>", true, slash == ""}, {"\t>", true, true}, {"\n>", true, true}, {"\r\n>", true, true}, {"\r>", true, true}, {" ", true, false}, {"", false, false}, {"/", false, false}, {"/x>", false, false}, {"x>", false, false}, {"-x>", false, false}, {"1>", false, false}, {":>", false, false}, {"\v>", false, false}, {"\f>", false, false}, {"\u00a0>", false, false}, {"\u0085>", false, false}, {"\x00>", false, false},
			} {
				token := "<" + slash + strings.ToUpper(name) + b.suffix
				t.Run(name+slash+fmt.Sprintf("%q", b.suffix), func(t *testing.T) {
					// An established raw owner is necessary for malformed prefix facts.
					r := parseTableStage(t, "<div>\n"+token)
					want := []int{}
					if b.match {
						want = []int{6}
					}
					got := assertFiltered(t, r, want)
					if b.match {
						f := got[0].fact
						if f.Name != name || f.Closing != (slash != "") || (f.Tag != nil) != b.tag || f.Prefix.Semantic != Some("<"+slash+strings.ToUpper(name)) {
							t.Fatal("prefix vs complete grammar", f)
						}
					}
					// Inline classification must remain core grammar, independent of prefix.
					inlineToken := strings.NewReplacer("\n>", "\n    >", "\r>", "\r    >").Replace(token)
					inline := parseTableStage(t, "a "+inlineToken)
					inlineWant := []int{}
					if b.match && b.tag {
						inlineWant = []int{2}
					}
					assertFiltered(t, inline, inlineWant)
				})
			}
		}
	}
	for _, input := range []string{"a < script>", "a < /script>", "a </ script>", "a <ſcript>", "a &lt;script>", "a \\<script>", "`<script>`", "    <script>\n", "```\n<script>\n```\n"} {
		assertFiltered(t, parseTableStage(t, input), nil)
	}
}
func TestFilteredActualOwnersAndCommentTails(t *testing.T) {
	for _, c := range []struct {
		input    string
		angles   []int
		comments int
	}{
		{"<!-- <script> --> <style>\n", []int{18}, 1},
		{"<!-- <script>\n", nil, 1},
		{"a <!-- <script> --> b", nil, 1},
		{"a <span x='<title>'> b", nil, 0},
		{"a <?x <title> ?> b", nil, 0},
		{"a <![CDATA[ <title> ]]> b", nil, 0},
		{"a <!x <title> > b", nil, 0},
		{"<script>\nconst s=\"<!-- <title> -->\";\n</script> <!-- <style> --> <xmp>\n", []int{0, 23, 37, 64}, 1},
		{"<div x='<title>'>\n<!-- <style> -->\n", []int{8, 23}, 0},
		{"<?x <title> ?>\n", []int{4}, 0},
		{"<![CDATA[ <title> ]]>\n", []int{10}, 0},
		{"<!x <title> >\n", []int{4}, 0},
		{"<pre>\n`<title>`\n</pre><xmp><!-- <style>\n", []int{7, 22}, 1},
	} {
		t.Run(fmt.Sprintf("%q", c.input), func(t *testing.T) {
			source := NewSource([]byte(c.input))
			op := budgetFor(t, DefaultLimits())
			r := parseWithProfile(context.Background(), source, op, gfmInProgressProfile)
			requireTableComplete(t, r, c.input)
			assertFiltered(t, r, c.angles)
			coreOp := budgetFor(t, DefaultLimits())
			core := ParseWithBudget(context.Background(), source, coreOp)
			requireTableComplete(t, core, c.input)
			got, err := r.Document.Comments(context.Background(), op)
			if err != nil || len(got) != c.comments {
				t.Fatal("confirmed comments", got, err)
			}
			original, err := core.Document.Comments(context.Background(), coreOp)
			if err != nil || !reflect.DeepEqual(got, original) {
				t.Fatal("comments changed")
			}
			a, err := r.Document.LiteralSpans(context.Background(), op)
			if err != nil {
				t.Fatal(err)
			}
			b, err := core.Document.LiteralSpans(context.Background(), coreOp)
			if err != nil || !reflect.DeepEqual(a, b) {
				t.Fatal("literal inventory changed")
			}
			owners, err := r.Document.ProtectedNodes(context.Background(), op)
			if err != nil {
				t.Fatal(err)
			}
			before, err := core.Document.ProtectedNodes(context.Background(), coreOp)
			if err != nil || len(owners) != len(before) {
				t.Fatal("owner inventory")
			}
			for i, n := range owners {
				if n.Kind() != before[i].Kind() || n.Span() != before[i].Span() || !reflect.DeepEqual(n.Content(), before[i].Content()) || n.Properties().HTML.BlockForm != before[i].Properties().HTML.BlockForm {
					t.Fatal("raw classification changed")
				}
			}
			for _, actual := range filteredActuals(r.Document.root) {
				for _, tail := range actual.owner.htmlTail {
					if len(filteredActuals(tail)) != 0 {
						t.Fatal("duplicate tail metadata")
					}
				}
			}
		})
	}
}

func TestFilteredSourceMappingsAndModel(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, prefix := range []string{"> ", "- ", "> - "} {
			continuation := strings.ReplaceAll(prefix, "- ", "  ")
			input := prefix + "<style x='é" + ending + continuation + "日'>" + ending + continuation + "raw" + ending + continuation + "</style>" + ending
			source := NewSource([]byte(input))
			r := parseWithProfile(context.Background(), source, budgetFor(t, DefaultLimits()), gfmInProgressProfile)
			requireTableComplete(t, r, input)
			start := len(prefix)
			close := strings.Index(input, "</style>")
			got := assertFiltered(t, r, []int{start, close})
			if len(got[0].fact.Tag.Segments) < 2 {
				t.Fatal("container mappings flattened")
			}
			for _, g := range got {
				for _, p := range g.fact.Tag.Segments {
					raw, err := source.Text(p.Source)
					if err != nil || raw != p.Semantic.Value || strings.Contains(raw, continuation+"日") && prefix != "- " {
						t.Fatal("container prefix included", p)
					}
				}
			}
			foreign := NewSource(source.Bytes())
			if _, err := foreign.Text(got[0].fact.Affected); err == nil {
				t.Fatal("foreign source accepted")
			}
		}
	}
	input := "é <title x='\x00'>\n"
	r := parseTableStage(t, input)
	got := assertFiltered(t, r, []int{3})
	n := got[0].owner
	normalized := false
	for _, p := range got[0].fact.Tag.Segments {
		normalized = normalized || p.Transformation == Normalized
	}
	if !normalized {
		t.Fatal("NUL mapping lost")
	}
	// Table escapes remain atomic and retain original pipe bytes on the tag.
	table := parseTableStage(t, "| a |\n| - |\n| <title x='a\\|b'> |\n")
	tag := assertFiltered(t, table, []int{14})[0].fact.Tag
	escaped := false
	for _, p := range tag.Segments {
		if p.Transformation == Escape {
			escaped = true
			raw, _ := table.Source.Text(p.Source)
			if raw != "\\|" || p.Semantic != Some("|") {
				t.Fatal("pipe mapping")
			}
		}
	}
	if !escaped {
		t.Fatal("escape mapping lost")
	}
	spec := n.spec
	spec.Properties = n.Properties()
	spec.Content = n.Content()
	manual, err := NewNode(spec)
	if err != nil {
		t.Fatal(err)
	}
	original := manual.Properties()
	spec.Properties.HTML.FilteredTags[0].Prefix.Segments[0].Semantic = Some("mutated")
	spec.Properties.HTML.FilteredTags[0].Tag.Segments = nil
	spec.Properties.HTML.FilteredTags[0].Name = "x"
	p := manual.Properties()
	p.HTML.FilteredTags[0].Prefix.Segments = nil
	*p.HTML.FilteredTags[0].Tag = Value{}
	p.HTML.FilteredTags = nil
	if !reflect.DeepEqual(manual.Properties(), original) {
		t.Fatal("mutable metadata retained")
	}
	for name, change := range map[string]func(*NodeSpec){
		"kind":    func(s *NodeSpec) { s.Kind = Text },
		"name":    func(s *NodeSpec) { s.Properties.HTML.FilteredTags[0].Name = "TITLE" },
		"closing": func(s *NodeSpec) { s.Properties.HTML.FilteredTags[0].Closing = true },
		"angle":   func(s *NodeSpec) { s.Properties.HTML.FilteredTags[0].Affected.start++ },
		"foreign": func(s *NodeSpec) {
			f := &s.Properties.HTML.FilteredTags[0]
			f.Affected.source = NewSource(r.Source.Bytes())
		},
		"duplicate": func(s *NodeSpec) {
			s.Properties.HTML.FilteredTags = append(s.Properties.HTML.FilteredTags, s.Properties.HTML.FilteredTags[0])
		},
		"prefix":       func(s *NodeSpec) { s.Properties.HTML.FilteredTags[0].Prefix.Semantic = Some("<script") },
		"transformed":  func(s *NodeSpec) { s.Properties.HTML.FilteredTags[0].Prefix.Segments[0].Transformation = Escape },
		"unrelatedtag": func(s *NodeSpec) { s.Properties.HTML.FilteredTags[0].Tag.Segments[0].Source.start++ },
		"gap":          func(s *NodeSpec) { s.Content = nil },
	} {
		t.Run(name, func(t *testing.T) {
			s := n.spec
			s.Properties = n.Properties()
			change(&s)
			if _, err := NewNode(s); err == nil {
				t.Fatal("forged metadata accepted")
			}
		})
	}
	two := parseTableStage(t, "<div> <title></title>\n").Document.root.spec.Children[0]
	s := two.spec
	s.Properties = two.Properties()
	s.Properties.HTML.FilteredTags[0], s.Properties.HTML.FilteredTags[1] = s.Properties.HTML.FilteredTags[1], s.Properties.HTML.FilteredTags[0]
	if _, err := NewNode(s); err == nil {
		t.Fatal("reordered metadata")
	}
	root, err := NewNode(NodeSpec{Kind: DocumentKind, Envelope: r.Document.root.Span(), Children: []*Node{manual}})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := NewDocument(root, r.Document.references)
	if err != nil {
		t.Fatal(err)
	}
	op := budgetFor(t, DefaultLimits())
	if _, err := doc.Comments(context.Background(), op); err == nil {
		t.Fatal("manual comment authority")
	}
	if _, err := doc.LiteralSpans(context.Background(), op); err == nil {
		t.Fatal("manual literal authority")
	}
	if _, err := doc.ProtectedNodes(context.Background(), op); err == nil {
		t.Fatal("manual protected authority")
	}
}

func TestFilteredResourceAndGrowth(t *testing.T) {
	input := "> - <textarea>\r\n>   <title x='é'>\r\n>   </textarea><!-- <xmp> --> <style>\r\n"
	source := NewSource([]byte(input))
	baseline := budgetFor(t, DefaultLimits())
	requireTableComplete(t, parseWithProfile(context.Background(), source, baseline, gfmInProgressProfile), input)
	for name, used := range map[string]int64{"work-units": baseline.work, "document-nodes": baseline.nodes, "total-nodes": baseline.nodes} {
		for _, delta := range []int64{-1, 0, 1} {
			l := DefaultLimits()
			switch name {
			case "work-units":
				l.WorkUnits = used + delta
			case "document-nodes":
				l.DocumentNodes = used + delta
			case "total-nodes":
				l.DocumentNodes = used
				l.TotalNodes = used + 1 + delta
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
	for _, generate := range []func(int) string{
		func(n int) string { return "<div>\n" + strings.Repeat("<title> </title> ", n) },
		func(n int) string { return "<div>\n" + strings.Repeat("<scriptxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx ", n) },
		func(n int) string { return "<div>\n" + strings.Repeat("<title x=\"", n) + "\"" },
		func(n int) string {
			return "<div>\n" + strings.Repeat("<title x='", n) + "' " + strings.Repeat("a ", n) + "!"
		},
		func(n int) string { return "<script>\n</script>" + strings.Repeat("<!-- <title> --> <xmp>", n) },
		func(n int) string { return "> <div>\n" + strings.Repeat("> <title x='é'>\n", n) },
		func(n int) string { return "| a |\n| - |\n| <title x='" + strings.Repeat("\x00\\|", n) + "'> |\n" },
	} {
		var previous int64
		for _, size := range []int{64, 128, 256} {
			input := generate(size)
			op := budgetFor(t, DefaultLimits())
			r := parseWithProfile(context.Background(), NewSource([]byte(input)), op, gfmInProgressProfile)
			requireTableComplete(t, r, input)
			if previous != 0 && op.work > previous*3 {
				t.Fatal("superlinear family", previous, op.work)
			}
			previous = op.work
		}
	}
	// New metadata introduces no depth beyond existing raw owners/containers.
	core := budgetFor(t, DefaultLimits())
	requireTableComplete(t, ParseWithBudget(context.Background(), source, core), input)
	l := DefaultLimits()
	l.DocumentNodes = baseline.nodes
	l.TotalNodes = baseline.nodes * 2
	op := budgetFor(t, l)
	requireTableComplete(t, parseWithProfile(context.Background(), source, op, gfmInProgressProfile), input)
	requireTableComplete(t, ParseWithBudget(context.Background(), source, op), input)
	requireLimit(t, parseWithProfile(context.Background(), source, op, gfmInProgressProfile), "total-nodes")
	requireLimit(t, ParseWithBudget(context.Background(), source, op), "total-nodes")
}
func TestFilteredCancellationAndIsolation(t *testing.T) {
	for _, input := range []string{"", "é <title x='\x00'>", "> <style x='a\n> b'>\n> </style><!-- <title> --> <xmp>\n", "<div>\n<title x=' <title x=\"'> <script ", "| a |\n| - |\n| <title x='a\\|b'> |\n"} {
		source := NewSource([]byte(input))
		for _, deadline := range []bool{false, true} {
			completed := false
			seen := 0
			for checkpoint := 1; checkpoint < 3000; checkpoint++ {
				ctx := &quoteCheckpoint{checkpointContext: checkpointContext{Context: context.Background(), remaining: checkpoint}, deadline: deadline}
				op := budgetFor(t, DefaultLimits())
				r := parseWithProfile(ctx, source, op, gfmInProgressProfile)
				if r.Status == Complete {
					requireTableComplete(t, r, input)
					completed = true
					break
				}
				seen++
				reason := Cancelled
				if deadline {
					reason = DeadlineExceeded
				}
				if r.Status != Incomplete || r.AST != Partial || r.Failure != nil || r.Interruption != reason || len(r.Uncertainty) != 1 || r.Document.references.LookupAvailability == Available {
					t.Fatal("incomplete publication", r)
				}
				assertTaskSpan(t, r.Uncertainty[0].Region, 0, len(input))
				if _, err := r.Document.Comments(context.Background(), op); err == nil {
					t.Fatal("partial comments")
				}
				if _, err := r.Document.LiteralSpans(context.Background(), op); err == nil {
					t.Fatal("partial literals")
				}
				if _, err := r.Document.ProtectedNodes(context.Background(), op); err == nil {
					t.Fatal("partial protection")
				}
			}
			if !completed || seen == 0 {
				t.Fatal("sweep did not finish", seen)
			}
		}
		requireTableComplete(t, parseWithProfile(context.Background(), source, budgetFor(t, DefaultLimits()), gfmInProgressProfile), input)
	}
	source := NewSource([]byte("a <title> b </title>"))
	shared := budgetFor(t, DefaultLimits())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			op := shared
			if i%2 == 0 {
				op = budgetFor(t, DefaultLimits())
			}
			profile := gfmInProgressProfile
			if i%3 == 0 {
				profile = coreProfile
			}
			r := parseWithProfile(context.Background(), source, op, profile)
			requireTableComplete(t, r, source.content)
			if profile == coreProfile {
				if len(filteredActuals(r.Document.root)) != 0 {
					t.Error("core leaked")
				}
				return
			}
			got := assertFiltered(t, r, []int{2, 12})
			p := got[0].owner.Properties()
			p.HTML.FilteredTags[0].Prefix.Segments = nil
			*p.HTML.FilteredTags[0].Tag = Value{}
			assertFiltered(t, r, []int{2, 12})
		}(i)
	}
	wg.Wait()
}

func TestFilteredGapForgeryPartialTabAndScalarPositions(t *testing.T) {
	input := "> <title x='é\r\n> 日'>\r\n"
	r := parseTableStage(t, input)
	got := assertFiltered(t, r, []int{2})[0]
	index := NewLineIndex(r.Source)
	for _, c := range []struct{ offset, line, column int }{{2, 1, 3}, {16, 2, 1}, {18, 2, 3}, {21, 2, 4}} {
		p, err := index.Position(c.offset)
		if err != nil || p.Line != c.line || p.Column != c.column {
			t.Fatal("scalar position", c, p, err)
		}
	}
	if _, err := index.Position(16); err != nil {
		t.Fatal(err)
	}
	// A real angle inside an omitted prefix cannot gain owner association solely
	// by lying inside the envelope. No owner Content selects this middle tag.
	source := NewSource([]byte("<title> GAP <style> TAIL"))
	whole, _ := source.Span(0, source.Len())
	a, _ := source.Span(0, 7)
	b, _ := source.Span(20, 24)
	hidden, _ := source.Span(12, 19)
	prefix, _ := source.Span(12, 18)
	angle, _ := source.Span(12, 13)
	s := NodeSpec{Kind: HTMLBlock, Envelope: whole, Content: []Segment{{Source: a, Transformation: Exact, Semantic: Some("<title>")}, {Source: b, Transformation: Exact, Semantic: Some("TAIL")}}, Properties: Properties{HTML: &HTMLProperties{FilteredTags: []HTMLFilteredTag{{Name: "style", Prefix: Value{Segments: []Segment{{Source: prefix, Transformation: Exact, Semantic: Some("<style")}}, Semantic: Some("<style")}, Affected: angle, Tag: &Value{Segments: []Segment{{Source: hidden, Transformation: Exact, Semantic: Some("<style>")}}, Semantic: Some("<style>")}}}}}}
	if _, err := NewNode(s); err == nil {
		t.Fatal("container-gap association accepted")
	}
	// Partial tabs remain existing atomic source contributions. They neither
	// manufacture raw prefixes nor disappear from a proven complete tag value.
	source = NewSource([]byte("<title x='\t'>"))
	whole, _ = source.Span(0, source.Len())
	a, _ = source.Span(0, 10)
	tab, _ := source.Span(10, 11)
	b, _ = source.Span(11, 13)
	parts := []Segment{{Source: a, Transformation: Exact, Semantic: Some("<title x='")}, {Source: tab, Transformation: PartialTab, Semantic: Some("  ")}, {Source: b, Transformation: Exact, Semantic: Some("'>")}}
	n, err := NewNode(NodeSpec{Kind: InlineHTML, Envelope: whole, Content: parts, Properties: Properties{HTML: &HTMLProperties{}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := &parserContext{Context: context.Background(), profile: gfmInProgressProfile}
	filtered, err := filterHTMLTree(ctx, n)
	if err != nil || len(filtered.spec.Properties.HTML.FilteredTags) != 1 || filtered.spec.Properties.HTML.FilteredTags[0].Tag.Segments[1] != parts[1] {
		t.Fatal("partial tab retained", err)
	}
	if !reflect.DeepEqual(n.Content(), parts) || len(n.Properties().HTML.FilteredTags) != 0 {
		t.Fatal("borrowed node changed")
	}
	// Prefix metadata cannot be manufactured by a transformed '<'.
	forged := got.owner.spec
	forged.Properties = got.owner.Properties()
	forged.Properties.HTML.FilteredTags[0].Prefix.Segments[0].Transformation = Normalized
	if _, err := NewNode(forged); err == nil {
		t.Fatal("generated prefix")
	}
}

func TestFilteredPostSyntaxCheckpoints(t *testing.T) {
	input := "<textarea>\n<title x=' <title x=\"'>\n</textarea><!-- <script> --> <xmp>\n"
	source := NewSource([]byte(input))
	core := Parse(context.Background(), source)
	requireTableComplete(t, core, input)
	before := core.Document.root
	for _, deadline := range []bool{false, true} {
		completed := false
		seen := 0
		for checkpoint := 1; checkpoint < 1000; checkpoint++ {
			cancel := &quoteCheckpoint{checkpointContext: checkpointContext{Context: context.Background(), remaining: checkpoint}, deadline: deadline}
			ctx := &parserContext{Context: cancel, profile: gfmInProgressProfile}
			next, err := filterHTMLTree(ctx, before)
			if err != nil {
				t.Fatal(err)
			}
			if len(filteredActuals(before)) != 0 {
				t.Fatal("borrowed tree mutated")
			}
			if interruption(ctx) != "" {
				seen++
				continue
			}
			if len(filteredActuals(next)) != 5 {
				t.Fatal("post-syntax facts", len(filteredActuals(next)))
			}
			completed = true
			break
		}
		if !completed || seen < 20 {
			t.Fatal("post-syntax sweep", completed, seen)
		}
	}
	for _, allowed := range []int64{3, 4, 5} {
		input := "> - <title>\n"
		l := DefaultLimits()
		l.SyntaxDepth = allowed
		for _, profile := range []parserProfile{coreProfile, gfmInProgressProfile} {
			r := parseWithProfile(context.Background(), NewSource([]byte(input)), budgetFor(t, l), profile)
			if allowed < 4 {
				requireLimit(t, r, "syntax-depth")
			} else {
				requireTableComplete(t, r, input)
			}
		}
	}
}

// Existing extension absence tests compare all ordinary source/private facts,
// allowing only this newly implemented additive inventory in staged raw owners.
func withoutFilteredFacts(root *Node) *Node {
	n := cloneNode(root)
	n.spec.Properties = root.Properties()
	if n.spec.Properties.HTML != nil {
		n.spec.Properties.HTML.FilteredTags = nil
	}
	n.spec.Children = make([]*Node, len(root.spec.Children))
	for i, child := range root.spec.Children {
		n.spec.Children[i] = withoutFilteredFacts(child)
	}
	// Preserve nilness for ordinary model equality.
	if root.spec.Children == nil {
		n.spec.Children = nil
	}
	return n
}

func TestFilteredOverlappingTagFactsAndReferences(t *testing.T) {
	input := "<div>\n<title x='<title>'>\n"
	r := parseTableStage(t, input)
	got := assertFiltered(t, r, []int{6, 16})
	if got[0].fact.Tag == nil || got[1].fact.Tag == nil {
		t.Fatal("missing overlapping proven tags")
	}
	assertTaskSpan(t, contentEnvelope(got[0].fact.Tag.Segments), 6, 25)
	assertTaskSpan(t, contentEnvelope(got[1].fact.Tag.Segments), 16, 23)
	if _, err := NewNode(got[0].owner.spec); err != nil {
		t.Fatal("coherent overlapping source facts", err)
	}
	input = "<textarea>\n\n[r]: /hidden\n<!-- mdchk:disable MD001 -->\n</textarea>\n\n[x][r] <title>\n\n[r]: /first\n[r]: /later\n"
	source := NewSource([]byte(input))
	core := Parse(context.Background(), source)
	stage := parseWithProfile(context.Background(), source, budgetFor(t, DefaultLimits()), gfmInProgressProfile)
	requireTableComplete(t, stage, input)
	if !reflect.DeepEqual(core.Document.references, stage.Document.references) || len(stage.Document.references.Definitions) != 2 {
		t.Fatal("references changed")
	}
	links := tableNodes(stage.Document.root, Link)
	if len(links) != 1 || links[0].Properties().Link.Destination.Semantic != Some("/first") {
		t.Fatal("first-definition binding")
	}
	if !reflect.DeepEqual(core.Document.root, withoutFilteredFacts(stage.Document.root)) {
		t.Fatal("raw source/ordinary tree changed")
	}
}

func TestFilteredRejectsInventedPrefixAndBoundaryGaps(t *testing.T) {
	for _, c := range []struct {
		input           string
		content, prefix [][2]int
	}{
		{"<scrXXipt>", [][2]int{{0, 4}, {6, 10}}, [][2]int{{0, 4}, {6, 9}}},
		{"<scriptx>", [][2]int{{0, 7}, {8, 9}}, [][2]int{{0, 7}}},
		{"<script/x>", [][2]int{{0, 8}, {9, 10}}, [][2]int{{0, 7}}},
	} {
		t.Run(c.input, func(t *testing.T) {
			source := NewSource([]byte(c.input))
			whole, _ := source.Span(0, source.Len())
			angle, _ := source.Span(0, 1)
			parts := func(ranges [][2]int) []Segment {
				var out []Segment
				for _, r := range ranges {
					span, _ := source.Span(r[0], r[1])
					raw, _ := source.Text(span)
					out = append(out, Segment{Source: span, Transformation: Exact, Semantic: Some(raw)})
				}
				return out
			}
			_, err := NewNode(NodeSpec{Kind: HTMLBlock, Envelope: whole, Content: parts(c.content), Properties: Properties{HTML: &HTMLProperties{FilteredTags: []HTMLFilteredTag{{Name: "script", Affected: angle, Prefix: Value{Segments: parts(c.prefix), Semantic: Some("<script")}}}}}})
			if err == nil {
				t.Fatal("source gap fabricated original raw prefix/boundary")
			}
		})
	}
}

func TestFilteredWorkArithmeticDoesNotWrap(t *testing.T) {
	// Pure costs exercise the supported 32-bit admission/expansion bounds
	// without allocating a large document or claiming a 32-bit runtime run.
	max32 := int64(math.MaxInt32) / 16
	for _, c := range []struct {
		semantic, segments int64
		bits               int
		want               int64
	}{
		{0, 0, 1, 0}, {7, 1, 1, 89},
		{max32, 1, 28, 8*max32 + 60},
		{3 * max32, max32, 28, 84 * max32},
		{3 * (1 << 30), 1 << 30, 32, 88 * (1 << 30)},
		{math.MaxInt64/8 + 1, 0, 1, math.MaxInt64},
		{0, math.MaxInt64/33 + 1, 1, math.MaxInt64},
		{math.MaxInt64 / 8, 1, 1, math.MaxInt64},
		{-1, 0, 1, math.MaxInt64}, {0, -1, 1, math.MaxInt64}, {0, 0, -1, math.MaxInt64},
	} {
		if got := filteredValueWork(c.semantic, c.segments, c.bits); got != c.want || got < 0 {
			t.Fatal("HTML work arithmetic", c, got)
		}
	}
	cost := filteredValueWork(3*max32, max32, 28)
	if cost <= math.MaxInt32 {
		t.Fatal("32-bit control did not exceed host-int bound")
	}
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
					if stop.exceeded.Name != "work-units" || stop.exceeded.Observed != cost {
						t.Fatal("wide typed charge", stop)
					}
					stopped = true
				}
			}()
			meterHTMLWork(ctx, cost)
		}()
		if stopped != (delta < 0) {
			t.Fatal("wide cost boundary", delta, stopped)
		}
	}
	// A saturated synthetic cost cannot refund work or bypass sticky failure.
	op := budgetFor(t, DefaultLimits())
	ctx := &parserContext{Context: context.Background(), budget: &parseBudget{operation: op}}
	func() {
		defer func() {
			if _, ok := recover().(budgetStop); !ok {
				t.Fatal("saturated cost did not stop")
			}
		}()
		meterHTMLWork(ctx, filteredValueWork(math.MaxInt64, math.MaxInt64, 63))
	}()
	if op.exceeded == nil || op.exceeded.Observed != math.MaxInt64 || op.work < 0 {
		t.Fatal("saturation/sticky accounting")
	}
	if err := op.ChargeWork(0); err == nil {
		t.Fatal("sticky failure reset")
	}
}
