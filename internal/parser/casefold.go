package parser

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

// FoldCase applies Unicode 17.0.0 full default folding (C and F mappings).
// It performs no whitespace, NUL, normalization or locale transformation.
// Allocation and traversal share the existing operation budget. This is text
// comparison support, not reference-label or metadata interpretation authority.
func FoldCase(ctx context.Context, budget *OperationBudget, text string) (string, error) {
	if ctx == nil || budget == nil {
		return "", errors.New("nil case-fold context or budget")
	}
	checkpoint := func() error { return errors.Join(ctx.Err(), budget.ChargeWork(0)) }
	if err := checkpoint(); err != nil {
		return "", err
	}
	if int64(len(text)) > budget.Limits().DocumentBytes {
		return "", &LimitExceeded{Name: "document-bytes", Observed: int64(len(text)), Allowed: budget.Limits().DocumentBytes}
	}
	if err := budget.ChargeWork(8*int64(len(text)) + 1); err != nil {
		return "", errors.Join(ctx.Err(), err)
	}
	if !utf8.ValidString(text) {
		return "", ErrRange
	}
	// Full default folds expand at most three UTF-8 bytes per input byte in
	// the pinned table. Reserve retained text before strings.Builder entry.
	if err := budget.ChargeNodes(3*int64(len(text)) + 1); err != nil {
		return "", errors.Join(ctx.Err(), err)
	}
	var out strings.Builder
	out.Grow(len(text))
	count := 0
	for _, r := range text {
		if count&255 == 0 {
			if err := checkpoint(); err != nil {
				return "", err
			}
		}
		count++
		if folded, ok := fullCaseFold[r]; ok {
			out.WriteString(folded)
		} else {
			out.WriteRune(r)
		}
	}
	if err := checkpoint(); err != nil {
		return "", err
	}
	return out.String(), nil
}
