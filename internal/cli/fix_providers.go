package cli

import (
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/rules"
)

// configuredCorrections is a build-time provider boundary, independent of
// project data. Rule packages supply reusable production producers and proof.
// A compiled acceptance overlay replaces this file, not the adapter or validator.
func configuredCorrections(registry *engine.Registry) (*fix.Validator, fix.Producer, error) {
	validator, err := fix.NewValidator(registry, rules.CorrectionCheckers())
	return validator, rules.Corrections, err
}
