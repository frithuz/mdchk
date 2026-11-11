package md003

import (
	"context"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules/internal/headingstyle"
)

// Correction regenerates MD003 heading style groups from the round's actual
// findings through the shared headingstyle rewrites.
func Correction(ctx context.Context, c fix.RoundContext, findings []result.Diagnostic) ([]fix.Group, error) {
	return headingstyle.Groups(ctx, c, findings)
}

// Checker returns the MD003 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD003", Check: headingstyle.Check}
}
