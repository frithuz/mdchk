// Package rules supplies production read-only checks and correction providers.
// Source ownership, decoding, directives, coordinates and budgets remain engine
// concerns. Registrations returns a fresh slice, with no mutable global registry.
package rules

import "github.com/frithuz/mdchk/internal/engine"

func Registrations() []engine.Registration {
	return []engine.Registration{{ID: "MD047", Check: terminatingNewline}}
}
