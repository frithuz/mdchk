package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
)

// metadataBlankLineApplies limits MD071 to documents with header recognition
// selected. A disabled selection makes the rule inapplicable, not incomplete.
func metadataBlankLineApplies(ctx context.Context, c engine.RuleContext) (bool, error) {
	if err := c.Checkpoint(ctx, 1); err != nil {
		return false, err
	}
	return c.Context().Frontmatter.Enabled(), nil
}

// metadataBlankLine inspects only the physical line that begins at a complete
// header's body offset. Unterminated headers never reach it because the engine
// withholds syntax-dependent rules on parser uncertainty. Metadata is not decoded.
func metadataBlankLine(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	if err := c.Checkpoint(ctx, 1); err != nil {
		return engine.RuleOutcome{Err: err}
	}
	header, ok := c.Frontmatter()
	if !ok || !header.Closer().Present() {
		return engine.RuleOutcome{}
	}
	source, offset := c.Source(), header.BodyOffset()
	if offset >= source.Len() {
		return engine.RuleOutcome{}
	}
	lines := c.Lines()
	position, err := lines.Position(offset)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	line, err := lines.Line(position.Line)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	text, err := source.Text(line.Content)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	if err := c.Checkpoint(ctx, int64(len(text))+1); err != nil {
		return engine.RuleOutcome{Err: err}
	}
	blank := true
	for i := 0; i < len(text) && blank; i++ {
		blank = text[i] == ' ' || text[i] == '\t'
	}
	if blank {
		return engine.RuleOutcome{}
	}
	span, err := source.Span(offset, offset)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return engine.RuleOutcome{Findings: []engine.Finding{{Span: span, Message: "Frontmatter requires a blank line before the document body."}}}
}
