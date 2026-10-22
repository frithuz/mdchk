package cli

import (
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
)

// configuredCorrections is a build-time provider boundary, independent of
// project data. The first production correction is supplied by issue #145.
// A compiled acceptance overlay replaces this file, not the adapter or validator.
func configuredCorrections(registry *engine.Registry) (*fix.Validator, fix.Producer, error) {
	validator, err := fix.NewValidator(registry, nil)
	return validator, nil, err
}
