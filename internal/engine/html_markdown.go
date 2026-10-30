package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// HTMLMarkdownCandidates retains the completed original production witness and
// caller AND origin controls. It grants no MD033 options or correction access.
func (c RuleContext) HTMLMarkdownCandidates(ctx context.Context) (out parser.HTMLMarkdown, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.HTMLMarkdown{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if len(c.options) != 0 {
		return out, parser.ErrModel
	}
	return c.document.HTMLMarkdownCandidates(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
