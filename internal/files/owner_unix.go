//go:build unix

package files

import (
	"io/fs"
	"os"
	"syscall"
)

// Owner and link count come from the stdlib stat result; no cgo is involved.
func ownerOf(info fs.FileInfo) (uid, gid int, ok bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(st.Uid), int(st.Gid), true
}

func sameOwner(a, b fs.FileInfo) bool {
	au, ag, aok := ownerOf(a)
	bu, bg, bok := ownerOf(b)
	return aok && bok && au == bu && ag == bg
}

// linkCount reports the number of directory entries naming the file. Zero means
// the platform does not report it.
func linkCount(_ *os.File, info fs.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Nlink) //nolint:unconvert // Nlink is uint16 on Darwin and uint64 on Linux.
	}
	return 0
}

// applyOwner gives the stage the target's uid and gid. A mismatch that cannot be
// corrected is an error: the replacement must not change who owns the file.
func applyOwner(stage *os.File, target fs.FileInfo) error {
	uid, gid, ok := ownerOf(target)
	if !ok {
		return errOwnerUnavailable
	}
	if current, err := stage.Stat(); err == nil && sameOwner(current, target) {
		return nil
	}
	return stage.Chown(uid, gid)
}
