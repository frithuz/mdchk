//go:build linux || darwin || windows

package resource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

func projectNativeFixture(t *testing.T) (*filesystemFixture, *Service, ProjectSource) {
	t.Helper()
	f := newFilesystemFixture(t)
	if e := os.WriteFile(filepath.Join(f.dir, "mkdocs.yml"), []byte("docs_dir: docs\nnav: []\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(filepath.Join(f.dir, "docs"), 0700); e != nil {
		t.Fatal(e)
	}
	service := filesystemTestService(t, f)
	key, _ := NewKey("mkdocs.yml")
	source, spec, e := service.AcquireSource(context.Background(), f.view, key)
	if e != nil || spec.Content != Available || source.Source().Token() == (parser.SourceToken{}) {
		t.Fatal(source, spec, e)
	}
	return f, service, source
}
func projectFactNames(i NameInventory) map[string]Existence {
	out := map[string]Existence{}
	for n := 0; n < i.KnownCount(); n++ {
		f, _ := i.KnownFact(n)
		out[f.Key().String()] = f.Existence()
	}
	return out
}

func TestFilesystemProjectInventoryConfinedNamesAndNoFollow(t *testing.T) {
	for _, link := range []string{"none", "inside", "outside", "dangling"} {
		t.Run(link, func(t *testing.T) {
			f, _, source := projectNativeFixture(t)
			for name, contents := range map[string]string{"docs/guide.md": "page", "docs/binary.bin": "\xff\x00", "docs/sub/index.md": "page", "docs/.git/private.md": "private"} {
				full := filepath.Join(f.dir, filepath.FromSlash(name))
				if e := os.MkdirAll(filepath.Dir(full), 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(full, []byte(contents), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if link != "none" {
				target := filepath.Join(f.dir, "docs", "sub")
				if link == "outside" {
					target = t.TempDir()
				}
				if link == "dangling" {
					target = filepath.Join(f.dir, "absent")
				}
				if e := os.Symlink(target, filepath.Join(f.dir, "docs", "link")); e != nil {
					t.Fatalf("critical symlink unavailable: %v", e)
				}
			}
			directory, e := source.DirectoryLocation(context.Background(), "docs")
			if e != nil {
				t.Fatal(e)
			}
			inventory, e := source.Enumerate(context.Background(), directory)
			if e != nil {
				t.Fatal(e)
			}
			expected := map[string]Existence{"docs/guide.md": File, "docs/binary.bin": File, "docs/sub": Directory, "docs/sub/index.md": File}
			if got := projectFactNames(inventory); !reflect.DeepEqual(got, expected) {
				t.Fatal("confined exact inventory", got, expected)
			}
			if inventory.Complete() != (link == "none") {
				t.Fatal("unavailable branch completeness", link, inventory.Complete())
			}
			copied, e := inventory.Facts(context.Background(), source)
			if e != nil {
				t.Fatal(e)
			}
			copied[0] = NameFact{}
			if !reflect.DeepEqual(projectFactNames(inventory), expected) {
				t.Fatal("mutable returned facts")
			}
			if link != "none" {
				location, e := source.DirectoryLocation(context.Background(), "docs/link/index.md")
				if e != nil {
					t.Fatal(e)
				}
				spec, e := source.Inspect(context.Background(), location, RegularFile)
				if e != nil || spec.Existence != Unknown || spec.Problem.Kind != Blocked {
					t.Fatal("symlink read", spec, e)
				}
			}
		})
	}
}

func TestFilesystemProjectInventoryLimitsInstabilityAndStops(t *testing.T) {
	for _, cause := range []string{"entries", "identities", "cancel", "close", "instability"} {
		t.Run(cause, func(t *testing.T) {
			f, service, source := projectNativeFixture(t)
			for _, name := range []string{"a.md", "b.md", "c.md"} {
				if e := os.WriteFile(filepath.Join(f.dir, "docs", name), []byte("page"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			directory, e := source.DirectoryLocation(context.Background(), "docs")
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			f.filesystem.hook = func(stage, key string) error {
				if stage != "inventory-entry" {
					return nil
				}
				calls++
				if calls != 2 {
					return nil
				}
				switch cause {
				case "entries":
					f.filesystem.limits.DirectoryEntries = f.filesystem.entries
				case "identities":
					service.mu.Lock()
					service.limits.Identities = int64(len(service.identities))
					service.mu.Unlock()
				case "cancel":
					cancel()
				case "close":
					if e := service.Close(); e != nil {
						t.Fatal(e)
					}
				case "instability":
					return errFilesystemUnstable
				}
				return nil
			}
			inventory, e := source.Enumerate(ctx, directory)
			if inventory.Complete() || e == nil || inventory.KnownCount() == 0 {
				t.Fatal("lost partial known facts/stop", inventory.Complete(), inventory.KnownCount(), e)
			}
			switch cause {
			case "entries", "identities":
				var limit *parser.LimitExceeded
				var input *InputError
				if !errors.As(e, &limit) || errors.As(e, &input) {
					t.Fatal("runtime quota became usage", e)
				}
			case "cancel", "close":
				if !errors.Is(e, context.Canceled) {
					t.Fatal("lost cancel", e)
				}
			case "instability":
				if !errors.Is(e, errFilesystemUnstable) {
					t.Fatal("lost instability", e)
				}
			}
			known, _ := inventory.KnownFact(0)
			if known.Key().String() == "" {
				t.Fatal("lost warm name")
			}
			if cause != "instability" {
				before := calls
				_, stopped := source.Enumerate(ctx, directory)
				if stopped == nil || calls != before {
					t.Fatal("new work after stop", calls, before, stopped)
				}
			}
		})
	}
}

func TestFilesystemProjectInventoryConcurrentReadIdentityLimit(t *testing.T) {
	f, service, source := projectNativeFixture(t)
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		if e := os.WriteFile(filepath.Join(f.dir, "docs", name), []byte("page"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	directory, e := source.DirectoryLocation(context.Background(), "docs")
	if e != nil {
		t.Fatal(e)
	}
	service.mu.Lock()
	service.limits.Identities = int64(len(service.identities)) + 2
	service.mu.Unlock()
	var group sync.WaitGroup
	group.Add(2)
	var inventory NameInventory
	var inventoryErr, readErr error
	go func() {
		defer group.Done()
		inventory, inventoryErr = source.Enumerate(context.Background(), directory)
	}()
	go func() {
		defer group.Done()
		location, e := source.DirectoryLocation(context.Background(), "docs/c.md")
		if e != nil {
			readErr = e
			return
		}
		_, readErr = source.Inspect(context.Background(), location, RegularFile)
	}()
	group.Wait()
	var limit *parser.LimitExceeded
	if inventory.Complete() || !errors.As(errors.Join(inventoryErr, readErr), &limit) || limit.Name != "resource-identities" {
		t.Fatal("concurrent shared identity quota", inventory.Complete(), inventoryErr, readErr)
	}
	_, later := source.Enumerate(context.Background(), directory)
	var laterLimit *parser.LimitExceeded
	if !errors.As(later, &laterLimit) || *laterLimit != *limit {
		t.Fatal("first quota lost", later, limit)
	}
}

func TestFilesystemProjectInventoryKnownFactsAndCompoundControls(t *testing.T) {
	f, service, source := projectNativeFixture(t)
	if e := os.WriteFile(filepath.Join(f.dir, "docs", "known.md"), []byte("page"), 0600); e != nil {
		t.Fatal(e)
	}
	directory, e := source.DirectoryLocation(context.Background(), "docs")
	if e != nil {
		t.Fatal(e)
	}
	warm, e := source.Enumerate(context.Background(), directory)
	if e != nil || !warm.Complete() {
		t.Fatal(e)
	}
	if e := service.Close(); e != nil {
		t.Fatal(e)
	}
	first := f.budget.ChargeWork(f.budget.Limits().WorkUnits + 1)
	expired, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer cancel()
	cold, e := source.Enumerate(expired, directory)
	var actual, want *parser.LimitExceeded
	if cold.Complete() || !errors.Is(e, context.Canceled) || !errors.Is(e, context.DeadlineExceeded) || !errors.As(e, &actual) || !errors.As(first, &want) || *actual != *want {
		t.Fatal("lost independent compound controls", e, first)
	}
	if warm.KnownCount() != 1 {
		t.Fatal("lost warm known names")
	}
	fact, _ := warm.KnownFact(0)
	if fact.Key().String() != "docs/known.md" {
		t.Fatal(fact)
	}
}
