package rules

import (
	"context"
	"errors"
	"strings"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules/internal/headingcase"
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
	edits := c.InteriorEdits()
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
		finding := engine.Finding{Span: span, Message: "Heading must follow the configured capitalization style."}
		finding.FixEligible, err = capitalizationEligible(ctx, c, edits, style, names, scalars, span)
		if err != nil {
			out.Err = err
			return out
		}
		out.Findings = append(out.Findings, finding)
	}
	out.Err = c.HeadingCapitalizationCheckpoint(ctx, 0, 0)
	return out
}

func capitalizationFinding(ctx context.Context, c engine.RuleContext, style string, names []string, scalars []parser.HeadingScalar) (out parser.Span, err error) {
	defer func() {
		err = errors.Join(err, c.HeadingCapitalizationCheckpoint(ctx, 0, 0))
		if err != nil {
			out = parser.Span{}
		}
	}()
	checkpoint := func(work, nodes int64) error { return c.HeadingCapitalizationCheckpoint(ctx, work, nodes) }
	protected, err := headingcase.Names(names, scalars, checkpoint)
	if err != nil {
		return parser.Span{}, err
	}
	err = headingcase.Targets(style, scalars, protected, checkpoint, func(i int) bool {
		out = scalars[i].Token
		return false
	})
	return out, err
}

// capitalizationEligible reports whether every required initial of the heading
// can be corrected together, each in an editable context under the finding. A
// heading that no blank line separates from leading front matter stays manual.
func capitalizationEligible(ctx context.Context, c engine.RuleContext, edits *engine.RuleInteriorEdits, style string, names []string, scalars []parser.HeadingScalar, anchor parser.Span) (bool, error) {
	if header, present := c.Document().Frontmatter(); present && header.Span().End() > 0 && header.Span().End() <= anchor.Start() {
		if err := c.HeadingCapitalizationCheckpoint(ctx, int64(anchor.Start()-header.Span().End())+1, 0); err != nil {
			return false, err
		}
		gap, err := c.Source().Span(header.Span().End()-1, anchor.Start())
		if err != nil {
			return false, err
		}
		text, err := c.Source().Text(gap)
		if err != nil {
			return false, err
		}
		if strings.Count(text, "\n") < 2 {
			return false, nil
		}
	}
	h, err := headingcase.Analyze(c.Source(), style, names, scalars, func(work, nodes int64) error { return c.HeadingCapitalizationCheckpoint(ctx, work, nodes) })
	if err != nil || !h.Ready {
		return false, err
	}
	for _, i := range h.Targets {
		ok, err := edits.Eligible(ctx, "MD063", anchor, scalars[i].Token)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}
