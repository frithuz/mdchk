package config

import (
	"errors"
	"io"
	"io/fs"
	"os"

	"github.com/frithuz/mdchk/internal/fsroot"
)

// rootAccess holds no write-capable adapter. The fsroot reader pins checked
// parents, and os.Root confines access even during concurrent renames.
type rootAccess struct {
	reader *fsroot.Reader
}

func openRootAccess(base string) (*rootAccess, error) {
	reader, err := fsroot.OpenReader(base)
	if err != nil {
		return nil, err
	}
	return &rootAccess{reader: reader}, nil
}
func (a *rootAccess) Close() error { return a.reader.Close() }

// accessError restores the configuration wording for fsroot sentinels.
func accessError(err error) error {
	switch {
	case errors.Is(err, fsroot.ErrOutside):
		return errors.New("path is outside discovery cap")
	case errors.Is(err, fsroot.ErrNotDir):
		return errors.New("configuration path component must be a nonsymlink directory")
	case errors.Is(err, fsroot.ErrDirChanged):
		return errors.New("configuration directory changed during access")
	case errors.Is(err, fsroot.ErrWrongKind):
		return errors.New("configuration must be a nonsymlink regular file")
	case errors.Is(err, fsroot.ErrLeafChanged):
		return errors.New("configuration changed during access")
	}
	return err
}
func (a *rootAccess) lstat(path string) (fs.FileInfo, error) {
	info, err := a.reader.Lstat(path)
	if err != nil {
		return nil, accessError(err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("configuration access cannot follow symlinks")
	}
	return info, nil
}
func (a *rootAccess) openRegular(path string) (*os.File, error) {
	file, err := a.reader.Open(path, fsroot.Regular)
	if err != nil {
		return nil, accessError(err)
	}
	return file, nil
}
func (a *rootAccess) readRegular(path string) ([]byte, error) {
	file, err := a.openRegular(path)
	if err != nil {
		return nil, err
	}
	defer closeReadHandle(file)
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
