package config

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
)

func TestEmptyBlockRepairPreservesUnrelatedInvalidTokens(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*token.Token)
	}{
		{"error", func(t *token.Token) { t.Error = "unrelated syntax error" }},
		{"origin", func(t *token.Token) { t.Origin = "bad" }},
		{"marker-line", func(t *token.Token) { t.Position.Line++ }},
		{"marker-offset", func(t *token.Token) { t.Position.Offset++ }},
		{"marker-column", func(t *token.Token) { t.Position.Column++ }},
		{"next-position", func(t *token.Token) { t.Next.Position.Offset++ }},
		{"mismatched-body-line", func(t *token.Token) { t.Prev.Origin = "\n    " }},
		{"nonempty-body", func(t *token.Token) { t.Prev.Value = "actual content" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tokens := lexer.Tokenize("x: |+\ny: next\n")
			marker := tokens.InvalidToken()
			if marker == nil {
				t.Fatal("pinned lexer fixture no longer exposes known defect")
			}
			tc.mutate(marker)
			fixed := normalizeEmptyBlocks(tokens)
			if fixed.InvalidToken() != marker {
				t.Fatal("repair suppressed unrelated or mismatched lexer error")
			}
		})
	}
}

func TestYAMLSourcePositionGuards(t *testing.T) {
	text := "é\rnext\r\nlast\n"
	b := yamlBuilder{content: text, lineStarts: yamlLineStarts(text)}
	for _, p := range [][2]int{{0, 1}, {-1, 1}, {99, 1}, {1, 0}, {1, 99}, {2, 99}, {4, 2}} {
		if _, err := b.sourceOffset(p[0], p[1]); err == nil {
			t.Fatalf("accepted invalid source position %v", p)
		}
	}
	if at, err := b.sourceOffset(2, 1); err != nil || text[at:] != "next\r\nlast\n" {
		t.Fatalf("CR offset %d, %v", at, err)
	}
	if at, err := b.sourceOffset(3, 1); err != nil || text[at:] != "last\n" {
		t.Fatalf("CRLF offset %d, %v", at, err)
	}
	if _, err := b.block(nil); err == nil {
		t.Fatal("nil external block source must fail closed")
	}
}

func TestNumericConversionAdmitsWorkBeforeCachingFailure(t *testing.T) {
	text := strings.Repeat("9", 131072)
	identity := &ast.TagNode{}
	for _, excess := range []int64{0, 1} {
		b := yamlBuilder{conversions: map[ast.Node]*scalarConversion{}, work: 250000000 - int64(len(text)) + excess}
		v := LocatedValue{node: &syntaxValue{kind: IntegerValue, text: text}}
		got, err := b.finishScalar(identity, v)
		if (err != nil) != (excess > 0) {
			t.Fatalf("work excess%d: %v", excess, err)
		}
		if excess > 0 {
			if len(b.conversions) != 0 {
				t.Fatal("over-budget conversion published result")
			}
			continue
		}
		if got.node.conversion == nil || got.node.conversion.representable {
			t.Fatal("overflow failure was not cached")
		}
		// Even at the exhausted boundary, a second use reuses the failure marker
		// without invoking strconv again or cloning its large error input.
		another := LocatedValue{node: &syntaxValue{kind: IntegerValue, text: text, origin: Origin{PropertyPath: "alias"}}}
		alias, err := b.finishScalar(identity, another)
		if err != nil || alias.node.conversion != got.node.conversion || alias.Origin().PropertyPath != "alias" {
			t.Fatalf("cached conversion use: %v", err)
		}
	}
}
