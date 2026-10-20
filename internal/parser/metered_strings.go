package parser

import (
	"context"
	"strings"
)

// Standard-library scans are charged conservatively before entering the bounded
// call; the admitted document bounds any indivisible library scan.
func meteredContains(ctx context.Context, s, sub string) bool {
	meterWork(ctx, len(s)+len(sub))
	return strings.Contains(s, sub)
}
func meteredIndex(ctx context.Context, s, sub string) int {
	meterWork(ctx, len(s)+len(sub))
	return strings.Index(s, sub)
}
func meteredEqualFold(ctx context.Context, a, b string) bool {
	meterWork(ctx, len(a)+len(b))
	return strings.EqualFold(a, b)
}
func meteredHasPrefix(ctx context.Context, s, prefix string) bool {
	meterWork(ctx, min(len(s), len(prefix)))
	return strings.HasPrefix(s, prefix)
}
func meteredRepeat(ctx context.Context, s string, n int) string {
	if n < 0 || len(s) != 0 && n > int(^uint(0)>>1)/len(s) {
		panic("parser: invalid repeat size")
	}
	meterWork(ctx, len(s)*n)
	return strings.Repeat(s, n)
}

func meteredString(ctx context.Context, s string) string { meterWork(ctx, len(s)); return s }
func meteredContainsRune(ctx context.Context, s string, r rune) bool {
	meterWork(ctx, len(s))
	return strings.ContainsRune(s, r)
}
func meteredReplaceAll(ctx context.Context, s, old, replacement string) string {
	// Charge the worst-case output before the standard-library allocator runs.
	n := strings.Count(meteredString(ctx, s), old)
	if len(replacement) > len(old) {
		meterWork(ctx, n*(len(replacement)-len(old)))
	}
	meterWork(ctx, len(s))
	return strings.ReplaceAll(s, old, replacement)
}

func meteredExpandedString(ctx context.Context, s string) string { meterWork(ctx, len(s)*4); return s }

func meterBuilderGrowth(ctx context.Context, b *strings.Builder, n int) {
	meterWork(ctx, n)
	if n > b.Cap()-b.Len() {
		meterWork(ctx, b.Len())
	}
}
func meteredWriteString(ctx context.Context, b *strings.Builder, s string) (int, error) {
	meterBuilderGrowth(ctx, b, len(s))
	return b.WriteString(s)
}
func meteredWriteByte(ctx context.Context, b *strings.Builder, c byte) error {
	meterBuilderGrowth(ctx, b, 1)
	return b.WriteByte(c)
}
func meteredWriteRune(ctx context.Context, b *strings.Builder, r rune) (int, error) {
	meterBuilderGrowth(ctx, b, 4)
	return b.WriteRune(r)
}

// Concatenation copies both operands; charge their complete output before Go
// can allocate the combined string, including a short prefix on a long value.
func meteredConcat(ctx context.Context, a, b string) string {
	if len(b) > int(^uint(0)>>1)-len(a) {
		panic("parser: concatenation size overflow")
	}
	meterWork(ctx, len(a)+len(b))
	return a + b
}
