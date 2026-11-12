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
// Source coordinates are never compared; callers map those separately. Bullets
// are not compared either: only an MD004 edit in the same round can change one,
// and its own checker proves it.
func (d *Document) SameMeaningAcrossLineEndWhitespace(ctx context.Context, budget *OperationBudget, other *Document) (bool, error) {
	return d.sameMeaning(ctx, budget, other, true, false, false, nil, 0, false, false, false)
}

// SameMeaningAcrossIndentation is SameMeaningAcrossLineEndWhitespace for sources
// that also differ in the leading spaces of list item lines: list content columns
// may differ, and every other node kind, order, property and text must match.
func (d *Document) SameMeaningAcrossIndentation(ctx context.Context, budget *OperationBudget, other *Document) (bool, error) {
	return d.sameMeaning(ctx, budget, other, true, true, false, nil, 0, false, false, false)
}

// SameMeaningAcrossProseSpaces compares two completed parses from this operation
// whose sources differ only in deleted interior prose SPACE bytes. It is
// SameMeaningAcrossListSeparators without the tightness exception, except that a
// Text value may differ only in the length of its SPACE runs: every run stays
// present and nothing else changes.
func (d *Document) SameMeaningAcrossProseSpaces(ctx context.Context, budget *OperationBudget, other *Document) (bool, error) {
	return d.sameMeaning(ctx, budget, other, false, false, true, nil, 0, false, false, false)
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
	return d.sameMeaning(ctx, budget, other, false, false, false, markers, 0, false, false, false)
}

// SameMeaningAcrossTabs is SameMeaningAcrossLineEndWhitespace for sources that
// also differ where a TAB became a run of spaces: Text values may differ
// only by reading every TAB on either side as that many spaces.
func (d *Document) SameMeaningAcrossTabs(ctx context.Context, budget *OperationBudget, other *Document, spaces int) (bool, error) {
	if spaces < 1 || spaces > MaxTabSpaces {
		return false, ErrModel
	}
	return d.sameMeaning(ctx, budget, other, true, false, false, nil, spaces, false, false, false)
}

// SameMeaningAcrossInsertedSpaces compares two completed parses from this
// operation whose sources differ only where an ASCII SPACE was inserted between
// prose scalars. Every node kind, order, property and reference must match, and
// a Text value may differ only by holding extra ASCII SPACEs.
func (d *Document) SameMeaningAcrossInsertedSpaces(ctx context.Context, budget *OperationBudget, other *Document) (bool, error) {
	return d.sameMeaning(ctx, budget, other, false, false, false, nil, 0, true, false, false)
}

// SameMeaningAcrossNameCase compares two completed parses from this operation
// whose sources differ only in the capitalization of configured names. It is
// SameMeaningAcrossListSeparators without the tightness exception, except that
// Text and CodeBlock values may differ as long as their full case folds match.
func (d *Document) SameMeaningAcrossNameCase(ctx context.Context, budget *OperationBudget, other *Document) (bool, error) {
	return d.sameMeaning(ctx, budget, other, false, false, false, nil, 0, false, true, false)
}

func (d *Document) sameMeaning(ctx context.Context, budget *OperationBudget, other *Document, breaks, indentation, spaces bool, markers map[int]bool, tabs int, inserted, folds, delimiters bool) (bool, error) {
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
		ok, err := sameNodeProperties(ctx, budget, f.a, f.b, breaks && f.beforeBreak, indentation, spaces, retight, tabs, inserted, folds, delimiters)
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

func sameNodeProperties(ctx context.Context, budget *OperationBudget, a, b *Node, beforeBreak, indentation, spaces, retight bool, tabs int, inserted, folds, delimiters bool) (bool, error) {
	p, q := a.spec.Properties, b.spec.Properties
	if err := headingFactCharge(ctx, budget, int64(len(p.Literal.Value)+len(q.Literal.Value))+1, 0); err != nil {
		return false, err
	}
	x, y := p.Literal, q.Literal
	if a.Kind() == Text && beforeBreak {
		x.Value = strings.TrimRight(x.Value, " \t")
		y.Value = strings.TrimRight(y.Value, " \t")
	}
	if a.Kind() == Text && spaces && len(y.Value) <= len(x.Value) {
		x.Value, y.Value = collapseSpaces(x.Value), collapseSpaces(y.Value)
	}
	if folds && (a.Kind() == Text || a.Kind() == CodeBlock) {
		fx, err := FoldCase(ctx, budget, x.Value)
		if err != nil {
			return false, err
		}
		fy, err := FoldCase(ctx, budget, y.Value)
		if err != nil {
			return false, err
		}
		if x.Present != y.Present || fx != fy {
			return false, nil
		}
	} else if tabs > 0 && a.Kind() == Text {
		if x.Present != y.Present || !equalAcrossTabs(x.Value, y.Value, tabs) {
			return false, nil
		}
	} else if inserted && a.Kind() == Text {
		if x.Present != y.Present || !equalAcrossInsertedSpaces(x.Value, y.Value) {
			return false, nil
		}
	} else if x != y {
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
	if p.Delimiter != nil && *p.Delimiter != *q.Delimiter && !(delimiters && a.Kind() == Emphasis && p.Delimiter.Width == q.Delimiter.Width) {
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
		if s.Ordered != t.Ordered || s.Start != t.Start || s.Delimiter != t.Delimiter || !retight && s.Tight != t.Tight || s.MarkerWidth != t.MarkerWidth || !indentation && s.ContentIndent != t.ContentIndent {
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

// equalAcrossTabs compares a and b reading each TAB as spaces ASCII spaces,
// without allocating the expansion.
func equalAcrossTabs(a, b string, spaces int) bool {
	i, j, ri, rj := 0, 0, 0, 0
	next := func(s string, k, r *int) (byte, bool) {
		if *r > 0 {
			*r--
			if *r == 0 {
				*k++
			}
			return ' ', true
		}
		if *k >= len(s) {
			return 0, false
		}
		if s[*k] == '\t' {
			*r = spaces - 1
			if *r == 0 {
				*k++
			}
			return ' ', true
		}
		*k++
		return s[*k-1], true
	}
	for {
		x, more := next(a, &i, &ri)
		y, other := next(b, &j, &rj)
		if more != other || x != y {
			return false
		}
		if !more {
			return true
		}
	}
}

// collapseSpaces shortens every run of SPACE bytes to one SPACE.
func collapseSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := range len(s) {
		if s[i] == ' ' && i > 0 && s[i-1] == ' ' {
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// equalAcrossInsertedSpaces reports whether b is a with zero or more ASCII
// SPACEs inserted.
func equalAcrossInsertedSpaces(a, b string) bool {
	i := 0
	for j := 0; j < len(b); j++ {
		if i < len(a) && a[i] == b[j] {
			i++
		} else if b[j] != ' ' {
			return false
		}
	}
	return i == len(a)
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
