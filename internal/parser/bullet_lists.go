package parser

import (
	"context"
	"errors"
)

// BulletListFact is one actual unordered list. Adjacent reports that the sibling
// block directly before or after it is another unordered list: CommonMark ends a
// list only at a bullet change there, so normalizing either bullet merges them.
type BulletListFact struct {
	Span     Span
	Bullet   byte
	Adjacent bool
}

// BulletLists returns every unordered list in source preorder, across container
// nesting. Values are detached from mutable syntax.
func (d *Document) BulletLists(ctx context.Context, budget *OperationBudget) ([]BulletListFact, error) {
	if ctx == nil || budget == nil {
		return nil, errors.New("nil bullet list context or budget")
	}
	var out []BulletListFact
	stack := []*Node{d.root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		children := n.spec.Children
		if err := headingFactCharge(ctx, budget, int64(len(children))*2+1, int64(len(children))); err != nil {
			return nil, err
		}
		for i, child := range children {
			if bullet, ok := bulletList(child); ok {
				_, before := bulletListAt(children, i-1)
				_, after := bulletListAt(children, i+1)
				out = append(out, BulletListFact{Span: child.Span(), Bullet: bullet, Adjacent: before || after})
			}
		}
		for i := len(children) - 1; i >= 0; i-- {
			stack = append(stack, children[i])
		}
	}
	return out, ctx.Err()
}

func bulletListAt(children []*Node, i int) (byte, bool) {
	if i < 0 || i >= len(children) {
		return 0, false
	}
	return bulletList(children[i])
}

func bulletList(n *Node) (byte, bool) {
	p := n.spec.Properties.List
	if n.Kind() != List || p == nil || p.Ordered || !p.Bullet.Present {
		return 0, false
	}
	return p.Bullet.Value, true
}

// SameStructureAcrossBullets compares the block structure of two completed parses
// from this operation: every container and leaf block keeps its kind, order and
// mapped source envelope, and every list and item keeps its ordered state, start,
// delimiter, tight state and widths. Bullets may differ, and inline content is
// not compared; callers prove the changed bullets and other edits separately.
func (d *Document) SameStructureAcrossBullets(ctx context.Context, budget *OperationBudget, other *Document, mapSpan func(Span) (Span, bool, error)) (bool, error) {
	if ctx == nil || budget == nil || mapSpan == nil {
		return false, errors.New("nil bullet structure context or budget")
	}
	for _, doc := range []*Document{d, other} {
		if doc == nil || !doc.parsedComplete || doc.commentOperation != budget.identity {
			return false, errors.New("bullet structure requires completed parses in this operation")
		}
	}
	type frame struct{ a, b *Node }
	stack := []frame{{d.root, other.root}}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if err := headingFactCharge(ctx, budget, int64(len(f.a.spec.Children)+len(f.b.spec.Children))*2+1, 1); err != nil {
			return false, err
		}
		if f.a.Kind() != f.b.Kind() {
			return false, nil
		}
		if f.a.Kind() != DocumentKind {
			mapped, ok, err := mapSpan(f.a.Span())
			if err != nil || !ok || mapped.Start() != f.b.Span().Start() || mapped.End() != f.b.Span().End() {
				return false, err
			}
		}
		if p, q := f.a.spec.Properties.List, f.b.spec.Properties.List; (p == nil) != (q == nil) || p != nil && (p.Ordered != q.Ordered || p.Start != q.Start || p.Delimiter != q.Delimiter || p.Tight != q.Tight || p.MarkerWidth != q.MarkerWidth || p.Padding != q.Padding || p.ContentIndent != q.ContentIndent) {
			return false, nil
		}
		switch f.a.Kind() {
		case DocumentKind, BlockQuote, List, ListItem:
		default:
			continue
		}
		ac, bc := f.a.spec.Children, f.b.spec.Children
		if len(ac) != len(bc) {
			return false, nil
		}
		for i := range ac {
			stack = append(stack, frame{ac[i], bc[i]})
		}
	}
	return true, ctx.Err()
}
