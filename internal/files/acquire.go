package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/frithuz/mdchk/internal/parser"
)

// AcquireOptions bounds original document bytes, cumulative bytes read (including
// failed reads), and shared work. Zero byte limits select the application defaults.
// Acquisition does not claim to meter discovery or later application work.
type AcquireOptions struct {
	DocumentBytes, TotalContentBytes int64
	Budget                           *parser.OperationBudget
}
type Document struct {
	Path   string
	Source parser.Source
}
type Interruption struct {
	Path string
	Err  error
}

// Acquisition keeps successful immutable snapshots distinct from selected files.
// PreflightErrors are known oversized inputs; runtime quotas/cancellation are
// interruptions. Errors retain effect/path/cause, including close failures.
type Acquisition struct {
	Selection               *Selection
	Documents               []Document
	Errors, PreflightErrors []*fs.PathError
	Interruptions           []Interruption
}
type readHandle interface {
	io.Reader
	Stat() (fs.FileInfo, error)
	Close() error
}

func Acquire(ctx context.Context, selection *Selection, options AcquireOptions) (*Acquisition, error) {
	return acquireFS(ctx, selection, options, openTarget)
}
func acquireFS(ctx context.Context, selection *Selection, options AcquireOptions, open func(Target) (readHandle, error)) (*Acquisition, error) {
	if ctx == nil || selection == nil {
		return nil, errors.New("nil acquisition context or selection")
	}
	if options.DocumentBytes == 0 {
		options.DocumentBytes = 1048576
	}
	if options.TotalContentBytes == 0 {
		options.TotalContentBytes = 8388608
	}
	if options.DocumentBytes < 1 || options.TotalContentBytes < options.DocumentBytes || options.DocumentBytes > 1<<30 {
		return nil, errors.New("incoherent or unsupported acquisition byte limits")
	}
	if options.Budget == nil {
		limits := parser.DefaultLimits()
		limits.DocumentBytes = options.DocumentBytes
		var err error
		options.Budget, err = parser.NewOperationBudget(limits)
		if err != nil {
			return nil, err
		}
	}
	out := &Acquisition{Selection: selection}
	targets := slices.Clone(selection.Targets)
	slices.SortFunc(targets, func(a, b Target) int { return strings.Compare(a.Path, b.Path) })
	var total int64
	checkpoint := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return options.Budget.ChargeWork(0)
	}
	for _, target := range targets {
		if err := checkpoint(); err != nil {
			out.Interruptions = append(out.Interruptions, Interruption{target.Path, err})
			break
		}
		file, err := open(target)
		if err != nil {
			out.Errors = append(out.Errors, pathError(target.Path, "open", err))
			continue
		}
		data, kind, err := readDocument(file, target, options, &total, checkpoint)
		closeErr := file.Close()
		if err != nil {
			switch kind {
			case "preflight":
				out.PreflightErrors = append(out.PreflightErrors, pathError(target.Path, "size", err))
			case "interrupted":
				out.Interruptions = append(out.Interruptions, Interruption{target.Path, err})
			default:
				out.Errors = append(out.Errors, pathError(target.Path, kind, err))
			}
		}
		if closeErr != nil {
			out.Errors = append(out.Errors, pathError(target.Path, "close", closeErr))
		}
		if err == nil && closeErr == nil {
			out.Documents = append(out.Documents, Document{target.Path, parser.NewSource(data)})
		}
		if total > options.TotalContentBytes && kind != "interrupted" {
			// A failing reader can return the quota probe byte alongside its I/O
			// error. Preserve both outcomes and never start a negative-budget read.
			out.Interruptions = append(out.Interruptions, Interruption{target.Path, &parser.LimitExceeded{Name: "total-content-bytes", Observed: total, Allowed: options.TotalContentBytes}})
			break
		}
		if kind == "interrupted" {
			break
		}
		if stop := options.Budget.ChargeWork(0); stop != nil {
			out.Interruptions = append(out.Interruptions, Interruption{target.Path, stop})
			break
		}
	}
	return out, nil
}
func pathError(path, op string, err error) *fs.PathError {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	return &fs.PathError{Op: op, Path: path, Err: err}
}
func readDocument(file readHandle, target Target, options AcquireOptions, total *int64, checkpoint func() error) ([]byte, string, error) {
	if err := checkpoint(); err != nil {
		return nil, "interrupted", err
	}
	before, err := file.Stat()
	if err != nil {
		return nil, "stat", err
	}
	if !before.Mode().IsRegular() {
		return nil, "stat", errors.New("target is not a regular file")
	}
	if len(target.identities) > 0 && !os.SameFile(target.identities[len(target.identities)-1].info, before) {
		return nil, "stat", errors.New("selected file identity changed")
	}
	if before.Size() < 0 {
		return nil, "stat", errors.New("negative file size")
	}
	if before.Size() > options.DocumentBytes {
		return nil, "preflight", &parser.LimitExceeded{Name: "document-bytes", Observed: before.Size(), Allowed: options.DocumentBytes}
	}
	retentionCap := min(options.DocumentBytes, options.TotalContentBytes-*total)
	data := make([]byte, 0, min(before.Size(), retentionCap, 32768))
	var scratch [32768]byte
	for {
		if err := checkpoint(); err != nil {
			return nil, "interrupted", err
		}
		remaining := min(options.DocumentBytes-int64(len(data)), options.TotalContentBytes-*total)
		// A single bounded probe byte detects growth beyond either cap; it is never
		// retained as document content. Counts remain within int64 by construction.
		readSize := min(int64(len(scratch)), remaining+1)
		n, readErr := file.Read(scratch[:readSize])
		if n < 0 || int64(n) > readSize {
			return nil, "read", errors.New("invalid reader count")
		}
		*total += int64(n)
		chargeErr := options.Budget.ChargeWork(int64(n)*3 + 1)
		// Preserve a read failure even when the bytes returned alongside it also
		// cross a quota: I/O failures retain their accepted precedence.
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, "read", readErr
		}
		if int64(len(data))+int64(n) > options.DocumentBytes {
			return nil, "interrupted", &parser.LimitExceeded{Name: "document-bytes", Observed: int64(len(data)) + int64(n), Allowed: options.DocumentBytes}
		}
		if *total > options.TotalContentBytes {
			return nil, "interrupted", &parser.LimitExceeded{Name: "total-content-bytes", Observed: *total, Allowed: options.TotalContentBytes}
		}
		if chargeErr != nil {
			return nil, "interrupted", chargeErr
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, "read", readErr
		}
		needed := len(data) + n
		if needed > cap(data) {
			capacity := min(retentionCap, max(int64(needed), int64(cap(data))*2))
			if err := options.Budget.ChargeWork(int64(len(data))); err != nil {
				return nil, "interrupted", err
			}
			grown := make([]byte, len(data), capacity)
			copy(grown, data)
			data = grown
		}
		data = append(data, scratch[:n]...)
		if errors.Is(readErr, io.EOF) {
			break
		}
		if n == 0 {
			return nil, "read", io.ErrNoProgress
		}
	}
	if err := checkpoint(); err != nil {
		return nil, "interrupted", err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, "stat", err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) || before.Size() != after.Size() || before.ModTime() != after.ModTime() || int64(len(data)) != after.Size() {
		return nil, "stat", errors.New("file changed during acquisition")
	}
	if verifier, ok := file.(interface{ Verify() error }); ok {
		if err := verifier.Verify(); err != nil {
			return nil, "identity", err
		}
	}
	return data, "", nil
}

// openTarget pins each selected directory and checks the next descriptor against
// its selection-time identity. os.Root confines raced lookup; final Unix opens
// additionally reject links and cannot block on a FIFO. One attempt is made.
func openTarget(target Target) (readHandle, error) {
	if len(target.identities) < 2 || !filepath.IsAbs(target.Path) || target.identities[len(target.identities)-1].path != target.Path {
		return nil, errors.New("target lacks selection identity")
	}
	chain := target.identities
	root, err := os.OpenRoot(chain[0].path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if root != nil {
			_ = root.Close()
		}
	}()
	actual, err := root.Stat(".")
	if err != nil {
		return nil, err
	}
	if !os.SameFile(chain[0].info, actual) {
		return nil, errors.New("root identity changed")
	}
	for _, part := range chain[1 : len(chain)-1] {
		name := filepath.Base(part.path)
		if err := rejectGitAlias(root, name, part.info); err != nil && !target.inspection {
			return nil, err
		}
		info, e := root.Lstat(name)
		if e != nil {
			return nil, e
		}
		if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 || !os.SameFile(info, part.info) {
			return nil, fmt.Errorf("directory identity changed: %s", part.path)
		}
		next, e := root.OpenRoot(name)
		if e != nil {
			return nil, e
		}
		actual, e = next.Stat(".")
		observed, observationErr := root.Lstat(name)
		if observationErr != nil || observed.Mode()&fs.ModeSymlink != 0 || !os.SameFile(info, observed) {
			_ = next.Close()
			if observationErr != nil {
				return nil, observationErr
			}
			return nil, errors.New("directory changed while opening")
		}
		if e != nil || !os.SameFile(info, actual) {
			_ = next.Close()
			if e != nil {
				return nil, e
			}
			return nil, errors.New("directory changed while opening")
		}
		if e = root.Close(); e != nil {
			_ = next.Close()
			return nil, e
		}
		root = next
	}
	name := filepath.Base(target.Path)
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(info, chain[len(chain)-1].info) {
		return nil, errors.New("selected file identity changed")
	}
	if err := rejectGitAlias(root, name, info); err != nil && !target.inspection {
		return nil, err
	}
	file, err := root.OpenFile(name, os.O_RDONLY|acquireOpenFlags, 0)
	if err != nil {
		return nil, err
	}
	actual, err = file.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(info, actual) {
		_ = file.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("file changed while opening")
	}
	if err := root.Close(); err != nil {
		_ = file.Close()
		root = nil
		return nil, err
	}
	root = nil
	return &verifiedFile{File: file, target: target}, nil
}

type verifiedFile struct {
	*os.File
	target Target
}

func (file *verifiedFile) Verify() error {
	checked, err := openTarget(file.target)
	if err != nil {
		return err
	}
	return checked.Close()
}
func rejectGitAlias(root *os.Root, name string, info fs.FileInfo) error {
	if name == ".git" {
		return errors.New("git metadata is not a target")
	}
	if !strings.EqualFold(name, ".git") {
		return nil
	}
	marker, err := root.Lstat(".git")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if os.SameFile(info, marker) {
		return errors.New("git metadata alias is not a target")
	}
	return nil
}
