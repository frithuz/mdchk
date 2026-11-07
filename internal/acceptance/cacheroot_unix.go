//go:build unix

package acceptance

import (
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// checkCacheRootPrivate refuses a root that another user owns or can reach.
func checkCacheRootPrivate(info fs.FileInfo) error {
	return checkCacheRootOwner(info, os.Getuid())
}

func checkCacheRootOwner(info fs.FileInfo, uid int) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s has no owner information", info.Name())
	}
	if int64(stat.Uid) != int64(uid) {
		return fmt.Errorf("%s is owned by uid %d, not %d", info.Name(), stat.Uid, uid)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("%s has mode %o, which other users can reach", info.Name(), perm)
	}
	return nil
}
