package md023

import (
	"context"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules/internal/headingindent"
)

// Correction regenerates MD023 indentation deletions from the round's actual
// findings through the shared headingindent helper.
func Correction(ctx context.Context, c fix.RoundContext, findings []result.Diagnostic) ([]fix.Group, error) {
	return headingindent.Groups(ctx, c, findings)
}

// Checker returns the MD023 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD023", Check: headingindent.Check}
}
