package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

func acquireFixture(t *testing.T, selection *Selection, options AcquireOptions) *Acquisition {
	t.Helper()
	got, err := Acquire(context.Background(), selection, options)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAcquireExactOwnedSnapshots(t *testing.T) {
	root := selectionRoot(t)
	contents := [][]byte{nil, []byte("\xef\xbb\xbf# 雪\t\r\nsecond\rthird\nno final newline"), {'a', 0xff, 0xc0, '\r', '\n', 0}}
	names := []string{"empty.md", "mixed.md", "invalid.md"}
	want := make(map[string][]byte)
	for i, name := range names {
		p := selectionFile(t, root, name, contents[i])
		want[p] = bytes.Clone(contents[i])
	}
	selection := selectFixture(t, Options{WorkingDirectory: root})
	got := acquireFixture(t, selection, AcquireOptions{})
	if len(got.Errors) != 0 || len(got.PreflightErrors) != 0 || len(got.Interruptions) != 0 || len(got.Documents) != len(want) {
		t.Fatalf("unexpected acquisition: %+v", got)
	}
	for _, document := range got.Documents {
		expected, ok := want[document.Path]
		if !ok {
			t.Fatalf("unexpected document %q", document.Path)
		}
		snapshot := document.Source.Bytes()
		if !bytes.Equal(snapshot, expected) || document.Source.Len() != len(expected) || sha256.Sum256(snapshot) != sha256.Sum256(expected) {
			t.Fatalf("snapshot changed original bytes for %q", document.Path)
		}
		if len(snapshot) > 0 {
			snapshot[0] ^= 0xff
		}
		if err := os.WriteFile(document.Path, []byte("changed after acquire"), 0600); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(document.Source.Bytes(), expected) {
			t.Fatalf("snapshot aliases caller or file for %q", document.Path)
		}
	}
}

type acquireFaultHandle struct {
	reader            io.Reader
	info              fs.FileInfo
	statErr, closeErr error
	closes            int
}

func (h *acquireFaultHandle) Read(p []byte) (int, error) { return h.reader.Read(p) }
func (h *acquireFaultHandle) Stat() (fs.FileInfo, error) { return h.info, h.statErr }
func (h *acquireFaultHandle) Close() error               { h.closes++; return h.closeErr }

type acquirePartialErrorReader struct {
	err  error
	used bool
}

func (r *acquirePartialErrorReader) Read(p []byte) (int, error) {
	if r.used {
		return 0, io.EOF
	}
	r.used = true
	return copy(p, "partial bytes"), r.err
}

func TestAcquireFaultsDiscardPartialSnapshotsAndRetainIndependentTargets(t *testing.T) {
	failure := errors.New("injected storage failure")
	for _, operation := range []string{"open", "stat", "read", "partial-read", "close", "read-and-close"} {
		t.Run(operation, func(t *testing.T) {
			root := selectionRoot(t)
			bad := selectionFile(t, root, "a-bad.md", []byte("original"))
			good := selectionFile(t, root, "z-good.md", []byte("independent\r\n"))
			selection := selectFixture(t, Options{WorkingDirectory: root})
			info, err := os.Stat(bad)
			if err != nil {
				t.Fatal(err)
			}
			handle := &acquireFaultHandle{reader: bytes.NewReader([]byte("original")), info: info}
			switch operation {
			case "stat":
				handle.statErr = failure
			case "read":
				handle.reader = acquireErrorReader{failure}
			case "partial-read":
				handle.reader = &acquirePartialErrorReader{err: failure}
			case "close":
				handle.closeErr = failure
			case "read-and-close":
				handle.reader = &acquirePartialErrorReader{err: failure}
				handle.closeErr = fs.ErrPermission
			}
			got, err := acquireFS(context.Background(), selection, AcquireOptions{}, func(target Target) (readHandle, error) {
				if target.Path == bad {
					if operation == "open" {
						return nil, failure
					}
					return handle, nil
				}
				return os.Open(target.Path)
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Documents) != 1 || got.Documents[0].Path != good || string(got.Documents[0].Source.Bytes()) != "independent\r\n" {
				t.Fatalf("lost independent target or accepted partial bytes: %+v", got.Documents)
			}
			if len(got.PreflightErrors) != 0 || len(got.Interruptions) != 0 {
				t.Fatalf("IO misclassified: %+v", got)
			}
			wantOp := operation
			if operation == "partial-read" || operation == "read-and-close" {
				wantOp = "read"
			}
			found := false
			for _, failureResult := range got.Errors {
				if failureResult.Path == bad && failureResult.Op == wantOp && errors.Is(failureResult, failure) {
					found = true
				}
			}
			if !found {
				t.Fatalf("lost path, operation or cause: %+v", got.Errors)
			}
			if operation == "read-and-close" {
				found = false
				for _, failureResult := range got.Errors {
					if failureResult.Op == "close" && errors.Is(failureResult, fs.ErrPermission) {
						found = true
					}
				}
				if !found {
					t.Fatalf("lost secondary close error: %v", got.Errors)
				}
			}
			expectedCloses := 1
			if operation == "open" {
				expectedCloses = 0
			}
			if handle.closes != expectedCloses {
				t.Fatalf("handle closed %d times, want %d", handle.closes, expectedCloses)
			}
		})
	}
}

type acquireErrorReader struct{ err error }

func (r acquireErrorReader) Read([]byte) (int, error) { return 0, r.err }

func TestAcquireKnownOversizeAndAggregateInterruption(t *testing.T) {
	root := selectionRoot(t)
	a := selectionFile(t, root, "a.md", []byte("1234"))
	b := selectionFile(t, root, "b.md", []byte("56789"))
	large := selectionFile(t, root, "large.md", []byte("123456789"))
	selection := selectFixture(t, Options{WorkingDirectory: root, Paths: []string{large, a}})
	got := acquireFixture(t, selection, AcquireOptions{DocumentBytes: 8, TotalContentBytes: 16})
	if len(got.Documents) != 1 || got.Documents[0].Path != a {
		t.Fatalf("completed files=%+v", got.Documents)
	}
	if len(got.Errors) != 0 || len(got.Interruptions) != 0 {
		t.Fatalf("known size misclassified: %+v", got)
	}
	if len(got.PreflightErrors) != 1 || got.PreflightErrors[0].Path != large {
		t.Fatalf("known oversize lost preflight classification: %v", got.PreflightErrors)
	}
	selection = selectFixture(t, Options{WorkingDirectory: root, Paths: []string{b, a}})
	got = acquireFixture(t, selection, AcquireOptions{DocumentBytes: 8, TotalContentBytes: 8})
	if len(got.Documents) != 1 || got.Documents[0].Path != a || len(got.Errors) != 0 || len(got.PreflightErrors) != 0 {
		t.Fatalf("aggregate quota mishandled independent completion: %+v", got)
	}
	if len(got.Interruptions) != 1 || got.Interruptions[0].Path != b {
		t.Fatalf("aggregate exhaustion lost interruption: %v", got.Interruptions)
	}
}

func TestAcquireRuntimeGrowthIsInterruption(t *testing.T) {
	root := selectionRoot(t)
	path := selectionFile(t, root, "growing.md", []byte("ok"))
	selection := selectFixture(t, Options{WorkingDirectory: root})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	handle := &acquireFaultHandle{reader: bytes.NewReader([]byte("content now exceeds quota")), info: info}
	got, err := acquireFS(context.Background(), selection, AcquireOptions{DocumentBytes: 8}, func(Target) (readHandle, error) { return handle, nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Documents) != 0 || len(got.Errors) != 0 || len(got.PreflightErrors) != 0 || len(got.Interruptions) != 1 || got.Interruptions[0].Path != path {
		t.Fatalf("growth was not an interruption: %+v", got)
	}
	if handle.closes != 1 {
		t.Fatalf("growth handle closed %d times", handle.closes)
	}
}

func TestAcquireCancellationAndWorkBudget(t *testing.T) {
	for _, kind := range []string{"cancelled", "work-budget"} {
		t.Run(kind, func(t *testing.T) {
			root := selectionRoot(t)
			selectionFile(t, root, "target.md", []byte("document"))
			selection := selectFixture(t, Options{WorkingDirectory: root})
			ctx := context.Background()
			options := AcquireOptions{}
			if kind == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			} else {
				limits := parser.DefaultLimits()
				limits.WorkUnits = 1
				var err error
				options.Budget, err = parser.NewOperationBudget(limits)
				if err != nil {
					t.Fatal(err)
				}
			}
			got, err := Acquire(ctx, selection, options)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Documents) != 0 || len(got.Errors) != 0 || len(got.PreflightErrors) != 0 || len(got.Interruptions) == 0 {
				t.Fatalf("interruption treated as completion/IO: %+v", got)
			}
			if kind == "cancelled" && !errors.Is(got.Interruptions[0].Err, context.Canceled) {
				t.Fatalf("cancel cause lost: %v", got.Interruptions)
			}
		})
	}
}

func TestAcquireOrderingAndSelectionOutcomes(t *testing.T) {
	root := selectionRoot(t)
	a := selectionFile(t, root, "a.md", []byte("alpha"))
	z := selectionFile(t, root, "z.md", []byte("zeta"))
	selectionFile(t, root, "skip.txt", nil)
	missing := filepath.Join(root, "missing.md")
	var previous *Acquisition
	for _, paths := range [][]string{{z, missing, ".", a}, {a, ".", missing, z}} {
		selection := selectFixture(t, Options{WorkingDirectory: root, Paths: paths})
		got := acquireFixture(t, selection, AcquireOptions{})
		if len(got.Documents) != 2 || got.Documents[0].Path != a || got.Documents[1].Path != z {
			t.Fatalf("unordered documents: %+v", got.Documents)
		}
		if got.Selection == nil || !reflect.DeepEqual(got.Selection.Errors, selection.Errors) || !reflect.DeepEqual(got.Selection.Skipped, selection.Skipped) {
			t.Fatalf("selection outcomes lost: %+v", got.Selection)
		}
		if previous != nil {
			for i := range got.Documents {
				if got.Documents[i].Path != previous.Documents[i].Path || !bytes.Equal(got.Documents[i].Source.Bytes(), previous.Documents[i].Source.Bytes()) {
					t.Fatal("argument order changed acquisitions")
				}
			}
		}
		previous = got
	}
}

func TestAcquireExactByteLimitAndDefaultLimit(t *testing.T) {
	root := selectionRoot(t)
	exact := selectionFile(t, root, "exact.md", bytes.Repeat([]byte{'x'}, 1<<20))
	over := selectionFile(t, root, "over.md", bytes.Repeat([]byte{'y'}, (1<<20)+1))
	selection := selectFixture(t, Options{WorkingDirectory: root})
	got := acquireFixture(t, selection, AcquireOptions{})
	if len(got.Documents) != 1 || got.Documents[0].Path != exact || got.Documents[0].Source.Len() != 1<<20 {
		t.Fatalf("exact default boundary was rejected: %+v", got.Documents)
	}
	if len(got.PreflightErrors) != 1 || got.PreflightErrors[0].Path != over || len(got.Interruptions) != 0 || len(got.Errors) != 0 {
		t.Fatalf("default oversize mishandled: %+v", got)
	}
}

type acquireCancelReader struct{ cancel context.CancelFunc }

func (r acquireCancelReader) Read(p []byte) (int, error) { r.cancel(); return copy(p, "partial"), nil }

func TestAcquireCancellationDuringReadDiscardsBytesAndCloses(t *testing.T) {
	root := selectionRoot(t)
	path := selectionFile(t, root, "target.md", []byte("partial"))
	selection := selectFixture(t, Options{WorkingDirectory: root})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handle := &acquireFaultHandle{reader: acquireCancelReader{cancel}, info: info}
	got, err := acquireFS(ctx, selection, AcquireOptions{}, func(Target) (readHandle, error) { return handle, nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Documents) != 0 || len(got.Errors) != 0 || len(got.PreflightErrors) != 0 || len(got.Interruptions) != 1 || !errors.Is(got.Interruptions[0].Err, context.Canceled) {
		t.Fatalf("mid-read cancellation became completion: %+v", got)
	}
	if handle.closes != 1 {
		t.Fatalf("canceled handle closed %d times", handle.closes)
	}
}

type acquireSecondStatFailure struct {
	*acquireFaultHandle
	stats int
	err   error
}

func (h *acquireSecondStatFailure) Stat() (fs.FileInfo, error) {
	h.stats++
	if h.stats == 2 {
		return nil, h.err
	}
	return h.acquireFaultHandle.Stat()
}

func TestAcquirePostReadStatFailureDiscardsBytes(t *testing.T) {
	root := selectionRoot(t)
	path := selectionFile(t, root, "target.md", []byte("original"))
	selection := selectFixture(t, Options{WorkingDirectory: root})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("post-read descriptor stat failure")
	handle := &acquireSecondStatFailure{acquireFaultHandle: &acquireFaultHandle{reader: bytes.NewReader([]byte("original")), info: info}, err: failure}
	got, err := acquireFS(context.Background(), selection, AcquireOptions{}, func(Target) (readHandle, error) { return handle, nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Documents) != 0 || len(got.Errors) != 1 || got.Errors[0].Op != "stat" || got.Errors[0].Path != path || !errors.Is(got.Errors[0], failure) {
		t.Fatalf("post-read stat failure accepted snapshot or lost cause: %+v", got)
	}
	if handle.closes != 1 {
		t.Fatalf("stat-failed handle closed %d times", handle.closes)
	}
}

func TestAcquireExpiredDeadlineBeforeOpen(t *testing.T) {
	root := selectionRoot(t)
	selectionFile(t, root, "target.md", []byte("original"))
	selection := selectFixture(t, Options{WorkingDirectory: root})
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	opened := false
	got, err := acquireFS(ctx, selection, AcquireOptions{}, func(Target) (readHandle, error) { opened = true; return nil, errors.New("must not open") })
	if err != nil {
		t.Fatal(err)
	}
	if opened || len(got.Documents) != 0 || len(got.Errors) != 0 || len(got.Interruptions) != 1 || !errors.Is(got.Interruptions[0].Err, context.DeadlineExceeded) {
		t.Fatalf("expired deadline not checked before open: %+v opened=%v", got, opened)
	}
}

func TestAcquireReadErrorCrossingAggregateRetainsBothCauses(t *testing.T) {
	root := selectionRoot(t)
	bad := selectionFile(t, root, "a.md", []byte("short"))
	selectionFile(t, root, "b.md", []byte("independent"))
	selection := selectFixture(t, Options{WorkingDirectory: root})
	info, err := os.Stat(bad)
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("probe storage failure")
	handle := &acquireFaultHandle{info: info, reader: &acquirePartialErrorReader{err: cause}}
	opens := 0
	got, err := acquireFS(context.Background(), selection, AcquireOptions{DocumentBytes: 8, TotalContentBytes: 8}, func(Target) (readHandle, error) { opens++; return handle, nil })
	if err != nil {
		t.Fatal(err)
	}
	if opens != 1 || len(got.Documents) != 0 || len(got.Errors) != 1 || !errors.Is(got.Errors[0], cause) || len(got.Interruptions) != 1 || handle.closes != 1 {
		t.Fatalf("lost IO/quota or continued after exhaustion: %+v, opens=%d", got, opens)
	}
}
