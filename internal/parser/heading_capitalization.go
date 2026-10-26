package parser

import (
	"context"
	"errors"
	"unicode/utf8"
)

// HeadingScalar retains one scalar in the shared heading Visible sequence.
// Code scalars and semantic breaks have no diagnostic Token. Exact prose maps
// to one original scalar; transformed prose keeps its complete atomic token.
type HeadingScalar struct {
	Scalar rune
	Token  Span
	Code   bool
}

// HeadingCapitalizationCheckpoint binds derived capitalization work to the
// completed original parse, source index and private operation identity.
func (d *Document) HeadingCapitalizationCheckpoint(ctx context.Context, budget *OperationBudget, index LineIndex, work, nodes int64) error {
	if ctx == nil || budget == nil || d == nil || !d.parsedComplete || d.commentOperation != budget.identity || index.source.Token() != d.Source().Token() {
		return ErrModel
	}
	return errors.Join(ctx.Err(), budget.ChargeWork(work), budget.ChargeNodes(nodes))
}

// HeadingScalars projects independently owned scalar slices for all actual
// headings, using the same Visible walk and original allowance. An interrupted
// collection publishes no inventory, including at the final checkpoint.
func (d *Document) HeadingScalars(ctx context.Context, budget *OperationBudget) (out [][]HeadingScalar, err error) {
	if ctx == nil || budget == nil || d == nil || !d.parsedComplete || d.commentOperation != budget.identity {
		return nil, ErrModel
	}
	defer func() {
		err = errors.Join(err, headingFactCharge(ctx, budget, 0, 0))
		if err != nil {
			out = nil
		}
	}()
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
		var scalars []HeadingScalar
		appendScalar := func(s HeadingScalar) error {
			work := int64(1)
			if len(scalars) == cap(scalars) {
				work += int64(len(scalars))
			}
			if err := headingFactCharge(ctx, budget, work, 1); err != nil {
				return err
			}
			scalars = append(scalars, s)
			return nil
		}
		if err := walkHeadingVisible(ctx, budget, n, func(owner *Node, value string) error {
			switch owner.Kind() {
			case SoftBreak, HardBreak:
				return appendScalar(HeadingScalar{Scalar: '\n'})
			case CodeSpan:
				for offset := 0; offset < len(value); {
					if err := headingFactCharge(ctx, budget, 5, 0); err != nil {
						return err
					}
					r, width := utf8.DecodeRuneInString(value[offset:])
					offset += width
					if err := appendScalar(HeadingScalar{Scalar: r, Code: true}); err != nil {
						return err
					}
				}
				return nil
			default:
			}
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
					token := part.Source
					if part.Transformation == Exact {
						if len(part.Semantic.Value) != token.end-token.start || start+width > token.end-token.start {
							return ErrModel
						}
						token.start += start
						token.end = token.start + width
					}
					if err := appendScalar(HeadingScalar{Scalar: r, Token: token}); err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
			return err
		}
		work := int64(1)
		if len(out) == cap(out) {
			work += int64(len(out))
		}
		if err := headingFactCharge(ctx, budget, work, 1); err != nil {
			return err
		}
		out = append(out, scalars)
		return nil
	}
	err = walk(d.root)
	return out, err
}
