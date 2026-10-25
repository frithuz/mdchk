// Package files selects filesystem targets without reading document contents.
package files

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/frithuz/mdchk/internal/config"
)

// Options binds one invocation's paths to its already selected configuration.
// Empty Paths scans WorkingDirectory; an empty WorkingDirectory uses os.Getwd.
// ExclusionBase defaults to WorkingDirectory. A selected configuration's adapter
// supplies its directory instead. No configuration or resource scope is inferred
// from target paths.
type Options struct {
	WorkingDirectory, ExclusionBase string
	Paths                           []string
	Exclusions                      *config.Patterns
}

// Target is a cleaned absolute native path, not an acquired or checked file.
// Distinct logical names remain distinct even when they are hard links.
type Target struct {
	Path       string
	identities []pathIdentity
	inspection bool
}
type pathIdentity struct {
	path string
	info fs.FileInfo
}

// SkipReason identifies an intentional selection decision, not an IO failure.
type SkipReason string

const (
	SkipExcluded   SkipReason = "excluded"
	SkipGit        SkipReason = "git-metadata"
	SkipSymlink    SkipReason = "symlink"
	SkipExtension  SkipReason = "extension"
	SkipNonregular SkipReason = "nonregular"
)

// Skipped identifies the observed entry that was skipped. A pruned directory or
// blocking ancestor has one record; unseen descendants are never invented.
type Skipped struct {
	Path   string
	Reason SkipReason
}

// Selection retains independent successes, skips and failures in stable native
// path order. Errors preserve their original causes with errors.Is/As. No target
// has been opened for reading or analyzed. Callers must handle Errors before
// applying empty-selection usage behavior; selection never chooses an exit code.
type Selection struct {
	Targets []Target
	Skipped []Skipped
	Errors  []*fs.PathError
}

// Empty reports whether selection yielded no targets, including on IO failure.
// It does not imply a successful scan or supersede Errors.
func (s *Selection) Empty() bool { return len(s.Targets) == 0 }

type selectionFS interface {
	lstat(string) (fs.FileInfo, error)
	readDir(string) ([]fs.DirEntry, error)
}
type hostFS struct{}

func (hostFS) lstat(p string) (fs.FileInfo, error)     { return os.Lstat(p) }
func (hostFS) readDir(p string) ([]fs.DirEntry, error) { return os.ReadDir(p) }

// Select discovers eligible regular files. Every explicit path component is
// checked before cleaning can erase it, and every discovered entry is Lstat'ed;
// symlinks are skipped, never intentionally followed. This is a selection-time
// snapshot, not an authority to open those names later: acquisition must recheck
// path components and file identity because the filesystem can change.
// Fatal setup errors return nil; target/traversal failures stay in Selection.
func Select(options Options) (*Selection, error) { return selectFS(options, hostFS{}) }

func selectFS(options Options, access selectionFS) (*Selection, error) {
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
	base := options.ExclusionBase
	if base == "" {
		base = cwd
	} else {
		base, err = absoluteFrom(cwd, base)
		if err != nil {
			return nil, err
		}
	}
	base = filepath.Clean(base)
	options.WorkingDirectory, options.ExclusionBase = cwd, base
	paths := slices.Clone(options.Paths)
	if len(paths) == 0 {
		paths = []string{cwd}
	}
	// Keep raw components for symlink checks. Sorting before any effects also makes
	// overlapping explicit roots independent of argument order.
	for i, p := range paths {
		if p == "" {
			p = "."
		}
		p, err = absoluteFrom(cwd, p)
		if err != nil {
			return nil, err
		}
		paths[i] = p
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	walker := selector{
		options: options, access: access,
		selected: map[string]Target{}, skips: map[Skipped]bool{},
		failures: map[failureKey]*fs.PathError{},
		visited:  map[string]bool{}, expanded: map[string]bool{},
	}
	for _, raw := range paths {
		path, ok := walker.checkComponents(raw)
		if ok {
			walker.walk(path, true)
		}
	}
	result := &Selection{}
	for _, target := range walker.selected {
		result.Targets = append(result.Targets, target)
	}
	for skip := range walker.skips {
		// Explicit files are eligible regardless of extension, even when also seen
		// through a directory argument.
		if skip.Reason != SkipExtension || walker.selected[skip.Path].Path == "" {
			result.Skipped = append(result.Skipped, skip)
		}
	}
	for _, failure := range walker.failures {
		result.Errors = append(result.Errors, failure)
	}
	slices.SortFunc(result.Targets, func(a, b Target) int { return strings.Compare(a.Path, b.Path) })
	slices.SortFunc(result.Skipped, func(a, b Skipped) int {
		if n := strings.Compare(a.Path, b.Path); n != 0 {
			return n
		}
		return strings.Compare(string(a.Reason), string(b.Reason))
	})
	slices.SortFunc(result.Errors, func(a, b *fs.PathError) int {
		if n := strings.Compare(a.Path, b.Path); n != 0 {
			return n
		}
		if n := strings.Compare(a.Op, b.Op); n != 0 {
			return n
		}
		return strings.Compare(a.Err.Error(), b.Err.Error())
	})
	return result, nil
}

// absoluteFrom preserves raw components while applying host-native rooted and
// drive-relative Windows semantics. filepath.Abs alone would clean link/../.
func absoluteFrom(cwd, path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	if filepath.Separator == '\\' {
		volume := filepath.VolumeName(path)
		if volume != "" {
			anchor := cwd
			if !strings.EqualFold(volume, filepath.VolumeName(cwd)) {
				var err error
				anchor, err = filepath.Abs(volume + ".")
				if err != nil {
					return "", err
				}
			}
			return anchor + string(filepath.Separator) + path[len(volume):], nil
		}
		if len(path) > 0 && os.IsPathSeparator(path[0]) {
			return filepath.VolumeName(cwd) + path, nil
		}
	}
	return cwd + string(filepath.Separator) + path, nil
}

type failureKey struct{ path, op, message string }
type selector struct {
	options  Options
	access   selectionFS
	selected map[string]Target
	skips    map[Skipped]bool
	failures map[failureKey]*fs.PathError
	visited  map[string]bool
	expanded map[string]bool
}

func (s *selector) fail(path, op string, err error) {
	// Avoid nested PathErrors while retaining the original underlying cause.
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	s.failures[failureKey{path, op, err.Error()}] = &fs.PathError{Op: op, Path: path, Err: err}
}

// Bounded effects also supply semantic matching under the identical operation.
func (s *selector) excludes(path string, isDir bool) (bool, error) {
	if matcher, ok := s.access.(interface {
		excludes(string, string, bool, *config.Patterns) (bool, error)
	}); ok {
		return matcher.excludes(s.options.ExclusionBase, path, isDir, s.options.Exclusions)
	}
	return Excludes(s.options.ExclusionBase, path, isDir, s.options.Exclusions)
}
func (s *selector) skip(path string, reason SkipReason) { s.skips[Skipped{path, reason}] = true }

// gitMetadata skips the literal marker without probing it. For alternate case
// spellings, compare native identities after Lstat: EqualFold alone would hide
// distinct .GIT documents/directories on case-sensitive filesystems. Identity
// comparison also works on hosts whose mounted volumes have different case rules.
// A nil info performs only the literal check before the ordinary Lstat.
func (s *selector) gitMetadata(path string, info fs.FileInfo) bool {
	name := filepath.Base(path)
	if name == ".git" {
		s.skip(path, SkipGit)
		return true
	}
	if info == nil || !strings.EqualFold(name, ".git") {
		return false
	}
	marker := filepath.Join(filepath.Dir(path), ".git")
	canonical, err := s.access.lstat(marker)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		s.fail(marker, "lstat", err)
		return true
	}
	if os.SameFile(info, canonical) {
		s.skip(path, SkipGit)
		return true
	}
	return false
}

// checkComponents inspects raw components, including components later removed by
// '..'. It stops before probing a pruned subtree. Terminal eligibility is handled
// by walk, so an explicit regular file keeps its any-extension eligibility.
func (s *selector) checkComponents(raw string) (string, bool) {
	volume := filepath.VolumeName(raw)
	current := volume + string(filepath.Separator)
	rest := strings.TrimLeftFunc(raw[len(volume):], func(r rune) bool { return r == rune(filepath.Separator) || (filepath.Separator == '\\' && r == '/') })
	parts := strings.FieldsFunc(rest, func(r rune) bool { return r == rune(filepath.Separator) || (filepath.Separator == '\\' && r == '/') })
	if len(parts) == 0 {
		parts = []string{"."}
	}
	for i, part := range parts {
		if part == "." {
			continue
		}
		if part == ".." {
			current = filepath.Dir(current)
			continue
		}
		current = filepath.Join(current, part)
		if s.gitMetadata(current, nil) {
			return "", false
		}
		info, err := s.access.lstat(current)
		if err != nil {
			s.fail(current, "lstat", err)
			return "", false
		}
		if s.gitMetadata(current, info) {
			return "", false
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			s.skip(current, SkipSymlink)
			return "", false
		}
		if i < len(parts)-1 {
			if !info.IsDir() {
				s.fail(current, "traverse", fmt.Errorf("not a directory"))
				return "", false
			}
			blocked, err := s.excludes(current, true)
			if err != nil {
				s.fail(current, "exclude", err)
				return "", false
			}
			if blocked {
				s.skip(current, SkipExcluded)
				return "", false
			}
		}
	}
	return filepath.Clean(raw), true
}

func (s *selector) walk(root string, explicit bool) {
	type entry struct {
		path     string
		explicit bool
	}
	pending := []entry{{root, explicit}}
	for len(pending) > 0 {
		item := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		p := item.path
		// A later explicit file may upgrade a previously extension-filtered entry.
		if s.expanded[p] || (s.visited[p] && !item.explicit) {
			continue
		}
		s.visited[p] = true
		if s.gitMetadata(p, nil) {
			continue
		}
		info, err := s.access.lstat(p)
		if err != nil {
			s.fail(p, "lstat", err)
			continue
		}
		if s.gitMetadata(p, info) {
			continue
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			s.skip(p, SkipSymlink)
			continue
		}
		excluded, err := s.excludes(p, info.IsDir())
		if err != nil {
			s.fail(p, "exclude", err)
			continue
		}
		if excluded {
			s.skip(p, SkipExcluded)
			continue
		}
		if info.IsDir() {
			s.expanded[p] = true
			entries, err := s.access.readDir(p)
			if err != nil {
				s.fail(p, "readdir", err)
				continue
			}
			// Do not depend on filesystem enumeration order, including injected effects.
			slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
			for i := len(entries) - 1; i >= 0; i-- {
				pending = append(pending, entry{filepath.Join(p, entries[i].Name()), false})
			}
			continue
		}
		if !info.Mode().IsRegular() {
			s.skip(p, SkipNonregular)
			continue
		}
		if !item.explicit && !eligible(p) {
			s.skip(p, SkipExtension)
			continue
		}
		identities, err := s.identities(p, info)
		if err != nil {
			s.fail(p, "identity", err)
			continue
		}
		s.selected[p] = Target{Path: p, identities: identities}
	}
}
func eligible(p string) bool {
	ext := filepath.Ext(p)
	// ASCII-only folding avoids interpreting Unicode lookalikes as extensions.
	folded := []byte(ext)
	for i, c := range folded {
		if c >= 'A' && c <= 'Z' {
			folded[i] = c + ('a' - 'A')
		}
	}
	ext = string(folded)
	return ext == ".md" || ext == ".markdown"
}

// Excludes converts absolute host-native paths into the shared matcher's logical
// namespace. Native separators alone become '/'; POSIX literal backslashes and
// all pattern escapes remain untouched. Outside-base entries are unmatched.
// Both paths must be absolute; this function performs no filesystem access.
func Excludes(base, target string, isDir bool, patterns *config.Patterns) (bool, error) {
	if patterns == nil {
		patterns = &config.Patterns{}
	}
	return excludesMatching(base, target, isDir, patterns.Excludes)
}
func excludesMatching(base, target string, isDir bool, match func(string, bool, bool) (bool, error)) (bool, error) {
	if !filepath.IsAbs(base) || !filepath.IsAbs(target) {
		return false, fmt.Errorf("exclusion paths must be absolute")
	}
	if !strings.EqualFold(filepath.VolumeName(base), filepath.VolumeName(target)) {
		return false, nil
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false, err
	}
	inBase := rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
	name := filepath.ToSlash(rel)
	if rel == "." {
		name = ""
	}
	matched, err := match(name, isDir, inBase)
	if err != nil {
		return false, fmt.Errorf("exclusions for %q: %w", target, err)
	}
	return matched, nil
}

// identities retains selection-time components so acquisition cannot silently
// accept a different parent or final file after selection.
func (s *selector) identities(path string, final fs.FileInfo) ([]pathIdentity, error) {
	var reversed []pathIdentity
	for current := path; ; current = filepath.Dir(current) {
		info := final
		if current != path {
			var err error
			info, err = s.access.lstat(current)
			if err != nil {
				return nil, err
			}
		}
		if info.Mode()&fs.ModeSymlink != 0 || (current != path && !info.IsDir()) {
			return nil, fmt.Errorf("path changed during selection")
		}
		reversed = append(reversed, pathIdentity{current, info})
		if filepath.Dir(current) == current {
			break
		}
	}
	slices.Reverse(reversed)
	return reversed, nil
}
