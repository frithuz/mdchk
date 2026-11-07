// Package findings grows finding slices under the rule's section budget.
package findings

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// Append adds f to items, charging the section budget before it grows the slice.
func Append(ctx context.Context, c engine.RuleContext, items []engine.Finding, f engine.Finding) ([]engine.Finding, error) {
	if len(items) == cap(items) {
		if cap(items) > int(^uint(0)>>1)/2 {
			return nil, parser.ErrModel
		}
		capacity := max(1, cap(items)*2)
		if err := c.SectionCheckpoint(ctx, int64(len(items))+int64(capacity)+1, int64(capacity)); err != nil {
			return nil, err
		}
		next := make([]engine.Finding, len(items), capacity)
		copy(next, items)
		items = next
	}
	return append(items, f), nil
}
