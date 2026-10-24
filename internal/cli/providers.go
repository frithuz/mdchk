package cli

import (
	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/rules"
)

// configuredProviders constructs the immutable availability boundary for this
// build. Keep the full catalog and its defaults as checks become available.
// An executable acceptance fixture replaces this file at build time;
// production has no runtime provider switches or placeholder callbacks.
func configuredProviders() (*engine.Registry, engine.ValidationProviders, config.Capabilities, error) {
	registry, err := engine.NewCatalogRegistry(rules.Registrations())
	return registry, engine.ValidationProviders{}, config.Capabilities{CommonMark: true, GFM: true, DirectiveProcessing: true, FrontmatterYAML: true, FrontmatterTOML: true}, err
}
