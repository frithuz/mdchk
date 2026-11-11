// Rule fact facades for the HTML-and-comments rules.

package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// EmbeddedHTMLAllowedTags returns detached entry storage from admitted immutable
// options. The existing permissive string-list domain is deliberately unchanged.
// Serves MD033.
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
// Serves MD033.
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

// UnclosedHTMLComments borrows only the original completed production witness.
// Its empty option domain and both caller/origin controls remain binding even
// when no unmatched comment exists or the context is unavailable.
// Serves MD086.
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

// HTMLMarkdownCandidates retains the completed original production witness and
// caller AND origin controls. It grants no MD033 options or correction access.
// Serves MD091.
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

// ConflictMarkers projects the complete original physical marker inventory under
// caller AND originating controls, without options or correction authority.
// Serves MD092.
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
