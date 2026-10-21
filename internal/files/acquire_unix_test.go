//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package files

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestAcquireRejectsReplacedSelectedPaths(t *testing.T) {
	for _, kind := range []string{"final-symlink", "final-regular", "ancestor-symlink", "ancestor-regular"} {
		t.Run(kind, func(t *testing.T) {
			root := selectionRoot(t)
			path := selectionFile(t, root, "parent/target.md", []byte("selected bytes"))
			other := selectionFile(t, root, "other/target.md", []byte("redirected bytes"))
			good := selectionFile(t, root, "good.md", []byte("independent"))
			selection := selectFixture(t, Options{WorkingDirectory: root, Paths: []string{path, good}})
			if kind == "final-symlink" || kind == "final-regular" {
				if err := os.Rename(path, path+".old"); err != nil {
					t.Fatal(err)
				}
				if kind == "final-symlink" {
					if err := os.Symlink(other, path); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.WriteFile(path, []byte("replacement regular file"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				parent := filepath.Dir(path)
				if err := os.Rename(parent, parent+".old"); err != nil {
					t.Fatal(err)
				}
				if kind == "ancestor-symlink" {
					if err := os.Symlink(filepath.Dir(other), parent); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Mkdir(parent, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.Link(filepath.Join(parent+".old", "target.md"), path); err != nil {
						t.Fatal(err)
					}
				}
			}
			got := acquireFixture(t, selection, AcquireOptions{})
			if len(got.Documents) != 1 || got.Documents[0].Path != good {
				t.Fatalf("replacement accepted or independent target lost: %+v", got.Documents)
			}
			if len(got.Errors) != 1 || got.Errors[0].Path != path {
				t.Fatalf("replacement missing IO failure at selected path: %v", got.Errors)
			}
			if len(got.PreflightErrors) != 0 || len(got.Interruptions) != 0 {
				t.Fatalf("replacement misclassified: %+v", got)
			}
		})
	}
}

// The child creates the FIFO only after selection. A subprocess deadline makes
// a blocking-open regression fail without leaving a stuck test goroutine.
func TestAcquireSelectedFileReplacedByFIFO(t *testing.T) {
	const marker = "MDCHK_TEST_ACQUIRE_FIFO_CHILD"
	if os.Getenv(marker) == "1" {
		root := selectionRoot(t)
		path := selectionFile(t, root, "target.md", []byte("selected"))
		selection := selectFixture(t, Options{WorkingDirectory: root})
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
		got := acquireFixture(t, selection, AcquireOptions{})
		if len(got.Documents) != 0 || len(got.Errors) != 1 || got.Errors[0].Path != path {
			t.Fatalf("FIFO accepted: %+v", got)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	//nolint:gosec // Relaunch the current test binary with fixed test arguments and a timeout.
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAcquireSelectedFileReplacedByFIFO$", "-test.count=1")
	command.Env = append(os.Environ(), marker+"=1")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("acquisition blocked on replaced FIFO: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("FIFO child: %v\n%s", err, output)
	}
}

type acquireReplaceOnRead struct {
	readHandle
	replace func() error
	done    bool
}

func (h *acquireReplaceOnRead) Read(p []byte) (int, error) {
	if !h.done {
		h.done = true
		if err := h.replace(); err != nil {
			return 0, err
		}
	}
	return h.readHandle.Read(p)
}
func (h *acquireReplaceOnRead) Verify() error {
	return h.readHandle.(interface{ Verify() error }).Verify()
}

func TestAcquireRejectsPathsReplacedDuringRead(t *testing.T) {
	for _, kind := range []string{"final", "ancestor"} {
		t.Run(kind, func(t *testing.T) {
			root := selectionRoot(t)
			path := selectionFile(t, root, "parent/target.md", []byte("original descriptor bytes"))
			good := selectionFile(t, root, "good.md", []byte("independent"))
			selection := selectFixture(t, Options{WorkingDirectory: root, Paths: []string{path, good}})
			got, err := acquireFS(context.Background(), selection, AcquireOptions{}, func(target Target) (readHandle, error) {
				handle, err := openTarget(target)
				if err != nil || target.Path != path {
					return handle, err
				}
				return &acquireReplaceOnRead{readHandle: handle, replace: func() error {
					if kind == "final" {
						if err := os.Rename(path, path+".old"); err != nil {
							return err
						}
						return os.WriteFile(path, []byte("redirected bytes"), 0600)
					}
					parent := filepath.Dir(path)
					if err := os.Rename(parent, parent+".old"); err != nil {
						return err
					}
					if err := os.Mkdir(parent, 0700); err != nil {
						return err
					}
					return os.Link(filepath.Join(parent+".old", "target.md"), path)
				}}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Documents) != 1 || got.Documents[0].Path != good || len(got.Errors) != 1 || got.Errors[0].Path != path {
				t.Fatalf("changed lookup accepted original pinned descriptor as current target: %+v", got)
			}
		})
	}
}

func TestAcquireRejectsNewGitMetadataAlias(t *testing.T) {
	root := selectionRoot(t)
	path := selectionFile(t, root, ".GIT", []byte("not metadata at selection"))
	selection := selectFixture(t, Options{WorkingDirectory: root, Paths: []string{path}})
	if len(selection.Targets) == 0 {
		t.Skip("case-insensitive filesystem already identifies .GIT as .git")
	}
	if err := os.Link(path, filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	got := acquireFixture(t, selection, AcquireOptions{})
	if len(got.Documents) != 0 || len(got.Errors) != 1 || got.Errors[0].Path != path {
		t.Fatalf("new metadata alias accepted: %+v", got)
	}
}
