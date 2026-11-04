//go:build unix

package fsroot

import (
	"os"
	"syscall"
)

// ReadFlags are the extra open flags for reading a file that may be raced.
// A nonblocking open keeps a raced FIFO from hanging the read; no-follow rejects
// a replaced final symlink before touching its target.
const ReadFlags = syscall.O_NONBLOCK | syscall.O_NOFOLLOW

// syncDir fsyncs the directory so a completed rename is durable.
func syncDir(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	if closeErr := dir.Close(); syncErr == nil {
		syncErr = closeErr
	}
	return syncErr
}
