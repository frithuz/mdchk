package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Snapshots include identity and timestamps, not merely a content checksum.
func unchangedFixFixture(t *testing.T, root string) func() {
	t.Helper()
	before := configurationBytes(t, root)
	entries := fixFixtureEntries(t, root)
	prior := map[string]os.FileInfo{}
	for path := range before {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		prior[path] = info
	}
	return func() {
		t.Helper()
		if !reflect.DeepEqual(before, configurationBytes(t, root)) || !reflect.DeepEqual(entries, fixFixtureEntries(t, root)) {
			t.Fatal("changed fixture bytes/inventory")
		}
		for path, info := range prior {
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(info, after) || info.Mode() != after.Mode() || !info.ModTime().Equal(after.ModTime()) {
				t.Fatal("changed fixture identity/metadata", path)
			}
		}
	}
}

// stableStat takes identity from an open handle. On Windows a path-based os.Stat
// result loads its file ID lazily, when SameFile first asks, so comparing it
// with a later stat of the same path would compare the current file with itself.
func stableStat(path string) (os.FileInfo, error) {
	f, err := os.Open(path) //nolint:gosec // Test-owned synthetic fixture path.
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	return info, errors.Join(err, f.Close())
}

func readFixture(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // Test-owned synthetic fixture path.
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// Include directories and symbolic links in the artifact inventory without
// following them into external trees.
func fixFixtureEntries(t *testing.T, root string) map[string]string {
	t.Helper()
	entries := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		value := entry.Type().String()
		if entry.Type()&os.ModeSymlink != 0 {
			target, e := os.Readlink(path)
			if e != nil {
				return e
			}
			value += target
		}
		entries[path] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// Ordinary apply preserves each line-ending convention and replaces exactly the
// selected file, on every supported host and from a CGO-free build.
func TestCompiledOrdinaryApply(t *testing.T) {
	t.Parallel()
	binary := sharedNoCGOExecutable(t)
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		t.Run(strings.ReplaceAll(strings.ReplaceAll(ending, "\r", "CR"), "\n", "LF"), func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
			name := filepath.Join("space ünicode", "δ document.md")
			putConfigurationFile(t, root, name, "first"+ending+"last")
			want := "first" + ending + "last" + ending
			preview := runFixConfiguration(t, binary, root, "fix", "--dry-run", name, "--format=json")
			if preview.Status != 0 || len(preview.Result.Changes) != 1 {
				t.Fatal(preview)
			}
			// Independently predicted ending, without depending on the diff renderer.
			if preview.Result.Changes[0].Candidate.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(want))) {
				t.Fatal(preview)
			}
			if got := readFixture(t, filepath.Join(root, name)); got != "first"+ending+"last" {
				t.Fatalf("dry run changed the file: %q", got)
			}
			got := runConfigurationProcess(t, binary, root, "fix", filepath.Join(".", name), "--format=json")
			r := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
			if got.Status != 0 || len(r.Errors) != 0 || len(r.Changes) != 1 || readinessFile(t, r, filepath.ToSlash(name)).WriteStatus != "written" {
				t.Fatal(got)
			}
			if saved := readFixture(t, filepath.Join(root, name)); saved != want {
				t.Fatalf("saved %q want %q", saved, want)
			}
		})
	}
}

func TestCompiledFixHelpWithoutProject(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	root := t.TempDir()
	for _, args := range [][]string{{"--help"}, {"fix", "--help"}, {"--help", "fix"}} {
		got := runConfigurationProcess(t, binary, root, args...)
		if got.Status != 0 || !strings.Contains(got.Stdout, "--dry-run") {
			t.Fatal(got)
		}
		if len(args) > 1 && (!strings.Contains(got.Stdout, "saved content") || !strings.Contains(got.Stdout, "candidate content") || !strings.Contains(got.Stdout, "text (default), json, vscode or github")) {
			t.Fatal(got)
		}
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}
