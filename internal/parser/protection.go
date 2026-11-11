package parser

import (
	"context"
	"errors"
	"strings"
)

// LiteralSpans returns original opaque code payload and raw HTML envelopes from
// a completed parse in this operation. Delimiter edits remain subject to combined
// candidate validation; these spans prove byte preservation only. The edge SPACE
// runs of a code span that CodeSpanPadding reports are not opaque: deleting both
// leaves the normalized value unchanged, and validation proves that it does.
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
			left, right, padded := Span{}, Span{}, false
			if n.Kind() == CodeSpan {
				left, right, padded = CodeSpanPadding(n)
			}
			for _, p := range n.spec.Content {
				if !p.Source.Present() {
					continue
				}
				s := p.Source
				if padded && s.start < left.end {
					s.start = min(left.end, s.end)
				}
				if padded && s.end > right.start {
					s.end = max(right.start, s.start)
				}
				out = append(out, s)
			}
		case HTMLBlock, InlineHTML:
			out = append(out, n.Span())
		case DocumentKind, Paragraph, Heading, ThematicBreak, BlockQuote, List, ListItem, Text, SoftBreak, HardBreak, Emphasis, Strong, Strike, Link, Image, Table, TableRow, TableCell:
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
		case DocumentKind, Paragraph, Heading, ThematicBreak, BlockQuote, List, ListItem, Text, SoftBreak, HardBreak, Emphasis, Strong, Strike, Link, Image, Table, TableRow, TableCell:
		}
		stack = append(stack, n.htmlTail...)
		stack = append(stack, n.spec.Children...)
	}
	return out, nil
}

// CodeSpanPadding returns the delimiter-adjacent ASCII SPACE runs of a code span
// whose deletion leaves its normalized value byte-for-byte unchanged: both runs
// are present, made of exact or normalized original SPACE bytes, and the value
// neither is all SPACE nor starts or ends with SPACE or a backtick. Normalization
// then strips exactly one SPACE at each edge, so each run is that single byte.
func CodeSpanPadding(n *Node) (left, right Span, ok bool) {
	if n == nil || n.Kind() != CodeSpan || len(n.spec.Markers) != 2 || len(n.spec.Content) == 0 {
		return Span{}, Span{}, false
	}
	literal := n.spec.Properties.Literal.Value
	if !n.spec.Properties.Literal.Present || strings.Trim(literal, " ") == "" {
		return Span{}, Span{}, false
	}
	if first, last := literal[0], literal[len(literal)-1]; first == '`' || last == '`' || first == ' ' || last == ' ' {
		return Span{}, Span{}, false
	}
	var opening, closing Span
	for _, m := range n.spec.Markers {
		switch m.Role {
		case "opening":
			opening = m.Source
		case "closing":
			closing = m.Source
		}
	}
	if !opening.Present() || !closing.Present() || opening.source.Token() != closing.source.Token() || opening.end > closing.start {
		return Span{}, Span{}, false
	}
	content := opening.source.content
	first, last := n.spec.Content[0], n.spec.Content[len(n.spec.Content)-1]
	for _, p := range []Segment{first, last} {
		if !p.Source.Present() || p.Source.source.Token() != opening.source.Token() || p.Transformation != Exact && p.Transformation != Normalized {
			return Span{}, Span{}, false
		}
	}
	if first.Source.start != opening.end || last.Source.end != closing.start || first.Source.end <= opening.end || last.Source.start >= closing.start {
		return Span{}, Span{}, false
	}
	if content[opening.end] != ' ' || content[closing.start-1] != ' ' {
		return Span{}, Span{}, false
	}
	left = Span{source: opening.source, start: opening.end, end: opening.end + 1, present: true}
	right = Span{source: opening.source, start: closing.start - 1, end: closing.start, present: true}
	if left.end > right.start {
		return Span{}, Span{}, false
	}
	return left, right, true
}
