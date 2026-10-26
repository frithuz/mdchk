package parser

import (
	"context"
	"strings"
)

// HeadingKey trims/collapses CommonMark Unicode whitespace in already semantic
// text, then uses pinned Unicode full default folding. It does not decode again,
// compose Unicode, remove punctuation or allocate a new operation allowance.
func (d *Document) HeadingKey(ctx context.Context, budget *OperationBudget, h HeadingFact) (string, error) {
	if ctx == nil || budget == nil || d == nil || !d.parsedComplete || d.commentOperation != budget.identity || !h.Label.Present() || h.Label.SourceToken() != d.Source().Token() {
		return "", ErrModel
	}
	text := h.Visible
	// Reserve scans, builder writes and amortized growth before allocation.
	if err := headingFactCharge(ctx, budget, 5*int64(len(text))+1, int64(len(text))+1); err != nil {
		return "", err
	}
	var out strings.Builder
	out.Grow(len(text))
	space := false
	count := 0
	for _, r := range text {
		if count&255 == 0 {
			if err := headingFactCharge(ctx, budget, 0, 0); err != nil {
				return "", err
			}
		}
		count++
		if IsUnicodeWhitespace(r) {
			space = out.Len() != 0
			continue
		}
		if space {
			out.WriteByte(' ')
			space = false
		}
		out.WriteRune(r)
	}
	return FoldCase(ctx, budget, out.String())
}
