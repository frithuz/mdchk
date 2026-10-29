package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// DelimiterStyle returns detached original delimiter facts without granting
// parse, metadata or correction authority. Both caller and origin remain binding.
func (c RuleContext) DelimiterStyle(ctx context.Context) (out *parser.DelimiterInventory, err error) {
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
	return c.document.DelimiterStyle(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
