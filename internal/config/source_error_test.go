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

func retainedSources(t *testing.T, err error) []SourceSnapshot {
	t.Helper()
	var acquired *SourceError
	if !errors.As(err, &acquired) {
		t.Fatalf("missing acquired source: %v", err)
	}
	return acquired.Sources()
}

func TestLoadFailureRetainsOriginalAcquisition(t *testing.T) {
	root := discoveryRoot(t)
	source := "version: 1\r\n# 雪😀\r\nversion: 1\r\n"
	path := discoveryFile(t, root, "設定.yaml", source)
	_, err := Load(path)
	var decode *DecodeError
	if !errors.As(err, &decode) || decode.Origin.Path != path || decode.Origin.Position == nil || decode.Origin.Position.Line != 3 || decode.Previous == nil {
		t.Fatalf("lost decode origins: %v", err)
	}
	if err := os.WriteFile(path, []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	want := []SourceSnapshot{snapshotSource(path, []byte(source))}
	got := retainedSources(t, err)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("original acquisition = %#v, want %#v", got, want)
	}
	got[0].Path = "mutated"
	if !reflect.DeepEqual(retainedSources(t, err), want) {
		t.Fatal("snapshot metadata is borrowed")
	}
}

func TestResolveFailureRetainsTypedCausesAndExactSources(t *testing.T) {
	sentinel := errors.New("provider failed")
	for _, tc := range []struct {
		name, source string
		providers    engine.ValidationProviders
		cause        error
		setting      bool
	}{
		{name: "setting", source: "version: 1\r\n# 雪\r\nrules:\r\n  MD013: {length: -1}\r\n", setting: true},
		{name: "provider", source: "version: 1\nrules:\n  MD095:\n    enabled: false\n    allowed_hosts: [example.com]\n", providers: engine.ValidationProviders{ValidateHostname: func(string) error { return sentinel }}, cause: sentinel, setting: true},
		{name: "capability", source: "version: 1\nflavor: pandoc\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := discoveryRoot(t)
			path := discoveryFile(t, root, Filename, tc.source)
			_, err := Resolve(SelectionOptions{WorkingDirectory: root, BoundaryDirectory: root}, registryFor(t), tc.providers, baseCaps())
			var validation *ValidationError
			if !errors.As(err, &validation) || validation.Origin.Path != path || validation.Origin.Position == nil {
				t.Fatalf("lost validation origin: %v", err)
			}
			if tc.cause != nil && !errors.Is(err, tc.cause) {
				t.Fatalf("lost provider identity: %v", err)
			}
			var setting *engine.SettingError
			if tc.setting && !errors.As(err, &setting) {
				t.Fatalf("lost engine setting error: %v", err)
			}
			if err := os.WriteFile(path, []byte("replaced"), 0600); err != nil {
				t.Fatal(err)
			}
			if got, want := retainedSources(t, err), []SourceSnapshot{snapshotSource(path, []byte(tc.source))}; !reflect.DeepEqual(got, want) {
				t.Fatalf("sources = %#v, want %#v", got, want)
			}
		})
	}
}

func TestPolicyFailureRetainsPrimaryAndGoverningAcquisitions(t *testing.T) {
	root := discoveryRoot(t)
	parentSource := "version: 1\nallow_nested_excludes: false\n"
	parent := discoveryFile(t, root, Filename, parentSource)
	child := discoveryDir(t, root, "子😀")
	childSource := "version: 1\nexclude: [blocked]\n"
	selected := discoveryFile(t, child, Filename, childSource)
	_, err := Resolve(SelectionOptions{WorkingDirectory: child, BoundaryDirectory: root}, registryFor(t), engine.ValidationProviders{}, baseCaps())
	var policy *PolicyError
	if !errors.As(err, &policy) || policy.Origin.Path != selected || policy.Origin.Position == nil || len(policy.Restrictions) != 1 || policy.Restrictions[0].Path != parent || policy.Restrictions[0].Position == nil {
		t.Fatalf("lost policy origins: %v", err)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(selected); err != nil {
		t.Fatal(err)
	}
	want := []SourceSnapshot{snapshotSource(parent, []byte(parentSource)), snapshotSource(selected, []byte(childSource))}
	if got := retainedSources(t, err); !reflect.DeepEqual(got, want) {
		t.Fatalf("sources = %#v, want %#v", got, want)
	}
}

func TestUnreadConfigurationHasNoInventedSnapshot(t *testing.T) {
	root := discoveryRoot(t)
	_, err := Load(filepath.Join(root, "missing.yaml"))
	var acquired *SourceError
	if !errors.Is(err, fs.ErrNotExist) || errors.As(err, &acquired) {
		t.Fatalf("missing source = %v", err)
	}
	path := filepath.Join(root, Filename)
	sentinel := errors.New("denied")
	access := &fixtureAccess{files: fstest.MapFS{fixtureName(root): {Mode: fs.ModeDir | 0700}, fixtureName(path): {Data: []byte("version: 1\n")}}, readFailures: map[string]error{path: sentinel}}
	_, err = resolveFS(access, root, root, path, registryFor(t), engine.ValidationProviders{}, baseCaps())
	var validation *ValidationError
	if !errors.Is(err, sentinel) || errors.As(err, &acquired) || !errors.As(err, &validation) || validation.Origin.Path != path || validation.Origin.Position != nil {
		t.Fatalf("unread source = %v", err)
	}
}

func TestUnreadSelectedSourceDoesNotBorrowAncestorSnapshot(t *testing.T) {
	root := discoveryRoot(t)
	child := filepath.Join(root, "child")
	parent := filepath.Join(root, Filename)
	selected := filepath.Join(child, Filename)
	source := []byte("version: 1\nallow_nested_excludes: false\n")
	sentinel := errors.New("selected read denied")
	access := &fixtureAccess{files: fstest.MapFS{
		fixtureName(root):     {Mode: fs.ModeDir | 0700},
		fixtureName(child):    {Mode: fs.ModeDir | 0700},
		fixtureName(parent):   {Data: source},
		fixtureName(selected): {Data: []byte("version: 1\n")},
	}, readFailures: map[string]error{selected: sentinel}}
	_, err := resolveFS(access, child, root, selected, registryFor(t), engine.ValidationProviders{}, baseCaps())
	var validation *ValidationError
	if !errors.Is(err, sentinel) || !errors.As(err, &validation) || validation.Origin.Path != selected || validation.Origin.Position != nil {
		t.Fatalf("read failure origin = %v", err)
	}
	if got, want := retainedSources(t, err), []SourceSnapshot{snapshotSource(parent, source)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sources = %#v, want only acquired ancestor %#v", got, want)
	}
}
