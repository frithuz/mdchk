package config

import (
	"fmt"
	"strings"
	"testing"
)

func numericAliasSource(text, tag, style string, aliases int) []byte {
	var s strings.Builder
	if style != "" {
		fmt.Fprintf(&s, "version: 1\nrules:\n  future:\n    original: &number %s %s\n      %s\n", tag, style, text)
	} else {
		fmt.Fprintf(&s, "version: 1\nrules:\n  future:\n    original: &number %s %s\n", tag, text)
	}
	for i := 0; i < aliases; i++ {
		fmt.Fprintf(&s, "    alias%d: *number\n", i)
	}
	return []byte(s.String())
}

func TestYAMLNumericConversionsShareSuccessAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name, text, tag, style string
		representable          bool
		want                   any
	}{
		{"integer-block", "12", "!!int", "|-", true, 12},
		{"integer-overflow-block", strings.Repeat("9", 32768), "!!int", "|-", false, nil},
		{"float-long-success-block", strings.Repeat("0", 32768) + "1", "!!float", "|-", true, float64(1)},
		{"float-overflow-block", strings.Repeat("9", 32768), "!!float", "|-", false, nil},
		{"integer-overflow-plain", strings.Repeat("9", 32768), "", "", false, nil},
		{"integer-tagged-quoted", "'12'", "!!int", "", true, 12},
		{"float-plain", "1e3", "", "", true, float64(1000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := DecodeYAML("source.yaml", numericAliasSource(tc.text, tc.tag, tc.style, 64))
			if err != nil {
				t.Fatal(err)
			}
			rule := contractEntry(t, contractEntry(t, d.Syntax(), "rules"), "future")
			original := contractEntry(t, rule, "original")
			conversion := original.node.conversion
			if conversion == nil || conversion.representable != tc.representable || conversion.value != tc.want {
				t.Fatalf("wrong cached conversion: %+v", conversion)
			}
			for _, entry := range rule.Entries() {
				if entry.Value.node.conversion != conversion {
					t.Fatal("numeric alias repeated conversion rather than sharing immutable result")
				}
				supplied := d.Rules["future"].Options[entry.Key]
				if tc.representable {
					if supplied != tc.want {
						t.Fatalf("%s: primitive = %v", entry.Key, supplied)
					}
				} else {
					located, ok := supplied.(LocatedValue)
					if !ok || located.Origin().PropertyPath != entry.Value.Origin().PropertyPath || *located.Origin().Position != *entry.Value.Origin().Position {
						t.Fatalf("%s: overflow fallback lost use-site origin", entry.Key)
					}
				}
			}
		})
	}
}

func BenchmarkDecodeYAMLNumericBlockAliases(b *testing.B) {
	for _, mode := range []string{"integer-success", "integer-overflow", "float-success", "float-overflow"} {
		for _, size := range []int{1024, 32768, 131072} {
			for _, aliases := range []int{1, 16, 64} {
				b.Run(fmt.Sprintf("%s/bytes%d/aliases%d", mode, size, aliases), func(b *testing.B) {
					tag := "!!float"
					text := strings.Repeat("0", size) + "1"
					if strings.HasPrefix(mode, "integer") {
						tag = "!!int"
					}
					if strings.HasSuffix(mode, "overflow") {
						text = strings.Repeat("9", size)
					} else if mode == "integer-success" {
						text = "1" + strings.Repeat("\n", size-1)
					}
					source := numericAliasSource(text, tag, "|-", aliases)
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

func BenchmarkDecodeYAMLNumericOverflowManyAliases(b *testing.B) {
	for _, tag := range []string{"!!int", "!!float"} {
		b.Run(tag, func(b *testing.B) {
			source := numericAliasSource(strings.Repeat("9", 131072), tag, "|-", 512)
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
