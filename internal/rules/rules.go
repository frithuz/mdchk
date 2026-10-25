// Package rules supplies production read-only checks and correction providers.
// Source ownership, decoding, directives, coordinates and budgets remain engine
// concerns. Registrations returns a fresh slice, with no mutable global registry.
package rules

import "github.com/frithuz/mdchk/internal/engine"

func Registrations() []engine.Registration {
	return []engine.Registration{{ID: "MD047", Check: terminatingNewline}, {ID: "MD001", Check: headingIncrements}, {ID: "MD003", Check: headingStyle}, {ID: "MD018", Check: openingHashSpacing}, {ID: "MD019", Check: extraOpeningHashSpaces}}
}
