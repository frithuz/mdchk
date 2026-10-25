package parser

import (
	"context"
	"errors"
	"slices"
)

// FencedBlockFact is an actual parser-owned fenced block. Info retains Markdown
// decoding separately from RawInfo, which addresses its original literal info
// spelling. Content is opaque logical code with original container/ending maps.
// No caller-created fact grants recognition; consumers obtain these from the
// completed Document under the same operation budget.
type FencedBlockFact struct {
	Envelope   Span
	RawInfo    Span
	Info       Value
	Content    Value
	Markers    []Marker
	Fence      byte
	Width      int
	Containers []HeadingContainer
}

// FencedBlocks projects recognized fences only from an actual completed parse
// in this operation. EOF-closed fences are ordinary recognized fences. A stop
// returns no complete-looking partial collection. HTML ID uncertainty is an
// independent projection and does not invalidate these core syntax facts.
func (d *Document) FencedBlocks(ctx context.Context, budget *OperationBudget) ([]FencedBlockFact, error) {
	if ctx == nil || budget == nil || d == nil || !d.parsedComplete || d.commentOperation != budget.identity {
		return nil, errors.New("fenced context is not a completed parse in this operation")
	}
	var out []FencedBlockFact
	var walk func(*Node, []HeadingContainer) error
	walk = func(n *Node, containers []HeadingContainer) error {
		if err := headingFactCharge(ctx, budget, int64(len(n.spec.Children))+1, 0); err != nil {
			return err
		}
		if n.Kind() == CodeBlock {
			code := n.spec.Properties.Code
			if code == nil || code.Style != "fenced" {
				return nil
			}
			if code.Info == nil || !code.Fence.Present || !code.Width.Present {
				return ErrModel
			}
			work := len(n.spec.Content) + len(n.spec.Markers) + len(code.Info.Segments) + len(containers) + 1
			if err := headingFactCharge(ctx, budget, int64(work), int64(work)); err != nil {
				return err
			}
			var rawInfo Span
			if len(code.Info.Segments) != 0 {
				first := code.Info.Segments[0].Source
				last := code.Info.Segments[len(code.Info.Segments)-1].Source
				rawInfo = Span{source: d.Source(), start: first.Start(), end: last.End(), present: true}
			}
			out = append(out, FencedBlockFact{
				Envelope: n.Span(), RawInfo: rawInfo, Info: cloneValue(*code.Info),
				Content: Value{Segments: slices.Clone(n.spec.Content), Semantic: n.spec.Properties.Literal},
				Markers: slices.Clone(n.spec.Markers), Fence: code.Fence.Value,
				Width: code.Width.Value, Containers: slices.Clone(containers),
			})
			return nil // code payload is never Markdown recursively interpreted
		}
		switch n.Kind() {
		case BlockQuote, List, ListItem:
			if err := headingFactCharge(ctx, budget, int64(len(containers))+1, int64(len(containers))+1); err != nil {
				return err
			}
			containers = append(slices.Clone(containers), HeadingContainer{Kind: n.Kind(), Envelope: n.Span()})
		default:
		}
		for _, child := range n.spec.Children {
			if err := walk(child, containers); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(d.root, nil); err != nil {
		return nil, err
	}
	if err := headingFactCharge(ctx, budget, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
