package acceptance

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
)

// concurrentPublishEnv hands a child process its work in the concurrent test.
const concurrentPublishEnv = "MDCHK_ACCEPTANCE_TEST_CONCURRENT_PUBLISH"

// fixtureModule is a small Git-tracked module with a cmd/mdchk that prints
// a source constant and an embedded file.
type fixtureModule struct {
	root string
	env  []string
}

func newFixtureModule(t *testing.T) fixtureModule {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := fixtureModule{root: root, env: fixtureEnv(root)}
	m.write(t, "go.mod", "module example.test/fixture\n\ngo 1.22\n")
	m.write(t, "cmd/mdchk/data.txt", "embedded\n")
	m.writeSource(t, "first")
	m.git(t, "init", "-q")
	m.commit(t, "initial")
	return m
}

// fixtureEnv isolates a fixture's Git state from the repository under test. A
// Git hook's GIT_DIR, in particular, would point these commands at that repository.
func fixtureEnv(root string) []string {
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "GIT_") {
			env = append(env, entry)
		}
	}
	return append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(root, ".git", "absent-gitconfig"), "GOFLAGS=", "GOWORK=off")
}

func (m fixtureModule) write(t *testing.T, name, contents string) {
	t.Helper()
	PutFile(t, m.root, name, contents)
}

func (m fixtureModule) writeSource(t *testing.T, output string) {
	t.Helper()
	m.write(t, "cmd/mdchk/main.go", fmt.Sprintf(`package main

import (
	_ "embed"
	"fmt"
)

//go:embed data.txt
var data string

func main() { fmt.Print(%q, " ", data) }
`, output))
}

func (m fixtureModule) git(t *testing.T, args ...string) {
	t.Helper()
	//nolint:gosec // Fixed Git tool on a test-owned fixture repository.
	command := exec.Command("git", args...)
	setCommandPlace(command, m.root, m.env)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func (m fixtureModule) commit(t *testing.T, message string) {
	t.Helper()
	m.git(t, "add", "-A")
	m.git(t, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", message)
}

func (m fixtureModule) key(t *testing.T, env ...string) string {
	t.Helper()
	key, err := cacheKey(m.root, append(m.env[:len(m.env):len(m.env)], env...))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func (m fixtureModule) resolve(t *testing.T, cacheRoot string, env []string, build buildFunc) (resolvedExecutable, []string) {
	t.Helper()
	var logged []string
	logf := func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
		t.Logf(format, args...)
	}
	got, err := resolveExecutable(m.root, cacheRoot, env, logf, build)
	if err != nil {
		t.Fatal(err)
	}
	if got.privateDir != "" {
		t.Cleanup(func() { _ = os.RemoveAll(got.privateDir) })
	}
	return got, logged
}

func newCacheRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), cacheDirName)
}

// countBuilds wraps goBuildAt and counts its calls.
func countBuilds(count *atomic.Int32) buildFunc {
	return func(root, binary, overlay string, env []string) error {
		count.Add(1)
		return goBuildAt(root, binary, overlay, env)
	}
}

func execute(t *testing.T, binary string) string {
	t.Helper()
	return RunProcess(t, binary, t.TempDir()).Stdout
}

func publishedEntries(t *testing.T, cacheRoot string) []string {
	t.Helper()
	entries, err := os.ReadDir(cacheRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "tmp-") {
			names = append(names, entry.Name())
		}
	}
	return names
}

func assertNoTemporaries(t *testing.T, cacheRoot string) {
	t.Helper()
	entries, err := os.ReadDir(cacheRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "tmp-") {
			t.Fatalf("temporary directory left behind: %s", entry.Name())
		}
	}
}

func assertPrivateBuild(t *testing.T, got resolvedExecutable, logged []string, cacheRoot, reason string) {
	t.Helper()
	if got.privateDir == "" {
		t.Fatalf("got a shared executable %s", got.binary)
	}
	if names := publishedEntries(t, cacheRoot); len(names) != 0 {
		t.Fatalf("published %v", names)
	}
	if !strings.Contains(strings.Join(logged, "\n"), reason) {
		t.Fatalf("no log line says %q: %q", reason, logged)
	}
	if out := execute(t, got.binary); !strings.Contains(out, "first") {
		t.Fatalf("private build printed %q", out)
	}
}

func TestSourceChangeForcesRebuild(t *testing.T) {
	t.Parallel()
	m := newFixtureModule(t)
	cacheRoot := newCacheRoot(t)
	var builds atomic.Int32
	first, _ := m.resolve(t, cacheRoot, m.env, countBuilds(&builds))
	if out := execute(t, first.binary); out != "first embedded\n" {
		t.Fatalf("first build printed %q", out)
	}
	m.writeSource(t, "second")
	second, _ := m.resolve(t, cacheRoot, m.env, countBuilds(&builds))
	if out := execute(t, second.binary); out != "second embedded\n" {
		t.Fatalf("stale binary after a source change printed %q", out)
	}
	if builds.Load() != 2 || first.binary == second.binary {
		t.Fatalf("builds %d, first %s, second %s", builds.Load(), first.binary, second.binary)
	}
	if out := execute(t, first.binary); out != "first embedded\n" {
		t.Fatalf("publishing replaced the first entry: %q", out)
	}
}

func TestUnchangedModuleReusesEntry(t *testing.T) {
	t.Parallel()
	m := newFixtureModule(t)
	cacheRoot := newCacheRoot(t)
	var builds atomic.Int32
	first, _ := m.resolve(t, cacheRoot, m.env, countBuilds(&builds))
	entry := filepath.Dir(first.binary)
	if first.privateDir != "" || filepath.Dir(entry) != cacheRoot {
		t.Fatalf("first build is not a cache entry: %+v", first)
	}
	assertNoTemporaries(t, cacheRoot)
	before, err := os.Stat(first.binary)
	if err != nil {
		t.Fatal(err)
	}
	// A hit records its use on the directory, never on the executable.
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(entry, past, past); err != nil {
		t.Fatal(err)
	}
	second, _ := m.resolve(t, cacheRoot, m.env, countBuilds(&builds))
	if builds.Load() != 1 || second.binary != first.binary || second.privateDir != "" {
		t.Fatalf("builds %d, second %+v", builds.Load(), second)
	}
	after, err := os.Stat(second.binary)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("a cache hit changed the executable")
	}
	info, err := os.Stat(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().After(past) {
		t.Fatal("a cache hit did not record its use")
	}
}

func TestCacheKeyInputs(t *testing.T) {
	t.Parallel()
	t.Run("stable", func(t *testing.T) {
		t.Parallel()
		m := newFixtureModule(t)
		first, second := m.key(t), m.key(t)
		if first != second {
			t.Fatal("an unchanged module changed its key")
		}
	})
	t.Run("module root", func(t *testing.T) {
		t.Parallel()
		first, second := newFixtureModule(t), newFixtureModule(t)
		if first.key(t) == second.key(t) {
			t.Fatal("two module roots share a key")
		}
	})
	t.Run("CGO_ENABLED", func(t *testing.T) {
		t.Parallel()
		m := newFixtureModule(t)
		if m.key(t, "CGO_ENABLED=0") == m.key(t, "CGO_ENABLED=1") {
			t.Fatal("two cgo settings share a key")
		}
	})
	// Each edit starts dirty, so the dirty flag can't explain a different key.
	for _, tc := range []struct{ name, file, contents string }{
		{"embedded file", "cmd/mdchk/data.txt", "changed\n"},
		{"source file", "cmd/mdchk/main.go", "package main\n\nfunc main() {}\n"},
		{"go.mod", "go.mod", "module example.test/fixture\n\ngo 1.22\n\n// changed\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newFixtureModule(t)
			m.write(t, "untracked.txt", "dirty\n")
			before := m.key(t)
			m.write(t, tc.file, tc.contents)
			if m.key(t) == before {
				t.Fatal("the change kept the key")
			}
		})
	}
	t.Run("new commit", func(t *testing.T) {
		t.Parallel()
		m := newFixtureModule(t)
		before := m.key(t)
		m.commit(t, "next")
		if m.key(t) == before {
			t.Fatal("a new commit kept the key")
		}
	})
	t.Run("new tag", func(t *testing.T) {
		t.Parallel()
		m := newFixtureModule(t)
		before := m.key(t)
		m.git(t, "tag", "v1.2.3")
		if m.key(t) == before {
			t.Fatal("a new tag kept the key")
		}
	})
	t.Run("clean and dirty", func(t *testing.T) {
		t.Parallel()
		m := newFixtureModule(t)
		clean := m.key(t)
		m.write(t, "untracked.txt", "dirty\n")
		dirty := m.key(t)
		if clean == dirty {
			t.Fatal("a dirty tree kept the clean key")
		}
		// Go stamps only that fact, so which files changed doesn't count.
		m.write(t, "second-untracked.txt", "dirty\n")
		if m.key(t) != dirty {
			t.Fatal("a second untracked file changed the key")
		}
	})
}

// TestConcurrentPublishProcess is the child side of TestConcurrentPublish, and
// does nothing in an ordinary run.
func TestConcurrentPublishProcess(t *testing.T) {
	encoded := os.Getenv(concurrentPublishEnv)
	if encoded == "" {
		return
	}
	var work concurrentPublishWork
	if err := json.Unmarshal([]byte(encoded), &work); err != nil {
		t.Fatal(err)
	}
	// Hold every child at the end of its build, so that all of them have missed
	// the cache and all of them try to publish.
	build := func(root, binary, overlay string, env []string) error {
		if err := goBuildAt(root, binary, overlay, env); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(work.Barrier, fmt.Sprint(os.Getpid())), nil, 0o600); err != nil {
			return err
		}
		for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			arrived, err := os.ReadDir(work.Barrier)
			if err != nil {
				return err
			}
			if len(arrived) == work.Processes {
				return nil
			}
		}
		return errors.New("timed out waiting for the other processes")
	}
	got, err := resolveExecutable(work.Root, work.CacheRoot, work.Env, t.Logf, build)
	if err != nil {
		t.Fatal(err)
	}
	if got.privateDir != "" {
		t.Fatalf("losing the race gave a private build: %+v", got)
	}
	if out := execute(t, got.binary); out != "first embedded\n" {
		t.Fatalf("published binary printed %q", out)
	}
}

type concurrentPublishWork struct {
	Root, CacheRoot, Barrier string
	Env                      []string
	Processes                int
}

func TestConcurrentPublish(t *testing.T) {
	t.Parallel()
	m := newFixtureModule(t)
	cacheRoot := newCacheRoot(t)
	work := concurrentPublishWork{Root: m.root, CacheRoot: cacheRoot, Barrier: t.TempDir(), Env: m.env, Processes: 4}
	encoded, err := json.Marshal(work)
	if err != nil {
		t.Fatal(err)
	}
	test, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, work.Processes)
	for range work.Processes {
		go func() {
			command := exec.Command(test, "-test.run=^TestConcurrentPublishProcess$", "-test.count=1")
			command.Env = append(os.Environ(), concurrentPublishEnv+"="+string(encoded))
			output, err := command.CombinedOutput()
			if err != nil {
				err = fmt.Errorf("%w\n%s", err, output)
			}
			results <- err
		}()
	}
	for range work.Processes {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	if names := publishedEntries(t, cacheRoot); len(names) != 1 {
		t.Fatalf("entries %v", names)
	}
	assertNoTemporaries(t, cacheRoot)
}

func TestPublishLosesToExistingEntry(t *testing.T) {
	t.Parallel()
	m := newFixtureModule(t)
	cacheRoot := newCacheRoot(t)
	entry := filepath.Join(cacheRoot, m.key(t))
	winner := filepath.Join(entry, ExecutableName())
	// Another process publishes after this one missed the cache.
	build := func(root, binary, overlay string, env []string) error {
		if err := goBuildAt(root, binary, overlay, env); err != nil {
			return err
		}
		if err := os.Mkdir(entry, 0o700); err != nil {
			return err
		}
		return goBuildAt(root, winner, overlay, env)
	}
	got, logged := m.resolve(t, cacheRoot, m.env, build)
	if got.binary != winner || got.privateDir != "" || len(logged) != 0 {
		t.Fatalf("got %+v, logged %q", got, logged)
	}
	assertNoTemporaries(t, cacheRoot)
	if out := execute(t, got.binary); out != "first embedded\n" {
		t.Fatalf("winner printed %q", out)
	}
}

func TestSourceChangeDuringBuildPreventsPublication(t *testing.T) {
	t.Parallel()
	m := newFixtureModule(t)
	cacheRoot := newCacheRoot(t)
	build := func(root, binary, overlay string, env []string) error {
		if err := goBuildAt(root, binary, overlay, env); err != nil {
			return err
		}
		m.writeSource(t, "changed-during-build")
		return nil
	}
	got, logged := m.resolve(t, cacheRoot, m.env, build)
	assertPrivateBuild(t, got, logged, cacheRoot, "changed during the build")
}

func TestSweepRemovesOnlyIdleDirectories(t *testing.T) {
	t.Parallel()
	cacheRoot := t.TempDir()
	now := time.Now()
	ages := map[string]time.Duration{
		"old-entry":    25 * time.Hour,
		"tmp-old":      25 * time.Hour,
		"recent-entry": 23 * time.Hour,
		"tmp-recent":   time.Minute,
	}
	for name, age := range ages {
		dir := filepath.Join(cacheRoot, name)
		PutFile(t, dir, ExecutableName(), "executable")
		if err := os.Chtimes(dir, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	sweepCache(cacheRoot, now)
	for name, age := range ages {
		_, err := os.Stat(filepath.Join(cacheRoot, name))
		if removed := errors.Is(err, fs.ErrNotExist); removed != (age > cacheIdleLimit) {
			t.Errorf("%s, idle %s: removed %t, stat error %v", name, age, removed, err)
		}
	}
}

func TestPublishSweepsIdleEntries(t *testing.T) {
	t.Parallel()
	m := newFixtureModule(t)
	cacheRoot := newCacheRoot(t)
	old := filepath.Join(cacheRoot, "old-entry")
	PutFile(t, old, ExecutableName(), "executable")
	past := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	var builds atomic.Int32
	m.resolve(t, cacheRoot, m.env, countBuilds(&builds))
	if _, err := os.Stat(old); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("idle entry survived a publish: %v", err)
	}
}

func TestUnsafeReuseBuildsPrivately(t *testing.T) {
	t.Parallel()
	var builds atomic.Int32
	t.Run("key can't be computed", func(t *testing.T) {
		t.Parallel()
		m := newFixtureModule(t)
		if err := os.RemoveAll(filepath.Join(m.root, ".git")); err != nil {
			t.Fatal(err)
		}
		cacheRoot := newCacheRoot(t)
		got, logged := m.resolve(t, cacheRoot, m.env, countBuilds(&builds))
		assertPrivateBuild(t, got, logged, cacheRoot, "no cache key")
	})
	t.Run("cache directory unavailable", func(t *testing.T) {
		t.Parallel()
		m := newFixtureModule(t)
		blocker := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(blocker, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		cacheRoot := filepath.Join(blocker, cacheDirName)
		got, logged := m.resolve(t, cacheRoot, m.env, countBuilds(&builds))
		if got.privateDir == "" || !strings.Contains(strings.Join(logged, "\n"), "unsafe cache directory") {
			t.Fatalf("got %+v, logged %q", got, logged)
		}
		if out := execute(t, got.binary); !strings.Contains(out, "first") {
			t.Fatalf("private build printed %q", out)
		}
	})
	t.Run("publish fails with no usable entry", func(t *testing.T) {
		t.Parallel()
		m := newFixtureModule(t)
		cacheRoot := newCacheRoot(t)
		entry := filepath.Join(cacheRoot, m.key(t))
		build := func(root, binary, overlay string, env []string) error {
			if err := goBuildAt(root, binary, overlay, env); err != nil {
				return err
			}
			return os.MkdirAll(filepath.Join(entry, "not-an-executable"), 0o700)
		}
		got, logged := m.resolve(t, cacheRoot, m.env, build)
		if got.privateDir == "" || filepath.Dir(got.binary) == entry || !strings.Contains(strings.Join(logged, "\n"), "publish failed") {
			t.Fatalf("got %+v, logged %q", got, logged)
		}
		if out := execute(t, got.binary); out != "first embedded\n" {
			t.Fatalf("private build printed %q", out)
		}
	})
	t.Run("existing entry without an executable", func(t *testing.T) {
		t.Parallel()
		m := newFixtureModule(t)
		cacheRoot := newCacheRoot(t)
		entry := filepath.Join(cacheRoot, m.key(t))
		if err := os.MkdirAll(entry, 0o700); err != nil {
			t.Fatal(err)
		}
		got, logged := m.resolve(t, cacheRoot, m.env, countBuilds(&builds))
		if got.privateDir == "" || !strings.Contains(strings.Join(logged, "\n"), "no usable executable") {
			t.Fatalf("got %+v, logged %q", got, logged)
		}
		if usableEntry(entry) {
			t.Fatal("a private build filled the entry")
		}
	})
	t.Run("GOFLAGS overlay", func(t *testing.T) {
		t.Parallel()
		m := newFixtureModule(t)
		overlay := filepath.Join(t.TempDir(), "overlay.json")
		if err := os.WriteFile(overlay, []byte(`{"Replace":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		cacheRoot := newCacheRoot(t)
		got, logged := m.resolve(t, cacheRoot, append(m.env, "GOFLAGS=-overlay="+overlay), countBuilds(&builds))
		assertPrivateBuild(t, got, logged, cacheRoot, "-overlay or -modfile")
	})
	t.Run("GOFLAGS modfile", func(t *testing.T) {
		t.Parallel()
		m := newFixtureModule(t)
		modfile := filepath.Join(t.TempDir(), "alternate.mod")
		if err := os.WriteFile(modfile, []byte("module example.test/fixture\n\ngo 1.22\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cacheRoot := newCacheRoot(t)
		got, logged := m.resolve(t, cacheRoot, append(m.env, "GOFLAGS=-modfile="+modfile), countBuilds(&builds))
		assertPrivateBuild(t, got, logged, cacheRoot, "-overlay or -modfile")
	})
	t.Run("cache root inside the module", func(t *testing.T) {
		t.Parallel()
		m := newFixtureModule(t)
		cacheRoot := filepath.Join(m.root, "cache", cacheDirName)
		got, logged := m.resolve(t, cacheRoot, m.env, countBuilds(&builds))
		assertPrivateBuild(t, got, logged, cacheRoot, "inside the module root")
		if _, err := os.Stat(filepath.Join(m.root, "cache")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("created a cache inside the module: %v", err)
		}
	})
}

func TestModuleRootRequiresAModule(t *testing.T) {
	t.Parallel()
	if root, err := moduleRootFrom(t.TempDir()); err == nil || !strings.Contains(err.Error(), "no Go module") {
		t.Fatalf("root %q, error %v", root, err)
	}
}

func TestModuleRootFromAnyPackage(t *testing.T) {
	t.Parallel()
	root := ModuleRoot(t)
	for _, dir := range []string{"", filepath.Join(root, "cmd", "mdchk"), filepath.Join(root, "internal", "rules")} {
		got, err := moduleRootFrom(dir)
		if err != nil || got != root {
			t.Fatalf("from %q: root %q, error %v, want %q", dir, got, err, root)
		}
	}
}

// providerFixtureConfiguration runs only the overlay's MD001 callback, whose
// "fixture violation" finding the production MD001 can't produce.
func providerFixtureConfiguration() string {
	var b strings.Builder
	b.WriteString(AvailableConfiguration + "rules:\n")
	for _, rule := range engine.Catalog() {
		if rule.ID != "MD001" {
			fmt.Fprintf(&b, "  %s: {enabled: false}\n", rule.ID)
		}
	}
	return b.String()
}

func TestOutsidePackageRun(t *testing.T) {
	t.Parallel()
	assertOutsideCommand(t)
	cacheRoot := newCacheRoot(t)
	got, err := resolveExecutable(ModuleRoot(t), cacheRoot, os.Environ(), t.Logf, goBuildAt)
	if err != nil {
		t.Fatal(err)
	}
	if got.privateDir != "" {
		t.Cleanup(func() { _ = os.RemoveAll(got.privateDir) })
	}
	root := Project(t)
	PutFile(t, root, ".mdchk.yaml", CleanConfiguration())
	PutFile(t, root, "doc.md", "# Title\n")
	run := RunProcess(t, got.binary, root, "lint", "--format=json", "doc.md")
	if run.Status != 0 {
		t.Fatalf("status %d: %s", run.Status, run.Stdout)
	}
	if report := ValidateOperationJSON(t, []byte(run.Stdout), "lint"); report.Status != "clean" {
		t.Fatalf("report %+v", report)
	}
}

func TestProviderOverlayBuild(t *testing.T) {
	t.Parallel()
	assertOutsideCommand(t)
	root := Project(t)
	PutFile(t, root, ".mdchk.yaml", providerFixtureConfiguration())
	PutFile(t, root, "doc.md", "# Title\n\nfixture violation\n")
	production, err := resolveExecutable(ModuleRoot(t), newCacheRoot(t), os.Environ(), t.Logf, goBuildAt)
	if err != nil {
		t.Fatal(err)
	}
	if production.privateDir != "" {
		t.Cleanup(func() { _ = os.RemoveAll(production.privateDir) })
	}
	args := []string{"lint", "--format=json", "doc.md"}
	if run := RunProcess(t, production.binary, root, args...); strings.Contains(run.Stdout, "fixture violation") {
		t.Fatalf("the unmodified binary produced the fixture finding: %s", run.Stdout)
	}
	run := RunProcess(t, BuildProviderFixtureExecutable(t), root, args...)
	if run.Status != 10 {
		t.Fatalf("status %d: %s", run.Status, run.Stdout)
	}
	report := ValidateOperationJSON(t, []byte(run.Stdout), "lint")
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Rule != "MD001" || report.Diagnostics[0].Message != "fixture violation" {
		t.Fatalf("overlay finding missing: %s", run.Stdout)
	}
}

func TestValidatorPathFromOutsideCommand(t *testing.T) {
	t.Parallel()
	assertOutsideCommand(t)
	path, err := validatorPath()
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("validator %s: %v", path, err)
	}
}

// assertOutsideCommand proves these tests run from this package, not cmd/mdchk.
func assertOutsideCommand(t *testing.T) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(cwd) != "acceptance" {
		t.Fatalf("running from %s", cwd)
	}
}
