package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/files"
	"github.com/frithuz/mdchk/internal/result"
)

// lintInvocation owns the complete preparation result for later target loading
// and execution. Document arguments never influence configuration discovery.
type lintInvocation struct {
	options                         lintOptions
	selection                       *config.Selection
	plan                            *config.ExecutionPlan
	workingDirectory, exclusionBase string
	exclusions                      *config.Patterns
	targetSelection                 *files.Selection
	acquisition                     *files.Acquisition
	result                          *result.Result
}

func prepareLint(options lintOptions) (*lintInvocation, error) {
	// No production rule callbacks are implemented yet. Preserve the complete
	// catalog, including unavailable default rules, independently of that fact.
	registry, err := engine.NewCatalogRegistry(nil)
	if err != nil {
		return nil, err
	}
	return prepareLintWith(options, registry, engine.ValidationProviders{}, config.Capabilities{CommonMark: true})
}

// prepareLintWith supplies real build providers explicitly. Tests can exercise
// future registered checks without mutable package globals or production stubs.
func prepareLintWith(options lintOptions, registry *engine.Registry, providers engine.ValidationProviders, capabilities config.Capabilities) (*lintInvocation, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("working directory: %w", err)
	}
	selection, err := config.Resolve(config.SelectionOptions{
		WorkingDirectory: cwd, ExplicitPath: options.config,
	}, registry, providers, capabilities)
	if err != nil {
		return nil, err
	}
	base := cwd
	if selection.SelectedPath != "" {
		base = filepath.Dir(selection.SelectedPath)
	}
	return &lintInvocation{
		options: options, selection: selection, plan: selection.Resolution.ExecutionPlan(), workingDirectory: cwd,
		exclusionBase: base, exclusions: selection.Resolution.Exclude,
	}, nil
}

// excludes applies the selected definition's compiled list to a native target
// path and its ancestor directories, including explicitly named descendants.
// The caller supplies directory identity; this seam performs no target IO.
// The shared files provider owns traversal, eligibility and symlink checks;
// source loading is separate work.
func (inv *lintInvocation) excludes(target string, isDir bool) (bool, error) {
	if !filepath.IsAbs(target) {
		target = filepath.Join(inv.workingDirectory, target)
	}
	return files.Excludes(inv.exclusionBase, target, isDir, inv.exclusions)
}
