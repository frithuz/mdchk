//go:build unix

package acceptance

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestCacheModes(t *testing.T) {
	t.Parallel()
	m := newFixtureModule(t)
	cacheRoot := newCacheRoot(t)
	var builds atomic.Int32
	got, _ := m.resolve(t, cacheRoot, m.env, countBuilds(&builds))
	for _, dir := range []string{cacheRoot, filepath.Dir(got.binary)} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Fatalf("%s has mode %o", dir, perm)
		}
	}
}

func TestUnsafeUnixCacheRootBuildsPrivately(t *testing.T) {
	t.Parallel()
	var builds atomic.Int32
	t.Run("group and other permissions", func(t *testing.T) {
		t.Parallel()
		m := newFixtureModule(t)
		cacheRoot := newCacheRoot(t)
		if err := os.Mkdir(cacheRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		//nolint:gosec // The test needs a root other users can reach.
		if err := os.Chmod(cacheRoot, 0o750); err != nil {
			t.Fatal(err)
		}
		got, logged := m.resolve(t, cacheRoot, m.env, countBuilds(&builds))
		assertPrivateBuild(t, got, logged, cacheRoot, "which other users can reach")
	})
	t.Run("symlink", func(t *testing.T) {
		t.Parallel()
		m := newFixtureModule(t)
		target := t.TempDir()
		//nolint:gosec // A cache root is a private directory.
		if err := os.Chmod(target, 0o700); err != nil {
			t.Fatal(err)
		}
		cacheRoot := newCacheRoot(t)
		if err := os.Symlink(target, cacheRoot); err != nil {
			t.Fatal(err)
		}
		got, logged := m.resolve(t, cacheRoot, m.env, countBuilds(&builds))
		assertPrivateBuild(t, got, logged, cacheRoot, "not a real directory")
	})
	t.Run("another owner", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		//nolint:gosec // A cache root is a private directory.
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkCacheRootOwner(info, os.Getuid()); err != nil {
			t.Fatalf("own directory refused: %v", err)
		}
		if err := checkCacheRootOwner(info, os.Getuid()+1); err == nil {
			t.Fatal("another user's directory accepted")
		}
	})
}
