// Correction facts for MD049.

package parser

import (
	"context"
	"errors"
)

// ItalicSwapsPreserved reports, for each italic fact in swaps, whether
// rewriting both of its one-byte markers to marker keeps the parse. The
// document is reparsed with every swap applied and compared through
// SameMeaningAcrossItalicMarkers. When the reparse differs, only the swaps
// whose italic survived it at the same markers are applied to one more
// reparse; if that still differs, no swap is preserved.
func (d *Document) ItalicSwapsPreserved(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, swaps []DelimiterFact, marker byte) ([]bool, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	if marker != '*' && marker != '_' {
		return nil, ErrModel
	}
	content := index.source.content
	for _, f := range swaps {
		if f.Kind != Emphasis || f.Width != 1 || f.Marker == marker {
			return nil, ErrModel
		}
		for _, s := range [2]Span{f.Opening, f.Closing} {
			if !s.Present() || s.source.Token() != index.source.Token() || s.end-s.start != 1 || content[s.start] != f.Marker {
				return nil, ErrModel
			}
		}
	}
	out := make([]bool, len(swaps))
	active := make([]bool, len(swaps))
	for i := range active {
		active[i] = true
	}
	for pass := 0; pass < 2; pass++ {
		if err := headingFactCharge(ctx, budget, int64(len(content)+len(swaps))*2+8, int64(len(swaps))+2); err != nil {
			return nil, err
		}
		edited := []byte(content)
		allowed := make(map[int]bool, len(swaps))
		for i, f := range swaps {
			if active[i] {
				edited[f.Opening.start], edited[f.Closing.start] = marker, marker
				allowed[f.Opening.start] = true
			}
		}
		if len(allowed) == 0 {
			return out, nil
		}
		parsed := ParseWithOptions(ctx, NewSource(edited), budget, options)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if parsed.Status != Complete || parsed.Document == nil || len(parsed.Uncertainty) != 0 {
			return out, nil
		}
		same, err := d.SameMeaningAcrossItalicMarkers(ctx, budget, parsed.Document, allowed, marker)
		if err != nil {
			return nil, err
		}
		if same {
			copy(out, active)
			return out, nil
		}
		survived, err := italicPairs(ctx, budget, parsed.Document, marker)
		if err != nil {
			return nil, err
		}
		for i, f := range swaps {
			active[i] = active[i] && survived[[2]int{f.Opening.start, f.Closing.start}]
		}
	}
	return out, nil
}

// SameMeaningAcrossItalicMarkers compares two completed parses from this
// operation whose sources differ only in rewritten italic delimiter bytes.
// Every node kind, order, property, Text value and reference must match, and
// every emphasis and strong node keeps its marker offsets and width. Only an
// italic whose opening marker starts at an offset in allowed may change its
// marker, and only to marker.
func (d *Document) SameMeaningAcrossItalicMarkers(ctx context.Context, budget *OperationBudget, other *Document, allowed map[int]bool, marker byte) (bool, error) {
	if ctx == nil || budget == nil || other == nil {
		return false, errors.New("nil italic marker meaning context or budget")
	}
	a, err := delimiterNodes(ctx, budget, d)
	if err != nil {
		return false, err
	}
	b, err := delimiterNodes(ctx, budget, other)
	if err != nil {
		return false, err
	}
	if len(a) != len(b) {
		return false, nil
	}
	for i, x := range a {
		y := b[i]
		if x.kind != y.kind || x.width != y.width || x.opening != y.opening || x.closing != y.closing {
			return false, nil
		}
		if x.marker != y.marker && (x.kind != Emphasis || x.width != 1 || !allowed[x.opening] || y.marker != marker) {
			return false, nil
		}
	}
	return d.sameMeaning(ctx, budget, other, false, false, false, nil, 0, false, false, true)
}

// delimiterNode is one emphasis or strong node by its marker offsets.
type delimiterNode struct {
	kind             Kind
	marker           byte
	width            int
	opening, closing int
}

// delimiterNodes lists every emphasis and strong node of a completed parse in
// document order.
func delimiterNodes(ctx context.Context, budget *OperationBudget, d *Document) ([]delimiterNode, error) {
	if d == nil || !d.parsedComplete || d.commentOperation != budget.identity {
		return nil, errors.New("italic marker meaning requires completed parses in this operation")
	}
	var out []delimiterNode
	stack := []*Node{d.root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if err := headingFactCharge(ctx, budget, int64(len(n.spec.Children)+len(n.htmlTail)+len(n.spec.Markers))+1, 1); err != nil {
			return nil, err
		}
		if n.Kind() == Emphasis || n.Kind() == Strong {
			p := n.spec.Properties.Delimiter
			if p == nil {
				return nil, ErrModel
			}
			item := delimiterNode{kind: n.Kind(), marker: p.Marker, width: p.Width, opening: -1, closing: -1}
			for _, m := range n.spec.Markers {
				switch m.Role {
				case "opening":
					item.opening = m.Source.start
				case "closing":
					item.closing = m.Source.start
				}
			}
			out = append(out, item)
		}
		for i := len(n.htmlTail) - 1; i >= 0; i-- {
			stack = append(stack, n.htmlTail[i])
		}
		for i := len(n.spec.Children) - 1; i >= 0; i-- {
			stack = append(stack, n.spec.Children[i])
		}
	}
	return out, nil
}

// italicPairs indexes the opening and closing offsets of every italic of a
// completed parse whose marker is marker.
func italicPairs(ctx context.Context, budget *OperationBudget, d *Document, marker byte) (map[[2]int]bool, error) {
	nodes, err := delimiterNodes(ctx, budget, d)
	if err != nil {
		return nil, err
	}
	out := make(map[[2]int]bool, len(nodes))
	for _, n := range nodes {
		if n.kind == Emphasis && n.width == 1 && n.marker == marker {
			out[[2]int{n.opening, n.closing}] = true
		}
	}
	return out, nil
}
