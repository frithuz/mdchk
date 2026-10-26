package parser

import (
	"context"
	"errors"
	"slices"
	"strings"
)

// HeadingContainer retains original syntax ancestry. It is not a final TOC
// eligibility decision: marked regions and the consumer's level policy are
// separate work. Quote/list headings still have ordinary anchor identities.
type HeadingContainer struct {
	Kind     Kind
	Envelope Span
}

// HeadingFact is a parser-confirmed Markdown heading, separate from arbitrary
// HTML ID targets. Style remains "atx" or "setext"; Closed is true only for an
// ATX heading with a parser-retained closing marker (including empty headings).
// Visible contains semantic inline text, including image alt
// descriptions, rather than source markup or link destinations/titles.
// OpeningSeparator is present only for nonempty raw ATX content, from the
// opening marker end to the first raw content byte. Empty ATX and Setext
// headings have an absent separator. HasRawContent is independent of Visible.
// ClosedOpeningGap and ClosedClosingGap are actual delimiter gaps of closed
// ATX headings. Raw-empty headings have only ClosedOpeningGap (the shared gap).
// Neither field changes OpeningSeparator or derives bounds from Visible.
// Label is the complete original raw Content envelope, or the present
// opening.End point for raw-empty ATX. It does not derive bounds from Visible.
type HeadingFact struct {
	Label            Span
	ClosedOpeningGap Span
	ClosedClosingGap Span
	OpeningSeparator Span
	HasRawContent    bool
	Envelope         Span
	Level            int
	Style            string
	Closed           bool
	Visible          string
	Containers       []HeadingContainer
}

// Headings projects source-ordered facts only from an actual completed parse in
// the same operation. All construction and text copying share its existing
// budget. An interrupted collection publishes no partial complete-looking list.
func (d *Document) Headings(ctx context.Context, budget *OperationBudget) ([]HeadingFact, error) {
	if ctx == nil || budget == nil || d == nil || !d.parsedComplete || d.commentOperation != budget.identity {
		return nil, errors.New("heading context is not a completed parse in this operation")
	}
	var out []HeadingFact
	var walk func(*Node, []HeadingContainer) error
	walk = func(n *Node, containers []HeadingContainer) error {
		if err := headingFactCharge(ctx, budget, int64(len(n.spec.Children))+1, 0); err != nil {
			return err
		}
		if n.Kind() == Heading {
			if n.spec.Properties.Heading == nil {
				return ErrModel
			}
			visible, err := headingVisibleText(ctx, budget, n)
			if err != nil {
				return err
			}
			if err := headingFactCharge(ctx, budget, int64(len(containers))+1, int64(len(containers))+1); err != nil {
				return err
			}
			closed := false
			var opening, closing Span
			for _, marker := range n.spec.Markers {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return err
				}
				closed = closed || marker.Role == "closing"
				if marker.Role == "closing" {
					closing = marker.Source
				}
				if marker.Role == "opening" {
					opening = marker.Source
				}
			}
			h := n.spec.Properties.Heading
			var separator Span
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return err
			}
			hasContent := len(n.spec.Content) != 0
			if h.Style == "atx" && hasContent {
				if !opening.Present() {
					return ErrModel
				}
				var err error
				separator, err = opening.source.Span(opening.End(), n.spec.Content[0].Source.Start())
				if err != nil {
					return err
				}
			}
			var label Span
			if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
				return err
			}
			if hasContent {
				label, err = n.spec.Content[0].Source.source.Span(n.spec.Content[0].Source.Start(), n.spec.Content[len(n.spec.Content)-1].Source.End())
			} else if h.Style == "atx" && opening.Present() {
				label, err = opening.source.Span(opening.End(), opening.End())
			} else {
				return ErrModel
			}
			if err != nil {
				return err
			}
			var closedOpening, closedClosing Span
			if h.Style == "atx" && closed {
				if err := headingFactCharge(ctx, budget, 3, 0); err != nil {
					return err
				}
				if !opening.Present() || !closing.Present() {
					return ErrModel
				}
				var err error
				if hasContent {
					closedOpening = separator
					closedClosing, err = opening.source.Span(n.spec.Content[len(n.spec.Content)-1].Source.End(), closing.Start())
				} else {
					closedOpening, err = opening.source.Span(opening.End(), closing.Start())
				}
				if err != nil {
					return err
				}
			}
			out = append(out, HeadingFact{Label: label, ClosedOpeningGap: closedOpening, ClosedClosingGap: closedClosing, OpeningSeparator: separator, HasRawContent: hasContent, Envelope: n.Span(), Level: h.Level, Style: h.Style, Closed: closed, Visible: visible, Containers: slices.Clone(containers)})
			return nil
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

func headingFactCharge(ctx context.Context, budget *OperationBudget, work, nodes int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := budget.ChargeWork(work); err != nil {
		return err
	}
	return budget.ChargeNodes(nodes)
}

func walkHeadingVisible(ctx context.Context, budget *OperationBudget, heading *Node, write func(*Node, string) error) error {
	raw := ""
	var walk func(*Node, bool) error
	walk = func(n *Node, alt bool) error {
		if err := headingFactCharge(ctx, budget, int64(len(n.spec.Children))+1, 0); err != nil {
			return err
		}
		if n.Kind() == InlineHTML {
			if !alt && htmlIDRawText(n.rawTag) {
				if raw == "" && !n.rawClosing {
					raw = n.rawTag
				} else if raw == n.rawTag && n.rawClosing {
					raw = ""
				}
			}
			return nil
		}
		if raw != "" && !alt {
			return nil
		}
		switch n.Kind() {
		case Text, CodeSpan:
			if !n.spec.Properties.Literal.Present {
				return ErrModel
			}
			return write(n, n.spec.Properties.Literal.Value)
		case SoftBreak, HardBreak:
			return write(n, "\n")
		case Image:
			alt = true
		case Heading, Emphasis, Strong, Strike, Link:
		default:
			return ErrModel
		}
		for _, child := range n.spec.Children {
			if err := walk(child, alt); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(heading, false)
}

func headingVisibleText(ctx context.Context, budget *OperationBudget, heading *Node) (string, error) {
	var text strings.Builder
	err := walkHeadingVisible(ctx, budget, heading, func(_ *Node, value string) error {
		if err := headingFactCharge(ctx, budget, 3*int64(len(value))+1, 0); err != nil {
			return err
		}
		_, err := text.WriteString(value)
		return err
	})
	if err != nil {
		return "", err
	}
	return text.String(), nil
}
