package cli

import (
	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
)

// configuredProviders constructs the immutable availability boundary for this
// build. Keep the full catalog and its defaults even before ordinary checks
// exist. An executable acceptance fixture replaces this file at build time;
// production has no runtime provider switches or placeholder callbacks.
func configuredProviders() (*engine.Registry, engine.ValidationProviders, config.Capabilities, error) {
	registry, err := engine.NewCatalogRegistry(nil)
	return registry, engine.ValidationProviders{}, config.Capabilities{CommonMark: true, DirectiveProcessing: true}, err
}
