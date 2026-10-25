package resource

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// Identity lookup uses immutable Source/view handles, not path or digest alone.
// Its value retains the full ParseCacheKey produced while execution was live.
// This lets an already completed index survive a later stop without rehashing
// content merely to retain known facts. Every entry also owns a parsed-cache slot.
type anchorOwnerKey struct {
	source parser.SourceToken
	view   *viewIdentity
}

type cachedAnchors struct {
	key    ParseCacheKey
	done   chan struct{}
	ready  bool
	result AnchorResult
	err    error
}

// Anchors obtains the operation's immutable heading/HTML target index under its
// full parsed-source policy key. Evidence must come from this service's committed
// available Read. Parsing finishes before the index job is queued on the same
// owned worker, avoiding recursive worker waits. Individual caller cancellation
// stops its wait without poisoning another requester's shared production.
func (s *Service) Anchors(ctx context.Context, view View, evidence ContentEvidence) (AnchorResult, error) {
	if s == nil || s.operation == nil || ctx == nil || view.operation != s.operation || view.identity == nil || evidence.operation != s.operation || evidence.source == (parser.Source{}) {
		return AnchorResult{}, ErrContract
	}
	s.mu.Lock()
	admission, admitted := s.validations[evidence.identity]
	owner := anchorOwnerKey{source: evidence.source.Token(), view: view.identity}
	known := s.anchors[owner]
	authorityWork := int64(1)
	if !admitted || admission.source.Token() != evidence.source.Token() {
		s.mu.Unlock()
		return AnchorResult{}, ErrContract
	}
	if admission.targetID != "" {
		authorityWork += int64(len(admission.targetID)+1) * int64(len(view.targets)+1)
		target, available, exists := view.Target(admission.targetID)
		if !exists || !available || target.Source.Token() != evidence.source.Token() {
			s.mu.Unlock()
			return AnchorResult{}, ErrContract
		}
	}
	if known != nil && known.ready {
		out, err, stop := known.result, known.err, s.stop
		s.mu.Unlock()
		return out, joinServiceErrors(err, stop, s.checkpoint(ctx, authorityWork), s.ctx.Err())
	}
	s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return s.stoppedAnchors(ParseCacheKey{source: evidence.source, view: view.identity}, evidence, err), err
	}
	key, err := s.ParseKey(view, evidence)
	if err != nil {
		return s.stoppedAnchors(ParseCacheKey{source: evidence.source, view: view.identity}, evidence, err), err
	}
	parsed, parseErr := s.parseKeyCached(ctx, key)
	if parsed.Source != key.source {
		if parseErr == nil {
			return AnchorResult{}, ErrContract
		}
		return s.stoppedAnchors(key, evidence, parseErr), parseErr
	}
	for {
		s.mu.Lock()
		entry := s.anchors[owner]
		if entry != nil && entry.ready {
			out, err, stop := entry.result, entry.err, s.stop
			s.mu.Unlock()
			return out, joinServiceErrors(err, parseErr, stop, s.checkpoint(ctx, 1), s.ctx.Err())
		}
		if s.closed || s.stop != nil {
			stop := s.stop
			if stop == nil {
				stop = context.Canceled
			}
			s.mu.Unlock()
			err := joinServiceErrors(parseErr, stop)
			return s.stoppedAnchors(key, evidence, err), err
		}
		if err := s.checkpoint(ctx, 1); err != nil {
			s.mu.Unlock()
			return s.stoppedAnchors(key, evidence, err), joinServiceErrors(parseErr, err)
		}
		if entry == nil {
			// There is at most one anchor entry per full parsed-source slot. Both
			// source/view identity and finite parsed admission were already proved.
			if s.parsed[key] == nil || int64(len(s.anchors)) >= s.limits.Identities {
				s.mu.Unlock()
				return AnchorResult{}, ErrContract
			}
			if err := s.operation.budget.ChargeNodes(4); err != nil {
				s.mu.Unlock()
				return s.stoppedAnchors(key, evidence, err), err
			}
			entry = &cachedAnchors{key: key, done: make(chan struct{})}
			s.anchors[owner] = entry
			s.enqueue(func(worker context.Context) {
				out, err := s.buildAnchors(worker, s.operation, key, parsed)
				if out.state != nil {
					out.state.validated = evidence
				}
				err = retainServiceError(err)
				var metadata *serviceReadError
				if errors.As(err, &metadata) {
					stopped := s.operation.Checkpoint(metadata.copyWork)
					if stopped == nil {
						stopped = s.operation.budget.ChargeNodes(metadata.copyNodes)
					}
					err = joinServiceErrors(err, stopped)
				}
				s.mu.Lock()
				entry.result, entry.err, entry.ready = out, err, true
				if entry.done != nil {
					close(entry.done)
					entry.done = nil
				}
				s.mu.Unlock()
			})
		}
		done := entry.done
		s.mu.Unlock()
		if err := s.wait(ctx, done); err != nil {
			s.mu.Lock()
			out, completedErr := entry.result, entry.err
			ready := entry.ready
			s.mu.Unlock()
			if ready {
				return out, joinServiceErrors(parseErr, completedErr, err)
			}
			return s.stoppedAnchors(key, evidence, err), joinServiceErrors(parseErr, err)
		}
	}
}

func (s *Service) stoppedAnchors(key ParseCacheKey, evidence ContentEvidence, err error) AnchorResult {
	out := stoppedAnchorResult(s.operation, key, err)
	if out.state != nil {
		out.state.validated = evidence
	}
	return out
}

// TargetAnchors admits only an already selected exact view snapshot. It supports
// same-document analysis for explicit targets outside the resource namespace,
// including NoKey targets, without minting a path or granting cross-file access.
// Original target bytes were already admitted by service construction; complete
// candidate/saved overlay admission remains the coordinated producer's duty.
func (s *Service) TargetAnchors(ctx context.Context, view View, targetID string) (AnchorResult, error) {
	if s == nil || s.operation == nil || ctx == nil || view.operation != s.operation || view.identity == nil {
		return AnchorResult{}, ErrContract
	}
	target, available, selected := view.Target(targetID)
	if !selected {
		return AnchorResult{}, ErrContract
	}
	key := ParseCacheKey{source: target.Source, view: view.identity}
	if !available {
		out := AnchorResult{&anchorResultState{operation: s.operation, key: key, availability: Unavailable, problem: Problem{Kind: Blocked, Message: "Selected saved source is unavailable."}}}
		return out, joinServiceErrors(s.checkpoint(ctx, int64(len(view.targets))+int64(len(targetID))+1), s.ctx.Err())
	}
	owner := anchorOwnerKey{source: target.Source.Token(), view: view.identity}
	s.mu.Lock()
	evidence, admitted := s.targetEvidence[owner]
	s.mu.Unlock()
	if admitted {
		return s.Anchors(ctx, view, evidence)
	}
	if err := s.checkpoint(ctx, int64(len(view.targets))+int64(len(targetID))+1); err != nil {
		return stoppedAnchorResult(s.operation, key, err), err
	}
	evidence, err := s.admitAnchorTarget(ctx, owner, targetID, target.Source)
	if err != nil {
		if _, stopped := InterruptionProblem(err); stopped {
			return stoppedAnchorResult(s.operation, key, err), err
		}
		out := AnchorResult{&anchorResultState{operation: s.operation, key: key, availability: Failed, problem: Problem{Kind: DecodeFailure, Message: "Selected source is not valid UTF-8."}}}
		return out, &FilesystemReadError{IO: &filesystemDecodeError{}}
	}
	return s.Anchors(ctx, view, evidence)
}

func (s *Service) admitAnchorTarget(ctx context.Context, owner anchorOwnerKey, targetID string, source parser.Source) (ContentEvidence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if evidence, admitted := s.targetEvidence[owner]; admitted {
		return evidence, nil
	}
	if s.closed || s.stop != nil {
		stop := s.stop
		if stop == nil {
			stop = context.Canceled
		}
		return ContentEvidence{}, joinServiceErrors(stop, ctx.Err(), s.ctx.Err())
	}
	if int64(len(s.targetEvidence)) >= s.limits.Identities {
		err := &parser.LimitExceeded{Name: "resource-derived-entries", Observed: int64(len(s.targetEvidence)) + 1, Allowed: s.limits.Identities}
		s.stop = err
		return ContentEvidence{}, err
	}
	if err := normalizationBound("document-bytes", int64(source.Len()), s.operation.budget.Limits().DocumentBytes); err != nil {
		return ContentEvidence{}, err
	}
	if err := s.operation.budget.ChargeNodes(3); err != nil {
		return ContentEvidence{}, err
	}
	evidence, err := s.operation.ValidateContent(source)
	if err != nil {
		return ContentEvidence{}, err
	}
	s.validations[evidence.identity] = serviceValidation{source: source, targetID: targetID}
	s.targetEvidence[owner] = evidence
	return evidence, nil
}

func stoppedAnchorResult(operation *Operation, key ParseCacheKey, err error) AnchorResult {
	problem, stopped := InterruptionProblem(err)
	if !stopped {
		return AnchorResult{}
	}
	return AnchorResult{&anchorResultState{operation: operation, key: key, availability: Interrupted, problem: problem}}
}

func anchorReadError(err error) error {
	if err == nil || errors.Is(err, ErrContract) {
		return err
	}
	return joinServiceErrors(err)
}
