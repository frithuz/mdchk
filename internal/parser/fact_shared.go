// Helpers shared across rule facts. Each one serves the fact collectors of
// more than one rule.

package parser

import (
	"context"
	"errors"
)

func (d *Document) headingProjectionCheckpoint(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, work, nodes int64) error {
	if ctx == nil || budget == nil || d == nil || !d.parsedComplete || d.commentOperation != budget.identity || index.source.Token() != d.Source().Token() || d.parsedOptions != options || (d.parsedProfile != coreProfile && d.parsedProfile != gfmInProgressProfile) {
		return ErrModel
	}
	return errors.Join(ctx.Err(), budget.ChargeWork(work), budget.ChargeNodes(nodes))
}

// Locked stages use the parser's ordinary metering helpers. The borrowed index
// gets this stage's context; no original context or syntax is mutated.
func nestedExampleLocked(ctx context.Context, budget *OperationBudget, index LineIndex, action func(*parserContext, LineIndex) error) (err error) {
	if err = headingFactCharge(ctx, budget, 2, 2); err != nil {
		return err
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	defer func() {
		if caught := recover(); caught != nil {
			if stop, ok := caught.(budgetStop); ok {
				copy := *stop.exceeded
				err = &copy
				return
			}
			panic(caught)
		}
	}()
	work := &parserContext{Context: ctx, budget: &parseBudget{operation: budget}}
	index.ctx = work
	err = action(work, index)
	meterWork(work, 1)
	return errors.Join(err, ctx.Err())
}

func shellPromptGrow[T any](ctx context.Context, budget *OperationBudget, dst []T) ([]T, error) {
	if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
		return nil, err
	}
	if len(dst) < cap(dst) {
		return dst, nil
	}
	if cap(dst) > int(^uint(0)>>1)/2 {
		return nil, ErrModel
	}
	capacity := max(1, cap(dst)*2)
	if err := headingFactCharge(ctx, budget, int64(len(dst))+int64(capacity)+1, int64(capacity)); err != nil {
		return nil, err
	}
	next := make([]T, len(dst), capacity)
	copy(next, dst)
	return next, nil
}

// proseIslands walks original prose once and hands every contiguous Text
// island to s.flush. It reports false when necessary ownership is unknown.
func (d *Document) proseIslands(s *swappedLinkScanner, index LineIndex) (bool, error) {
	ctx, budget := s.ctx, s.budget
	type frame struct {
		node        *Node
		prose, exit bool
	}
	var stack []frame
	push := func(f frame) error {
		var e error
		stack, e = swappedLinkGrow(s, stack)
		if e != nil {
			return e
		}
		if e = s.charge(1, 1); e != nil {
			return e
		}
		stack = append(stack, f)
		return nil
	}
	err := push(frame{node: d.root})
	if err != nil {
		return false, err
	}
	raw := conflictRawState{}
	rawKnown := true
	if err = s.charge(2, 2); err != nil {
		return false, err
	}
	htmlWork := &parserContext{Context: ctx, budget: &parseBudget{operation: budget}}
	for {
		if err = s.charge(32, 0); err != nil {
			return false, err
		}
		if len(stack) == 0 {
			break
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := f.node
		if n == nil || !n.spec.Envelope.present || n.spec.Envelope.SourceToken() != index.source.Token() || n.spec.Envelope.start < 0 || n.spec.Envelope.end < n.spec.Envelope.start || n.spec.Envelope.end > index.source.Len() {
			return false, ErrModel
		}
		if f.exit {
			if n.Kind() != Text {
				if err = s.flush(); err != nil {
					return false, err
				}
			}
			continue
		}
		prose := f.prose
		if n.Kind() != Text {
			if err = s.flush(); err != nil {
				return false, err
			}
		}
		switch n.Kind() {
		case Paragraph, Heading:
			prose = true
		case TableCell:
			if n.spec.Properties.Cell == nil {
				return false, nil
			}
			prose = !n.spec.Properties.Cell.Alignment.Present
		case DocumentKind, BlockQuote, List, ListItem, Table, TableRow, HTMLBlock, CodeBlock:
			prose = false
		case Text:
			if prose && (raw.name == "" || !rawKnown) {
				s.unknown = !rawKnown
				var known bool
				known, err = s.text(n)
				if err != nil {
					return false, err
				}
				if !known {
					return false, nil
				}
			} else if err = s.flush(); err != nil {
				return false, err
			}
		case Link, Image, CodeSpan:
			if n.Kind() != CodeSpan {
				s.linkEnd = n.spec.Envelope.end
			}
			continue // Whole owners are opaque, including raw tags in image alternatives.
		case InlineHTML, SoftBreak, HardBreak, ThematicBreak, Emphasis, Strong, Strike:
			// Boundaries retain their actual local inline owner or opaque spelling.
		default:
			return false, ErrModel
		}
		if n.Kind() == HTMLBlock {
			var available bool
			raw, _, available, err = conflictHTMLRaw(htmlWork, index.source, n, raw)
			if err != nil {
				return false, err
			}
			rawKnown = rawKnown && available
			continue // HTML-owned spelling and closing tails were consumed once.
		}
		if err = s.charge(2*int64(len(n.rawTag))+int64(len(raw.name))+1, 0); err != nil {
			return false, err
		}
		if n.rawTag != "" {
			if raw.name == "" && !n.rawClosing {
				raw = conflictRawState{name: n.rawTag, start: n.spec.Envelope.start}
			} else if raw.name == n.rawTag && n.rawClosing {
				raw = conflictRawState{}
			}
		}
		if n.Kind() == CodeBlock {
			continue
		}
		if err = push(frame{node: n, exit: true}); err != nil {
			return false, err
		}
		for group := 0; group < 2; group++ {
			children := n.htmlTail
			if group == 1 {
				children = n.spec.Children
			}
			if err = s.charge(int64(len(children))+3, 0); err != nil {
				return false, err
			}
			for i := len(children) - 1; i >= 0; i-- {
				if err = push(frame{node: children[i], prose: prose}); err != nil {
					return false, err
				}
			}
		}
	}
	if err = s.flush(); err != nil {
		return false, err
	}
	return true, nil
}
