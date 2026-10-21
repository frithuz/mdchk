//go:build unix

package files

import "syscall"

// Nonblocking open prevents a raced FIFO from hanging source acquisition;
// no-follow rejects a replaced final symlink before touching its target.
const acquireOpenFlags = syscall.O_NONBLOCK | syscall.O_NOFOLLOW
