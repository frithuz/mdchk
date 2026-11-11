// Rule fact facades for the tables rules.

package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// TableOuterPipes retains both caller and originating controls and returns only
// detached table row outer-pipe observations. It grants no alternate parse or correction authority.
// Serves MD055.
func (c RuleContext) TableOuterPipes(ctx context.Context) (out parser.TableOuterPipesInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.TableOuterPipesInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	switch style, _ := c.options["style"].(string); style {
	case "consistent", "leading_and_trailing", "none":
	default:
		return out, parser.ErrModel
	}
	if len(c.options) != 1 {
		return out, parser.ErrModel
	}
	return c.document.TableOuterPipes(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// TableCellCounts retains both caller and originating controls and returns only
// detached table body row cell counts. It grants no alternate parse or correction authority.
// Serves MD056.
func (c RuleContext) TableCellCounts(ctx context.Context) (out parser.TableCellCountsInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.TableCellCountsInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if len(c.options) != 0 {
		return out, parser.ErrModel
	}
	return c.document.TableCellCounts(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// TableSpacing projects detached missing-side original points around tables
// under both caller and originating operation controls. It accepts no rule
// options.
// Serves MD058.
func (c RuleContext) TableSpacing(ctx context.Context) (out []parser.TableSpacingFact, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	if len(c.options) != 0 {
		return nil, parser.ErrModel
	}
	return c.document.TableSpacing(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// ThematicSpacing projects detached missing-side original points under both
// caller and originating operation controls. It accepts no rule options.
// Serves MD058 and MD065.
func (c RuleContext) ThematicSpacing(ctx context.Context) (out []parser.ThematicSpacingFact, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	if len(c.options) != 0 {
		return nil, parser.ErrModel
	}
	return c.document.ThematicSpacing(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// TableLayout retains both caller and originating controls and returns only
// detached table cell field and text bounds. It grants no alternate parse or correction authority.
// Serves MD060.
func (c RuleContext) TableLayout(ctx context.Context) (out parser.TableLayoutInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.TableLayoutInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	switch style, _ := c.options["style"].(string); style {
	case "aligned", "compact", "tight":
	default:
		return out, parser.ErrModel
	}
	if len(c.options) != 1 {
		return out, parser.ErrModel
	}
	return c.document.TableLayout(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// PipeRowCandidates retains both caller and originating controls and returns only
// detached Paragraph pipe-row candidate lines. It grants no alternate parse or correction authority.
// Serves MD075.
func (c RuleContext) PipeRowCandidates(ctx context.Context) (out parser.PipeRowCandidatesInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.PipeRowCandidatesInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if len(c.options) != 0 {
		return out, parser.ErrModel
	}
	return c.document.PipeRowCandidates(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
