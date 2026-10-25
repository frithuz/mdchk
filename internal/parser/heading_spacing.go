package parser

import (
	"context"
	"errors"
)

// HeadingSpacingFact describes the whole physical heading and its consecutive
// adjacent blank body lines. Edge flags mean that the run reaches the actual
// document edge, including a zero-length run. Points precede physical endings.
// This projection does not change HeadingFact or its semantic consumers.
type HeadingSpacingFact struct {
	Before, After           Span
	BeforeBlank, AfterBlank int
	BeforeEdge, AfterEdge   bool
}

type headingBodyLine struct {
	prefixEnd, before, after int
	blank                    bool
}

// HeadingSpacing uses only retained container markers from the completed parse
// and the original physical index. Prefix ownership is local to each line;
// lazy text has no synthesized prefix. Work is bounded by source bytes, tree
// records and logarithmic line lookups, independently of configured minima.
// Any failure withholds the entire inventory.
func (d *Document) HeadingSpacing(ctx context.Context, budget *OperationBudget, index LineIndex) ([]HeadingSpacingFact, error) {
	if ctx == nil || budget == nil || d == nil || !d.parsedComplete || d.commentOperation != budget.identity || index.source.Token() != d.Source().Token() {
		return nil, errors.New("heading spacing requires the original completed parse and line index")
	}
	lines := index.lines
	if err := headingFactCharge(ctx, budget, int64(len(lines))+1, int64(len(lines))); err != nil {
		return nil, err
	}
	body := make([]headingBodyLine, len(lines))
	// Binary search actual physical starts; End-1, rather than exclusive End,
	// selects the last heading line even when its envelope retains a terminator.
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
	var out []HeadingSpacingFact
	var walk func(*Node) error
	walk = func(n *Node) error {
		if err := headingFactCharge(ctx, budget, int64(len(n.spec.Children))+1, 0); err != nil {
			return err
		}
		switch n.Kind() {
		case BlockQuote, ListItem:
			for _, marker := range n.spec.Markers {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return err
				}
				if marker.Role != "quote-prefix" && marker.Role != "item-prefix" && marker.Role != "item-indent" {
					continue
				}
				i, err := locate(marker.Source.Start())
				if err != nil {
					return err
				}
				if marker.Source.End() > lines[i].end {
					return ErrModel
				}
				// Prefixes can share a partially consumed tab. Removing its source byte
				// also removes only ASCII whitespace; any residual columns remain blank.
				body[i].prefixEnd = max(body[i].prefixEnd, marker.Source.End())
			}
		case Heading:
			first, err := locate(n.spec.Envelope.Start())
			if err != nil {
				return err
			}
			last, err := locate(n.spec.Envelope.End() - 1)
			if err != nil {
				return err
			}
			// Reserve the fact and both possible side observations before publication.
			if err := headingFactCharge(ctx, budget, 1, 3); err != nil {
				return err
			}
			if err := openingHashGrowth(ctx, budget, len(out), cap(out)); err != nil {
				return err
			}
			before, _ := index.source.Span(lines[first].start, lines[first].start)
			after, _ := index.source.Span(lines[last].end, lines[last].end)
			out = append(out, HeadingSpacingFact{Before: before, After: after})
			return nil
		default:
		}
		for _, child := range n.spec.Children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(d.root); err != nil {
		return nil, err
	}
	run := 0
	for i, line := range lines {
		if err := headingFactCharge(ctx, budget, int64(line.end-line.start)+1, 0); err != nil {
			return nil, err
		}
		body[i].before = run
		body[i].blank = true
		start := max(line.start, body[i].prefixEnd)
		for j := start; j < line.end; j++ {
			if (j-start)&255 == 0 {
				if err := headingFactCharge(ctx, budget, 0, 0); err != nil {
					return nil, err
				}
			}
			if b := index.source.content[j]; b != ' ' && b != '\t' {
				body[i].blank = false
				break
			}
		}
		if body[i].blank {
			run++
		} else {
			run = 0
		}
	}
	run = 0
	for i := len(lines) - 1; i >= 0; i-- {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		body[i].after = run
		if body[i].blank {
			run++
		} else {
			run = 0
		}
	}
	for i := range out {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		first, err := locate(out[i].Before.Start())
		if err != nil {
			return nil, err
		}
		last, err := locate(out[i].After.Start())
		if err != nil {
			return nil, err
		}
		out[i].BeforeBlank = body[first].before
		out[i].AfterBlank = body[last].after
		out[i].BeforeEdge = body[first].before == first
		out[i].AfterEdge = body[last].after == len(lines)-last-1
	}
	if err := headingFactCharge(ctx, budget, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
