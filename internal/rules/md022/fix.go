package md022

import (
	"context"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules/internal/headingblanks"
)

// Correction regenerates MD022 blank line insertions from the round's actual
// findings through the shared headingblanks helper.
func Correction(ctx context.Context, c fix.RoundContext, findings []result.Diagnostic) ([]fix.Group, error) {
	return headingblanks.Groups(ctx, c, findings)
}

// Checker returns the MD022 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD022", Check: headingblanks.Check}
}
