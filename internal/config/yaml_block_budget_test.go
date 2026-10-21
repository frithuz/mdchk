package config

import (
	"fmt"
	"github.com/goccy/go-yaml/ast"
	yamlparser "github.com/goccy/go-yaml/parser"
	"strings"
	"testing"
	"unsafe"
)

func blockAliasSource(bytes, aliases int, style, tag string) []byte {
	var s strings.Builder
	fmt.Fprintf(&s, "version: 1\nrules:\n  future:\n    original: &body %s%s\n      %s\n\n", tag, style, strings.Repeat("a", bytes))
	for i := 0; i < aliases; i++ {
		fmt.Fprintf(&s, "    alias%d: *body\n", i)
	}
	return []byte(s.String())
}

func TestYAMLBlockAliasesShareImmutableBytes(t *testing.T) {
	for _, style := range []string{"|+", ">", "|2-"} {
		for _, tag := range []string{"", "!!str "} {
			t.Run(tag+style, func(t *testing.T) {
				d, err := DecodeYAML("source.yaml", blockAliasSource(32768, 64, style, tag))
				if err != nil {
					t.Fatal(err)
				}
				rule := contractEntry(t, contractEntry(t, d.Syntax(), "rules"), "future")
				original := contractEntry(t, rule, "original")
				for _, e := range rule.Entries() {
					if e.Key == "original" {
						continue
					}
					if unsafe.StringData(e.Value.Spelling()) != unsafe.StringData(original.Spelling()) || unsafe.StringData(e.Value.StringValue()) != unsafe.StringData(original.StringValue()) { //nolint:gosec // G103: test-only comparison of immutable string storage addresses; pointers are never dereferenced or mutated.
						t.Fatal("block alias copied body-sized immutable source/semantic storage")
					}
					if e.Value.Origin().PropertyPath == original.Origin().PropertyPath || e.Value.Origin().Position.Line == original.Origin().Position.Line {
						t.Fatal("shared scalar bytes lost use-site provenance")
					}
				}
			})
		}
	}
}

// Benchmarks report complete decoding cost, including the YAML lexer/parser.
// At fixed body size, aliases must add only located nodes/paths, not body copies.
func BenchmarkDecodeYAMLBlockAliases(b *testing.B) {
	for _, tag := range []string{"", "!!str "} {
		name := "plain"
		if tag != "" {
			name = "tagged"
		}
		for _, size := range []int{1024, 32768, 131072} {
			for _, aliases := range []int{1, 16, 64} {
				b.Run(fmt.Sprintf("%s/bytes%d/aliases%d", name, size, aliases), func(b *testing.B) {
					source := blockAliasSource(size, aliases, "|+", tag)
					b.ReportAllocs()
					b.SetBytes(int64(len(source)))
					b.ResetTimer()
					for b.Loop() {
						if _, err := DecodeYAML("source.yaml", source); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}

func TestYAMLBlockCacheAdmitsWorkBeforePublication(t *testing.T) {
	text := "x: |+\n  hello\n\n"
	file, err := yamlparser.ParseBytes([]byte(text), 0)
	if err != nil {
		t.Fatal(err)
	}
	node := file.Docs[0].Body.(*ast.MappingNode).Values[0].Value.(*ast.LiteralNode)
	starts := []int{0}
	for i, c := range text {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	cost := int64(len(text) - 3 + len(node.Value.Value))
	for _, excess := range []int64{0, 1} {
		builder := yamlBuilder{content: text, lineStarts: starts, blocks: map[*ast.LiteralNode]blockScalar{}, work: 250000000 - cost + excess}
		_, err := builder.block(node)
		if (err != nil) != (excess > 0) {
			t.Fatalf("work excess %d: %v", excess, err)
		}
		if excess > 0 && len(builder.blocks) != 0 {
			t.Fatal("failed work admission published block cache")
		}
	}
}
