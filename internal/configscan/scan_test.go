package configscan

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/parser"
)

func temp(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func fixture(t *testing.T, root, name, data string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	r, e := os.OpenRoot(root)
	if e != nil {
		t.Fatal(e)
	}
	defer closeHandle(r)
	if e = r.WriteFile(filepath.FromSlash(name), []byte(data), 0600); e != nil {
		t.Fatal(e)
	}
}
func environment(t *testing.T) map[string]string {
	t.Helper()
	home := temp(t)
	return map[string]string{"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, "xdg"), "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": filepath.Join(home, "global")}
}
func names(root string, r Result) []string {
	out := []string{}
	for _, d := range r.Definitions {
		p, _ := filepath.Rel(root, d.Path)
		out = append(out, filepath.ToSlash(p))
	}
	return out
}
func TestScanPruningNestedTrackedAndReadOnly(t *testing.T) {
	root := temp(t)
	env := environment(t)
	fixture(t, root, ".git/config", "[core]\nignoreCase = false\n")
	fixture(t, root, ".mdchk.yaml", "version: 1\n")
	fixture(t, root, ".gitignore", "vendor/\n*.yaml\n!keep/.mdchk.yaml\n")
	fixture(t, root, "vendor/.gitignore", "!child/\n")
	fixture(t, root, "vendor/child/.mdchk.yaml", "invalid yaml")
	fixture(t, root, "keep/.mdchk.yaml", "version: 1\r\n")
	fixture(t, root, "other/.mdchk.yaml", "version: 1\n")
	before := snapshot(t, root)
	r := Scan(context.Background(), Options{Root: root, Environment: env}, Hooks{})
	if !r.Complete || len(r.Errors) > 0 || !reflect.DeepEqual(names(root, r), []string{"keep/.mdchk.yaml"}) {
		t.Fatalf("%+v names=%v", r, names(root, r))
	}
	if !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("scan wrote fixture")
	}
	found := false
	for _, skip := range r.Skipped {
		if skip.Path == filepath.Join(root, "vendor") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing pruned directory record")
	}
}
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	e := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type().IsRegular() {
			r, e := os.OpenRoot(root)
			if e != nil {
				return e
			}
			rel, e := filepath.Rel(root, p)
			if e != nil {
				closeHandle(r)
				return e
			}
			b, e := r.ReadFile(rel)
			closeHandle(r)
			if e != nil {
				return e
			}
			out[p] = string(b)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func TestBoundariesAndPolicySeam(t *testing.T) {
	outer := temp(t)
	root := filepath.Join(outer, "project")
	fixture(t, outer, ".mdchk.yaml", "hostile invalid")
	fixture(t, root, ".mdchk.yaml", "root bytes")
	fixture(t, root, "child/.mdchk.yaml", "child bytes")
	fixture(t, root, "blocked/.mdchk.yaml", "never read")
	calls := []string{}
	r := Scan(context.Background(), Options{Root: root, Environment: environment(t)}, Hooks{Prune: func(path string, _ config.PolicyState) bool { return filepath.Base(path) == "blocked" }, Definition: func(d Definition, s config.PolicyState) (config.PolicyState, error) {
		calls = append(calls, d.Path)
		if strings.Contains(string(d.Data), "root") {
			return s, errors.New("invalid definition")
		}
		return s, nil
	}})
	if !r.Complete || len(r.Errors) != 1 || len(calls) != 2 || len(r.Definitions) != 2 {
		t.Fatalf("%+v calls%v", r, calls)
	}
}

type failureAccess struct {
	scanAccess
	path      string
	enumerate bool
}

func (a failureAccess) open(path string, dir bool) (readHandle, error) {
	if path == a.path && dir == a.enumerate {
		return nil, fs.ErrPermission
	}
	return a.scanAccess.open(path, dir)
}
func TestFailuresContinueSiblingsAndConservativeIgnore(t *testing.T) {
	root := temp(t)
	for _, name := range []string{"a", "b", "c"} {
		fixture(t, root, name+"/.mdchk.yaml", name)
	}
	fixture(t, root, "a/.gitignore", "# source")
	for _, tc := range []struct {
		path string
		dir  bool
	}{{"a/.gitignore", false}, {"a", true}} {
		t.Run(tc.path, func(t *testing.T) {
			r := scan(context.Background(), Options{Root: root, Environment: environment(t)}, Hooks{}, func(a scanAccess) scanAccess { return failureAccess{a, filepath.Join(root, tc.path), tc.dir} })
			if r.Complete || len(r.Errors) != 1 || !errors.Is(r.Errors[0], fs.ErrPermission) {
				t.Fatalf("%+v", r)
			}
			if !strings.Contains(strings.Join(names(root, r), ","), "b/.mdchk.yaml") {
				t.Fatal("sibling lost")
			}
			if tc.path == "a/.gitignore" && strings.Contains(strings.Join(names(root, r), ","), "a/.mdchk.yaml") {
				t.Fatal("failed policy read treated as empty")
			}
		})
	}
}
func TestExactBudgetsCancellationAndRetention(t *testing.T) {
	root := temp(t)
	fixture(t, root, ".mdchk.yaml", "1234")
	limits := DefaultLimits()
	limits.SourceBytes = 4
	limits.TotalSourceBytes = 4
	r := Scan(context.Background(), Options{Root: root, Environment: environment(t), Limits: limits}, Hooks{})
	if !r.Complete {
		t.Fatalf("exact %+v", r)
	}
	limits.SourceBytes = 3
	limits.TotalSourceBytes = 3
	r = Scan(context.Background(), Options{Root: root, Environment: environment(t), Limits: limits}, Hooks{})
	var exceeded *parser.LimitExceeded
	if !errors.As(r.Interruption, &exceeded) || exceeded.Name != "config-bytes" {
		t.Fatalf("one over %+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	effects := 0
	r = scan(ctx, Options{Root: root}, Hooks{}, func(a scanAccess) scanAccess { effects++; return a })
	if !errors.Is(r.Interruption, context.Canceled) || effects != 0 {
		t.Fatalf("precancel %+v effects%d", r, effects)
	}
	fixture(t, root, "a/.mdchk.yaml", "a")
	fixture(t, root, "b/.mdchk.yaml", "b")
	limits = DefaultLimits()
	limits.Entries = 1
	r = Scan(context.Background(), Options{Root: root, Environment: environment(t), Limits: limits}, Hooks{})
	if !errors.As(r.Interruption, &exceeded) || exceeded.Name != "directory-entries" {
		t.Fatalf("entries %+v", r)
	}
	limits = DefaultLimits()
	limits.Depth = 1
	r = Scan(context.Background(), Options{Root: root, Environment: environment(t), Limits: limits}, Hooks{})
	if !errors.As(r.Interruption, &exceeded) || exceeded.Name != "path-depth" {
		t.Fatalf("depth %+v", r)
	}
}
func git(t *testing.T, root string, env map[string]string, args ...string) string {
	t.Helper()
	binary, e := exec.LookPath("git")
	if e != nil {
		t.Skip("native Git development oracle unavailable")
	}
	cmd := exec.Command(binary, args...) //nolint:gosec // Native Git is a development-only oracle with controlled arguments.
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v %s", args, e, out)
	}
	return string(out)
}
func TestNativeGitSourcePrecedenceSubrootAndTracked(t *testing.T) {
	root := temp(t)
	env := environment(t)
	git(t, root, env, "init", "-q")
	fixture(t, root, "sub/.mdchk.yaml", "version: 1\n")
	git(t, root, env, "add", "sub/.mdchk.yaml")
	user := filepath.Join(env["HOME"], "ignore")
	fixture(t, env["HOME"], "ignore", "/sub/.mdchk.yaml\n")
	fixture(t, env["HOME"], "global", "[core]\nexcludesFile = "+user+"\n")
	fixture(t, root, ".git/info/exclude", "!/sub/.mdchk.yaml\n")
	fixture(t, root, ".gitignore", "/sub/.mdchk.yaml\n")
	git(t, root, env, "check-ignore", "--no-index", "-q", "sub/.mdchk.yaml")
	r := Scan(context.Background(), Options{Root: filepath.Join(root, "sub"), Environment: env}, Hooks{})
	if !r.Complete || len(r.Definitions) != 0 {
		t.Fatalf("%+v", r)
	}
	fixture(t, root, "sub/.gitignore", "!.mdchk.yaml\n")
	r = Scan(context.Background(), Options{Root: filepath.Join(root, "sub"), Environment: env}, Hooks{})
	if !r.Complete || len(r.Definitions) != 1 {
		t.Fatalf("nested override %+v", r)
	}
	fixture(t, root, ".gitignore", "sub/\n")
	r = Scan(context.Background(), Options{Root: filepath.Join(root, "sub"), Environment: env}, Hooks{})
	if !r.Complete || len(r.Definitions) != 0 || len(r.Skipped) != 1 {
		t.Fatalf("pruning %+v", r)
	}
}
func TestWorktreeCommonDirAndUnsupportedConfiguration(t *testing.T) {
	root := temp(t)
	metadata := temp(t)
	fixture(t, root, ".git", "gitdir: "+metadata+"\n")
	fixture(t, metadata, "commondir", "common\n")
	fixture(t, metadata, "common/config", "[core]\nignoreCase = true\n")
	fixture(t, metadata, "common/info/exclude", "UPPER/\n")
	fixture(t, root, "upper/.mdchk.yaml", "ignored")
	fixture(t, root, ".mdchk.yaml", "ok")
	r := Scan(context.Background(), Options{Root: root, Environment: environment(t)}, Hooks{})
	if !r.Complete || len(r.Definitions) != 1 {
		t.Fatalf("worktree %+v", r)
	}
	fixture(t, metadata, "common/config", "[include]\npath = arbitrary\n")
	r = Scan(context.Background(), Options{Root: root, Environment: environment(t)}, Hooks{})
	if r.Complete || len(r.Errors) != 1 {
		t.Fatalf("include %+v", r)
	}
}

func TestRemainingBoundsAndErrorRetention(t *testing.T) {
	root := temp(t)
	fixture(t, root, ".mdchk.yaml", "1234")
	fixture(t, root, ".gitignore", "#123\n")
	fixture(t, root, "nested/longname/.mdchk.yaml", "data")
	for _, tc := range []struct {
		name string
		edit func(*Options)
	}{
		{"total-config-bytes", func(o *Options) { o.Limits.SourceBytes = 5; o.Limits.TotalSourceBytes = 8 }},
		{"path-bytes", func(o *Options) { o.Limits.PathBytes = 5 }},
		{"work-units", func(o *Options) {
			l := parser.DefaultLimits()
			l.WorkUnits = 1
			o.Budget, _ = parser.NewOperationBudget(l)
		}},
		{"operation-time", func(o *Options) {
			l := parser.DefaultLimits()
			l.OperationTime = 1
			o.Budget, _ = parser.NewOperationBudget(l)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := Options{Root: root, Environment: environment(t), Limits: DefaultLimits()}
			tc.edit(&o)
			r := Scan(context.Background(), o, Hooks{})
			var limit *parser.LimitExceeded
			if !errors.As(r.Interruption, &limit) || limit.Name != tc.name {
				t.Fatalf("%+v", r)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := Scan(ctx, Options{Root: root, Environment: environment(t)}, Hooks{Definition: func(_ Definition, s config.PolicyState) (config.PolicyState, error) {
		cancel()
		return s, fs.ErrPermission
	}})
	if len(r.Errors) != 1 || !errors.Is(r.Interruption, context.Canceled) {
		t.Fatalf("retention %+v", r)
	}
}
func TestSymlinksNeverFollowedAndNoRuntimeGit(t *testing.T) {
	root := temp(t)
	outside := temp(t)
	fixture(t, outside, ".mdchk.yaml", "outside")
	fixture(t, root, ".mdchk.yaml", "inside")
	if e := os.Symlink(outside, filepath.Join(root, "linked")); e != nil {
		t.Skip(e)
	}
	t.Setenv("PATH", "")
	r := Scan(context.Background(), Options{Root: root, Environment: environment(t)}, Hooks{})
	if !r.Complete || len(r.Definitions) != 1 || len(r.Skipped) != 1 || r.Skipped[0].Reason != "symlink" {
		t.Fatalf("%+v", r)
	}
	if e := os.Symlink(filepath.Join(outside, ".mdchk.yaml"), filepath.Join(root, ".gitignore")); e != nil {
		t.Fatal(e)
	}
	r = Scan(context.Background(), Options{Root: root, Environment: environment(t)}, Hooks{})
	if r.Complete || len(r.Definitions) != 0 || len(r.Errors) != 1 {
		t.Fatalf("symlink policy %+v", r)
	}
}

func TestAggregateSourceRecordBoundAndEmptyExcludes(t *testing.T) {
	root := temp(t)
	fixture(t, root, ".git/config", "[core]\nexcludesFile = \"\"\n")
	fixture(t, root, ".gitignore", "a\nb\n")
	fixture(t, root, "child/.gitignore", "c\nd\n")
	fixture(t, root, "child/.mdchk.yaml", "ok")
	env := environment(t)
	fixture(t, env["XDG_CONFIG_HOME"], "git/ignore", "child/\n")
	l := parser.DefaultLimits()
	l.DocumentNodes = 2
	l.TotalNodes = 4
	b, _ := parser.NewOperationBudget(l)
	r := Scan(context.Background(), Options{Root: root, Environment: env, Budget: b}, Hooks{})
	if !r.Complete || len(r.Definitions) != 1 {
		t.Fatalf("exact %+v", r)
	}
	l.TotalNodes = 3
	b, _ = parser.NewOperationBudget(l)
	r = Scan(context.Background(), Options{Root: root, Environment: env, Budget: b}, Hooks{})
	var limit *parser.LimitExceeded
	if !errors.As(r.Interruption, &limit) || limit.Name != "total-nodes" {
		t.Fatalf("over %+v", r)
	}
}

type changingAccess struct {
	scanAccess
	target    string
	change    func() error
	directory bool
}

func (a changingAccess) open(path string, dir bool) (readHandle, error) {
	f, e := a.scanAccess.open(path, dir)
	if e != nil {
		return nil, e
	}
	if path == a.target && dir == a.directory {
		if !dir {
			return &changingHandle{readHandle: f, change: a.change}, nil
		}
		if e = a.change(); e != nil {
			closeHandle(f)
			return nil, e
		}
	}
	return f, nil
}
func TestChangedDirectoryAndDefinitionDetected(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "definition", true: "directory"}[directory], func(t *testing.T) {
			root := temp(t)
			fixture(t, root, "child/.mdchk.yaml", "original")
			target := filepath.Join(root, "child")
			if !directory {
				target = filepath.Join(target, ".mdchk.yaml")
			}
			r := scan(context.Background(), Options{Root: root, Environment: environment(t)}, Hooks{}, func(a scanAccess) scanAccess {
				return changingAccess{a, target, func() error {
					if directory {
						if e := os.Rename(target, target+"-old"); e != nil {
							return e
						}
						return os.Mkdir(target, 0700)
					}
					fixture(t, root, "child/.mdchk.yaml", "replacement with different size")
					return nil
				}, directory}
			})
			if r.Complete || len(r.Errors) == 0 || !strings.Contains(r.Errors[0].Error(), "changed") {
				t.Fatalf("%+v", r)
			}
		})
	}
}

type changingHandle struct {
	readHandle
	change  func() error
	changed bool
}

func (f *changingHandle) Read(p []byte) (int, error) {
	n, e := f.readHandle.Read(p)
	if !f.changed {
		f.changed = true
		if err := f.change(); err != nil {
			return n, err
		}
	}
	return n, e
}

func TestSharedPreviouslyConsumedNodeBudget(t *testing.T) {
	root := temp(t)
	fixture(t, root, ".gitignore", "a\nb\n")
	l := parser.DefaultLimits()
	l.DocumentNodes = 2
	l.TotalNodes = 3
	b, _ := parser.NewOperationBudget(l)
	if e := b.ChargeNodes(2); e != nil {
		t.Fatal(e)
	}
	r := Scan(context.Background(), Options{Root: root, Environment: environment(t), Budget: b}, Hooks{})
	var limit *parser.LimitExceeded
	if !errors.As(r.Interruption, &limit) || limit.Name != "total-nodes" || limit.Observed != 4 {
		t.Fatalf("%+v", r)
	}
}
