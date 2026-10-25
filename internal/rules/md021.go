package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func closedHeadingSpaces(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	headings, err := c.Headings(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	out := engine.RuleOutcome{}
	for _, heading := range headings {
		if err := c.Checkpoint(ctx, 1); err != nil {
			out.Err = err
			return out
		}
		if heading.Style != "atx" || !heading.Closed {
			continue
		}
		for side, separator := range [...]parser.Span{heading.ClosedOpeningGap, heading.ClosedClosingGap} {
			if !separator.Present() {
				continue
			}
			// Reserve source access, the complete byte scan and finding construction
			// before observing this bounded parser-confirmed separator.
			if err := c.Checkpoint(ctx, int64(separator.End()-separator.Start())+2); err != nil {
				out.Err = err
				return out
			}
			text, err := c.Source().Text(separator)
			if err != nil {
				out.Err = err
				return out
			}
			count, start, end := 0, 0, 0
			for i := range len(text) {
				if i > 0 && i&255 == 0 {
					if err := c.Checkpoint(ctx, 0); err != nil {
						out.Err = err
						return out
					}
				}
				if text[i] == ' ' {
					count++
					if count == 2 {
						start = i
					}
					end = i + 1
				}
			}
			if err := c.Checkpoint(ctx, 0); err != nil {
				out.Err = err
				return out
			}
			if count != 1 {
				if count == 0 {
					start, end = 0, 0
					if side == 1 {
						start, end = len(text), len(text)
					}
				}
				span, err := c.Source().Span(separator.Start()+start, separator.Start()+end)
				if err != nil {
					out.Err = err
					return out
				}
				out.Findings = append(out.Findings, engine.Finding{Span: span, Message: "Closed heading delimiter gaps must contain exactly one ASCII space."})
			}
		}
	}
	return out
}
