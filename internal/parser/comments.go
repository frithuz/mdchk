package parser

import (
	"context"
	"errors"
)

// CommentFact describes only a parser-confirmed HTML comment. Payload excludes
// delimiters and container prefixes; Envelope selects the original spelling.
// Close is absent for an unclosed block comment. Protected comments occur in
// confirmed literal HTML interiors and cannot drive Markdown consumers.
type CommentFact struct {
	Envelope, Open, Close Span
	Payload               []Segment
	Protected             bool
}

// Comments collects source-ordered facts only from an actual completed parse.
// Constructed or unfinished documents cannot assert confirmed syntax. Collection
// shares the caller's operation budget; interruption withholds the whole inventory.
func (d *Document) Comments(ctx context.Context, budget *OperationBudget) ([]CommentFact, error) {
	if ctx == nil || budget == nil {
		return nil, errors.New("nil comment context or budget")
	}
	if d == nil || !d.parsedComplete || d.commentOperation != budget.identity {
		return nil, errors.New("comment context is not a completed parse in this operation")
	}
	var out []CommentFact
	stack := []*Node{d.root}
	raw := ""
	for len(stack) != 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if err := budget.ChargeWork(int64(1 + len(n.spec.Children) + len(n.htmlTail) + len(n.spec.Markers) + len(n.spec.Content))); err != nil {
			return nil, err
		}
		if n.rawTag != "" {
			if raw == "" && !n.rawClosing {
				raw = n.rawTag
			} else if raw == n.rawTag && n.rawClosing {
				raw = ""
			}
		}
		open, close := n.commentOpen, n.commentClose
		for _, m := range n.spec.Markers {
			if m.Role == "html-comment-open" {
				open = m.Source
			}
			if m.Role == "html-comment-close" {
				close = m.Source
			}
		}
		if open.Present() {
			end := n.Span().End()
			payloadEnd := end
			if close.Present() {
				end, payloadEnd = close.End(), close.Start()
			}
			envelope, _ := d.Source().Span(open.Start(), end)
			fact := CommentFact{Envelope: envelope, Open: open, Close: close, Protected: raw != ""}
			for _, part := range n.spec.Content {
				if !part.Source.Present() {
					continue
				}
				start, stop := max(part.Source.Start(), open.End()), min(part.Source.End(), payloadEnd)
				if start >= stop {
					continue
				}
				span, _ := d.Source().Span(start, stop)
				part.Source = span
				// Comment bytes are raw spelling, not decoded inline semantics.
				text, _ := d.Source().Text(span)
				part.Semantic = Some(text)
				fact.Payload = append(fact.Payload, part)
			}
			out = append(out, fact)
		}
		for i := len(n.htmlTail) - 1; i >= 0; i-- {
			stack = append(stack, n.htmlTail[i])
		}
		for i := len(n.spec.Children) - 1; i >= 0; i-- {
			stack = append(stack, n.spec.Children[i])
		}
	}
	return out, nil
}
