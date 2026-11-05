package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// TableOuterPipes retains both caller and originating controls and returns only
// detached table row outer-pipe observations. It grants no alternate parse or correction authority.
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
