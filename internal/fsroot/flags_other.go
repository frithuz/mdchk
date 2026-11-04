//go:build !unix

package fsroot

import "os"

// ReadFlags adds nothing here. os.Root rejects Windows device paths, and Lstat plus the identity checks in
// OpenRegular reject nonregular entries, so no extra open flags are needed.
const ReadFlags = 0

// Windows cannot fsync a directory handle; MoveFileEx completes the replace.
func syncDir(*os.Root) error { return nil }
