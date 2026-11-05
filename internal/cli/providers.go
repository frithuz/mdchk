package cli

import (
	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/idna"
	"github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/rules"
)

// configuredProviders constructs the immutable availability boundary for this
// build. Keep the full catalog and its defaults as checks become available.
// Hostname options validate with the pinned Unicode 17.0.0 UTS #46 profile in
// internal/idna; resolved options keep the configured spelling.
// An executable acceptance fixture replaces this file at build time;
// production has no runtime provider switches or placeholder callbacks.
func configuredProviders() (*engine.Registry, engine.ValidationProviders, config.Capabilities, error) {
	registry, err := engine.NewCatalogRegistry(rules.Registrations())
	return registry, engine.ValidationProviders{ValidateHostname: idna.ValidateHostname}, config.Capabilities{CommonMark: true, GFM: true, SlideAnnotation: true, DirectiveProcessing: true, FrontmatterYAML: true, FrontmatterTOML: true, MetadataInterpretation: true, GitHubAnchors: true, ExplicitAnchors: true, ResourceIndex: true, RepresentIndexFile: resource.RepresentNativeIndexFile}, err
}
