//go:build linux || darwin || windows

package files

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/frithuz/mdchk/internal/fsroot"
	"github.com/frithuz/mdchk/internal/parser"
)

// One portable implementation serves Linux, macOS and Windows. Directory
// traversal is pinned and verified by fsroot on os.Root; the only platform
// specific code is the owner and extended-attribute shims beside this file.

// errUnsupportedTarget marks an entry the replacement cannot reproduce exactly:
// not a regular file, owner-unwritable, or carrying set-ID or sticky bits.
var errUnsupportedTarget = errors.New("unsupported target")

type nativeObservation struct {
	path      string
	source    parser.Source
	original  parser.Source
	target    bool
	absent    bool
	uncertain bool
	parent    *fsroot.Dir
	chain     []fs.FileInfo
	info      fs.FileInfo
	links     uint64
	xattrs    xattrSet
	budget    *parser.OperationBudget
}
type nativeWriteSet struct {
	entries  map[string]*nativeObservation
	required map[string][]string
	// Test-only hooks surround real operations; production sets none.
	hook   func(string, string) error
	write  func(*os.File, []byte) (int, error)
	rename func(*fsroot.Dir, string, string) error
	// Per-acquisition operation seams retain real handle ownership in tests.
	closeFile   func(string, io.Closer) error
	statFile    func(string, *os.File) (fs.FileInfo, error)
	openStage   func(*fsroot.Dir, string) (*os.File, error)
	removeStage func(*fsroot.Dir, string) error
	syncFile    func(string, *os.File) error
	syncDir     func(*fsroot.Dir) error
}

// checkTarget enforces the supported-target contract on an opened file.
func checkTarget(info fs.FileInfo) error {
	mode := info.Mode()
	switch {
	case !mode.IsRegular():
		return fmt.Errorf("%w: not a regular file", errUnsupportedTarget)
	case mode.Perm()&0o200 == 0:
		return fmt.Errorf("%w: not owner-writable", errUnsupportedTarget)
	case mode&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0:
		return fmt.Errorf("%w: set-ID or sticky bit", errUnsupportedTarget)
	}
	return nil
}

func sameChain(a, b []fs.FileInfo) bool {
	if !fsroot.SameChain(a, b) {
		return false
	}
	for i := range a {
		if !sameOwner(a[i], b[i]) {
			return false
		}
	}
	return true
}

func sameSecurity(a, b fs.FileInfo) bool {
	return a.Mode() == b.Mode() && sameOwner(a, b)
}

func pinParent(path string) (*fsroot.Dir, []fs.FileInfo, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == "." || strings.IndexByte(path, 0) >= 0 {
		return nil, nil, writeError(path, "noncanonical native path")
	}
	return fsroot.Pin(filepath.Dir(path))
}

func (o *nativeObservation) reopen() (result *os.File, err error) {
	if o.uncertain {
		return nil, writeError(o.path, "saved state is uncertain; new acquisition required")
	}
	if err := reserveNativeParents(o.budget, o.path); err != nil {
		return nil, err
	}
	parent, chain, err := pinParent(o.path)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, parent.Close())
		if err != nil && result != nil {
			err = errors.Join(err, result.Close())
			result = nil
		}
	}()
	if !sameChain(o.chain, chain) {
		return nil, writeError(o.path, "parent identity changed")
	}
	if o.absent {
		_, statErr := parent.Lstat(filepath.Base(o.path))
		if errors.Is(statErr, fs.ErrNotExist) {
			return nil, nil
		}
		if statErr != nil {
			return nil, statErr
		}
		return nil, writeError(o.path, "absent dependency appeared")
	}
	f, info, err := parent.OpenRegular(filepath.Base(o.path))
	if err != nil {
		return nil, err
	}
	if !os.SameFile(o.info, info) || !sameSecurity(o.info, info) {
		return nil, errors.Join(f.Close(), writeError(o.path, "identity, type or security metadata changed"))
	}
	if err = checkTarget(info); err != nil {
		return nil, errors.Join(err, f.Close())
	}
	attrs, err := readXattrs(f)
	if err != nil || !equalXattrs(attrs, o.xattrs) {
		return nil, errors.Join(err, f.Close(), writeError(o.path, "extended attributes changed"))
	}
	return f, nil
}

type nativeRead struct {
	*os.File
	observation *nativeObservation
}

func (f *nativeRead) Verify() error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !sameSecurity(f.observation.info, info) {
		return errors.New("metadata changed during acquisition")
	}
	if err = checkTarget(info); err != nil {
		return err
	}
	other, err := f.observation.reopen()
	if other != nil {
		err = errors.Join(err, other.Close())
	}
	return err
}

func acquireNativeWrite(ctx context.Context, selected *Selection, options WriteOptions) (out *nativeWriteSet, targets, resources *Acquisition, err error) {
	if err = checkpoint(ctx, options.Budget, int64(len(selected.Targets)+len(options.Absences))*8+1); err != nil {
		return
	}
	count := len(selected.Targets) + len(options.Absences)
	if options.Resources != nil {
		count += len(options.Resources.Targets)
	}
	if err = options.Budget.ChargeNodes(int64(count)*8 + 1); err != nil {
		return
	}
	out = &nativeWriteSet{entries: map[string]*nativeObservation{}, required: map[string][]string{}}
	defer func() {
		if err != nil {
			err = errors.Join(err, out.close())
		}
	}()

	writable := map[string]bool{}
	all := &Selection{Targets: slices.Clone(selected.Targets)}
	for _, t := range selected.Targets {
		writable[t.Path] = true
	}
	if options.Resources != nil {
		all.Targets = append(all.Targets, options.Resources.Targets...)
	}
	readOptions := options.AcquireOptions
	if options.ResourceDocumentBytes != 0 {
		if options.ResourceDocumentBytes < 1 || options.ResourceDocumentBytes > 1<<30 {
			err = errors.New("unsupported resource document bound")
			return
		}
		readOptions.documentLimits = map[string]int64{}
		if options.Resources != nil {
			for _, target := range options.Resources.Targets {
				readOptions.documentLimits[target.Path] = options.ResourceDocumentBytes
			}
		}
	}
	combined, acquisitionErr := acquireFS(ctx, all, readOptions, func(t Target) (readHandle, error) {
		if err := checkpoint(ctx, options.Budget, int64(len(t.Path))*3+pathDepth(t.Path)*8192+1); err != nil {
			return nil, err
		}
		if t.inspection {
			return nil, writeError(t.Path, "inspection acquisition is read-only")
		}
		if out.entries[t.Path] != nil {
			return nil, writeError(t.Path, "duplicate native observation")
		}
		if err := reserveNativeParents(options.Budget, t.Path); err != nil {
			return nil, err
		}
		parent, chain, e := pinParent(t.Path)
		if e != nil {
			return nil, e
		}
		if len(t.identities) != len(chain)+1 {
			return nil, errors.Join(writeError(t.Path, "missing selection parent authority"), parent.Close())
		}
		for i, info := range chain {
			if !os.SameFile(info, t.identities[i].info) {
				return nil, errors.Join(writeError(t.Path, "selected parent identity changed"), parent.Close())
			}
		}
		f, info, e := parent.OpenRegular(filepath.Base(t.Path))
		if e != nil {
			return nil, errors.Join(e, parent.Close())
		}
		if e = checkTarget(info); e != nil {
			return nil, errors.Join(e, f.Close(), parent.Close())
		}
		attrs, e := readXattrs(f)
		if e != nil {
			return nil, errors.Join(e, f.Close(), parent.Close())
		}
		observation := &nativeObservation{budget: options.Budget, xattrs: attrs, path: t.Path, parent: parent, chain: chain, info: info, links: linkCount(f, info), target: writable[t.Path]}
		out.entries[t.Path] = observation
		return &nativeRead{f, observation}, nil
	})
	if acquisitionErr != nil {
		err = acquisitionErr
		return
	}
	targets = &Acquisition{Selection: selected}
	resources = &Acquisition{Selection: options.Resources}
	destination := func(path string) *Acquisition {
		if writable[path] {
			return targets
		}
		return resources
	}
	for _, d := range combined.Documents {
		a := destination(d.Path)
		a.Documents = append(a.Documents, d)
	}
	for _, e := range combined.Errors {
		a := destination(e.Path)
		a.Errors = append(a.Errors, e)
	}
	for _, e := range combined.PreflightErrors {
		a := destination(e.Path)
		a.PreflightErrors = append(a.PreflightErrors, e)
	}
	for _, e := range combined.Interruptions {
		a := destination(e.Path)
		a.Interruptions = append(a.Interruptions, e)
	}

	for _, a := range []*Acquisition{targets, resources} {
		if a == nil {
			continue
		}
		for _, d := range a.Documents {
			out.entries[d.Path].source = d.Source
			out.entries[d.Path].original = d.Source
		}
	}
	for path, baseline := range options.Baselines {
		if err = options.Budget.ChargeWork(int64(baseline.SizeBytes)*3 + int64(len(path)) + 1); err != nil {
			return
		}
		observation := out.entries[path]
		if observation == nil || baseline.Info == nil || observation.info == nil ||
			!os.SameFile(baseline.Info, observation.info) || baseline.Info.ModTime() != observation.info.ModTime() ||
			baseline.SizeBytes != int64(observation.source.Len()) ||
			baseline.SHA256 != fmt.Sprintf("%x", sha256.Sum256(observation.source.Bytes())) {
			err = writeError(path, "configuration changed since resolution")
			return
		}
	}

	for _, path := range options.Absences {
		if e := ctx.Err(); e != nil {
			err = e
			return
		}
		if e := options.Budget.ChargeWork(int64(len(path)) + 1); e != nil {
			err = e
			return
		}
		if out.entries[path] != nil {
			err = writeError(path, "duplicate dependency")
			return
		}
		if e := reserveNativeParents(options.Budget, path); e != nil {
			err = e
			return
		}
		parent, chain, e := pinParent(path)
		if e != nil {
			err = e
			return
		}
		o := &nativeObservation{budget: options.Budget, path: path, parent: parent, chain: chain, absent: true}
		out.entries[path] = o
		if _, e = o.reopen(); e != nil {
			err = e
			return
		}
	}
	for path, deps := range options.Required {
		// Failed selected targets retain their acquisition outcomes but have no
		// observation that could authorize publication. Keep independent owners
		// eligible without weakening their required dependency checks.
		if writable[path] && out.entries[path] == nil {
			continue
		}
		if out.entries[path] == nil || !out.entries[path].target {
			err = writeError(path, "unknown dependency owner")
			return
		}
		out.required[path] = slices.Clone(deps)
		for _, dep := range deps {
			if out.entries[dep] == nil {
				err = writeError(dep, "unobserved dependency")
				return
			}
		}
	}
	return
}
func (s *nativeWriteSet) close() error {
	var errs []error
	for _, o := range s.entries {
		errs = append(errs, s.closeHandle("shutdown", o.parent))
	}
	s.entries = nil
	return errors.Join(errs...)
}

// closeHandle consumes the handle even when Close reports failure. Injected
// operations have the same ownership contract; callers never retry a close.
func (s *nativeWriteSet) closeHandle(stage string, c io.Closer) error {
	if s.closeFile != nil {
		return s.closeFile(stage, c)
	}
	return c.Close()
}
func (s *nativeWriteSet) statHandle(stage string, f *os.File) (fs.FileInfo, error) {
	if s.statFile != nil {
		return s.statFile(stage, f)
	}
	return f.Stat()
}
func (s *nativeWriteSet) syncHandle(stage string, f *os.File) error {
	if s.syncFile != nil {
		return s.syncFile(stage, f)
	}
	return f.Sync()
}
func (s *nativeWriteSet) syncDirectory(d *fsroot.Dir) error {
	if s.syncDir != nil {
		return s.syncDir(d)
	}
	return d.Sync()
}
func (s *nativeWriteSet) at(stage, path string) error {
	if s.hook != nil {
		return s.hook(stage, path)
	}
	return nil
}
func checkpoint(ctx context.Context, b *parser.OperationBudget, n int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return b.ChargeWork(n)
}

// pathDepth counts separators so budget charges scale with traversal length.
func pathDepth(path string) int64 {
	return int64(strings.Count(path, string(filepath.Separator)))
}

func (s *nativeWriteSet) validate(ctx context.Context, o *nativeObservation, b *parser.OperationBudget) (err error) {
	if err := checkpoint(ctx, b, int64(o.source.Len())*3+int64(len(o.path))*3+pathDepth(o.path)*4096+1); err != nil {
		return err
	}
	f, err := o.reopen()
	if err != nil {
		return err
	}
	if f == nil {
		return nil
	}
	defer func() { err = errors.Join(err, s.closeHandle("revalidation", f)) }()
	// Bound a growth probe independently of potentially hostile current file size.
	raw, err := io.ReadAll(io.LimitReader(f, int64(o.source.Len())+1))
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, o.source.Bytes()) {
		return writeError(o.path, "content changed")
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !sameSecurity(o.info, info) || !os.SameFile(o.info, info) || info.Size() != int64(len(raw)) || !info.ModTime().Equal(o.info.ModTime()) {
		return writeError(o.path, "metadata changed during validation")
	}
	return checkpoint(ctx, b, 1)
}

// Stale protection detects concurrent changes; it does not serialize writers.
// No cross-process lock is taken, so a noncooperating writer can still change a
// file between the last check and the rename. See docs/engineering/native-writes.md.
func (s *nativeWriteSet) publish(ctx context.Context, path string, original, candidate parser.Source, deps []string, b *parser.OperationBudget) (out Publication) {
	out.Path = path
	o := s.entries[path]
	if o == nil || !o.target || o.absent || o.source == (parser.Source{}) || o.source != original {
		out.Err = writeError(path, "source lacks target acquisition authority")
		return
	}
	if candidate == original {
		out.NoChange = true
		return
	}
	if candidate.Len() == original.Len() {
		if err := checkpoint(ctx, b, int64(original.Len())*3+1); err != nil {
			out.Err = err
			return
		}
		if bytes.Equal(candidate.Bytes(), original.Bytes()) {
			out.NoChange = true
			return
		}
	}
	if int64(candidate.Len()) > b.Limits().DocumentBytes {
		out.Err = writeError(path, "candidate exceeds document limit")
		return
	}
	if err := checkpoint(ctx, b, int64(len(deps)+len(s.required[path]))+int64(candidate.Len())*8+pathDepth(path)*4096+1); err != nil {
		out.Err = err
		return
	}
	if err := reserveNativeParents(b, path); err != nil {
		out.Err = err
		return
	}
	if err := b.ChargeNodes(int64(len(deps)+len(s.required[path]))*3 + 3); err != nil {
		out.Err = err
		return
	}
	observations := []*nativeObservation{o}
	seen := map[string]bool{path: true}
	for _, list := range [][]string{deps, s.required[path]} {
		for _, dep := range list {
			if seen[dep] {
				continue
			}
			seen[dep] = true
			v := s.entries[dep]
			if v == nil || !v.absent && v.source == (parser.Source{}) {
				out.Err = writeError(dep, "dependency has no acquired observation")
				return
			}
			observations = append(observations, v)
		}
	}
	if err := s.at("start", path); err != nil {
		out.Err = err
		return
	}
	for _, v := range observations {
		if err := s.validate(ctx, v, b); err != nil {
			out.Err = err
			return
		}
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		out.Err = err
		return
	}
	name := ".mdchk-stage-" + hex.EncodeToString(random[:])
	if err := s.at("create", path); err != nil {
		out.Err = err
		return
	}
	openStage := s.openStage
	if openStage == nil {
		openStage = func(parent *fsroot.Dir, name string) (*os.File, error) {
			return parent.CreateExclusive(name, 0o600)
		}
	}
	temp, err := openStage(o.parent, name)
	if err != nil {
		out.Err = err
		return
	}
	out.Started = true
	createdInfo, createStatErr := s.statHandle("created", temp)
	if createStatErr != nil {
		out.Err = errors.Join(createStatErr, s.closeHandle("created", temp))
		out.Leftovers = []string{name}
		return
	}
	closed := false
	published := false
	defer func() {
		if !closed {
			out.Err = errors.Join(out.Err, s.closeHandle("stage", temp))
		}
		if !published {
			if e := s.at("cleanup", path); e != nil {
				out.Leftovers = append(out.Leftovers, name)
				out.Err = errors.Join(out.Err, e)
			} else if e := s.cleanupStage(o.parent, name, createdInfo); e != nil {
				out.Leftovers = append(out.Leftovers, name)
				out.Err = errors.Join(out.Err, e)
			}
		}
	}()
	if err = s.at("write", path); err != nil {
		out.Err = err
		return
	}
	data := candidate.Bytes()
	write := s.write
	if write == nil {
		write = func(f *os.File, data []byte) (int, error) { return f.Write(data) }
	}
	n, err := write(temp, data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = s.at("short-write", path)
	}
	if err != nil {
		out.Err = err
		return
	}
	if err = s.at("metadata", path); err != nil {
		out.Err = err
		return
	}
	// Owner first: chown clears set-ID bits. Attributes before mode: a POSIX ACL
	// write recomputes the group bits, and the target's mode already reflects its
	// own ACL mask, so the final chmod restores the exact value.
	err = applyOwner(temp, o.info)
	if err == nil {
		err = copyXattrs(temp, o.xattrs)
	}
	if err == nil {
		err = temp.Chmod(o.info.Mode().Perm())
	}
	if err == nil {
		err = verifyStageMetadata(temp, o)
	}
	if err != nil {
		out.Err = err
		return
	}
	if err = s.at("flush", path); err == nil {
		err = s.syncHandle("stage", temp)
	}
	if err != nil {
		out.Err = err
		return
	}
	stagedInfo, statErr := temp.Stat()
	if statErr != nil {
		out.Err = statErr
		return
	}
	err = s.closeHandle("stage", temp)
	closed = true
	if err == nil {
		err = s.at("close", path)
	}
	if err != nil {
		out.Err = err
		return
	}
	if err = s.at("before-final-check", path); err != nil {
		out.Err = err
		return
	}
	for _, v := range observations {
		if err = s.validate(ctx, v, b); err != nil {
			out.Err = err
			return
		}
	}
	stageCheck, stageInfo, e := o.parent.OpenRegular(name)
	if e != nil {
		out.Err = e
		return
	}
	if !os.SameFile(createdInfo, stageInfo) || !sameSecurity(stagedInfo, stageInfo) {
		e = errors.New("staged entry identity or metadata changed")
	}
	if e == nil {
		e = verifyStageMetadata(stageCheck, o)
	}
	if e == nil {
		stageBytes, readErr := io.ReadAll(io.LimitReader(stageCheck, int64(len(data))+1))
		if readErr != nil || !bytes.Equal(stageBytes, data) {
			e = errors.Join(readErr, errors.New("staged content changed"))
		}
	}
	e = errors.Join(e, s.closeHandle("verification", stageCheck))
	if e != nil {
		out.Err = e
		return
	}
	if err = checkpoint(ctx, b, 1); err != nil {
		out.Err = err
		return
	}
	// The explicitly accepted noncooperating-writer gap starts here. Rename
	// publishes the final entry through the pinned parent, never its referent.
	if err = s.at("after-final-check", path); err != nil {
		out.Err = err
		return
	}

	if err = s.at("rename", path); err != nil {
		out.Err = err
		return
	}
	if err = checkpoint(ctx, b, 1); err != nil {
		out.Err = err
		return
	}
	rename := s.rename
	if rename == nil {
		rename = (*fsroot.Dir).Rename
	}
	renameErr := rename(o.parent, name, filepath.Base(path))
	if renameErr != nil {
		current, currentErr := o.parent.Lstat(filepath.Base(path))
		if currentErr != nil || !os.SameFile(stagedInfo, current) {
			stage, stageErr := o.parent.Lstat(name)
			if currentErr != nil || stageErr != nil || !os.SameFile(createdInfo, stage) || !os.SameFile(o.info, current) {
				out.Uncertain = true
				o.uncertain = true
			}
			out.Err = errors.Join(renameErr, currentErr, stageErr)
			return
		}
		// An actual failed native call may nevertheless have published. Evidence
		// of the staged inode at the target retains that fact without rollback.
	}
	published = true
	o.uncertain = true
	out.Published = true
	out.HardLinks = o.links
	out.Err = errors.Join(renameErr, s.at("published", path))
	// Observe publication before later flush/cancellation failures. Never rollback.
	currentParent, currentChain, parentErr := pinParent(path)
	if currentParent != nil {
		out.Err = errors.Join(out.Err, s.closeHandle("saved-parent", currentParent))
	}
	if parentErr != nil || !sameChain(o.chain, currentChain) {
		out.Uncertain = true
		out.Err = errors.Join(out.Err, parentErr, errors.New("published logical parent changed"))
		return
	}
	f, info, e := o.parent.OpenRegular(filepath.Base(path))
	if e == nil {
		statErr = verifyStageMetadata(f, o)
		if statErr == nil && os.SameFile(stagedInfo, info) && sameSecurity(stagedInfo, info) {
			raw, readErr := io.ReadAll(io.LimitReader(f, int64(candidate.Len())+1))
			if readErr == nil && bytes.Equal(raw, data) {
				o.source = candidate
				o.info = info
				out.Saved = candidate
				o.uncertain = false
			} else {
				e = errors.Join(readErr, errors.New("published content changed before observation"))
			}
		} else {
			e = errors.Join(statErr, errors.New("published entry identity or metadata changed"))
		}
		out.Err = errors.Join(out.Err, s.closeHandle("saved", f))
	}
	if e != nil {
		out.Uncertain = true
		out.Err = errors.Join(out.Err, e)
	}
	if err = s.at("directory-flush", path); err == nil {
		err = s.syncDirectory(o.parent)
	}
	out.Err = errors.Join(out.Err, err)
	if err = checkpoint(ctx, b, 1); err != nil {
		out.Err = errors.Join(out.Err, err)
	}
	return
}

func (s *nativeWriteSet) matches(path string, source parser.Source, absent bool) bool {
	o := s.entries[path]
	return o != nil && o.absent == absent && (absent || o.original == source)
}

// Never remove a detected replacement of our private stage name. The remaining
// no-CAS check/unlink interval is subject to the documented external-writer gap.
func (s *nativeWriteSet) cleanupStage(parent *fsroot.Dir, name string, owned fs.FileInfo) error {
	actual, err := parent.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !os.SameFile(owned, actual) {
		return errors.New("staging entry substituted; foreign artifact retained")
	}
	remove := s.removeStage
	if remove == nil {
		remove = (*fsroot.Dir).Remove
	}
	return remove(parent, name)
}

func reserveNativeParents(b *parser.OperationBudget, path string) error {
	n := pathDepth(path) + 1
	if err := b.ChargeWork(n*4096 + int64(len(path))*2 + 1); err != nil {
		return err
	}
	return b.ChargeNodes(n*4 + 1)
}

// verifyStageMetadata proves the file carries exactly the target's mode,
// owner and extended attributes, and still satisfies the target contract.
func verifyStageMetadata(f *os.File, o *nativeObservation) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode() != o.info.Mode() || !sameOwner(info, o.info) {
		return errors.New("staged mode or owner differs from target")
	}
	attrs, err := readXattrs(f)
	if err != nil {
		return err
	}
	if !equalXattrs(attrs, o.xattrs) {
		return errors.New("staged extended attributes differ from target")
	}
	return nil
}
