// Facts for MD051, MD073 and MD080.

package parser

import "context"

// HeadingAnchorCheckpoint binds a local anchor projection to the completed
// original parse, its actual production profile/options, index and allowance.
// Public ParseResult fields alone cannot establish this private witness.
func (d *Document) HeadingAnchorCheckpoint(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, work, nodes int64) error {
	return d.headingProjectionCheckpoint(ctx, budget, index, options, work, nodes)
}
