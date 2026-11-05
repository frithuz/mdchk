package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// TableLayout retains both caller and originating controls and returns only
// detached table cell field and text bounds. It grants no alternate parse or correction authority.
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
