package files

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitInspectionRejectsIdentitySwaps(t *testing.T) {
	for _, kind := range []string{"file", "symlink", "parent"} {
		t.Run(kind, func(t *testing.T) {
			root := selectionRoot(t)
			path := selectionFile(t, root, "parent/x.txt", []byte("original"))
			other := selectionFile(t, root, "other.txt", []byte("replacement"))
			out, err := acquireExplicit(context.Background(), path, AcquireOptions{}, func(Target) {
				switch kind {
				case "file":
					if err := os.Rename(other, path); err != nil {
						t.Fatal(err)
					}
				case "symlink":
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(other, path); err != nil {
						t.Fatal(err)
					}
				case "parent":
					parent := filepath.Dir(path)
					if err := os.Rename(parent, parent+"-old"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(parent, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte("replacement"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Documents) != 0 || len(out.Errors) != 1 {
				t.Fatalf("raced target accepted: %+v", out)
			}
		})
	}
}

type changingInspectionHandle struct {
	readHandle
	path    string
	changed bool
}

func (h *changingInspectionHandle) Read(data []byte) (int, error) {
	n, err := h.readHandle.Read(data)
	if !h.changed {
		h.changed = true
		if changeErr := os.WriteFile(h.path, []byte("changed during acquisition"), 0600); changeErr != nil {
			return n, changeErr
		}
	}
	return n, err
}
func TestInspectionSharedReaderRejectsChangingFile(t *testing.T) {
	root := selectionRoot(t)
	path := selectionFile(t, root, "x.md", []byte("original"))
	selection := selectFixture(t, Options{WorkingDirectory: root, Paths: []string{path}})
	out, err := acquireFS(context.Background(), selection, AcquireOptions{}, func(target Target) (readHandle, error) {
		handle, err := openTarget(target)
		if err != nil {
			return nil, err
		}
		return &changingInspectionHandle{readHandle: handle, path: path}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Documents) != 0 || len(out.Errors) != 1 {
		t.Fatalf("changing read accepted: %+v", out)
	}
}

func TestAcquireExplicitRawSymlinkDotDot(t *testing.T) {
	root := selectionRoot(t)
	physical := selectionFile(t, root, "elsewhere/target.md", []byte("physical"))
	selectionFile(t, root, "target.md", []byte("wrong lexical"))
	if err := os.Mkdir(filepath.Join(root, "elsewhere", "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "elsewhere", "dir"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	// An OS-working-directory-relative request also exercises the direct API,
	// independently of inspect.Options.WorkingDirectory.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root + "/link/../target.md", relative + "/link/../target.md"} {
		out, err := AcquireExplicit(context.Background(), path, AcquireOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Documents) != 1 || len(out.Errors) != 0 || out.Documents[0].Path != physical || string(out.Documents[0].Source.Bytes()) != "physical" {
			t.Fatalf("wrong direct target: %+v", out)
		}
	}
}
