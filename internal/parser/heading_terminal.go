package parser

import (
	"context"
	"errors"
	"unicode/utf8"
)

// HeadingTerminal is the final non-whitespace scalar of an actual heading's
// Visible sequence. Absent Token means an empty/whitespace-only sequence, or an
// excluded code owner. Escaped records retain their atomic token but are excluded
// by consumers requiring unescaped prose. Transformed tokens remain atomic.
type HeadingTerminal struct {
	Scalar  rune
	Token   Span
	Code    bool
	Escaped bool
}

// HeadingTerminals uses the same semantic walk as HeadingFact.Visible, without
// retaining that string or exposing a new parse, source, or allowance. Failed
// collections publish no inventory, including on the final operation checkpoint.
func (d *Document) HeadingTerminals(ctx context.Context, budget *OperationBudget) ([]HeadingTerminal, error) {
	if ctx == nil || budget == nil || d == nil || !d.parsedComplete || d.commentOperation != budget.identity {
		return nil, errors.New("heading context is not a completed parse in this operation")
	}
	var out []HeadingTerminal
	var walk func(*Node) error
	walk = func(n *Node) error {
		if err := headingFactCharge(ctx, budget, int64(len(n.spec.Children))+1, 0); err != nil {
			return err
		}
		if n.Kind() != Heading {
			for _, child := range n.spec.Children {
				if err := walk(child); err != nil {
					return err
				}
			}
			return nil
		}
		var terminal HeadingTerminal
		err := walkHeadingVisible(ctx, budget, n, func(owner *Node, value string) error {
			if owner.Kind() == SoftBreak || owner.Kind() == HardBreak {
				return nil
			}
			if owner.Kind() == CodeSpan {
				for offset := 0; offset < len(value); {
					if err := headingFactCharge(ctx, budget, 5, 0); err != nil {
						return err
					}
					r, width := utf8.DecodeRuneInString(value[offset:])
					offset += width
					if !IsUnicodeWhitespace(r) {
						terminal = HeadingTerminal{Scalar: r, Code: true}
					}
				}
				return nil
			}
			// Borrow actual parser-owned segments; do not decode or search source again.
			for _, part := range owner.spec.Content {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return err
				}
				if !part.Semantic.Present || !part.Source.Present() || part.Source.source != d.Source() {
					return ErrModel
				}
				switch part.Transformation {
				case Exact, Escape, Entity, Normalized, PartialTab:
				default:
					return ErrModel
				}
				for offset := 0; offset < len(part.Semantic.Value); {
					if err := headingFactCharge(ctx, budget, 9, 0); err != nil {
						return err
					}
					r, width := utf8.DecodeRuneInString(part.Semantic.Value[offset:])
					start := offset
					offset += width
					if IsUnicodeWhitespace(r) {
						continue
					}
					token := part.Source
					if part.Transformation == Exact {
						if len(part.Semantic.Value) != (token.end-token.start) || start+width > (token.end-token.start) {
							return ErrModel
						}
						token.start += start
						token.end = token.start + width
					}
					terminal = HeadingTerminal{Scalar: r, Token: token, Escaped: part.Transformation == Escape}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		work := int64(1)
		if len(out) == cap(out) {
			work += int64(len(out))
		}
		if err := headingFactCharge(ctx, budget, work, 1); err != nil {
			return err
		}
		out = append(out, terminal)
		return nil
	}
	if err := walk(d.root); err != nil {
		return nil, err
	}
	if err := headingFactCharge(ctx, budget, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
