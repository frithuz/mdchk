// Rule fact facades for the content-and-navigation rules.

package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// ProhibitedTerms returns prohibited term occurrence spans under both caller
// and originating controls. Option copying uses the original finite allowance.
// Serves MD061.
func (c RuleContext) ProhibitedTerms(ctx context.Context) (out []parser.Span, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	if err = c.SectionCheckpoint(ctx, 3, 0); err != nil {
		return nil, err
	}
	values, ok := c.options["terms"].([]string)
	if !ok || len(c.options) != 2 {
		return nil, parser.ErrModel
	}
	caseSensitive, ok := c.options["case_sensitive"].(bool)
	if !ok {
		return nil, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, int64(len(values))+1, int64(len(values))); err != nil {
		return nil, err
	}
	terms := make([]string, len(values))
	for i, value := range values {
		if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
			return nil, err
		}
		terms[i] = value
	}
	return c.document.ProhibitedTerms(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), terms, caseSensitive)
}

// TableOfContents borrows the admitted MD073 marker spellings and returns the
// detached marker and block inventory of the original completed parse under
// both caller and originating controls. An unavailable inventory is distinct
// from a complete one without markers; failure withholds the whole inventory.
// Serves MD073.
func (c RuleContext) TableOfContents(ctx context.Context) (out parser.TableOfContents, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.TableOfContents{}
		}
	}()
	start, okStart := c.options["start_marker"].(string)
	end, okEnd := c.options["end_marker"].(string)
	if !okStart || !okEnd || start == "" || start == end {
		return out, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, int64(len(start)+len(end))+1, 0); err != nil {
		return out, err
	}
	return c.document.TableOfContents(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), start, end)
}
