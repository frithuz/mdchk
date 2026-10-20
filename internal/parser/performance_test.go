package parser_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

// BenchmarkParseGrowth measures parsing an already-owned immutable snapshot.
// Source construction and fixture generation are deliberately outside the timer.
// Keep depth and payload separate: nested models can legitimately retain more
// source provenance than a flat model with the same number of input bytes.
func BenchmarkParseGrowth(b *testing.B) {
	for _, tc := range []struct {
		name      string
		makeInput func(int) string
	}{
		{"plain", func(n int) string { return strings.Repeat("plain prose ", n) }},
		{"short-lines", func(n int) string { return strings.Repeat("a\n", n) }},
		{"dense-inlines", func(n int) string { return strings.Repeat("*a* **b** [c](u) ![d](v) `e` &amp; ", n) }},
		{"unfinished-html", func(n int) string { return strings.Repeat("<a x='", n) }},
		{"backticks-malformed-tail", func(n int) string { return strings.Repeat("`a``b```c ", n) + "````unfinished" }},
		{"undefined-references", func(n int) string { return strings.Repeat("[missing][label] ", n) }},
	} {
		b.Run(tc.name, func(b *testing.B) {
			sizes := []int{256, 1024, 4096}
			if tc.name == "dense-inlines" {
				sizes = []int{64, 256, 1024}
			}
			for _, n := range sizes {
				b.Run(fmt.Sprint(n), func(b *testing.B) { benchmarkParseSnapshot(b, tc.makeInput(n)) })
			}
		})
	}
	for _, family := range []string{"containers", "images-emphasis"} {
		b.Run(family, func(b *testing.B) {
			for _, depth := range []int{4, 16, 64} {
				b.Run(fmt.Sprintf("depth/%d", depth), func(b *testing.B) {
					benchmarkParseSnapshot(b, nestedBenchmarkInput(family, depth, 256))
				})
			}
			for _, payload := range []int{256, 1024, 4096} {
				b.Run(fmt.Sprintf("payload/%d", payload), func(b *testing.B) {
					benchmarkParseSnapshot(b, nestedBenchmarkInput(family, 4, payload))
				})
			}
		})
	}
}

func nestedBenchmarkInput(family string, depth, payload int) string {
	if family == "containers" {
		return strings.Repeat(strings.Repeat("> ", depth)+strings.Repeat("x", payload)+"\n", 8)
	}
	text := strings.Repeat("x", payload)
	for i := 0; i < depth; i++ {
		text = "![*" + text + "*](image)"
	}
	return text
}

func benchmarkParseSnapshot(b *testing.B, input string) {
	source := parser.NewSource([]byte(input))
	ctx := context.Background()
	observed := parser.Parse(ctx, source)
	if observed.Status != parser.Complete {
		b.Fatalf("fixture must complete: status=%s failure=%v interruption=%s", observed.Status, observed.Failure, observed.Interruption)
	}
	// Retained tree size helps distinguish work proportional to output from
	// repeated scans of unchanged input. Tree inspection is outside the timer.
	var countNodes func(*parser.Node) int
	countNodes = func(n *parser.Node) int {
		count := 1
		for _, child := range n.Children() {
			count += countNodes(child)
		}
		return count
	}
	nodes := countNodes(observed.Document.Root())
	b.ReportAllocs()
	b.SetBytes(int64(source.Len()))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := parser.Parse(ctx, source)
		if r.Status != parser.Complete || r.Failure != nil || r.Interruption != "" {
			b.Fatalf("fixture outcome changed: status=%s failure=%v interruption=%s", r.Status, r.Failure, r.Interruption)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(nodes), "nodes/op")
	b.ReportMetric(1, "complete/op")
}

// BenchmarkParseDefaultLimit measures explicit budget rejection separately from
// completed parses. Never compare this number to a completed-parse baseline.
func BenchmarkParseDefaultLimit(b *testing.B) {
	source := parser.NewSource([]byte(strings.Repeat("*a* **b** [c](u) ![d](v) `e` &amp; ", 4096)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := parser.Parse(context.Background(), source)
		if r.Status != parser.Incomplete || r.AST != parser.Partial || r.Failure != nil || r.Limit == nil || r.Limit.Name != "document-nodes" || r.Limit.Observed <= r.Limit.Allowed || r.Interruption != parser.LimitExceededReason {
			b.Fatalf("expected explicit document-node limit: status=%s availability=%s limit=%v interruption=%s", r.Status, r.AST, r.Limit, r.Interruption)
		}
	}
	b.StopTimer()
	b.ReportMetric(1, "limited/op")
}
