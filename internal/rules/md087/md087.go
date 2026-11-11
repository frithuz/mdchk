// Package md087 registers MD087. Its check, engine.CheckUnusedSuppressions,
// lives in the engine.
package md087

import "github.com/frithuz/mdchk/internal/engine"

// Registration returns the MD087 rule ID and the engine's check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD087", Check: engine.CheckUnusedSuppressions}
}
