package resource

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// ExistenceOutcome describes one completed local-target existence query. Only
// the four absence/mismatch outcomes agree with Treatment() == MissingTarget.
type ExistenceOutcome string

const (
	ExistenceFound              ExistenceOutcome = "found"
	ExistenceMissing            ExistenceOutcome = "missing"
	ExistenceFileWithHint       ExistenceOutcome = "file-with-directory-hint"
	ExistenceIndexMissing       ExistenceOutcome = "index-missing"
	ExistenceIndexIsDirectory   ExistenceOutcome = "index-is-directory"
	ExistenceSkipped            ExistenceOutcome = "skipped"
	ExistenceContextUnavailable ExistenceOutcome = "context-unavailable"
)

// Absent reports whether the outcome is an authoritative absence or type
// mismatch that a consumer may report as a missing target.
func (o ExistenceOutcome) Absent() bool {
	return o == ExistenceMissing || o == ExistenceFileWithHint || o == ExistenceIndexMissing || o == ExistenceIndexIsDirectory
}

// ExistenceResolver composes the operation's existing normalizer and service for
// existence-only lookups. It owns neither a worker nor a cache, never requests
// content or anchors, and never registers targets or writes.
type ExistenceResolver struct {
	service    *Service
	normalizer Normalizer
}

func NewExistenceResolver(service *Service, normalizer *Normalizer) (*ExistenceResolver, error) {
	if service == nil || normalizer == nil || service.operation == nil || normalizer.view.operation != service.operation || normalizer.view.identity == nil {
		return nil, ErrContract
	}
	return &ExistenceResolver{service, *normalizer}, nil
}

// Existence returns an existence resolver over this resolver's service and
// view, so fragment and existence consumers share one operation's resources.
func (r *FragmentResolver) Existence() (*ExistenceResolver, error) {
	if r == nil {
		return nil, ErrContract
	}
	return NewExistenceResolver(r.service, &r.normalizer)
}

// ExistenceQuery reserves one resolution and its reporting provenance while the
// execution meter is live. Copies share the single-use guard. Zero is invalid.
type ExistenceQuery struct{ state *existenceQueryState }
type existenceQueryState struct {
	resolver   *ExistenceResolver
	normalized NormalizedLink
	index      IndexLookup
	hasIndex   bool
	indexErr   error
	location   result.Range
	snapshot   result.Snapshot
	requests   [2]Request
	consumed   atomic.Bool
}

// Prepare normalizes exactly once and plans the single configured index
// attempt. Planning grants no directory fact; a path-bound failure of the
// hypothetical index matters only after the locator is proven a directory.
func (r *ExistenceResolver) Prepare(link Link) (ExistenceQuery, error) {
	if r == nil || r.service == nil {
		return ExistenceQuery{}, ErrContract
	}
	n, err := r.normalizer.Normalize(link)
	if err != nil {
		return ExistenceQuery{}, err
	}
	o := r.service.operation
	if err := o.Checkpoint(int64(link.source.Len())*4 + int64(len(link.destination)) + 1); err != nil {
		return ExistenceQuery{}, err
	}
	whole, _ := link.source.Span(0, link.source.Len())
	text, _ := link.source.Text(whole)
	if err := o.budget.ChargeNodes(int64(2*(strings.Count(text, "\r")+strings.Count(text, "\n")+1)) + 16); err != nil {
		return ExistenceQuery{}, err
	}
	location, snapshot, err := projectLink(link, r.normalizer.view.phase)
	if err != nil {
		return ExistenceQuery{}, err
	}
	q := &existenceQueryState{resolver: r, normalized: n, location: location, snapshot: snapshot}
	if n.class == Local && n.location.root.identity != nil && n.same == (parser.Source{}) {
		q.index, err = n.DirectoryIndex()
		if err != nil {
			var limit *parser.LimitExceeded
			if !errors.As(err, &limit) || (limit.Name != "path-bytes" && limit.Name != "path-depth") {
				return ExistenceQuery{}, err
			}
			q.indexErr = joinServiceErrors(err)
		} else {
			q.hasIndex = true
		}
	}
	makeRequest := func(key Key, required RequiredType) Request {
		return Request{spec: RequestSpec{Key: key, Link: link, Class: Local, Capability: ExistenceOnly, Required: required}, view: r.normalizer.view, identity: &requestIdentity{}, location: location, snapshot: snapshot}
	}
	if key, ok := n.location.Key(); ok {
		q.requests[0] = makeRequest(key, n.RequiredType())
	}
	if key, ok := q.index.location.Key(); q.hasIndex && q.index.class == Local && ok {
		q.requests[1] = makeRequest(key, RegularFile)
	}
	if err := o.Checkpoint(0); err != nil {
		return ExistenceQuery{}, err
	}
	return ExistenceQuery{q}, nil
}

// ExistenceResolution retains the acquired existence facts independently of the
// query outcome. No consumer should infer completion from Err()==nil.
type ExistenceResolution struct{ state *existenceResolutionState }
type existenceResolutionState struct {
	query              *existenceQueryState
	outcome            ExistenceOutcome
	observations       [2]Observation
	count              int
	err                error
	failures           [2]fragmentFailure
	failureCount       int
	controls           error
	finalCauseIdentity string
}

func (e ExistenceResolution) Outcome() ExistenceOutcome {
	if e.state == nil {
		return ""
	}
	return e.state.outcome
}
func (e ExistenceResolution) Normalized() NormalizedLink {
	if e.state == nil {
		return NormalizedLink{}
	}
	return e.state.query.normalized
}
func (e ExistenceResolution) Observations() []Observation {
	if e.state == nil {
		return nil
	}
	return slices.Clone(e.state.observations[:e.state.count])
}
func (e ExistenceResolution) Err() error {
	if e.state == nil {
		return ErrContract
	}
	return joinServiceErrors(e.state.err)
}

// Resolve consumes a prepared query with at most two existence-only reads: the
// locator, then exactly one configured index when the locator is a directory.
// There is no fallback name, recursion or directory scan.
func (r *ExistenceResolver) Resolve(ctx context.Context, query ExistenceQuery) (ExistenceResolution, error) {
	if r == nil || ctx == nil || query.state == nil || query.state.resolver != r || !query.state.consumed.CompareAndSwap(false, true) {
		return ExistenceResolution{}, ErrContract
	}
	q := query.state
	n := q.normalized
	e := &existenceResolutionState{query: q, outcome: ExistenceContextUnavailable}
	out := ExistenceResolution{e}
	active := false
	finish := func() (ExistenceResolution, error) {
		if active {
			e.controls = joinServiceErrors(ctx.Err(), r.service.operation.Checkpoint(0), r.service.ctx.Err())
			e.err = joinServiceErrors(e.err, e.controls)
		}
		e.err = joinServiceErrors(e.err)
		return out, joinServiceErrors(e.err)
	}
	switch {
	case n.class == Ignored:
		e.outcome = ExistenceSkipped
		return finish()
	case n.class != Local:
		return finish()
	case n.same != (parser.Source{}):
		// The current document exists without a lookup.
		e.outcome = ExistenceFound
		return finish()
	}
	active = true
	key := n.location.key.value
	if key == "" {
		key = n.link.path
	}
	e.finalCauseIdentity = key
	retain := func(identity string, err error) {
		if err != nil {
			e.failures[e.failureCount] = fragmentFailure{identity, joinServiceErrors(err)}
			e.failureCount++
		}
		e.err = joinServiceErrors(e.err, err)
	}
	read := func(request Request) (Observation, error) {
		spec, err := r.service.Read(ctx, request)
		observation, bindErr := r.service.operation.Record(request, spec)
		if bindErr != nil {
			return Observation{}, bindErr
		}
		e.observations[e.count] = observation
		e.count++
		retain(request.Key().value, err)
		return observation, nil
	}
	var existence Existence
	if n.location.IsRootDirectory() {
		var err error
		existence, err = r.service.RootDirectory(ctx, n.location)
		retain(key, err)
	} else {
		observation, err := read(q.requests[0])
		if err != nil {
			return ExistenceResolution{}, err
		}
		existence = observation.existence
	}
	switch existence {
	case Missing:
		e.outcome = ExistenceMissing
		return finish()
	case File:
		e.outcome = ExistenceFound
		if n.directory {
			e.outcome = ExistenceFileWithHint
		}
		return finish()
	case Directory:
	default:
		return finish()
	}
	if !q.hasIndex || q.index.class != Local {
		retain(key, q.indexErr)
		return finish()
	}
	if q.index.same != (parser.Source{}) {
		e.outcome = ExistenceFound
		return finish()
	}
	key = q.index.location.key.value
	e.finalCauseIdentity = key
	observation, err := read(q.requests[1])
	if err != nil {
		return ExistenceResolution{}, err
	}
	switch observation.existence {
	case Missing:
		e.outcome = ExistenceIndexMissing
	case Directory:
		e.outcome = ExistenceIndexIsDirectory
	case File:
		e.outcome = ExistenceFound
	default:
		// An unknown index remains context-unavailable.
	}
	return finish()
}
