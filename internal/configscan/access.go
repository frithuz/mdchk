package configscan

import (
	"errors"
	"io"
	"io/fs"

	"github.com/frithuz/mdchk/internal/fsroot"
)

// access pins every nonsymlink component from a filesystem volume root through
// an fsroot reader. It has no write-capable methods. Open handles and pre/open
// identity comparisons keep renamed directories from redirecting reads outside
// their checked identities.
type access struct {
	reader *fsroot.Reader
}

func openAccess(path string) (*access, error) {
	reader, err := fsroot.OpenReader(fsroot.VolumeRoot(path))
	if err != nil {
		return nil, err
	}
	return &access{reader}, nil
}
func closeHandle(c io.Closer) { _ = c.Close() }

// accessError restores the scan wording for fsroot sentinels.
func accessError(err error) error {
	switch {
	case errors.Is(err, fsroot.ErrOutside):
		return errors.New("path outside filesystem volume")
	case errors.Is(err, fsroot.ErrNotDir):
		return errors.New("path component must be a nonsymlink directory")
	case errors.Is(err, fsroot.ErrDirChanged):
		return errors.New("directory changed during access")
	case errors.Is(err, fsroot.ErrWrongKind):
		return errors.New("source must have the expected nonsymlink type")
	case errors.Is(err, fsroot.ErrLeafChanged):
		return errors.New("source changed during access")
	}
	return err
}
func (a *access) lstat(path string) (fs.FileInfo, error) {
	info, e := a.reader.Lstat(path)
	if e != nil {
		return nil, accessError(e)
	}
	return info, nil
}
func (a *access) open(path string, directory bool) (readHandle, error) {
	kind := fsroot.Regular
	if directory {
		kind = fsroot.Directory
	}
	f, e := a.reader.Open(path, kind)
	if e != nil {
		return nil, accessError(e)
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
