package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// buildTimeout bounds one go build, and each command that computes a cache key.
const buildTimeout = time.Minute

// sharedExecutables holds the unmodified mdchk builds that tests share. Every
// build without an overlay produces the same executable for a given cgo
// setting, and building dominates most compiled tests, so each setting resolves
// once per test process. Tests must not modify a shared executable.
var sharedExecutables struct {
	sync.Mutex
	builds map[string]*sharedBuild
}

type sharedBuild struct {
	once sync.Once
	// dir is set only for a build private to this process, never a cache entry.
	dir    string
	binary string
	err    error
}

// currentModuleRoot finds the module once: a test process stays in one module.
var currentModuleRoot = sync.OnceValues(func() (string, error) { return moduleRootFrom("") })

// moduleRootFrom returns the root of the module containing dir, or of the current
// directory when dir is empty.
func moduleRootFrom(dir string) (string, error) {
	output, err := commandOutput(dir, nil, "go", "env", "GOMOD")
	if err != nil {
		return "", fmt.Errorf("find the module root: %w", err)
	}
	gomod := strings.TrimSpace(string(output))
	if gomod == "" || gomod == os.DevNull {
		return "", fmt.Errorf("find the module root: no Go module here (go env GOMOD printed %q)", gomod)
	}
	return filepath.Dir(gomod), nil
}

// ModuleRoot returns the root of the module containing the current directory.
func ModuleRoot(t *testing.T) string {
	t.Helper()
	root, err := currentModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// Cleanup removes this process's private builds once every test has finished
// with them. Cache entries stay: other processes may be running them.
func Cleanup() {
	sharedExecutables.Lock()
	defer sharedExecutables.Unlock()
	for _, build := range sharedExecutables.builds {
		if build.dir != "" {
			_ = os.RemoveAll(build.dir)
		}
	}
}

// SharedExecutable returns the mdchk build for the environment's cgo setting.
func SharedExecutable(t *testing.T) string {
	t.Helper()
	return SharedExecutableFor(t, "")
}

// SharedNoCGOExecutable returns the mdchk build with CGO_ENABLED=0.
func SharedNoCGOExecutable(t *testing.T) string {
	t.Helper()
	return SharedExecutableFor(t, "0")
}

// SharedExecutableFor resolves the build with cgo set to CGO_ENABLED, or with the
// environment's setting when cgo is empty.
func SharedExecutableFor(t *testing.T, cgo string) string {
	t.Helper()
	sharedExecutables.Lock()
	if sharedExecutables.builds == nil {
		sharedExecutables.builds = map[string]*sharedBuild{}
	}
	build := sharedExecutables.builds[cgo]
	if build == nil {
		build = &sharedBuild{}
		sharedExecutables.builds[cgo] = build
	}
	sharedExecutables.Unlock()
	build.once.Do(func() {
		env := os.Environ()
		if cgo != "" {
			env = append(env, "CGO_ENABLED="+cgo)
		}
		root, err := currentModuleRoot()
		if err != nil {
			build.err = err
			return
		}
		var resolved resolvedExecutable
		if base, cacheErr := os.UserCacheDir(); cacheErr == nil {
			resolved, build.err = resolveExecutable(root, filepath.Join(base, cacheDirName), env, t.Logf, goBuildAt)
		} else {
			t.Logf("acceptance: building a private executable: no user cache directory: %v", cacheErr)
			resolved, build.err = buildPrivately(root, env, goBuildAt)
		}
		build.binary, build.dir = resolved.binary, resolved.privateDir
	})
	if build.err != nil {
		t.Fatalf("build shared executable: %v", build.err)
	}
	return build.binary
}

// BuildExecutable builds a private executable for an overlay.
// Use SharedExecutable when the build has no overlay.
func BuildExecutable(t *testing.T, overlay string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), ExecutableName())
	if err := GoBuildExecutable(binary, overlay, nil); err != nil {
		t.Fatalf("build executable: %v", err)
	}
	return binary
}

// BuildProviderFixtureExecutable isolates test registration at compile time.
// Reuse it for compiled CLI checks without adding a production runtime switch.
func BuildProviderFixtureExecutable(t *testing.T) string {
	t.Helper()
	root := ModuleRoot(t)
	overlayBytes, err := json.Marshal(struct{ Replace map[string]string }{map[string]string{
		filepath.Join(root, "internal", "cli", "providers.go"): filepath.Join(root, "cmd", "mdchk", "testdata", "configuration_providers.go"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(t.TempDir(), "overlay.json")
	if err := os.WriteFile(overlay, overlayBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return BuildExecutable(t, overlay)
}

// ExecutableName returns the mdchk executable's file name on this host.
func ExecutableName() string {
	if runtime.GOOS == "windows" {
		return "mdchk.exe"
	}
	return "mdchk"
}

// GoBuildExecutable builds ./cmd/mdchk of the current module to binary. A nil
// env inherits the test process environment.
func GoBuildExecutable(binary, overlay string, env []string) error {
	root, err := currentModuleRoot()
	if err != nil {
		return err
	}
	return goBuildAt(root, binary, overlay, env)
}

// goBuildAt builds ./cmd/mdchk of the module at root.
func goBuildAt(root, binary, overlay string, env []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()
	args := []string{"build", "-o", binary}
	if overlay != "" {
		args = append(args, "-overlay", overlay)
	}
	args = append(args, "./cmd/mdchk")
	//nolint:gosec // Fixed Go tool builds this module into a test-owned destination.
	command := exec.CommandContext(ctx, "go", args...)
	setCommandPlace(command, root, env)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%w\n%s", err, output)
	}
	return nil
}

// commandOutput returns the standard output of a tool run in dir under env. An
// empty dir means the current directory and a nil env inherits the environment.
func commandOutput(dir string, env []string, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()
	//nolint:gosec // Fixed go and git commands with test-owned arguments.
	command := exec.CommandContext(ctx, name, args...)
	setCommandPlace(command, dir, env)
	output, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return output, nil
}

// setCommandPlace sets a command's directory and environment. exec sets PWD only
// for a nil env. Set it for an explicit env too, so the Go tool sees dir under the
// same name either way and absolute overlay paths keep matching.
func setCommandPlace(command *exec.Cmd, dir string, env []string) {
	command.Dir = dir
	command.Env = env
	if dir != "" && env != nil {
		command.Env = append(slices.Clip(env), "PWD="+dir)
	}
}
