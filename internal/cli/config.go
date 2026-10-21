package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
)

// lintInvocation owns the complete preparation result for later target loading
// and execution. Document arguments never influence configuration discovery.
type lintInvocation struct {
	options                         lintOptions
	selection                       *config.Selection
	workingDirectory, exclusionBase string
	exclusions                      *config.Patterns
}

func prepareLint(options lintOptions) (*lintInvocation, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("working directory: %w", err)
	}
	// No production rule callbacks are implemented yet. Preserve the complete
	// catalog, including unavailable default rules, independently of that fact.
	registry, err := engine.NewCatalogRegistry(nil)
	if err != nil {
		return nil, err
	}
	selection, err := config.Resolve(config.SelectionOptions{
		WorkingDirectory: cwd, ExplicitPath: options.config,
	}, registry, engine.ValidationProviders{}, config.Capabilities{CommonMark: true})
	if err != nil {
		return nil, err
	}
	base := cwd
	if selection.SelectedPath != "" {
		base = filepath.Dir(selection.SelectedPath)
	}
	return &lintInvocation{
		options: options, selection: selection, workingDirectory: cwd,
		exclusionBase: base, exclusions: selection.Resolution.Exclude,
	}, nil
}

// excludes applies the selected definition's compiled list to a native target
// path and its ancestor directories, including explicitly named descendants.
// The caller supplies directory identity; this seam performs no target IO.
// Portable traversal, eligibility, symlink checks and loading are separate work.
func (inv *lintInvocation) excludes(target string, isDir bool) (bool, error) {
	if !filepath.IsAbs(target) {
		target = filepath.Join(inv.workingDirectory, target)
	}
	if !strings.EqualFold(filepath.VolumeName(inv.exclusionBase), filepath.VolumeName(target)) {
		return false, nil
	}
	rel, err := filepath.Rel(inv.exclusionBase, target)
	if err != nil {
		return false, err
	}
	inBase := rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
	name := filepath.ToSlash(rel)
	if rel == "." {
		name = ""
	}
	excluded, err := inv.exclusions.Excludes(name, isDir, inBase)
	if err != nil {
		return false, fmt.Errorf("exclusions for %q: %w", target, err)
	}
	return excluded, nil
}

func unavailableLint(_ *lintInvocation, _ io.Writer, stderr io.Writer) int {
	return output(stderr, "mdchk: lint execution is not available in this build; no files were checked\n", exitIncomplete)
}
