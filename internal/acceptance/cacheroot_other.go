//go:build !unix

package acceptance

import "io/fs"

// checkCacheRootPrivate has nothing to check here. The user cache directory is
// already private on Windows, and checking ACLs needs native calls.
func checkCacheRootPrivate(fs.FileInfo) error { return nil }
