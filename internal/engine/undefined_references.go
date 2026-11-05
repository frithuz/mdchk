package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// UndefinedReferences retains both caller and originating controls and returns
// only detached observations. It grants no alternate parse or correction
// authority.
func (c RuleContext) UndefinedReferences(ctx context.Context) (out parser.UndefinedReferenceInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.UndefinedReferenceInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if len(c.options) != 0 {
		return out, parser.ErrModel
	}
	return c.document.UndefinedReferences(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
