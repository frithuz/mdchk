//go:build !unix

package files

import (
	"io/fs"
	"os"
)

// Windows ownership is governed by ACLs that os.FileInfo does not expose and that
// the stage inherits from the directory, as for any ordinary file replacement.
func sameOwner(_, _ fs.FileInfo) bool { return true }

func applyOwner(*os.File, fs.FileInfo) error { return nil }
