package parser_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func span(t *testing.T, source parser.Source, start, end int) parser.Span {
	t.Helper()
	s, err := source.Span(start, end)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func node(t *testing.T, spec parser.NodeSpec) *parser.Node {
	t.Helper()
	n, err := parser.NewNode(spec)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func raw(s parser.Span) parser.Segment {
	return parser.Segment{Source: s, Transformation: parser.Exact}
}

func TestSpanOwnershipBoundsAndAbsence(t *testing.T) {
	s := parser.NewSource([]byte("é\t\r\nZ"))
	if s.Len() != 6 {
		t.Fatalf("byte length = %d, want 6", s.Len())
	}
	for _, bad := range [][2]int{{-1, 0}, {2, 1}, {0, 7}, {7, 7}} {
		if _, err := s.Span(bad[0], bad[1]); !errors.Is(err, parser.ErrRange) {
			t.Errorf("Span%v = %v", bad, err)
		}
	}
	zero, eof := span(t, s, 0, 0), span(t, s, 6, 6)
	if !zero.Present() || !eof.Present() || (parser.Span{}).Present() {
		t.Fatal("absent and zero-width ranges conflated")
	}
	for _, r := range []parser.Span{zero, eof} {
		if got, err := s.Text(r); err != nil || got != "" {
			t.Fatalf("empty source slice = %q, %v", got, err)
		}
	}
	if got, err := s.Text(span(t, s, 0, 2)); err != nil || got != "é" {
		t.Fatalf("Unicode bytes = %q, %v", got, err)
	}
	if got, err := s.Text(span(t, s, 2, 5)); err != nil || got != "\t\r\n" {
		t.Fatalf("unchanged endings = %q, %v", got, err)
	}
	other := parser.NewSource(s.Bytes())
	if _, err := other.Text(span(t, s, 0, 2)); !errors.Is(err, parser.ErrRange) {
		t.Fatal("accepted range from independently acquired identical bytes")
	}
	copyOfSource := s
	if _, err := copyOfSource.Text(span(t, s, 0, 2)); err != nil {
		t.Fatal("Source copy lost snapshot ownership")
	}
	if _, err := s.Text(parser.Span{}); !errors.Is(err, parser.ErrRange) {
		t.Fatal("accepted absent span")
	}
	var empty parser.Source
	if got, err := empty.Text(span(t, empty, 0, 0)); err != nil || got != "" {
		t.Fatalf("zero Source range = %q, %v", got, err)
	}
}

func TestConstructedQuoteSegmentsAndTraversal(t *testing.T) {
	// Constructed model, not parser output. Expected offsets are literal byte facts.
	input := []byte("> a\r\n> b")
	s := parser.NewSource(input)
	a := node(t, parser.NodeSpec{Kind: parser.Text, Envelope: span(t, s, 2, 5), Content: []parser.Segment{raw(span(t, s, 2, 5))}})
	b := node(t, parser.NodeSpec{Kind: parser.Text, Envelope: span(t, s, 7, 8), Content: []parser.Segment{raw(span(t, s, 7, 8))}})
	children := []*parser.Node{a, b}
	paragraph := node(t, parser.NodeSpec{Kind: parser.Paragraph, Envelope: span(t, s, 2, 8), Content: []parser.Segment{raw(span(t, s, 2, 5)), raw(span(t, s, 7, 8))}, Children: children})
	markers := []parser.Marker{{Role: "quote-prefix", Source: span(t, s, 0, 2)}, {Role: "quote-prefix", Source: span(t, s, 5, 7)}}
	quote := node(t, parser.NodeSpec{Kind: parser.BlockQuote, Envelope: span(t, s, 0, 8), Markers: markers, Children: []*parser.Node{paragraph}})
	root := node(t, parser.NodeSpec{Kind: parser.DocumentKind, Envelope: span(t, s, 0, 8), Children: []*parser.Node{quote}})
	input[2] = 'X'
	children[0] = b
	markers[0].Role = "changed"
	for i, want := range []string{"a\r\n", "b"} {
		got, err := s.Text(paragraph.Content()[i].Source)
		if err != nil || got != want {
			t.Fatalf("segment %d = %q, %v", i, got, err)
		}
	}
	if got, _ := s.Text(quote.Markers()[1].Source); got != "> " {
		t.Fatalf("prefix = %q", got)
	}
	if quote.Markers()[0].Role != "quote-prefix" {
		t.Fatal("constructor retained mutable marker slice")
	}
	paragraph.Children()[0] = b
	quote.Markers()[0].Role = "changed again"
	paragraph.Content()[0].Transformation = parser.Synthetic
	for round := 0; round < 2; round++ {
		var got []parser.Kind
		parser.Walk(root, func(n *parser.Node) bool { got = append(got, n.Kind()); return true })
		want := []parser.Kind{parser.DocumentKind, parser.BlockQuote, parser.Paragraph, parser.Text, parser.Text}
		if !reflect.DeepEqual(got, want) || paragraph.Children()[0].Span() != a.Span() || paragraph.Content()[0].Transformation != parser.Exact {
			t.Fatalf("unstable/aliased traversal: %v", got)
		}
	}
	visits := 0
	parser.Walk(root, func(*parser.Node) bool { visits++; return visits < 2 })
	if visits != 2 {
		t.Fatalf("stop visited %d nodes", visits)
	}
	parser.Walk(nil, func(*parser.Node) bool { t.Fatal("visited nil"); return true })
}

func TestConstructedSemanticProvenance(t *testing.T) {
	s := parser.NewSource([]byte("&amp;\\*\t"))
	segments := []parser.Segment{
		{Source: span(t, s, 0, 5), Transformation: parser.Entity, Semantic: parser.Some("&")},
		{Source: span(t, s, 5, 7), Transformation: parser.Escape, Semantic: parser.Some("*")},
		{Source: span(t, s, 7, 8), Transformation: parser.PartialTab, Semantic: parser.Some("  ")},
		{Transformation: parser.Synthetic, Semantic: parser.Some("synthetic")},
		{Transformation: parser.MappingUnavailable},
	}
	n := node(t, parser.NodeSpec{Kind: parser.Text, Envelope: span(t, s, 0, 8), Content: segments, Properties: parser.Properties{Literal: parser.Some("&*  synthetic")}})
	segments[0].Semantic.Value = "mutated"
	if got, _ := s.Text(n.Content()[0].Source); got != "&amp;" || n.Content()[0].Semantic.Value != "&" {
		t.Fatal("semantic value replaced raw entity provenance")
	}
	if got, _ := s.Text(n.Content()[1].Source); got != "\\*" {
		t.Fatalf("escape spelling = %q", got)
	}
	if n.Content()[3].Source.Present() || n.Properties().Literal.Value != "&*  synthetic" {
		t.Fatal("synthetic text masquerades as source")
	}
}

func TestNodeRejectsInvalidSourceAssociations(t *testing.T) {
	s := parser.NewSource([]byte("abcd"))
	other := parser.NewSource([]byte("abcd"))
	outer := span(t, s, 1, 3)
	child := node(t, parser.NodeSpec{Kind: parser.Text, Envelope: span(t, s, 0, 1)})
	foreign := node(t, parser.NodeSpec{Kind: parser.Text, Envelope: span(t, other, 1, 2)})
	tests := []parser.NodeSpec{
		{Kind: parser.Text},
		{Kind: "unknown", Envelope: outer},
		{Kind: parser.Text, Envelope: outer, Children: []*parser.Node{child}},
		{Kind: parser.Text, Envelope: outer, Children: []*parser.Node{foreign}},
		{Kind: parser.Text, Envelope: outer, Children: []*parser.Node{nil}},
		{Kind: parser.Text, Envelope: outer, Markers: []parser.Marker{{Role: "marker", Source: span(t, other, 1, 2)}}},
		{Kind: parser.Text, Envelope: outer, Content: []parser.Segment{raw(span(t, s, 0, 1))}},
		{Kind: parser.Text, Envelope: outer, Content: []parser.Segment{{Transformation: parser.Exact}}},
		{Kind: parser.Text, Envelope: outer, Content: []parser.Segment{{Source: outer, Transformation: parser.Exact, Semantic: parser.Some("wrong")}}},
		{Kind: parser.Text, Envelope: outer, Content: []parser.Segment{{Source: outer, Transformation: "unknown"}}},
		{Kind: parser.Text, Envelope: outer, Content: []parser.Segment{raw(outer)}, Properties: parser.Properties{Literal: parser.Some("decoded without provenance")}},
		{Kind: parser.Text, Envelope: outer, Properties: parser.Properties{Heading: &parser.HeadingProperties{Level: 1, Style: "atx"}}},
		{Kind: parser.Heading, Envelope: outer, Properties: parser.Properties{Heading: &parser.HeadingProperties{Level: 7, Style: "atx"}}},
	}
	for i, spec := range tests {
		if _, err := parser.NewNode(spec); !errors.Is(err, parser.ErrModel) {
			t.Errorf("case %d accepted: %v", i, err)
		}
	}
	late := node(t, parser.NodeSpec{Kind: parser.Text, Envelope: span(t, s, 2, 2)})
	early := node(t, parser.NodeSpec{Kind: parser.Text, Envelope: span(t, s, 1, 1)})
	if _, err := parser.NewNode(parser.NodeSpec{Kind: parser.Paragraph, Envelope: outer, Children: []*parser.Node{late, early}}); !errors.Is(err, parser.ErrModel) {
		t.Fatal("accepted reversed children")
	}
	otherAtSameStart := node(t, parser.NodeSpec{Kind: parser.SoftBreak, Envelope: span(t, s, 1, 1)})
	equal := node(t, parser.NodeSpec{Kind: parser.Paragraph, Envelope: outer, Children: []*parser.Node{early, otherAtSameStart, late}})
	if !reflect.DeepEqual(equal.Children(), []*parser.Node{early, otherAtSameStart, late}) {
		t.Fatal("reordered equal-start children")
	}
}

func TestPropertiesSnapshotNestedValues(t *testing.T) {
	s := parser.NewSource([]byte("[x](a)"))
	e := span(t, s, 0, 6)
	label := parser.Value{Segments: []parser.Segment{raw(span(t, s, 1, 2))}, Semantic: parser.Some("x")}
	title := parser.Value{Semantic: parser.Some("")}
	p := &parser.LinkProperties{Origin: "inline", Label: label, Destination: parser.Value{Segments: []parser.Segment{raw(span(t, s, 4, 5))}, Semantic: parser.Some("a")}, Title: &title, Resolution: parser.Available}
	n := node(t, parser.NodeSpec{Kind: parser.Link, Envelope: e, Properties: parser.Properties{Link: p}})
	p.Label.Segments[0].Transformation = parser.Synthetic
	title.Semantic.Value = "changed"
	got := n.Properties()
	got.Link.Label.Segments[0].Transformation = parser.MappingUnavailable
	got.Link.Title.Semantic.Value = "also changed"
	if n.Properties().Link.Label.Segments[0].Transformation != parser.Exact || n.Properties().Link.Title.Semantic.Value != "" {
		t.Fatal("nested property alias escaped ownership boundary")
	}
	foreign := parser.NewSource([]byte("[x](a)"))
	p.Label.Segments[0] = raw(span(t, foreign, 1, 2))
	if _, err := parser.NewNode(parser.NodeSpec{Kind: parser.Link, Envelope: e, Properties: parser.Properties{Link: p}}); !errors.Is(err, parser.ErrModel) {
		t.Fatal("accepted foreign nested property range")
	}
	info := parser.Value{Segments: []parser.Segment{raw(span(t, s, 0, 1))}}
	code := node(t, parser.NodeSpec{Kind: parser.CodeBlock, Envelope: e, Properties: parser.Properties{Code: &parser.CodeProperties{Style: "fenced", Fence: parser.Some(byte('`')), Width: parser.Some(3), Info: &info}}})
	info.Segments[0].Transformation = parser.Synthetic
	code.Properties().Code.Info.Segments[0].Transformation = parser.Synthetic
	if code.Properties().Code.Info.Segments[0].Transformation != parser.Exact {
		t.Fatal("code info aliases source metadata")
	}
}

func TestListItemMetadataValidationAndOwnership(t *testing.T) {
	s := parser.NewSource([]byte("-\ttext\n"))
	e := span(t, s, 0, 7)
	prefix := parser.Value{Semantic: parser.Some("-   "), Segments: []parser.Segment{
		{Source: span(t, s, 0, 1), Transformation: parser.Exact, Semantic: parser.Some("-")},
		{Source: span(t, s, 1, 2), Transformation: parser.PartialTab, Semantic: parser.Some("   ")},
	}}
	p := &parser.ListProperties{Bullet: parser.Some(byte('-')), MarkerWidth: parser.Some(1), Padding: parser.Some(3), ContentIndent: parser.Some(4), Prefix: &prefix}
	n := node(t, parser.NodeSpec{Kind: parser.ListItem, Envelope: e, Properties: parser.Properties{List: p}})
	p.Padding = parser.Some(1)
	prefix.Segments[0].Transformation = parser.Synthetic
	prefix.Semantic.Value = "changed"
	got := n.Properties().List
	got.Prefix.Segments[1].Semantic.Value = "changed"
	*got.Prefix = parser.Value{}
	got.MarkerWidth = parser.Some(8)
	if frozen := n.Properties().List; frozen.Padding != parser.Some(3) || frozen.MarkerWidth != parser.Some(1) || frozen.Prefix.Semantic != parser.Some("-   ") || frozen.Prefix.Segments[0].Transformation != parser.Exact || frozen.Prefix.Segments[1].Semantic != parser.Some("   ") {
		t.Fatal("item prefix escaped constructor/accessor snapshot boundary", frozen)
	}
	foreign := parser.NewSource([]byte("-\ttext\n"))
	for name, change := range map[string]func(*parser.ListProperties){
		"bullet width": func(p *parser.ListProperties) { p.MarkerWidth = parser.Some(2) },
		"ordered width": func(p *parser.ListProperties) {
			p.Ordered = true
			p.Bullet = parser.Optional[byte]{}
			p.MarkerWidth = parser.Some(1)
		},
		"zero padding":         func(p *parser.ListProperties) { p.Padding = parser.Some(0) },
		"excess padding":       func(p *parser.ListProperties) { p.Padding = parser.Some(5) },
		"small indent":         func(p *parser.ListProperties) { p.ContentIndent = parser.Some(1) },
		"large indent":         func(p *parser.ListProperties) { p.ContentIndent = parser.Some(18) },
		"four leading columns": func(p *parser.ListProperties) { p.ContentIndent = parser.Some(8) },
		"foreign prefix":       func(p *parser.ListProperties) { p.Prefix.Segments[0].Source = span(t, foreign, 0, 1) },
		"unmapped prefix semantics": func(p *parser.ListProperties) {
			p.Prefix = &parser.Value{Segments: []parser.Segment{raw(span(t, s, 0, 1))}, Semantic: parser.Some("wrong")}
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := n.Properties().List
			change(p)
			if _, err := parser.NewNode(parser.NodeSpec{Kind: parser.ListItem, Envelope: e, Properties: parser.Properties{List: p}}); !errors.Is(err, parser.ErrModel) {
				t.Fatal("invalid item facts accepted", err)
			}
		})
	}
	if _, err := parser.NewNode(parser.NodeSpec{Kind: parser.List, Envelope: e, Properties: parser.Properties{List: n.Properties().List}}); !errors.Is(err, parser.ErrModel) {
		t.Fatal("item-only metadata accepted on list")
	}
	// Unknown first-line facts remain absent on existing constructed fixtures.
	if _, err := parser.NewNode(parser.NodeSpec{Kind: parser.ListItem, Envelope: e, Properties: parser.Properties{List: &parser.ListProperties{}}}); err != nil {
		t.Fatal("optional item facts made mandatory", err)
	}
}

func TestReferenceRecordsKeepDuplicatesSeparateFromLookup(t *testing.T) {
	s := parser.NewSource([]byte("[x]: /a\n[x]: /b\n"))
	makeReference := func(start int) *parser.ReferenceDefinition {
		spec := parser.ReferenceSpec{Envelope: span(t, s, start, start+8), Markers: []parser.Marker{{Role: "label-open", Source: span(t, s, start, start+1)}}, Label: parser.Value{Segments: []parser.Segment{raw(span(t, s, start+1, start+2))}, Semantic: parser.Some("x")}, Destination: parser.Value{Segments: []parser.Segment{raw(span(t, s, start+5, start+7))}}, NormalizedLabel: parser.Some("x")}
		r, err := parser.NewReferenceDefinition(spec)
		if err != nil {
			t.Fatal(err)
		}
		spec.Label.Segments[0].Transformation = parser.Synthetic
		return r
	}
	first, second := makeReference(0), makeReference(8)
	root := node(t, parser.NodeSpec{Kind: parser.DocumentKind, Envelope: span(t, s, 0, 16)})
	refs := parser.ReferenceCollection{Availability: parser.Available, LookupAvailability: parser.Available, Definitions: []*parser.ReferenceDefinition{first, second}, Lookup: map[string]int{"x": 0}}
	doc, err := parser.NewDocument(root, refs)
	if err != nil {
		t.Fatal(err)
	}
	refs.Definitions[0] = second
	refs.Lookup["x"] = 1
	got := doc.References()
	got.Definitions[0] = second
	got.Lookup["x"] = 1
	gotRecord := first.Properties()
	gotRecord.Label.Segments[0].Transformation = parser.MappingUnavailable
	if len(doc.Root().Children()) != 0 || len(doc.References().Definitions) != 2 || doc.References().Lookup["x"] != 0 || doc.References().Definitions[0].Properties().Envelope != first.Properties().Envelope {
		t.Fatal("definition retention/effective lookup conflated or aliased")
	}
	for i, want := range []string{"/a", "/b"} {
		r := doc.References().Definitions[i].Properties()
		text, err := s.Text(r.Destination.Segments[0].Source)
		if err != nil || text != want || r.Label.Segments[0].Transformation != parser.Exact {
			t.Fatalf("record %d = %q, %v", i, text, err)
		}
	}
	bad := doc.References()
	bad.Lookup["x"] = 1
	if _, err := parser.NewDocument(root, bad); !errors.Is(err, parser.ErrModel) {
		t.Fatal("allowed later duplicate to supersede first definition")
	}
	bad = doc.References()
	bad.Definitions[0], bad.Definitions[1] = bad.Definitions[1], bad.Definitions[0]
	if _, err := parser.NewDocument(root, bad); !errors.Is(err, parser.ErrModel) {
		t.Fatal("accepted reversed definition order")
	}
	foreign := node(t, parser.NodeSpec{Kind: parser.DocumentKind, Envelope: span(t, parser.NewSource(s.Bytes()), 0, 16)})
	if _, err := parser.NewDocument(foreign, doc.References()); !errors.Is(err, parser.ErrModel) {
		t.Fatal("accepted foreign reference records")
	}
	bad = doc.References()
	bad.LookupAvailability = parser.Unavailable
	if _, err := parser.NewDocument(root, bad); !errors.Is(err, parser.ErrModel) {
		t.Fatal("unavailable lookup had effective entries")
	}
}
