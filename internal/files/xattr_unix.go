//go:build linux || darwin

package files

import (
	"bytes"
	"errors"
	"maps"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// Extended attributes carry quarantine, provenance, SELinux labels and Linux
// POSIX ACLs. They are copied exactly; the bounds keep a hostile file from
// forcing unbounded reads.
const (
	maxXattrCount = 256
	maxXattrBytes = 1 << 20
)

type xattrSet map[string][]byte

func equalXattrs(a, b xattrSet) bool { return maps.EqualFunc(a, b, bytes.Equal) }

func withFD[T any](f *os.File, call func(fd int) (T, error)) (T, error) {
	var zero T
	conn, err := f.SyscallConn()
	if err != nil {
		return zero, err
	}
	var out T
	var callErr error
	if err = conn.Control(func(fd uintptr) { out, callErr = call(int(fd)) }); err != nil {
		return zero, err
	}
	return out, callErr
}

func xattrUnsupported(err error) bool {
	return errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP)
}

// readXattrs returns every attribute visible to the process. A filesystem
// without attribute support has none.
func readXattrs(f *os.File) (xattrSet, error) {
	names, err := listXattrs(f)
	if err != nil || len(names) == 0 {
		return nil, err
	}
	if len(names) > maxXattrCount {
		return nil, errXattrBound
	}
	out := make(xattrSet, len(names))
	total := 0
	for _, name := range names {
		value, err := getXattr(f, name)
		if err != nil {
			return nil, err
		}
		if total += len(name) + len(value); total > maxXattrBytes {
			return nil, errXattrBound
		}
		out[name] = value
	}
	return out, nil
}

func listXattrs(f *os.File) ([]string, error) {
	for range 4 {
		size, err := withFD(f, func(fd int) (int, error) { return unix.Flistxattr(fd, nil) })
		if err != nil {
			if xattrUnsupported(err) {
				return nil, nil
			}
			return nil, err
		}
		if size == 0 {
			return nil, nil
		}
		if size > maxXattrBytes {
			return nil, errXattrBound
		}
		buf := make([]byte, size)
		n, err := withFD(f, func(fd int) (int, error) { return unix.Flistxattr(fd, buf) })
		if errors.Is(err, unix.ERANGE) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var names []string
		for name := range strings.SplitSeq(string(buf[:n]), "\x00") {
			if name != "" {
				names = append(names, name)
			}
		}
		return names, nil
	}
	return nil, errors.New("extended attribute list kept changing")
}

func getXattr(f *os.File, name string) ([]byte, error) {
	for range 4 {
		size, err := withFD(f, func(fd int) (int, error) { return unix.Fgetxattr(fd, name, nil) })
		if err != nil {
			return nil, err
		}
		if size > maxXattrBytes {
			return nil, errXattrBound
		}
		buf := make([]byte, size)
		n, err := withFD(f, func(fd int) (int, error) { return unix.Fgetxattr(fd, name, buf) })
		if errors.Is(err, unix.ERANGE) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return buf[:n], nil
	}
	return nil, errors.New("extended attribute value kept changing")
}

// copyXattrs makes the stage's attribute set equal to want. Attributes the
// filesystem attached to the new file, such as an inherited default ACL or a
// creation-time label, are removed unless the target carries them too.
func copyXattrs(stage *os.File, want xattrSet) error {
	have, err := readXattrs(stage)
	if err != nil {
		return err
	}
	for name := range have {
		if _, keep := want[name]; keep {
			continue
		}
		if _, err = withFD(stage, func(fd int) (struct{}, error) { return struct{}{}, unix.Fremovexattr(fd, name) }); err != nil && !errors.Is(err, unix.ENODATA) {
			return err
		}
	}
	for name, value := range want {
		if current, ok := have[name]; ok && bytes.Equal(current, value) {
			continue
		}
		if _, err = withFD(stage, func(fd int) (struct{}, error) { return struct{}{}, unix.Fsetxattr(fd, name, value, 0) }); err != nil {
			return err
		}
	}
	return nil
}
