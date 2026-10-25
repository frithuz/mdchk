package resource

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
)

type viewIdentity struct{ _ byte }
type Phase string

const (
	Original  Phase = "original"
	Candidate Phase = "candidate"
	Saved     Phase = "saved"
)

// Target is explicit selected-target authority supplied by the caller. A resource
// observation has no conversion/registration method. ResourceKey may be absent
// for an explicit target outside the resource namespace; same-source work remains
// possible without granting cross-file access or enlarging Root.
type Target struct {
	ID          string
	Source      parser.Source
	ResourceKey Key
}
type Replacement struct {
	TargetID    string
	Source      parser.Source
	Unavailable bool
}
type targetState struct {
	target      Target
	unavailable bool
}
type Operation struct {
	ctx       context.Context
	budget    *parser.OperationBudget
	root      Root
	settings  Settings
	targets   []Target
	serviceMu sync.Mutex
	service   *Service
}
type View struct {
	operation *Operation
	identity  *viewIdentity
	phase     Phase
	targets   []targetState
}

func NewOperation(ctx context.Context, budget *parser.OperationBudget, root Root, settings Settings, targets []Target) (*Operation, error) {
	if ctx == nil || budget == nil || root.identity == nil || !settings.valid {
		return nil, ErrContract
	}
	if err := budget.Limits().Validate(); err != nil {
		return nil, ErrContract
	}
	if err := budget.ChargeWork(int64(len(targets)) + 1); err != nil {
		return nil, err
	}
	if err := budget.ChargeNodes(int64(2*len(targets)) + 1); err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	keys := map[Key]bool{}
	for _, target := range targets {
		if target.ID == "" || !utf8.ValidString(target.ID) || target.Source == (parser.Source{}) || ids[target.ID] {
			return nil, ErrContract
		}
		ids[target.ID] = true
		if target.ResourceKey.value != "" {
			if keys[target.ResourceKey] {
				return nil, ErrContract
			}
			keys[target.ResourceKey] = true
		}
	}
	return &Operation{ctx: ctx, budget: budget, root: root, settings: settings, targets: slices.Clone(targets)}, nil
}
func (o *Operation) Context() context.Context        { return o.ctx }
func (o *Operation) Budget() *parser.OperationBudget { return o.budget }
func (o *Operation) Root() Root                      { return o.root }
func (o *Operation) Settings() Settings              { return o.settings }
func (o *Operation) Targets() []Target               { return slices.Clone(o.targets) }
func (o *Operation) Checkpoint(units int64) error {
	if o == nil {
		return ErrContract
	}
	if err := o.ctx.Err(); err != nil {
		return err
	}
	if err := o.budget.ChargeWork(0); err != nil {
		return err
	}
	return o.budget.ChargeWork(units)
}

// NewView describes immutable target replacements, not execution of a coordinated
// fix overlay or persistence. Saved replacements must be confirmed by the caller;
// a stale/failed write uses Unavailable rather than presumed original/candidate.
// Every new view has distinct identity, even if its bytes are identical.
func (o *Operation) NewView(phase Phase, replacements []Replacement) (View, error) {
	if o == nil || (phase != Original && phase != Candidate && phase != Saved) || (phase == Original && len(replacements) > 0) {
		return View{}, ErrContract
	}
	if phase == Saved {
		reservation, err := o.ReserveSavedView()
		if err != nil {
			return View{}, err
		}
		return reservation.Bind(replacements)
	}
	if err := o.Checkpoint(int64(len(o.targets)+len(replacements)) + 1); err != nil {
		return View{}, err
	}
	if err := o.budget.ChargeNodes(int64(2*len(o.targets)+len(replacements)) + 1); err != nil {
		return View{}, err
	}
	states := make([]targetState, len(o.targets))
	index := map[string]int{}
	for i, target := range o.targets {
		states[i] = targetState{target: target}
		index[target.ID] = i
	}
	seen := map[string]bool{}
	for _, replacement := range replacements {
		i, ok := index[replacement.TargetID]
		if !ok || seen[replacement.TargetID] || replacement.Unavailable && phase != Saved || !replacement.Unavailable && replacement.Source == (parser.Source{}) || replacement.Unavailable && replacement.Source != (parser.Source{}) {
			return View{}, ErrContract
		}
		seen[replacement.TargetID] = true
		states[i].unavailable = replacement.Unavailable
		states[i].target.Source = replacement.Source
	}
	return View{o, &viewIdentity{}, phase, states}, nil
}

// SavedViewReservation reserves one bounded final fact-binding attempt while
// execution is permitted. It authorizes neither persistence nor further analysis.
// Its mutable bookkeeping is private; Bind is safe against concurrent attempts.
type SavedViewReservation struct{ state *savedViewReservationState }

type savedViewReservationState struct {
	operation  *Operation
	states     []targetState
	index      map[string]int
	seen       []bool
	maxIDBytes int
	bound      atomic.Bool
}

// ReserveSavedView preallocates and charges reconciliation capacity for exactly
// the selected target set. Call before starting work that may finish after stop.
func (o *Operation) ReserveSavedView() (*SavedViewReservation, error) {
	if o == nil {
		return nil, ErrContract
	}
	idBytes, maxIDBytes := int64(0), 0
	for _, target := range o.targets {
		idBytes += int64(len(target.ID))
		maxIDBytes = max(maxIDBytes, len(target.ID))
	}
	// Reserve worst-case ID inspection for every post-stop fact, including
	// bounded invalid IDs. All storage is allocated before execution stops.
	if err := o.Checkpoint(idBytes + int64(maxIDBytes)*int64(len(o.targets)) + int64(3*len(o.targets)) + 1); err != nil {
		return nil, err
	}
	if err := o.budget.ChargeNodes(int64(3*len(o.targets)) + 1); err != nil {
		return nil, err
	}
	r := &savedViewReservationState{operation: o, states: make([]targetState, len(o.targets)), index: make(map[string]int, len(o.targets)), seen: make([]bool, len(o.targets))}
	for i, target := range o.targets {
		r.states[i] = targetState{target: target}
		r.index[target.ID] = i
		r.maxIDBytes = max(r.maxIDBytes, len(target.ID))
	}
	if err := o.Checkpoint(0); err != nil {
		return nil, err
	}
	return &SavedViewReservation{state: r}, nil
}

// Bind admits caller-confirmed saved bytes or explicit unavailable facts, even
// after cancellation/quota exhaustion. No byte scanning, execution checkpoint,
// budget reset, writes or query permission is performed. Omitted targets mean
// caller-confirmed unchanged original bytes. Every attempt consumes the reserve,
// including invalid input; at most the preselected number of IDs is examined.
func (r *SavedViewReservation) Bind(replacements []Replacement) (View, error) {
	if r == nil || r.state == nil || !r.state.bound.CompareAndSwap(false, true) {
		return View{}, ErrContract
	}
	state := r.state
	if len(replacements) > len(state.states) {
		return View{}, ErrContract
	}
	for _, replacement := range replacements {
		if len(replacement.TargetID) > state.maxIDBytes {
			return View{}, ErrContract
		}
		i, ok := state.index[replacement.TargetID]
		if !ok || state.seen[i] || replacement.Unavailable && replacement.Source != (parser.Source{}) || !replacement.Unavailable && replacement.Source == (parser.Source{}) {
			return View{}, ErrContract
		}
		state.seen[i] = true
		state.states[i].target.Source = replacement.Source
		state.states[i].unavailable = replacement.Unavailable
	}
	return View{operation: state.operation, identity: &viewIdentity{}, phase: Saved, targets: state.states}, nil
}

func (v View) Phase() Phase { return v.phase }
func (v View) Root() Root {
	if v.operation == nil {
		return Root{}
	}
	return v.operation.root
}
func (v View) Target(id string) (Target, bool, bool) {
	for _, s := range v.targets {
		if s.target.ID == id {
			return s.target, !s.unavailable, true
		}
	}
	return Target{}, false, false
}
func (v View) Same(other View) bool {
	return v.identity != nil && v.identity == other.identity && v.operation == other.operation
}
func (v View) Request(spec RequestSpec) (Request, error) {
	if v.operation == nil || v.identity == nil || spec.Link.path == "" || !spec.Link.span.Present() {
		return Request{}, ErrContract
	}
	target, available, selected := v.Target(spec.Link.path)
	if !selected || !available || target.Source != spec.Link.source {
		return Request{}, ErrContract
	}
	if spec.Capability != ExistenceOnly && spec.Capability != TextContent && spec.Capability != ParsedAnchors {
		return Request{}, ErrContract
	}
	if spec.Required != AnyType && spec.Required != RegularFile && spec.Required != DirectoryType {
		return Request{}, ErrContract
	}
	switch spec.Class {
	case Local:
		if spec.Key.value == "" {
			return Request{}, ErrContract
		}
	case Ignored, BlockedReference, InvalidReference:
		if spec.Key.value != "" {
			return Request{}, ErrContract
		}
	default:
		return Request{}, ErrContract
	}
	if err := v.operation.Checkpoint(int64(spec.Link.source.Len())*4 + int64(len(spec.Link.destination)) + 1); err != nil {
		return Request{}, err
	}
	whole, _ := spec.Link.source.Span(0, spec.Link.source.Len())
	text, _ := spec.Link.source.Text(whole)
	lineRecords := strings.Count(text, "\r") + strings.Count(text, "\n") + 1
	if err := v.operation.budget.ChargeNodes(int64(2*lineRecords) + 4); err != nil {
		return Request{}, err
	} // conservative line-index capacity, request, observation, coverage and cause
	location, snapshot, err := projectLink(spec.Link, v.phase)
	if err != nil {
		return Request{}, err
	}
	if err := v.operation.Checkpoint(0); err != nil {
		return Request{}, err
	}
	return Request{spec: spec, view: v, identity: &requestIdentity{}, location: location, snapshot: snapshot}, nil
}
