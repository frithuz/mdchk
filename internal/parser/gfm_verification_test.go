package parser

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// These literals and assertions are independent of the HTML/catalog exporter and
// the published expectation projections. Offsets select authored original bytes.
const gfmMixed = "> - [X]\t ~*é*~ www.a.b <title>\n\n" +
	"| a\\|b | `~code~` |\n| :-- | --: |\n| ~short~ |\n| a@b.c | ~~two~~ | `<script>` <!--hidden--> |\n\n" +
	"<https://a.b> [i](/inline) [r][key]\n\n[key]: /first\n[key]: /second\n"

func gfmParse(t *testing.T, s Source, op *OperationBudget, p parserProfile) ParseResult {
	t.Helper()
	r := parseWithProfile(context.Background(), s, op, p)
	requireTableComplete(t, r, s.content)
	return r
}
func gfmSpan(t *testing.T, s Source, span Span, start, end int) {
	t.Helper()
	assertTaskSpan(t, span, start, end)
	if span.source != s {
		t.Fatal("foreign original snapshot")
	}
	if _, err := NewSource(s.Bytes()).Text(span); err == nil {
		t.Fatal("equal-byte foreign source accepted span")
	}
}
func gfmToken(t *testing.T, s Source, span Span, token string) {
	t.Helper()
	start := strings.Index(s.content, token)
	if start < 0 {
		t.Fatal("missing authored token", token)
	}
	gfmSpan(t, s, span, start, start+len(token))
	if tableRaw(t, s, span) != token {
		t.Fatal("original spelling", token)
	}
}
func gfmOpaque(t *testing.T, r ParseResult, op *OperationBudget) {
	t.Helper()
	// The excess third body cell is omitted by rendering but remains source-owned.
	want := []string{"code-span:`~code~`", "inline-html:<title>", "code-span:`<script>`", "inline-html:<!--hidden-->"}
	sort.Strings(want)
	nodes, err := r.Document.ProtectedNodes(context.Background(), op)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range nodes {
		got = append(got, string(n.Kind())+":"+tableRaw(t, r.Source, n.Span()))
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("opaque owners %q want %q", got, want)
	}
	spans, err := r.Document.LiteralSpans(context.Background(), op)
	if err != nil {
		t.Fatal(err)
	}
	got = nil
	for _, s := range spans {
		got = append(got, tableRaw(t, r.Source, s))
	}
	sort.Strings(got)
	want = []string{"~code~", "<title>", "<script>", "<!--hidden-->"}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("literal inventory %q want %q", got, want)
	}
	comments, err := r.Document.Comments(context.Background(), op)
	if err != nil || len(comments) != 1 {
		t.Fatal("comment inventory", comments, err)
	}
	gfmToken(t, r.Source, comments[0].Envelope, "<!--hidden-->")
	if comments[0].Protected {
		t.Fatal("invented raw interior")
	}
	if len(comments[0].Payload) != 1 || tableRaw(t, r.Source, comments[0].Payload[0].Source) != "hidden" {
		t.Fatal("comment payload")
	}
	for _, other := range []*OperationBudget{budgetFor(t, DefaultLimits())} {
		gfmDenied(t, r.Document, other)
	}
	manual, err := NewDocument(r.Document.Root(), r.Document.References())
	if err != nil {
		t.Fatal(err)
	}
	gfmDenied(t, manual, op)
}
func gfmDenied(t *testing.T, d *Document, op *OperationBudget) {
	t.Helper()
	if _, err := d.Comments(context.Background(), op); err == nil {
		t.Fatal("untrusted comments")
	}
	if _, err := d.LiteralSpans(context.Background(), op); err == nil {
		t.Fatal("untrusted literals")
	}
	if _, err := d.ProtectedNodes(context.Background(), op); err == nil {
		t.Fatal("untrusted protection")
	}
}
func TestGFMVerifiedMixedSource(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		t.Run(fmt.Sprintf("ending-%q", ending), func(t *testing.T) {
			input := strings.ReplaceAll(gfmMixed, "\n", ending)
			s := NewSource([]byte(input))
			op := budgetFor(t, DefaultLimits())
			baseline := Parse(context.Background(), s)
			r := gfmParse(t, s, op, gfmInProgressProfile)
			children := r.Document.Root().Children()
			kinds := []Kind{BlockQuote, Table, Paragraph}
			if len(children) != len(kinds) {
				t.Fatal("top-level structure", len(children))
			}
			for i, k := range kinds {
				if children[i].Kind() != k {
					t.Fatal("ordered root", i, children[i].Kind())
				}
			}
			quote := children[0]
			list := quote.Children()[0]
			item := list.Children()[0]
			para := item.Children()[0]
			if list.Kind() != List || item.Kind() != ListItem || para.Kind() != Paragraph {
				t.Fatal("container ancestry")
			}
			task := item.Properties().Task
			if task == nil || !sameTask(task, para.Properties().Task) || !task.Checked || task.CheckSpelling != "X" {
				t.Fatal("exact task", task)
			}
			gfmSpan(t, s, task.Marker, 4, 7)
			gfmSpan(t, s, task.Check, 5, 6)
			gfmSpan(t, s, task.Separator, 7, 8)
			if tableRaw(t, s, para.Content()[0].Source)[0] != ' ' {
				t.Fatal("extra separator space consumed")
			}
			strike := tableNodes(para, Strike)
			if len(strike) != 1 || strike[0].Properties().Delimiter.Width != 1 || strike[0].Children()[0].Kind() != Emphasis {
				t.Fatal("nested single strike")
			}
			gfmToken(t, s, strike[0].Span(), "~*é*~")
			for _, m := range strike[0].Markers() {
				if tableRaw(t, s, m.Source) != "~" {
					t.Fatal("strike delimiter")
				}
			}
			table := children[1]
			tp := table.Properties().Table
			if tp.Width != 2 || !reflect.DeepEqual(tp.Alignments, []TableAlignment{AlignLeft, AlignRight}) {
				t.Fatal("table alignment", tp)
			}
			rows := table.Children()
			if len(rows) != 4 {
				t.Fatal("all source rows")
			}
			for i, w := range []struct {
				role     string
				raw, pad int
				line     string
			}{{"header", 2, 0, "| a\\|b | `~code~` |"}, {"delimiter", 2, 0, "| :-- | --: |"}, {"body", 1, 1, "| ~short~ |"}, {"body", 3, 0, "| a@b.c | ~~two~~ | `<script>` <!--hidden--> |"}} {
				row := rows[i]
				p := row.Properties().Row
				if p.Role != w.role || p.RawCells != w.raw || p.Padding != w.pad || len(row.Children()) != w.raw {
					t.Fatal("raw versus rendered cells", i, p)
				}
				// Row envelopes retain physical endings; each literal line is independently selected.
				start := strings.Index(input, w.line)
				gfmSpan(t, s, row.Span(), start, start+len(w.line)+len(ending))
				if len(p.Separators) != w.raw+1 {
					t.Fatal("separator count", p)
				}
				for _, sep := range p.Separators {
					if tableRaw(t, s, sep) != "|" {
						t.Fatal("raw separator")
					}
				}
				for _, cell := range row.Children() {
					if cell.Kind() != TableCell || cell.Span().source != s {
						t.Fatal("cell owner")
					}
				}
			}

			for i, wants := range [][]string{{" a\\|b ", " `~code~` "}, {" :-- ", " --: "}, {" ~short~ "}, {" a@b.c ", " ~~two~~ ", " `<script>` <!--hidden--> "}} {
				offset := rows[i].Span().Start() + 1
				for j, raw := range wants {
					cell := rows[i].Children()[j]
					gfmSpan(t, s, cell.Span(), offset, offset+len(raw))
					if tableRaw(t, s, cell.Span()) != raw {
						t.Fatal("raw cell slice", i, j)
					}
					content := cell.Content()
					if i == 1 { // Delimiter cells expose alignment markers, not inline Content.
						offset += len(raw) + 1
						continue
					}
					if len(content) == 0 {
						t.Fatal("trimmed cell content missing")
					}
					if content[0].Source.Start() != offset+1 || content[len(content)-1].Source.End() != offset+len(raw)-1 {
						t.Fatal("trimmed cell source", i, j)
					}
					offset += len(raw) + 1
				}
			}
			first := rows[0].Children()[0]
			if len(first.Properties().Cell.EscapedPipes) != 1 {
				t.Fatal("escaped structural pipe")
			}
			gfmToken(t, s, first.Properties().Cell.EscapedPipes[0], "\\|")
			if tableText(first) != "a|b" {
				t.Fatal("trimmed escaped cell", tableText(first))
			}
			links := tableNodes(r.Document.root, Link)
			expected := []struct {
				raw, origin, dest string
				synthetic         bool
			}{{"www.a.b", "bare-gfm", "http://www.a.b", true}, {"a@b.c", "bare-gfm", "mailto:a@b.c", true}, {"<https://a.b>", "autolink", "https://a.b", false}, {"[i](/inline)", "inline", "/inline", false}, {"[r][key]", "full", "/first", true}}
			if len(links) != len(expected) {
				t.Fatal("link inventory", len(links))
			}
			for i, w := range expected {
				n := links[i]
				p := n.Properties().Link
				gfmToken(t, s, n.Span(), w.raw)
				if p.Origin != w.origin || p.Destination.Semantic != Some(w.dest) || p.Resolution != Available {
					t.Fatal("link origin/destination", i, p)
				}
				synthetic := false
				for _, part := range p.Destination.Segments {
					if part.Source.Present() {
						if part.Source.source != s || part.Transformation == Synthetic {
							t.Fatal("invented destination source")
						}
					} else {
						if part.Transformation != Synthetic {
							t.Fatal("spanless nonsynthetic destination")
						}
						synthetic = true
					}
				}
				if synthetic != w.synthetic {
					t.Fatal("destination provenance", i)
				}
			}
			refs := r.Document.References()
			if len(refs.Definitions) != 2 || refs.Lookup["key"] != 0 {
				t.Fatal("first reference definition")
			}
			gfmToken(t, s, refs.Definitions[0].Properties().Destination.Segments[0].Source, "/first")
			filter := assertFiltered(t, r, []int{strings.Index(input, "<title>")})
			if filter[0].owner.Kind() != InlineHTML || filter[0].fact.Name != "title" || filter[0].fact.Tag == nil {
				t.Fatal("filter raw owner")
			}
			gfmToken(t, s, filter[0].fact.Affected, "<") // first original '<' is the raw title.
			gfmOpaque(t, r, op)
			// Mutating detached accessors never changes the stored source/model.
			root := r.Document.Root()
			*root = Node{}
			handles := r.Document.Root().Children()
			*handles[0] = Node{}
			props := table.Properties()
			props.Table.Alignments[0] = AlignNone
			task.Segments[0] = Span{}
			links[0].Properties().Link.Destination.Segments[0] = Segment{}
			bytes := s.Bytes()
			bytes[0] = '!'
			// Fresh accessors on this SAME integrated document must still expose
			// authored values. A shallow saved NodeSpec would alias descendants
			// and could change together with the document under a broken accessor.
			freshRoot := r.Document.Root()
			t.Run("detached-handles-and-source", func(t *testing.T) {
				if freshRoot.Kind() != DocumentKind || len(freshRoot.Children()) != 3 {
					t.Fatal("returned root handle changed stored tree")
				}
				gfmSpan(t, s, freshRoot.Span(), 0, len(input))
				quote := freshRoot.Children()[0]
				if quote.Kind() != BlockQuote || len(quote.Children()) != 1 ||
					quote.Children()[0].Kind() != List || quote.Children()[0].Children()[0].Kind() != ListItem ||
					quote.Children()[0].Children()[0].Children()[0].Kind() != Paragraph {
					t.Fatal("returned quote handle changed stored ancestry")
				}
				if string(s.Bytes()) != input || r.Document.Source() != s {
					t.Fatal("returned source bytes changed original snapshot")
				}
			})
			t.Run("detached-table-alignment", func(t *testing.T) {
				got := freshRoot.Children()[1].Properties().Table
				if got == nil || !reflect.DeepEqual(got.Alignments, []TableAlignment{AlignLeft, AlignRight}) {
					t.Fatal("returned alignment slice changed stored table")
				}
			})
			t.Run("detached-task-segments", func(t *testing.T) {
				freshItem := freshRoot.Children()[0].Children()[0].Children()[0]
				got := freshItem.Properties().Task
				if got == nil || len(got.Segments) != 1 || got.CheckSpelling != "X" || !got.Checked ||
					!sameTask(got, freshItem.Children()[0].Properties().Task) {
					t.Fatal("returned task slice changed stored item/paragraph agreement")
				}
				gfmSpan(t, s, got.Segments[0], 4, 7)
				gfmSpan(t, s, got.Marker, 4, 7)
				gfmSpan(t, s, got.Separator, 7, 8)
			})
			t.Run("detached-bare-destination", func(t *testing.T) {
				got := tableNodes(freshRoot, Link)[0].Properties().Link
				if got == nil || got.Origin != "bare-gfm" || got.Destination.Semantic != Some("http://www.a.b") ||
					len(got.Destination.Segments) != 2 {
					t.Fatal("returned destination slice changed stored bare link")
				}
				prefix, source := got.Destination.Segments[0], got.Destination.Segments[1]
				if prefix.Source.Present() || prefix.Transformation != Synthetic || prefix.Semantic != Some("http://") ||
					source.Transformation != Exact || source.Semantic != Some("www.a.b") {
					t.Fatal("returned destination slice changed stored provenance")
				}
				gfmToken(t, s, source.Source, "www.a.b")
			})
			if after := Parse(context.Background(), s); !reflect.DeepEqual(baseline, after) {
				t.Fatal("public core profile leaked")
			}
		})
	}
}

func TestGFMVerifiedExcludedSyntax(t *testing.T) {
	cases := []struct {
		input string
		top   []Kind
		text  string
		links int
	}{
		{"[^n]: /target\n\n[^n]\n", []Kind{Paragraph}, "^n", 1},
		{"[^missing]\n", []Kind{Paragraph}, "[^missing]", 0},
		{"> [!NOTE]\n> ordinary\n", []Kind{BlockQuote}, "[!NOTE]ordinary", 0},
		{":smile: $x$\n", []Kind{Paragraph}, ":smile: $x$", 0},
		{"$$\nx\n$$\n", []Kind{Paragraph}, "$$x$$", 0},
		{"---\nname: value\n---\n", []Kind{ThematicBreak, Heading}, "name: value", 0},
		{"+++\nname: value\n+++\n", []Kind{Paragraph}, "+++name: value+++", 0},
	}
	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			s := NewSource([]byte(c.input))
			core := gfmParse(t, s, budgetFor(t, DefaultLimits()), coreProfile)
			for _, p := range []parserProfile{coreProfile, gfmInProgressProfile} {
				r := gfmParse(t, s, budgetFor(t, DefaultLimits()), p)
				if !reflect.DeepEqual(core, r) {
					t.Fatal("excluded syntax changed ordinary interpretation")
				}
				children := r.Document.Root().Children()
				if len(children) != len(c.top) {
					t.Fatal("ordinary blocks")
				}
				for i, k := range c.top {
					if children[i].Kind() != k {
						t.Fatal("ordinary kind", i)
					}
				}
				if tableText(r.Document.root) != c.text || len(tableNodes(r.Document.root, Link)) != c.links {
					t.Fatal("ordinary text/link", tableText(r.Document.root))
				}
				if c.links == 1 {
					refs := r.Document.References()
					if len(refs.Definitions) != 1 || refs.Lookup["^n"] != 0 || tableNodes(r.Document.root, Link)[0].Properties().Link.Destination.Semantic != Some("/target") {
						t.Fatal("ordinary reference resembling footnote")
					}
				}
			}
		})
	}
}

func TestGFMVerifiedScopeConcurrency(t *testing.T) {
	s := NewSource([]byte(gfmMixed))
	baseline := Parse(context.Background(), s)
	shared := budgetFor(t, DefaultLimits())
	start := make(chan struct{})
	var ready, syncDone sync.WaitGroup
	profiles := []parserProfile{coreProfile, tablesOnlyProfile, tasksOnlyProfile, strikeOnlyProfile, bareOnlyProfile, filterOnlyProfile, gfmInProgressProfile}
	for _, p := range profiles {
		for _, useShared := range []bool{false, true} {
			ready.Add(1)
			syncDone.Add(1)
			go func(p parserProfile, useShared bool) {
				defer syncDone.Done()
				ready.Done()
				<-start
				op := shared
				if !useShared {
					op = budgetFor(t, DefaultLimits())
				}
				for j := 0; j < 3; j++ {
					r := gfmParse(t, s, op, p)
					if p == coreProfile && !reflect.DeepEqual(baseline, r) {
						t.Error("core tree changed")
					}
					for _, c := range []struct {
						feature parserProfile
						actual  bool
					}{{tablesOnlyProfile, len(tableNodes(r.Document.root, Table)) == 1}, {tasksOnlyProfile, len(taskItems(r.Document.root)) == 1}, {strikeOnlyProfile, len(tableNodes(r.Document.root, Strike)) > 0}, {bareOnlyProfile, len(bareNodes(r.Document.root)) > 0}, {filterOnlyProfile, len(filteredActuals(r.Document.root)) == 1}} {
						if c.actual != p.enabled(c.feature) {
							t.Error("scope metadata leaked", p, c.feature, c.actual)
						}
					}
					if !reflect.DeepEqual(baseline, Parse(context.Background(), s)) {
						t.Error("ordinary Parse inherited scope")
					}
				}
			}(p, useShared)
		}
	}
	ready.Wait()
	close(start)
	syncDone.Wait()
}

func TestGFMVerifiedResources(t *testing.T) {
	s := NewSource([]byte(gfmMixed))
	baseline := budgetFor(t, DefaultLimits())
	gfmParse(t, s, baseline, gfmInProgressProfile)
	for name, used := range map[string]int64{"work-units": baseline.work, "document-nodes": baseline.nodes, "total-nodes": baseline.nodes} {
		for _, delta := range []int64{-1, 0, 1} {
			t.Run(fmt.Sprintf("%s/%d", name, delta), func(t *testing.T) {
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
				r := parseWithProfile(context.Background(), s, op, gfmInProgressProfile)
				if r.EffectiveLimits != l {
					t.Fatal("effective limits")
				}
				if delta < 0 {
					requireLimit(t, r, name)
					gfmDenied(t, r.Document, op)
				} else {
					requireTableComplete(t, r, gfmMixed)
				}
			})
		}
	}
	for _, name := range []string{"work-units", "total-nodes", "operation-time"} {
		t.Run("sticky-"+name, func(t *testing.T) {
			l := DefaultLimits()
			coreOp := budgetFor(t, l)
			gfmParse(t, s, coreOp, coreProfile)
			// Real source consumer charges share the same operation before the core reparse.
			probe := budgetFor(t, l)
			r := gfmParse(t, s, probe, gfmInProgressProfile)
			gfmOpaque(t, r, probe)
			if name == "work-units" {
				l.WorkUnits = probe.work + coreOp.work
			}
			if name == "total-nodes" {
				l.TotalNodes = probe.nodes + coreOp.nodes
				l.DocumentNodes = l.TotalNodes
			}
			op := budgetFor(t, l)
			r = gfmParse(t, s, op, gfmInProgressProfile)
			gfmOpaque(t, r, op)
			gfmParse(t, s, op, coreProfile)
			if name == "operation-time" {
				op.started = time.Now().Add(-2 * l.OperationTime)
			}
			r = parseWithProfile(context.Background(), s, op, gfmInProgressProfile)
			requireLimit(t, r, name)
			gfmDenied(t, r.Document, op)
			r.Limit.Name = "mutated"
			r.EffectiveLimits.WorkUnits = 0
			requireLimit(t, ParseWithBudget(context.Background(), s, op), name)
			requireLimit(t, parseWithProfile(context.Background(), s, op, gfmInProgressProfile), name)
			var limit *LimitExceeded
			if !errors.As(op.ChargeWork(0), &limit) || limit.Name != name {
				t.Fatal("sticky detached charge error", limit)
			}
			limit.Name = "mutated"
			if !errors.As(op.ChargeWork(0), &limit) || limit.Name != name {
				t.Fatal("charge error mutated state")
			}
		})
	}
	// Table/row/cell, quote/list/item, paragraph/Strike/Link each count syntax;
	// task/filter metadata adds no depth. These independently known chains are 6.
	for _, input := range []string{"> - [x] ~~www.a.b~~ <title>\n", "> | a |\n> | - |\n> | ~~www.a.b~~ |\n"} {
		for _, allowed := range []int64{5, 6, 7} {
			l := DefaultLimits()
			l.SyntaxDepth = allowed
			r := parseWithProfile(context.Background(), NewSource([]byte(input)), budgetFor(t, l), gfmInProgressProfile)
			if allowed < 6 {
				requireLimit(t, r, "syntax-depth")
				if r.Limit.Observed != 6 {
					t.Fatal("late depth")
				}
			} else {
				requireTableComplete(t, r, input)
				if r.Document.root.depth != 6 {
					t.Fatal("mixed depth", r.Document.root.depth)
				}
			}
		}
	}
	for _, deadline := range []bool{false, true} {
		complete := false
		for checkpoint := 1; checkpoint < 10000; checkpoint++ {
			ctx := &quoteCheckpoint{checkpointContext: checkpointContext{Context: context.Background(), remaining: checkpoint}, deadline: deadline}
			op := budgetFor(t, DefaultLimits())
			r := parseWithProfile(ctx, s, op, gfmInProgressProfile)
			if r.Status == Complete {
				complete = true
				break
			}
			reason := Cancelled
			if deadline {
				reason = DeadlineExceeded
			}
			if r.Status != Incomplete || r.AST != Partial || r.Failure != nil || r.Interruption != reason || len(r.Uncertainty) != 1 || r.Document.references.LookupAvailability == Available {
				t.Fatal("mixed interrupted publication", r)
			}
			gfmSpan(t, s, r.Uncertainty[0].Region, 0, s.Len())
			gfmDenied(t, r.Document, op)
		}
		if !complete {
			t.Fatal("checkpoint sweep failed to finish")
		}
	}
	// Encoding/admission precedence also holds at the integrated entry point.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	bad := NewSource([]byte(gfmMixed + "\xff"))
	r := parseWithProfile(ctx, bad, budgetFor(t, DefaultLimits()), gfmInProgressProfile)
	if r.Failure == nil || r.Failure.Code != InvalidUTF8 {
		t.Fatal("encoding precedence")
	}
	l := DefaultLimits()
	l.DocumentBytes = 1
	requireLimit(t, parseWithProfile(ctx, bad, budgetFor(t, l), gfmInProgressProfile), "document-bytes")
}

func gfmAdversarial(n int) []string {
	return []string{
		gfmMixed + "\n| a | b |\n| - | - |\n| " + strings.Repeat("x|", n) + "\n",
		gfmMixed + "\n" + strings.Repeat("~* [~~~]( ", n) + strings.Repeat("*~]", n),
		gfmMixed + "\n" + strings.Repeat("www.a.b/a&x;&y;). a@bad_.a_ xmpp:a@b.c/r/s ", n),
		gfmMixed + "\n<div>\n" + strings.Repeat("<title x=' <scriptx <!--hidden--> ", n),
		gfmMixed + "\n| a | b |\n| " + strings.Repeat("- |", n) + "\n",
	}
}
func TestGFMVerifiedAdversarial(t *testing.T) {
	for _, n := range []int{16, 32, 64} {
		for family, input := range gfmAdversarial(n) {
			l := DefaultLimits()
			l.WorkUnits = 500000
			op := budgetFor(t, l)
			r := parseWithProfile(context.Background(), NewSource([]byte(input)), op, gfmInProgressProfile)
			if r.Limit != nil {
				requireLimit(t, r, r.Limit.Name)
				gfmDenied(t, r.Document, op)
			} else {
				requireTableComplete(t, r, input)
			}
			t.Logf("family=%d size=%d bytes=%d outcome=%s limit=%v work=%d construction=%d", family, n, len(input), r.Status, r.Limit, op.work, op.nodes)
		}
	}
}
