package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real filesystem mutations exercise the oracle independently of mdchk and its
// correction provider. No sleeps, metadata tolerances or capture retries hide a
// change. The same selection executes these controls on every CI host.
func TestCLIReadinessStateOracle(t *testing.T) {
	type mutation struct {
		name, path, field string
		change            func(*testing.T, string, map[string]readinessEntry)
	}
	must := func(t *testing.T, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	restoreRoot := func(t *testing.T, root string, before map[string]readinessEntry) {
		t.Helper()
		stamp := before["."].Info.ModTime()
		must(t, os.Chtimes(root, stamp, stamp))
	}
	mutations := []mutation{
		{"same-length bytes with restored mtime", "file.md", "bytes SHA256", func(t *testing.T, root string, before map[string]readinessEntry) {
			path := filepath.Join(root, "file.md")
			must(t, os.WriteFile(path, []byte("other"), 0600))
			stamp := before["file.md"].Info.ModTime()
			must(t, os.Chtimes(path, stamp, stamp))
		}},
		{"same bytes replacement with restored metadata", "file.md", "native identity equal", func(t *testing.T, root string, before map[string]readinessEntry) {
			path := filepath.Join(root, "file.md")
			// Keep the old object alive outside the captured tree so the filesystem
			// cannot reuse its native ID for the newly created object.
			must(t, os.Rename(path, filepath.Join(filepath.Dir(root), "held-file.md")))
			must(t, os.WriteFile(path, []byte("first"), before["file.md"].Info.Mode().Perm()))
			stamp := before["file.md"].Info.ModTime()
			must(t, os.Chtimes(path, stamp, stamp))
			restoreRoot(t, root, before)
		}},
		{"mtime", "file.md", "mtime", func(t *testing.T, root string, before map[string]readinessEntry) {
			stamp := before["file.md"].Info.ModTime().Add(-24 * time.Hour)
			must(t, os.Chtimes(filepath.Join(root, "file.md"), stamp, stamp))
		}},
		{"size", "file.md", "size", func(t *testing.T, root string, _ map[string]readinessEntry) {
			must(t, os.WriteFile(filepath.Join(root, "file.md"), []byte("longer content"), 0600))
		}},
		{"extra", "added.md", "extra entry", func(t *testing.T, root string, _ map[string]readinessEntry) {
			must(t, os.WriteFile(filepath.Join(root, "added.md"), []byte("new"), 0600))
		}},
		{"missing", "file.md", "missing entry", func(t *testing.T, root string, _ map[string]readinessEntry) {
			must(t, os.Remove(filepath.Join(root, "file.md")))
		}},
		{"directory replacement with restored metadata", "nested", "native identity equal", func(t *testing.T, root string, before map[string]readinessEntry) {
			path := filepath.Join(root, "nested")
			must(t, os.Rename(path, filepath.Join(filepath.Dir(root), "held-directory")))
			must(t, os.Mkdir(path, before["nested"].Info.Mode().Perm()))
			stamp := before["nested"].Info.ModTime()
			must(t, os.Chtimes(path, stamp, stamp))
			restoreRoot(t, root, before)
		}},
		{"root replacement", ".", "native identity equal", func(t *testing.T, root string, before map[string]readinessEntry) {
			must(t, os.Rename(root, filepath.Join(filepath.Dir(root), "held-root")))
			must(t, os.Mkdir(root, before["."].Info.Mode().Perm()))
			must(t, os.Mkdir(filepath.Join(root, "nested"), before["nested"].Info.Mode().Perm()))
			must(t, os.WriteFile(filepath.Join(root, "file.md"), []byte("first"), before["file.md"].Info.Mode().Perm()))
			for _, name := range []string{"file.md", "nested", "."} {
				stamp := before[name].Info.ModTime()
				must(t, os.Chtimes(filepath.Join(root, name), stamp, stamp))
			}
		}},
		{"type", "file.md", "type", func(t *testing.T, root string, _ map[string]readinessEntry) {
			path := filepath.Join(root, "file.md")
			must(t, os.Remove(path))
			must(t, os.Mkdir(path, 0700))
		}},
		{"directory mtime", "nested", "mtime", func(t *testing.T, root string, before map[string]readinessEntry) {
			stamp := before["nested"].Info.ModTime().Add(-24 * time.Hour)
			must(t, os.Chtimes(filepath.Join(root, "nested"), stamp, stamp))
		}},
		{"mode where exposed", "file.md", "mode", func(t *testing.T, root string, before map[string]readinessEntry) {
			path := filepath.Join(root, "file.md")
			must(t, os.Chmod(path, 0400))
			// Restore write permission before Windows temporary directory cleanup.
			t.Cleanup(func() { must(t, os.Chmod(path, 0600)) })
			after := readinessState(t, root)
			if after["file.md"].Info.Mode() == before["file.md"].Info.Mode() {
				t.Skip("host does not expose this mode change; normative mode comparison remains enabled")
			}
		}},
	}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "project")
			must(t, os.Mkdir(root, 0700))
			must(t, os.Mkdir(filepath.Join(root, "nested"), 0700))
			must(t, os.WriteFile(filepath.Join(root, "file.md"), []byte("first"), 0600))
			before := readinessState(t, root)
			readinessAssertEqual(t, "no-command repeated capture", before, readinessState(t, root))
			m.change(t, root, before)
			differences := readinessDifferences(before, readinessState(t, root))
			found := false
			for _, difference := range differences {
				if strings.HasPrefix(difference, `"`+m.path+`": `+m.field) {
					found = true
				}
			}
			if !found {
				t.Fatalf("mutation was not identified as %q/%s: %v", m.path, m.field, differences)
			}
			t.Logf("detected %s: %s", m.name, strings.Join(differences, "; "))
		})
	}
}
