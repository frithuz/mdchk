//go:build linux || darwin

package resource

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A separate instance of this exact test binary gives the FIFO assertion a real
// kernel-level completion bound even if a regression removes nonblocking flags.
// It launches no descendants and receives no service credentials/environment.
func TestNativeResourceFIFONeverBlocks(t *testing.T) {
	if os.Getenv("MDCHK_RESOURCE_FIFO_CHILD") != "1" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, executable, "-test.run=^TestNativeResourceFIFONeverBlocks$", "-test.count=1") //nolint:gosec // Exact current test executable and fixed arguments, with no shell or descendants.
		command.Dir = t.TempDir()
		command.Env = []string{"MDCHK_RESOURCE_FIFO_CHILD=1", "TMPDIR=" + os.TempDir()}
		output := &nativeTestOutput{}
		command.Stdout = output
		command.Stderr = output
		command.WaitDelay = time.Second
		if err := command.Run(); err != nil {
			t.Fatalf("native FIFO check did not finish successfully: %v (deadline=%v)\n%s", err, ctx.Err(), output.buffer.String())
		}
		return
	}
	for _, stage := range []string{"existing", "before-open", "before-content-open"} {
		t.Run(stage, func(t *testing.T) {
			f := newFilesystemFixture(t)
			path := filepath.Join(f.dir, "fifo.md")
			if stage != "existing" {
				f.write(t, "fifo.md", []byte("original"))
				changed := false
				f.filesystem.hook = func(at, key string) error {
					if !changed && at == stage && key == "fifo.md" {
						changed = true
						if err := os.Remove(path); err != nil {
							return err
						}
						return unix.Mkfifo(path, 0600)
					}
					return nil
				}
			} else if err := unix.Mkfifo(path, 0600); err != nil {
				t.Fatal(err)
			}
			readCount := 0
			read := f.filesystem.read
			f.filesystem.read = func(file *os.File, data []byte) (int, error) { readCount++; return read(file, data) }
			observation, err := f.observe(t, f.request(t, "fifo.md", TextContent, RegularFile))
			if err != nil || observation.Existence() != Unknown || observation.Problem().Kind != Blocked || readCount != 0 {
				t.Fatal("special file was read or treated as absence", observation, err, readCount)
			}
		})
	}
}

type nativeTestOutput struct{ buffer bytes.Buffer }

func (b *nativeTestOutput) Write(data []byte) (int, error) {
	n := len(data)
	remaining := 32768 - b.buffer.Len()
	if len(data) > remaining {
		data = data[:remaining]
	}
	_, err := b.buffer.Write(data)
	return n, err
}

func TestNativeResourceRealPermissionPreservesExistence(t *testing.T) {
	// Never run as a privilege-bypass identity and call that permission proof.
	// CI's ordinary user and the local user exercise this real file permission;
	// injected errors separately cover the same outcome on other configurations.
	if os.Geteuid() == 0 {
		t.Fatal("native permission proof requires an unprivileged test user")
	}
	f := newFilesystemFixture(t)
	f.write(t, "private.md", []byte("unreadable content"))
	path := filepath.Join(f.dir, "private.md")
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(path, 0600); err != nil {
			t.Error(err)
		}
	})
	existence, err := f.observe(t, f.request(t, "private.md", ExistenceOnly, RegularFile))
	if err != nil || existence.Existence() != File || existence.Treatment() != Complete {
		t.Fatal("metadata required read-data privilege", existence, err)
	}
	content, err := f.observe(t, f.request(t, "private.md", TextContent, RegularFile))
	if !errors.Is(err, os.ErrPermission) || content.Existence() != File || content.ContentAvailability() != Failed {
		t.Fatal("permission became absence/unknown", content, err)
	}
}

func TestNativeResourceUnreadableWorktreeMarker(t *testing.T) {
	base := rootPolicyTemp(t)
	start := rootPolicyDir(t, filepath.Join(base, "docs"))
	marker := rootPolicyFile(t, filepath.Join(base, ".git"), "gitdir: /unopened/pointer\n")
	if err := os.Chmod(marker, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(marker, 0600); err != nil {
			t.Error(err)
		}
	})
	rootPolicySelect(t, RootSelectionOptions{Mode: RootCLI, WorkingDirectory: start}, base)
}

func TestNativeResourceHardLinksKeepLogicalIdentity(t *testing.T) {
	f := newFilesystemFixture(t)
	f.write(t, "first.md", []byte("old"))
	if err := os.Link(filepath.Join(f.dir, "first.md"), filepath.Join(f.dir, "second.md")); err != nil {
		t.Fatal(err)
	}
	first, err := f.observe(t, f.request(t, "first.md", TextContent, RegularFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.dir, "second.md")); err != nil {
		t.Fatal(err)
	}
	f.write(t, "second.md", []byte("replacement"))
	second, err := f.observe(t, f.request(t, "second.md", TextContent, RegularFile))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := first.Content()
	b, _ := second.Content()
	if string(a.Bytes()) != "old" || string(b.Bytes()) != "replacement" || first.Request().Key() == second.Request().Key() {
		t.Fatal("hard-linked logical entries were merged", first, second)
	}
}
