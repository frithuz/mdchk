//go:build linux || darwin

package files

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"syscall"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestNativeSetIDAndStickyBitsRefusedAtAcquisition(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode fs.FileMode
	}{
		{"setuid", 0644 | fs.ModeSetuid},
		{"setgid", 0644 | fs.ModeSetgid},
		{"sticky", 0644 | fs.ModeSticky},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, path := nativeFixture(t)
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Skipf("host refuses to set %s: %v", tc.name, err)
			}
			if info, err := os.Stat(path); err != nil || info.Mode()&tc.mode&^0777 == 0 {
				t.Skipf("host did not retain %s: %v", tc.name, err)
			}
			selection, _ := Select(Options{Paths: []string{path}})
			b, _ := parser.NewOperationBudget(parser.DefaultLimits())
			set, a, _, err := AcquireWrite(context.Background(), selection, WriteOptions{AcquireOptions: AcquireOptions{Budget: b}})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = set.Close() }()
			if len(a.Errors) != 1 || !errors.Is(a.Errors[0], errUnsupportedTarget) || len(a.Documents) != 0 {
				t.Fatalf("unsupported bit acquired %+v", a)
			}
			assertNativeBytes(t, path, []byte("old\r\nbody\r"))
			assertNoStage(t, root)
		})
	}
}

func TestNativeFIFOReplacementIsStaleNotBlocking(t *testing.T) {
	root, path := nativeFixture(t)
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Skipf("host denies mkfifo: %v", err)
	}
	out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("candidate\n")), nil)
	if out.Err == nil || out.Published {
		t.Fatalf("accepted a FIFO %+v", out)
	}
	assertNoStage(t, root)
}

// A parent the process cannot write to cannot hold a stage: the failure is an
// I/O refusal that leaves the original untouched.
func TestNativeUnwritableParentRefusesWithoutMutation(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	root, path := nativeFixture(t)
	// The parent is unwritable before acquisition; changing it afterwards would be
	// caught earlier, as stale parent metadata.
	if err := os.Chmod(root, 0500); err != nil { //nolint:gosec // Exercise refusal of an unwritable synthetic parent.
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0700) }) //nolint:gosec // Restore the synthetic parent so cleanup can remove it.
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	before, _ := stableStat(path)
	out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("candidate\n")), nil)
	if out.Err == nil || out.Published || !errors.Is(out.Err, fs.ErrPermission) {
		t.Fatalf("unwritable parent accepted %+v", out)
	}
	after, _ := stableStat(path)
	if !os.SameFile(before, after) {
		t.Fatal("original replaced")
	}
	assertNativeBytes(t, path, a.Documents[0].Source.Bytes())
	if err := os.Chmod(root, 0700); err != nil { //nolint:gosec // Restore the synthetic parent before inspecting it.
		t.Fatal(err)
	}
	assertNoStage(t, root)
}

func TestNativeParentOwnerAndModeAreStaleProtected(t *testing.T) {
	root, path := nativeFixture(t)
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	if err := os.Chmod(root, 0750); err != nil { //nolint:gosec // Mutate the synthetic parent's mode to prove it is revalidated.
		t.Fatal(err)
	}
	out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("candidate\n")), nil)
	if out.Err == nil || out.Published {
		t.Fatalf("parent mode change accepted %+v", out)
	}
	assertNativeBytes(t, path, a.Documents[0].Source.Bytes())
	assertNoStage(t, root)
}
