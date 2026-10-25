package resource

import (
	"context"
	"crypto/sha256"
	"errors"
	"slices"

	"github.com/frithuz/mdchk/internal/parser"
)

// ParseCacheKey describes derived-cache identity, not permission to acquire a
// resource. Digest, complete parsing options/revision, anchor policy, exact view
// and Source ownership all participate. Equal bytes do not transfer AST spans
// or operation-budget provenance from another Source or request.
type ParseCacheKey struct {
	digest           [32]byte
	options          parser.ParseOptions
	revision, anchor string
	source           parser.Source
	view             *viewIdentity
}

func (k ParseCacheKey) Digest() [32]byte                  { return k.digest }
func (k ParseCacheKey) ParseOptions() parser.ParseOptions { return k.options }
func (k ParseCacheKey) ParserRevision() string            { return k.revision }
func (k ParseCacheKey) AnchorPolicy() string              { return k.anchor }

type cachedParse struct {
	done   chan struct{}
	ready  bool
	result parser.ParseResult
	err    error
}

// ParseKey requires completed validation evidence returned by this Service.Read.
// Generic same-operation validation alone does not admit new input/content.
// Computing a key meters the real digest/hash work; it creates no new budget.
func (s *Service) ParseKey(view View, evidence ContentEvidence) (ParseCacheKey, error) {
	if s == nil || s.operation == nil || view.operation != s.operation || view.identity == nil || evidence.operation != s.operation || evidence.source == (parser.Source{}) {
		return ParseCacheKey{}, ErrContract
	}
	s.mu.Lock()
	admission, admitted := s.validations[evidence.identity]
	s.mu.Unlock()
	if !admitted || admission.source.Token() != evidence.source.Token() {
		return ParseCacheKey{}, ErrContract
	}
	if admission.targetID != "" {
		if err := s.operation.Checkpoint(int64(len(admission.targetID)+1) * int64(len(view.targets)+1)); err != nil {
			return ParseCacheKey{}, err
		}
		target, available, exists := view.Target(admission.targetID)
		if !exists || !available || target.Source.Token() != evidence.source.Token() {
			return ParseCacheKey{}, ErrContract
		}
	}

	if err := s.operation.Checkpoint(3*int64(evidence.source.Len()) + int64(len(s.operation.settings.revision)) + 1); err != nil {
		return ParseCacheKey{}, err
	}
	return ParseCacheKey{digest: sha256.Sum256(evidence.source.Bytes()), options: s.operation.settings.parsing, revision: s.operation.settings.revision, anchor: s.operation.settings.anchor, source: evidence.source, view: view.identity}, nil
}

// Parse reuses an immutable parser result under the full source/policy key.
// The single operation worker runs ParseWithOptions using the exact existing
// settings and budget. Results are defensively copied; Document is immutable.
// Use Anchors for computed named-target authority; Parse alone grants none.
// Completed evidence remains valid if parsing later stops; callers keep it.
func (s *Service) Parse(ctx context.Context, view View, evidence ContentEvidence) (parser.ParseResult, error) {
	out, err := s.parseCached(ctx, view, evidence)
	if err != nil && !errors.Is(err, ErrContract) {
		err = joinServiceErrors(err)
	}
	return out, err
}

func (s *Service) parseCached(ctx context.Context, view View, evidence ContentEvidence) (parser.ParseResult, error) {
	if ctx == nil {
		return parser.ParseResult{}, ErrContract
	}
	key, err := s.ParseKey(view, evidence)
	if err != nil {
		return parser.ParseResult{}, err
	}
	return s.parseKeyCached(ctx, key)
}

func (s *Service) parseKeyCached(ctx context.Context, key ParseCacheKey) (parser.ParseResult, error) {
	for {
		s.mu.Lock()
		p := s.parsed[key]
		if p != nil && p.ready {
			out, readErr := copyCachedParse(p.result), p.err
			stop := s.stop
			s.mu.Unlock()
			return out, joinServiceErrors(readErr, stop, s.checkpoint(ctx, 0), s.ctx.Err())
		}
		if s.closed || s.stop != nil {
			stop := s.stop
			if stop == nil {
				stop = context.Canceled
			}
			s.mu.Unlock()
			return parser.ParseResult{}, stop
		}
		if err := s.checkpoint(ctx, 1); err != nil {
			s.mu.Unlock()
			return parser.ParseResult{}, err
		}
		if p == nil {
			if int64(len(s.parsed)) >= s.limits.Identities {
				err := &parser.LimitExceeded{Name: "resource-derived-entries", Observed: int64(len(s.parsed)) + 1, Allowed: s.limits.Identities}
				s.stop = err
				s.mu.Unlock()
				return parser.ParseResult{}, err
			}
			if err := s.operation.budget.ChargeNodes(3); err != nil {
				s.mu.Unlock()
				return parser.ParseResult{}, err
			}
			p = &cachedParse{done: make(chan struct{})}
			s.parsed[key] = p
			s.enqueue(func(worker context.Context) {
				out := parser.ParseWithOptions(worker, key.source, s.operation.budget, key.options)
				var stopped error
				if out.Limit != nil {
					v := *out.Limit
					stopped = &v
				} else if out.Interruption != "" {
					stopped = worker.Err()
					if stopped == nil {
						stopped = s.operation.Checkpoint(0)
					}
				}
				s.mu.Lock()
				p.result = copyCachedParse(out)
				p.err = stopped
				p.ready = true
				if p.done != nil {
					close(p.done)
					p.done = nil
				}
				s.mu.Unlock()
			})
		}
		done := p.done
		s.mu.Unlock()
		if err := s.wait(ctx, done); err != nil {
			return parser.ParseResult{}, err
		}
	}
}
func copyCachedParse(in parser.ParseResult) parser.ParseResult {
	in.Uncertainty = slices.Clone(in.Uncertainty)
	if in.Failure != nil {
		v := *in.Failure
		in.Failure = &v
	}
	if in.Limit != nil {
		v := *in.Limit
		in.Limit = &v
	}
	return in
}
