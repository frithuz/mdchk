package parser

import (
	"context"
	"unicode/utf8"
)

// EmptySection is a finalized empty section's original label. Final means the
// globally last actual heading in this document, including container headings.
type EmptySection struct {
	Label Span
	Final bool
}

// SectionCheckpoint binds section work to the complete production parse and its
// actual options, source index and original allowance. Anchor policy is irrelevant.
func (d *Document) SectionCheckpoint(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, work, nodes int64) error {
	return d.headingProjectionCheckpoint(ctx, budget, index, options, work, nodes)
}

// EmptySections visits actual syntax once, retaining one pending heading. A
// deeper next heading itself satisfies its parent. Interrupted projections are
// withheld atomically; a partial body scan cannot prove a section empty.
func (d *Document) EmptySections(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) ([]EmptySection, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	var out []EmptySection
	var pending Span
	level := 0
	content := false
	publish := func(final bool) error {
		work := int64(1)
		if len(out) == cap(out) {
			work += int64(len(out))
		}
		if err := headingFactCharge(ctx, budget, work, 1); err != nil {
			return err
		}
		out = append(out, EmptySection{Label: pending, Final: final})
		return nil
	}
	// Stack storage is reserved before allocation/growth. Existing AST pointers
	// are borrowed; visiting them consumes work, not new syntax-node records.
	type visit struct {
		node  *Node
		label bool
		alt   bool
	}
	stack := make([]visit, 1)
	stack[0] = visit{node: d.root}
	raw := ""
	for len(stack) != 0 {
		entry := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := entry.node
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		if !entry.alt && n.rawTag != "" {
			if raw == "" && !n.rawClosing {
				raw = n.rawTag
			} else if raw == n.rawTag && n.rawClosing {
				raw = ""
			}
		}
		label := entry.label
		if n.Kind() == Heading {
			h := n.spec.Properties.Heading
			if h == nil {
				return nil, ErrModel
			}
			if pending.Present() && !content && h.Level <= level {
				if err := publish(false); err != nil {
					return nil, err
				}
			}
			next, err := headingLabel(ctx, budget, n, Span{})
			if err != nil {
				return nil, err
			}
			pending, level, content = next, h.Level, false
			label = true // Own inline label children update provenance, never body.
		} else if !label && pending.Present() {
			switch n.Kind() {
			case CodeBlock, ThematicBreak, Table, CodeSpan, Image:
				content = true
				label = true
			case HTMLBlock, InlineHTML:
				if !content {
					substantive, err := sectionHTMLContent(ctx, budget, n, raw != "")
					if err != nil {
						return nil, err
					}
					content = substantive
				}
				label = true
			case Text:
				if !content {
					if !n.spec.Properties.Literal.Present {
						return nil, ErrModel
					}
					substantive, err := sectionTextContent(ctx, budget, n.spec.Properties.Literal.Value)
					if err != nil {
						return nil, err
					}
					content = substantive
				}
			default:
			}
			if n.spec.Properties.Task != nil {
				content = true
			}
		}
		count := len(n.spec.Children) + len(n.htmlTail)
		if err := headingFactCharge(ctx, budget, int64(count)+1, 0); err != nil {
			return nil, err
		}
		needed := len(stack) + count
		if needed > cap(stack) {
			// needed is bounded by parser-owned records; reject unrepresentable growth
			// without performing allocation or overflowing doubled capacity.
			if needed < len(stack) || cap(stack) > int(^uint(0)>>1)/2 {
				return nil, ErrModel
			}
			capacity := max(needed, 2*cap(stack))
			if err := headingFactCharge(ctx, budget, int64(len(stack))+1, int64(capacity)); err != nil {
				return nil, err
			}
			next := make([]visit, len(stack), capacity)
			copy(next, stack)
			stack = next
		}
		for i := len(n.htmlTail) - 1; i >= 0; i-- {
			stack = append(stack, visit{node: n.htmlTail[i], label: true, alt: entry.alt})
		}
		for i := len(n.spec.Children) - 1; i >= 0; i-- {
			stack = append(stack, visit{node: n.spec.Children[i], label: label, alt: entry.alt || n.Kind() == Image})
		}
	}
	if pending.Present() && !content {
		if err := publish(true); err != nil {
			return nil, err
		}
	}
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}

func sectionTextContent(ctx context.Context, budget *OperationBudget, text string) (bool, error) {
	for start := 0; start < len(text); {
		// Include a whole final scalar in each bounded byte chunk. Reserve its
		// inspections before decoding, including multibyte Unicode whitespace.
		end := start + min(256, len(text)-start)
		if err := headingFactCharge(ctx, budget, int64(end-start)+utf8.UTFMax, 0); err != nil {
			return false, err
		}
		for start < end {
			r, width := utf8.DecodeRuneInString(text[start:])
			if !IsUnicodeWhitespace(r) {
				return true, nil
			}
			start += width
		}
	}
	return false, nil
}

// A comment owner's Content omits container prefixes. Only the actual confirmed
// comment and confirmed tail comments are excluded; uncovered owned bytes remain
// HTML content. Scanning Envelope gaps would mistake quote/list markers for body.
func sectionHTMLContent(ctx context.Context, budget *OperationBudget, n *Node, protected bool) (bool, error) {
	if protected {
		return true, nil
	}
	comment := func(node *Node) (Span, Span, error) {
		open, close := node.commentOpen, node.commentClose
		for _, marker := range node.spec.Markers {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return Span{}, Span{}, err
			}
			if marker.Role == "html-comment-open" {
				open = marker.Source
			}
			if marker.Role == "html-comment-close" {
				close = marker.Source
			}
		}
		return open, close, nil
	}
	open, close, err := comment(n)
	if err != nil {
		return false, err
	}
	if !open.Present() {
		return true, nil
	} // Actual noncomment HTML is an object.
	if !close.Present() {
		return false, nil
	}
	// Inline comments end at their own closer. Block form-2 owners can retain
	// physical-line tails, whose private nodes identify subsequent comments/tags.
	tails := n.htmlTail
	tailAt := 0
	var tailOpen, tailClose Span
	nextTail := func() error {
		tailOpen, tailClose = Span{}, Span{}
		if tailAt < len(tails) {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return err
			}
			t := tails[tailAt]
			tailAt++
			a, b, err := comment(t)
			if err != nil {
				return err
			}
			if !a.Present() {
				return ErrModel
			} // Handled as an authored object below.
			tailOpen, tailClose = a, b
			if !b.Present() {
				tailClose = t.Span()
			}
			return nil
		}
		return nil
	}
	// Noncomment private tail nodes prove an authored HTML object immediately.
	for _, tail := range tails {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return false, err
		}
		a, _, err := comment(tail)
		if err != nil {
			return false, err
		}
		if !a.Present() {
			return true, nil
		}
	}
	if err := nextTail(); err != nil {
		return false, err
	}
	for _, part := range n.spec.Content {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return false, err
		}
		start := max(part.Source.Start(), close.End())
		end := part.Source.End()
		for start < end {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return false, err
			}
			if tailOpen.Present() && tailClose.End() <= start {
				if err := nextTail(); err != nil {
					return false, err
				}
				continue
			}
			if tailOpen.Present() && tailOpen.Start() <= start {
				start = min(end, tailClose.End())
				continue
			}
			stop := end
			if tailOpen.Present() {
				stop = min(stop, tailOpen.Start())
			}
			span, err := part.Source.source.Span(start, stop)
			if err != nil {
				return false, err
			}
			text, err := part.Source.source.Text(span)
			if err != nil {
				return false, err
			}
			found, err := sectionTextContent(ctx, budget, text)
			if err != nil || found {
				return found, err
			}
			start = stop
		}
	}
	return false, nil
}
