package acceptance

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// ReadinessHash returns the hexadecimal SHA-256 of s.
func ReadinessHash(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }

// ReadinessEntry is the captured state of one fixture entry.
//
// Info comes from File.Stat, not DirEntry.Info or path Stat. In particular,
// Windows File.Stat captures native IDs immediately; path FileInfo may defer
// identity lookup until SameFile, after a fixture has already been replaced.
// No handle remains open while an ordinary command runs.
type ReadinessEntry struct {
	Info fs.FileInfo
	Hash string
}

// ReadinessState captures every entry of the physical tree under root.
func ReadinessState(t *testing.T, root string) map[string]ReadinessEntry {
	t.Helper()
	out := map[string]ReadinessEntry{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("fixture contains symlink: %s", path)
		}
		if d.Type() != 0 && !d.IsDir() {
			return fmt.Errorf("unsupported fixture entry type %s: %s", d.Type(), path)
		}
		//nolint:gosec // Enumerated paths belong to this test-owned physical tree.
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		info, statErr := f.Stat()
		var data []byte
		var readErr error
		if statErr == nil {
			if info.Mode().IsRegular() {
				data, readErr = io.ReadAll(f)
			} else if !info.IsDir() {
				readErr = fmt.Errorf("unsupported fixture entry type %s: %s", info.Mode(), path)
			}
		}
		if err := errors.Join(statErr, readErr, f.Close()); err != nil {
			return fmt.Errorf("capture %s: %w", path, err)
		}
		if info.Mode().Type() != d.Type() {
			return fmt.Errorf("fixture type changed during capture: %s", path)
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(name)] = ReadinessEntry{Info: info, Hash: ReadinessHash(string(data))}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// ReadinessDifferences describes every difference between two captured states.
func ReadinessDifferences(a, b map[string]ReadinessEntry) []string {
	names := make(map[string]bool, len(a)+len(b))
	for name := range a {
		names[name] = true
	}
	for name := range b {
		names[name] = true
	}
	paths := make([]string, 0, len(names))
	for name := range names {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	var differences []string
	for _, path := range paths {
		before, was := a[path]
		after, now := b[path]
		if !was {
			differences = append(differences, fmt.Sprintf("%q: extra entry", path))
			continue
		}
		if !now {
			differences = append(differences, fmt.Sprintf("%q: missing entry", path))
			continue
		}
		if before.Info == nil || after.Info == nil {
			differences = append(differences, fmt.Sprintf("%q: missing captured metadata", path))
			continue
		}
		add := func(field string, old, new any) {
			differences = append(differences, fmt.Sprintf("%q: %s before=%v after=%v", path, field, old, new))
		}
		if before.Hash != after.Hash {
			add("bytes SHA256", before.Hash, after.Hash)
		}
		// Directory size is filesystem bookkeeping, not file contents. Its inventory,
		// type, identity, mode and mtime remain checked exactly.
		if before.Info.Mode().IsRegular() && before.Info.Size() != after.Info.Size() {
			add("size", before.Info.Size(), after.Info.Size())
		}
		if before.Info.Mode().Type() != after.Info.Mode().Type() {
			add("type", before.Info.Mode().Type(), after.Info.Mode().Type())
		}
		if before.Info.Mode() != after.Info.Mode() {
			add("mode", before.Info.Mode(), after.Info.Mode())
		}
		if !os.SameFile(before.Info, after.Info) {
			add("native identity equal", true, false)
		}
		if !before.Info.ModTime().Equal(after.Info.ModTime()) {
			add("mtime", before.Info.ModTime(), after.Info.ModTime())
		}
	}
	return differences
}

// ReadinessAssertEqual fails the test when two captured states differ.
func ReadinessAssertEqual(t *testing.T, checkpoint string, a, b map[string]ReadinessEntry) {
	t.Helper()
	if differences := ReadinessDifferences(a, b); len(differences) != 0 {
		t.Fatalf("%s (%s/%s): fixture preservation failed:\n%s", checkpoint, runtime.GOOS, runtime.GOARCH, strings.Join(differences, "\n"))
	}
}

// ReadinessAssertParentAfterSave fails unless a saved file's parent kept its
// identity and mode.
func ReadinessAssertParentAfterSave(t *testing.T, before, after ReadinessEntry) {
	t.Helper()
	if before.Info == nil || after.Info == nil {
		t.Fatal("apply parent: missing captured metadata")
	}
	if !os.SameFile(before.Info, after.Info) {
		t.Fatal("apply parent: native identity changed")
	}
	if before.Info.Mode() != after.Info.Mode() {
		t.Fatalf("apply parent: type/mode before=%s after=%s", before.Info.Mode(), after.Info.Mode())
	}
}
