// Package fsroot provides pin-and-verify directory traversal on os.Root.
//
// os.Root guarantees that no lookup escapes the root it was opened on, but it
// follows symlinks that stay inside the root. mdchk's contract is stricter:
// every component of a path must be a plain directory or regular file. This
// package enforces that rule on top of os.Root by examining each component with
// Lstat, opening it through the held parent, and comparing the opened handle
// with the Lstat result before and after the open. A component swapped for a
// contained symlink between check and open is detected by the identity
// comparison and can never lead outside the root.
//
// Reader is the read-only side: a confined reader anchored at a directory the
// caller names, used by internal/config (anchored at the discovery cap or the
// volume root) and internal/configscan (anchored at the volume root).
//
// The package contains no platform-specific code beyond the open flags that
// keep a raced FIFO from blocking a read.
package fsroot

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotPlain reports a component that is a symlink, junction, reparse point,
// device or any other entry that is not a plain directory or regular file.
var ErrNotPlain = errors.New("path component is not a plain directory or regular file")

// ErrChanged reports a component whose identity differs between observations.
var ErrChanged = errors.New("path component changed while it was being opened")

// hook is set only by this package's tests. It runs between the check and the
// use of a component so tests can inject deterministic races.
var hook func(stage, path string)

func at(stage, path string) {
	if hook != nil {
		hook(stage, path)
	}
}

// Dir is a verified directory held open. Every operation is relative to the
// held handle, never to a path that could be re-resolved.
type Dir struct {
	root *os.Root
	path string
}

// Path returns the directory path the handle was pinned from.
func (d *Dir) Path() string { return d.path }

// Close releases the directory handle.
func (d *Dir) Close() error {
	if d == nil || d.root == nil {
		return nil
	}
	err := d.root.Close()
	d.root = nil
	return err
}

// Info returns the identity of the pinned directory itself.
func (d *Dir) Info() (fs.FileInfo, error) { return d.root.Stat(".") }

// Pin walks an absolute, clean directory path from its volume root, verifying
// that every component is a plain directory. It returns the held final
// directory and the identity of each directory from the volume root down.
func Pin(dir string) (*Dir, []fs.FileInfo, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || strings.IndexByte(dir, 0) >= 0 {
		return nil, nil, fmt.Errorf("fsroot: noncanonical path %q", dir)
	}
	volume := filepath.VolumeName(dir)
	start := volume + string(filepath.Separator)
	root, err := os.OpenRoot(start)
	if err != nil {
		return nil, nil, err
	}
	info, err := root.Stat(".")
	if err != nil {
		return nil, nil, errors.Join(err, root.Close())
	}
	chain := []fs.FileInfo{info}
	rest := strings.TrimPrefix(dir[len(volume):], string(filepath.Separator))
	current := start
	if rest != "" {
		for _, name := range strings.Split(rest, string(filepath.Separator)) {
			current = filepath.Join(current, name)
			next, nextInfo, err := openDir(root, name, current)
			if err != nil {
				return nil, nil, errors.Join(err, root.Close())
			}
			if err = root.Close(); err != nil {
				return nil, nil, errors.Join(err, next.Close())
			}
			root = next
			chain = append(chain, nextInfo)
		}
	}
	return &Dir{root: root, path: dir}, chain, nil
}

// openDir verifies and opens one child directory through its held parent.
func openDir(parent *os.Root, name, path string) (*os.Root, fs.FileInfo, error) {
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if before.Mode().Type() != fs.ModeDir {
		return nil, nil, fmt.Errorf("%w: %s", ErrNotPlain, path)
	}
	at("lstat-dir", path)
	next, err := parent.OpenRoot(name)
	if err != nil {
		return nil, nil, err
	}
	opened, err := next.Stat(".")
	if err != nil {
		return nil, nil, errors.Join(err, next.Close())
	}
	after, err := parent.Lstat(name)
	if err != nil {
		return nil, nil, errors.Join(err, next.Close())
	}
	if after.Mode().Type() != fs.ModeDir || !os.SameFile(before, opened) || !os.SameFile(before, after) {
		return nil, nil, errors.Join(fmt.Errorf("%w: %s", ErrChanged, path), next.Close())
	}
	return next, opened, nil
}

// SameChain reports whether two pinned chains name the same directories.
func SameChain(a, b []fs.FileInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !os.SameFile(a[i], b[i]) || a[i].Mode() != b[i].Mode() {
			return false
		}
	}
	return true
}

// Lstat observes one entry of the pinned directory without following it.
func (d *Dir) Lstat(name string) (fs.FileInfo, error) { return d.root.Lstat(name) }

// OpenRegular opens an existing regular file for reading through the pinned
// directory. The entry must be a regular file before the open, the opened
// handle must be the same file, and the entry must still be that file after
// the open. The returned info describes the opened handle.
func (d *Dir) OpenRegular(name string) (*os.File, fs.FileInfo, error) {
	before, err := d.root.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%w: %s", ErrNotPlain, filepath.Join(d.path, name))
	}
	at("lstat-file", filepath.Join(d.path, name))
	file, err := d.root.OpenFile(name, os.O_RDONLY|ReadFlags, 0)
	if err != nil {
		return nil, nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		return nil, nil, errors.Join(err, file.Close())
	}
	after, err := d.root.Lstat(name)
	if err != nil {
		return nil, nil, errors.Join(err, file.Close())
	}
	if !opened.Mode().IsRegular() || !after.Mode().IsRegular() || !os.SameFile(before, opened) || !os.SameFile(before, after) {
		return nil, nil, errors.Join(fmt.Errorf("%w: %s", ErrChanged, filepath.Join(d.path, name)), file.Close())
	}
	return file, opened, nil
}

// CreateExclusive creates a new regular file that must not exist yet.
func (d *Dir) CreateExclusive(name string, perm fs.FileMode) (*os.File, error) {
	return d.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
}

// Rename replaces newName with oldName inside the pinned directory.
func (d *Dir) Rename(oldName, newName string) error { return d.root.Rename(oldName, newName) }

// Remove deletes one entry of the pinned directory.
func (d *Dir) Remove(name string) error { return d.root.Remove(name) }

// Sync flushes directory metadata where the platform supports it.
func (d *Dir) Sync() error { return syncDir(d.root) }
