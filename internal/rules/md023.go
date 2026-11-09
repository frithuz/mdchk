package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func indentedHeadings(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	headings, err := c.IndentedHeadings(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	out := engine.RuleOutcome{}
	edits := c.InteriorEdits()
	for _, heading := range headings {
		work := int64(1)
		if len(out.Findings) == cap(out.Findings) {
			work += int64(len(out.Findings))
		}
		if err := c.Checkpoint(ctx, work); err != nil {
			out.Err = err
			return out
		}
		finding := engine.Finding{Span: heading.Marker, Message: "Heading must not be indented."}
		finding.FixEligible, err = indentationEligible(ctx, c, edits, heading)
		if err != nil {
			out.Err = err
			return out
		}
		out.Findings = append(out.Findings, finding)
	}
	return out
}

// indentationEligible withholds a heading with a lazy line or a partially
// consumed tab, and requires every deletion to pass the shared interior gate
// keyed on the marker.
func indentationEligible(ctx context.Context, c engine.RuleContext, edits *engine.RuleInteriorEdits, heading parser.IndentedHeading) (bool, error) {
	if heading.Lazy || len(heading.Lines) == 0 {
		return false, nil
	}
	for _, line := range heading.Lines {
		if err := c.Checkpoint(ctx, 1); err != nil {
			return false, err
		}
		if line.PartialTab {
			return false, nil
		}
		ok, err := edits.Eligible(ctx, "MD023", heading.Marker, line.Span)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}
