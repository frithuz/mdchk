package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// FencedLanguages borrows the admitted immutable allowlist synchronously and
// preserves original syntax authority, allowance, and caller AND origin controls.
func (c RuleContext) FencedLanguages(ctx context.Context) (out parser.FencedLanguageInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.FencedLanguageInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 16, 0); err != nil {
		return out, err
	}
	allowed, ok := c.options["allowed_languages"].([]string)
	if !ok || len(c.options) != 1 {
		return out, parser.ErrModel
	}
	return c.document.FencedLanguages(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), allowed)
}
