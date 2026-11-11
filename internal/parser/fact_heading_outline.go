package parser

import (
	"context"
	"errors"
)

// HeadingOutlineCheckpoint binds derived outline work to the completed original
// parse, source index and operation without projecting a heading inventory.
func (d *Document) HeadingOutlineCheckpoint(ctx context.Context, budget *OperationBudget, index LineIndex, work, nodes int64) error {
	if ctx == nil || budget == nil || d == nil || !d.parsedComplete || d.commentOperation != budget.identity || index.source.Token() != d.Source().Token() {
		return ErrModel
	}
	return errors.Join(ctx.Err(), budget.ChargeWork(work), budget.ChargeNodes(nodes))
}
