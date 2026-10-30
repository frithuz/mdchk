package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// UnclosedHTMLComments borrows only the original completed production witness.
// Its empty option domain and both caller/origin controls remain binding even
// when no unmatched comment exists or the context is unavailable.
func (c RuleContext) UnclosedHTMLComments(ctx context.Context) (out parser.UnclosedHTMLComments, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.UnclosedHTMLComments{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if len(c.options) != 0 {
		return out, parser.ErrModel
	}
	return c.document.UnclosedHTMLComments(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
