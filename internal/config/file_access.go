package config

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// rootAccess holds no write-capable adapter. Directory handles pin checked
// parents, and os.Root confines access even during concurrent renames.
type rootAccess struct {
	root *os.Root
	base string
}

func (a *rootAccess) parent(path string) (*os.Root, string, error) {
	if !inside(a.base, path) {
		return nil, "", errors.New("path is outside discovery cap")
	}
	rel, err := filepath.Rel(a.base, path)
	if err != nil {
		return nil, "", err
	}
	parts := strings.Split(rel, string(filepath.Separator))
	current, err := a.root.OpenRoot(".")
	if err != nil {
		return nil, "", err
	}
	for _, part := range parts[:len(parts)-1] {
		info, e := current.Lstat(part)
		if e != nil {
			closeReadHandle(current)
			return nil, "", e
		}
		if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			closeReadHandle(current)
			return nil, "", errors.New("configuration path component must be a nonsymlink directory")
		}
		next, e := current.OpenRoot(part)
		if e != nil {
			closeReadHandle(current)
			return nil, "", e
		}
		actual, e := next.Stat(".")
		closeReadHandle(current)
		if e != nil {
			closeReadHandle(next)
			return nil, "", e
		}
		if !os.SameFile(info, actual) {
			closeReadHandle(next)
			return nil, "", errors.New("configuration directory changed during access")
		}
		current = next
	}
	return current, parts[len(parts)-1], nil
}
func (a *rootAccess) lstat(path string) (fs.FileInfo, error) {
	parent, name, err := a.parent(path)
	if err != nil {
		return nil, err
	}
	defer closeReadHandle(parent)
	info, err := parent.Lstat(name)
	if err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("configuration access cannot follow symlinks")
	}
	return info, err
}
func (a *rootAccess) readRegular(path string) ([]byte, error) {
	parent, name, err := a.parent(path)
	if err != nil {
		return nil, err
	}
	defer closeReadHandle(parent)
	info, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("configuration must be a nonsymlink regular file")
	}
	file, err := parent.OpenFile(name, os.O_RDONLY|configOpenFlags, 0)
	if err != nil {
		return nil, err
	}
	defer closeReadHandle(file)
	actual, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !actual.Mode().IsRegular() || !os.SameFile(info, actual) {
		return nil, errors.New("configuration changed during access")
	}
	data, err := io.ReadAll(io.LimitReader(file, configBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > configBytes {
		return nil, errors.New("config-bytes limit exceeded")
	}
	return data, nil
}

// Read-only handles have no buffered writes to commit. A close failure cannot
// turn an already validated snapshot into different configuration contents.
func closeReadHandle(handle io.Closer) { _ = handle.Close() }
