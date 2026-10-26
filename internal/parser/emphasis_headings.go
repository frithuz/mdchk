package parser

import (
	"context"
	"unicode/utf8"
)

// EmphasisHeadings returns original outer emphasis envelopes for single-line
// paragraph labels immediately followed by ordinary prose in the same parent.
// It borrows immutable parser ownership and the original physical index, and
// shares the completed parse's allowance. A failed collection publishes nothing.
func (d *Document) EmphasisHeadings(ctx context.Context, budget *OperationBudget, index LineIndex) ([]Span, error) {
	if ctx == nil || budget == nil || d == nil || !d.parsedComplete || d.commentOperation != budget.identity || index.source.Token() != d.Source().Token() {
		return nil, ErrModel
	}
	lines := index.lines
	if err := headingFactCharge(ctx, budget, 2*int64(len(lines))+1, 2*int64(len(lines))+1); err != nil {
		return nil, err
	}
	prefixes := make([]int, len(lines))
	nonblank := make([]int, len(lines)+1)
	locate := func(offset int) (int, error) {
		low, high := 0, len(lines)
		for low < high {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return 0, err
			}
			mid := low + (high-low)/2
			if lines[mid].start <= offset {
				low = mid + 1
			} else {
				high = mid
			}
		}
		if low == 0 {
			return 0, ErrModel
		}
		return low - 1, nil
	}
	var prefixesWalk func(*Node) error
	prefixesWalk = func(n *Node) error {
		if err := headingFactCharge(ctx, budget, int64(len(n.spec.Children))+1, 0); err != nil {
			return err
		}
		if n.Kind() == BlockQuote || n.Kind() == ListItem {
			for _, m := range n.spec.Markers {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return err
				}
				if m.Role != "quote-prefix" && m.Role != "item-prefix" && m.Role != "item-indent" {
					continue
				}
				i, err := locate(m.Source.Start())
				if err != nil {
					return err
				}
				if m.Source.End() > lines[i].end {
					return ErrModel
				}
				prefixes[i] = max(prefixes[i], m.Source.End())
			}
		}
		for _, child := range n.spec.Children {
			if err := prefixesWalk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := prefixesWalk(d.root); err != nil {
		return nil, err
	}
	for i, line := range lines {
		if err := headingFactCharge(ctx, budget, int64(line.end-line.start)+1, 0); err != nil {
			return nil, err
		}
		nonblank[i+1] = nonblank[i]
		for j := max(line.start, prefixes[i]); j < line.end; j++ {
			if (j-line.start)&255 == 0 {
				if err := headingFactCharge(ctx, budget, 0, 0); err != nil {
					return nil, err
				}
			}
			if d.Source().content[j] != ' ' && d.Source().content[j] != '\t' {
				nonblank[i+1]++
				break
			}
		}
	}
	var out []Span
	var walk func(*Node) error
	walk = func(parent *Node) error {
		if err := headingFactCharge(ctx, budget, int64(len(parent.spec.Children))+1, 0); err != nil {
			return err
		}
		for i, n := range parent.spec.Children {
			if n.Kind() == Paragraph && n.spec.Properties.Task == nil && len(n.spec.Children) == 1 && len(n.spec.Content) > 0 && i+1 < len(parent.spec.Children) {
				outer := n.spec.Children[0]
				if (outer.Kind() == Emphasis || outer.Kind() == Strong) && outer.Span().Start() == n.spec.Content[0].Source.Start() && outer.Span().End() == n.spec.Content[len(n.spec.Content)-1].Source.End() {
					first, err := locate(outer.Span().Start())
					if err != nil {
						return err
					}
					last, err := locate(outer.Span().End() - 1)
					if err != nil {
						return err
					}
					if first == last {
						eligible, meaningful, err := emphasisLabel(ctx, budget, outer)
						if err != nil {
							return err
						}
						next := parent.spec.Children[i+1]
						if eligible && meaningful && next.Kind() == Paragraph && next.spec.Properties.Task == nil && len(next.spec.Content) > 0 {
							after, err := locate(next.spec.Content[0].Source.Start())
							if err != nil {
								return err
							}
							if after > last && nonblank[after] == nonblank[last+1] {
								prose, err := ordinaryParagraph(ctx, budget, next)
								if err != nil {
									return err
								}
								if prose {
									if err := openingHashGrowth(ctx, budget, len(out), cap(out)); err != nil {
										return err
									}
									if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
										return err
									}
									out = append(out, outer.Span())
								}
							}
						}
					}
				}
			}
			// Inline children are classified only by the candidate/follower helpers.
			// Other blocks retain actual sibling boundaries rather than a flat DFS list.
			if n.Kind() == DocumentKind || n.Kind() == BlockQuote || n.Kind() == List || n.Kind() == ListItem {
				if err := walk(n); err != nil {
					return err
				}
			}
		}
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

func emphasisTextNonblank(ctx context.Context, budget *OperationBudget, n *Node) (bool, error) {
	if !n.spec.Properties.Literal.Present {
		return false, ErrModel
	}
	value := n.spec.Properties.Literal.Value
	for offset := 0; offset < len(value); {
		if err := headingFactCharge(ctx, budget, 5, 0); err != nil {
			return false, err
		}
		r, width := utf8.DecodeRuneInString(value[offset:])
		offset += width
		if !IsUnicodeWhitespace(r) {
			return true, nil
		}
	}
	return false, nil
}

func emphasisLabel(ctx context.Context, budget *OperationBudget, n *Node) (bool, bool, error) {
	if err := headingFactCharge(ctx, budget, int64(len(n.spec.Children))+1, 0); err != nil {
		return false, false, err
	}
	if n.Kind() == Text {
		meaningful, err := emphasisTextNonblank(ctx, budget, n)
		return true, meaningful, err
	}
	if n.Kind() != Emphasis && n.Kind() != Strong {
		return false, false, nil
	}
	meaningful := false
	for _, child := range n.spec.Children {
		ok, text, err := emphasisLabel(ctx, budget, child)
		if err != nil {
			return false, false, err
		}
		if !ok {
			return false, false, nil
		}
		meaningful = meaningful || text
	}
	return true, meaningful, nil
}

func ordinaryParagraph(ctx context.Context, budget *OperationBudget, n *Node) (bool, error) {
	raw := ""
	var walk func(*Node, bool, bool) (bool, error)
	walk = func(n *Node, eligible, alt bool) (bool, error) {
		if err := headingFactCharge(ctx, budget, int64(len(n.spec.Children))+1, 0); err != nil {
			return false, err
		}
		if n.Kind() == InlineHTML {
			if !alt && htmlIDRawText(n.rawTag) {
				if raw == "" && !n.rawClosing {
					raw = n.rawTag
				} else if raw == n.rawTag && n.rawClosing {
					raw = ""
				}
			}
			return false, nil
		}
		// Raw payload cannot supply ordinary text, but its actual closing tag
		// may be nested in a later wrapper. Keep structural traversal alive.
		switch n.Kind() {
		case Text:
			if eligible && (raw == "" || alt) {
				return emphasisTextNonblank(ctx, budget, n)
			}
			return false, nil
		case CodeSpan, SoftBreak, HardBreak:
			return false, nil
		case Emphasis, Strong, Link:
			eligible = false
		case Image:
			eligible = false
			alt = true
		case Paragraph, Strike:
		default:
			return false, ErrModel
		}
		for _, child := range n.spec.Children {
			found, err := walk(child, eligible, alt)
			if err != nil || found {
				return found, err
			}
		}
		return false, nil
	}
	return walk(n, true, false)
}
