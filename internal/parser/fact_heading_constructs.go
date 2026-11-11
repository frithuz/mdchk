package parser

import "context"

// HeadingConstruct is one actual inline construct's original source envelope.
// Kind is Emphasis, Strong, CodeSpan, Link or Image. Returned slices are owned
// by the caller; no public tree or decoded label is used to recover syntax.
type HeadingConstruct struct {
	Kind Kind
	Span Span
}

// HeadingConstructs borrows completed production syntax and the original
// allowance. Nested constructs are independent, including image descriptions.
// Code and HTML payload remain opaque. Failure withholds the whole inventory.
func (d *Document) HeadingConstructs(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) ([]HeadingConstruct, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	type frame struct {
		node         *Node
		next         int
		heading, alt bool
	}
	stack := make([]frame, 1)
	stack[0].node = d.root
	var out []HeadingConstruct
	raw := ""
	for len(stack) != 0 {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		f := &stack[len(stack)-1]
		if f.next == len(f.node.spec.Children) {
			stack = stack[:len(stack)-1]
			continue
		}
		n := f.node.spec.Children[f.next]
		f.next++
		heading, alt := f.heading, f.alt
		if n.Kind() == Heading {
			heading, alt, raw = true, false, ""
		}
		if heading && n.Kind() == InlineHTML {
			if !alt && htmlIDRawText(n.rawTag) {
				if raw == "" && !n.rawClosing {
					raw = n.rawTag
				} else if raw == n.rawTag && n.rawClosing {
					raw = ""
				}
			}
			continue
		}
		eligible := heading && (raw == "" || alt)
		if eligible {
			switch n.Kind() {
			case Emphasis, Strong, CodeSpan, Link, Image:
				if err := openingHashGrowth(ctx, budget, len(out), cap(out)); err != nil {
					return nil, err
				}
				if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
					return nil, err
				}
				out = append(out, HeadingConstruct{Kind: n.Kind(), Span: n.Span()})
			default:
				// Other actual kinds are not MD093 constructs.
			}
		}
		descend := false
		if heading {
			switch n.Kind() {
			case Heading, Emphasis, Strong, Strike, Link:
				descend = true // A suppressed wrapper may contain the real raw closer.
			case Image:
				descend, alt = eligible, true // Alt HTML cannot change outer raw state.
			default:
				// Leaves remain opaque; only structural wrappers descend.
			}
		} else {
			descend = n.Kind() == BlockQuote || n.Kind() == List || n.Kind() == ListItem
		}
		if descend {
			if len(stack) == cap(stack) {
				if cap(stack) > int(^uint(0)>>1)/2 {
					return nil, ErrModel
				}
				capacity := 2 * cap(stack)
				if err := headingFactCharge(ctx, budget, int64(len(stack))+1, int64(capacity)); err != nil {
					return nil, err
				}
				next := make([]frame, len(stack), capacity)
				copy(next, stack)
				stack = next
			}
			stack = append(stack, frame{node: n, heading: heading, alt: alt})
		}
	}
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
