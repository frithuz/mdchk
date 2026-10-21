package configscan

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

type effectAccess struct {
	scanAccess
	stat    func(string)
	opening func(string, bool)
}

func (a effectAccess) lstat(path string) (fs.FileInfo, error) {
	if a.stat != nil {
		a.stat(path)
	}
	return a.scanAccess.lstat(path)
}
func (a effectAccess) open(path string, dir bool) (readHandle, error) {
	if a.opening != nil {
		a.opening(path, dir)
	}
	return a.scanAccess.open(path, dir)
}
func TestOwnSourcesBoundedBeforeEffects(t *testing.T) {
	for _, name := range []string{config.Filename, ".gitignore"} {
		for _, kind := range []string{"depth", "bytes"} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				root := temp(t)
				relative := "child/" + name
				fixture(t, root, relative, "#source\n")
				target := filepath.Join(root, filepath.FromSlash(relative))
				l := DefaultLimits()
				if kind == "depth" {
					l.Depth = 1
				} else {
					l.PathBytes = int64(len(relative) - 1)
				}
				effects, hooks := 0, 0
				r := scan(context.Background(), Options{Root: root, Environment: environment(t), Limits: l}, Hooks{Definition: func(_ Definition, s config.PolicyState) (config.PolicyState, error) { hooks++; return s, nil }}, func(a scanAccess) scanAccess {
					return effectAccess{a, func(p string) {
						if p == target {
							effects++
						}
					}, func(p string, _ bool) {
						if p == target {
							effects++
						}
					}}
				})
				var limit *parser.LimitExceeded
				if !errors.As(r.Interruption, &limit) || effects != 0 || hooks != 0 || len(r.Definitions) != 0 {
					t.Fatalf("r=%+v effects=%d hooks=%d", r, effects, hooks)
				}
				if kind == "depth" {
					l.Depth = 2
				} else {
					l.PathBytes = int64(len(relative))
				}
				r = Scan(context.Background(), Options{Root: root, Environment: environment(t), Limits: l}, Hooks{})
				if !r.Complete || r.Interruption != nil {
					t.Fatalf("exact source bound %+v", r)
				}
			})
		}
	}
	root := temp(t)
	if e := os.Mkdir(filepath.Join(root, "child"), 0700); e != nil {
		t.Fatal(e)
	}
	l := DefaultLimits()
	l.Depth = 1
	l.PathBytes = int64(len("child"))
	r := Scan(context.Background(), Options{Root: root, Environment: environment(t), Limits: l}, Hooks{})
	if !r.Complete || r.Interruption != nil {
		t.Fatalf("missing hypothetical sources %+v", r)
	}
}
func TestSymlinkDefinitionIsOneSkipWithoutErrorOrCallback(t *testing.T) {
	root := temp(t)
	outside := temp(t)
	fixture(t, outside, config.Filename, "outside")
	if e := os.Symlink(filepath.Join(outside, config.Filename), filepath.Join(root, config.Filename)); e != nil {
		t.Skip(e)
	}
	calls := 0
	r := Scan(context.Background(), Options{Root: root, Environment: environment(t)}, Hooks{Definition: func(_ Definition, s config.PolicyState) (config.PolicyState, error) { calls++; return s, nil }})
	if !r.Complete || r.Interruption != nil || len(r.Errors) != 0 || len(r.Definitions) != 0 || calls != 0 || len(r.Skipped) != 1 || r.Skipped[0].Reason != "symlink" {
		t.Fatalf("%+v calls%d", r, calls)
	}
}
func TestSemanticRejectionRetainsCompleteAndPriorState(t *testing.T) {
	root := temp(t)
	fixture(t, root, config.Filename, "root")
	fixture(t, root, "child/"+config.Filename, "child")
	registry, e := engine.NewRegistry(engine.Catalog(), nil)
	if e != nil {
		t.Fatal(e)
	}
	d, e := config.DecodeYAML("rejected", []byte("version: 1\nexclude: [child/]\n"))
	if e != nil {
		t.Fatal(e)
	}
	rejected, _, e := (config.PolicyState{}).Apply(d, registry, engine.ValidationProviders{}, config.Capabilities{})
	if e != nil {
		t.Fatal(e)
	}
	sentinel := errors.New("semantic rejection")
	calls := 0
	r := Scan(context.Background(), Options{Root: root, Environment: environment(t)}, Hooks{Definition: func(d Definition, s config.PolicyState) (config.PolicyState, error) {
		calls++
		if d.Path == filepath.Join(root, config.Filename) {
			return rejected, sentinel
		}
		if len(s.Exclusions()) != 0 {
			t.Fatal("rejected state activated")
		}
		return s, nil
	}})
	if !r.Complete || len(r.Errors) != 1 || !errors.Is(r.Errors[0], sentinel) || r.Interruption != nil || len(r.Definitions) != 2 || calls != 2 {
		t.Fatalf("%+v calls%d", r, calls)
	}
}
func TestWinningGitSettingSourceAndKey(t *testing.T) {
	for _, key := range []string{"ignoreCase", "excludesFile"} {
		for _, winner := range []string{"global", "local", "worktree"} {
			t.Run(key+"/"+winner, func(t *testing.T) {
				root := temp(t)
				env := environment(t)
				global := env["GIT_CONFIG_GLOBAL"]
				local := filepath.Join(root, ".git", "config")
				worktree := filepath.Join(root, ".git", "config.worktree")
				paths := map[string]string{"global": global, "local": local, "worktree": worktree}
				bad := "invalid"
				if key == "excludesFile" {
					bad = "relative-ignore"
				}
				fixture(t, root, ".git/config", "[core]\n")
				fixture(t, filepath.Dir(global), filepath.Base(global), "[core]\n")
				if winner == "worktree" {
					fixture(t, root, ".git/config", "[extensions]\nworktreeConfig = true\n")
				}
				fixture(t, filepath.Dir(paths[winner]), filepath.Base(paths[winner]), "[core]\n"+key+" = "+bad+"\n")
				r := Scan(context.Background(), Options{Root: root, Environment: env}, Hooks{})
				var setting *GitSettingError
				if r.Complete || len(r.Errors) != 1 || r.Errors[0].Path != paths[winner] || !errors.As(r.Errors[0], &setting) || setting.Path != paths[winner] || setting.Key != "core."+strings.ToLower(key) {
					t.Fatalf("%+v typed%+v", r, setting)
				}
				// A higher-priority valid source overrides an invalid lower-priority value.
				if winner == "global" {
					valid := "false"
					if key == "excludesFile" {
						valid = "\"\""
					}
					fixture(t, root, ".git/config", "[core]\n"+key+" = "+valid+"\n")
					r = Scan(context.Background(), Options{Root: root, Environment: env}, Hooks{})
					if !r.Complete {
						t.Fatalf("valid override %+v", r)
					}
				}
			})
		}
	}
}
func TestCancellationAtFirstAndFinalEntryStat(t *testing.T) {
	for _, at := range []int{1, 3} {
		t.Run(strconv.Itoa(at), func(t *testing.T) {
			root := temp(t)
			for _, name := range []string{"a", "b", "c"} {
				fixture(t, root, name, "data")
			}
			ctx, cancel := context.WithCancel(context.Background())
			stats := 0
			r := scan(ctx, Options{Root: root, Environment: environment(t)}, Hooks{}, func(a scanAccess) scanAccess {
				return effectAccess{scanAccess: a, stat: func(p string) {
					if filepath.Dir(p) == root && len(filepath.Base(p)) == 1 {
						stats++
						if stats == at {
							cancel()
						}
					}
				}}
			})
			if r.Complete || !errors.Is(r.Interruption, context.Canceled) || stats != at {
				t.Fatalf("%+v stats%d", r, stats)
			}
		})
	}
}
func TestPostSourceIdentityCancellationDoesNotPublishOrCallHook(t *testing.T) {
	root := temp(t)
	fixture(t, root, config.Filename, "definition")
	ctx, cancel := context.WithCancel(context.Background())
	stats, calls := 0, 0
	target := filepath.Join(root, config.Filename)
	r := scan(ctx, Options{Root: root, Environment: environment(t)}, Hooks{Definition: func(_ Definition, s config.PolicyState) (config.PolicyState, error) { calls++; return s, nil }}, func(a scanAccess) scanAccess {
		return effectAccess{scanAccess: a, stat: func(p string) {
			if p == target {
				stats++
				if stats == 2 {
					cancel()
				}
			}
		}}
	})
	if r.Complete || !errors.Is(r.Interruption, context.Canceled) || len(r.Definitions) != 0 || calls != 0 {
		t.Fatalf("%+v stats%d calls%d", r, stats, calls)
	}
}

func TestEnumerationFirstPreservesNestedMetadataIdentity(t *testing.T) {
	for _, marker := range []string{".git", ".GIT"} {
		t.Run(marker, func(t *testing.T) {
			root := temp(t)
			fixture(t, root, "child/"+config.Filename, "child")
			fixture(t, root, "child/"+marker+"/config", "metadata")
			_, e := os.Lstat(filepath.Join(root, "child", ".git"))
			isRepository := e == nil
			r := Scan(context.Background(), Options{Root: root, Environment: environment(t)}, Hooks{})
			if !r.Complete {
				t.Fatalf("%+v", r)
			}
			if isRepository {
				if len(r.Definitions) != 0 || len(r.Skipped) != 1 || r.Skipped[0].Reason != "nested repository" {
					t.Fatalf("nested %+v", r)
				}
			} else if len(r.Definitions) != 1 {
				t.Fatalf("distinct case-sensitive marker %+v", r)
			}
		})
	}
}
