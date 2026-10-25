package resource

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

// ServiceLimits are operator policy. Identities counts the union of admitted
// targets, supplied entries/parents and queried keys. Lookups counts distinct
// requested keys. Both also bound retained flights; derived entries have the
// same finite Identities cap. Authoritative facts are never evicted/reopened.
type ServiceLimits struct{ Identities, Lookups, TotalContentBytes, TargetFiles int64 }

func DefaultServiceLimits() ServiceLimits { return ServiceLimits{25000, 25000, 8388608, 5000} }
func (l ServiceLimits) valid(op *Operation) bool {
	return op != nil && l.Identities > 0 && l.Identities <= 1<<30 && l.Lookups > 0 && l.Lookups <= 1<<30 && l.TotalContentBytes >= op.budget.Limits().DocumentBytes && l.TotalContentBytes <= 1<<30 && l.TargetFiles > 0 && l.TargetFiles <= 1<<30
}

type serviceKey struct {
	key    Key
	view   *viewIdentity
	denied bool
}
type cachedResource struct {
	existence, content       ObservationSpec
	existenceErr, contentErr error
	hasExistence, hasContent bool
	receipt                  filesystemReceipt
	flight                   chan struct{}
}
type serviceJob func(context.Context)
type serviceValidation struct {
	source   parser.Source
	targetID string
}

// Service retains first resource facts for one exact Operation. One owned
// worker coalesces acquisition and derived parsing; caller cancellation stops
// only that caller's wait. Close cancels and joins the worker before the owner
// closes its native root. A stuck OS call still limits cancellation/Close latency.
// The zero value is invalid. Read returns facts AND independently retained errors;
// project both with ServiceReport. No query registers a target or a write.
type Service struct {
	operation        *Operation
	filesystem       *FilesystemReader
	supplied         *SuppliedReader
	limits           ServiceLimits
	ctx              context.Context
	cancel           context.CancelFunc
	done             chan struct{}
	wake             chan struct{}
	mu               sync.Mutex
	closed           bool
	stop             error
	identities       map[Key]bool
	unkeyed          int64
	lookups          map[Key]bool
	entries          map[serviceKey]*cachedResource
	selected         map[Key]int
	bytes            int64
	queue            []serviceJob
	parsed           map[ParseCacheKey]*cachedParse
	anchors          map[anchorOwnerKey]*cachedAnchors
	validations      map[*contentIdentity]serviceValidation
	targetEvidence   map[anchorOwnerKey]ContentEvidence
	targetParseKeys  map[anchorOwnerKey]ParseCacheKey
	targetParseReady map[*viewIdentity]*targetParseIndex
	// read is an internal deterministic-test seam. Production always uses the
	// exact registered adapter; it is never exported as generic filesystem authority.
	read func(context.Context, Request, filesystemReceipt) (ObservationSpec, filesystemReceipt, error)
	// Private deterministic production boundary, like read. It never accepts a
	// caller-supplied builder or enlarges resource authority.
	buildAnchors func(context.Context, *Operation, ParseCacheKey, parser.ParseResult) (AnchorResult, error)
}

// NewFilesystemService registers at most one service per operation. Repeated
// construction with the same native root and limits returns that same service;
// an alternate adapter or policy cannot reset admitted identities/bytes/work.
// Native raw retry-byte accounting must use the same total-content policy.
func NewFilesystemService(reader *FilesystemReader, limits ServiceLimits) (*Service, error) {
	if reader == nil || reader.filesystem == nil || reader.operation == nil || limits.TotalContentBytes != reader.filesystem.limits.TotalContentBytes {
		return nil, ErrContract
	}
	return newService(reader.operation, reader, nil, limits)
}

// NewSuppliedService indexes all already validated supplied identities/content,
// including excluded seeds, without granting selected-target authority.
func NewSuppliedService(reader *SuppliedReader, limits ServiceLimits) (*Service, error) {
	if reader == nil || reader.operation == nil {
		return nil, ErrContract
	}
	return newService(reader.operation, nil, reader, limits)
}
func newService(op *Operation, native *FilesystemReader, supplied *SuppliedReader, limits ServiceLimits) (*Service, error) {
	if !limits.valid(op) {
		return nil, ErrContract
	}
	op.serviceMu.Lock()
	defer op.serviceMu.Unlock()
	if old := op.service; old != nil {
		if old.limits != limits || (native == nil) != (old.filesystem == nil) || (supplied == nil) != (old.supplied == nil) || native != nil && old.filesystem.filesystem != native.filesystem || supplied != nil && old.supplied != supplied {
			return nil, ErrContract
		}
		return old, nil
	}
	if err := op.Checkpoint(1); err != nil {
		return nil, err
	}
	if err := op.budget.ChargeNodes(13); err != nil {
		return nil, err
	}
	s := &Service{operation: op, filesystem: native, supplied: supplied, limits: limits, done: make(chan struct{}), wake: make(chan struct{}, 1), identities: map[Key]bool{}, lookups: map[Key]bool{}, entries: map[serviceKey]*cachedResource{}, selected: map[Key]int{}, parsed: map[ParseCacheKey]*cachedParse{}, anchors: map[anchorOwnerKey]*cachedAnchors{}, validations: map[*contentIdentity]serviceValidation{}, targetEvidence: map[anchorOwnerKey]ContentEvidence{}, targetParseKeys: map[anchorOwnerKey]ParseCacheKey{}, targetParseReady: map[*viewIdentity]*targetParseIndex{}}
	targetCount := len(op.targets)
	if supplied != nil {
		if err := op.Checkpoint(int64(len(supplied.entries)) + 1); err != nil {
			return nil, err
		}
		targetCount = 0
		for _, fact := range supplied.entries {
			if fact.seed {
				targetCount++
			}
		}
	}
	if err := inputBound("files", "target-files", int64(targetCount), limits.TargetFiles); err != nil {
		return nil, err
	}

	add := func(key Key, source parser.Source) error {
		if err := op.Checkpoint(int64(len(key.value)) + 1); err != nil {
			return err
		}
		if source != (parser.Source{}) {
			if err := inputBound("resources", "document-bytes", int64(source.Len()), op.budget.Limits().DocumentBytes); err != nil {
				return err
			}
		}
		delta := int64(0)
		if key.value == "" || !s.identities[key] {
			delta = 1
		}
		if err := inputBound("resources", "resource-identities", int64(len(s.identities))+s.unkeyed+delta, limits.Identities); err != nil {
			return err
		}
		if key.value == "" {
			s.unkeyed++
		} else if delta != 0 {
			if err := op.budget.ChargeNodes(1); err != nil {
				return err
			}
			s.identities[key] = true
		}
		s.bytes += int64(source.Len())
		return inputBound("resources", "total-content-bytes", s.bytes, limits.TotalContentBytes)
	}
	if supplied != nil {
		for name, fact := range supplied.entries {
			if err := add(Key{value: name}, fact.source); err != nil {
				return nil, err
			}
		}
	} else {
		for _, target := range op.targets {
			if err := add(target.ResourceKey, target.Source); err != nil {
				return nil, err
			}
		}
	}
	for i, target := range op.targets {
		if target.ResourceKey.value != "" {
			if err := op.Checkpoint(int64(len(target.ResourceKey.value)) + 1); err != nil {
				return nil, err
			}
			if err := op.budget.ChargeNodes(1); err != nil {
				return nil, err
			}
			s.selected[target.ResourceKey] = i
		}
	}
	if err := op.Checkpoint(0); err != nil {
		return nil, err
	}
	s.ctx, s.cancel = context.WithCancel(op.ctx)
	s.buildAnchors = buildAnchorIndex
	if native != nil {
		s.read = native.readReceipt
	} else {
		s.read = func(ctx context.Context, q Request, _ filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
			spec, err := supplied.Read(ctx, q)
			return spec, filesystemReceipt{}, err
		}
	}
	op.service = s
	go s.worker()
	return s, nil
}

// Close is idempotent and joins the single owned worker. Returned immutable
// facts remain usable by Record/projection after close; no new I/O is allowed.
func (s *Service) Close() error {
	if s == nil || s.operation == nil {
		return ErrContract
	}
	s.cancel()
	<-s.done
	return nil
}
func (s *Service) checkpoint(ctx context.Context, units int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.operation.Checkpoint(units)
}
func (s *Service) worker() {
	defer close(s.done)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := s.checkpoint(s.ctx, 1); err != nil {
			s.finishWorker(err)
			return
		}
		s.mu.Lock()
		if s.stop != nil {
			err := s.stop
			s.mu.Unlock()
			s.finishWorker(err)
			return
		}
		if len(s.queue) > 0 {
			job := s.queue[0]
			s.queue[0] = nil
			s.queue = s.queue[1:]
			s.mu.Unlock()
			job(s.ctx)
			continue
		}
		s.mu.Unlock()
		select {
		case <-s.ctx.Done():
		case <-s.wake:
		case <-ticker.C:
		}
	}
}
func (s *Service) finishWorker(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.stop == nil {
		s.stop = err
	}
	// Work/flight storage was admitted while execution was live. Cleanup releases
	// all waiters after stop without admitting new records or running queued work.
	s.queue = nil
	for _, e := range s.entries {
		if e.flight != nil {
			close(e.flight)
			e.flight = nil
		}
	}
	for _, p := range s.parsed {
		if p.done != nil {
			close(p.done)
			p.done = nil
		}
	}
	for _, p := range s.anchors {
		if p.done != nil {
			close(p.done)
			p.done = nil
		}
	}
}
func (s *Service) enqueue(job serviceJob) {
	s.queue = append(s.queue, job)
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Service) admit(key Key) error {
	if s.stop != nil {
		return s.stop
	}
	if !s.lookups[key] && int64(len(s.lookups)) >= s.limits.Lookups {
		s.stop = &parser.LimitExceeded{Name: "resource-lookups", Observed: int64(len(s.lookups)) + 1, Allowed: s.limits.Lookups}
		return s.stop
	}
	if !s.identities[key] && int64(len(s.identities))+s.unkeyed >= s.limits.Identities {
		s.stop = &parser.LimitExceeded{Name: "resource-identities", Observed: int64(len(s.identities)) + s.unkeyed + 1, Allowed: s.limits.Identities}
		return s.stop
	}
	if err := s.operation.Checkpoint(int64(len(key.value)) + 1); err != nil {
		return err
	}
	if err := s.operation.budget.ChargeNodes(3); err != nil {
		return err
	}
	s.lookups[key] = true
	s.identities[key] = true
	return nil
}
func (s *Service) wait(ctx context.Context, done <-chan struct{}) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := s.checkpoint(ctx, 1); err != nil {
			return err
		}
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-s.ctx.Done():
			return s.ctx.Err()
		case <-ticker.C:
		}
	}
}

// Read returns each requesting link's own facts, never a cached Observation.
// Resource-only originals share one layer across views; selected replacements
// are partitioned by exact immutable view identity. Cache hits perform no I/O.
func (s *Service) Read(ctx context.Context, q Request) (ObservationSpec, error) {
	spec, err := s.readCached(ctx, q)
	if q.Capability() == ParsedAnchors && spec.Content == Available {
		anchors, anchorErr := s.Anchors(ctx, q.View(), spec.Validated)
		if anchors.state != nil {
			spec, anchorErr = applyAnchorResult(anchors, spec, anchorErr)
		}
		if errors.Is(anchorErr, ErrContract) {
			return spec, anchorErr
		}
		err = joinServiceErrors(err, anchorErr)
	}
	spec.Problem = copyProblem(spec.Problem)
	if spec.Parsed != nil {
		parsed := copyCachedParse(*spec.Parsed)
		spec.Parsed = &parsed
	}
	if err != nil && !errors.Is(err, ErrContract) {
		err = joinServiceErrors(err)
		var metadata *serviceReadError
		if errors.As(err, &metadata) {
			// Reserve a conservative multiple covering the bounded internal
			// merges on this call. Post-stop copies reconcile existing facts.
			stopped := s.operation.Checkpoint(8 * metadata.copyWork)
			if stopped == nil {
				stopped = s.operation.budget.ChargeNodes(8 * metadata.copyNodes)
			}
			if stopped != nil {
				err = joinServiceErrors(err, stopped)
			}
		}
	}
	return spec, err
}

func applyAnchorResult(anchors AnchorResult, spec ObservationSpec, prior error) (ObservationSpec, error) {
	out, err := anchors.apply(spec)
	if err != nil {
		return out, err
	}
	return out, anchorReadError(errors.Join(prior, err))
}

func (s *Service) readCached(ctx context.Context, q Request) (ObservationSpec, error) {
	if s == nil || s.operation == nil || ctx == nil || q.view.operation != s.operation || q.view.identity == nil || q.identity == nil {
		return ObservationSpec{}, ErrContract
	}
	if q.Class() != Local {
		spec := ObservationSpec{Existence: Unknown, Content: NotRequested, Anchors: NotRequested}
		switch q.Class() {
		case Ignored:
		case BlockedReference:
			spec = filesystemBlocked("Blocked local reference.")
		case InvalidReference:
			spec = suppliedUnavailable(MalformedReference, "Invalid local reference.")
		default:
			return ObservationSpec{}, ErrContract
		}
		return spec, joinServiceErrors(s.checkpoint(ctx, 0), s.ctx.Err())
	}
	callerStop := ctx.Err()
	pathLimits := DefaultPathLimits()
	if s.filesystem != nil {
		pathLimits = s.filesystem.filesystem.limits.Path
	} else {
		pathLimits = PathLimits{Bytes: s.supplied.limits.PathBytes, Depth: s.supplied.limits.PathDepth}
	}
	if err := normalizationBound("path-bytes", int64(len(q.Key().value)), pathLimits.Bytes); err != nil {
		return filesystemStop(Unknown, err), err
	}
	// These facts depend on requester authority/class rather than resource state.
	// Do not let an unproven native source or a Git self exception poison a shared
	// ordinary resource entry or lend its authority to a different requester.
	denied := false
	if s.filesystem != nil {
		source, _, _ := q.view.Target(q.Link().Path())
		denied = source.ResourceKey.value == ""
	}
	if s.supplied != nil && hasGitComponent(q.Key()) {
		i, selected := s.selected[q.Key()]
		denied = !selected || q.view.targets[i].target.ID != q.Link().Path()
	}
	key := serviceKey{key: q.Key(), denied: denied}
	_, selected := s.selected[q.Key()]
	if selected {
		key.view = q.view.identity
	}
	unsupported := q.Capability() == ParsedAnchors && !selected && !markdownResourceKey(q.Key())
	content := q.Capability() != ExistenceOnly && q.RequiredType() != DirectoryType && !unsupported
	for {
		s.mu.Lock()
		e := s.entries[key]
		if e != nil && e.hasExistence && (!content || e.hasContent || e.existence.Existence != File) {
			spec, err := e.existence, e.existenceErr
			if content && e.hasContent {
				spec = e.content
				err = joinServiceErrors(err, e.contentErr)
			} else if e.hasContent {
				err = joinServiceErrors(err, e.contentErr)
			}
			if unsupported && spec.Existence == File {
				spec.Anchors = Unavailable
				spec.Problem = Problem{Kind: NotSupplied, Message: "Anchor interpretation is unavailable for this resource type."}
			}
			stop := s.stop
			s.mu.Unlock()
			return spec, joinServiceErrors(err, stop, callerStop, s.checkpoint(ctx, 3*int64(len(q.Key().value))+1), s.ctx.Err())
		}
		if s.closed || s.stop != nil {
			err := s.stop
			if err == nil {
				err = context.Canceled
			}
			spec := serviceStoppedEntry(e, content, err)
			if e != nil {
				err = joinServiceErrors(err, e.existenceErr, e.contentErr)
			}
			s.mu.Unlock()
			return spec, err
		}
		if err := s.checkpoint(ctx, 1); err != nil {
			spec := serviceStoppedEntry(e, content, err)
			if e != nil {
				err = joinServiceErrors(err, e.existenceErr, e.contentErr)
			}
			s.mu.Unlock()
			return spec, err
		}
		if e == nil {
			if err := s.operation.Checkpoint(int64(len(q.Key().value)) + 1); err != nil {
				s.mu.Unlock()
				return filesystemStop(Unknown, err), err
			}
			if err := normalizationBound("path-depth", int64(strings.Count(q.Key().value, "/")+1), pathLimits.Depth); err != nil {
				s.mu.Unlock()
				return filesystemStop(Unknown, err), err
			}
			if err := s.admit(q.Key()); err != nil {
				s.mu.Unlock()
				return filesystemStop(Unknown, err), err
			}
			e = &cachedResource{}
			s.entries[key] = e
		}
		if e.flight == nil {
			if err := s.operation.budget.ChargeNodes(5); err != nil {
				s.mu.Unlock()
				return filesystemStop(Unknown, err), err
			}
			e.flight = make(chan struct{})
			// Capability/type planning is separate from the request's result binding.
			acquire := q
			acquire.spec.Required = AnyType
			acquire.spec.Capability = ExistenceOnly
			if content {
				acquire.spec.Capability = TextContent
			}
			s.enqueue(func(worker context.Context) { s.acquire(worker, e, acquire, content) })
		}
		done := e.flight
		s.mu.Unlock()
		if err := s.wait(ctx, done); err != nil {
			// Retain completed capabilities if completion raced the caller's stop;
			// an uncompleted content flight cannot erase earlier known existence.
			s.mu.Lock()
			spec := serviceStoppedEntry(e, content, err)
			cachedErr := joinServiceErrors(e.existenceErr, e.contentErr)
			s.mu.Unlock()
			return spec, joinServiceErrors(cachedErr, err)
		}
	}
}
func serviceStoppedEntry(e *cachedResource, content bool, err error) ObservationSpec {
	if e != nil && e.hasExistence {
		if content && e.hasContent {
			return e.content
		}
		if !content {
			return e.existence
		}
		return filesystemStop(e.existence.Existence, err)
	}
	return filesystemStop(Unknown, err)
}

func hasGitComponent(key Key) bool {
	for p := range strings.SplitSeq(key.value, "/") {
		if p == ".git" {
			return true
		}
	}
	return false
}
func (s *Service) acquire(ctx context.Context, e *cachedResource, q Request, content bool) {
	var spec ObservationSpec
	var receipt filesystemReceipt
	var err error
	_, selected := s.selected[q.Key()]
	unprovedUpgrade := content && s.filesystem != nil && !selected && e.hasExistence && e.existence.Existence == File && (e.receipt.operation != s.operation || e.receipt.key != q.Key() || e.receipt.existence != File || e.receipt.identity == "")
	if unprovedUpgrade {
		// A partial File fact is not proof of a stable first object. Zero prior
		// is valid only for an initial acquisition, never an unproved upgrade.
		spec = ObservationSpec{Existence: File, Content: Unavailable, Anchors: Unavailable, Problem: Problem{Kind: Blocked, Message: "Content upgrade is unavailable because the first native identity was not proved."}}
		receipt = e.receipt
		err = joinServiceErrors(e.existenceErr, s.checkpoint(ctx, 0))
	} else {
		spec, receipt, err = s.read(ctx, q, e.receipt)
	}
	// Some raw adapters express decode failure entirely in the passive Spec.
	// Retain that real encountered cause independently for later existence hits.
	if err == nil && spec.Problem.Kind == DecodeFailure {
		err = &FilesystemReadError{IO: &filesystemDecodeError{}}
	} else if err == nil && spec.Problem.Kind == ReadFailure {
		err = &FilesystemReadError{IO: errors.New(spec.Problem.Message)}
	}

	err = retainServiceError(err)
	var metadata *serviceReadError
	if errors.As(err, &metadata) {
		stopped := s.operation.Checkpoint(metadata.copyWork)
		if stopped == nil {
			stopped = s.operation.budget.ChargeNodes(metadata.copyNodes)
		}
		if stopped != nil {
			err = joinServiceErrors(err, stopped)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !e.hasExistence {
		e.existence = spec
		e.existenceErr = err
		e.hasExistence = true
		if spec.Existence == File {
			e.existence = ObservationSpec{Existence: File, Content: NotRequested, Anchors: NotRequested}
		}
	}
	if content {
		if e.existence.Existence == File && spec.Existence == Unknown {
			spec.Existence = File
		}
		e.content = spec
		e.contentErr = err
		e.hasContent = true
	}
	e.receipt = receipt
	if spec.Content == Available && s.filesystem != nil {
		_, selected := s.selected[q.Key()]
		if !selected {
			size := int64(spec.Source.Len())
			if size > s.limits.TotalContentBytes-s.bytes {
				limit := &parser.LimitExceeded{Name: "total-content-bytes", Observed: s.bytes + size, Allowed: s.limits.TotalContentBytes}
				e.content = filesystemStop(e.existence.Existence, limit)
				e.contentErr = joinServiceErrors(err, limit)
				s.stop = limit
			} else {
				s.bytes += size
			}
		}
	}
	if e.hasContent && e.content.Content == Available && e.content.Validated.operation == s.operation && e.content.Validated.source == e.content.Source && e.content.Validated.identity != nil {
		admission := serviceValidation{source: e.content.Source}
		if i, selected := s.selected[q.Key()]; selected {
			admission.targetID = q.view.targets[i].target.ID
			if int64(len(s.targetEvidence)) < s.limits.Identities {
				s.targetEvidence[anchorOwnerKey{source: e.content.Source.Token(), view: q.view.identity}] = e.content.Validated
			}
		}
		s.validations[e.content.Validated.identity] = admission
	}
	if e.flight != nil {
		close(e.flight)
		e.flight = nil
	}
}

type serviceIOCause struct {
	kind    ProblemKind
	message string
}

func joinServiceErrors(values ...error) error {
	var ioErr, stop error
	var controls serviceControls
	var causes []serviceIOCause
	var nodes, work int64
	snapshot := func(err error) error { n, w, v := snapshotServiceError(err); nodes += n; work += w; return v }
	addCauses := func(additional []serviceIOCause) {
		for _, cause := range additional {
			found := false
			for _, old := range causes {
				found = found || old.kind == cause.kind
			}
			if !found {
				causes = append(causes, cause)
			}
		}
	}
	for _, err := range values {
		if err == nil {
			continue
		}
		// Recognize our top-level carriers without traversing an untrusted/cyclic
		// native cause. Every other graph is detached and bounded before inspection.
		switch e := err.(type) { //nolint:errorlint // Inspect only exact top-level carriers; unwrapping first could traverse a cycle.
		case *serviceReadError:
			detached := snapshot(e.io)
			ioErr = errors.Join(ioErr, detached)
			if len(e.causes) != 0 {
				addCauses(e.causes)
			} else if detached != nil {
				addCauses([]serviceIOCause{serviceCanonicalCause(detached)})
			}
			controls.merge(e.controls)
			stop = errors.Join(stop, snapshot(e.stop))
		case *FilesystemReadError:
			detached := snapshot(e.IO)
			ioErr = errors.Join(ioErr, detached)
			if detached != nil {
				addCauses([]serviceIOCause{serviceCanonicalCause(detached)})
			}
			stop = errors.Join(stop, snapshot(e.Stop))
		default:
			detached := snapshot(err)
			var own *serviceReadError
			var native *FilesystemReadError
			switch {
			case errors.As(detached, &own):
				ioErr = errors.Join(ioErr, own.io)
				addCauses(own.causes)
				controls.merge(own.controls)
				stop = errors.Join(stop, own.stop)
			case errors.As(detached, &native):
				ioErr = errors.Join(ioErr, native.IO)
				if native.IO != nil {
					addCauses([]serviceIOCause{serviceCanonicalCause(native.IO)})
				}
				stop = errors.Join(stop, native.Stop)
			default:
				if _, controlled := InterruptionProblem(detached); controlled {
					stop = errors.Join(stop, detached)
				} else {
					ioErr = errors.Join(ioErr, detached)
					addCauses([]serviceIOCause{serviceCanonicalCause(detached)})
				}
			}
		}
	}
	if ioErr == nil && stop == nil {
		return nil
	}
	controls.merge(serviceControlFacts(stop))
	if controls.present() {
		stop = controls.err()
	}
	return &serviceReadError{io: ioErr, stop: stop, controls: controls, causes: causes, copyNodes: nodes, copyWork: work}
}

// Retained messages are immutable even if a caller uses errors.As to inspect a
// mutable native PathError. Unwrap preserves identity/provenance for errors.Is.
type serviceIO struct {
	message string
	cause   error
}

func (e serviceIO) Error() string { return e.message }
func (e serviceIO) Unwrap() error { return e.cause }
func retainServiceError(err error) error {
	normalized := joinServiceErrors(err)
	if normalized == nil {
		return nil
	}
	var own *serviceReadError
	if !errors.As(normalized, &own) {
		return normalized
	}
	if own.io != nil {
		own.io = serviceIO{message: own.io.Error(), cause: own.io}
	}
	own.stop = copyServiceStop(own.stop)
	return own
}

// Mandatory control facts are private values, independent of optional mutable
// inspection graphs. Rebuilding a stop exposes fresh limit metadata each time.
type serviceControls struct {
	cancelled, deadline bool
	limited             bool
	limit               parser.LimitExceeded
}

func serviceControlFacts(err error) serviceControls {
	out := serviceControls{cancelled: errors.Is(err, context.Canceled), deadline: errors.Is(err, context.DeadlineExceeded)}
	var limit *parser.LimitExceeded
	if errors.As(err, &limit) && limit != nil {
		out.limited, out.limit = true, *limit
	}
	return out
}
func (c *serviceControls) merge(other serviceControls) {
	c.cancelled = c.cancelled || other.cancelled
	c.deadline = c.deadline || other.deadline
	if !c.limited && other.limited {
		c.limited, c.limit = true, other.limit
	}
}
func (c serviceControls) present() bool { return c.cancelled || c.deadline || c.limited }
func (c serviceControls) err() error {
	var out error
	if c.limited {
		limit := c.limit
		out = &limit
	}
	if c.cancelled {
		out = errors.Join(out, context.Canceled)
	}
	if c.deadline {
		out = errors.Join(out, context.DeadlineExceeded)
	}
	return out
}
func copyServiceStop(err error) error { return serviceControlFacts(err).err() }

type serviceReadError struct {
	io, stop            error
	controls            serviceControls
	causes              []serviceIOCause
	copyNodes, copyWork int64
}

func (e *serviceReadError) Error() string { return errors.Join(e.io, e.stop).Error() }
func (e *serviceReadError) Unwrap() []error {
	var out []error
	if e.io != nil {
		out = append(out, e.io)
	}
	if e.stop != nil {
		out = append(out, e.stop)
	}
	return out
}

// RootDirectory retains the namespace's known root fact without a synthetic Key
// or lookup admission. Planning has no source Request: its independent stop is
// the caller's operation-level reporting obligation, even when Directory is known.
func (s *Service) RootDirectory(ctx context.Context, location Location) (Existence, error) {
	if s == nil || s.operation == nil || ctx == nil || !location.IsRootDirectory() || location.root != s.operation.root {
		return Unknown, ErrContract
	}
	return Directory, joinServiceErrors(s.checkpoint(ctx, 0), s.ctx.Err())
}
