package parser

import "context"

// OpeningBlockFact describes the first actual root body block after introductory
// comment-only blocks. Point is its original zero-width start, or original EOF
// for an empty body. Level is zero unless that block is an actual Heading.
type OpeningBlockFact struct {
	Point Span
	Level int
}

// OpeningBlock borrows completed parser ownership under the original operation
// and source-bound index. Recognized headers and removed reference definitions
// are not root children. Failure withholds the fact, including at publication.
func (d *Document) OpeningBlock(ctx context.Context, budget *OperationBudget, index LineIndex) (out OpeningBlockFact, err error) {
	if ctx == nil || budget == nil || d == nil || !d.parsedComplete || d.commentOperation != budget.identity || index.source.Token() != d.Source().Token() {
		return OpeningBlockFact{}, ErrModel
	}
	if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
		return OpeningBlockFact{}, err
	}
	defer func() {
		if err == nil {
			err = headingFactCharge(ctx, budget, 0, 0)
		}
		if err != nil {
			out = OpeningBlockFact{}
		}
	}()
	start := d.Source().Len()
	level := 0
	for _, n := range d.root.spec.Children {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return OpeningBlockFact{}, err
		}
		comments, err := d.introductoryComments(ctx, budget, n)
		if err != nil {
			return OpeningBlockFact{}, err
		}
		if comments {
			continue
		}
		start = n.Span().Start()
		if n.Kind() == Heading {
			if n.spec.Properties.Heading == nil {
				return OpeningBlockFact{}, ErrModel
			}
			level = n.spec.Properties.Heading.Level
		}
		break
	}
	if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
		return OpeningBlockFact{}, err
	}
	point, err := d.Source().Span(start, start)
	if err != nil {
		return OpeningBlockFact{}, err
	}
	return OpeningBlockFact{Point: point, Level: level}, nil
}

// introductoryComments checks actual confirmed comment coverage, not an HTML
// spelling or visible-text approximation. It scans only uncovered original gaps;
// comment payload stays borrowed and opaque. Each block is considered once.
func (d *Document) introductoryComments(ctx context.Context, budget *OperationBudget, block *Node) (bool, error) {
	cursor, stop := block.Span().Start(), block.Span().End()
	seen, outside := false, false
	raw := ""
	gap := func(end int) (bool, error) {
		if end < cursor || end > stop {
			return false, ErrModel
		}
		if err := headingFactCharge(ctx, budget, int64(end-cursor)+1, 0); err != nil {
			return false, err
		}
		for i := cursor; i < end; i++ {
			if (i-cursor)&255 == 0 {
				if err := headingFactCharge(ctx, budget, 0, 0); err != nil {
					return false, err
				}
			}
			switch d.Source().content[i] {
			case ' ', '\t', '\r', '\n':
			default:
				return false, nil
			}
		}
		return true, nil
	}
	var walk func(*Node) error
	walk = func(n *Node) error {
		if outside {
			return nil
		}
		if err := headingFactCharge(ctx, budget, int64(len(n.spec.Children)+len(n.htmlTail)+len(n.spec.Markers))+1, 0); err != nil {
			return err
		}
		if n.rawTag != "" {
			if raw == "" && !n.rawClosing {
				raw = n.rawTag
			} else if raw == n.rawTag && n.rawClosing {
				raw = ""
			}
		}
		open, close := n.commentOpen, n.commentClose
		for _, marker := range n.spec.Markers {
			if marker.Role == "html-comment-open" {
				open = marker.Source
			} else if marker.Role == "html-comment-close" {
				close = marker.Source
			}
		}
		if open.Present() && raw == "" {
			end := n.Span().End()
			if close.Present() {
				end = close.End()
			}
			if open.SourceToken() != d.Source().Token() || end < open.End() || end > stop {
				return ErrModel
			}
			blank, err := gap(open.Start())
			if err != nil {
				return err
			}
			if !blank {
				outside = true
				return nil
			}
			seen, cursor = true, end
		}
		for _, child := range n.spec.Children {
			if err := walk(child); err != nil {
				return err
			}
		}
		for _, tail := range n.htmlTail {
			if err := walk(tail); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(block); err != nil {
		return false, err
	}
	if outside || !seen {
		return false, nil
	}
	return gap(stop)
}
