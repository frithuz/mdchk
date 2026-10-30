package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// EmbeddedHTMLAllowedTags returns detached entry storage from admitted immutable
// options. The existing permissive string-list domain is deliberately unchanged.
func (c RuleContext) EmbeddedHTMLAllowedTags(ctx context.Context) (out []string, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = nil
		}
	}()
	if err = c.SectionCheckpoint(ctx, 16, 0); err != nil {
		return nil, err
	}
	list, ok := c.options["allowed_tags"].([]string)
	if !ok || len(c.options) != 1 {
		return nil, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, int64(len(list))+1, int64(len(list))); err != nil {
		return nil, err
	}
	out = make([]string, len(list))
	copy(out, list)
	return out, nil
}

// EmbeddedHTMLTags keeps the complete original production witness and both
// caller/origin controls. A failed local context is distinct from interruption.
func (c RuleContext) EmbeddedHTMLTags(ctx context.Context) (out parser.EmbeddedHTML, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.EmbeddedHTML{}
		}
	}()
	if _, err = c.EmbeddedHTMLAllowedTags(ctx); err != nil {
		return out, err
	}
	return c.document.EmbeddedHTMLTags(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
