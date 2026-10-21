package files

import (
	"errors"
	"github.com/frithuz/mdchk/internal/config"
	"io/fs"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type faultFS struct {
	hostFS
	statErrors, dirErrors map[string]error
	stats, reads          []string
	reverse               bool
}

func (f *faultFS) lstat(p string) (fs.FileInfo, error) {
	f.stats = append(f.stats, p)
	if err := f.statErrors[p]; err != nil {
		return nil, err
	}
	return f.hostFS.lstat(p)
}
func (f *faultFS) readDir(p string) ([]fs.DirEntry, error) {
	f.reads = append(f.reads, p)
	if err := f.dirErrors[p]; err != nil {
		return nil, err
	}
	entries, err := f.hostFS.readDir(p)
	if f.reverse {
		slices.Reverse(entries)
	}
	return entries, err
}

func TestSelectionFaultsRetainIndependentTargetsAndCauses(t *testing.T) {
	root := selectionRoot(t)
	good := selectionFile(t, root, "ok.md", nil)
	denied := selectionFile(t, root, "denied.md", nil)
	folderChild := selectionFile(t, root, "unreadable/hidden.md", nil)
	folder := filepath.Dir(folderChild)
	access := &faultFS{statErrors: map[string]error{denied: fs.ErrPermission}, dirErrors: map[string]error{folder: fs.ErrPermission}}
	got, err := selectFS(Options{WorkingDirectory: root, Paths: []string{".", denied, folder, good}}, access)
	if err != nil {
		t.Fatal(err)
	}
	wantTargets(t, got, good)
	if len(got.Errors) != 2 {
		t.Fatalf("errors=%v", got.Errors)
	}
	for i, path := range []string{denied, folder} {
		failure := got.Errors[i]
		if failure.Path != path || !errors.Is(failure, fs.ErrPermission) {
			t.Fatalf("error=%v want permission at %q", failure, path)
		}
	}
	if got.Errors[0].Op != "lstat" || got.Errors[1].Op != "readdir" {
		t.Fatalf("lost effect operations: %v", got.Errors)
	}
	if len(got.Skipped) != 0 {
		t.Fatalf("failures reported as intentional skips: %v", got.Skipped)
	}
	if slices.Contains(access.stats, folderChild) {
		t.Fatal("invented inaccessible child")
	}
	only, err := selectFS(Options{WorkingDirectory: root, Paths: []string{folder}}, access)
	if err != nil || !only.Empty() || len(only.Errors) != 1 {
		t.Fatalf("empty lost traversal error: %v %v", only, err)
	}
}

func TestSelectionPrunesBeforeDirectoryEnumerationOrChildAccess(t *testing.T) {
	for _, pattern := range []string{"generated/", "generated/**", "**"} {
		t.Run(pattern, func(t *testing.T) {
			root := selectionRoot(t)
			child := selectionFile(t, root, "generated/deep/never.md", nil)
			pruned := filepath.Join(root, "generated")
			if pattern == "**" {
				pruned = root
			}
			access := &faultFS{dirErrors: map[string]error{pruned: errors.New("must not enumerate")}}
			got, err := selectFS(Options{WorkingDirectory: root, Paths: []string{".", child, "generated/deep"}, Exclusions: selectionPatterns(t, pattern)}, access)
			if err != nil {
				t.Fatal(err)
			}
			wantTargets(t, got)
			wantSkip(t, got, pruned, SkipExcluded)
			if slices.Contains(access.reads, pruned) || slices.Contains(access.stats, child) || len(got.Errors) != 0 {
				t.Fatalf("accessed pruned subtree: reads %v stats %v errors %v", access.reads, access.stats, got.Errors)
			}
			if len(got.Skipped) != 1 {
				t.Fatalf("invented skipped descendants: %v", got.Skipped)
			}
		})
	}
}

func TestSelectionEnumerationOrderAndRepeatedAliases(t *testing.T) {
	root := selectionRoot(t)
	for _, name := range []string{"z.md", "a/b.MD", "a/c.md", "other.txt", "empty/d.txt"} {
		selectionFile(t, root, name, nil)
	}
	var previous *Selection
	for _, reverse := range []bool{true, false, true} {
		access := &faultFS{reverse: reverse}
		got, err := selectFS(Options{WorkingDirectory: root, Paths: []string{".", root, "a/../.", "a"}}, access)
		if err != nil {
			t.Fatal(err)
		}
		if previous != nil && !reflect.DeepEqual(got, previous) {
			t.Fatalf("unstable enumeration: %+v vs %+v", previous, got)
		}
		previous = got
	}
}

func TestSelectionMatchingFailureIsRetained(t *testing.T) {
	root := selectionRoot(t)
	good := selectionFile(t, root, "good.md", nil)
	// The matcher rejects a normalized name longer than its explicit bound.
	// Inject a virtual deep entry rather than relying on OS-specific path limits.
	access := &longNameFS{hostFS: hostFS{}, root: root}
	got, err := selectFS(Options{WorkingDirectory: root, Paths: []string{good, "."}}, access)
	if err != nil {
		t.Fatal(err)
	}
	wantTargets(t, got, good)
	if len(got.Errors) != 1 || got.Errors[0].Op != "exclude" {
		t.Fatalf("matcher failure lost: %v", got.Errors)
	}
}

type longNameFS struct {
	hostFS
	root string
}

func (f *longNameFS) readDir(p string) ([]fs.DirEntry, error) {
	entries, err := f.hostFS.readDir(p)
	if p == f.root {
		info, statErr := f.hostFS.lstat(filepath.Join(f.root, "good.md"))
		if statErr != nil {
			return nil, statErr
		}
		entries = append(entries, fs.FileInfoToDirEntry(renamedInfo{FileInfo: info, name: strings.Repeat("x", config.MaxMatchNameBytes+1)}))
	}
	return entries, err
}
func (f *longNameFS) lstat(p string) (fs.FileInfo, error) {
	if len(filepath.Base(p)) > config.MaxMatchNameBytes {
		return f.hostFS.lstat(filepath.Join(f.root, "good.md"))
	}
	return f.hostFS.lstat(p)
}

type renamedInfo struct {
	fs.FileInfo
	name string
}

func (i renamedInfo) Name() string { return i.name }
