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

// FragmentOutcome describes completed analysis independently of decoding status. Missing resources never become missing named targets.
type FragmentOutcome string

const (
	NoNamedQuery               FragmentOutcome = "no-named-query"
	DocumentReference          FragmentOutcome = "document-reference"
	FragmentSkipped            FragmentOutcome = "skipped"
	FragmentInapplicable       FragmentOutcome = "target-inapplicable"
	FragmentContextUnavailable FragmentOutcome = "context-unavailable"
	FragmentMalformed          FragmentOutcome = "malformed"
	FragmentFound              FragmentOutcome = "found"
	FragmentAbsentTarget       FragmentOutcome = "named-absent"
)

// FragmentResolver composes the operation's existing normalizer and service.
// It owns neither a worker nor a cache. Close the Service before its native root.
type FragmentResolver struct {
	service    *Service
	normalizer Normalizer
}

func NewFragmentResolver(service *Service, normalizer *Normalizer) (*FragmentResolver, error) {
	if service == nil || normalizer == nil || service.operation == nil || normalizer.view.operation != service.operation || normalizer.view.identity == nil {
		return nil, ErrContract
	}
	return &FragmentResolver{service, *normalizer}, nil
}

// FragmentQuery reserves one resolution and its reporting provenance while the
// execution meter is live. Copies share the single-use guard. Zero is invalid.
// A Prepare failure admits no query: the producer must retain that stop as an
// operation/source effect, rather than fabricate a resource Request or coverage.
type FragmentQuery struct{ state *fragmentQueryState }
type fragmentQueryState struct {
	resolver   *FragmentResolver
	normalized NormalizedLink
	index      IndexLookup
	hasIndex   bool
	indexErr   error
	location   result.Range
	snapshot   result.Snapshot
	requests   [3]Request
	consumed   atomic.Bool
}

// Prepare normalizes exactly once. Pure index planning grants no directory fact.
// The bounded requests and Range/SHA are prepared before acquisition or lookup can
// exhaust the budget; reconciliation never rescans or hashes the source afterward.
func (r *FragmentResolver) Prepare(link Link) (FragmentQuery, error) {
	if r == nil || r.service == nil {
		return FragmentQuery{}, ErrContract
	}
	n, err := r.normalizer.Normalize(link)
	if err != nil {
		return FragmentQuery{}, err
	}
	o := r.service.operation
	if err := o.Checkpoint(int64(link.source.Len())*4 + int64(len(link.destination)) + 1); err != nil {
		return FragmentQuery{}, err
	}
	whole, _ := link.source.Span(0, link.source.Len())
	text, _ := link.source.Text(whole)
	if err := o.budget.ChargeNodes(int64(2*(strings.Count(text, "\r")+strings.Count(text, "\n")+1)) + 24); err != nil {
		return FragmentQuery{}, err
	}
	location, snapshot, err := projectLink(link, r.normalizer.view.phase)
	if err != nil {
		return FragmentQuery{}, err
	}
	q := &fragmentQueryState{resolver: r, normalized: n, location: location, snapshot: snapshot}
	if n.class == Local && n.location.root.identity != nil && n.same == (parser.Source{}) && n.fragment.status != FragmentAbsent && !(n.fragment.status == FragmentValid && n.fragment.text == "") {
		q.index, err = n.DirectoryIndex()
		if err != nil {
			var limit *parser.LimitExceeded
			if !errors.As(err, &limit) || (limit.Name != "path-bytes" && limit.Name != "path-depth") {
				return FragmentQuery{}, err
			}
			// A hypothetical child may exceed a path bound while this locator
			// is a valid ordinary file. It matters only after Directory proof.
			q.indexErr = joinServiceErrors(err)
		} else {
			q.hasIndex = true
		}
	}
	makeRequest := func(key Key, capability Capability, required RequiredType) Request {
		return Request{spec: RequestSpec{Key: key, Link: link, Class: Local, Capability: capability, Required: required}, view: r.normalizer.view, identity: &requestIdentity{}, location: location, snapshot: snapshot}
	}
	if key, ok := n.location.Key(); ok {
		q.requests[0] = makeRequest(key, ExistenceOnly, n.RequiredType())
		q.requests[1] = makeRequest(key, ParsedAnchors, n.RequiredType())
	}
	if key, ok := q.index.location.Key(); q.hasIndex && q.index.class == Local && ok {
		q.requests[2] = makeRequest(key, ParsedAnchors, RegularFile)
	}
	if err := o.Checkpoint(0); err != nil {
		return FragmentQuery{}, err
	}
	return FragmentQuery{q}, nil
}

// FragmentResolution retains acquired resource/index facts independently of the
// query outcome. Only found/absent follow a successfully completed exact Lookup.
// No consumer should infer completion from Err()==nil or index availability.
type FragmentResolution struct{ state *fragmentResolutionState }
type fragmentResolutionState struct {
	query              *fragmentQueryState
	outcome            FragmentOutcome
	observations       [2]Observation
	count              int
	anchors            AnchorResult
	matches            []AnchorOccurrence
	err                error
	failures           [3]fragmentFailure
	failureCount       int
	controls           error
	finalCauseIdentity string
}
type fragmentFailure struct {
	key string
	err error
}

func (f FragmentResolution) Outcome() FragmentOutcome {
	if f.state == nil {
		return ""
	}
	return f.state.outcome
}
func (f FragmentResolution) Normalized() NormalizedLink {
	if f.state == nil {
		return NormalizedLink{}
	}
	return f.state.query.normalized
}
func (f FragmentResolution) Observations() []Observation {
	if f.state == nil {
		return nil
	}
	return slices.Clone(f.state.observations[:f.state.count])
}
func (f FragmentResolution) Anchors() AnchorResult {
	if f.state == nil {
		return AnchorResult{}
	}
	return f.state.anchors
}
func (f FragmentResolution) Err() error {
	if f.state == nil {
		return ErrContract
	}
	return joinServiceErrors(f.state.err)
}

// Matches returns caller-owned occurrences, charging the original meter before
// copying. A later failed inspection cannot change the completed query outcome.
func (f FragmentResolution) Matches(ctx context.Context) ([]AnchorOccurrence, error) {
	if f.state == nil || ctx == nil {
		return nil, ErrContract
	}
	if err := f.state.query.resolver.service.checkpoint(ctx, int64(len(f.state.matches))+1); err != nil {
		return nil, err
	}
	work, nodes := int64(0), int64(len(f.state.matches))
	for _, m := range f.state.matches {
		work += int64(len(m.Value.Segments))
		nodes += int64(len(m.Value.Segments))
	}
	if err := f.state.query.resolver.service.checkpoint(ctx, work); err != nil {
		return nil, err
	}
	if err := f.state.query.resolver.service.operation.budget.ChargeNodes(nodes); err != nil {
		return nil, err
	}
	out := slices.Clone(f.state.matches)
	for i := range out {
		out[i] = copyAnchorOccurrence(out[i])
	}
	return out, nil
}

// Resolve consumes a prepared query. CrossFile is a producer choice; it grants
// no target/write authority and does not change MD057 ownership when disabled.
// Orchestration runs outside the service worker to avoid recursive worker waits.
func (r *FragmentResolver) Resolve(ctx context.Context, query FragmentQuery, crossFile bool) (FragmentResolution, error) {
	if r == nil || ctx == nil || query.state == nil || query.state.resolver != r || !query.state.consumed.CompareAndSwap(false, true) {
		return FragmentResolution{}, ErrContract
	}
	q := query.state
	n := q.normalized
	f := &fragmentResolutionState{query: q, outcome: FragmentContextUnavailable}
	out := FragmentResolution{f}
	active := false
	finish := func() (FragmentResolution, error) {
		if active {
			f.controls = joinServiceErrors(ctx.Err(), r.service.operation.Checkpoint(0), r.service.ctx.Err())
			f.err = joinServiceErrors(f.err, f.controls)
		}
		f.err = joinServiceErrors(f.err)
		return out, joinServiceErrors(f.err)
	}
	switch {
	case n.class == Ignored:
		f.outcome = FragmentSkipped
		return finish()
	case n.fragment.status == FragmentAbsent:
		f.outcome = NoNamedQuery
		return finish()
	case n.class != Local:
		return finish()
	case n.fragment.status == FragmentValid && n.fragment.text == "":
		f.outcome = DocumentReference
		return finish()
	}
	same := n.same != (parser.Source{})
	potentialSelf := q.hasIndex && q.index.same != (parser.Source{})
	// A directory spelling can name the current selected index. Only that pure
	// identity possibility permits acquisition with cross-file analysis disabled.
	target, _, _ := r.normalizer.view.Target(n.link.path)
	selectedLocator := target.ResourceKey.value != "" && target.ResourceKey == n.location.key
	if !crossFile && !same && !potentialSelf && !selectedLocator {
		f.outcome = FragmentSkipped
		return finish()
	}
	active = true
	key := n.location.key.value
	if key == "" {
		key = n.link.path
	}
	f.finalCauseIdentity = key
	retain := func(identity string, err error) {
		if err != nil {
			f.failures[f.failureCount] = fragmentFailure{identity, joinServiceErrors(err)}
			f.failureCount++
		}
		f.err = joinServiceErrors(f.err, err)
	}
	read := func(request Request) (Observation, error) {
		spec, err := r.service.Read(ctx, request)
		observation, bindErr := r.service.operation.Record(request, spec)
		if bindErr != nil {
			return Observation{}, bindErr
		}
		f.observations[f.count] = observation
		f.count++
		retain(request.Key().value, err)
		if request.Capability() == ParsedAnchors && spec.Content == Available {
			state := &anchorResultState{operation: r.service.operation, key: ParseCacheKey{source: spec.Source, view: r.normalizer.view.identity}, availability: spec.Anchors, index: spec.Index, validated: spec.Validated, problem: spec.Problem}
			if spec.Parsed != nil {
				state.parsed = *spec.Parsed
			}
			if spec.Index.state != nil {
				state.key = spec.Index.Key()
			}
			f.anchors = AnchorResult{state}
		}
		return observation, nil
	}
	var final Observation
	if !same {
		var existence Existence
		if n.location.IsRootDirectory() {
			var err error
			existence, err = r.service.RootDirectory(ctx, n.location)
			retain(key, err)
		} else {
			observation, err := read(q.requests[0])
			if err != nil {
				return FragmentResolution{}, err
			}
			final = observation
			existence = observation.existence
		}
		if existence == Missing || n.directory && existence == File {
			f.outcome = FragmentInapplicable
			return finish()
		}
		if existence == Unknown {
			return finish()
		}
		if existence == Directory {
			if !q.hasIndex || q.index.class != Local {
				retain(key, q.indexErr)
				return finish()
			}
			same = potentialSelf
			key = q.index.location.key.value
			f.finalCauseIdentity = key
			if !same {
				observation, err := read(q.requests[2])
				if err != nil {
					return FragmentResolution{}, err
				}
				final = observation
			}
		} else {
			observation, err := read(q.requests[1])
			if err != nil {
				return FragmentResolution{}, err
			}
			final = observation
		}
		if !same {
			if final.existence == Missing || final.existence == Directory {
				f.outcome = FragmentInapplicable
				return finish()
			}
			if final.anchors != Available {
				return finish()
			}
		}
	}
	if same {
		var err error
		f.anchors, err = r.service.TargetAnchors(ctx, r.normalizer.view, n.link.path)
		retain(key, err)
	}
	f.err = joinServiceErrors(f.err, ctx.Err(), r.service.operation.Checkpoint(0), r.service.ctx.Err())
	index, available := f.anchors.Index()
	if !available {
		return finish()
	}
	// Acquired complete indexes remain truthful even when their ready-cache hit
	// returns an independent interruption. Such a query has not completed.
	if f.err != nil {
		return finish()
	}
	if n.fragment.status == FragmentInvalid {
		f.outcome = FragmentMalformed
		return finish()
	}
	matches, err := index.Lookup(ctx, n.fragment.text)
	retain(key, joinServiceErrors(err, r.service.ctx.Err()))
	if f.err != nil {
		return finish()
	}
	f.matches = matches
	if len(matches) == 0 {
		f.outcome = FragmentAbsentTarget
	} else {
		f.outcome = FragmentFound
	}
	return finish()
}
