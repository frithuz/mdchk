package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
)

// terminatingNewline examines physical boundaries already owned by the engine.
// Every ending, including CRLF, is indivisible. Empty content on trailing lines
// denotes adjacent endings; whitespace content breaks that run. The earliest
// ending in the run is retained, and only its excess suffix is offending.
func terminatingNewline(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	if err := c.Checkpoint(ctx, 1); err != nil {
		return engine.RuleOutcome{Err: err}
	}
	source := c.Source()
	if source.Len() == 0 {
		return engine.RuleOutcome{}
	}
	lines := c.Lines()
	n := lines.LineCount()
	last, err := lines.Line(n)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	start := source.Len()
	message := "File must end with a line-ending sequence."
	if last.Ending.Start() != last.Ending.End() {
		start = last.Ending.End()
		for n > 1 && last.Content.Start() == last.Content.End() {
			if err := c.Checkpoint(ctx, 1); err != nil {
				return engine.RuleOutcome{Err: err}
			}
			n--
			last, err = lines.Line(n)
			if err != nil {
				return engine.RuleOutcome{Err: err}
			}
			start = last.Ending.End()
		}
		if start == source.Len() {
			return engine.RuleOutcome{}
		}
		message = "File must end with exactly one line-ending sequence."
	}
	span, err := source.Span(start, source.Len())
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return engine.RuleOutcome{Findings: []engine.Finding{{Span: span, Message: message}}}
}
