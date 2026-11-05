package engine

import (
	"context"
	"errors"
	"strconv"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
)

// HiddenCharacters returns hidden character spans under both caller and
// originating controls. Allowed entries match by numeric code point value.
func (c RuleContext) HiddenCharacters(ctx context.Context) (out []parser.Span, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	if err = c.SectionCheckpoint(ctx, 2, 0); err != nil {
		return nil, err
	}
	values, ok := c.options["allowed_codepoints"].([]string)
	if !ok || len(c.options) != 1 {
		return nil, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, int64(len(values))+1, int64(len(values))); err != nil {
		return nil, err
	}
	allowed := make([]rune, len(values))
	for i, value := range values {
		if err = c.SectionCheckpoint(ctx, int64(len(value))+1, 0); err != nil {
			return nil, err
		}
		n, e := strconv.ParseUint(value, 16, 32)
		if e != nil || n > utf8.MaxRune {
			return nil, parser.ErrModel
		}
		allowed[i] = rune(n)
	}
	return c.document.HiddenCharacters(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), allowed)
}
