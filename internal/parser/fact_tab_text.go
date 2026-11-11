// Correction facts for MD010.

package parser

import (
	"context"
	"sort"
)

// MaxTabSpaces is the longest run of spaces a correction may replace one TAB
// with. Beyond it, spaces_per_tab could multiply a file's size from one
// configuration line; it bounds growth to a few bytes per TAB.
const MaxTabSpaces = 8

// InlineTextTabs returns one detached span per original TAB byte that sits in the
// exact inline Text content of a paragraph, heading or table cell, outside image
// descriptions, after a non-whitespace byte of the same block on its line and
// before another one: line-end whitespace belongs to MD009, and spaces there
// could form a hard break. Every other TAB, including indentation, container prefixes, code, HTML, link
// destinations, titles, reference definitions, delimiter rows and cell trimming,
// is owned by no such Text and so is never returned.
func (d *Document) InlineTextTabs(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) ([]Span, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	type frame struct {
		node  *Node
		owner *Node
		alt   bool
	}
	content := index.source.content
	var out []Span
	j, k := 0, 0
	stack := []frame{{node: d.root}}
	for len(stack) > 0 {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := f.node
		switch n.Kind() {
		case Paragraph, Heading, TableCell:
			f.owner = n
		case DocumentKind, BlockQuote, List, ListItem, CodeBlock, HTMLBlock, ThematicBreak, Table, TableRow:
			f.owner = nil
		case Image:
			f.alt = true
		case Text:
			if f.owner == nil || f.alt {
				break
			}
			for _, segment := range n.spec.Content {
				if segment.Transformation != Exact || !segment.Source.Present() || segment.Source.source.Token() != index.source.Token() {
					continue
				}
				if err := headingFactCharge(ctx, budget, int64(segment.Source.End()-segment.Source.Start())+1, 0); err != nil {
					return nil, err
				}
				for offset := segment.Source.Start(); offset < segment.Source.End(); offset++ {
					if content[offset] != '\t' {
						continue
					}
					// One whitespace run is scanned once, however many TABs it holds.
					if offset >= k {
						j = offset - 1
						for j >= 0 && (content[j] == ' ' || content[j] == '\t') {
							j--
						}
						k = offset + 1
						for k < len(content) && (content[k] == ' ' || content[k] == '\t') {
							k++
						}
						if err := headingFactCharge(ctx, budget, int64(k-j)+1, 0); err != nil {
							return nil, err
						}
					}
					if j < f.owner.spec.Envelope.Start() || content[j] == '\n' || content[j] == '\r' || k == len(content) || content[k] == '\n' || content[k] == '\r' {
						continue
					}
					if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
						return nil, err
					}
					out = append(out, Span{source: index.source, start: offset, end: offset + 1, present: true})
				}
			}
		case InlineHTML, SoftBreak, HardBreak, CodeSpan, Emphasis, Strong, Strike, Link:
		}
		children := n.spec.Children
		if err := headingFactCharge(ctx, budget, int64(len(children))+1, int64(len(children))); err != nil {
			return nil, err
		}
		for i := len(children) - 1; i >= 0; i-- {
			stack = append(stack, frame{node: children[i], owner: f.owner, alt: f.alt})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].start < out[j].start })
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
