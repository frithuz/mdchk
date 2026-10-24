//go:build linux || darwin

package resource

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

func nativeValidateAnchor(path string) error {
	if strings.IndexByte(path, 0) >= 0 {
		return ErrContract
	}
	return nil
}

func nativeOpenAnchor(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || nativeValidateAnchor(path) != nil {
		return nil, ErrContract
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	current := os.NewFile(uintptr(fd), "/")
	for part := range strings.SplitSeq(strings.TrimPrefix(path, "/"), "/") {
		if part == "" {
			continue
		}
		next, openErr := nativeOpenChild(current, part, true)
		closeErr := current.Close()
		if openErr != nil || closeErr != nil {
			if next != nil {
				closeErr = errors.Join(closeErr, next.Close())
			}
			return nil, errors.Join(openErr, closeErr)
		}
		current = next
	}
	return current, nil
}

func nativeComponent(name string, directory bool) bool {
	return name != "" && name != ".." && (name != "." || directory) && !strings.ContainsAny(name, "/\x00")
}

func nativeOpenChild(parent *os.File, name string, directory bool) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if directory {
		flags |= unix.O_DIRECTORY
	}
	return nativeOpenFlags(parent, name, flags, directory)
}

func nativeOpenMetadata(parent *os.File, name string, directory bool) (*os.File, error) {
	if directory {
		return nativeOpenChild(parent, name, true)
	}
	return nativeOpenFlags(parent, name, nativeMetadataFlags, false)
}

func nativeOpenGitMetadata(parent *os.File) (*os.File, error) {
	return nativeOpenMetadata(parent, ".git", false)
}

func nativeOpenGitAliasMetadata(parent *os.File, name string) (*os.File, error) {
	if !strings.EqualFold(name, ".git") {
		return nil, ErrContract
	}
	return nativeOpenMetadata(parent, name, false)
}

func nativeOpenFlags(parent *os.File, name string, flags int, directory bool) (*os.File, error) {
	if parent == nil || !nativeComponent(name, directory) {
		return nil, ErrContract
	}
	connection, err := parent.SyscallConn()
	if err != nil {
		return nil, err
	}
	fd := -1
	var openErr error
	controlErr := connection.Control(func(parentFD uintptr) {
		// The runtime holds the parent descriptor alive during this native call.
		fd, openErr = unix.Openat(int(parentFD), name, flags, 0)
	})
	if err = errors.Join(controlErr, openErr); err != nil {
		if fd >= 0 {
			err = errors.Join(err, unix.Close(fd))
		}
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		if err == nil {
			err = errNativePolicy
		}
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func nativeIdentity(file *os.File) (string, error) {
	connection, err := file.SyscallConn()
	if err != nil {
		return "", err
	}
	var stat unix.Stat_t
	var statErr error
	controlErr := connection.Control(func(fd uintptr) { statErr = unix.Fstat(int(fd), &stat) })
	if err = errors.Join(controlErr, statErr); err != nil {
		return "", err
	}
	if stat.Ino == 0 {
		return "", errNativePolicy
	}
	return fmt.Sprintf("%x:%x", stat.Dev, stat.Ino), nil
}

func nativeRepresentable(name string) bool {
	return nativeComponent(name, false) && utf8.ValidString(name)
}

func nativePolicyError(err error) bool {
	return errors.Is(err, errNativePolicy) || errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.ENAMETOOLONG)
}
