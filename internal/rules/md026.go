package rules

import (
	"context"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
)

func headingEndPunctuation(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	out := engine.RuleOutcome{}
	option := c.Options()["punctuation"].(string)
	if err := c.HeadingComparisonCheckpoint(ctx, 2*int64(len(option))+1, int64(len(option))+1); err != nil {
		return engine.RuleOutcome{Err: err}
	}
	punctuation := make(map[rune]struct{}, len(option))
	for offset := 0; offset < len(option); {
		if err := c.HeadingComparisonCheckpoint(ctx, 9, 1); err != nil {
			return engine.RuleOutcome{Err: err}
		}
		r, width := utf8.DecodeRuneInString(option[offset:])
		offset += width
		punctuation[r] = struct{}{}
	}
	terminals, err := c.HeadingTerminals(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	for _, terminal := range terminals {
		if err := c.HeadingComparisonCheckpoint(ctx, 2, 0); err != nil {
			out.Err = err
			return out
		}
		if !terminal.Token.Present() || terminal.Code || terminal.Escaped {
			continue
		}
		if _, ok := punctuation[terminal.Scalar]; !ok {
			continue
		}
		work := int64(1)
		if len(out.Findings) == cap(out.Findings) {
			work += int64(len(out.Findings))
		}
		if err := c.HeadingComparisonCheckpoint(ctx, work, 1); err != nil {
			out.Err = err
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: terminal.Token, Message: "Heading must not end with configured punctuation."})
	}
	return out
}
