package parser

import (
	"context"
	"math/bits"
	"sort"
)

// ListRepetitions projects maximal identical bullet runs from actual item
// prefixes. Returned spans are detached values borrowing immutable original
// source; they carry no syntax mutation or correction authority.
func (d *Document) ListRepetitions(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) ([]Span, error) {
	items, err := d.ListItems(ctx, budget, index, options)
	if err != nil {
		return nil, err
	}
	type key struct {
		token      SourceToken
		start, end int
		present    bool
	}
	identity := func(s Span) key { return key{s.SourceToken(), s.Start(), s.End(), s.Present()} }
	type state struct {
		line, run int
		marker    Span
		bullet    Optional[byte]
		ordered   bool
	}
	type run struct {
		span  Span
		count int
	}
	// Map capacity and run capacity are reserved before construction, including
	// clean inventories. Keys have constant size, independent of source length.
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, int64(len(items))*2+1, int64(len(items))*2); err != nil {
		return nil, err
	}
	states := make(map[key]state, len(items))
	runs := make([]run, 0, len(items))
	previous := -1
	for _, item := range items {
		// Reserve index comparisons, constant-key lookup/insertion and state work.
		if err := headingFactCharge(ctx, budget, int64(bits.Len(uint(len(index.lines))))+3, 0); err != nil {
			return nil, err
		}
		marker := item.Marker
		if !marker.Present() || marker.SourceToken() != index.source.Token() || marker.Start() < previous || marker.End() <= marker.Start() || item.Item.SourceToken() != index.source.Token() {
			return nil, ErrModel
		}
		line := sort.Search(len(index.lines), func(i int) bool { return index.lines[i].start > marker.Start() }) - 1
		if line < 0 || marker.End() > index.lines[line].end {
			return nil, ErrModel
		}
		current := state{line: line, run: -1, marker: marker, bullet: item.Bullet, ordered: item.Ordered}
		var parent state
		if item.ParentItem.Present() {
			var ok bool
			parent, ok = states[identity(item.ParentItem)]
			if !ok {
				return nil, ErrModel
			}
		}
		if !item.Ordered {
			if !item.Bullet.Present || marker.End()-marker.Start() != 1 || index.source.content[marker.Start()] != item.Bullet.Value {
				return nil, ErrModel
			}
			adjacent := item.ParentItem.Present() && !parent.ordered && parent.line == line && parent.marker.End() < marker.Start() && parent.bullet == item.Bullet
			// Reject other physical lines before borrowing or charging a source gap.
			if adjacent {
				if err := headingFactCharge(ctx, budget, int64(marker.Start()-parent.marker.End()), 0); err != nil {
					return nil, err
				}
				for j := parent.marker.End(); j < marker.Start(); j++ {
					if (j-parent.marker.End())&255 == 0 {
						if err := headingFactCharge(ctx, budget, 0, 0); err != nil {
							return nil, err
						}
					}
					if b := index.source.content[j]; b != ' ' && b != '\t' {
						adjacent = false
						break
					}
				}
			}
			if adjacent {
				if parent.run < 0 || runs[parent.run].span.End() != parent.marker.End() {
					return nil, ErrModel
				}
				current.run = parent.run
				runs[current.run].span.end = marker.End()
				runs[current.run].count++
			} else {
				if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
					return nil, err
				}
				current.run = len(runs)
				runs = append(runs, run{span: marker, count: 1})
			}
		}
		if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
			return nil, err
		}
		states[identity(item.Item)] = current
		previous = marker.End()
	}
	if err := headingFactCharge(ctx, budget, int64(len(runs))+1, int64(len(runs))); err != nil {
		return nil, err
	}
	out := make([]Span, 0, len(runs))
	for _, r := range runs {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		if r.count < 2 {
			continue
		}
		if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
			return nil, err
		}
		out = append(out, r.span)
	}
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
