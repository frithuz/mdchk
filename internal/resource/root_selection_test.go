//go:build linux || darwin || windows

package resource

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func rootPolicyTemp(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func rootPolicyDir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func rootPolicyFile(t *testing.T, path, content string) string {
	t.Helper()
	rootPolicyDir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func rootPolicyBudget(t *testing.T, limits parser.Limits) *parser.OperationBudget {
	t.Helper()
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	return budget
}

func rootPolicySelect(t *testing.T, options RootSelectionOptions, want string) {
	t.Helper()
	got, err := SelectFilesystemRoot(context.Background(), rootPolicyBudget(t, parser.DefaultLimits()), options)
	if err != nil || got != want {
		t.Fatalf("options=%+v: got %q, %v; want %q", options, got, err, want)
	}
}

func TestFilesystemRootRepositoryAndWorktree(t *testing.T) {
	base := rootPolicyTemp(t)
	repo := rootPolicyDir(t, filepath.Join(base, "repository"))
	rootPolicyDir(t, filepath.Join(repo, ".git"))
	start := rootPolicyDir(t, filepath.Join(repo, "docs", "deep"))
	config := rootPolicyFile(t, filepath.Join(start, "below", "mdchk.yaml"), "not decoded\n")
	rootPolicySelect(t, RootSelectionOptions{Mode: RootCLI, WorkingDirectory: start, SelectedConfig: config}, repo)

	nested := rootPolicyDir(t, filepath.Join(start, "nested"))
	rootPolicyFile(t, filepath.Join(nested, ".git"), "gitdir: ../../outside-is-not-resource-authority\n")
	inside := rootPolicyDir(t, filepath.Join(nested, "more"))
	rootPolicySelect(t, RootSelectionOptions{Mode: RootCLI, WorkingDirectory: inside, SelectedConfig: config}, nested)

	// A regular worktree marker is enough; its bytes must never redirect root
	// selection to its gitdir target or require that the target exists.
	worktree := rootPolicyDir(t, filepath.Join(base, "worktree"))
	rootPolicyFile(t, filepath.Join(worktree, ".git"), "gitdir: /not-a-real-path\n")
	rootPolicySelect(t, RootSelectionOptions{Mode: RootCLI, WorkingDirectory: worktree}, worktree)
}

func TestFilesystemRootNoRepositoryConfigScope(t *testing.T) {
	base := rootPolicyTemp(t)
	start := rootPolicyDir(t, filepath.Join(base, "project", "docs"))
	above := rootPolicyFile(t, filepath.Join(base, "project", "mdchk.yaml"), "version: malformed-on-purpose\n")
	below := rootPolicyFile(t, filepath.Join(start, "nested", "mdchk.yaml"), "invalid config not loaded\n")
	beside := rootPolicyFile(t, filepath.Join(base, "sibling", "mdchk.yaml"), "invalid config not loaded\n")
	same := rootPolicyFile(t, filepath.Join(start, "mdchk.yaml"), "invalid config not loaded\n")
	for _, tc := range []struct {
		name, config, want string
	}{
		{"builtins", "", start},
		{"containing config", above, filepath.Dir(above)},
		{"config below", below, start},
		{"config beside", beside, start},
		{"config same directory", same, start},
		{"irrelevant directory is not reopened", filepath.Join(start, "removed", "mdchk.yaml"), start},
		{"external namespace is not probed", `\\127.0.0.1\never-contact\mdchk.yaml`, start},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rootPolicySelect(t, RootSelectionOptions{Mode: RootCLI, WorkingDirectory: start, SelectedConfig: tc.config}, tc.want)
		})
	}
}

func TestFilesystemRootGitAliasUsesNativeIdentity(t *testing.T) {
	base := rootPolicyTemp(t)
	repo := rootPolicyDir(t, filepath.Join(base, "case-root"))
	rootPolicyDir(t, filepath.Join(repo, ".GIT"))
	start := rootPolicyDir(t, filepath.Join(repo, "docs"))
	marker, err := os.Lstat(filepath.Join(repo, ".git"))
	want := start
	switch {
	case err == nil:
		if !marker.IsDir() {
			t.Fatal("native alias has unexpected type")
		}
		want = repo
	case errors.Is(err, fs.ErrNotExist):
		// On a case-sensitive filesystem .GIT is ordinary data, not a marker.
	default:
		t.Fatal(err)
	}
	rootPolicySelect(t, RootSelectionOptions{Mode: RootCLI, WorkingDirectory: start}, want)
}

func TestFilesystemRootInspectionAndProjectScope(t *testing.T) {
	base := rootPolicyTemp(t)
	caller := rootPolicyDir(t, filepath.Join(base, "caller"))
	rootPolicyDir(t, filepath.Join(caller, ".git"))
	other := rootPolicyDir(t, filepath.Join(base, "other"))
	rootPolicyDir(t, filepath.Join(other, ".git"))
	target := rootPolicyFile(t, filepath.Join(other, "docs", "guide.md"), "# source\n")
	rootPolicySelect(t, RootSelectionOptions{Mode: RootInspect, WorkingDirectory: caller, CanonicalTarget: target}, other)

	// Startup authority wins over both a repository above it and one nested
	// beneath it. Neither a target nor configuration may expand/narrow it.
	narrow := rootPolicyDir(t, filepath.Join(other, "docs"))
	nested := rootPolicyDir(t, filepath.Join(narrow, "nested"))
	rootPolicyDir(t, filepath.Join(nested, ".git"))
	config := rootPolicyFile(t, filepath.Join(caller, "mdchk.yaml"), "not loaded\n")
	rootPolicySelect(t, RootSelectionOptions{Mode: RootProject, StartupRoot: narrow, WorkingDirectory: nested, CanonicalTarget: target, SelectedConfig: config}, narrow)

	// Outside a repository, inspect applies config containment to the target's
	// canonical parent, not to the unrelated caller directory.
	plain := rootPolicyFile(t, filepath.Join(base, "plain", "docs", "readme.md"), "# source\n")
	plainConfig := rootPolicyFile(t, filepath.Join(base, "plain", "mdchk.yaml"), "not loaded\n")
	rootPolicySelect(t, RootSelectionOptions{Mode: RootInspect, WorkingDirectory: caller, CanonicalTarget: plain, SelectedConfig: plainConfig}, filepath.Dir(plainConfig))
}

func TestFilesystemRootRejectsUnprovedMarkerAndCanonicalTarget(t *testing.T) {
	base := rootPolicyTemp(t)
	start := rootPolicyDir(t, filepath.Join(base, "start"))
	outside := rootPolicyDir(t, filepath.Join(base, "outside"))
	marker := filepath.Join(start, ".git")
	if err := os.Symlink(outside, marker); err != nil {
		t.Fatalf("required native marker symlink fixture: %v", err)
	}
	got, err := SelectFilesystemRoot(context.Background(), rootPolicyBudget(t, parser.DefaultLimits()), RootSelectionOptions{Mode: RootCLI, WorkingDirectory: start})
	if got != "" || !(errors.Is(err, ErrContract) || nativePolicyError(err)) {
		t.Fatal("linked marker was accepted or treated as no repository", got, err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "absent"), marker); err != nil {
		t.Fatal(err)
	}
	got, err = SelectFilesystemRoot(context.Background(), rootPolicyBudget(t, parser.DefaultLimits()), RootSelectionOptions{Mode: RootCLI, WorkingDirectory: start})
	if got != "" || !(errors.Is(err, ErrContract) || nativePolicyError(err)) {
		t.Fatal("dangling marker became absence", got, err)
	}

	alias := filepath.Join(base, "alias")
	if err := os.Symlink(outside, alias); err != nil {
		t.Fatal(err)
	}
	// Inspection receives the already canonical acquired target. An alias in
	// its parent is a broken caller precondition, not a request to resolve it.
	got, err = SelectFilesystemRoot(context.Background(), rootPolicyBudget(t, parser.DefaultLimits()), RootSelectionOptions{Mode: RootInspect, CanonicalTarget: filepath.Join(alias, "doc.md")})
	if got != "" || !(errors.Is(err, ErrContract) || nativePolicyError(err)) {
		t.Fatal("inspection silently canonicalized a target alias", got, err)
	}
	// Trusted startup anchors may resolve a host alias. That does not authorize
	// any subsequent below-root resource symlink traversal.
	if runtime.GOOS == "windows" {
		got, err := SelectFilesystemRoot(context.Background(), rootPolicyBudget(t, parser.DefaultLimits()), RootSelectionOptions{Mode: RootProject, StartupRoot: alias})
		if got != "" || !nativePolicyError(err) {
			t.Fatal("Windows trusted anchors must refuse reparses before metadata traversal", got, err)
		}
	} else {
		rootPolicySelect(t, RootSelectionOptions{Mode: RootProject, StartupRoot: alias}, outside)
	}
}

func TestFilesystemRootContractLimitsAndStops(t *testing.T) {
	base := rootPolicyTemp(t)
	for _, options := range []RootSelectionOptions{
		{},
		{Mode: RootMode("standalone"), StartupRoot: base},
		{Mode: RootCLI, WorkingDirectory: "relative"},
		{Mode: RootProject},
		{Mode: RootInspect, CanonicalTarget: "relative.md"},
		{Mode: RootProject, StartupRoot: base, PathLimits: PathLimits{Bytes: 1}},
	} {
		if got, err := SelectFilesystemRoot(context.Background(), rootPolicyBudget(t, parser.DefaultLimits()), options); got != "" || !errors.Is(err, ErrContract) {
			t.Fatal("invalid root contract accepted", options, got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := SelectFilesystemRoot(ctx, rootPolicyBudget(t, parser.DefaultLimits()), RootSelectionOptions{Mode: RootProject, StartupRoot: base}); got != "" || !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation identity lost", got, err)
	}
	if got, err := SelectFilesystemRoot(context.Background(), nil, RootSelectionOptions{Mode: RootProject, StartupRoot: base}); got != "" || !errors.Is(err, ErrContract) {
		t.Fatal("nil budget accepted", got, err)
	}
	//nolint:staticcheck // Deliberately verify the API rejects a nil context.
	if got, err := SelectFilesystemRoot(nil, rootPolicyBudget(t, parser.DefaultLimits()), RootSelectionOptions{Mode: RootProject, StartupRoot: base}); got != "" || !errors.Is(err, ErrContract) {
		t.Fatal("nil context accepted", got, err)
	}

	limits := parser.DefaultLimits()
	limits.WorkUnits = 1
	budget := rootPolicyBudget(t, limits)
	for attempt := 0; attempt < 2; attempt++ {
		got, err := SelectFilesystemRoot(context.Background(), budget, RootSelectionOptions{Mode: RootProject, StartupRoot: base})
		var exceeded *parser.LimitExceeded
		if got != "" || !errors.As(err, &exceeded) || exceeded.Name != "work-units" {
			t.Fatal("shared quota was reset or misclassified", attempt, got, err)
		}
	}
	for _, tc := range []struct {
		limits PathLimits
		name   string
	}{
		{PathLimits{Bytes: int64(len(base) - 1), Depth: 64}, "path-bytes"},
		{PathLimits{Bytes: 4096, Depth: 1}, "path-depth"},
	} {
		got, err := SelectFilesystemRoot(context.Background(), rootPolicyBudget(t, parser.DefaultLimits()), RootSelectionOptions{Mode: RootProject, StartupRoot: base, PathLimits: tc.limits})
		var exceeded *parser.LimitExceeded
		if got != "" || !errors.As(err, &exceeded) || exceeded.Name != tc.name {
			t.Fatal("operator path bound not applied", got, err)
		}
	}
	limits = parser.DefaultLimits()
	limits.DocumentNodes, limits.TotalNodes = 1, 1
	got, err := SelectFilesystemRoot(context.Background(), rootPolicyBudget(t, limits), RootSelectionOptions{Mode: RootCLI, WorkingDirectory: base})
	var exceeded *parser.LimitExceeded
	if got != "" || !errors.As(err, &exceeded) || exceeded.Name != "total-nodes" {
		t.Fatal("metadata construction did not share nodes", got, err)
	}
	got, err = SelectFilesystemRoot(context.Background(), rootPolicyBudget(t, parser.DefaultLimits()), RootSelectionOptions{Mode: RootProject, StartupRoot: filepath.Join(base, "missing")})
	if got != "" || !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("missing anchor lost native error", got, err)
	}
}

func TestFilesystemRootCancellationBeforePublication(t *testing.T) {
	base := rootPolicyTemp(t)
	rootPolicyDir(t, filepath.Join(base, ".git"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// This boundary callback cancels at the first post-pin checkpoint,
	// before native metadata can become selected root authority.
	check := func(work int64) error {
		if work == 0 {
			cancel()
		}
		return ctx.Err()
	}
	found, err := rootRepositoryAt(base, rootPolicyBudget(t, parser.DefaultLimits()), check)
	if found || !errors.Is(err, context.Canceled) || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("a completed probe published authority after cancellation", found, err)
	}
}

func TestFilesystemRootProbeDoesNotHideIO(t *testing.T) {
	missing := &fs.PathError{Op: "metadata", Path: "marker", Err: fs.ErrNotExist}
	failed := errors.Join(missing, &fs.PathError{Op: "close", Path: "marker", Err: fs.ErrPermission})
	if got := rootProbeFailure(failed); !errors.Is(got, fs.ErrPermission) || !errors.Is(got, fs.ErrNotExist) {
		t.Fatal("a joined actual failure became an absence assertion", got)
	}
	if got := rootProbeFailure(missing); got != nil {
		t.Fatal("a successful absence probe invented an I/O failure", got)
	}
}

func TestFilesystemKeyPreservesNamespaceAndOutsideTargets(t *testing.T) {
	base := rootPolicyTemp(t)
	root := filepath.Join(base, "project")
	for _, tc := range []struct {
		path, want string
		inside     bool
	}{
		{filepath.Join(root, "docs", "Guide%20.md"), "docs/Guide%20.md", true},
		{filepath.Join(root, "docs", "é.md"), "docs/é.md", true},
		{filepath.Join(root, "docs", "e\u0301.md"), "docs/e\u0301.md", true},
		{filepath.Join(root, ".GIT", "guide.md"), ".GIT/guide.md", true},
		{filepath.Join(root, ".git", "guide.md"), "", false},
		{filepath.Join(root, "docs", ".git", "guide.md"), "", false},
		{filepath.Join(base, "projected", "guide.md"), "", false},
		{filepath.Join(base, "outside.md"), "", false},
		{root, "", false},
	} {
		key, inside, err := FilesystemKey(root, tc.path)
		if err != nil || inside != tc.inside || key.String() != tc.want {
			t.Fatal("key mapping changed identity/authority", tc.path, key.String(), inside, err)
		}
	}
	for _, tc := range [][2]string{
		{"relative", filepath.Join(root, "doc.md")},
		{root, "relative.md"},
		{root, root + string(filepath.Separator) + ".." + string(filepath.Separator) + "doc.md"},
		{root, filepath.Join(root, "bad\x00.md")},
		{root, filepath.Join(root, "xx\n.md")},
	} {
		key, inside, err := FilesystemKey(tc[0], tc[1])
		if !errors.Is(err, ErrContract) || inside || key != (Key{}) {
			t.Fatal("unproved/nonliteral target became namespace member", tc, key, inside, err)
		}
	}
}
