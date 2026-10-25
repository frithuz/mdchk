package files

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
)

func selectionRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func selectionFile(t *testing.T, root, name string, contents []byte) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func selectFixture(t *testing.T, options Options) *Selection {
	t.Helper()
	selection, err := Select(options)
	if err != nil {
		t.Fatal(err)
	}
	return selection
}

func targetPaths(selection *Selection) []string {
	paths := make([]string, len(selection.Targets))
	for i, target := range selection.Targets {
		paths[i] = target.Path
	}
	return paths
}

func wantTargets(t *testing.T, selection *Selection, paths ...string) {
	t.Helper()
	sort.Strings(paths)
	got := targetPaths(selection)
	if !slices.Equal(got, paths) {
		t.Fatalf("selected paths = %q, want %q", got, paths)
	}
	if selection.Empty() != (len(paths) == 0) {
		t.Fatalf("Empty() = %v with %d targets", selection.Empty(), len(paths))
	}
}

func wantSkip(t *testing.T, selection *Selection, path string, reason SkipReason) {
	t.Helper()
	for _, skipped := range selection.Skipped {
		if skipped.Path == path && skipped.Reason == reason {
			return
		}
	}
	t.Errorf("missing skip for %q with reason %v; got %+v", path, reason, selection.Skipped)
}

func selectionPatterns(t *testing.T, patterns ...string) *config.Patterns {
	t.Helper()
	compiled, err := config.CompilePatterns(patterns)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func TestSelectDirectoryEligibilityAndSourcePreservation(t *testing.T) {
	root := selectionRoot(t)
	contents := []byte{'#', ' ', 0xff, '\r', '\n', 0, '\n'}
	names := []string{"README.MD", "nested/guide.Markdown", ".hidden.md", ".github/guide.md", "nested/article.data", "notes.txt", "nested/noextension", "lookalike.marKdown", ".git/private.md"}
	paths := make(map[string]string)
	for _, name := range names {
		paths[name] = selectionFile(t, root, name, contents)
	}
	selectionFile(t, root, ".gitignore", []byte(".hidden.md\nnested/\n"))
	selection := selectFixture(t, Options{WorkingDirectory: root})
	wantTargets(t, selection, paths["README.MD"], paths["nested/guide.Markdown"], paths[".hidden.md"], paths[".github/guide.md"])
	wantSkip(t, selection, filepath.Join(root, ".git"), SkipGit)
	for _, name := range []string{"nested/article.data", "notes.txt", "lookalike.marKdown"} {
		wantSkip(t, selection, paths[name], SkipExtension)
	}
	if len(selection.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", selection.Errors)
	}
	for _, path := range paths {
		got, err := os.ReadFile(path) //nolint:gosec // Read only test-owned fixture paths to prove selection preserved bytes.
		if err != nil || !bytes.Equal(got, contents) {
			t.Fatalf("source changed at %s: bytes %q, error %v", path, got, err)
		}
	}
}

func TestSelectExtensionFoldingDoesNotFoldExclusions(t *testing.T) {
	root := selectionRoot(t)
	upper := selectionFile(t, root, "README.MD", nil)
	lower := selectionFile(t, root, "guide.md", nil)
	selection := selectFixture(t, Options{WorkingDirectory: root, Exclusions: selectionPatterns(t, "**/*.md")})
	wantTargets(t, selection, upper)
	wantSkip(t, selection, lower, SkipExcluded)
}

func TestSelectExplicitFilesOverlapAndDeterminism(t *testing.T) {
	root := selectionRoot(t)
	md := selectionFile(t, root, "nested/a.md", nil)
	other := selectionFile(t, root, "nested/custom", nil)
	qmd := selectionFile(t, root, "notes.custom", nil)
	inputs := [][]string{
		{".", "nested/custom", md, "nested/./a.md", "notes.custom"},
		{"notes.custom", "nested/./a.md", md, "nested/custom", "."},
		{root, other, qmd, filepath.Join(root, "nested"), md},
	}
	var previous *Selection
	for _, paths := range inputs {
		selection := selectFixture(t, Options{WorkingDirectory: root, Paths: paths})
		wantTargets(t, selection, md, other, qmd)
		for _, skip := range selection.Skipped {
			if skip.Path == other || skip.Path == qmd {
				t.Errorf("explicit selected file retained scan skip: %+v", skip)
			}
		}
		if previous != nil {
			wantSameSelection(t, selection, previous)
		}
		previous = selection
	}
}

func TestSelectExclusionsPruneExplicitAncestors(t *testing.T) {
	for _, pattern := range []string{"generated", "generated/", "generated/**", "**/generated/"} {
		t.Run(pattern, func(t *testing.T) {
			root := selectionRoot(t)
			keep := selectionFile(t, root, "keep.md", nil)
			child := selectionFile(t, root, "generated/deep/a.md", nil)
			selection := selectFixture(t, Options{WorkingDirectory: root, Paths: []string{child, ".", "generated/deep"}, Exclusions: selectionPatterns(t, pattern)})
			wantTargets(t, selection, keep)
			wantSkip(t, selection, filepath.Join(root, "generated"), SkipExcluded)
			for _, skip := range selection.Skipped {
				if skip.Path == child {
					t.Errorf("reported child of pruned directory: %+v", skip)
				}
			}
		})
	}
}

func TestSelectConfigurationBaseAndOutsideTargets(t *testing.T) {
	root := selectionRoot(t)
	base := filepath.Join(root, "project")
	nested := filepath.Join(base, "docs")
	blocked := selectionFile(t, root, "project/docs/generated/a.md", nil)
	keep := selectionFile(t, root, "project/docs/keep.md", nil)
	sibling := selectionFile(t, root, "project/other.md", nil)
	outside := selectionFile(t, root, "outside/generated/a.md", nil)
	selection := selectFixture(t, Options{WorkingDirectory: nested, ExclusionBase: base, Paths: []string{".", blocked, sibling, outside}, Exclusions: selectionPatterns(t, "docs/generated/**", "outside/**")})
	wantTargets(t, selection, keep, sibling, outside)
	wantSkip(t, selection, filepath.Join(nested, "generated"), SkipExcluded)
	if len(selection.Errors) != 0 {
		t.Fatalf("outside targets produced errors: %v", selection.Errors)
	}
}

func TestSelectPatternEscapesAndNativePaths(t *testing.T) {
	root := selectionRoot(t)
	keep := selectionFile(t, root, "nested/keep.md", nil)
	literal := selectionFile(t, root, "[draft].md", nil)
	selection := selectFixture(t, Options{WorkingDirectory: root, Paths: []string{filepath.Join("nested", "keep.md"), literal}, Exclusions: selectionPatterns(t, `\[draft\].md`)})
	wantTargets(t, selection, keep)
	wantSkip(t, selection, literal, SkipExcluded)
	if runtime.GOOS == "windows" {
		return // Backslash is a native separator here, covered by filepath.Join above.
	}
	backslash := selectionFile(t, root, `nested\literal.md`, nil)
	selection = selectFixture(t, Options{WorkingDirectory: root, Exclusions: selectionPatterns(t, "nested/**", `\[draft\].md`)})
	wantTargets(t, selection, backslash)
	selection = selectFixture(t, Options{WorkingDirectory: root, Paths: []string{backslash}, Exclusions: selectionPatterns(t, `nested\\literal.md`)})
	wantTargets(t, selection)
	wantSkip(t, selection, backslash, SkipExcluded)
}

func TestSelectSymlinksAndGitAncestorsCannotBeBypassed(t *testing.T) {
	root := selectionRoot(t)
	keep := selectionFile(t, root, "keep.md", nil)
	real := selectionFile(t, root, "real/a.md", nil)
	git := selectionFile(t, root, ".git/deep/a.md", nil)
	links := map[string]string{"file.md": real, "linked": filepath.Join(root, "real"), "dangling.md": filepath.Join(root, "missing")}
	for name, destination := range links {
		if err := os.Symlink(destination, filepath.Join(root, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	selection := selectFixture(t, Options{WorkingDirectory: root, Paths: []string{".", "file.md", "linked/a.md", "dangling.md", git}})
	wantTargets(t, selection, keep, real)
	for name := range links {
		wantSkip(t, selection, filepath.Join(root, name), SkipSymlink)
	}
	wantSkip(t, selection, filepath.Join(root, ".git"), SkipGit)
	if len(selection.Errors) != 0 {
		t.Fatalf("symlink skips became errors: %v", selection.Errors)
	}
	// filepath.Join would erase the component whose symlink must be observed.
	raw := "linked" + string(filepath.Separator) + ".." + string(filepath.Separator) + "keep.md"
	selection = selectFixture(t, Options{WorkingDirectory: root, Paths: []string{raw}})
	wantTargets(t, selection)
	wantSkip(t, selection, filepath.Join(root, "linked"), SkipSymlink)
}

func TestSelectDistinctHardLinkNames(t *testing.T) {
	root := selectionRoot(t)
	original := selectionFile(t, root, "first.md", []byte("# Shared inode\n"))
	alias := filepath.Join(root, "second.md")
	if err := os.Link(original, alias); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	selection := selectFixture(t, Options{WorkingDirectory: root, Paths: []string{alias, original, "."}})
	wantTargets(t, selection, original, alias)
}

func TestSelectEmptyAndIndependentMissingTargets(t *testing.T) {
	root := selectionRoot(t)
	file := selectionFile(t, root, "keep.md", nil)
	missing := filepath.Join(root, "missing.md")
	selection := selectFixture(t, Options{WorkingDirectory: root, Paths: []string{missing, file}})
	wantTargets(t, selection, file)
	if len(selection.Errors) != 1 || selection.Errors[0].Path != missing || !errors.Is(selection.Errors[0], fs.ErrNotExist) {
		t.Fatalf("missing target errors = %v", selection.Errors)
	}
	selection = selectFixture(t, Options{WorkingDirectory: root, Paths: []string{missing}})
	wantTargets(t, selection)
	if len(selection.Errors) != 1 || !errors.Is(selection.Errors[0], fs.ErrNotExist) {
		t.Fatalf("empty selection lost file error: %v", selection.Errors)
	}
	selection = selectFixture(t, Options{WorkingDirectory: root, Exclusions: selectionPatterns(t, "**/*.md")})
	wantTargets(t, selection)
	wantSkip(t, selection, file, SkipExcluded)
	if len(selection.Errors) != 0 {
		t.Fatalf("all-excluded selection has errors: %v", selection.Errors)
	}
}

func TestSelectDirectoryOnlyPatternsAndMatchingDirectoryNames(t *testing.T) {
	root := selectionRoot(t)
	regular := selectionFile(t, root, "generated", nil)
	child := selectionFile(t, root, "archive.md/deep/child.md", nil)
	selection := selectFixture(t, Options{WorkingDirectory: root, Paths: []string{regular, child, "."}, Exclusions: selectionPatterns(t, "generated/", "*.md")})
	wantTargets(t, selection, regular)
	wantSkip(t, selection, filepath.Join(root, "archive.md"), SkipExcluded)
	if len(selection.Errors) != 0 {
		t.Fatalf("directory-only selection: %v", selection.Errors)
	}
}

func TestSelectWindowsRootedAndDriveRelativePaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows native path semantics")
	}
	root := selectionRoot(t)
	target := selectionFile(t, root, "nested/guide.md", nil)
	drive := filepath.VolumeName(root)
	rooted := target[len(drive):]
	for _, path := range []string{rooted, drive + `nested\guide.md`, filepath.ToSlash(rooted)} {
		got := selectFixture(t, Options{WorkingDirectory: root, Paths: []string{path}})
		wantTargets(t, got, target)
		if len(got.Errors) != 0 {
			t.Fatalf("native path %q: %v", path, got.Errors)
		}
	}
}

// Enumeration can update access timestamps in opaque FileInfo system data.
// Compare observable outcomes and the stable identities acquisition relies on.
func wantSameSelection(t *testing.T, got, want *Selection) {
	t.Helper()
	if !slices.Equal(targetPaths(got), targetPaths(want)) || !reflect.DeepEqual(got.Skipped, want.Skipped) || !reflect.DeepEqual(got.Errors, want.Errors) {
		t.Fatalf("selection outcomes changed: %+v vs %+v", got, want)
	}
	for i, target := range got.Targets {
		previous := want.Targets[i]
		if len(target.identities) != len(previous.identities) {
			t.Fatalf("identity chain length changed for %q", target.Path)
		}
		for j, part := range target.identities {
			prior := previous.identities[j]
			if part.path != prior.path || !os.SameFile(part.info, prior.info) {
				t.Fatalf("identity chain changed for %q at %q", target.Path, part.path)
			}
		}
	}
}
