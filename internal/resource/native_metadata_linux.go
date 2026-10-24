//go:build linux

package resource

import (
	"time"

	"golang.org/x/sys/unix"
)

// O_PATH observes type/identity without opening regular-file contents or devices.
const nativeMetadataFlags = unix.O_PATH | unix.O_CLOEXEC | unix.O_NOFOLLOW

func nativeStatModified(stat unix.Stat_t) time.Time { return time.Unix(stat.Mtim.Sec, stat.Mtim.Nsec) }
