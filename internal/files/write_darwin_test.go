//go:build darwin && arm64 && cgo

package files

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

func nativeFixture(t *testing.T) (string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "a.md")
	if err = os.WriteFile(path, []byte("old\r\nbody\r"), 0640); err != nil { //nolint:gosec // Synthetic fixture deliberately exercises preservation of group-readable mode.
		t.Fatal(err)
	}
	return root, path
}
func nativeAcquire(t *testing.T, paths []string, options WriteOptions) (*WriteSet, *Acquisition) {
	t.Helper()
	selection, err := Select(Options{Paths: paths})
	if err != nil || len(selection.Errors) > 0 {
		t.Fatalf("select: %v %+v", err, selection)
	}
	if options.Budget == nil {
		options.Budget, _ = parser.NewOperationBudget(parser.DefaultLimits())
	}
	set, a, _, err := AcquireWrite(context.Background(), selection, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close() })
	if len(a.Errors) > 0 || len(a.Interruptions) > 0 || len(a.Documents) != len(paths) {
		t.Fatalf("acquire: %+v", a)
	}
	return set, a
}
func assertNativeBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path) //nolint:gosec // The path names an independently created synthetic test fixture.
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("bytes at %s=%q,%v want %q", path, got, err, want)
	}
}
func assertNoStage(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".mdchk-stage-") {
			t.Errorf("leftover %s", e.Name())
		}
	}
}
func TestNativePublicationExactBytesMetadataAndHardlinks(t *testing.T) {
	root, path := nativeFixture(t)
	alias := filepath.Join(root, "alias.md")
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	candidate := parser.NewSource([]byte("old\r\nbody\r\n"))
	out := set.PublishPrototype(context.Background(), path, a.Documents[0].Source, candidate, nil)
	if out.Err != nil || !out.Published || out.Uncertain || out.Saved != candidate || out.HardLinks != 2 {
		t.Fatalf("publication %+v", out)
	}
	after, _ := os.Stat(path)
	if os.SameFile(before, after) || after.Mode() != before.Mode() {
		t.Fatalf("identity/mode before=%v after=%v", before, after)
	}
	x := before.Sys().(*syscall.Stat_t)
	y := after.Sys().(*syscall.Stat_t)
	if x.Uid != y.Uid || x.Gid != y.Gid {
		t.Fatal("ownership changed")
	}
	assertNativeBytes(t, path, candidate.Bytes())
	assertNativeBytes(t, alias, []byte("old\r\nbody\r"))
	assertNoStage(t, root)
}
func TestNativeStalePreconditions(t *testing.T) {
	for _, kind := range []string{"content-restored-mtime", "identical-inode-replacement", "missing", "mode", "symlink", "fifo", "parent", "xattr", "immutable"} {
		t.Run(kind, func(t *testing.T) {
			root, path := nativeFixture(t)
			set, a := nativeAcquire(t, []string{path}, WriteOptions{})
			before, _ := os.Stat(path)
			outside := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-sentinel")
			if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.Remove(outside) }()
			switch kind {
			case "content-restored-mtime":
				if err := os.WriteFile(path, []byte("NEW\r\nBODY\r"), 0640); err != nil { //nolint:gosec // Synthetic fixture deliberately exercises preservation of group-readable mode.
					t.Fatal(err)
				}
				if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
					t.Fatal(err)
				}
			case "identical-inode-replacement":
				tmp := path + ".new"
				_ = os.WriteFile(tmp, []byte("old\r\nbody\r"), 0640) //nolint:gosec // Synthetic fixture deliberately exercises preservation of group-readable mode.
				if err := os.Rename(tmp, path); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
					t.Fatal(err)
				}
				replacement, err := os.Stat(path)
				if err != nil || os.SameFile(before, replacement) {
					t.Fatalf("replacement did not create independent inode: %v", err)
				}
			case "missing":
				_ = os.Remove(path)
			case "mode":
				_ = os.Chmod(path, 0600)
			case "symlink":
				_ = os.Remove(path)
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				_ = os.Remove(path)
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "parent":
				moved := root + ".moved"
				if err := os.Rename(root, moved); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = os.RemoveAll(moved) }()
				if err := os.Symlink(moved, root); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = os.Remove(root) }()
			case "xattr":
				if raw, err := exec.Command("/usr/bin/xattr", "-w", "user.mdchk-test", "value", path).CombinedOutput(); err != nil { //nolint:gosec // Fixed native utility or current test executable; arguments name synthetic fixtures only.
					t.Fatalf("xattr %v %s", err, raw)
				}
			case "immutable":
				if err := syscall.Chflags(path, 0x00000002); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = syscall.Chflags(path, 0) }()
			}
			out := set.PublishPrototype(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("candidate\n")), nil)
			if out.Err == nil || out.Published {
				t.Fatalf("accepted stale %s: %+v", kind, out)
			}
			assertNativeBytes(t, outside, []byte("outside"))
			if kind != "parent" {
				assertNoStage(t, root)
			}
		})
	}
}
func TestNativeAuthorityAndReadOnlyResources(t *testing.T) {
	root, path := nativeFixture(t)
	resource := filepath.Join(root, "config.md")
	_ = os.WriteFile(resource, []byte("config"), 0600)
	selected, _ := Select(Options{Paths: []string{path}})
	resources, _ := Select(Options{Paths: []string{resource}})
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	missing := filepath.Join(root, "missing.yaml")
	set, a, r, err := AcquireWrite(context.Background(), selected, WriteOptions{AcquireOptions: AcquireOptions{Budget: budget}, Resources: resources, Absences: []string{missing}, Required: map[string][]string{path: {resource, missing}}})
	if err != nil || len(a.Errors) > 0 || len(r.Errors) > 0 {
		t.Fatalf("acquire %v %+v %+v", err, a, r)
	}
	defer func() { _ = set.Close() }()
	original := a.Documents[0].Source
	candidate := parser.NewSource([]byte("candidate\n"))
	if got := set.PublishPrototype(context.Background(), path, parser.NewSource(original.Bytes()), candidate, nil); got.Err == nil {
		t.Fatal("foreign source accepted")
	}
	if got := set.PublishPrototype(context.Background(), resource, r.Documents[0].Source, candidate, nil); got.Err == nil {
		t.Fatal("resource promoted to target")
	}
	a.Documents[0].Path = resource
	a.Documents[0].Source = parser.NewSource([]byte("forged"))
	_ = os.WriteFile(missing, []byte("appeared"), 0600)
	if got := set.PublishPrototype(context.Background(), path, original, candidate, nil); got.Err == nil {
		t.Fatal("appeared absence accepted")
	}
	_ = os.Remove(missing)
	_ = os.WriteFile(resource, []byte("changed"), 0600)
	if got := set.PublishPrototype(context.Background(), path, original, candidate, nil); got.Err == nil {
		t.Fatal("changed config accepted")
	}
	assertNativeBytes(t, path, original.Bytes())
	assertNoStage(t, root)
}
func TestNativePublicationFailureBoundaries(t *testing.T) {
	for _, stage := range []string{"locked", "create", "write", "short-write", "metadata", "flush", "close", "before-final-check", "rename", "directory-flush", "cleanup"} {
		t.Run(stage, func(t *testing.T) {
			root, path := nativeFixture(t)
			set, a := nativeAcquire(t, []string{path}, WriteOptions{})
			original := a.Documents[0].Source
			candidate := parser.NewSource([]byte("new\r\n"))
			sentinel := errors.New("injected " + stage)
			set.native.hook = func(at, _ string) error {
				if at == stage || stage == "cleanup" && at == "write" {
					return sentinel
				}
				return nil
			}
			got := set.PublishPrototype(context.Background(), path, original, candidate, nil)
			if !errors.Is(got.Err, sentinel) {
				t.Fatalf("missing injected failure %+v", got)
			}
			if stage == "directory-flush" {
				if !got.Published || got.Saved != candidate {
					t.Fatalf("lost published evidence %+v", got)
				}
				assertNativeBytes(t, path, candidate.Bytes())
			} else {
				if got.Published {
					t.Fatalf("premature publication %+v", got)
				}
				assertNativeBytes(t, path, original.Bytes())
			}
			if stage == "cleanup" {
				if len(got.Leftovers) != 1 {
					t.Fatalf("unreported leftovers %+v", got)
				}
				for _, name := range got.Leftovers {
					_ = os.Remove(filepath.Join(root, name))
				}
			}
			assertNoStage(t, root)
		})
	}
}
func TestNativeDeterministicRacesAndAcceptedExternalGap(t *testing.T) {
	for _, stage := range []string{"locked", "before-final-check", "after-final-check"} {
		t.Run(stage, func(t *testing.T) {
			root, path := nativeFixture(t)
			set, a := nativeAcquire(t, []string{path}, WriteOptions{})
			candidate := parser.NewSource([]byte("candidate\n"))
			set.native.hook = func(at, _ string) error {
				if at == stage {
					return os.WriteFile(path, []byte("external newer"), 0600)
				}
				return nil
			}
			out := set.PublishPrototype(context.Background(), path, a.Documents[0].Source, candidate, nil)
			if stage == "after-final-check" {
				if out.Err != nil || !out.Published {
					t.Fatalf("accepted gap demonstration %+v", out)
				}
				assertNativeBytes(t, path, candidate.Bytes())
			} else {
				if out.Err == nil || out.Published {
					t.Fatalf("missed detectable race %+v", out)
				}
				assertNativeBytes(t, path, []byte("external newer"))
			}
			assertNoStage(t, root)
		})
	}
}
func TestNativeCancellationAndExhaustedBudget(t *testing.T) {
	root, path := nativeFixture(t)
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := set.PublishPrototype(ctx, path, a.Documents[0].Source, parser.NewSource([]byte("new")), nil)
	if !errors.Is(out.Err, context.Canceled) {
		t.Fatalf("cancel %+v", out)
	}
	_ = set.budget.ChargeWork(set.budget.Limits().WorkUnits + 1)
	if out = set.PublishPrototype(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("new")), nil); out.Err == nil {
		t.Fatal("exhausted residual revived")
	}
	assertNoStage(t, root)
}
func TestNativeLockSubprocess(t *testing.T) {
	if path := os.Getenv("MDCHK_NATIVE_LOCK_CHILD"); path != "" {
		set, a := nativeAcquire(t, []string{path}, WriteOptions{})
		out := set.PublishPrototype(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("child\n")), nil)
		if !errors.Is(out.Err, context.DeadlineExceeded) && out.Err == nil {
			t.Fatal("child bypassed parent lock")
		}
		return
	}
	root, path := nativeFixture(t)
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	set.native.hook = func(stage, _ string) error {
		if stage != "locked" {
			return nil
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestNativeLockSubprocess$") //nolint:gosec // Fixed native utility or current test executable; arguments name synthetic fixtures only.
		cmd.Env = append(os.Environ(), "MDCHK_NATIVE_LOCK_CHILD="+path)
		start := time.Now()
		raw, err := cmd.CombinedOutput()
		if err != nil {
			return errors.New(string(raw))
		}
		if time.Since(start) < 1500*time.Millisecond {
			return errors.New("child did not wait on stable parent lock")
		}
		return nil
	}
	out := set.PublishPrototype(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("parent\n")), nil)
	if out.Err != nil || !out.Published {
		t.Fatalf("parent %+v", out)
	}
	assertNativeBytes(t, path, []byte("parent\n"))
	assertNoStage(t, root)
}
func TestNativeReadOnlyAndSymlinkAcquisitionRefused(t *testing.T) {
	root, path := nativeFixture(t)
	_ = os.Chmod(path, 0444) //nolint:gosec // Exercise refusal of deliberately read-only synthetic input.
	selection, _ := Select(Options{Paths: []string{path}})
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	set, a, _, err := AcquireWrite(context.Background(), selection, WriteOptions{AcquireOptions: AcquireOptions{Budget: b}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = set.Close() }()
	if len(a.Errors) != 1 {
		t.Fatalf("read-only acquired %+v", a)
	}
	_ = os.Chmod(path, 0600)
	link := filepath.Join(root, "link.md")
	_ = os.Symlink(path, link)
	target := Target{Path: link, inspection: true}
	set2, a2, _, err := AcquireWrite(context.Background(), &Selection{Targets: []Target{target}}, WriteOptions{AcquireOptions: AcquireOptions{Budget: b}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = set2.Close() }()
	if len(a2.Errors) != 1 || !errors.Is(a2.Errors[0], fs.ErrPermission) && !strings.Contains(a2.Errors[0].Error(), "read-only") {
		t.Fatalf("inspection became writable %+v", a2)
	}
}

func TestNativeACLRefusal(t *testing.T) {
	_, path := nativeFixture(t)
	raw, err := exec.Command("/bin/chmod", "+a", os.Getenv("USER")+" allow read", path).CombinedOutput() //nolint:gosec // Fixed native utility or current test executable; arguments name synthetic fixtures only.
	if err != nil {
		t.Fatalf("ACL setup %v %s", err, raw)
	}
	selected, _ := Select(Options{Paths: []string{path}})
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	set, a, _, err := AcquireWrite(context.Background(), selected, WriteOptions{AcquireOptions: AcquireOptions{Budget: b}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = set.Close() }()
	if len(a.Errors) != 1 {
		t.Fatalf("ACL silently accepted %+v", a)
	}
}

func TestNativeLockSurvivesReplacement(t *testing.T) {
	if path := os.Getenv("MDCHK_NATIVE_REPLACEMENT_CHILD"); path != "" {
		set, a := nativeAcquire(t, []string{path}, WriteOptions{})
		if err := os.WriteFile(os.Getenv("MDCHK_NATIVE_READY"), []byte("ready"), 0600); err != nil { //nolint:gosec // Private subprocess protocol receives a parent-owned synthetic ready-file path.
			t.Fatal(err)
		}
		out := set.PublishPrototype(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("child\n")), nil)
		if out.Err == nil || out.Published {
			t.Fatalf("waiting stale child published %+v", out)
		}
		return
	}
	root, path := nativeFixture(t)
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	ready := filepath.Join(root, "ready")
	var child *exec.Cmd
	set.native.hook = func(stage, _ string) error {
		if stage != "after-final-check" {
			return nil
		}
		child = exec.Command(os.Args[0], "-test.run=^TestNativeLockSurvivesReplacement$") //nolint:gosec // Fixed native utility or current test executable; arguments name synthetic fixtures only.
		child.Env = append(os.Environ(), "MDCHK_NATIVE_REPLACEMENT_CHILD="+path, "MDCHK_NATIVE_READY="+ready)
		if err := child.Start(); err != nil {
			return err
		}
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(ready); err == nil {
				return nil
			}
			time.Sleep(time.Millisecond)
		}
		return errors.New("child did not acquire original before replacement")
	}
	out := set.PublishPrototype(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("parent\n")), nil)
	if out.Err != nil || !out.Published {
		t.Fatalf("parent %+v", out)
	}
	if child == nil {
		t.Fatal("no child")
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	assertNativeBytes(t, path, []byte("parent\n"))
	assertNoStage(t, root)
}
func TestNativeReverseOverlapLockOrderingCancellationAndRelease(t *testing.T) {
	root, path := nativeFixture(t)
	otherDir := filepath.Join(root, "other")
	if err := os.Mkdir(otherDir, 0700); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(otherDir, "b.md")
	if err := os.WriteFile(other, []byte("other"), 0600); err != nil {
		t.Fatal(err)
	}
	a, aa := nativeAcquire(t, []string{path, other}, WriteOptions{})
	b, bb := nativeAcquire(t, []string{other, path}, WriteOptions{})
	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan Publication, 1)
	a.native.hook = func(stage, _ string) error {
		if stage == "locked" {
			close(held)
			<-release
		}
		return nil
	}
	go func() {
		done <- a.PublishPrototype(context.Background(), path, aa.Documents[0].Source, parser.NewSource([]byte("parent\n")), []string{other})
	}()
	<-held
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	out := b.PublishPrototype(ctx, other, bb.Documents[1].Source, parser.NewSource([]byte("new other")), []string{path})
	cancel()
	if !errors.Is(out.Err, context.DeadlineExceeded) || out.Published {
		t.Fatalf("lock cancellation %+v", out)
	}
	close(release)
	if out = <-done; out.Err != nil || !out.Published {
		t.Fatalf("first writer %+v", out)
	}
	// A new operation acquires both directories in reverse caller order after the
	// first completes; no inode-dependent or cancellation-held lock survives.
	c, cc := nativeAcquire(t, []string{other, path}, WriteOptions{})
	out = c.PublishPrototype(context.Background(), other, cc.Documents[1].Source, parser.NewSource([]byte("new other")), []string{path})
	if out.Err != nil || !out.Published {
		t.Fatalf("released locks %+v", out)
	}
}

func TestNativePostPublicationUncertaintyNeverRollsBack(t *testing.T) {
	root, path := nativeFixture(t)
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	set.native.hook = func(stage, _ string) error {
		if stage == "published" {
			return os.WriteFile(path, []byte("later external"), 0600)
		}
		return nil
	}
	out := set.PublishPrototype(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("candidate\n")), nil)
	if !out.Published || !out.Uncertain || out.Err == nil || out.Saved != (parser.Source{}) {
		t.Fatalf("invented saved state %+v", out)
	}
	assertNativeBytes(t, path, []byte("later external"))
	assertNoStage(t, root)
}
func TestNativeCancelBeforeAndAfterPublication(t *testing.T) {
	for _, stage := range []string{"after-final-check", "published"} {
		t.Run(stage, func(t *testing.T) {
			_, path := nativeFixture(t)
			set, a := nativeAcquire(t, []string{path}, WriteOptions{})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			set.native.hook = func(at, _ string) error {
				if at == stage {
					cancel()
				}
				return nil
			}
			out := set.PublishPrototype(ctx, path, a.Documents[0].Source, parser.NewSource([]byte("new\n")), nil)
			if !errors.Is(out.Err, context.Canceled) || out.Published != (stage == "published") {
				t.Fatalf("cancellation boundary %+v", out)
			}
		})
	}
}
func TestNativeCrashReleasesDirectoryLock(t *testing.T) {
	if path := os.Getenv("MDCHK_NATIVE_CRASH_CHILD"); path != "" {
		set, _ := nativeAcquire(t, []string{path}, WriteOptions{})
		o := set.native.entries[path]
		if err := syscall.Flock(int(o.parent.Fd()), syscall.LOCK_EX); err != nil {
			t.Fatal(err)
		}
		//nolint:gosec // Parent-owned synthetic ready-file path in the private test subprocess protocol.
		if err := os.WriteFile(os.Getenv("MDCHK_NATIVE_CRASH_READY"), []byte("ready"), 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(30 * time.Second)
		return
	}
	root, path := nativeFixture(t)
	ready := filepath.Join(root, "ready")
	//nolint:gosec // Only the current test executable and synthetic fixture arguments are launched.
	child := exec.Command(os.Args[0], "-test.run=^TestNativeCrashReleasesDirectoryLock$")
	child.Env = append(os.Environ(), "MDCHK_NATIVE_CRASH_CHILD="+path, "MDCHK_NATIVE_CRASH_READY="+ready)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not lock")
		}
		time.Sleep(time.Millisecond)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	out := set.PublishPrototype(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("after crash\n")), nil)
	if out.Err != nil || !out.Published {
		t.Fatalf("crash retained lock %+v", out)
	}
}

func TestNativeParentACLChangeRefused(t *testing.T) {
	root, path := nativeFixture(t)
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	//nolint:gosec // Fixed native utility changes only the synthetic parent-directory ACL.
	raw, err := exec.Command("/bin/chmod", "+a", os.Getenv("USER")+" allow read", root).CombinedOutput()
	if err != nil {
		t.Fatalf("ACL setup %v %s", err, raw)
	}
	out := set.PublishPrototype(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("new\n")), nil)
	if out.Err == nil || out.Published {
		t.Fatalf("parent ACL mutation accepted %+v", out)
	}
	assertNativeBytes(t, path, a.Documents[0].Source.Bytes())
	assertNoStage(t, root)
}
func TestNativeRealShortWriteLeavesOriginal(t *testing.T) {
	root, path := nativeFixture(t)
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	set.native.write = func(f *os.File, data []byte) (int, error) { return f.Write(data[:1]) }
	out := set.PublishPrototype(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("new content\n")), nil)
	if !errors.Is(out.Err, io.ErrShortWrite) || out.Published {
		t.Fatalf("short write %+v", out)
	}
	assertNativeBytes(t, path, a.Documents[0].Source.Bytes())
	assertNoStage(t, root)
}
func TestNativeForeignStageIsNotPublishedOrRemoved(t *testing.T) {
	root, path := nativeFixture(t)
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	var foreign string
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
				foreign = filepath.Join(root, entry.Name())
				if err = os.Remove(foreign); err != nil {
					return err
				}
				return os.WriteFile(foreign, []byte("foreign"), 0600)
			}
		}
		return errors.New("no stage")
	}
	out := set.PublishPrototype(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("new\n")), nil)
	if out.Err == nil || out.Published || len(out.Leftovers) != 1 {
		t.Fatalf("foreign stage mishandled %+v", out)
	}
	assertNativeBytes(t, path, a.Documents[0].Source.Bytes())
	assertNativeBytes(t, foreign, []byte("foreign"))
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
}

func TestNativeCanceledAcquisitionContextCannotBeRevived(t *testing.T) {
	_, path := nativeFixture(t)
	selected, _ := Select(Options{Paths: []string{path}})
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	ctx, cancel := context.WithCancel(context.Background())
	set, a, _, err := AcquireWrite(ctx, selected, WriteOptions{AcquireOptions: AcquireOptions{Budget: budget}})
	if err != nil || len(a.Errors) > 0 {
		t.Fatalf("acquire %v %+v", err, a)
	}
	defer func() { _ = set.Close() }()
	cancel()
	out := set.PublishPrototype(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("new\n")), nil)
	if !errors.Is(out.Err, context.Canceled) || out.Published {
		t.Fatalf("canceled acquisition revived %+v", out)
	}
}

func TestNativeRenameErrorReconcilesActualPublication(t *testing.T) {
	for _, kind := range []string{"before", "after", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			root, path := nativeFixture(t)
			set, a := nativeAcquire(t, []string{path}, WriteOptions{})
			candidate := parser.NewSource([]byte("candidate\n"))
			set.native.rename = func(parent *os.File, stage, name string) error {
				if kind != "before" {
					if err := nativeRename(parent, stage, name); err != nil {
						return err
					}
					if kind == "unknown" {
						if err := nativeRemove(parent, name); err != nil {
							return err
						}
					}
				}
				return syscall.EIO
			}
			out := set.PublishPrototype(context.Background(), path, a.Documents[0].Source, candidate, nil)
			if !errors.Is(out.Err, syscall.EIO) {
				t.Fatalf("rename error lost %+v", out)
			}
			switch kind {
			case "before":
				if out.Published || out.Uncertain {
					t.Fatalf("invented publication %+v", out)
				}
				assertNativeBytes(t, path, a.Documents[0].Source.Bytes())
			case "after":
				if !out.Published || out.Uncertain || out.Saved != candidate {
					t.Fatalf("lost actual publication %+v", out)
				}
				assertNativeBytes(t, path, candidate.Bytes())
			case "unknown":
				if !out.Uncertain || out.Saved != (parser.Source{}) {
					t.Fatalf("uncertain rename misreported %+v", out)
				}
			}
			assertNoStage(t, root)
		})
	}
}

func originNativeFixture(t *testing.T) (string, string, *WriteSet, *Acquisition, context.CancelFunc) {
	t.Helper()
	root, path := nativeFixture(t)
	selected, _ := Select(Options{Paths: []string{path}})
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	set, a, _, err := AcquireWrite(ctx, selected, WriteOptions{AcquireOptions: AcquireOptions{Budget: budget}})
	if err != nil || len(a.Errors) > 0 {
		t.Fatalf("acquire %v %+v", err, a)
	}
	t.Cleanup(func() { _ = set.Close() })
	return root, path, set, a, cancel
}
func TestOriginCancellationThroughoutPublication(t *testing.T) {
	for _, stage := range []string{"locked", "before-final-check", "after-final-check", "published"} {
		t.Run(stage, func(t *testing.T) {
			root, path, set, a, cancel := originNativeFixture(t)
			candidate := parser.NewSource([]byte("candidate\n"))
			set.native.hook = func(at, _ string) error {
				if at == stage {
					cancel()
				}
				return nil
			}
			out := set.PublishPrototype(context.Background(), path, a.Documents[0].Source, candidate, nil)
			if !errors.Is(out.Err, context.Canceled) || out.Published != (stage == "published") {
				t.Fatalf("origin cancellation at %s: %+v", stage, out)
			}
			if stage == "published" {
				if out.Saved != candidate {
					t.Fatalf("lost actual publication %+v", out)
				}
				assertNativeBytes(t, path, candidate.Bytes())
			} else {
				assertNativeBytes(t, path, a.Documents[0].Source.Bytes())
			}
			assertNoStage(t, root)
		})
	}
}
func TestOriginCancellationDuringRealLockWait(t *testing.T) {
	root, path, waiting, a, cancel := originNativeFixture(t)
	holding, heldAcquisition := nativeAcquire(t, []string{path}, WriteOptions{})
	locked := make(chan struct{})
	release := make(chan struct{})
	done := make(chan Publication, 1)
	holding.native.hook = func(stage, _ string) error {
		if stage == "locked" {
			close(locked)
			<-release
		}
		return nil
	}
	go func() {
		done <- holding.PublishPrototype(context.Background(), path, heldAcquisition.Documents[0].Source, parser.NewSource([]byte("holder\n")), nil)
	}()
	<-locked
	waiting.native.hook = func(stage, _ string) error {
		if stage == "lock-wait" {
			cancel()
		}
		return nil
	}
	start := time.Now()
	out := waiting.PublishPrototype(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("waiter\n")), nil)
	if !errors.Is(out.Err, context.Canceled) || out.Published || time.Since(start) > time.Second {
		t.Errorf("origin lock wait cancellation %+v", out)
	}
	assertNativeBytes(t, path, a.Documents[0].Source.Bytes())
	close(release)
	if out = <-done; out.Err != nil || !out.Published {
		t.Fatalf("holder failed %+v", out)
	}
	assertNoStage(t, root)
}
func TestFinalStageSecurityMetadata(t *testing.T) {
	for _, kind := range []string{"xattr", "acl"} {
		t.Run(kind, func(t *testing.T) {
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
					if !strings.HasPrefix(entry.Name(), ".mdchk-stage-") {
						continue
					}
					stagePath := filepath.Join(root, entry.Name())
					switch kind {
					case "xattr":
						//nolint:gosec // Fixed native utility changes only the owned synthetic staging fixture.
						raw, err := exec.Command("/usr/bin/xattr", "-w", "user.mdchk-stage", "changed", stagePath).CombinedOutput()
						if err != nil {
							return fmt.Errorf("xattr: %w %s", err, raw)
						}
					case "acl":
						//nolint:gosec // Fixed native utility changes only the owned synthetic staging fixture ACL.
						raw, err := exec.Command("/bin/chmod", "+a", os.Getenv("USER")+" allow read", stagePath).CombinedOutput()
						if err != nil {
							return fmt.Errorf("ACL: %w %s", err, raw)
						}

					}
					return nil
				}
				return errors.New("stage missing")
			}
			out := set.PublishPrototype(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("candidate\n")), nil)
			if out.Err == nil || out.Published {
				t.Fatalf("changed stage %s published %+v", kind, out)
			}
			assertNativeBytes(t, path, a.Documents[0].Source.Bytes())
			assertNoStage(t, root)
		})
	}
}
func TestNativeNoChangeNeverConfirmsSaved(t *testing.T) {
	for _, text := range []string{"", "clean\n"} {
		t.Run(fmt.Sprintf("bytes%d", len(text)), func(t *testing.T) {
			root, path := nativeFixture(t)
			if err := os.WriteFile(path, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			set, a := nativeAcquire(t, []string{path}, WriteOptions{})
			source := a.Documents[0].Source
			before, _ := os.Stat(path)
			out := set.PublishPrototype(context.Background(), path, source, parser.NewSource(source.Bytes()), nil)
			if out.Err != nil || !out.NoChange || out.Published || out.Saved != (parser.Source{}) {
				t.Fatalf("no-change confirmation %+v", out)
			}
			after, _ := os.Stat(path)
			if !os.SameFile(before, after) {
				t.Fatal("no-change replaced inode")
			}
			assertNoStage(t, root)
			if err := os.WriteFile(path, []byte("external"), 0600); err != nil {
				t.Fatal(err)
			}
			out = set.PublishPrototype(context.Background(), path, source, source, nil)
			if out.Published || out.Saved != (parser.Source{}) {
				t.Fatalf("stale no-change confirmed %+v", out)
			}
			assertNativeBytes(t, path, []byte("external"))
			assertNoStage(t, root)
		})
	}
}

func TestNativeExactProvenanceComparison(t *testing.T) {
	_, path := nativeFixture(t)
	//nolint:gosec // Operation-owned synthetic fixture, never a user document.
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	observed, err := nativeSecurity(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyNativeSecurity(f, observed); err != nil {
		t.Fatal(err)
	}
	different := []byte(observed)
	if len(different) == 0 {
		different = []byte("different")
	} else {
		different[0] ^= 1
	}
	if err := verifyNativeSecurity(f, string(different)); err == nil {
		t.Fatal("mismatched allowed provenance accepted")
	}
	if err := preserveNativeSecurity(f, string(different)); err != nil {
		t.Logf("kernel refuses provenance mutation: %v", err)
	}
	actual, err := nativeSecurity(f)
	if err != nil {
		t.Fatal(err)
	}
	if actual == observed {
		t.Log("kernel retained original provenance despite native set request; exact mismatch comparison exercised independently")
	} else if err := verifyNativeSecurity(f, observed); err == nil {
		t.Fatal("native changed provenance accepted")
	}
}
