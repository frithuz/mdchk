package parser_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func assertSegment(t *testing.T, source parser.Source, segment parser.Segment, start, end int, transformation parser.Transformation, raw, semantic string) {
	t.Helper()
	text, err := source.Text(segment.Source)
	if err != nil || !segment.Source.Present() || segment.Source.Start() != start || segment.Source.End() != end || segment.Transformation != transformation || text != raw || segment.Semantic != parser.Some(semantic) {
		t.Fatalf("segment [%d,%d) %s raw=%q semantic=%+v, err=%v; want [%d,%d) %s raw=%q semantic=%q", segment.Source.Start(), segment.Source.End(), segment.Transformation, text, segment.Semantic, err, start, end, transformation, raw, semantic)
	}
}

func TestSemanticNULProvenance(t *testing.T) {
	for _, input := range []string{"a\x00b", "é\t\x00\r\nZ", "\x00\x00", "\ufffd\x00", "", "plain\ttext"} {
		source := parser.NewSource([]byte(input))
		index := parser.NewLineIndex(source)
		span, _ := source.Span(0, source.Len())
		value, err := index.SemanticText(span)
		want := strings.ReplaceAll(input, "\x00", "\ufffd")
		if err != nil || value.Semantic != parser.Some(want) {
			t.Fatalf("semantic %q, %v; want %q", value.Semantic.Value, err, want)
		}
		var raw, semantic strings.Builder
		for _, segment := range value.Segments {
			part, err := source.Text(segment.Source)
			if err != nil {
				t.Fatal(err)
			}
			raw.WriteString(part)
			semantic.WriteString(segment.Semantic.Value)
			if part == "\x00" {
				if segment.Transformation != parser.Normalized || segment.Semantic != parser.Some("\ufffd") {
					t.Fatal("NUL transformation is not explicit")
				}
			} else if segment.Transformation != parser.Exact || part != segment.Semantic.Value {
				t.Fatal("unchanged bytes lost exact provenance")
			}
		}
		if raw.String() != input || semantic.String() != want || string(source.Bytes()) != input {
			t.Fatal("semantic output shifted or overwrote raw bytes")
		}
		if len(value.Segments) > 0 {
			before, _ := index.SemanticText(span)
			value.Segments[0].Semantic = parser.Some("caller mutation")
			again, _ := index.SemanticText(span)
			if !reflect.DeepEqual(before, again) {
				t.Fatal("returned semantic slices alias later outputs")
			}
		}
	}
	for _, input := range []string{"a\x00b", "é\t\x00\r\nZ"} {
		source := parser.NewSource([]byte(input))
		index := parser.NewLineIndex(source)
		span, _ := source.Span(0, source.Len())
		value, _ := index.SemanticText(span)
		if len(value.Segments) != 3 {
			t.Fatal("expected exact/NUL/exact sequence")
		}
		nul := 1
		if input[0] != 'a' {
			nul = 3
		}
		assertSegment(t, source, value.Segments[1], nul, nul+1, parser.Normalized, "\x00", "\ufffd")
		// Actual helper output must be usable by the existing provenance model.
		if _, err := parser.NewNode(parser.NodeSpec{Kind: parser.Text, Envelope: span, Content: value.Segments, Properties: parser.Properties{Literal: value.Semantic}}); err != nil {
			t.Fatalf("helper-produced provenance rejected by model: %v", err)
		}
	}
	source := parser.NewSource([]byte("é\r\n\ufffd"))
	index := parser.NewLineIndex(source)
	for _, pair := range [][2]int{{0, 1}, {1, 1}, {1, 2}, {4, 5}, {5, 5}} {
		span, _ := source.Span(pair[0], pair[1])
		if _, err := index.SemanticText(span); !errors.Is(err, parser.ErrPosition) {
			t.Fatalf("interior scalar span %v accepted: %v", pair, err)
		}
	}
	for _, span := range []parser.Span{{}, mustSpan(t, parser.NewSource(source.Bytes()), 0, source.Len())} {
		if _, err := index.SemanticText(span); !errors.Is(err, parser.ErrRange) {
			t.Fatal("absent/foreign semantic range accepted")
		}
	}
	// Semantic byte ranges can split CRLF, independently of position conversion.
	span := mustSpan(t, source, 3, 4)
	if value, err := index.SemanticText(span); err != nil || value.Semantic != parser.Some("\n") {
		t.Fatal("semantic byte range incorrectly restricted to diagnostic domain")
	}
}

func mustSpan(t *testing.T, source parser.Source, start, end int) parser.Span {
	t.Helper()
	span, err := source.Span(start, end)
	if err != nil {
		t.Fatal(err)
	}
	return span
}

func TestIndentationTabPhasesAndRepeatedSplits(t *testing.T) {
	for phase := 0; phase < 4; phase++ {
		input := strings.Repeat(">", phase) + "\tfoo\tbar"
		source := parser.NewSource([]byte(input))
		index := parser.NewLineIndex(source)
		cursor, err := index.Indentation(1)
		if err != nil {
			t.Fatal(err)
		}
		for n := 0; n < phase; n++ {
			span, err := cursor.AdvanceMarker()
			if err != nil || span.Start() != n || span.End() != n+1 {
				t.Fatal("marker advancement lost original offset")
			}
		}
		copyCursor := cursor
		for n := 1; n <= 4-phase; n++ {
			got, err := cursor.Consume(1)
			if err != nil || got.Columns != 1 || got.Content.Semantic != parser.Some(" ") || len(got.Content.Segments) != 1 || cursor.Column() != phase+n {
				t.Fatalf("phase=%d step=%d: %+v, %v", phase, n, got, err)
			}
			assertSegment(t, source, got.Content.Segments[0], phase, phase+1, parser.PartialTab, "\t", " ")
			remaining := cursor.Remaining()
			if remaining.Semantic != parser.Some(strings.Repeat(" ", 4-phase-n)+"foo\tbar") {
				t.Fatalf("phase=%d step=%d remaining=%q", phase, n, remaining.Semantic.Value)
			}
			if n < 4-phase {
				if cursor.Offset() != phase {
					t.Fatal("partial tab moved off its original byte")
				}
				assertSegment(t, source, remaining.Segments[0], phase, phase+1, parser.PartialTab, "\t", strings.Repeat(" ", 4-phase-n))
				before := cursor
				if _, err := cursor.AdvanceMarker(); !errors.Is(err, parser.ErrIndentation) || !reflect.DeepEqual(cursor, before) {
					t.Fatal("marker skipped a partial tab")
				}
			} else if cursor.Offset() != phase+1 {
				t.Fatal("fully consumed tab did not advance its byte")
			}
		}
		whole, err := copyCursor.Consume(99)
		if err != nil || whole.Columns != 4-phase || copyCursor.Column() != 4 || copyCursor.Remaining().Semantic != parser.Some("foo\tbar") {
			t.Fatal("whole tab phase mismatch or content tabs expanded")
		}
		if cursor.Column() != 4 || string(source.Bytes()) != input {
			t.Fatal("independent cursor copy or original bytes changed")
		}
	}
}

func TestIndentationQuoteSequenceAndOwnership(t *testing.T) {
	source := parser.NewSource([]byte(">\t\tfoo\r\n"))
	index := parser.NewLineIndex(source)
	cursor, _ := index.Indentation(1)
	marker, err := cursor.AdvanceMarker()
	if err != nil || marker.Start() != 0 || marker.End() != 1 {
		t.Fatal("quote marker sequence failed")
	}
	delimiter, err := cursor.Consume(1)
	if err != nil || delimiter.Columns != 1 || len(delimiter.Content.Segments) != 1 {
		t.Fatal("delimiter column failed")
	}
	assertSegment(t, source, delimiter.Content.Segments[0], 1, 2, parser.PartialTab, "\t", " ")
	indent, err := cursor.Consume(4)
	if err != nil || indent.Columns != 4 || cursor.Column() != 6 || cursor.Offset() != 2 || len(indent.Content.Segments) != 2 {
		t.Fatalf("four-column consumption: %+v, %v", indent, err)
	}
	assertSegment(t, source, indent.Content.Segments[0], 1, 2, parser.PartialTab, "\t", "  ")
	assertSegment(t, source, indent.Content.Segments[1], 2, 3, parser.PartialTab, "\t", "  ")
	remaining := cursor.Remaining()
	if remaining.Semantic != parser.Some("  foo") || len(remaining.Segments) != 2 {
		t.Fatalf("remaining=%+v", remaining)
	}
	assertSegment(t, source, remaining.Segments[0], 2, 3, parser.PartialTab, "\t", "  ")
	assertSegment(t, source, remaining.Segments[1], 3, 6, parser.Exact, "foo", "foo")
	if _, err := parser.NewNode(parser.NodeSpec{Kind: parser.Text, Envelope: mustSpan(t, source, 2, 6), Content: remaining.Segments, Properties: parser.Properties{Literal: remaining.Semantic}}); err != nil {
		t.Fatal("helper partial-tab value incompatible with existing model", err)
	}
	remaining.Segments[0].Semantic = parser.Some("changed")
	if !reflect.DeepEqual(cursor.Remaining(), cursor.Remaining()) || cursor.Remaining().Semantic.Value != "  foo" || cursor.Remaining().Segments[0].Semantic.Value != "  " {
		t.Fatal("caller mutation changed cursor output")
	}
	before := cursor
	if _, err := cursor.Consume(-1); !errors.Is(err, parser.ErrIndentation) || !reflect.DeepEqual(cursor, before) {
		t.Fatal("negative request changed cursor")
	}
	zero, err := cursor.Consume(0)
	if err != nil || zero.Columns != 0 || zero.Content.Semantic != parser.Some("") || !reflect.DeepEqual(cursor, before) {
		t.Fatal("zero request is not a no-op")
	}
	if string(source.Bytes()) != ">\t\tfoo\r\n" {
		t.Fatal("indentation changed original bytes or endings")
	}
}

func TestIndentationSpacesEmptyAndSemanticContent(t *testing.T) {
	source := parser.NewSource([]byte("  \t\x00\tZ\n\n"))
	index := parser.NewLineIndex(source)
	cursor, _ := index.Indentation(1)
	if _, err := cursor.AdvanceMarker(); !errors.Is(err, parser.ErrIndentation) {
		t.Fatal("space bypassed explicit indentation consumption")
	}
	consumed, err := cursor.Consume(4)
	if err != nil || consumed.Columns != 4 || len(consumed.Content.Segments) != 2 {
		t.Fatal("mixed space/tab indentation failed")
	}
	assertSegment(t, source, consumed.Content.Segments[0], 0, 2, parser.Exact, "  ", "  ")
	assertSegment(t, source, consumed.Content.Segments[1], 2, 3, parser.PartialTab, "\t", "  ")
	remaining := cursor.Remaining()
	if remaining.Semantic != parser.Some("\ufffd\tZ") {
		t.Fatal("remaining NUL or internal tab transformed incorrectly")
	}
	assertSegment(t, source, remaining.Segments[0], 3, 4, parser.Normalized, "\x00", "\ufffd")
	empty, _ := index.Indentation(2)
	if got, err := empty.Consume(1); err != nil || got.Columns != 0 || got.Content.Semantic != parser.Some("") {
		t.Fatal("actual empty line indentation failed")
	}
	if _, err := empty.AdvanceMarker(); !errors.Is(err, parser.ErrIndentation) {
		t.Fatal("marker at EOF accepted")
	}
	for _, n := range []int{-1, 0, 3} {
		if _, err := index.Indentation(n); !errors.Is(err, parser.ErrPosition) {
			t.Fatal("nonexistent content line accepted")
		}
	}
	var zero parser.Indentation
	if zero.Remaining().Semantic != parser.Some("") || zero.Column() != 0 || zero.Offset() != 0 {
		t.Fatal("zero cursor is not empty")
	}
}
