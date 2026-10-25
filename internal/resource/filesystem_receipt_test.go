//go:build linux || darwin || windows

package resource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestFilesystemReceiptLazyCompatibleContent(t *testing.T) {
	f := newFilesystemFixture(t)
	f.write(t, "asset.md", []byte("old"))
	reads := 0
	read := f.filesystem.read
	f.filesystem.read = func(file *os.File, data []byte) (int, error) {
		reads++
		return read(file, data)
	}
	existence := f.request(t, "asset.md", ExistenceOnly, RegularFile)
	first, receipt, err := f.reader.readReceipt(context.Background(), existence, filesystemReceipt{})
	if err != nil || first.Existence != File || first.Content != NotRequested || receipt.identity == "" || receipt.operation != f.operation || receipt.key != existence.Key() || reads != 0 {
		t.Fatal("existence did not retain a metadata-only identity", first, receipt, reads, err)
	}
	// Writing the existing object changes bytes, size and mtime but preserves
	// native identity. Existence never claimed to snapshot unrequested content.
	f.write(t, "asset.md", []byte("new and longer content\r\n"))
	content := f.request(t, "asset.md", TextContent, RegularFile)
	second, upgraded, err := f.reader.readReceipt(context.Background(), content, receipt)
	if err != nil || upgraded != receipt || second.Existence != File || second.Content != Available || string(second.Source.Bytes()) != "new and longer content\r\n" || reads == 0 {
		t.Fatal("compatible late content did not preserve first identity", second, upgraded, reads, err)
	}
	if observation, err := f.operation.Record(content, second); err != nil || observation.Treatment() != Complete {
		t.Fatal("upgraded content is not recordable", observation, err)
	}
}

func TestFilesystemReceiptRejectsChangedNativeObject(t *testing.T) {
	for _, transition := range []string{"replacement", "directory", "missing", "symlink"} {
		t.Run(transition, func(t *testing.T) {
			f := newFilesystemFixture(t)
			f.write(t, "asset.md", []byte("old object"))
			request := f.request(t, "asset.md", ExistenceOnly, RegularFile)
			_, receipt, err := f.reader.readReceipt(context.Background(), request, filesystemReceipt{})
			if err != nil || receipt.identity == "" {
				t.Fatal("initial identity was not acquired", receipt, err)
			}
			// Keep the old inode/file ID alive so a replacement cannot reuse it.
			path := filepath.Join(f.dir, "asset.md")
			if err := os.Rename(path, filepath.Join(f.dir, "retained-old.md")); err != nil {
				t.Fatal(err)
			}
			switch transition {
			case "replacement":
				f.write(t, "asset.md", []byte("unrelated replacement"))
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "missing":
			case "symlink":
				if err := os.Symlink(filepath.Join(f.dir, "retained-old.md"), path); err != nil {
					t.Fatal("required native symlink fixture unavailable", err)
				}
			}
			reads := 0
			read := f.filesystem.read
			f.filesystem.read = func(file *os.File, data []byte) (int, error) {
				reads++
				return read(file, data)
			}
			request = f.request(t, "asset.md", TextContent, RegularFile)
			out, next, readErr := f.reader.readReceipt(context.Background(), request, receipt)
			var fault *FilesystemReadError
			if !errors.As(readErr, &fault) || !errors.Is(fault.IO, errFilesystemReceiptChanged) || fault.Stop != nil || next != receipt || out.Existence != File || out.Content != Failed || out.Problem.Kind != ReadFailure || reads != 0 {
				t.Fatal("incompatible upgrade replaced first fact or read new object", out, next, reads, readErr)
			}
			if observation, err := f.operation.Record(request, out); err != nil || observation.Treatment() != ContextIncomplete {
				t.Fatal("upgrade failure lost valid known-file fact", observation, err)
			}
		})
	}
}

func TestFilesystemReceiptRetainsLateCloseAndStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := newFilesystemFixtureOptions(t, context.Background(), ctx, parser.DefaultLimits(), DefaultFilesystemLimits())
	f.write(t, "asset.md", []byte("resource"))
	failure := errors.New("real metadata close fault")
	closeFile := f.filesystem.close
	f.filesystem.close = func(file *os.File) error {
		err := closeFile(file)
		if file.Name() == "asset.md" {
			cancel()
			return errors.Join(err, failure)
		}
		return err
	}
	request := f.request(t, "asset.md", ExistenceOnly, RegularFile)
	out, receipt, err := f.reader.readReceipt(context.Background(), request, filesystemReceipt{})
	var fault *FilesystemReadError
	if !errors.As(err, &fault) || !errors.Is(fault.IO, failure) || !errors.Is(fault.Stop, context.Canceled) || receipt.identity == "" || out.Existence != File {
		t.Fatal("late close/stop erased metadata identity or independent causes", out, receipt, err)
	}
}

func TestFilesystemReceiptRejectsForeignProof(t *testing.T) {
	f := newFilesystemFixture(t)
	f.write(t, "asset.md", []byte("resource"))
	request := f.request(t, "asset.md", ExistenceOnly, RegularFile)
	_, receipt, err := f.reader.readReceipt(context.Background(), request, filesystemReceipt{})
	if err != nil {
		t.Fatal(err)
	}
	other := newFilesystemFixture(t)
	other.write(t, "asset.md", []byte("other"))
	if _, _, err := other.reader.readReceipt(context.Background(), other.request(t, "asset.md", TextContent, RegularFile), receipt); !errors.Is(err, ErrContract) {
		t.Fatal("foreign operation receipt was admitted", err)
	}
	f.write(t, "other.md", []byte("other"))
	if _, _, err := f.reader.readReceipt(context.Background(), f.request(t, "other.md", TextContent, RegularFile), receipt); !errors.Is(err, ErrContract) {
		t.Fatal("foreign key receipt was admitted", err)
	}
}

func TestFilesystemReceiptRejectsDataHandleSubstitution(t *testing.T) {
	f := newFilesystemFixture(t)
	f.write(t, "asset.md", []byte("original object"))
	_, receipt, err := f.reader.readReceipt(context.Background(), f.request(t, "asset.md", ExistenceOnly, RegularFile), filesystemReceipt{})
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	f.filesystem.hook = func(stage, _ string) error {
		if stage == "before-content-open" && !changed {
			changed = true
			path := filepath.Join(f.dir, "asset.md")
			if err := os.Rename(path, filepath.Join(f.dir, "retained-original.md")); err != nil {
				return err
			}
			return os.WriteFile(path, []byte("substituted bytes"), 0600)
		}
		return nil
	}
	reads := 0
	read := f.filesystem.read
	f.filesystem.read = func(file *os.File, data []byte) (int, error) {
		reads++
		return read(file, data)
	}
	out, next, err := f.reader.readReceipt(context.Background(), f.request(t, "asset.md", TextContent, RegularFile), receipt)
	if !changed || !errors.Is(err, errFilesystemReceiptChanged) || out.Existence != File || out.Content != Failed || next != receipt || reads != 0 {
		t.Fatal("data-handle substitution bypassed stable first identity", changed, out, next, reads, err)
	}
}
