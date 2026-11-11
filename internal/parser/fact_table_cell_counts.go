// Facts for MD056.

package parser

import (
	"context"
	"errors"
)

// TableCellCountRow is one detached original body TableRow. Start and End are
// the row's trimmed content bounds: the line ending, then leading and trailing
// spaces and tabs, are excluded. RawCells is the parser-owned source cell count
// before padding or truncation, and Width is the row's table width.
type TableCellCountRow struct {
	Start, End      int
	RawCells, Width int
}

// TableCellCountsInventory contains every body TableRow of every Table in
// document order. Unknown table or row properties withhold all rows.
type TableCellCountsInventory struct {
	Rows         []TableCellCountRow
	Availability Availability
}

// TableCellCounts observes the cell counts the table parser recorded for each
// body row, including tables nested in block quotes and list items. It never
// rescans row text for pipes.
func (d *Document) TableCellCounts(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out TableCellCountsInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = TableCellCountsInventory{}
		} else if out.Availability != Available {
			out.Rows = nil
		}
	}()
	charge := func(w, n int64) error { return headingFactCharge(ctx, budget, w, n) }
	grow := func(length, capacity int) (int, error) {
		if length < capacity {
			return capacity, nil
		}
		if capacity > int(^uint(0)>>1)/2 {
			return 0, ErrModel
		}
		capacity = max(1, capacity*2)
		if e := charge(int64(length)+int64(capacity)+1, int64(capacity)); e != nil {
			return 0, e
		}
		return capacity, nil
	}
	var rows []TableCellCountRow
	publish := func(row TableCellCountRow) error {
		if e := charge(2, 0); e != nil {
			return e
		}
		capacity, e := grow(len(rows), cap(rows))
		if e != nil {
			return e
		}
		if capacity != cap(rows) {
			next := make([]TableCellCountRow, len(rows), capacity)
			copy(next, rows)
			rows = next
		}
		if e = charge(1, 1); e != nil {
			return e
		}
		rows = append(rows, row)
		return nil
	}
	// Each pending node carries the width of the table that contains it; only
	// a Table's own children have one.
	type pending struct {
		node  *Node
		width int
	}
	var stack []pending
	push := func(n *Node, width int) error {
		capacity, e := grow(len(stack), cap(stack))
		if e != nil {
			return e
		}
		if capacity != cap(stack) {
			next := make([]pending, len(stack), capacity)
			copy(next, stack)
			stack = next
		}
		if e = charge(1, 1); e != nil {
			return e
		}
		stack = append(stack, pending{n, width})
		return nil
	}
	if err = push(d.root, 0); err != nil {
		return out, err
	}
	text := index.source.content
	for {
		if err = charge(16, 0); err != nil {
			return out, err
		}
		if len(stack) == 0 {
			break
		}
		top := stack[len(stack)-1]
		n := top.node
		stack = stack[:len(stack)-1]
		if n == nil || !n.spec.Envelope.present || n.spec.Envelope.SourceToken() != index.source.Token() || n.spec.Envelope.start < 0 || n.spec.Envelope.end < n.spec.Envelope.start || n.spec.Envelope.end > index.source.Len() {
			return out, ErrModel
		}
		width := 0
		switch n.Kind() {
		case DocumentKind, BlockQuote, List, ListItem:
		case Table:
			p := n.spec.Properties.Table
			if p == nil {
				return TableCellCountsInventory{Availability: Unavailable}, nil
			}
			width = p.Width
		case TableRow:
			p := n.spec.Properties.Row
			if p == nil {
				return TableCellCountsInventory{Availability: Unavailable}, nil
			}
			if p.Role != "body" {
				continue
			}
			// The same trimming as validateTableSpec: the line ending, then
			// spaces and tabs at both edges.
			start, end := n.spec.Envelope.start, n.spec.Envelope.end
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
			row := TableCellCountRow{Start: start, End: end, RawCells: p.RawCells, Width: top.width}
			if err = publish(row); err != nil {
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
			if err = push(n.spec.Children[i], width); err != nil {
				return out, err
			}
		}
	}
	return TableCellCountsInventory{Rows: rows, Availability: Available}, nil
}
