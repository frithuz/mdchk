package configscan

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// access pins every nonsymlink component from a filesystem volume root. It has
// no write-capable methods. Open handles and pre/open identity comparisons keep
// renamed directories from redirecting reads outside their checked identities.
type access struct {
	root   *os.Root
	volume string
}

func openAccess(path string) (*access, error) {
	volume := filepath.VolumeName(path) + string(filepath.Separator)
	root, err := os.OpenRoot(volume)
	if err != nil {
		return nil, err
	}
	return &access{root, volume}, nil
}
func closeHandle(c io.Closer) { _ = c.Close() }
func (a *access) parent(path string) (*os.Root, string, error) {
	rel, err := filepath.Rel(a.volume, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, "", errors.New("path outside filesystem volume")
	}
	parts := strings.Split(rel, string(filepath.Separator))
	current, err := a.root.OpenRoot(".")
	if err != nil {
		return nil, "", err
	}
	for _, part := range parts[:len(parts)-1] {
		info, e := current.Lstat(part)
		if e != nil {
			closeHandle(current)
			return nil, "", e
		}
		if !info.IsDir() {
			closeHandle(current)
			return nil, "", errors.New("path component must be a nonsymlink directory")
		}
		next, e := current.OpenRoot(part)
		if e != nil {
			closeHandle(current)
			return nil, "", e
		}
		actual, e := next.Stat(".")
		closeHandle(current)
		if e != nil {
			closeHandle(next)
			return nil, "", e
		}
		if !os.SameFile(info, actual) {
			closeHandle(next)
			return nil, "", errors.New("directory changed during access")
		}
		current = next
	}
	return current, parts[len(parts)-1], nil
}
func (a *access) lstat(path string) (fs.FileInfo, error) {
	p, n, e := a.parent(path)
	if e != nil {
		return nil, e
	}
	defer closeHandle(p)
	return p.Lstat(n)
}
func (a *access) open(path string, directory bool) (readHandle, error) {
	p, n, e := a.parent(path)
	if e != nil {
		return nil, e
	}
	defer closeHandle(p)
	info, e := p.Lstat(n)
	if e != nil {
		return nil, e
	}
	if directory && !info.IsDir() || !directory && !info.Mode().IsRegular() {
		return nil, errors.New("source must have the expected nonsymlink type")
	}
	f, e := p.OpenFile(n, os.O_RDONLY|configOpenFlags, 0)
	if e != nil {
		return nil, e
	}
	actual, e := f.Stat()
	if e != nil {
		closeHandle(f)
		return nil, e
	}
	if !os.SameFile(info, actual) || actual.Mode().Type() != info.Mode().Type() {
		closeHandle(f)
		return nil, errors.New("source changed during access")
	}
	return f, nil
}

// Small read-only interfaces permit deterministic traversal failures in tests.
type readHandle interface {
	io.Reader
	io.Closer
	Stat() (fs.FileInfo, error)
	ReadDir(int) ([]fs.DirEntry, error)
}
type scanAccess interface {
	lstat(string) (fs.FileInfo, error)
	open(string, bool) (readHandle, error)
}
