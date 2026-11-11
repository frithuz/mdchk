package fsroot

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrOutside reports a path that is not inside the reader's anchor.
var ErrOutside = errors.New("path is outside the reader anchor")

// ErrNotDir reports an intermediate component that is not a nonsymlink
// directory.
var ErrNotDir = errors.New("path component must be a nonsymlink directory")

// ErrDirChanged reports an intermediate directory whose identity changed
// between its Lstat and its open.
var ErrDirChanged = errors.New("directory changed during access")

// ErrWrongKind reports a leaf that is not of the expected nonsymlink kind.
var ErrWrongKind = errors.New("leaf does not have the expected nonsymlink type")

// ErrLeafChanged reports a leaf whose identity or type changed between its
// Lstat and its open.
var ErrLeafChanged = errors.New("leaf changed during access")

// Kind is the leaf type that Reader.Open requires.
type Kind int

const (
	// Regular requires a regular file.
	Regular Kind = iota
	// Directory requires a directory.
	Directory
)

// Reader confines read-only access to an anchor directory. It has no
// write-capable methods and never exposes its directory handles. Every
// intermediate component is pinned through its held parent and compared with
// its Lstat identity, so a concurrent rename cannot redirect a read.
type Reader struct {
	root   *os.Root
	anchor string
}

// VolumeRoot returns the root directory of the volume that holds path.
func VolumeRoot(path string) string {
	return filepath.VolumeName(path) + string(filepath.Separator)
}

// OpenReader opens a reader anchored at the absolute directory anchor.
func OpenReader(anchor string) (*Reader, error) {
	root, err := os.OpenRoot(anchor)
	if err != nil {
		return nil, err
	}
	return &Reader{root: root, anchor: anchor}, nil
}

// Close releases the anchor handle.
func (r *Reader) Close() error { return r.root.Close() }

func closeQuietly(root *os.Root) { _ = root.Close() }

// walkParent returns the held parent directory of path and the leaf name.
func walkParent(r *Reader, path string) (*os.Root, string, error) {
	rel, err := filepath.Rel(r.anchor, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, "", ErrOutside
	}
	parts := strings.Split(rel, string(filepath.Separator))
	current, err := r.root.OpenRoot(".")
	if err != nil {
		return nil, "", err
	}
	for _, part := range parts[:len(parts)-1] {
		info, e := current.Lstat(part)
		if e != nil {
			closeQuietly(current)
			return nil, "", e
		}
		if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			closeQuietly(current)
			return nil, "", ErrNotDir
		}
		next, e := current.OpenRoot(part)
		if e != nil {
			closeQuietly(current)
			return nil, "", e
		}
		actual, e := next.Stat(".")
		closeQuietly(current)
		if e != nil {
			closeQuietly(next)
			return nil, "", e
		}
		if !os.SameFile(info, actual) {
			closeQuietly(next)
			return nil, "", ErrDirChanged
		}
		current = next
	}
	return current, parts[len(parts)-1], nil
}

// Lstat observes the leaf of path without following it.
func (r *Reader) Lstat(path string) (fs.FileInfo, error) {
	parent, name, err := walkParent(r, path)
	if err != nil {
		return nil, err
	}
	defer closeQuietly(parent)
	return parent.Lstat(name)
}

// Open opens the leaf of path read-only. The leaf must be a nonsymlink entry
// of the expected kind, and the opened handle must be the same entry.
func (r *Reader) Open(path string, kind Kind) (*os.File, error) {
	parent, name, err := walkParent(r, path)
	if err != nil {
		return nil, err
	}
	defer closeQuietly(parent)
	info, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if kind == Directory && !info.IsDir() || kind == Regular && !info.Mode().IsRegular() {
		return nil, ErrWrongKind
	}
	file, err := parent.OpenFile(name, os.O_RDONLY|ReadFlags, 0)
	if err != nil {
		return nil, err
	}
	actual, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !os.SameFile(info, actual) || actual.Mode().Type() != info.Mode().Type() {
		_ = file.Close()
		return nil, ErrLeafChanged
	}
	return file, nil
}
