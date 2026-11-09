package parser

import (
	"context"
	"errors"
)

// IndentedHeading is one actual heading with positive leaf indentation on any
// physical line. Marker is its full opening or underline marker. Lines holds the
// leaf indentation of each physical heading line that has some, in source order:
// each span starts right after that line's matched container prefixes and their
// padding and stops before its first non-indentation byte. Lazy reports a heading
// line whose enclosing container prefix was absent.
type IndentedHeading struct {
	Marker Span
	Lines  []HeadingIndentation
	Lazy   bool
}

// HeadingIndentation is one physical line's leaf indentation. PartialTab reports
// that its first byte is a tab the container prefix already partly consumed.
type HeadingIndentation struct {
	Span       Span
	PartialTab bool
}

// lineIndent is a paragraph line's retained record until the paragraph closes.
type lineIndent struct {
	start int
	line  HeadingIndentation
	lazy  bool
}

// IndentedHeadings returns each actual heading with positive leaf indentation on
// any physical line. The block lifecycle retains this evidence from its
// post-matched-container cursor, including partial tabs and lazy lines, before
// normalization. Removed definitions, paragraphs and tables cannot publish
// heading evidence. Only a completed original parse and operation can project
// the inventory.
func (d *Document) IndentedHeadings(ctx context.Context, budget *OperationBudget) ([]IndentedHeading, error) {
	if ctx == nil || budget == nil || d == nil || !d.parsedComplete || d.commentOperation != budget.identity {
		return nil, errors.New("heading indentation requires a completed parse in this operation")
	}
	var out []IndentedHeading
	for _, heading := range d.indentedHeadings {
		if err := headingFactCharge(ctx, budget, int64(len(heading.Lines))+1, int64(len(heading.Lines))+1); err != nil {
			return nil, err
		}
		if err := openingHashGrowth(ctx, budget, len(out), cap(out)); err != nil {
			return nil, err
		}
		heading.Lines = append([]HeadingIndentation(nil), heading.Lines...)
		out = append(out, heading)
	}
	if err := headingFactCharge(ctx, budget, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
