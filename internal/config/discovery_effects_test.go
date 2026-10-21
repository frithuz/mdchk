package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/frithuz/mdchk/internal/engine"
)

type fixtureAccess struct {
	files        fstest.MapFS
	failures     map[string]error
	readFailures map[string]error
	calls        []string
}

func (f *fixtureAccess) lstat(path string) (fs.FileInfo, error) {
	f.calls = append(f.calls, path)
	if e := f.failures[path]; e != nil {
		return nil, e
	}
	return fs.Stat(f.files, fixtureName(path))
}
func (f *fixtureAccess) readRegular(path string) ([]byte, error) {
	f.calls = append(f.calls, path)
	if e := f.readFailures[path]; e != nil {
		return nil, e
	}
	if e := f.failures[path]; e != nil {
		return nil, e
	}
	return fs.ReadFile(f.files, fixtureName(path))
}
func fixtureName(path string) string {
	volume := filepath.VolumeName(path)
	path = path[len(volume):]
	if path == string(filepath.Separator) {
		return "."
	}
	return filepath.ToSlash(path[1:])
}
func TestDiscoveryFilesystemRootAndUnreadableFailures(t *testing.T) {
	root := filepath.VolumeName(t.TempDir()) + string(filepath.Separator)
	join := func(parts ...string) string { return filepath.Join(append([]string{root}, parts...)...) }
	config := join(Filename)
	access := &fixtureAccess{files: fstest.MapFS{
		"workspace/sub": {Mode: fs.ModeDir | 0755},
		Filename:        {Data: []byte("version: 1\n")},
	}}
	got, err := resolveFS(access, join("workspace", "sub"), root, "", registryFor(t), engine.ValidationProviders{}, baseCaps())
	if err != nil || got.SelectedPath != config || got.Boundary.Directory != root || got.Boundary.RepositoryRoot != "" {
		t.Fatalf("root traversal = %+v, %v", got, err)
	}
	sentinel := errors.New("injected unreadable configuration")
	near := join("workspace", Filename)
	access.failures = map[string]error{near: sentinel}
	got, err = resolveFS(access, join("workspace", "sub"), root, "", registryFor(t), engine.ValidationProviders{}, baseCaps())
	if got != nil || !errors.Is(err, sentinel) {
		t.Fatalf("unreadable nearest = %+v, %v", got, err)
	}
	var located *ValidationError
	if !errors.As(err, &located) || located.Origin.Path != near {
		t.Fatalf("unreadable origin = %v", err)
	}
	// A selected file may be readable while an applicable ancestor is not.
	selected := join("workspace", "sub", Filename)
	access.files[fixtureName(selected)] = &fstest.MapFile{Data: []byte("version: 1\n")}
	got, err = resolveFS(access, join("workspace", "sub"), root, "", registryFor(t), engine.ValidationProviders{}, baseCaps())
	if got != nil || !errors.Is(err, sentinel) {
		t.Fatalf("unreadable ancestor = %+v, %v", got, err)
	}
}
func TestDiscoveryNeverProbesBeyondCallerOrRepositoryBound(t *testing.T) {
	root := filepath.VolumeName(t.TempDir()) + string(filepath.Separator)
	cap := filepath.Join(root, "workspace")
	start := filepath.Join(cap, "sub")
	access := &fixtureAccess{files: fstest.MapFS{"workspace/sub": {Mode: fs.ModeDir | 0755}}}
	got, err := resolveFS(access, start, cap, "", registryFor(t), engine.ValidationProviders{}, baseCaps())
	if err != nil || got.SelectedPath != "" {
		t.Fatalf("bounded defaults = %+v, %v", got, err)
	}
	for _, path := range access.calls {
		if !inside(cap, path) {
			t.Fatalf("read outside caller cap: %s", path)
		}
	}
	access.files["workspace/sub/.git"] = &fstest.MapFile{Data: []byte("gitdir: elsewhere")}
	access.calls = nil
	got, err = resolveFS(access, start, root, "", registryFor(t), engine.ValidationProviders{}, baseCaps())
	if err != nil || got.Boundary.Directory != start {
		t.Fatalf("repo bound = %+v, %v", got, err)
	}
	for _, path := range access.calls {
		if !inside(start, path) {
			t.Fatalf("read beyond repository scope: %s", path)
		}
	}
}
func TestLoadReadsOwnedStrictYAMLAndPreservesCauses(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "custom.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nrules: {MD013: {length: 88}}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := Load(path)
	if err != nil || d.Provenance["rules.MD013.length"].Path != path {
		t.Fatalf("Load = %+v, %v", d, err)
	}
	before := d.Syntax()
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, d.Syntax()) {
		t.Fatal("loaded snapshot changed with file")
	}
	d, err = Load(filepath.Join(dir, "missing.yaml"))
	if d != nil || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing = %+v, %v", d, err)
	}
	d, err = Load("")
	if d != nil || err == nil {
		t.Fatalf("empty = %+v, %v", d, err)
	}
}

func TestDiscoveryReadAndProviderErrorsRetainAncestorOrigin(t *testing.T) {
	root := discoveryRoot(t)
	discoveryDir(t, root, ".git")
	parent := discoveryFile(t, root, Filename, "version: 1\nrules: {MD095: {enabled: false, allowed_hosts: [example.com]}}\n")
	leaf := discoveryDir(t, root, "leaf")
	discoveryFile(t, leaf, Filename, "version: 1\n")
	sentinel := errors.New("hostname provider failed")
	got, err := Resolve(SelectionOptions{WorkingDirectory: leaf}, registryFor(t), engine.ValidationProviders{ValidateHostname: func(string) error { return sentinel }}, baseCaps())
	var at *ValidationError
	if got != nil || !errors.Is(err, sentinel) || !errors.As(err, &at) || at.Origin.Path != parent || at.Origin.PropertyPath != "rules.MD095.allowed_hosts[0]" {
		t.Fatalf("provider failure = %+v, %v", got, err)
	}
	volumeRoot := filepath.VolumeName(root) + string(filepath.Separator)
	selected := filepath.Join(volumeRoot, "workspace", Filename)
	fake := &fixtureAccess{files: fstest.MapFS{"workspace": {Mode: fs.ModeDir | 0755}, "workspace/.mdchk.yaml": {Data: []byte("version: 1\n")}}, readFailures: map[string]error{selected: sentinel}}
	got, err = resolveFS(fake, filepath.Dir(selected), filepath.Dir(selected), "", registryFor(t), engine.ValidationProviders{}, baseCaps())
	if got != nil || !errors.Is(err, sentinel) || !errors.As(err, &at) || at.Origin.Path != selected {
		t.Fatalf("read failure = %+v, %v", got, err)
	}
}
