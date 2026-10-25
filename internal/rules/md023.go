package rules

import (
	"context"
	"github.com/frithuz/mdchk/internal/engine"
)

func indentedHeadings(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	markers, err := c.IndentedHeadings(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	out := engine.RuleOutcome{}
	for _, marker := range markers {
		work := int64(1)
		if len(out.Findings) == cap(out.Findings) {
			work += int64(len(out.Findings))
		}
		if err := c.Checkpoint(ctx, work); err != nil {
			out.Err = err
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: marker, Message: "Heading must not be indented."})
	}
	return out
}
