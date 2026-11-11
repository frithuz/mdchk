// Correction facts for MD085.

package parser

import (
	"context"
	"sort"
)

// ParagraphIndentationPreserved reports whether deleting one surplus span that
// ParagraphIndentation returned keeps its line a plain paragraph continuation.
// Leading whitespace of such a line is not paragraph content, so only block
// structure can change: the line could start a block, interrupt the paragraph
// or become a setext underline or table delimiter row. The check parses the
// previous physical line and the line, with and without the span, both as the
// start of a paragraph and after a neutral opening line, and compares each pair
// of parses across line-end whitespace.
func (d *Document) ParagraphIndentationPreserved(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, span Span) (bool, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return false, err
	}
	if !span.Present() || span.source.Token() != index.source.Token() || span.start >= span.end {
		return false, ErrModel
	}
	i := sort.Search(len(index.lines), func(i int) bool { return index.lines[i].start > span.start }) - 1
	if i < 1 || span.end > index.lines[i].end {
		return false, ErrModel
	}
	content := index.source.content
	prev, line := index.lines[i-1], index.lines[i]
	for _, opening := range []string{"", "x\n"} {
		if err := headingFactCharge(ctx, budget, int64(prev.end-prev.start+line.end-line.start)*2+8, 2); err != nil {
			return false, err
		}
		lead := append(append([]byte(opening), content[prev.start:prev.end]...), '\n')
		before := append(append(append([]byte{}, lead...), content[line.start:line.end]...), '\n')
		after := append(append(append(append([]byte{}, lead...), content[line.start:span.start]...), content[span.end:line.end]...), '\n')
		local := ParseOptions{GFM: options.GFM}
		a := ParseWithOptions(ctx, NewSource(before), budget, local)
		b := ParseWithOptions(ctx, NewSource(after), budget, local)
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if a.Status != Complete || b.Status != Complete || a.Document == nil || b.Document == nil || len(a.Uncertainty) != 0 || len(b.Uncertainty) != 0 {
			return false, nil
		}
		if same, err := a.Document.SameMeaningAcrossLineEndWhitespace(ctx, budget, b.Document); err != nil || !same {
			return false, err
		}
	}
	return true, nil
}
