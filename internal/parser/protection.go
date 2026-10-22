package parser

import (
	"context"
	"errors"
)

// LiteralSpans returns original opaque code payload and raw HTML envelopes from
// a completed parse in this operation. Delimiter edits remain subject to combined
// candidate validation; these spans prove byte preservation only.
func (d *Document) LiteralSpans(ctx context.Context, budget *OperationBudget) ([]Span, error) {
	if ctx == nil || budget == nil {
		return nil, errors.New("nil literal context or budget")
	}
	if d == nil || !d.parsedComplete || d.commentOperation != budget.identity {
		return nil, errors.New("literal context is not a completed parse in this operation")
	}
	var out []Span
	stack := []*Node{d.root}
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if err := budget.ChargeWork(int64(1 + len(n.spec.Children) + len(n.htmlTail) + len(n.spec.Content))); err != nil {
			return nil, err
		}
		if err := budget.ChargeNodes(int64(len(n.spec.Children) + len(n.htmlTail) + len(n.spec.Content) + 1)); err != nil {
			return nil, err
		}
		switch n.Kind() {
		case CodeBlock, CodeSpan:
			for _, p := range n.spec.Content {
				if p.Source.Present() {
					out = append(out, p.Source)
				}
			}
		case HTMLBlock, InlineHTML:
			out = append(out, n.Span())
		case DocumentKind, Paragraph, Heading, ThematicBreak, BlockQuote, List, ListItem, Text, SoftBreak, HardBreak, Emphasis, Strong, Link, Image, Table, TableRow, TableCell:
			// These nodes do not themselves establish opaque literal bytes.
		}
		stack = append(stack, n.htmlTail...)
		stack = append(stack, n.spec.Children...)
	}
	return out, nil
}

// ProtectedNodes returns confirmed opaque code/HTML nodes for binding checks,
// charging construction before copying handles. Full envelopes include syntax
// delimiters; literal payload byte preservation alone does not prove binding.
func (d *Document) ProtectedNodes(ctx context.Context, budget *OperationBudget) ([]*Node, error) {
	if ctx == nil || budget == nil {
		return nil, errors.New("nil protected-node context or budget")
	}
	if d == nil || !d.parsedComplete || d.commentOperation != budget.identity {
		return nil, errors.New("protected nodes require a completed parse in this operation")
	}
	var out []*Node
	stack := []*Node{d.root}
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count := int64(len(n.spec.Children) + len(n.htmlTail) + 1)
		if err := budget.ChargeWork(count); err != nil {
			return nil, err
		}
		if err := budget.ChargeNodes(count); err != nil {
			return nil, err
		}
		switch n.Kind() {
		case CodeBlock, CodeSpan, HTMLBlock, InlineHTML:
			out = append(out, cloneNode(n))
		case DocumentKind, Paragraph, Heading, ThematicBreak, BlockQuote, List, ListItem, Text, SoftBreak, HardBreak, Emphasis, Strong, Link, Image, Table, TableRow, TableCell:
		}
		stack = append(stack, n.htmlTail...)
		stack = append(stack, n.spec.Children...)
	}
	return out, nil
}
