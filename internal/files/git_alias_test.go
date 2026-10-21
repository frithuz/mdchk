package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// aliasFS models a case-insensitive metadata entry using real FileInfo identity.
// Only the metadata component is aliased; unrelated filenames remain native.
type aliasFS struct {
	hostFS
	root, marker string
	stats, reads []string
	markerError  error
}

func (a *aliasFS) native(path string) string {
	rel, err := filepath.Rel(a.root, path)
	if err != nil {
		return path
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) > 0 && strings.EqualFold(parts[0], ".git") {
		parts[0] = a.marker
		return filepath.Join(a.root, filepath.Join(parts...))
	}
	return path
}
func (a *aliasFS) lstat(path string) (fs.FileInfo, error) {
	a.stats = append(a.stats, path)
	if path == filepath.Join(a.root, ".git") && a.markerError != nil {
		return nil, a.markerError
	}
	return a.hostFS.lstat(a.native(path))
}
func (a *aliasFS) readDir(path string) ([]fs.DirEntry, error) {
	a.reads = append(a.reads, path)
	return a.hostFS.readDir(a.native(path))
}
func (a *aliasFS) assertNoMetadataChildren(t *testing.T) {
	t.Helper()
	for _, path := range a.reads {
		if strings.EqualFold(filepath.Base(path), ".git") {
			t.Errorf("enumerated metadata directory %q", path)
		}
	}
	for _, path := range a.stats {
		if strings.Contains(strings.ToLower(path), string(filepath.Separator)+".git"+string(filepath.Separator)) {
			t.Errorf("probed metadata child %q", path)
		}
	}
}

func TestSelectionSkipsNativeGitCaseAliases(t *testing.T) {
	for _, stored := range []string{".git", ".GIT"} {
		t.Run(stored, func(t *testing.T) {
			root := selectionRoot(t)
			keep := selectionFile(t, root, "keep.md", nil)
			selectionFile(t, root, stored+"/deep/cache.md", nil)
			selectionFile(t, root, stored+"/config", nil)
			access := &aliasFS{root: root, marker: stored}
			got, err := selectFS(Options{WorkingDirectory: root, Paths: []string{".", ".GIT/config", ".gIt/deep/cache.md", ".Git"}}, access)
			if err != nil {
				t.Fatal(err)
			}
			wantTargets(t, got, keep)
			for _, spelling := range []string{stored, ".GIT", ".gIt", ".Git"} {
				wantSkip(t, got, filepath.Join(root, spelling), SkipGit)
			}
			if len(got.Errors) != 0 {
				t.Fatalf("metadata aliases became errors: %v", got.Errors)
			}
			access.assertNoMetadataChildren(t)
		})
	}
}

func TestGitAliasIdentityFailureIsRetained(t *testing.T) {
	root := selectionRoot(t)
	selectionFile(t, root, ".GIT/secret.md", nil)
	keep := selectionFile(t, root, "keep.md", nil)
	access := &aliasFS{root: root, marker: ".GIT", markerError: fs.ErrPermission}
	got, err := selectFS(Options{WorkingDirectory: root}, access)
	if err != nil {
		t.Fatal(err)
	}
	wantTargets(t, got, keep)
	if len(got.Errors) != 1 || got.Errors[0].Path != filepath.Join(root, ".git") || !errors.Is(got.Errors[0], fs.ErrPermission) {
		t.Fatalf("lost metadata identity failure: %v", got.Errors)
	}
	access.assertNoMetadataChildren(t)
}

func TestGitMetadataCaseBehaviorOnNativeFilesystem(t *testing.T) {
	root := selectionRoot(t)
	marker := filepath.Join(root, ".git")
	config := selectionFile(t, root, ".git/config", nil)
	markerInfo, err := os.Lstat(marker)
	if err != nil {
		t.Fatal(err)
	}
	upper := filepath.Join(root, ".GIT")
	upperInfo, upperErr := os.Lstat(upper)
	insensitive := upperErr == nil && os.SameFile(markerInfo, upperInfo)
	if insensitive {
		for _, spelling := range []string{".GIT/config", ".gIt/config"} {
			got := selectFixture(t, Options{WorkingDirectory: root, Paths: []string{spelling}})
			wantTargets(t, got)
			wantSkip(t, got, filepath.Dir(filepath.Join(root, spelling)), SkipGit)
			if len(got.Errors) != 0 {
				t.Fatalf("native alias failure: %v", got.Errors)
			}
		}
		return
	}
	// A distinct .GIT directory on a case-sensitive volume is ordinary content.
	distinct := selectionFile(t, root, ".GIT/guide.md", nil)
	got := selectFixture(t, Options{WorkingDirectory: root, Paths: []string{".", ".GIT/guide.md", config}})
	wantTargets(t, got, distinct)
	wantSkip(t, got, marker, SkipGit)
	if len(got.Errors) != 0 {
		t.Fatalf("distinct-case content failure: %v", got.Errors)
	}
}
