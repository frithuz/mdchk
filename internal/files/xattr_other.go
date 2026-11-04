//go:build !linux && !darwin

package files

import "os"

// Windows alternate data streams, including Mark-of-the-Web, are not copied: the
// replacement is a new file. This is an accepted, documented limit.
type xattrSet map[string][]byte

func equalXattrs(a, b xattrSet) bool { return len(a) == len(b) }

func readXattrs(*os.File) (xattrSet, error) { return nil, nil }

func copyXattrs(*os.File, xattrSet) error { return nil }
