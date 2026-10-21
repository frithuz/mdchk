package result

import "github.com/frithuz/mdchk/internal/parser"

// ConfigurationOrigin identifies an approved setting's textual or object origin.
// Position is absent for a supplied typed object.
type ConfigurationOrigin struct {
	Path         string
	PropertyPath string
	Position     *parser.Position
}
