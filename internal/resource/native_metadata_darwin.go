//go:build darwin

package resource

import (
	"time"

	"golang.org/x/sys/unix"
)

// O_EVTONLY requests metadata without reading contents. macOS may still deny
// unreadable entries; nativeStatAt provides the no-follow metadata fallback.
const nativeMetadataFlags = unix.O_EVTONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK

func nativeStatModified(stat unix.Stat_t) time.Time {
	return time.Unix(stat.Mtim.Sec, stat.Mtim.Nsec)
}
