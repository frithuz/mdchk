//go:build darwin && arm64 && cgo

package files

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestNativeCheckedDescriptorLifecycle(t *testing.T) {
	for _, boundary := range []string{"created", "revalidation", "stage", "verification", "saved-parent", "saved", "cleanup", "shutdown"} {
		t.Run(boundary, func(t *testing.T) {
			root, path := nativeFixture(t)
			set, a := nativeAcquire(t, []string{path}, WriteOptions{})
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			sentinel := errors.New("descriptor close failed")
			primary := errors.New("stage identity failed")
			seen := false
			set.native.closeFile = func(stage string, f *os.File) error {
				err := f.Close()
				if stage == boundary {
					seen = true
					return errors.Join(err, sentinel)
				}
				return err
			}
			if boundary == "created" {
				set.native.statFile = func(stage string, f *os.File) (fs.FileInfo, error) {
					if stage == "created" {
						return nil, primary
					}
					return f.Stat()
				}
			}
			if boundary == "cleanup" {
				set.native.write = func(f *os.File, b []byte) (int, error) { return f.Write(b[:1]) }
			}
			if boundary == "shutdown" {
				if err := set.Close(); !errors.Is(err, sentinel) {
					t.Fatalf("shutdown lost error: %v", err)
				}
				if err := set.Close(); err != nil {
					t.Fatalf("second close: %v", err)
				}
				out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("new\n")), nil)
				if out.Err == nil || out.Published {
					t.Fatalf("closed acquisition reused: %+v", out)
				}
				return
			}
			out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("new\n")), nil)
			if !seen || !errors.Is(out.Err, sentinel) {
				t.Fatalf("close error lost: %+v seen=%v", out, seen)
			}
			published := boundary == "saved" || boundary == "saved-parent"
			if out.Published != published || out.Uncertain {
				t.Fatalf("incorrect publication evidence: %+v", out)
			}
			if published {
				if string(out.Saved.Bytes()) != "new\n" {
					t.Fatalf("observed saved bytes lost: %+v", out)
				}
				assertNativeBytes(t, path, []byte("new\n"))
			} else {
				assertNativeBytes(t, path, a.Documents[0].Source.Bytes())
				after, err := os.Stat(path)
				if err != nil || !os.SameFile(before, after) {
					t.Fatal("prepublication changed original inode", err)
				}
				if out.Saved != (parser.Source{}) {
					t.Fatal("invented saved snapshot")
				}
			}
			if boundary == "created" || boundary == "cleanup" {
				if len(out.Leftovers) != 1 {
					t.Fatalf("missing owned leftover: %+v", out)
				}
				if boundary == "created" && !errors.Is(out.Err, primary) {
					t.Fatal("lost primary stat failure")
				}
				if boundary == "cleanup" && !errors.Is(out.Err, io.ErrShortWrite) {
					t.Fatal("lost primary short write")
				}
				if _, err := os.Stat(filepath.Join(root, out.Leftovers[0])); err != nil {
					t.Fatal("reported leftover absent", err)
				}
			} else {
				assertNoStage(t, root)
			}
		})
	}
}

func TestNativePartialWriteAndCleanupFailureRetainOwnedLeftover(t *testing.T) {
	root, path := nativeFixture(t)
	foreign := filepath.Join(root, ".mdchk-stage-foreign")
	if err := os.WriteFile(foreign, []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	cleanupErr := errors.New("native unlink refused")
	set.native.write = func(f *os.File, b []byte) (int, error) { return f.Write(b[:2]) }
	set.native.removeStage = func(parent *os.File, name string) error { return cleanupErr }
	out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("replacement\r\n")), nil)
	if !errors.Is(out.Err, io.ErrShortWrite) || !errors.Is(out.Err, cleanupErr) || out.Published || out.Uncertain || out.Saved != (parser.Source{}) || len(out.Leftovers) != 1 {
		t.Fatalf("lost failure evidence: %+v", out)
	}
	assertNativeBytes(t, path, a.Documents[0].Source.Bytes())
	assertNativeBytes(t, foreign, []byte("foreign"))
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() {
		t.Fatal("original changed", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 3 {
		t.Fatalf("unexpected inventory: %v %v", entries, err)
	}
	assertNativeBytes(t, filepath.Join(root, out.Leftovers[0]), []byte("re"))
}

func TestNativeExclusiveCreateCollisionNeverCleansForeignEntry(t *testing.T) {
	root, path := nativeFixture(t)
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	var collision string
	set.native.openStage = func(parent *os.File, name string) (*os.File, error) {
		collision = filepath.Join(root, name)
		if err := os.WriteFile(collision, []byte("external"), 0600); err != nil {
			t.Fatal(err)
		}
		return nativeOpen(parent, name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0600)
	}
	out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("replacement")), nil)
	if !errors.Is(out.Err, fs.ErrExist) || out.Published || out.Uncertain || len(out.Leftovers) != 0 {
		t.Fatalf("foreign create collision misreported: %+v", out)
	}
	assertNativeBytes(t, path, a.Documents[0].Source.Bytes())
	assertNativeBytes(t, collision, []byte("external"))
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 2 {
		t.Fatalf("unexpected inventory %v %v", entries, err)
	}
}

func TestNativePostPublicationSyncAndUnlockFailures(t *testing.T) {
	for _, boundary := range []string{"directory", "unlock"} {
		t.Run(boundary, func(t *testing.T) {
			root, path := nativeFixture(t)
			set, a := nativeAcquire(t, []string{path}, WriteOptions{})
			injected := errors.New("native lifecycle failed")
			if boundary == "directory" {
				set.native.syncFile = func(stage string, f *os.File) error {
					if stage == "directory" {
						return errors.Join(f.Sync(), injected)
					}
					return f.Sync()
				}
			}
			if boundary == "unlock" {
				set.native.unlock = func(*os.File) error { return injected }
			}
			out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("replacement\r\n")), nil)
			if !errors.Is(out.Err, injected) || !out.Published || out.Uncertain || string(out.Saved.Bytes()) != "replacement\r\n" {
				t.Fatalf("publication lost after lifecycle failure: %+v", out)
			}
			assertNativeBytes(t, path, []byte("replacement\r\n"))
			assertNoStage(t, root)
			if boundary == "unlock" {
				retry := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("forged")), nil)
				if retry.Err == nil || retry.Published {
					t.Fatalf("failed unlock acquisition reused: %+v", retry)
				}
				// A fresh acquisition can obtain the actual kernel lock after disposal.
				fresh, aa := nativeAcquire(t, []string{path}, WriteOptions{})
				next := fresh.Publish(context.Background(), path, aa.Documents[0].Source, parser.NewSource([]byte("next\n")), nil)
				if next.Err != nil || !next.Published {
					t.Fatalf("kernel lock leaked: %+v", next)
				}
			}
		})
	}
}
