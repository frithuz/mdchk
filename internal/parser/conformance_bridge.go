//go:build mdchk_conformance

package parser

import (
	"context"
	"fmt"
)

// ParseConformance is a test-build-only bridge, never a product flavor selector.
// It preserves parseWithProfile's source, outcomes and shared budget authority.
func ParseConformance(ctx context.Context, source Source, budget *OperationBudget, scope string) (ParseResult, error) {
	profiles := map[string]parserProfile{"core": coreProfile, "tables-only": tablesOnlyProfile, "tasks-only": tasksOnlyProfile, "strike-only": strikeOnlyProfile, "bare-only": bareOnlyProfile, "filter-only": filterOnlyProfile, "integrated": gfmInProgressProfile}
	p, ok := profiles[scope]
	if !ok {
		return ParseResult{}, fmt.Errorf("unknown conformance scope %q", scope)
	}
	return parseWithProfile(ctx, source, budget, p), nil
}
