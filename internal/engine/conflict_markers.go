package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// ConflictMarkers projects the complete original physical marker inventory under
// caller AND originating controls, without options or correction authority.
func (c RuleContext) ConflictMarkers(ctx context.Context) (out parser.ConflictMarkerInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.ConflictMarkerInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if len(c.options) != 0 {
		return out, parser.ErrModel
	}
	return c.document.ConflictMarkers(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
