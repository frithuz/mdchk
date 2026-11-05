package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// TableCellCounts retains both caller and originating controls and returns only
// detached table body row cell counts. It grants no alternate parse or correction authority.
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
