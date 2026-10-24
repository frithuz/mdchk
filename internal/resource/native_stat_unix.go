//go:build linux || darwin

package resource

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// nativeStatAt observes one entry without following it or opening contents.
// macOS O_EVTONLY may still require data permission; fstatat does not.
func nativeStatAt(parent *os.File, name string) (filesystemMetadata, error) {
	if parent == nil || !nativeComponent(name, false) {
		return filesystemMetadata{}, ErrContract
	}
	connection, err := parent.SyscallConn()
	if err != nil {
		return filesystemMetadata{}, err
	}
	var stat unix.Stat_t
	var statErr error
	controlErr := connection.Control(func(fd uintptr) { statErr = unix.Fstatat(int(fd), name, &stat, unix.AT_SYMLINK_NOFOLLOW) })
	if err := errors.Join(controlErr, statErr); err != nil {
		return filesystemMetadata{}, err
	}
	if stat.Ino == 0 {
		return filesystemMetadata{}, errNativePolicy
	}
	mode := fs.FileMode(stat.Mode & 0777)
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		mode |= fs.ModeDir
	case unix.S_IFLNK:
		mode |= fs.ModeSymlink
	case unix.S_IFIFO:
		mode |= fs.ModeNamedPipe
	case unix.S_IFSOCK:
		mode |= fs.ModeSocket
	case unix.S_IFCHR:
		mode |= fs.ModeDevice | fs.ModeCharDevice
	case unix.S_IFBLK:
		mode |= fs.ModeDevice
	case unix.S_IFREG:
	default:
		mode |= fs.ModeIrregular
	}
	return filesystemMetadata{info: filesystemStatInfo{name: name, size: stat.Size, mode: mode, modified: nativeStatModified(stat)}, identity: fmt.Sprintf("%x:%x", stat.Dev, stat.Ino)}, nil
}

type filesystemStatInfo struct {
	name     string
	size     int64
	mode     fs.FileMode
	modified time.Time
}

func (i filesystemStatInfo) Name() string       { return i.name }
func (i filesystemStatInfo) Size() int64        { return i.size }
func (i filesystemStatInfo) Mode() fs.FileMode  { return i.mode }
func (i filesystemStatInfo) ModTime() time.Time { return i.modified }
func (i filesystemStatInfo) IsDir() bool        { return i.mode.IsDir() }
func (i filesystemStatInfo) Sys() any           { return nil }
