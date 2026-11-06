package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// LinkDestinations retains both caller and originating controls and returns
// only detached destination observations. It grants no alternate parse or
// correction authority.
func (c RuleContext) LinkDestinations(ctx context.Context) (out parser.LinkDestinationsInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.LinkDestinationsInventory{}
		}
	}()
	if _, err = c.AllowedHosts(ctx); err != nil {
		return out, err
	}
	return c.document.LinkDestinations(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// AllowedHosts returns detached entry storage from admitted immutable options,
// in the spelling the user wrote. It does not canonicalize the entries.
func (c RuleContext) AllowedHosts(ctx context.Context) (out []string, err error) {
	if err = c.SectionCheckpoint(ctx, 16, 0); err != nil {
		return nil, err
	}
	list, ok := c.options["allowed_hosts"].([]string)
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
