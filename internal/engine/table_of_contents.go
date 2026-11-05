package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// TableOfContents borrows the admitted MD073 marker spellings and returns the
// detached marker and block inventory of the original completed parse under
// both caller and originating controls. An unavailable inventory is distinct
// from a complete one without markers; failure withholds the whole inventory.
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
