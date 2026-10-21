package parser

import (
	"context"
	"strings"
)

// Standard-library scans are charged conservatively before entering the bounded
// call; the admitted document bounds any indivisible library scan.
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
func meteredWriteString(ctx context.Context, b *strings.Builder, s string) {
	meterBuilderGrowth(ctx, b, len(s))
	_, _ = b.WriteString(s) // strings.Builder writes always succeed.
}
func meteredWriteByte(ctx context.Context, b *strings.Builder, c byte) {
	meterBuilderGrowth(ctx, b, 1)
	_ = b.WriteByte(c) // strings.Builder writes always succeed.
}
func meteredWriteRune(ctx context.Context, b *strings.Builder, r rune) {
	meterBuilderGrowth(ctx, b, 4)
	_, _ = b.WriteRune(r) // strings.Builder writes always succeed.
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
