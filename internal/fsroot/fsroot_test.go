package fsroot

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// symlinkOrSkip skips only when the OS denies creating symlinks, which Windows
// does without developer mode or elevation.
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("host denies os.Symlink: %v", err)
	}
}

func setHook(t *testing.T, f func(stage, path string)) {
	t.Helper()
	hook = f
	t.Cleanup(func() { hook = nil })
}

func TestPinReturnsIdentityOfEveryDirectory(t *testing.T) {
	root := fixture(t)
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	dir, chain, err := Pin(nested)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dir.Close() }()
	expected := 1
	for p := nested; filepath.Dir(p) != p; p = filepath.Dir(p) {
		expected++
	}
	if len(chain) != expected {
		t.Fatalf("chain length %d want %d", len(chain), expected)
	}
	info, err := dir.Info()
	if err != nil || !os.SameFile(info, chain[len(chain)-1]) {
		t.Fatalf("final identity mismatch: %v", err)
	}
	again, second, err := Pin(nested)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = again.Close() }()
	if !SameChain(chain, second) {
		t.Fatal("identical pin reported a different chain")
	}
}

func TestPinRejectsNoncanonicalPaths(t *testing.T) {
	root := fixture(t)
	for _, path := range []string{"relative", filepath.Join(root, "a", "..", "b") + string(filepath.Separator), root + string(filepath.Separator) + string(filepath.Separator) + "x", root + "\x00"} {
		if dir, _, err := Pin(path); err == nil {
			_ = dir.Close()
			t.Errorf("accepted %q", path)
		}
	}
}

func TestPinRejectsSymlinkAndMissingComponents(t *testing.T) {
	root := fixture(t)
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	symlinkOrSkip(t, real, link)
	if dir, _, err := Pin(link); !errors.Is(err, ErrNotPlain) {
		if dir != nil {
			_ = dir.Close()
		}
		t.Fatalf("symlinked directory pinned: %v", err)
	}
	if dir, _, err := Pin(filepath.Join(link, "child")); err == nil {
		_ = dir.Close()
		t.Fatal("pinned through a symlinked ancestor")
	}
	if dir, _, err := Pin(filepath.Join(root, "missing")); err == nil {
		_ = dir.Close()
		t.Fatal("pinned a missing directory")
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if dir, _, err := Pin(file); !errors.Is(err, ErrNotPlain) {
		if dir != nil {
			_ = dir.Close()
		}
		t.Fatalf("regular file pinned as directory: %v", err)
	}
}

// A directory replaced by a symlink between Lstat and open must be detected and
// must never lead outside the root. A contained link is caught by the identity
// comparison; an escaping link is refused by os.Root itself.
func TestPinDetectsDirectorySwappedToSymlink(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target func(decoy string) string
		is     error
	}{
		{"contained", func(string) string { return "decoy" }, ErrChanged},
		{"escaping", func(decoy string) string { return decoy }, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t)
			victim := filepath.Join(root, "victim")
			decoy := filepath.Join(root, "decoy")
			for _, d := range []string{victim, decoy} {
				if err := os.Mkdir(d, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			swapped := false
			setHook(t, func(stage, path string) {
				if stage != "lstat-dir" || path != victim || swapped {
					return
				}
				if err := os.Remove(victim); err != nil {
					t.Error(err)
					return
				}
				if err := os.Symlink(tc.target(decoy), victim); err != nil {
					t.Skipf("host denies os.Symlink: %v", err)
				}
				swapped = true
			})
			dir, _, err := Pin(victim)
			if !swapped {
				t.Fatal("hook did not swap the directory")
			}
			if err == nil {
				_ = dir.Close()
				t.Fatal("swapped directory was pinned")
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestOpenRegularReadsPlainFile(t *testing.T) {
	root := fixture(t)
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir, _, err := Pin(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dir.Close() }()
	file, info, err := dir.OpenRegular("a.md")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(file)
	if err != nil || string(raw) != "hello" || info.Size() != 5 {
		t.Fatalf("read %q %v size=%d", raw, err, info.Size())
	}
}

func TestOpenRegularRefusesSymlinksDirectoriesAndMissing(t *testing.T) {
	root := fixture(t)
	if err := os.WriteFile(filepath.Join(root, "real.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	dir, _, err := Pin(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dir.Close() }()
	if _, _, err := dir.OpenRegular("sub"); !errors.Is(err, ErrNotPlain) {
		t.Fatalf("directory opened as file: %v", err)
	}
	if _, _, err := dir.OpenRegular("missing.md"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: %v", err)
	}
	symlinkOrSkip(t, filepath.Join(root, "real.md"), filepath.Join(root, "link.md"))
	if _, _, err := dir.OpenRegular("link.md"); !errors.Is(err, ErrNotPlain) {
		t.Fatalf("symlinked file opened: %v", err)
	}
}

func TestOpenRegularDetectsFileSwappedToSymlink(t *testing.T) {
	root := fixture(t)
	outside := filepath.Join(fixture(t), "outside.md")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "a.md")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir, _, err := Pin(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dir.Close() }()
	swapped := false
	setHook(t, func(stage, p string) {
		if stage != "lstat-file" || p != path || swapped {
			return
		}
		swapped = true
		if err := os.Remove(path); err != nil {
			t.Error(err)
			return
		}
		if err := os.Symlink(outside, path); err != nil {
			t.Error(err)
		}
	})
	file, _, err := dir.OpenRegular("a.md")
	if !swapped {
		t.Skip("host denies os.Symlink or hook did not run")
	}
	if err == nil {
		raw, _ := io.ReadAll(file)
		_ = file.Close()
		t.Fatalf("swapped file opened and read %q", raw)
	}
}

func TestOpenRegularDetectsInodeReplacement(t *testing.T) {
	root := fixture(t)
	path := filepath.Join(root, "a.md")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir, _, err := Pin(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dir.Close() }()
	replaced := false
	setHook(t, func(stage, p string) {
		if stage != "lstat-file" || p != path || replaced {
			return
		}
		replaced = true
		tmp := path + ".new"
		if err := os.WriteFile(tmp, []byte("replacement"), 0o600); err != nil {
			t.Error(err)
			return
		}
		if err := os.Rename(tmp, path); err != nil {
			t.Error(err)
		}
	})
	file, _, err := dir.OpenRegular("a.md")
	if err == nil {
		_ = file.Close()
		t.Fatal("replacement between Lstat and open went unnoticed")
	}
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMutationsStayInsidePinnedDirectory(t *testing.T) {
	root := fixture(t)
	outer := fixture(t)
	if err := os.WriteFile(filepath.Join(outer, "sentinel"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir, _, err := Pin(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dir.Close() }()
	for _, name := range []string{"../escape", filepath.Join("..", filepath.Base(outer), "sentinel")} {
		if file, err := dir.CreateExclusive(name, 0o600); err == nil {
			_ = file.Close()
			t.Errorf("created %q outside the pinned directory", name)
		}
	}
	stage, err := dir.CreateExclusive("stage", 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = stage.WriteString("new"); err != nil {
		t.Fatal(err)
	}
	if err = stage.Close(); err != nil {
		t.Fatal(err)
	}
	if again, err := dir.CreateExclusive("stage", 0o600); err == nil {
		_ = again.Close()
		t.Fatal("exclusive create replaced an existing entry")
	}
	if err = os.WriteFile(filepath.Join(root, "target"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = dir.Rename("stage", "target"); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(root, "target")); err != nil || string(raw) != "new" { //nolint:gosec // Test-owned synthetic fixture path.
		t.Fatalf("rename result %q %v", raw, err)
	}
	if err = dir.Sync(); err != nil {
		t.Fatalf("directory sync: %v", err)
	}
	if err = dir.Remove("target"); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(outer, "sentinel")); err != nil || string(raw) != "outside" { //nolint:gosec // Test-owned synthetic fixture path.
		t.Fatalf("outside sentinel disturbed: %q %v", raw, err)
	}
}

func TestSameChainComparesIdentityAndMode(t *testing.T) {
	root := fixture(t)
	_, a, err := pinAndClose(root)
	if err != nil {
		t.Fatal(err)
	}
	if SameChain(a, a[:len(a)-1]) {
		t.Fatal("different lengths compared equal")
	}
	other := fixture(t)
	_, b, err := pinAndClose(other)
	if err != nil {
		t.Fatal(err)
	}
	if SameChain(a, b) {
		t.Fatal("different directories compared equal")
	}
}

func pinAndClose(path string) (*Dir, []os.FileInfo, error) {
	dir, chain, err := Pin(path)
	if err != nil {
		return nil, nil, err
	}
	return dir, chain, dir.Close()
}
