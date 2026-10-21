//go:build unix

package configscan

import "syscall"

// Nonblocking open prevents a raced FIFO from hanging configuration startup;
// no-follow rejects a replaced final symlink before touching its target.
const configOpenFlags = syscall.O_NONBLOCK | syscall.O_NOFOLLOW
