// Facts for MD055.

package parser

import (
	"context"
	"errors"
)

// TableOuterPipeRow is one detached original TableRow. Start and End are the
// row's trimmed content bounds: the line ending, then leading and trailing
// spaces and tabs, are excluded. The pipes are the parser-owned outer
// separators, present only when the row has them.
type TableOuterPipeRow struct {
	Role                      string // header, delimiter, body
	Start, End                int
	LeadingPipe, TrailingPipe Span
}

// TableOuterPipesInventory contains every TableRow of every Table in document
// order; each table starts with its header row. Unknown row properties withhold
// all rows.
type TableOuterPipesInventory struct {
	Rows         []TableOuterPipeRow
	Availability Availability
}

// TableOuterPipes observes the outer pipes the table parser recorded for each
// row, including tables nested in block quotes and list items. It never
// rescans row text for pipes.
func (d *Document) TableOuterPipes(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out TableOuterPipesInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = TableOuterPipesInventory{}
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
	var rows []TableOuterPipeRow
	publish := func(row TableOuterPipeRow) error {
		if e := charge(2, 0); e != nil {
			return e
		}
		capacity, e := grow(len(rows), cap(rows))
		if e != nil {
			return e
		}
		if capacity != cap(rows) {
			next := make([]TableOuterPipeRow, len(rows), capacity)
			copy(next, rows)
			rows = next
		}
		if e = charge(1, 1); e != nil {
			return e
		}
		rows = append(rows, row)
		return nil
	}
	var stack []*Node
	push := func(n *Node) error {
		capacity, e := grow(len(stack), cap(stack))
		if e != nil {
			return e
		}
		if capacity != cap(stack) {
			next := make([]*Node, len(stack), capacity)
			copy(next, stack)
			stack = next
		}
		if e = charge(1, 1); e != nil {
			return e
		}
		stack = append(stack, n)
		return nil
	}
	if err = push(d.root); err != nil {
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
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil || !n.spec.Envelope.present || n.spec.Envelope.SourceToken() != index.source.Token() || n.spec.Envelope.start < 0 || n.spec.Envelope.end < n.spec.Envelope.start || n.spec.Envelope.end > index.source.Len() {
			return out, ErrModel
		}
		switch n.Kind() {
		case DocumentKind, BlockQuote, List, ListItem, Table:
		case TableRow:
			p := n.spec.Properties.Row
			if p == nil {
				return TableOuterPipesInventory{Availability: Unavailable}, nil
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
			row := TableOuterPipeRow{Role: p.Role, Start: start, End: end, LeadingPipe: p.LeadingPipe, TrailingPipe: p.TrailingPipe}
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
			if err = push(n.spec.Children[i]); err != nil {
				return out, err
			}
		}
	}
	return TableOuterPipesInventory{Rows: rows, Availability: Available}, nil
}
