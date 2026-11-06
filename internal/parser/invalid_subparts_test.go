package parser

import (
	"bytes"
	"reflect"
	"testing"
)

func TestInvalidSubpartBoundariesAndPositions(t *testing.T) {
	type part = InvalidSubpart
	at := func(start, end, line, column int) part {
		return part{Start: start, End: end, Position: Position{Line: line, Column: column}}
	}
	for _, tc := range []struct {
		name  string
		input []byte
		want  []part
	}{
		{"two invalid leads", []byte{0xFF, 0xFE}, []part{at(0, 1, 1, 1), at(1, 2, 1, 2)}},
		{"truncated before ASCII", []byte{0xE2, 0x82, 0x41}, []part{at(0, 2, 1, 1)}},
		{"truncated before invalid", []byte{0xE2, 0x82, 0xFF}, []part{at(0, 2, 1, 1), at(2, 3, 1, 2)}},
		{"truncated before LF", []byte{0xF0, 0x9F, 0x98, 0x0A}, []part{at(0, 3, 1, 1)}},
		{"surrogate", []byte{0xED, 0xA0, 0x80}, []part{at(0, 1, 1, 1), at(1, 2, 1, 2), at(2, 3, 1, 3)}},
		{"overlong two bytes", []byte{0xC0, 0xAF}, []part{at(0, 1, 1, 1), at(1, 2, 1, 2)}},
		{"overlong three bytes", []byte{0xE0, 0x80, 0xAF}, []part{at(0, 1, 1, 1), at(1, 2, 1, 2), at(2, 3, 1, 3)}},
		{"above U+10FFFF", []byte{0xF4, 0x90, 0x80, 0x80}, []part{at(0, 1, 1, 1), at(1, 2, 1, 2), at(2, 3, 1, 3), at(3, 4, 1, 4)}},
		{"interrupted sequence", []byte{0xE2, 0x28, 0xA1}, []part{at(0, 1, 1, 1), at(2, 3, 1, 3)}},
		{"truncated at EOF", []byte{0xE2, 0x82}, []part{at(0, 2, 1, 1)}},
		{"after CRLF", []byte{0x61, 0x0D, 0x0A, 0xFF}, []part{at(3, 4, 2, 1)}},
		{"after lone CR", []byte{0x61, 0x0D, 0xFF}, []part{at(2, 3, 2, 1)}},
		{"after tab", []byte{0x09, 0xFF, 0xFE}, []part{at(1, 2, 1, 2), at(2, 3, 1, 3)}},
		{"after BOM", []byte{0xEF, 0xBB, 0xBF, 0xFF, 0x41, 0xFE}, []part{at(3, 4, 1, 2), at(5, 6, 1, 4)}},
		{"table 3-7 second byte ranges", []byte{0xE0, 0xA0, 0x0A, 0xED, 0x9F, 0x0A, 0xF0, 0x90, 0x80, 0x0A, 0xF4, 0x8F, 0xBF, 0x0A, 0xF1, 0x0A}, []part{at(0, 2, 1, 1), at(3, 5, 2, 1), at(6, 9, 3, 1), at(10, 13, 4, 1), at(14, 15, 5, 1)}},
		{"lone continuation and C1", []byte{0x80, 0xC1, 0xF5}, []part{at(0, 1, 1, 1), at(1, 2, 1, 2), at(2, 3, 1, 3)}},
		{"scalars between subparts", []byte("雪\xff雪\xe2\x82\t\xff"), []part{at(3, 4, 1, 2), at(7, 9, 1, 4), at(10, 11, 1, 6)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			index := NewLineIndex(NewSource(tc.input))
			got, total := index.InvalidSubparts(1000)
			if !reflect.DeepEqual(got, tc.want) || total != len(tc.want) {
				t.Fatalf("got %+v total %d, want %+v", got, total, tc.want)
			}
			// The first subpart is the point the typed failure already reports.
			failure := index.EncodingFailure()
			position, err := index.FailurePosition()
			if failure == nil || err != nil || failure.Location.Start() != got[0].Start || position != got[0].Position {
				t.Fatalf("failure=%+v position=%+v err=%v first=%+v", failure, position, err, got[0])
			}
		})
	}
}

func TestInvalidSubpartsValidInput(t *testing.T) {
	for _, text := range []string{"", "\ufefftext\n", "a\ufffdb", "a\nb\rc\r\nd"} {
		if got, total := NewLineIndex(NewSource([]byte(text))).InvalidSubparts(1000); got != nil || total != 0 {
			t.Fatalf("%q: %+v %d", text, got, total)
		}
	}
}

func TestInvalidSubpartsRetentionCountsExactly(t *testing.T) {
	index := NewLineIndex(NewSource(bytes.Repeat([]byte{0xFF}, 5000)))
	for _, retain := range []int{0, 1, 3, 5000, 6000} {
		got, total := index.InvalidSubparts(retain)
		if total != 5000 || len(got) != min(retain, 5000) {
			t.Fatal(retain, len(got), total)
		}
		for i, part := range got {
			if part.Start != i || part.End != i+1 || part.Position != (Position{Line: 1, Column: i + 1}) {
				t.Fatal(retain, i, part)
			}
		}
	}
}

// The retained storage does not grow with the error count past the retention.
func TestInvalidSubpartsAllocationBounded(t *testing.T) {
	index := NewLineIndex(NewSource(bytes.Repeat([]byte{0xFF}, 1<<20)))
	allocations := testing.AllocsPerRun(1, func() { index.InvalidSubparts(1000) })
	if allocations > 32 {
		t.Fatal(allocations)
	}
}
