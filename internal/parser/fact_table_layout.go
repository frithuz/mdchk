package parser

import (
	"context"
	"errors"
)

// TableLayoutCell is one detached original source TableCell. FieldStart and
// FieldEnd bound the cell's field: its envelope between separators, clipped to
// the row's trimmed bounds, so a first cell without a leading pipe excludes the
// row indentation and a last cell without a trailing pipe excludes trailing
// blanks. TextStart and TextEnd bound the field without its edge spaces and
// tabs; an empty cell has TextStart == TextEnd == FieldEnd. Parser-added
// padding cells have no source and are never listed.
type TableLayoutCell struct {
	FieldStart, FieldEnd int
	TextStart, TextEnd   int
}

// TableLayoutRow is one detached original TableRow. Start and End are the
// row's trimmed content bounds: the line ending, then leading and trailing
// spaces and tabs, are excluded. LeadingPipe and TrailingPipe report the
// parser-owned outer separators, and Cells lists every source cell in order.
type TableLayoutRow struct {
	Role                      string // header, delimiter, body
	Start, End                int
	LeadingPipe, TrailingPipe bool
	Cells                     []TableLayoutCell
}

// TableLayoutTable lists one Table's rows in source order: its header, its
// delimiter and then its body rows.
type TableLayoutTable struct {
	Rows []TableLayoutRow
}

// TableLayoutInventory contains every Table in document order. Unknown table,
// row or cell properties withhold all tables.
type TableLayoutInventory struct {
	Tables       []TableLayoutTable
	Availability Availability
}

// tableLayoutAppend appends one value with the same metering as the other
// table facts: doubling growth charged for the copy and the new capacity.
func tableLayoutAppend[T any](charge func(w, n int64) error, dst []T, value T) ([]T, error) {
	if e := charge(2, 0); e != nil {
		return nil, e
	}
	if len(dst) == cap(dst) {
		if cap(dst) > int(^uint(0)>>1)/2 {
			return nil, ErrModel
		}
		capacity := max(1, cap(dst)*2)
		if e := charge(int64(len(dst))+int64(capacity)+1, int64(capacity)); e != nil {
			return nil, e
		}
		next := make([]T, len(dst), capacity)
		copy(next, dst)
		dst = next
	}
	if e := charge(1, 1); e != nil {
		return nil, e
	}
	return append(dst, value), nil
}

// TableLayout observes the field and text bounds of every source cell the table
// parser recorded, including tables nested in block quotes and list items. It
// reads only cell envelopes and the parser's outer pipes, and never rescans row
// text for pipes; it scans field bytes only for space and tab edges.
func (d *Document) TableLayout(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out TableLayoutInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = TableLayoutInventory{}
		} else if out.Availability != Available {
			out.Tables = nil
		}
	}()
	charge := func(w, n int64) error { return headingFactCharge(ctx, budget, w, n) }
	text := index.source.content
	valid := func(n *Node) bool {
		return n != nil && n.spec.Envelope.present && n.spec.Envelope.SourceToken() == index.source.Token() && n.spec.Envelope.start >= 0 && n.spec.Envelope.end >= n.spec.Envelope.start && n.spec.Envelope.end <= index.source.Len()
	}
	var tables []TableLayoutTable
	var stack []*Node
	if stack, err = tableLayoutAppend(charge, stack, d.root); err != nil {
		return out, err
	}
	for {
		if err = charge(16, 0); err != nil {
			return out, err
		}
		if len(stack) == 0 {
			break
		}
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !valid(n) {
			return out, ErrModel
		}
		switch n.Kind() {
		case DocumentKind, BlockQuote, List, ListItem:
		case Table:
			if n.spec.Properties.Table == nil {
				return TableLayoutInventory{Availability: Unavailable}, nil
			}
			var table TableLayoutTable
			if err = charge(int64(len(n.spec.Children))+1, 0); err != nil {
				return out, err
			}
			for _, r := range n.spec.Children {
				if err = charge(16, 0); err != nil {
					return out, err
				}
				if !valid(r) || r.Kind() != TableRow {
					return out, ErrModel
				}
				p := r.spec.Properties.Row
				if p == nil {
					return TableLayoutInventory{Availability: Unavailable}, nil
				}
				// The same trimming as validateTableSpec: the line ending, then
				// spaces and tabs at both edges.
				start, end := r.spec.Envelope.start, r.spec.Envelope.end
				if err = charge(int64(end-start)+1, 0); err != nil {
					return out, err
				}
				if end > start && text[end-1] == '\n' {
					end--
				}
				if end > start && text[end-1] == '\r' {
					end--
				}
				for start < end && (text[start] == ' ' || text[start] == '\t') {
					start++
				}
				for end > start && (text[end-1] == ' ' || text[end-1] == '\t') {
					end--
				}
				row := TableLayoutRow{Role: p.Role, Start: start, End: end, LeadingPipe: p.LeadingPipe.Present(), TrailingPipe: p.TrailingPipe.Present()}
				if err = charge(int64(len(r.spec.Children))+1, 0); err != nil {
					return out, err
				}
				for _, c := range r.spec.Children {
					if err = charge(16, 0); err != nil {
						return out, err
					}
					if !valid(c) || c.Kind() != TableCell {
						return out, ErrModel
					}
					if c.spec.Properties.Cell == nil {
						return TableLayoutInventory{Availability: Unavailable}, nil
					}
					fieldStart, fieldEnd := max(c.spec.Envelope.start, start), min(c.spec.Envelope.end, end)
					if fieldEnd < fieldStart {
						return out, ErrModel
					}
					if err = charge(int64(fieldEnd-fieldStart)+1, 0); err != nil {
						return out, err
					}
					textStart, textEnd := fieldStart, fieldEnd
					for textStart < textEnd && (text[textStart] == ' ' || text[textStart] == '\t') {
						textStart++
					}
					for textEnd > textStart && (text[textEnd-1] == ' ' || text[textEnd-1] == '\t') {
						textEnd--
					}
					cell := TableLayoutCell{FieldStart: fieldStart, FieldEnd: fieldEnd, TextStart: textStart, TextEnd: textEnd}
					if row.Cells, err = tableLayoutAppend(charge, row.Cells, cell); err != nil {
						return out, err
					}
				}
				if table.Rows, err = tableLayoutAppend(charge, table.Rows, row); err != nil {
					return out, err
				}
			}
			if tables, err = tableLayoutAppend(charge, tables, table); err != nil {
				return out, err
			}
			continue
		default:
			continue
		}
		if err = charge(int64(len(n.spec.Children))+1, 0); err != nil {
			return out, err
		}
		for i := len(n.spec.Children) - 1; i >= 0; i-- {
			if stack, err = tableLayoutAppend(charge, stack, n.spec.Children[i]); err != nil {
				return out, err
			}
		}
	}
	return TableLayoutInventory{Tables: tables, Availability: Available}, nil
}
