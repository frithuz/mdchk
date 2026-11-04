//go:build linux || darwin

package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/frithuz/mdchk/internal/parser"
)

// setXattrOrSkip skips only when the filesystem refuses user attributes.
func setXattrOrSkip(t *testing.T, path, name, value string) {
	t.Helper()
	if err := unix.Setxattr(path, name, []byte(value), 0); err != nil {
		if xattrUnsupported(err) || errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) {
			t.Skipf("filesystem refuses extended attribute %s: %v", name, err)
		}
		t.Fatal(err)
	}
}

func xattrName() string {
	// Linux confines unprivileged attributes to the user namespace; Darwin has no
	// namespaces, so the same name works on both.
	return "user.mdchk-test"
}

func attributesOf(t *testing.T, path string) xattrSet {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // Synthetic fixture path created by the test.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	attrs, err := readXattrs(f)
	if err != nil {
		t.Fatal(err)
	}
	return attrs
}

func TestXattrRoundTripIsPreservedExactly(t *testing.T) {
	root, path := nativeFixture(t)
	setXattrOrSkip(t, path, xattrName(), "value")
	setXattrOrSkip(t, path, xattrName()+"-empty", "")
	want := attributesOf(t, path)
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	before, _ := os.Stat(path)
	out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("new\n")), nil)
	if out.Err != nil || !out.Published || out.Uncertain {
		t.Fatalf("publication %+v", out)
	}
	after, _ := os.Stat(path)
	if os.SameFile(before, after) {
		t.Fatal("expected a replacement inode")
	}
	if got := attributesOf(t, path); !equalXattrs(got, want) {
		t.Fatalf("attributes changed: got %v want %v", got, want)
	}
	assertNoStage(t, root)
}

func TestXattrChangedAfterAcquisitionIsStale(t *testing.T) {
	root, path := nativeFixture(t)
	setXattrOrSkip(t, path, xattrName(), "one")
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	setXattrOrSkip(t, path, xattrName(), "two")
	out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("new\n")), nil)
	if out.Err == nil || out.Published {
		t.Fatalf("accepted changed attribute %+v", out)
	}
	assertNativeBytes(t, path, a.Documents[0].Source.Bytes())
	assertNoStage(t, root)
}

func TestXattrAddedAfterAcquisitionIsStale(t *testing.T) {
	root, path := nativeFixture(t)
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	setXattrOrSkip(t, path, xattrName(), "late")
	out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("new\n")), nil)
	if out.Err == nil || out.Published {
		t.Fatalf("accepted added attribute %+v", out)
	}
	assertNativeBytes(t, path, a.Documents[0].Source.Bytes())
	assertNoStage(t, root)
}

func TestXattrChangedOnStageBeforeFinalCheckIsRefused(t *testing.T) {
	root, path := nativeFixture(t)
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	set.native.hook = func(stage, _ string) error {
		if stage != "before-final-check" {
			return nil
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".mdchk-stage-") {
				stagePath := filepath.Join(root, entry.Name())
				if err := unix.Setxattr(stagePath, xattrName(), []byte("changed"), 0); err != nil {
					if xattrUnsupported(err) || errors.Is(err, unix.EPERM) {
						return nil
					}
					return err
				}
				return nil
			}
		}
		return errors.New("stage missing")
	}
	out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("candidate\n")), nil)
	if out.Err == nil {
		// The filesystem refused the attribute, so there was nothing to detect.
		if _, err := unix.Getxattr(path, xattrName(), nil); err == nil || !out.Published {
			t.Fatalf("unexpected outcome %+v", out)
		}
		t.Skip("filesystem refuses extended attributes")
	}
	if out.Published {
		t.Fatalf("changed stage published %+v", out)
	}
	assertNativeBytes(t, path, a.Documents[0].Source.Bytes())
	assertNoStage(t, root)
}

// A new file can arrive with attributes the target does not carry, such as an
// inherited default ACL or a creation-time label. They must be removed so the
// replacement carries exactly the target's set.
func TestCopyXattrsRemovesAttributesTheTargetLacks(t *testing.T) {
	_, path := nativeFixture(t)
	setXattrOrSkip(t, path, xattrName(), "stale")
	setXattrOrSkip(t, path, xattrName()+"-keep", "same")
	f, err := os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // Synthetic fixture path created by the test.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	want := xattrSet{xattrName() + "-keep": []byte("same"), xattrName() + "-new": []byte("added")}
	if err = copyXattrs(f, want); err != nil {
		t.Fatal(err)
	}
	got, err := readXattrs(f)
	if err != nil || !equalXattrs(got, want) {
		t.Fatalf("attributes %v %v want %v", got, err, want)
	}
}

func TestEqualXattrsComparesNamesAndValues(t *testing.T) {
	a := xattrSet{"user.a": []byte("1")}
	for name, b := range map[string]xattrSet{
		"value":   {"user.a": []byte("2")},
		"name":    {"user.b": []byte("1")},
		"extra":   {"user.a": []byte("1"), "user.b": nil},
		"missing": nil,
	} {
		if equalXattrs(a, b) {
			t.Errorf("%s mismatch compared equal", name)
		}
	}
	if !equalXattrs(a, xattrSet{"user.a": []byte("1")}) || !equalXattrs(nil, xattrSet{}) {
		t.Error("equal sets compared different")
	}
}
