package md021

import (
	"context"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules/internal/headingspacing"
)

// Correction regenerates heading spacing groups from the round's actual
// findings through the shared headingspacing helper. It takes MD019 and MD021
// findings together, so identical edits from both rules stay separate groups.
func Correction(ctx context.Context, c fix.RoundContext, findings []result.Diagnostic) ([]fix.Group, error) {
	return headingspacing.Groups(ctx, c, findings)
}

// Checker returns the MD021 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD021", Check: headingspacing.Check}
}
