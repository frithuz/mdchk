package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/frithuz/mdchk/internal/engine"
)

// SelectionOptions separates the invocation directory (for relative explicit
// paths) from discovery's start (for example, an inspect target's parent).
// An empty StartDirectory uses the working directory. BoundaryDirectory, when set,
// caps every discovery access, as required by project MCP. A repository within
// that cap narrows it further. These are directory identities supplied by the
// operation adapter; config paths below the cap must not contain symlinks.
type SelectionOptions struct {
	WorkingDirectory, StartDirectory, BoundaryDirectory, ExplicitPath string
}

// Boundary records the inclusive discovery boundary and nearest repository
// found within the caller's cap. RepositoryRoot is empty outside repositories.
type Boundary struct{ StartDirectory, Directory, RepositoryRoot string }

// Selection is an owned operation snapshot. An empty SelectedPath means built-in
// defaults. AncestorPaths lists policy definitions in top-down order. Only the
// selected definition supplies settings; explicit document targets are unrelated.
type Selection struct {
	Boundary      Boundary
	SelectedPath  string
	AncestorPaths []string
	Resolution    *Resolution
}

// PolicyError identifies an exclusion addition and every earlier governing
// denial. Restrictions retain file, field and source location independently.
type PolicyError struct {
	Origin       Origin
	Restrictions []Origin
}

func (e *PolicyError) Error() string {
	paths := make([]string, len(e.Restrictions))
	for i, o := range e.Restrictions {
		paths[i] = formatOrigin(o)
	}
	return fmt.Sprintf("%s: nonempty exclusions prohibited by %s", formatOrigin(e.Origin), strings.Join(paths, ", "))
}

// discoveryFS is the read-only effect boundary, allowing deterministic access
// failures and filesystem-root traversal to be tested without host fixtures.
// Names are clean absolute paths inside the cap. Implementations must reject
// symlink components and never open nonregular definition files.
type discoveryFS interface {
	lstat(string) (fs.FileInfo, error)
	readRegular(string) ([]byte, error)
}

// Resolve selects one YAML definition, validates strict-parent policies, and
// admits only the selected operation. It never writes or invokes Git, Markdown
// processing, target selection or reporters. Failure returns no partial result.
func Resolve(options SelectionOptions, registry *engine.Registry, providers engine.ValidationProviders, capabilities Capabilities) (*Selection, error) {
	cwd := options.WorkingDirectory
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	var err error
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	start := options.StartDirectory
	if start == "" {
		start = cwd
	} else if !filepath.IsAbs(start) {
		start = filepath.Join(cwd, start)
	}
	start = filepath.Clean(start)
	cap := options.BoundaryDirectory
	if cap == "" {
		cap = filepath.VolumeName(start) + string(filepath.Separator)
	} else if !filepath.IsAbs(cap) {
		cap = filepath.Join(cwd, cap)
	}
	cap = filepath.Clean(cap)
	if !inside(cap, start) {
		return nil, pathError(start, errors.New("discovery start is outside caller boundary"))
	}
	// Establish a nonsymlink caller cap. Adapters with a resolved target or
	// canonical startup root should supply that identity directly.
	canonicalCap, err := filepath.EvalSymlinks(cap)
	if err != nil {
		return nil, pathError(cap, err)
	}
	if canonicalCap != cap {
		return nil, pathError(cap, errors.New("discovery boundary cannot contain symlinks"))
	}
	rel, err := filepath.Rel(cap, start)
	if err != nil {
		return nil, err
	}
	canonicalStart := filepath.Join(canonicalCap, rel)
	root, err := os.OpenRoot(canonicalCap)
	if err != nil {
		return nil, pathError(cap, err)
	}
	defer closeReadHandle(root)
	access := &rootAccess{root: root, base: canonicalCap}
	explicit := options.ExplicitPath
	rawExplicit := explicit
	if explicit != "" {
		if !filepath.IsAbs(explicit) {
			explicit = cwd + string(filepath.Separator) + explicit
			rawExplicit = explicit
		}
		explicit = filepath.Clean(explicit)
		if !inside(cap, explicit) {
			return nil, pathError(explicit, errors.New("explicit configuration is outside caller boundary"))
		}
		r, e := filepath.Rel(cap, explicit)
		if e != nil {
			return nil, e
		}
		explicit = filepath.Join(canonicalCap, r)
	}
	return resolveFS(access, canonicalStart, canonicalCap, explicit, registry, providers, capabilities, rawExplicit)
}

func resolveFS(access discoveryFS, start, cap, explicit string, registry *engine.Registry, providers engine.ValidationProviders, capabilities Capabilities, rawExplicit ...string) (*Selection, error) {
	info, err := access.lstat(start)
	if err != nil {
		return nil, pathError(start, err)
	}
	if !info.IsDir() {
		return nil, pathError(start, errors.New("discovery start must be a directory"))
	}
	boundary := Boundary{StartDirectory: start, Directory: cap}
	// Establish scope before selecting settings, even if a nearer config exists.
	for dir := start; ; dir = filepath.Dir(dir) {
		marker := filepath.Join(dir, ".git")
		found, err := repositoryAt(access, marker)
		if err != nil {
			return nil, err
		}
		if found {
			boundary.Directory, boundary.RepositoryRoot = dir, dir
			break
		}
		if dir == cap {
			break
		}
	}
	selected := explicit
	if explicit != "" {
		if !inside(boundary.Directory, explicit) {
			return nil, pathError(explicit, errors.New("explicit configuration is outside discovery boundary"))
		}
		if len(rawExplicit) > 0 {
			if err := checkExplicitComponents(access, rawExplicit[0], boundary.Directory); err != nil {
				return nil, pathError(explicit, err)
			}
		}
		// A sibling is permitted, but a separate or nested repository is not. Scope
		// checks stop at the same inclusive bound and do not read Git file contents.
		for dir := filepath.Dir(explicit); ; dir = filepath.Dir(dir) {
			if !inside(boundary.Directory, dir) {
				return nil, pathError(explicit, errors.New("explicit configuration is outside discovery boundary"))
			}
			found, e := repositoryAt(access, filepath.Join(dir, ".git"))
			if e != nil {
				return nil, e
			}
			if found && dir != boundary.RepositoryRoot {
				return nil, pathError(explicit, errors.New("explicit configuration belongs to another repository scope"))
			}
			if dir == boundary.Directory {
				break
			}
		}
	} else {
		for dir := start; ; dir = filepath.Dir(dir) {
			path := filepath.Join(dir, Filename)
			_, e := access.lstat(path)
			if e == nil {
				selected = path
				break
			}
			if !errors.Is(e, fs.ErrNotExist) {
				return nil, pathError(path, e)
			}
			if dir == boundary.Directory {
				break
			}
		}
	}
	inherited := Permissions{}
	ancestors := []string{}
	if selected != "" {
		dirs := []string{}
		if dir := filepath.Dir(selected); dir != boundary.Directory {
			for dir = filepath.Dir(dir); ; dir = filepath.Dir(dir) {
				dirs = append(dirs, dir)
				if dir == boundary.Directory {
					break
				}
			}
		}
		slices.Reverse(dirs)
		for _, dir := range dirs {
			path := filepath.Join(dir, Filename)
			_, e := access.lstat(path)
			if errors.Is(e, fs.ErrNotExist) {
				continue
			}
			if e != nil {
				return nil, pathError(path, e)
			}
			v, e := validateFile(access, path, registry, providers, inherited)
			if e != nil {
				return nil, e
			}
			inherited = v.Permissions(inherited)
			ancestors = append(ancestors, path)
		}
	}
	var validated *Validated
	if selected == "" {
		d, e := DecodeObject(map[string]any{"version": Version})
		if e != nil {
			return nil, e
		}
		validated, err = ValidateDefinition(d, registry, providers)
	} else {
		validated, err = validateFile(access, selected, registry, providers, inherited)
	}
	if err != nil {
		return nil, err
	}
	resolved, err := validated.Admit(capabilities, inherited)
	if err != nil {
		return nil, err
	}
	return &Selection{boundary, selected, ancestors, resolved}, nil
}
func validateFile(access discoveryFS, path string, registry *engine.Registry, providers engine.ValidationProviders, inherited Permissions) (*Validated, error) {
	data, err := access.readRegular(path)
	if err != nil {
		return nil, pathError(path, err)
	}
	d, err := DecodeYAML(path, data)
	if err != nil {
		return nil, err
	}
	v, err := ValidateDefinition(d, registry, providers)
	if err != nil {
		return nil, err
	}
	if len(v.settings.Exclude) > 0 && len(inherited.NestedExcludes) > 0 {
		return nil, &PolicyError{cloneOrigin(v.definition.Provenance["exclude"]), clonePermissions(inherited).NestedExcludes}
	}
	return v, nil
}
func repositoryAt(access discoveryFS, path string) (bool, error) {
	info, err := access.lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, pathError(path, err)
	}
	if info.IsDir() || info.Mode().IsRegular() {
		return true, nil
	}
	return false, pathError(path, errors.New("repository marker must be a directory or regular file"))
}
func inside(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func pathError(path string, err error) error {
	return &ValidationError{Origin: Origin{Path: path}, Err: err}
}

// Inspect lexical components before dot cleaning can erase a symlink or a
// nonexistent directory (link/../config does not acquire a symlink exception).
func checkExplicitComponents(access discoveryFS, raw, boundary string) error {
	raw = filepath.FromSlash(raw)
	volume := filepath.VolumeName(raw)
	parts := strings.Split(strings.TrimPrefix(raw[len(volume):], string(filepath.Separator)), string(filepath.Separator))
	prefix := volume + string(filepath.Separator)
	entered := prefix == boundary
	for _, part := range parts[:len(parts)-1] {
		if part == "" {
			continue
		}
		prefix = filepath.Join(prefix, part)
		if !inside(boundary, prefix) {
			if entered {
				return errors.New("explicit configuration traversal escapes discovery boundary")
			}
			continue
		}
		entered = true
		info, err := access.lstat(prefix)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errors.New("explicit configuration path component must be a directory")
		}
	}
	return nil
}
