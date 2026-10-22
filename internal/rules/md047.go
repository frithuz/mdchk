package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// terminatingNewline examines physical boundaries already owned by the engine.
// Every ending, including CRLF, is indivisible. Empty content on trailing lines
// denotes adjacent endings; whitespace content breaks that run. The earliest
// ending in the run is retained, and only its excess suffix is offending.
func terminatingNewline(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	span, err := TerminalNewline(ctx, c.Source(), c.Lines(), c.Checkpoint)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	if !span.Present() {
		return engine.RuleOutcome{}
	}

	message := "File must end with exactly one line-ending sequence."
	if span.Start() == span.End() {
		message = "File must end with a line-ending sequence."
	}
	finding := engine.Finding{Span: span, Message: message}
	eligible, err := c.TerminalEditEligible(ctx, "MD047", span)
	if err != nil {
		return engine.RuleOutcome{Findings: []engine.Finding{finding}, Err: err}
	}
	finding.FixEligible = eligible
	return engine.RuleOutcome{Findings: []engine.Finding{finding}}

}

// TerminalNewline returns the minimal MD047 obligation using the same immutable
// physical-line facts as analysis. An absent span means no violation.
func TerminalNewline(ctx context.Context, source parser.Source, lines parser.LineIndex, checkpoint func(context.Context, int64) error) (parser.Span, error) {
	if err := checkpoint(ctx, 1); err != nil {
		return parser.Span{}, err
	}
	if lines.Source() != source {
		return parser.Span{}, parser.ErrRange
	}
	if source.Len() == 0 {
		return parser.Span{}, nil
	}
	n := lines.LineCount()
	last, err := lines.Line(n)
	if err != nil {
		return parser.Span{}, err
	}
	start := source.Len()
	if last.Ending.Start() != last.Ending.End() {
		start = last.Ending.End()
		for n > 1 && last.Content.Start() == last.Content.End() {
			if err := checkpoint(ctx, 1); err != nil {
				return parser.Span{}, err
			}
			n--
			last, err = lines.Line(n)
			if err != nil {
				return parser.Span{}, err
			}
			start = last.Ending.End()
		}
		if start == source.Len() {
			return parser.Span{}, nil
		}
	}
	return source.Span(start, source.Len())
}
