//go:build linux

package files

import (
	"context"
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/frithuz/mdchk/internal/parser"
)

// fsImmutableFlag is FS_IMMUTABLE_FL from <linux/fs.h>; x/sys/unix does not export it.
const fsImmutableFlag = 0x10

// Linux has no pre-check for immutable files: the failure surfaces at rename and
// refuses with no mutation. Setting the flag needs CAP_LINUX_IMMUTABLE.
func TestNativeImmutableTargetRefusesWithoutMutation(t *testing.T) {
	root, path := nativeFixture(t)
	f, err := os.Open(path) //nolint:gosec // Synthetic fixture path created by the test.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	flags, err := unix.IoctlGetInt(int(f.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil {
		t.Skipf("filesystem has no inode flags: %v", err)
	}
	if err = unix.IoctlSetPointerInt(int(f.Fd()), unix.FS_IOC_SETFLAGS, flags|fsImmutableFlag); err != nil {
		t.Skipf("process lacks CAP_LINUX_IMMUTABLE: %v", err)
	}
	t.Cleanup(func() { _ = unix.IoctlSetPointerInt(int(f.Fd()), unix.FS_IOC_SETFLAGS, flags) })
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("candidate\n")), nil)
	if out.Err == nil || out.Published || out.Uncertain || errors.Is(out.Err, context.Canceled) {
		t.Fatalf("immutable target %+v", out)
	}
	assertNativeBytes(t, path, a.Documents[0].Source.Bytes())
	assertNoStage(t, root)
}
