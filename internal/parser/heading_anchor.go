package parser

import (
	"context"
	"errors"
)

// HeadingAnchorCheckpoint binds a local anchor projection to the completed
// original parse, its actual production profile/options, index and allowance.
// Public ParseResult fields alone cannot establish this private witness.
func (d *Document) HeadingAnchorCheckpoint(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, work, nodes int64) error {
	if ctx == nil || budget == nil || d == nil || !d.parsedComplete || d.commentOperation != budget.identity || index.source.Token() != d.Source().Token() || d.parsedOptions != options || (d.parsedProfile != coreProfile && d.parsedProfile != gfmInProgressProfile) {
		return ErrModel
	}
	return errors.Join(ctx.Err(), budget.ChargeWork(work), budget.ChargeNodes(nodes))
}
