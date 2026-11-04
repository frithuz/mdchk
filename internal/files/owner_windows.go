//go:build windows

package files

import (
	"io/fs"
	"os"
	"syscall"
)

// linkCount asks the open handle for the number of directory entries naming the
// file, as os.FileInfo does not carry it on Windows. Zero means it is unavailable.
func linkCount(f *os.File, _ fs.FileInfo) uint64 {
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &info); err != nil {
		return 0
	}
	return uint64(info.NumberOfLinks)
}
