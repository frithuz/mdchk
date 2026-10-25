package resource

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// OwnsOperation is exact service/operation identity, not permission to acquire
// resources or manufacture content, syntax or inventory evidence.
func (s *Service) OwnsOperation(operation *Operation) bool {
	return s != nil && operation != nil && s.operation == operation && s.ctx != nil
}

// Checkpoint shares this service's operation meter and lifetime with synchronous
// derived consumers. Close stops new work and remains independently inspectable
// with caller/operation cancellation and the first exact exhausted quota. This
// does not enqueue work, acquire data, or grant any content/parse authority.
func (s *Service) Checkpoint(ctx context.Context, units int64) error {
	if s == nil || s.operation == nil || s.ctx == nil || ctx == nil || units < 0 {
		return ErrContract
	}
	s.mu.Lock()
	stop, closed := s.stop, s.closed
	s.mu.Unlock()
	if closed && stop == nil {
		stop = context.Canceled
	}
	err := joinServiceErrors(stop, ctx.Err(), s.ctx.Err(), s.operation.ctx.Err(), s.operation.budget.ChargeWork(0))
	if err != nil {
		return err
	}
	return joinServiceErrors(s.operation.Checkpoint(units), ctx.Err(), s.ctx.Err(), s.operation.ctx.Err(), s.operation.budget.ChargeWork(0))
}

// TargetParseResult is an opaque admitted parse for one exact selected snapshot
// and full operation policy. It conveys no anchor or cross-file authority.
type TargetParseResult struct{ state *targetParseState }
type targetParseState struct {
	service      *Service
	key          ParseCacheKey
	validated    ContentEvidence
	parsed       parser.ParseResult
	availability Availability
	problem      Problem
}

func (r TargetParseResult) Availability() Availability {
	if r.state == nil {
		return Unavailable
	}
	return r.state.availability
}
func (r TargetParseResult) Source() parser.Source {
	if r.state == nil {
		return parser.Source{}
	}
	return r.state.key.source
}
func (r TargetParseResult) Problem() Problem {
	if r.state == nil {
		return Problem{}
	}
	return copyProblem(r.state.problem)
}

// Parsed returns a detached outcome and immutable Document. The boolean states
// that parsing was attempted for these admitted bytes, not that it completed.
// Consumers must honor status and the Document's own private projection witness.
func (r TargetParseResult) Parsed() (parser.ParseResult, bool) {
	if r.state == nil || r.state.parsed.Source.Token() != r.state.key.source.Token() || r.state.parsed.Source.Token() == (parser.SourceToken{}) {
		return parser.ParseResult{}, false
	}
	return copyCachedParse(r.state.parsed), true
}

// targetParseIndex reserves only admitted selected identities while execution is
// permitted. The maximum admitted ID length bounds even invalid stopped lookups,
// like SavedViewReservation; no source or selected-target traversal is needed.
// Slots are reserved before parsing; only immutable completed outcomes populate
// them. Exact map lookup still reads bounded identifier bytes; it is not an
// O(1) string operation. Pending slots never authorize waiting or new work.
type targetParseIndex struct {
	maxIDBytes int
	ready      map[string]*targetParseState
}

func (s *Service) readyTargetParse(view View, targetID string) TargetParseResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.targetParseReady[view.identity]
	if index == nil || len(targetID) > index.maxIDBytes {
		return TargetParseResult{}
	}
	return TargetParseResult{state: index.ready[targetID]}
}

// TargetParse admits/reuses only an existing selected snapshot, including NoKey
// targets. It uses the service's same first-source admission and full-policy parse
// cache, without computing HTML IDs/anchors or opening a resource. Complete
// candidate/saved aggregate admission remains the coordinated producer's duty.
func (s *Service) TargetParse(ctx context.Context, view View, targetID string) (TargetParseResult, error) {
	if s == nil || s.operation == nil || ctx == nil || view.operation != s.operation || view.identity == nil {
		return TargetParseResult{}, ErrContract
	}
	if err := s.Checkpoint(ctx, int64(len(targetID)+1)*int64(len(view.targets)+1)); err != nil {
		// A stop denies new work, not inspection of previously admitted ready facts.
		// This fixed-view lookup has a live-reserved ID bound and never scans bytes
		// of a source, validates a target, or enters the parse worker.
		return s.readyTargetParse(view, targetID), err
	}
	target, available, selected := view.Target(targetID)
	if !selected {
		return TargetParseResult{}, ErrContract
	}
	out := &targetParseState{service: s, key: ParseCacheKey{source: target.Source, view: view.identity}, availability: Unavailable}
	result := TargetParseResult{out}
	if !available {
		out.problem = Problem{Kind: Blocked, Message: "Selected saved source is unavailable."}
		return result, s.Checkpoint(ctx, 0)
	}
	owner := anchorOwnerKey{source: target.Source.Token(), view: view.identity}
	s.mu.Lock()
	evidence, admitted := s.targetEvidence[owner]
	key, keyed := s.targetParseKeys[owner]
	s.mu.Unlock()
	if !admitted {
		if err := s.Checkpoint(ctx, 1); err != nil {
			out.problem, _ = InterruptionProblem(err)
			return result, err
		}
		var err error
		evidence, err = s.admitAnchorTarget(ctx, owner, targetID, target.Source)
		if err != nil {
			if problem, stopped := InterruptionProblem(err); stopped {
				out.problem = problem
				return result, joinServiceErrors(err, s.Checkpoint(ctx, 0))
			}
			out.availability = Failed
			out.problem = Problem{Kind: DecodeFailure, Message: "Selected source is not valid UTF-8."}
			return result, &FilesystemReadError{IO: &filesystemDecodeError{}}
		}
	}
	out.validated = evidence
	if !keyed {
		var err error
		key, err = s.ParseKey(view, evidence)
		if err != nil {
			out.problem, _ = InterruptionProblem(err)
			return result, joinServiceErrors(err, s.Checkpoint(ctx, 0))
		}
		s.mu.Lock()
		if existing, exists := s.targetParseKeys[owner]; exists {
			key = existing
		} else {
			if err := s.operation.budget.ChargeNodes(1); err != nil {
				s.mu.Unlock()
				return result, joinServiceErrors(err, s.Checkpoint(ctx, 0))
			}
			s.targetParseKeys[owner] = key
		}
		s.mu.Unlock()
	}
	// Reserve each exact selected ID, including two IDs sharing one Source.
	s.mu.Lock()
	index := s.targetParseReady[view.identity]
	reserved := false
	if index != nil {
		_, reserved = index.ready[targetID]
	}
	if !reserved {
		// Reserve the stopped-ready lookup before parsing. Its identities are
		// bounded by admitted selected IDs/views, and misses never add state.
		nodes := int64(1)
		if index == nil {
			nodes += 2
		}
		if err := s.operation.Checkpoint(2*int64(len(targetID)) + 1); err != nil {
			s.mu.Unlock()
			return result, joinServiceErrors(err, s.Checkpoint(ctx, 0))
		}
		if err := s.operation.budget.ChargeNodes(nodes); err != nil {
			s.mu.Unlock()
			return result, joinServiceErrors(err, s.Checkpoint(ctx, 0))
		}
		if index == nil {
			index = &targetParseIndex{ready: make(map[string]*targetParseState)}
			s.targetParseReady[view.identity] = index
		}
		index.maxIDBytes = max(index.maxIDBytes, len(targetID))
		index.ready[targetID] = nil
	}
	s.mu.Unlock()
	out.key = key
	parsed, err := s.parseKeyCached(ctx, key)
	out.parsed = parsed
	switch {
	case parsed.Failure != nil:
		out.availability = Failed
		out.problem = Problem{Kind: InternalFailure, Message: parsed.Failure.Message}
		if parsed.Failure.Code == parser.InvalidUTF8 {
			out.problem.Kind = DecodeFailure
		}
		if parsed.Failure.Code == parser.InvalidUTF8 {
			err = joinServiceErrors(err, &FilesystemReadError{IO: &filesystemDecodeError{}})
		} else {
			err = joinServiceErrors(err, anchorInternalError(parsed.Failure))
		}
	case parsed.Status == parser.Complete && parsed.Document != nil && parsed.Source.Token() == target.Source.Token():
		out.availability = Available
	case parsed.Limit != nil || parsed.Interruption != "":
		out.availability = Interrupted
		out.problem, _ = InterruptionProblem(err)
	default:
		out.availability = Uncertain
		out.problem = Problem{Kind: ParserUncertain, Message: "Required selected-document syntax is unavailable or uncertain."}
	}
	if out.availability == Available {
		s.mu.Lock()
		// The finite slot was reserved while live. Publishing already acquired
		// immutable facts consumes no new admission, even if parsing just stopped.
		if index := s.targetParseReady[view.identity]; index != nil {
			if _, reserved := index.ready[targetID]; reserved && index.ready[targetID] == nil {
				index.ready[targetID] = out
			}
		}
		s.mu.Unlock()
	}
	// Known complete parsed facts survive a later stop; the independent error
	// still prevents a new derived query from claiming it completed after Close.
	err = joinServiceErrors(err, s.Checkpoint(ctx, 0))
	if errors.Is(err, ErrContract) {
		return TargetParseResult{}, err
	}
	return result, err
}
