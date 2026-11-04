//go:build linux || darwin || windows

package files

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/fsroot"
	"github.com/frithuz/mdchk/internal/parser"
)

// stableStat takes identity from an open handle. On Windows a path-based os.Stat
// result loads its file ID lazily, when SameFile first asks, so comparing it
// with a later stat of the same path would compare the current file with itself.
func stableStat(path string) (fs.FileInfo, error) {
	f, err := os.Open(path) //nolint:gosec // The path names an independently created synthetic test fixture.
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	return info, errors.Join(err, f.Close())
}

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

// symlinkOrSkip skips only when the OS denies creating symlinks, which Windows
// does without developer mode or elevation.
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("host denies os.Symlink: %v", err)
	}
}

// outsideSentinel creates a file beside the fixture root that no apply may touch.
func outsideSentinel(t *testing.T, root string) string {
	t.Helper()
	outside := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-sentinel")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	return outside
}

func TestNativePublicationExactBytesMetadataAndHardlinks(t *testing.T) {
	root, path := nativeFixture(t)
	alias := filepath.Join(root, "alias.md")
	if err := os.Link(path, alias); err != nil {
		t.Skipf("host denies hard links: %v", err)
	}
	before, _ := stableStat(path)
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	candidate := parser.NewSource([]byte("old\r\nbody\r\n"))
	out := set.Publish(context.Background(), path, a.Documents[0].Source, candidate, nil)
	if out.Err != nil || !out.Published || out.Uncertain || out.Saved != candidate {
		t.Fatalf("publication %+v", out)
	}
	if out.HardLinks != 2 {
		t.Fatalf("hard link count %d", out.HardLinks)
	}
	after, _ := stableStat(path)
	if os.SameFile(before, after) || after.Mode() != before.Mode() {
		t.Fatalf("identity/mode before=%v after=%v", before, after)
	}
	if !sameOwner(before, after) {
		t.Fatal("ownership changed")
	}
	assertNativeBytes(t, path, candidate.Bytes())
	assertNativeBytes(t, alias, []byte("old\r\nbody\r"))
	assertNoStage(t, root)
}

func TestNativeStalePreconditions(t *testing.T) {
	for _, kind := range []string{"content-restored-mtime", "identical-inode-replacement", "missing", "mode", "symlink", "directory", "parent"} {
		t.Run(kind, func(t *testing.T) {
			root, path := nativeFixture(t)
			set, a := nativeAcquire(t, []string{path}, WriteOptions{})
			before, _ := stableStat(path)
			outside := outsideSentinel(t, root)
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
				replacement, err := stableStat(path)
				if err != nil || os.SameFile(before, replacement) {
					t.Fatalf("replacement did not create independent inode: %v", err)
				}
			case "missing":
				_ = os.Remove(path)
			case "mode":
				// 0444 changes the mode on every host: Windows maps only the
				// read-only attribute, so 0600 would be indistinguishable there.
				_ = os.Chmod(path, 0444) //nolint:gosec // Exercise stale detection of a deliberately read-only synthetic input.
			case "symlink":
				_ = os.Remove(path)
				symlinkOrSkip(t, outside, path)
			case "directory":
				_ = os.Remove(path)
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "parent":
				moved := root + ".moved"
				if err := os.Rename(root, moved); err != nil {
					t.Skipf("host refuses to rename a held directory: %v", err)
				}
				defer func() { _ = os.RemoveAll(moved) }()
				symlinkOrSkip(t, moved, root)
				defer func() { _ = os.Remove(root) }()
			}
			out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("candidate\n")), nil)
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

// A component swapped to a symlink at each boundary must never let the write
// escape the selected target, whether the race is lost or won.
func TestNativeSymlinkSwapRacesNeverWriteOutside(t *testing.T) {
	for _, swap := range []string{"file", "parent"} {
		for _, stage := range []string{"start", "before-final-check", "after-final-check"} {
			t.Run(swap+"-"+stage, func(t *testing.T) {
				root, path := nativeFixture(t)
				outside := outsideSentinel(t, root)
				set, a := nativeAcquire(t, []string{path}, WriteOptions{})
				moved := root + ".moved"
				swapped := false
				set.native.hook = func(at, _ string) error {
					if at != stage || swapped {
						return nil
					}
					swapped = true
					switch swap {
					case "file":
						if err := os.Remove(path); err != nil {
							return err
						}
						return os.Symlink(outside, path)
					default:
						if err := os.Rename(root, moved); err != nil {
							return err
						}
						return os.Symlink(moved, root)
					}
				}
				t.Cleanup(func() { _ = os.Remove(root); _ = os.RemoveAll(moved) })
				out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("candidate\n")), nil)
				if !swapped {
					t.Fatal("hook never ran")
				}
				assertNativeBytes(t, outside, []byte("outside"))
				if stage != "after-final-check" && (out.Err == nil || out.Published) {
					t.Fatalf("missed detectable swap %+v", out)
				}
			})
		}
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
	if got := set.Publish(context.Background(), path, parser.NewSource(original.Bytes()), candidate, nil); got.Err == nil {
		t.Fatal("foreign source accepted")
	}
	if got := set.Publish(context.Background(), resource, r.Documents[0].Source, candidate, nil); got.Err == nil {
		t.Fatal("resource promoted to target")
	}
	a.Documents[0].Path = resource
	a.Documents[0].Source = parser.NewSource([]byte("forged"))
	_ = os.WriteFile(missing, []byte("appeared"), 0600)
	if got := set.Publish(context.Background(), path, original, candidate, nil); got.Err == nil {
		t.Fatal("appeared absence accepted")
	}
	_ = os.Remove(missing)
	_ = os.WriteFile(resource, []byte("changed"), 0600)
	if got := set.Publish(context.Background(), path, original, candidate, nil); got.Err == nil {
		t.Fatal("changed config accepted")
	}
	assertNativeBytes(t, path, original.Bytes())
	assertNoStage(t, root)
}

func TestNativePublicationFailureBoundaries(t *testing.T) {
	for _, stage := range []string{"start", "create", "write", "short-write", "metadata", "flush", "close", "before-final-check", "rename", "directory-flush", "cleanup"} {
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
			got := set.Publish(context.Background(), path, original, candidate, nil)
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
	for _, stage := range []string{"start", "before-final-check", "after-final-check"} {
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
			out := set.Publish(context.Background(), path, a.Documents[0].Source, candidate, nil)
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
	out := set.Publish(ctx, path, a.Documents[0].Source, parser.NewSource([]byte("new")), nil)
	if !errors.Is(out.Err, context.Canceled) {
		t.Fatalf("cancel %+v", out)
	}
	_ = set.budget.ChargeWork(set.budget.Limits().WorkUnits + 1)
	if out = set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("new")), nil); out.Err == nil {
		t.Fatal("exhausted residual revived")
	}
	assertNoStage(t, root)
}

func TestNativeUnsupportedTargetsRefusedAtAcquisition(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode fs.FileMode
	}{
		{"read-only", 0444},
		{"owner-unwritable", 0500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, path := nativeFixture(t)
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			selection, _ := Select(Options{Paths: []string{path}})
			b, _ := parser.NewOperationBudget(parser.DefaultLimits())
			set, a, _, err := AcquireWrite(context.Background(), selection, WriteOptions{AcquireOptions: AcquireOptions{Budget: b}})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = set.Close() }()
			if len(a.Errors) != 1 || !errors.Is(a.Errors[0], errUnsupportedTarget) || len(a.Documents) != 0 {
				t.Fatalf("unsupported target acquired %+v", a)
			}
			assertNativeBytes(t, path, []byte("old\r\nbody\r"))
			assertNoStage(t, root)
		})
	}
}

func TestNativeInspectionAcquisitionIsReadOnly(t *testing.T) {
	root, path := nativeFixture(t)
	link := filepath.Join(root, "link.md")
	symlinkOrSkip(t, path, link)
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	set, a, _, err := AcquireWrite(context.Background(), &Selection{Targets: []Target{{Path: link, inspection: true}}}, WriteOptions{AcquireOptions: AcquireOptions{Budget: b}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = set.Close() }()
	if len(a.Errors) != 1 || !strings.Contains(a.Errors[0].Error(), "read-only") {
		t.Fatalf("inspection became writable %+v", a)
	}
}

func TestNativeSavedDependencyRemainsValidForLaterPublication(t *testing.T) {
	root, path := nativeFixture(t)
	other := filepath.Join(root, "b.md")
	if err := os.WriteFile(other, []byte("other"), 0600); err != nil {
		t.Fatal(err)
	}
	set, a := nativeAcquire(t, []string{path, other}, WriteOptions{})
	first := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("first\n")), []string{other})
	if first.Err != nil || !first.Published {
		t.Fatalf("first %+v", first)
	}
	second := set.Publish(context.Background(), other, a.Documents[1].Source, parser.NewSource([]byte("second\n")), []string{path})
	if second.Err != nil || !second.Published {
		t.Fatalf("a dependency saved by this operation became stale: %+v", second)
	}
	assertNativeBytes(t, path, []byte("first\n"))
	assertNativeBytes(t, other, []byte("second\n"))
	assertNoStage(t, root)
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
	out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("candidate\n")), nil)
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
			out := set.Publish(ctx, path, a.Documents[0].Source, parser.NewSource([]byte("new\n")), nil)
			if !errors.Is(out.Err, context.Canceled) || out.Published != (stage == "published") {
				t.Fatalf("cancellation boundary %+v", out)
			}
		})
	}
}

func TestNativeRealShortWriteLeavesOriginal(t *testing.T) {
	root, path := nativeFixture(t)
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	set.native.write = func(f *os.File, data []byte) (int, error) { return f.Write(data[:1]) }
	out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("new content\n")), nil)
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
				// Create the replacement before the stage is freed so the two can
				// never share an inode number, which filesystems readily reuse.
				substitute := filepath.Join(root, "substitute")
				if err = os.WriteFile(substitute, []byte("foreign"), 0600); err != nil {
					return err
				}
				return os.Rename(substitute, foreign)
			}
		}
		return errors.New("no stage")
	}
	out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("new\n")), nil)
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
	out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("new\n")), nil)
	if !errors.Is(out.Err, context.Canceled) || out.Published {
		t.Fatalf("canceled acquisition revived %+v", out)
	}
}

func TestNativeRenameErrorReconcilesActualPublication(t *testing.T) {
	injected := errors.New("injected rename failure")
	for _, kind := range []string{"before", "after", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			root, path := nativeFixture(t)
			set, a := nativeAcquire(t, []string{path}, WriteOptions{})
			candidate := parser.NewSource([]byte("candidate\n"))
			set.native.rename = func(parent *fsroot.Dir, stage, name string) error {
				if kind != "before" {
					if err := parent.Rename(stage, name); err != nil {
						return err
					}
					if kind == "unknown" {
						if err := parent.Remove(name); err != nil {
							return err
						}
					}
				}
				return injected
			}
			out := set.Publish(context.Background(), path, a.Documents[0].Source, candidate, nil)
			if !errors.Is(out.Err, injected) {
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
	for _, stage := range []string{"start", "before-final-check", "after-final-check", "published"} {
		t.Run(stage, func(t *testing.T) {
			root, path, set, a, cancel := originNativeFixture(t)
			candidate := parser.NewSource([]byte("candidate\n"))
			set.native.hook = func(at, _ string) error {
				if at == stage {
					cancel()
				}
				return nil
			}
			out := set.Publish(context.Background(), path, a.Documents[0].Source, candidate, nil)
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

func TestNativeNoChangeNeverConfirmsSaved(t *testing.T) {
	for _, text := range []string{"", "clean\n"} {
		t.Run(fmt.Sprintf("bytes%d", len(text)), func(t *testing.T) {
			root, path := nativeFixture(t)
			if err := os.WriteFile(path, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			set, a := nativeAcquire(t, []string{path}, WriteOptions{})
			source := a.Documents[0].Source
			before, _ := stableStat(path)
			out := set.Publish(context.Background(), path, source, parser.NewSource(source.Bytes()), nil)
			if out.Err != nil || !out.NoChange || out.Published || out.Started || out.Saved != (parser.Source{}) {
				t.Fatalf("no-change confirmation %+v", out)
			}
			after, _ := stableStat(path)
			if !os.SameFile(before, after) {
				t.Fatal("no-change replaced inode")
			}
			assertNoStage(t, root)
			if err := os.WriteFile(path, []byte("external"), 0600); err != nil {
				t.Fatal(err)
			}
			out = set.Publish(context.Background(), path, source, source, nil)
			if out.Published || out.Saved != (parser.Source{}) {
				t.Fatalf("stale no-change confirmed %+v", out)
			}
			assertNativeBytes(t, path, []byte("external"))
			assertNoStage(t, root)
		})
	}
}
