package main

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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

func TestCompiledOrdinaryUnsupportedApply(t *testing.T) {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		t.Skip("ordinary Darwin arm64 publication has its own native suite; no-cgo fallback is separately exercised")
	}
	binary := buildConfigurationExecutable(t, "")
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		t.Run(strings.ReplaceAll(strings.ReplaceAll(ending, "\r", "CR"), "\n", "LF"), func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
			name := filepath.Join("space ünicode", "δ document.md")
			putConfigurationFile(t, root, name, "first"+ending+"last")
			verify := unchangedFixFixture(t, root)
			for _, format := range []string{"json", "text"} {
				got := runConfigurationProcess(t, binary, root, "fix", filepath.Join(".", name), "--format="+format)
				if got.Status != 13 || !strings.Contains(got.Stdout, "unsupported") {
					t.Fatal(got)
				}
				if format == "json" {
					r := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
					if len(r.Changes) != 0 || len(r.Errors) == 0 {
						t.Fatal(r)
					}
					for _, f := range r.Files {
						if f.WriteStatus == "written" {
							t.Fatal(f)
						}
					}
				}
				preview := runFixConfiguration(t, binary, root, "fix", "--dry-run", name, "--format="+format)
				if preview.Status != 0 {
					t.Fatal(preview)
				}
				if format == "json" {
					if len(preview.Result.Changes) != 1 {
						t.Fatal(preview)
					}
					// Independently predicted ending, without depending on the diff renderer.
					want := "first" + ending + "last" + ending
					if preview.Result.Changes[0].Candidate.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(want))) {
						t.Fatal(preview)
					}
				}
				verify()
			}
		})
	}
}

func TestCompiledFixHelpWithoutProject(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := t.TempDir()
	for _, args := range [][]string{{"--help"}, {"fix", "--help"}, {"--help", "fix"}} {
		got := runConfigurationProcess(t, binary, root, args...)
		if got.Status != 0 || !strings.Contains(got.Stdout, "--dry-run") {
			t.Fatal(got)
		}
		if len(args) > 1 && (!strings.Contains(got.Stdout, "saved content") || !strings.Contains(got.Stdout, "candidate content") || !strings.Contains(got.Stdout, "text (default) or json")) {
			t.Fatal(got)
		}
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}
