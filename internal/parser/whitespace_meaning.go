package parser

import (
	"context"
	"errors"
	"strings"
)

// SameMeaningAcrossLineEndWhitespace compares two completed parses from this
// operation whose sources differ only in line-end ASCII whitespace: node kinds and
// order, break origins, heading/list/table/code/task/link properties and every
// reference definition. Text values must match, except that spaces and tabs
// directly before a break may differ, including a Text holding nothing else.
// Source coordinates are never compared; callers map those separately.
func (d *Document) SameMeaningAcrossLineEndWhitespace(ctx context.Context, budget *OperationBudget, other *Document) (bool, error) {
	return d.sameMeaning(ctx, budget, other, true, false, nil)
}

// SameMeaningAcrossIndentation is SameMeaningAcrossLineEndWhitespace for sources
// that also differ in the leading spaces of list item lines: list content columns
// may differ, and every other node kind, order, property and text must match.
func (d *Document) SameMeaningAcrossIndentation(ctx context.Context, budget *OperationBudget, other *Document) (bool, error) {
	return d.sameMeaning(ctx, budget, other, true, true, nil)
}

// SameMeaningAcrossListSeparators compares two completed parses from this
// operation whose sources differ only in blank lines between sibling list items.
// It is SameMeaningAcrossLineEndWhitespace with every Text value compared exactly,
// except that a list owning a direct item whose item-marker starts at one of the
// original offsets in markers may change tightness: that is the intended effect.
func (d *Document) SameMeaningAcrossListSeparators(ctx context.Context, budget *OperationBudget, other *Document, markers map[int]bool) (bool, error) {
	if ctx == nil || budget == nil {
		return false, errors.New("nil list separator meaning context or budget")
	}
	return d.sameMeaning(ctx, budget, other, false, false, markers)
}

func (d *Document) sameMeaning(ctx context.Context, budget *OperationBudget, other *Document, breaks, indentation bool, markers map[int]bool) (bool, error) {
	if ctx == nil || budget == nil {
		return false, errors.New("nil whitespace meaning context or budget")
	}
	for _, doc := range []*Document{d, other} {
		if doc == nil || !doc.parsedComplete || doc.commentOperation != budget.identity {
			return false, errors.New("whitespace meaning requires completed parses in this operation")
		}
	}
	type frame struct {
		a, b        *Node
		beforeBreak bool
	}
	stack := []frame{{a: d.root, b: other.root}}
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		ac, bc := f.a.spec.Children, f.b.spec.Children
		if breaks {
			ac, bc = visibleChildren(ac), visibleChildren(bc)
		}
		records := int64(len(f.a.spec.Children) + len(f.b.spec.Children) + len(f.a.htmlTail) + 1)
		if err := headingFactCharge(ctx, budget, records*2, records); err != nil {
			return false, err
		}
		if f.a.Kind() != f.b.Kind() || len(ac) != len(bc) || len(f.a.htmlTail) != len(f.b.htmlTail) {
			return false, nil
		}
		retight := false
		if f.a.Kind() == List && len(markers) != 0 {
			for _, item := range f.a.spec.Children {
				if err := headingFactCharge(ctx, budget, int64(len(item.spec.Markers))+1, 0); err != nil {
					return false, err
				}
				for _, marker := range item.spec.Markers {
					if marker.Role == "item-marker" && markers[marker.Source.Start()] {
						retight = true
					}
				}
			}
		}
		ok, err := sameNodeProperties(ctx, budget, f.a, f.b, breaks && f.beforeBreak, indentation, retight)
		if err != nil || !ok {
			return false, err
		}
		for i := range ac {
			next := i+1 < len(ac) && (ac[i+1].Kind() == SoftBreak || ac[i+1].Kind() == HardBreak)
			stack = append(stack, frame{ac[i], bc[i], next})
		}
		for i := range f.a.htmlTail {
			stack = append(stack, frame{a: f.a.htmlTail[i], b: f.b.htmlTail[i]})
		}
	}
	return sameReferences(ctx, budget, d.references, other.references)
}

// visibleChildren drops whitespace-only Text directly before a break: removing a
// tab after a closing delimiter removes that Text and nothing else.
func visibleChildren(children []*Node) []*Node {
	out := make([]*Node, 0, len(children))
	for i, c := range children {
		if c.Kind() == Text && i+1 < len(children) && (children[i+1].Kind() == SoftBreak || children[i+1].Kind() == HardBreak) && strings.Trim(c.spec.Properties.Literal.Value, " \t") == "" {
			continue
		}
		out = append(out, c)
	}
	return out
}

func sameNodeProperties(ctx context.Context, budget *OperationBudget, a, b *Node, beforeBreak, indentation, retight bool) (bool, error) {
	p, q := a.spec.Properties, b.spec.Properties
	if err := headingFactCharge(ctx, budget, int64(len(p.Literal.Value)+len(q.Literal.Value))+1, 0); err != nil {
		return false, err
	}
	x, y := p.Literal, q.Literal
	if a.Kind() == Text && beforeBreak {
		x.Value = strings.TrimRight(x.Value, " \t")
		y.Value = strings.TrimRight(y.Value, " \t")
	}
	if x != y {
		return false, nil
	}
	if (p.Task == nil) != (q.Task == nil) || (p.Table == nil) != (q.Table == nil) || (p.Row == nil) != (q.Row == nil) || (p.Cell == nil) != (q.Cell == nil) || (p.Heading == nil) != (q.Heading == nil) || (p.List == nil) != (q.List == nil) || (p.Code == nil) != (q.Code == nil) || (p.HTML == nil) != (q.HTML == nil) || (p.Delimiter == nil) != (q.Delimiter == nil) || (p.Break == nil) != (q.Break == nil) || (p.Link == nil) != (q.Link == nil) {
		return false, nil
	}
	if p.Task != nil && (p.Task.Checked != q.Task.Checked || p.Task.CheckSpelling != q.Task.CheckSpelling) {
		return false, nil
	}
	if p.Heading != nil && *p.Heading != *q.Heading {
		return false, nil
	}
	if p.Delimiter != nil && *p.Delimiter != *q.Delimiter {
		return false, nil
	}
	if p.Break != nil && *p.Break != *q.Break {
		return false, nil
	}
	if p.Table != nil {
		if p.Table.Width != q.Table.Width || len(p.Table.Alignments) != len(q.Table.Alignments) {
			return false, nil
		}
		if err := headingFactCharge(ctx, budget, int64(len(p.Table.Alignments)), 0); err != nil {
			return false, err
		}
		for i, alignment := range p.Table.Alignments {
			if alignment != q.Table.Alignments[i] {
				return false, nil
			}
		}
	}
	if p.Row != nil && (p.Row.Role != q.Row.Role || p.Row.RawCells != q.Row.RawCells || p.Row.Padding != q.Row.Padding) {
		return false, nil
	}
	if p.Cell != nil && (p.Cell.Alignment != q.Cell.Alignment || len(p.Cell.EscapedPipes) != len(q.Cell.EscapedPipes)) {
		return false, nil
	}
	if p.List != nil {
		s, t := p.List, q.List
		if s.Ordered != t.Ordered || s.Start != t.Start || s.Delimiter != t.Delimiter || s.Bullet != t.Bullet || !retight && s.Tight != t.Tight || s.MarkerWidth != t.MarkerWidth || !indentation && s.ContentIndent != t.ContentIndent {
			return false, nil
		}
	}
	if p.Code != nil {
		s, t := p.Code, q.Code
		if s.Style != t.Style || s.Fence != t.Fence || s.Width != t.Width || (s.Info == nil) != (t.Info == nil) || s.Info != nil && s.Info.Semantic != t.Info.Semantic {
			return false, nil
		}
	}
	if p.HTML != nil && (p.HTML.BlockForm != q.HTML.BlockForm || len(p.HTML.FilteredTags) != len(q.HTML.FilteredTags)) {
		return false, nil
	}
	if p.Link != nil {
		s, t := p.Link, q.Link
		if err := headingFactCharge(ctx, budget, int64(len(s.Destination.Semantic.Value)+len(t.Destination.Semantic.Value)+len(s.NormalizedLabel.Value)+len(t.NormalizedLabel.Value))+1, 0); err != nil {
			return false, err
		}
		if s.Origin != t.Origin || s.Resolution != t.Resolution || s.NormalizedLabel != t.NormalizedLabel || s.Destination.Semantic != t.Destination.Semantic || (s.ReferenceLabel == nil) != (t.ReferenceLabel == nil) || !sameTitle(s.Title, t.Title) {
			return false, nil
		}
	}
	return true, nil
}

func sameTitle(a, b *Value) bool {
	return (a == nil) == (b == nil) && (a == nil || a.Semantic == b.Semantic)
}

func sameReferences(ctx context.Context, budget *OperationBudget, a, b ReferenceCollection) (bool, error) {
	if a.Availability != b.Availability || a.LookupAvailability != b.LookupAvailability || len(a.Definitions) != len(b.Definitions) || len(a.Lookup) != len(b.Lookup) {
		return false, nil
	}
	for i, definition := range a.Definitions {
		s, t := definition.spec, b.Definitions[i].spec
		if err := headingFactCharge(ctx, budget, int64(len(s.NormalizedLabel.Value)+len(t.NormalizedLabel.Value)+len(s.Destination.Semantic.Value)+len(t.Destination.Semantic.Value))+1, 0); err != nil {
			return false, err
		}
		if s.NormalizedLabel != t.NormalizedLabel || s.Destination.Semantic != t.Destination.Semantic || !sameTitle(s.Title, t.Title) {
			return false, nil
		}
	}
	for label, i := range a.Lookup {
		if err := headingFactCharge(ctx, budget, int64(len(label))+1, 0); err != nil {
			return false, err
		}
		if j, ok := b.Lookup[label]; !ok || i != j {
			return false, nil
		}
	}
	return true, nil
}
