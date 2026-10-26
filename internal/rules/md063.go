package rules

import (
	"context"
	"errors"
	"unicode"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func headingCapitalization(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	style, names, err := c.HeadingCapitalizationOptions(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	headings, err := c.HeadingScalars(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	out := engine.RuleOutcome{}
	for _, scalars := range headings {
		span, err := capitalizationFinding(ctx, c, style, names, scalars)
		if err != nil {
			out.Err = err
			return out
		}
		if !span.Present() {
			continue
		}
		work := int64(1)
		if len(out.Findings) == cap(out.Findings) {
			work += int64(len(out.Findings))
		}
		if err := c.HeadingCapitalizationCheckpoint(ctx, work, 1); err != nil {
			out.Err = err
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: span, Message: "Heading must follow the configured capitalization style."})
	}
	out.Err = c.HeadingCapitalizationCheckpoint(ctx, 0, 0)
	return out
}

func capitalizationWord(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsNumber(r) || unicode.Is(unicode.Pc, r)
}

func capitalizationFinding(ctx context.Context, c engine.RuleContext, style string, names []string, scalars []parser.HeadingScalar) (out parser.Span, err error) {
	defer func() {
		err = errors.Join(err, c.HeadingCapitalizationCheckpoint(ctx, 0, 0))
		if err != nil {
			out = parser.Span{}
		}
	}()
	protected, err := capitalizationNames(ctx, c, names, scalars)
	if err != nil {
		return parser.Span{}, err
	}
	seen := false
	for i, s := range scalars {
		if err := c.HeadingCapitalizationCheckpoint(ctx, 16, 0); err != nil {
			return parser.Span{}, err
		}
		if style == "title" {
			word := capitalizationWord(s.Scalar)
			if (s.Scalar == '\'' || s.Scalar == '\u2019') && i > 0 && i+1 < len(scalars) {
				word = capitalizationWord(scalars[i-1].Scalar) && capitalizationWord(scalars[i+1].Scalar)
			}
			if !word {
				seen = false
				continue
			}
		}
		if !unicode.IsUpper(s.Scalar) && !unicode.IsLower(s.Scalar) && !unicode.IsTitle(s.Scalar) {
			continue
		}
		if seen {
			continue
		}
		seen = true
		if !unicode.IsLower(s.Scalar) || s.Code || (len(protected) > 0 && protected[i]) {
			continue
		}
		if !s.Token.Present() {
			return parser.Span{}, parser.ErrModel
		}
		return s.Token, nil
	}
	return parser.Span{}, nil
}

// Name matches settle before initials are examined. Empty names are no-ops;
// every repeated scalar comparison and protection write uses the original work.
func capitalizationNames(ctx context.Context, c engine.RuleContext, names []string, scalars []parser.HeadingScalar) ([]bool, error) {
	if len(names) == 0 {
		return nil, nil
	}
	if err := c.HeadingCapitalizationCheckpoint(ctx, int64(len(scalars))+1, int64(len(scalars))); err != nil {
		return nil, err
	}
	protected := make([]bool, len(scalars))
	for _, name := range names {
		if err := c.HeadingCapitalizationCheckpoint(ctx, 1, 0); err != nil {
			return nil, err
		}
		if name == "" {
			continue
		}
		for start := range scalars {
			if err := c.HeadingCapitalizationCheckpoint(ctx, 8, 0); err != nil {
				return nil, err
			}
			if start > 0 && capitalizationWord(scalars[start-1].Scalar) {
				continue
			}
			end, offset := start, 0
			for offset < len(name) && end < len(scalars) {
				if err := c.HeadingCapitalizationCheckpoint(ctx, 9, 0); err != nil {
					return nil, err
				}
				r, width := utf8.DecodeRuneInString(name[offset:])
				if r == utf8.RuneError && width == 1 || r != scalars[end].Scalar {
					break
				}
				offset += width
				end++
			}
			if offset != len(name) || (end < len(scalars) && capitalizationWord(scalars[end].Scalar)) {
				continue
			}
			for i := start; i < end; i++ {
				if err := c.HeadingCapitalizationCheckpoint(ctx, 1, 0); err != nil {
					return nil, err
				}
				protected[i] = true
			}
		}
	}
	return protected, nil
}
