// Package filestat provides file identity helpers for tests only.
package filestat

import (
	"errors"
	"io/fs"
	"os"
)

// StableStat takes identity from an open handle. On Windows a path-based os.Stat
// result loads its file ID lazily, when SameFile first asks, so comparing it
// with a later stat of the same path would compare the current file with itself.
func StableStat(path string) (fs.FileInfo, error) {
	f, err := os.Open(path) //nolint:gosec // The path names an independently created synthetic test fixture.
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	return info, errors.Join(err, f.Close())
}
