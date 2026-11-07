package acceptance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// cacheDirName is the cache root's name inside the user cache directory.
	cacheDirName = "mdchk-acceptance"
	// cacheKeyFormat changes whenever the key's inputs change.
	cacheKeyFormat = "mdchk-acceptance-key-1"
	// cacheTempPattern names temporary directories, which never look like a key.
	cacheTempPattern = "tmp-*"
	// cacheIdleLimit is far longer than any test timeout in this repository, so a
	// running process's executable is not removed.
	cacheIdleLimit = 24 * time.Hour
)

// resolvedExecutable is a usable executable. privateDir is set when the build
// belongs to this process alone, which then removes that directory.
type resolvedExecutable struct {
	binary     string
	privateDir string
}

// buildFunc matches goBuildAt. Tests wrap it to count builds and to change the
// world while a build runs.
type buildFunc func(root, binary, overlay string, env []string) error

// resolveExecutable returns the executable for the module at root: a cache entry
// under cacheRoot when reuse is safe, or a build private to this process. It logs
// why when reuse isn't safe. Only a failing build is an error.
func resolveExecutable(root, cacheRoot string, env []string, logf func(string, ...any), build buildFunc) (resolvedExecutable, error) {
	private := func(format string, args ...any) (resolvedExecutable, error) {
		logf("acceptance: building a private executable: "+format, args...)
		return buildPrivately(root, env, build)
	}
	key, err := cacheKey(root, env)
	if err != nil {
		return private("no cache key: %v", err)
	}
	if err := prepareCacheRoot(root, cacheRoot); err != nil {
		return private("unsafe cache directory: %v", err)
	}
	entry := filepath.Join(cacheRoot, key)
	binary := filepath.Join(entry, ExecutableName())
	if _, err := os.Lstat(entry); err == nil {
		if !usableEntry(entry) {
			return private("cache entry %s has no usable executable", entry)
		}
		// An unrecorded use could let a sweep remove an executable in use.
		now := time.Now()
		if err := os.Chtimes(entry, now, now); err != nil {
			return private("record use of cache entry: %v", err)
		}
		return resolvedExecutable{binary: binary}, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return private("read cache entry: %v", err)
	}

	temp, err := os.MkdirTemp(cacheRoot, cacheTempPattern)
	if err != nil {
		return private("create temporary cache directory: %v", err)
	}
	built := filepath.Join(temp, ExecutableName())
	if err := build(root, built, "", env); err != nil {
		_ = os.RemoveAll(temp)
		return resolvedExecutable{}, err
	}
	stillPrivate := resolvedExecutable{binary: built, privateDir: temp}
	after, err := cacheKey(root, env)
	if err != nil {
		logf("acceptance: keeping the executable private: no cache key after the build: %v", err)
		return stillPrivate, nil
	}
	if after != key {
		logf("acceptance: keeping the executable private: the module changed during the build")
		return stillPrivate, nil
	}

	renameErr := os.Rename(temp, entry)
	if renameErr == nil {
		sweepCache(cacheRoot, time.Now())
		return resolvedExecutable{binary: binary}, nil
	}
	// Another process won the race, or the host refused the rename. Windows
	// refuses to rename over an existing directory, and Unix over a nonempty one.
	if usableEntry(entry) {
		_ = os.RemoveAll(temp)
		now := time.Now()
		_ = os.Chtimes(entry, now, now)
		sweepCache(cacheRoot, now)
		return resolvedExecutable{binary: binary}, nil
	}
	logf("acceptance: keeping the executable private: publish failed with no usable entry: %v", renameErr)
	sweepCache(cacheRoot, time.Now())
	return stillPrivate, nil
}

// buildPrivately builds into a new temporary directory that nothing else uses.
func buildPrivately(root string, env []string, build buildFunc) (resolvedExecutable, error) {
	dir, err := os.MkdirTemp("", "mdchk-executable-")
	if err != nil {
		return resolvedExecutable{}, err
	}
	binary := filepath.Join(dir, ExecutableName())
	if err := build(root, binary, "", env); err != nil {
		_ = os.RemoveAll(dir)
		return resolvedExecutable{}, err
	}
	return resolvedExecutable{binary: binary, privateDir: dir}, nil
}

// usableEntry reports whether entry is a real directory with a regular executable.
func usableEntry(entry string) bool {
	info, err := os.Lstat(entry)
	if err != nil || !info.IsDir() {
		return false
	}
	binary, err := os.Lstat(filepath.Join(entry, ExecutableName()))
	if err != nil || !binary.Mode().IsRegular() {
		return false
	}
	return runtime.GOOS == "windows" || binary.Mode().Perm()&0o100 != 0
}

// prepareCacheRoot creates cacheRoot unless that would put binaries inside the
// module, and refuses a root another user could write to.
func prepareCacheRoot(root, cacheRoot string) error {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	resolvedCache, err := resolveExistingPrefix(cacheRoot)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedCache)
	if err != nil {
		return err
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return fmt.Errorf("%s is inside the module root %s", resolvedCache, resolvedRoot)
	}
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(cacheRoot)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a real directory", cacheRoot)
	}
	return checkCacheRootPrivate(info)
}

// resolveExistingPrefix resolves symlinks in the part of path that exists, so a
// path can be placed before it is created.
func resolveExistingPrefix(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	parent := filepath.Dir(path)
	if !errors.Is(err, fs.ErrNotExist) || parent == path {
		return "", err
	}
	resolved, err = resolveExistingPrefix(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(path)), nil
}

// sweepCache removes entries and abandoned temporary directories idle for longer
// than cacheIdleLimit. Removal errors are left for a later sweep, because Windows
// refuses to delete a running executable.
func sweepCache(cacheRoot string, now time.Time) {
	entries, err := os.ReadDir(cacheRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) <= cacheIdleLimit {
			continue
		}
		_ = os.RemoveAll(filepath.Join(cacheRoot, entry.Name()))
	}
}

// listedPackage holds the go list fields that determine a package's build inputs.
type listedPackage struct {
	Dir          string
	ImportPath   string
	Standard     bool
	GoFiles      []string
	CgoFiles     []string
	CFiles       []string
	CXXFiles     []string
	MFiles       []string
	HFiles       []string
	FFiles       []string
	SFiles       []string
	SwigFiles    []string
	SwigCXXFiles []string
	SysoFiles    []string
	EmbedFiles   []string
}

func (p listedPackage) files() []string {
	var all []string
	for _, files := range [][]string{p.GoFiles, p.CgoFiles, p.CFiles, p.CXXFiles, p.MFiles, p.HFiles, p.FFiles, p.SFiles, p.SwigFiles, p.SwigCXXFiles, p.SysoFiles, p.EmbedFiles} {
		all = append(all, files...)
	}
	return all
}

// cacheKey hashes everything that decides the executable's bytes under env: the
// module's place, the Go settings, the source of every non-standard package and
// the Git state that Go stamps into the binary. It fails when env makes the build
// depend on inputs the key doesn't cover.
func cacheKey(root string, env []string) (string, error) {
	h := sha256.New()
	addKeyField(h, "format", []byte(cacheKeyFormat))

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	addKeyField(h, "root", []byte(resolvedRoot))

	output, err := commandOutput(root, env, "go", "env", "-json")
	if err != nil {
		return "", err
	}
	var goEnv map[string]any
	if err := json.Unmarshal(output, &goEnv); err != nil {
		return "", fmt.Errorf("decode go env: %w", err)
	}
	if flags, _ := goEnv["GOFLAGS"].(string); strings.Contains(flags, "-overlay") || strings.Contains(flags, "-modfile") {
		return "", fmt.Errorf("GOFLAGS %q has -overlay or -modfile, whose inputs the key doesn't cover", flags)
	}
	// GOGCCFLAGS holds a random /tmp/go-build path, different on every call.
	delete(goEnv, "GOGCCFLAGS")
	canonical, err := json.Marshal(goEnv)
	if err != nil {
		return "", err
	}
	addKeyField(h, "go env", canonical)

	if err := addPackages(h, root, env); err != nil {
		return "", err
	}

	for _, name := range []string{"go.mod", "go.sum"} {
		//nolint:gosec // Fixed names in the module under test.
		data, err := os.ReadFile(filepath.Join(root, name))
		switch {
		case err == nil:
			addKeyField(h, name, data)
		case name == "go.sum" && errors.Is(err, fs.ErrNotExist):
			// A module with no dependencies has no go.sum.
			addKeyField(h, "no go.sum", nil)
		default:
			return "", err
		}
	}

	for _, args := range [][]string{{"rev-parse", "HEAD"}, {"for-each-ref", "refs/tags"}} {
		output, err := commandOutput(root, env, "git", args...)
		if err != nil {
			return "", err
		}
		addKeyField(h, args[0], output)
	}
	status, err := commandOutput(root, env, "git", "status", "--porcelain")
	if err != nil {
		return "", err
	}
	// Go stamps only whether the tree is dirty, not which files changed.
	addKeyField(h, "dirty", []byte(fmt.Sprint(len(bytes.TrimSpace(status)) != 0)))

	return hex.EncodeToString(h.Sum(nil)), nil
}

// addPackages hashes the import path and source of every non-standard dependency.
func addPackages(h hash.Hash, root string, env []string) error {
	output, err := commandOutput(root, env, "go", "list", "-deps", "-json", "./cmd/mdchk")
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return fmt.Errorf("decode go list: %w", err)
		}
		if pkg.Standard {
			continue
		}
		addKeyField(h, "package", []byte(pkg.ImportPath))
		for _, name := range pkg.files() {
			data, err := os.ReadFile(filepath.Join(pkg.Dir, filepath.FromSlash(name)))
			if err != nil {
				return err
			}
			addKeyField(h, "file name", []byte(name))
			addKeyField(h, "file bytes", data)
		}
	}
}

// addKeyField writes one length-prefixed field, so two fields can't run together
// and collide with a different pair.
func addKeyField(h hash.Hash, label string, data []byte) {
	_, _ = fmt.Fprintf(h, "%s %d\n", label, len(data))
	_, _ = h.Write(data)
}
