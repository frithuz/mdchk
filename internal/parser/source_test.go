package parser_test

import (
	"bytes"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestNewSourcePreservesBytes(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{name: "nil"},
		{name: "empty", input: []byte{}},
		{name: "no final newline", input: []byte("# heading")},
		{name: "unicode and line endings", input: []byte("café\r\n日本語\nlast\rend")},
		{name: "tabs and NUL", input: []byte("\tbefore\x00after\t")},
		{name: "unvalidated encoding", input: []byte{0xff, 0xc0, 0x80}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := parser.NewSource(tt.input)
			if got := source.Bytes(); !bytes.Equal(got, tt.input) {
				t.Fatalf("Bytes() = %q, want original bytes %q", got, tt.input)
			}
		})
	}
}

func TestSourceOwnsBytes(t *testing.T) {
	const original = "original\r\nsource"
	input := []byte(original)
	source := parser.NewSource(input)
	input[0] = 'X'
	if got := string(source.Bytes()); got != original {
		t.Fatalf("caller mutation changed retained source to %q", got)
	}

	first := source.Bytes()
	second := source.Bytes()
	first[0] = 'Y'
	if got := string(second); got != original {
		t.Fatalf("returned slices share mutable bytes: got %q", got)
	}
	if got := string(source.Bytes()); got != original {
		t.Fatalf("returned-slice mutation changed retained source to %q", got)
	}
}

func TestSourceZeroValue(t *testing.T) {
	var source parser.Source
	if got := source.Bytes(); len(got) != 0 {
		t.Fatalf("zero-value Bytes() = %q, want empty content", got)
	}
}
