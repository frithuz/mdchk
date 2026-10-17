package parser_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestLineIndexIndependentCoordinates(t *testing.T) {
	tests := []struct {
		input   string
		lines   int
		pairs   map[int]parser.Position
		reject  []int
		invalid []parser.Position
	}{
		{"", 0, map[int]parser.Position{0: {1, 1}}, []int{-1, 1}, []parser.Position{{1, 2}, {2, 1}}},
		{"a", 1, map[int]parser.Position{0: {1, 1}, 1: {1, 2}}, []int{-1, 2}, []parser.Position{{2, 1}, {1, 3}}},
		{"a\n", 1, map[int]parser.Position{0: {1, 1}, 1: {1, 2}, 2: {2, 1}}, []int{3}, []parser.Position{{1, 3}, {2, 2}}},
		{"a\r", 1, map[int]parser.Position{0: {1, 1}, 1: {1, 2}, 2: {2, 1}}, []int{3}, []parser.Position{{1, 3}, {2, 2}}},
		{"a\r\n", 1, map[int]parser.Position{0: {1, 1}, 1: {1, 2}, 3: {2, 1}}, []int{2, 4}, []parser.Position{{1, 3}, {2, 2}}},
		{"é\r\n\t\x00", 2, map[int]parser.Position{0: {1, 1}, 2: {1, 2}, 4: {2, 1}, 5: {2, 2}, 6: {2, 3}}, []int{1, 3, 7}, []parser.Position{{1, 3}, {2, 4}, {3, 1}}},
		{"é\r\n\t\x00\n", 2, map[int]parser.Position{0: {1, 1}, 2: {1, 2}, 4: {2, 1}, 5: {2, 2}, 6: {2, 3}, 7: {3, 1}}, []int{1, 3, 8}, []parser.Position{{3, 2}}},
		{"é\tX", 1, map[int]parser.Position{0: {1, 1}, 2: {1, 2}, 3: {1, 3}, 4: {1, 4}}, []int{1, 5}, []parser.Position{{1, 5}, {2, 1}}},
		{"e\u0301🙂", 1, map[int]parser.Position{0: {1, 1}, 1: {1, 2}, 3: {1, 3}, 7: {1, 4}}, []int{2, 4, 5, 6}, []parser.Position{{1, 5}, {2, 1}}},
		{"\r\n\n\ra\r\nb", 5, map[int]parser.Position{0: {1, 1}, 2: {2, 1}, 3: {3, 1}, 4: {4, 1}, 5: {4, 2}, 7: {5, 1}, 8: {5, 2}}, []int{1, 6, 9}, []parser.Position{{1, 2}, {2, 2}, {3, 2}, {6, 1}}},
		{"\n\n", 2, map[int]parser.Position{0: {1, 1}, 1: {2, 1}, 2: {3, 1}}, []int{3}, []parser.Position{{1, 2}, {3, 2}, {4, 1}}},
		{"é\t\x00\r\nZ", 2, map[int]parser.Position{0: {1, 1}, 2: {1, 2}, 3: {1, 3}, 4: {1, 4}, 6: {2, 1}, 7: {2, 2}}, []int{1, 5, 8}, []parser.Position{{1, 5}, {2, 3}, {3, 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			source := parser.NewSource([]byte(tt.input))
			index := parser.NewLineIndex(source)
			if index.LineCount() != tt.lines || index.Source() != source {
				t.Fatalf("lines=%d, want %d, or changed source identity", index.LineCount(), tt.lines)
			}
			for offset, want := range tt.pairs {
				got, err := index.Position(offset)
				if err != nil || got != want {
					t.Errorf("Position(%d) = %v, %v; want %v", offset, got, err, want)
				}
				gotOffset, err := index.Offset(want)
				if err != nil || gotOffset != offset {
					t.Errorf("Offset(%v) = %d, %v; want %d", want, gotOffset, err, offset)
				}
			}
			for _, offset := range tt.reject {
				if _, err := index.Position(offset); !errors.Is(err, parser.ErrPosition) {
					t.Errorf("Position(%d) error = %v", offset, err)
				}
			}
			for _, pos := range append(tt.invalid, parser.Position{}, parser.Position{-1, 1}, parser.Position{1, -1}, parser.Position{1, 0}, parser.Position{0, 1}) {
				if _, err := index.Offset(pos); !errors.Is(err, parser.ErrPosition) {
					t.Errorf("Offset(%v) error = %v", pos, err)
				}
			}
			// Exhaustive round trips supplement the independently expected table.
			for offset := 0; offset <= len(tt.input); offset++ {
				pos, err := index.Position(offset)
				if _, accepted := tt.pairs[offset]; accepted != (err == nil) {
					t.Fatalf("unexpected boundary domain at %d: %v", offset, err)
				}
				if err == nil {
					if back, err := index.Offset(pos); err != nil || back != offset {
						t.Errorf("round trip %d -> %v -> %d, %v", offset, pos, back, err)
					}
				}
			}
			if !bytes.Equal(source.Bytes(), []byte(tt.input)) {
				t.Fatal("mapping changed original source")
			}
		})
	}
}

func TestPhysicalLinesAndArbitraryBytes(t *testing.T) {
	input := []byte("é\r\n\n\xff\rZ")
	source := parser.NewSource(input)
	index := parser.NewLineIndex(source)
	input[0] = 'X'
	wants := []struct {
		start, end, next int
		content, ending  string
	}{{0, 2, 4, "é", "\r\n"}, {4, 4, 5, "", "\n"}, {5, 6, 7, "\xff", "\r"}, {7, 8, 8, "Z", ""}}
	if index.LineCount() != len(wants) {
		t.Fatal("arbitrary bytes changed physical-line count")
	}
	for n, want := range wants {
		line, err := index.Line(n + 1)
		if err != nil || !line.Content.Present() || !line.Ending.Present() || line.Content.Start() != want.start || line.Content.End() != want.end || line.Ending.Start() != want.end || line.Ending.End() != want.next {
			t.Fatalf("line %d: %+v, %v", n+1, line, err)
		}
		content, err := source.Text(line.Content)
		if err != nil || content != want.content {
			t.Fatalf("line content %q, %v", content, err)
		}
		ending, err := source.Text(line.Ending)
		if err != nil || ending != want.ending {
			t.Fatalf("line ending %q, %v", ending, err)
		}
		foreign := parser.NewSource(source.Bytes())
		if _, err := foreign.Text(line.Content); !errors.Is(err, parser.ErrRange) {
			t.Fatal("line span lost snapshot ownership")
		}
	}
	for _, n := range []int{-1, 0, 5} {
		if _, err := index.Line(n); !errors.Is(err, parser.ErrPosition) {
			t.Fatalf("Line(%d) error = %v", n, err)
		}
	}
	// Byte access deliberately accepts CRLF and scalar interiors.
	for _, pair := range [][2]int{{1, 2}, {3, 4}, {8, 8}} {
		span, err := source.Span(pair[0], pair[1])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := source.Text(span); err != nil {
			t.Fatal("byte access incorrectly narrowed to text boundaries")
		}
	}
}

func TestLineIndexEncodingFailurePrefix(t *testing.T) {
	for _, suffix := range [][]byte{{0xff}, {0x80}, {0xe2, 0x82}, {0xe2, '(', 0xa1}} {
		input := append([]byte("é\r\n\tA\rB\n"), suffix...)
		source := parser.NewSource(input)
		index := parser.NewLineIndex(source)
		failure := index.EncodingFailure()
		if failure == nil || failure.Code != parser.InvalidUTF8 || failure.Location.Start() != 9 || failure.Location.End() != 9 {
			t.Fatalf("wrong authoritative failure: %+v", failure)
		}
		if point, err := index.FailurePosition(); err != nil || point != (parser.Position{4, 1}) {
			t.Fatalf("valid-prefix point %v, %v", point, err)
		}
		for _, offset := range []int{0, 9, len(input)} {
			_, err := index.Position(offset)
			var typed *parser.Failure
			if !errors.As(err, &typed) || typed.Code != parser.InvalidUTF8 {
				t.Fatal("malformed document acquired a normal text map")
			}
		}
		if _, err := index.Offset(parser.Position{1, 1}); err == nil {
			t.Fatal("reverse mapping accepted malformed document")
		}
		span, _ := source.Span(0, 2)
		if _, err := index.SemanticText(span); err == nil {
			t.Fatal("semantic processing accepted malformed document")
		}
		if _, err := index.Indentation(1); err == nil {
			t.Fatal("indentation accepted malformed document")
		}
		failure.Code = parser.InternalFailure
		if index.EncodingFailure().Code != parser.InvalidUTF8 {
			t.Fatal("caller mutated index failure metadata")
		}
		parsed := parser.Parse(context.Background(), source)
		if parsed.Failure == nil || parsed.Failure.Location != index.EncodingFailure().Location || !bytes.Equal(parsed.Source.Bytes(), input) {
			t.Fatal("index and Parse disagree on retained original failure location")
		}
	}
	// A first invalid continuation byte has an empty valid prefix.
	bad := parser.NewLineIndex(parser.NewSource([]byte{0x80}))
	if pos, err := bad.FailurePosition(); err != nil || pos != (parser.Position{1, 1}) {
		t.Fatalf("invalid byte-zero prefix = %v, %v", pos, err)
	}
	for _, suffix := range []string{"\xff", "\xe2\x82"} {
		index := parser.NewLineIndex(parser.NewSource([]byte("é\r\n\tée\u0301" + suffix)))
		if point, err := index.FailurePosition(); err != nil || point != (parser.Position{2, 5}) || index.EncodingFailure().Location.Start() != 10 {
			t.Fatalf("multibyte/combining valid-prefix point %v, %v", point, err)
		}
	}
	var zero parser.LineIndex
	if !reflect.DeepEqual(zero, parser.NewLineIndex(parser.Source{})) || zero.LineCount() != 0 || zero.EncodingFailure() != nil {
		t.Fatal("zero line index is not empty")
	}
	if _, err := zero.FailurePosition(); !errors.Is(err, parser.ErrPosition) {
		t.Fatal("valid source invented encoding failure")
	}
	if pos, err := zero.Position(0); err != nil || pos != (parser.Position{1, 1}) {
		t.Fatal("zero index EOF mismatch")
	}
}
