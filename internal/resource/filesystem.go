package resource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
)

var (
	// ErrFilesystemUnavailable identifies an unsupported native guarantee or
	// denied native namespace. It is never authoritative absence.
	ErrFilesystemUnavailable = errors.New("native resource guarantee is unavailable")
	errNativePolicy          = ErrFilesystemUnavailable
	errFilesystemUnstable    = errors.New("resource changed during acquisition")
)

// FilesystemLimits are finite operator-owned acquisition bounds. Their counters
// belong to a FilesystemRoot's operation, not an individual Read or reader handle.
// Queried-identity/cache accounting remains the higher-level provider's job.
type FilesystemLimits struct {
	Path                                PathLimits
	DirectoryEntries, TotalContentBytes int64
	SnapshotRetries                     int
}

func DefaultFilesystemLimits() FilesystemLimits {
	return FilesystemLimits{DefaultPathLimits(), 100000, 8388608, 2}
}

func (l FilesystemLimits) validate(budget *parser.OperationBudget) error {
	if budget == nil || l.Path.Bytes <= 0 || l.Path.Bytes > 1<<30 || l.Path.Depth <= 0 || l.Path.Depth > 1<<20 || l.DirectoryEntries <= 0 || l.DirectoryEntries > 1<<30 || l.TotalContentBytes < budget.Limits().DocumentBytes || l.TotalContentBytes > 1<<30 || l.SnapshotRetries < 0 || l.SnapshotRetries > 2 {
		return ErrContract
	}
	return nil
}

// FilesystemRoot owns a pinned read-only native anchor and one operation's
// acquisition accounting. No raw descriptor, generic filesystem or writer is
// exported. Construct another session with Session when reusing an authorized
// startup anchor for another operation; it retains the anchor, not its contents.
// Close prevents new acquisitions; already returned immutable facts remain valid.
type FilesystemRoot struct {
	mu               sync.RWMutex
	file             *os.File
	path, identity   string
	root             Root
	ctx              context.Context
	budget           *parser.OperationBudget
	limits           FilesystemLimits
	gate             chan struct{}
	entries, bytes   int64
	failure          atomic.Pointer[filesystemCounterFailure]
	operationContext atomic.Pointer[filesystemOperationContext]
	operation        *Operation
	// Instance-local deterministic test seams surround real native effects.
	hook     func(stage, key string) error
	open     func(*os.File, string, bool) (*os.File, error)
	metadata func(*os.File, string, bool) (*os.File, error)
	read     func(*os.File, []byte) (int, error)
	close    func(*os.File) error
}

type filesystemCounterFailure struct{ err error }
type filesystemOperationContext struct{ ctx context.Context }

func (f *FilesystemRoot) setFailure(err error) {
	f.failure.CompareAndSwap(nil, &filesystemCounterFailure{err})
}

// OpenFilesystemRoot pins an already selected canonical absolute native anchor.
// Root selection is a separate operation; untrusted reference keys never pass
// through path canonicalization. Native operations reject every encountered
// link/reparse component, including contained links, instead of resolving it.
func OpenFilesystemRoot(ctx context.Context, budget *parser.OperationBudget, path string, limits FilesystemLimits) (*FilesystemRoot, error) {
	if ctx == nil || limits.validate(budget) != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrContract
	}
	if err := nativeValidateAnchor(path); err != nil {
		return nil, err
	}
	if err := normalizationBound("path-bytes", int64(len(path)), limits.Path.Bytes); err != nil {
		return nil, err
	}
	if err := normalizationBound("path-depth", int64(strings.Count(path, string(filepath.Separator))), limits.Path.Depth); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := budget.ChargeWork(16*int64(len(path)) + 1); err != nil {
		return nil, err
	}
	if err := budget.ChargeNodes(int64(strings.Count(path, string(filepath.Separator))) + 4); err != nil {
		return nil, err
	}
	file, err := nativeOpenAnchor(path)
	if err != nil {
		return nil, err
	}
	identity, err := nativeIdentity(file)
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if err := errors.Join(ctx.Err(), budget.ChargeWork(0)); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	root, err := NewRoot(ProjectRoot)
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return newFilesystemRoot(ctx, budget, path, limits, file, identity, root), nil
}

func newFilesystemRoot(ctx context.Context, budget *parser.OperationBudget, path string, limits FilesystemLimits, file *os.File, identity string, root Root) *FilesystemRoot {
	f := &FilesystemRoot{file: file, path: path, identity: identity, root: root, ctx: ctx, budget: budget, limits: limits, gate: make(chan struct{}, 1), open: nativeOpenChild, metadata: nativeOpenMetadata, read: func(file *os.File, b []byte) (int, error) { return file.Read(b) }, close: func(file *os.File) error { return file.Close() }}
	f.gate <- struct{}{}
	return f
}

func (f *FilesystemRoot) Root() Root {
	if f == nil {
		return Root{}
	}
	return f.root
}

func (f *FilesystemRoot) Close() error {
	if f == nil {
		return ErrContract
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.file == nil {
		return fs.ErrClosed
	}
	err := f.file.Close()
	f.file = nil
	return err
}

// Session creates independent per-call accounting and requires a distinct
// operation budget over the same pinned startup directory, without reopening
// its pathname or sharing content/misses/settings.
func (f *FilesystemRoot) Session(ctx context.Context, budget *parser.OperationBudget, limits FilesystemLimits) (*FilesystemRoot, error) {
	if f == nil || ctx == nil || limits.validate(budget) != nil || budget.Token() == f.budget.Token() {
		return nil, ErrContract
	}
	if err := errors.Join(ctx.Err(), budget.ChargeWork(1)); err != nil {
		return nil, err
	}
	if err := budget.ChargeNodes(3); err != nil {
		return nil, err
	}
	file, err := f.cloneAnchor()
	if err != nil {
		return nil, err
	}
	root, err := NewRoot(ProjectRoot)
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return newFilesystemRoot(ctx, budget, f.path, limits, file, f.identity, root), nil
}

func (f *FilesystemRoot) cloneAnchor() (*os.File, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.file == nil {
		return nil, fs.ErrClosed
	}
	file, err := f.open(f.file, ".", true)
	if err != nil {
		return nil, err
	}
	id, err := nativeIdentity(file)
	if err != nil || id != f.identity {
		if err == nil {
			err = errFilesystemUnstable
		}
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func (f *FilesystemRoot) checkpoint(ctx context.Context, work int64) error {
	if err := errors.Join(f.ctx.Err(), ctx.Err()); err != nil {
		return err
	}
	if owner := f.operationContext.Load(); owner != nil {
		if err := owner.ctx.Err(); err != nil {
			return err
		}
	}
	if failure := f.failure.Load(); failure != nil {
		return failure.err
	}
	return f.budget.ChargeWork(work)
}

func (f *FilesystemRoot) enter(ctx context.Context) error {
	if f == nil || ctx == nil {
		return ErrContract
	}
	if err := f.checkpoint(ctx, 0); err != nil {
		return err
	}
	select {
	case <-f.gate:
		return f.checkpointAcquired(ctx)
	default:
	}
	// Waiting does not reset the shared monotonic deadline. Check its existing
	// meter while also responding immediately to either caller cancellation.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.ctx.Done():
			return f.ctx.Err()
		case <-ticker.C:
			if err := f.checkpoint(ctx, 1); err != nil {
				return err
			}
		case <-f.gate:
			return f.checkpointAcquired(ctx)
		}
	}
}
func (f *FilesystemRoot) checkpointAcquired(ctx context.Context) error {
	if err := f.checkpoint(ctx, 0); err != nil {
		f.gate <- struct{}{}
		return err
	}
	return nil
}
func (f *FilesystemRoot) leave() { f.gate <- struct{}{} }

// NativeNamePolicy is a pure active-project predicate; supplied inventories must
// not use it. Actual spelling, type and identity remain native read checks.
func NativeNamePolicy(key Key) bool {
	if key.value == "" {
		return false
	}
	for part := range strings.SplitSeq(key.value, "/") {
		if part == ".git" || !nativeRepresentable(part) {
			return false
		}
	}
	return true
}

// FilesystemTarget is an opaque native locator receipt plus caller-owned original
// source. Target returns a value copy. Only exact native entry spelling yields a
// ResourceKey; outside/unproved/metadata paths retain self-only source authority.
// The caller still owns the original source acquisition/provenance guarantee.
type FilesystemTarget struct {
	root     Root
	target   Target
	identity string
}

func (t FilesystemTarget) Target() Target { return t.target }

// BindTarget checks native membership without reading/reopening source contents.
// Call before constructing the Operation; candidate/saved bytes never rebind it.
// On error the returned receipt still retains the supplied ID/source without a
// ResourceKey, so callers can keep independent same-document analysis.
func (f *FilesystemRoot) BindTarget(path, id string, source parser.Source) (FilesystemTarget, error) {
	out := FilesystemTarget{target: Target{ID: id, Source: source}}
	if f == nil || id == "" || source == (parser.Source{}) {
		return out, ErrContract
	}
	out.root = f.root
	if err := f.enter(f.ctx); err != nil {
		return out, err
	}
	defer f.leave()
	if f.operation != nil {
		return out, ErrContract
	}
	if err := normalizationBound("path-bytes", int64(len(id)), f.limits.Path.Bytes); err != nil {
		return out, err
	}
	if err := normalizationBound("path-bytes", int64(len(path)), int64(len(f.path))+1+f.limits.Path.Bytes); err != nil {
		return out, err
	}
	if err := f.checkpoint(f.ctx, 16*(int64(len(path))+int64(len(id)))+1); err != nil {
		return out, err
	}
	if err := f.budget.ChargeNodes(2); err != nil {
		return out, err
	}
	key, inside, err := FilesystemKey(f.path, path)
	if err != nil || !inside {
		return out, err
	}
	if !NativeNamePolicy(key) {
		return out, nil
	}
	lookup, err := f.acquire(f.ctx, key, false)
	if err != nil {
		return out, err
	}
	if lookup.existence != File {
		return out, nil
	}
	out.root, out.identity, out.target.ResourceKey = f.root, lookup.identity, key
	return out, nil
}

// FilesystemReader is bound to one exact Operation. All readers from the same
// root share its counters; replacing a reader does not reset allowance. Read is
// safe for concurrent calls, serialized at the finite acquisition boundary.
type FilesystemReader struct {
	filesystem *FilesystemRoot
	operation  *Operation
}

func (f *FilesystemRoot) Reader(operation *Operation, targets ...FilesystemTarget) (*FilesystemReader, error) {
	if f == nil || operation == nil || operation.root != f.root || operation.budget.Token() != f.budget.Token() {
		return nil, ErrContract
	}
	if err := f.enter(operation.ctx); err != nil {
		return nil, err
	}
	defer f.leave()
	if f.operation != nil && f.operation != operation {
		return nil, ErrContract
	}
	if len(targets) > len(operation.targets) {
		return nil, ErrContract
	}
	if err := f.budget.ChargeNodes(int64(len(targets)+len(operation.targets)) + 3); err != nil {
		return nil, err
	}
	for _, target := range operation.targets {
		if err := normalizationBound("path-bytes", int64(len(target.ID)), f.limits.Path.Bytes); err != nil {
			return nil, err
		}
		if err := f.checkpoint(operation.ctx, 16*(int64(len(target.ID))+int64(len(target.ResourceKey.value)))+int64(target.Source.Len())+1); err != nil {
			return nil, err
		}
	}
	for _, proof := range targets {
		if err := normalizationBound("path-bytes", int64(len(proof.target.ID)), f.limits.Path.Bytes); err != nil {
			return nil, err
		}
		if err := f.checkpoint(operation.ctx, 16*(int64(len(proof.target.ID))+int64(len(proof.target.ResourceKey.value)))+int64(proof.target.Source.Len())+1); err != nil {
			return nil, err
		}
	}
	proofs := make(map[string]FilesystemTarget, len(targets))
	for _, proof := range targets {
		if proof.root != f.root || proof.target.ID == "" || proof.target.Source == (parser.Source{}) || proof.target.ResourceKey.value != "" && proof.identity == "" {
			return nil, ErrContract
		}
		if _, exists := proofs[proof.target.ID]; exists {
			return nil, ErrContract
		}
		proofs[proof.target.ID] = proof
	}
	for _, target := range operation.targets {
		if target.ResourceKey.value != "" {
			proof, ok := proofs[target.ID]
			if !ok || proof.target != target {
				return nil, ErrContract
			}
		}
	}
	if f.operation == nil {
		var size int64
		for _, target := range operation.targets {
			if int64(target.Source.Len()) > f.budget.Limits().DocumentBytes {
				return nil, &parser.LimitExceeded{Name: "document-bytes", Observed: int64(target.Source.Len()), Allowed: f.budget.Limits().DocumentBytes}
			}
			size += int64(target.Source.Len())
		}
		f.operation = operation
		f.operationContext.Store(&filesystemOperationContext{operation.ctx})
		if size > f.limits.TotalContentBytes {
			err := &parser.LimitExceeded{Name: "total-content-bytes", Observed: size, Allowed: f.limits.TotalContentBytes}
			f.setFailure(err)
			return nil, err
		}
		f.bytes = size
	}
	return &FilesystemReader{filesystem: f, operation: operation}, nil
}

// FilesystemReadError retains real I/O causes independently of known facts and a
// concurrent runtime stop. Read may return a valid partial Spec with this error.
// Supply both the recorded Spec and its error to FilesystemReport.Append.
// It retains every cause using the existing Result contract; do not discard
// the Spec when err is nonnil.
type FilesystemReadError struct{ IO, Stop error }

func (e *FilesystemReadError) Error() string {
	if joined := errors.Join(e.IO, e.Stop); joined != nil {
		return joined.Error()
	}
	return "resource acquisition failed"
}
func (e *FilesystemReadError) Unwrap() []error {
	var out []error
	if e.IO != nil {
		out = append(out, e.IO)
	}
	if e.Stop != nil {
		out = append(out, e.Stop)
	}
	return out
}

func (r *FilesystemReader) Read(ctx context.Context, request Request) (ObservationSpec, error) {
	out, _, err := r.readReceipt(ctx, request, filesystemReceipt{})
	return out, err
}

func (r *FilesystemReader) read(ctx context.Context, request Request, receipt *filesystemReceipt) (ObservationSpec, error) {
	if r == nil || r.filesystem == nil || ctx == nil || request.view.operation != r.operation || request.identity == nil {
		return ObservationSpec{}, ErrContract
	}
	f := r.filesystem
	if err := f.enter(ctx); err != nil {
		return filesystemStop(Unknown, err), err
	}
	defer f.leave()
	if err := f.budget.ChargeNodes(3); err != nil {
		return filesystemStop(Unknown, err), err
	}
	switch request.Class() {
	case Ignored:
		return ObservationSpec{Existence: Unknown, Content: NotRequested, Anchors: NotRequested}, nil
	case BlockedReference:
		return filesystemBlocked("Blocked local reference."), nil
	case InvalidReference:
		out := filesystemBlocked("Invalid local reference.")
		out.Problem.Kind = MalformedReference
		return out, nil
	case Local:
	default:
		return ObservationSpec{}, ErrContract
	}
	source, available, selected := request.view.Target(request.Link().Path())
	if !request.project && (!selected || !available || source.Source != request.Link().Source()) {
		return ObservationSpec{}, ErrContract
	}
	if !request.project && source.ResourceKey.value == "" {
		return filesystemBlocked("Source has no proven native resource membership."), nil
	}
	key := request.Key()
	if key.value == "" {
		return ObservationSpec{}, ErrContract
	}
	if err := f.keyBound(key); err != nil {
		return filesystemStop(Unknown, err), err
	}
	for _, state := range request.view.targets {
		if state.target.ResourceKey == key {
			if state.unavailable {
				return filesystemBlocked("Selected saved resource is unavailable."), nil
			}
			return r.selected(ctx, request, state.target.Source)
		}
	}
	if !NativeNamePolicy(key) {
		return filesystemBlocked("Native resource name is blocked or unsupported."), nil
	}
	unsupportedAnchors := request.Capability() == ParsedAnchors && !markdownResourceKey(key)
	wantContent := request.Capability() != ExistenceOnly && request.RequiredType() != DirectoryType && !unsupportedAnchors
	lookup, err := f.acquireReceipt(ctx, key, wantContent, *receipt)
	if receipt.identity == "" && lookup.identity != "" && !lookup.policy && (lookup.existence == File || lookup.existence == Directory) && !errors.Is(err, errFilesystemUnstable) {
		*receipt = filesystemReceipt{operation: r.operation, key: key, existence: lookup.existence, identity: lookup.identity}
	}
	if lookup.policy {
		return filesystemBlocked("Native resource path is blocked or unsupported."), err
	}
	if err != nil {
		var fault *FilesystemReadError
		if errors.As(err, &fault) {
			out := filesystemFailure(lookup.existence, fault.IO)
			var decode *filesystemDecodeError
			if errors.As(fault.IO, &decode) {
				out.Problem.Kind = DecodeFailure
			}
			if lookup.existence == Missing || lookup.existence == Directory {
				out = ObservationSpec{Existence: lookup.existence, Content: Inapplicable, Anchors: Inapplicable}
			}
			if lookup.source != (parser.Source{}) {
				out = ObservationSpec{Existence: File, Content: Available, Anchors: NotRequested, Source: lookup.source, Validated: lookup.validated}
			}
			if fault.IO == nil && lookup.source == (parser.Source{}) {
				out = filesystemStop(lookup.existence, fault.Stop)
			}
			return out, fault
		}
		if _, stop := InterruptionProblem(err); stop {
			return filesystemStop(lookup.existence, err), err
		}
		var decode *filesystemDecodeError
		if errors.As(err, &decode) {
			return ObservationSpec{Existence: File, Content: Failed, Anchors: NotRequested, Problem: Problem{Kind: DecodeFailure, Message: decode.Error()}}, nil
		}
		return filesystemFailure(lookup.existence, err), &FilesystemReadError{IO: err}
	}
	if lookup.existence == Missing || lookup.existence == Directory {
		return ObservationSpec{Existence: lookup.existence, Content: Inapplicable, Anchors: Inapplicable}, nil
	}
	if lookup.source == (parser.Source{}) {
		if unsupportedAnchors {
			return ObservationSpec{Existence: File, Content: NotRequested, Anchors: Unavailable, Problem: Problem{Kind: NotSupplied, Message: "Anchor interpretation is unavailable for this resource type."}}, nil
		}
		return ObservationSpec{Existence: File, Content: NotRequested, Anchors: NotRequested}, nil
	}
	return ObservationSpec{Existence: File, Content: Available, Anchors: NotRequested, Source: lookup.source, Validated: lookup.validated}, nil
}

func (r *FilesystemReader) selected(ctx context.Context, request Request, source parser.Source) (ObservationSpec, error) {
	if request.Capability() == ExistenceOnly || request.RequiredType() == DirectoryType {
		return ObservationSpec{Existence: File, Content: NotRequested, Anchors: NotRequested}, nil
	}
	if err := r.filesystem.checkpoint(ctx, 0); err != nil {
		return filesystemStop(File, err), err
	}
	if err := normalizationBound("document-bytes", int64(source.Len()), r.filesystem.budget.Limits().DocumentBytes); err != nil {
		return filesystemStop(File, err), err
	}
	if err := r.filesystem.checkpoint(ctx, int64(source.Len())+1); err != nil {
		return filesystemStop(File, err), err
	}
	if !utf8.Valid(source.Bytes()) {
		return ObservationSpec{Existence: File, Content: Failed, Anchors: NotRequested, Problem: Problem{Kind: DecodeFailure, Message: "Selected resource content is not UTF-8."}}, nil
	}
	evidence, err := r.operation.ValidateContent(source)
	if err != nil {
		return filesystemStop(File, err), err
	}
	out := ObservationSpec{Existence: File, Content: Available, Anchors: NotRequested, Source: source, Validated: evidence}
	if stop := r.filesystem.checkpoint(ctx, 0); stop != nil {
		return out, &FilesystemReadError{Stop: stop}
	}
	return out, nil
}

func markdownResourceKey(key Key) bool {
	extension := filepath.Ext(key.value)
	return strings.EqualFold(extension, ".md") || strings.EqualFold(extension, ".markdown")
}

func filesystemBlocked(message string) ObservationSpec {
	return ObservationSpec{Existence: Unknown, Content: Unavailable, Anchors: Unavailable, Problem: Problem{Kind: Blocked, Message: message}}
}
func filesystemFailure(existence Existence, err error) ObservationSpec {
	if existence == "" {
		existence = Unknown
	}
	return ObservationSpec{Existence: existence, Content: Failed, Anchors: NotRequested, Problem: Problem{Kind: ReadFailure, Message: fmt.Sprintf("Resource acquisition failed: %v", err)}}
}
func filesystemStop(existence Existence, err error) ObservationSpec {
	if existence == "" {
		existence = Unknown
	}
	if existence == Missing || existence == Directory {
		return ObservationSpec{Existence: existence, Content: Inapplicable, Anchors: Inapplicable}
	}
	problem, ok := InterruptionProblem(err)
	if !ok {
		return filesystemFailure(existence, err)
	}
	return ObservationSpec{Existence: existence, Content: Interrupted, Anchors: NotRequested, Problem: problem}
}

func (f *FilesystemRoot) keyBound(key Key) error {
	if err := normalizationBound("path-bytes", int64(len(key.value)), f.limits.Path.Bytes); err != nil {
		return err
	}
	return normalizationBound("path-depth", int64(strings.Count(key.value, "/")+1), f.limits.Path.Depth)
}

type filesystemLookup struct {
	existence Existence
	identity  string
	source    parser.Source
	validated ContentEvidence
	policy    bool
}
type filesystemMetadata struct {
	info     fs.FileInfo
	identity string
}

func filesystemEveryCause(err error, predicate func(error) bool) bool {
	if err == nil {
		return false
	}
	if multiple, ok := err.(interface{ Unwrap() []error }); ok {
		causes := multiple.Unwrap()
		if len(causes) == 0 {
			return predicate(err)
		}
		for _, cause := range causes {
			if !filesystemEveryCause(cause, predicate) {
				return false
			}
		}
		return true
	}
	if cause := errors.Unwrap(err); cause != nil {
		return filesystemEveryCause(cause, predicate)
	}
	return predicate(err)
}
func filesystemPolicyError(err error) bool { return filesystemEveryCause(err, nativePolicyError) }

func inspectFilesystemMetadata(parent *os.File, name string, open func(*os.File, string, bool) (*os.File, error)) (*os.File, filesystemMetadata, error) {
	file, err := open(parent, name, false)
	if err != nil {
		if filesystemEveryCause(err, func(cause error) bool { return errors.Is(cause, fs.ErrPermission) }) {
			metadata, statErr := nativeStatAt(parent, name)
			if statErr == nil {
				return nil, metadata, nil
			}
			return nil, filesystemMetadata{}, errors.Join(err, statErr)
		}
		return nil, filesystemMetadata{}, err
	}
	info, statErr := file.Stat()
	id, idErr := nativeIdentity(file)
	return file, filesystemMetadata{info, id}, errors.Join(statErr, idErr)
}

func nativeGitMetadataSnapshot(parent *os.File, name string) (filesystemMetadata, error) {
	file, metadata, err := inspectFilesystemMetadata(parent, name, func(parent *os.File, name string, _ bool) (*os.File, error) {
		if name == ".git" {
			return nativeOpenGitMetadata(parent)
		}
		return nativeOpenGitAliasMetadata(parent, name)
	})
	if file != nil {
		err = errors.Join(err, file.Close())
	}
	return metadata, err
}

type filesystemStep struct {
	parent         *os.File
	name, identity string
	directory      bool
}

func (f *FilesystemRoot) callHook(stage, key string) error {
	if f.hook != nil {
		return f.hook(stage, key)
	}
	return nil
}

func (f *FilesystemRoot) acquire(ctx context.Context, key Key, content bool) (filesystemLookup, error) {
	return f.acquireReceipt(ctx, key, content, filesystemReceipt{})
}

func (f *FilesystemRoot) acquireReceipt(ctx context.Context, key Key, content bool, prior filesystemReceipt) (filesystemLookup, error) {
	if err := f.keyBound(key); err != nil {
		return filesystemLookup{existence: Unknown}, err
	}
	var last filesystemLookup
	for attempt := 0; attempt <= f.limits.SnapshotRetries; attempt++ {
		if err := f.checkpoint(ctx, 16*int64(len(key.value))+256*int64(strings.Count(key.value, "/")+1)+1); err != nil {
			return last, err
		}
		if err := f.budget.ChargeNodes(int64(strings.Count(key.value, "/")*8) + 12); err != nil {
			return last, err
		}
		lookup, err := f.attemptReceipt(ctx, key, content, prior)
		lookup, err = retainFilesystemReceipt(prior, lookup, err)
		// Native calls cannot be asynchronously interrupted. Observe stops even
		// when the call itself failed, so an I/O failure cannot hide cancellation
		// or quota exhaustion that occurred during that call or its cleanup.
		if stop := f.checkpoint(ctx, 0); stop != nil {
			var prior *FilesystemReadError
			if errors.As(err, &prior) {
				err = &FilesystemReadError{IO: prior.IO, Stop: errors.Join(prior.Stop, stop)}
			} else if _, interrupted := InterruptionProblem(err); interrupted {
				err = &FilesystemReadError{Stop: errors.Join(err, stop)}
			} else {
				err = &FilesystemReadError{IO: err, Stop: stop}
			}
		}
		last = lookup
		// A retry must not erase an independent read/close failure that happened
		// alongside instability. Only a pure unstable observation is retryable.
		if !filesystemEveryCause(err, func(e error) bool { return errors.Is(e, errFilesystemUnstable) }) {
			return lookup, err
		}
		if attempt == f.limits.SnapshotRetries {
			return lookup, &FilesystemReadError{IO: err}
		}
		if err := f.callHook("retry", key.value); err != nil {
			return lookup, err
		}
	}
	return last, errFilesystemUnstable
}

func (f *FilesystemRoot) attemptReceipt(ctx context.Context, key Key, content bool, prior filesystemReceipt) (out filesystemLookup, err error) {
	out.existence = Unknown
	parent, err := f.cloneAnchor()
	if err != nil {
		return out, err
	}
	handles := []*os.File{parent}
	defer func() {
		var closeErr error
		for i := len(handles) - 1; i >= 0; i-- {
			closeErr = errors.Join(closeErr, f.close(handles[i]))
		}
		if closeErr != nil {
			var prior *FilesystemReadError
			if errors.As(err, &prior) {
				err = &FilesystemReadError{IO: errors.Join(prior.IO, closeErr), Stop: prior.Stop}
			} else if _, stop := InterruptionProblem(err); stop {
				err = &FilesystemReadError{IO: closeErr, Stop: err}
			} else {
				err = &FilesystemReadError{IO: errors.Join(err, closeErr)}
			}
		}
	}()
	steps := make([]filesystemStep, 0, strings.Count(key.value, "/")+1)
	defer func() {
		if err != nil || out.policy {
			return
		}
		if verifyErr := f.verify(ctx, steps, key.value); verifyErr != nil {
			out.source, out.validated = parser.Source{}, ContentEvidence{}
			err = verifyErr
			return
		}
		if stop := errors.Join(f.callHook("after-verify", key.value), f.checkpoint(ctx, 0)); stop != nil {
			if out.source != (parser.Source{}) {
				err = &FilesystemReadError{Stop: stop}
			} else {
				err = stop
			}
		}
	}()
	parts := strings.Split(key.value, "/")
	prefixEnd := 0
	for i, name := range parts {
		if i > 0 {
			prefixEnd++
		}
		prefixEnd += len(name)
		prefix := key.value[:prefixEnd]
		found, findErr := f.exact(ctx, parent, name, prefix)
		if findErr != nil {
			return out, findErr
		}
		if !found {
			if strings.EqualFold(name, ".git") {
				alias, aliasErr := nativeGitMetadataSnapshot(parent, name)
				if aliasErr == nil {
					blocked, gitErr := f.gitAlias(parent, alias.identity)
					if gitErr != nil {
						return out, gitErr
					}
					if blocked {
						out.policy = true
						return out, nil
					}
				} else if !filesystemEveryCause(aliasErr, func(e error) bool { return errors.Is(e, fs.ErrNotExist) }) {
					if filesystemPolicyError(aliasErr) {
						out.policy = true
						return out, nil
					}
					return out, aliasErr
				}
			}
			out.existence = Missing
			return out, nil
		}
		if name == ".git" {
			out.policy = true
			return out, nil
		}
		if err := f.callHook("before-open", prefix); err != nil {
			return out, err
		}
		if err := f.checkpoint(ctx, 0); err != nil {
			return out, err
		}
		metadata, snapshot, openErr := inspectFilesystemMetadata(parent, name, f.metadata)
		if metadata != nil {
			handles = append(handles, metadata)
		}
		if openErr != nil {
			if filesystemPolicyError(openErr) {
				out.policy = true
				return out, nil
			}
			if filesystemEveryCause(openErr, func(e error) bool { return errors.Is(e, fs.ErrNotExist) }) {
				return out, errFilesystemUnstable
			}
			return out, openErr
		}
		if err := f.callHook("after-open", prefix); err != nil {
			return out, err
		}
		info, identity := snapshot.info, snapshot.identity
		if strings.EqualFold(name, ".git") {
			blocked, gitErr := f.gitAlias(parent, identity)
			if gitErr != nil {
				return out, gitErr
			}
			if blocked {
				out.policy = true
				return out, nil
			}
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			out.policy = true
			return out, nil
		}
		directory := i < len(parts)-1
		steps = append(steps, filesystemStep{parent, name, identity, info.IsDir()})
		if directory {
			if !info.IsDir() {
				out.existence = Missing
				return out, nil
			}
			if err := errors.Join(f.callHook("before-directory-open", prefix), f.checkpoint(ctx, 0)); err != nil {
				return out, err
			}
			next, openErr := f.open(parent, name, true)
			if openErr != nil {
				if filesystemPolicyError(openErr) {
					out.policy = true
					return out, nil
				}
				return out, openErr
			}
			handles = append(handles, next)
			if err := f.callHook("after-directory-open", prefix); err != nil {
				return out, err
			}
			nextID, idErr := nativeIdentity(next)
			if idErr != nil {
				return out, idErr
			}
			if nextID != identity {
				return out, errFilesystemUnstable
			}
			parent = next
			continue
		}
		out.identity = identity
		if info.IsDir() {
			out.existence = Directory
		} else {
			out.existence = File
		}
		if prior.identity != "" && (identity != prior.identity || out.existence != prior.existence) {
			return out, errFilesystemReceiptChanged
		}
		if content && out.existence == File {
			if err := errors.Join(f.callHook("before-content-open", prefix), f.checkpoint(ctx, 0)); err != nil {
				return out, err
			}
			file, openErr := f.open(parent, name, false)
			if openErr != nil {
				if filesystemPolicyError(openErr) {
					out.policy = true
					return out, nil
				}
				return out, openErr
			}
			handles = append(handles, file)
			if err := f.callHook("after-content-open", prefix); err != nil {
				return out, err
			}
			id, idErr := nativeIdentity(file)
			if idErr != nil {
				return out, idErr
			}
			if id != identity {
				return out, errFilesystemUnstable
			}
			before, statErr := file.Stat()
			if statErr != nil {
				return out, statErr
			}
			if !before.Mode().IsRegular() {
				out.policy = true
				return out, nil
			}
			data, readErr := f.readContent(ctx, file, key.value, before.Size())
			if readErr != nil {
				return out, readErr
			}
			after, statErr := file.Stat()
			if statErr != nil {
				return out, statErr
			}
			if before.Size() != after.Size() || before.ModTime() != after.ModTime() || int64(len(data)) != after.Size() {
				return out, errFilesystemUnstable
			}
			if !utf8.Valid(data) {
				return out, &filesystemDecodeError{}
			}
			if err := f.budget.ChargeWork(2*int64(len(data)) + 1); err != nil {
				return out, err
			}
			if err := f.budget.ChargeNodes(1); err != nil {
				return out, err
			}
			source := parser.NewSource(data)
			if f.operation == nil {
				return out, ErrContract
			}
			evidence, validateErr := f.operation.ValidateContent(source)
			if validateErr != nil {
				return out, validateErr
			}
			out.source, out.validated = source, evidence
		}
	}
	return out, nil
}

func (f *FilesystemRoot) verify(ctx context.Context, steps []filesystemStep, key string) error {
	if err := f.callHook("before-verify", key); err != nil {
		return err
	}
	for _, step := range steps {
		found, err := f.exact(ctx, step.parent, step.name, key)
		if err != nil {
			return err
		}
		if !found {
			return errFilesystemUnstable
		}
		file, metadata, err := inspectFilesystemMetadata(step.parent, step.name, f.metadata)
		if err != nil {
			if file != nil {
				err = errors.Join(err, f.close(file))
			}
			if filesystemEveryCause(err, func(e error) bool { return errors.Is(e, fs.ErrNotExist) }) || filesystemPolicyError(err) {
				return errFilesystemUnstable
			}
			return err
		}
		if file != nil {
			if err := f.close(file); err != nil {
				return err
			}
		}
		if metadata.identity != step.identity || metadata.info.IsDir() != step.directory {
			return errFilesystemUnstable
		}
	}
	return nil
}

type filesystemDecodeError struct{}

func (*filesystemDecodeError) Error() string { return "resource content is not UTF-8" }

func (f *FilesystemRoot) gitAlias(parent *os.File, identity string) (bool, error) {
	marker, err := nativeGitMetadataSnapshot(parent, ".git")
	if filesystemEveryCause(err, func(e error) bool { return errors.Is(e, fs.ErrNotExist) }) {
		return false, nil
	}
	if err != nil {
		if filesystemPolicyError(err) {
			return true, nil
		}
		return false, err
	}
	return marker.identity == identity, nil
}

func (f *FilesystemRoot) exact(ctx context.Context, parent *os.File, name, key string) (found bool, err error) {
	if err := f.checkpoint(ctx, 1); err != nil {
		return false, err
	}
	if err := f.callHook("before-enumerate", key); err != nil {
		return false, err
	}
	if err := f.checkpoint(ctx, 0); err != nil {
		return false, err
	}
	before, err := parent.Stat()
	if err != nil {
		return false, err
	}
	directory, err := f.open(parent, ".", true)
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, f.close(directory)) }()
	for {
		if err := f.checkpoint(ctx, 1); err != nil {
			return false, err
		}
		if err := f.budget.ChargeNodes(1); err != nil {
			return false, err
		}
		entries, readErr := directory.Readdirnames(1)
		for _, entry := range entries {
			f.entries++
			if f.entries > f.limits.DirectoryEntries {
				err := &parser.LimitExceeded{Name: "directory-entries", Observed: f.entries, Allowed: f.limits.DirectoryEntries}
				f.setFailure(err)
				return false, err
			}
			if err := f.budget.ChargeWork(2*int64(len(entry)) + 1); err != nil {
				return false, err
			}
			if err := f.callHook("entry", key); err != nil {
				return false, err
			}
			if err := f.checkpoint(ctx, 0); err != nil {
				return false, err
			}
			if entry == name {
				found = true
				break
			}
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return false, readErr
		}
		if found || errors.Is(readErr, io.EOF) {
			break
		}
		if len(entries) == 0 {
			return false, io.ErrNoProgress
		}
	}
	after, err := parent.Stat()
	if err != nil {
		return false, err
	}
	if before.ModTime() != after.ModTime() {
		return false, errFilesystemUnstable
	}
	if err := f.checkpoint(ctx, 0); err != nil {
		return false, err
	}
	return found, nil
}

func (f *FilesystemRoot) readContent(ctx context.Context, file *os.File, key string, knownSize int64) ([]byte, error) {
	if knownSize < 0 {
		return nil, errors.New("negative resource size")
	}
	if err := normalizationBound("document-bytes", knownSize, f.budget.Limits().DocumentBytes); err != nil {
		return nil, err
	}
	if err := normalizationBound("total-content-bytes", f.bytes+knownSize, f.limits.TotalContentBytes); err != nil {
		f.setFailure(err)
		return nil, err
	}
	data := make([]byte, 0, min(knownSize, 32768))
	var buffer [32768]byte
	for {
		if err := f.checkpoint(ctx, 0); err != nil {
			return nil, err
		}
		if err := f.callHook("before-read", key); err != nil {
			return nil, err
		}
		if err := f.checkpoint(ctx, 0); err != nil {
			return nil, err
		}
		remaining := min(f.budget.Limits().DocumentBytes-int64(len(data)), f.limits.TotalContentBytes-f.bytes)
		n, readErr := f.read(file, buffer[:min(int64(len(buffer)), remaining+1)])
		if n < 0 || n > len(buffer) {
			return nil, errors.New("invalid native reader count")
		}
		f.bytes += int64(n)
		chargeErr := f.budget.ChargeWork(3*int64(n) + 1)
		var stop error
		if int64(len(data))+int64(n) > f.budget.Limits().DocumentBytes {
			stop = &parser.LimitExceeded{Name: "document-bytes", Observed: int64(len(data)) + int64(n), Allowed: f.budget.Limits().DocumentBytes}
		}
		if f.bytes > f.limits.TotalContentBytes {
			stop = &parser.LimitExceeded{Name: "total-content-bytes", Observed: f.bytes, Allowed: f.limits.TotalContentBytes}
			f.setFailure(stop)
		}
		if stop == nil {
			stop = chargeErr
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, &FilesystemReadError{IO: readErr, Stop: stop}
		}
		if stop != nil {
			return nil, stop
		}
		if needed := len(data) + n; needed > cap(data) {
			if err := f.budget.ChargeWork(int64(len(data))); err != nil {
				return nil, err
			}
			grown := make([]byte, len(data), min(int64(needed)*2, f.budget.Limits().DocumentBytes))
			copy(grown, data)
			data = grown
		}
		data = append(data, buffer[:n]...)
		if err := f.callHook("after-read", key); err != nil {
			return nil, err
		}
		if err := f.checkpoint(ctx, 0); err != nil {
			return nil, err
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if n == 0 {
			return nil, io.ErrNoProgress
		}
	}
	return data, nil
}
