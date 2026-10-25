package parser

import (
	"context"
	"errors"
)

// IndentedHeadings returns one original full opening or underline marker for
// each actual heading with positive leaf indentation on any physical line.
// The block lifecycle retains this evidence from its post-matched-container
// cursor, including partial tabs and lazy lines, before normalization. Removed
// definitions, paragraphs and tables cannot publish heading evidence.
// Only a completed original parse and operation can project the inventory.
func (d *Document) IndentedHeadings(ctx context.Context, budget *OperationBudget) ([]Span, error) {
	if ctx == nil || budget == nil || d == nil || !d.parsedComplete || d.commentOperation != budget.identity {
		return nil, errors.New("heading indentation requires a completed parse in this operation")
	}
	var out []Span
	for _, marker := range d.indentedHeadings {
		if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
			return nil, err
		}
		if err := openingHashGrowth(ctx, budget, len(out), cap(out)); err != nil {
			return nil, err
		}
		out = append(out, marker)
	}
	if err := headingFactCharge(ctx, budget, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
