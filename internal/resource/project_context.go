package resource

import (
	"context"
	"errors"
	"strings"

	"github.com/frithuz/mdchk/internal/parser"
)

// ProjectSource is an acquired, validated source in one service/view. It grants
// source-bound read queries without manufacturing selected Markdown provenance.
// Its zero value is invalid; equal bytes cannot reproduce its private witness.
type ProjectSource struct{ state *projectSourceState }
type projectSourceState struct {
	service *Service
	view    View
	key     Key
	source  parser.Source
}

func (p ProjectSource) Source() parser.Source {
	if p.state == nil {
		return parser.Source{}
	}
	return p.state.source
}
func (p ProjectSource) Key() Key {
	if p.state == nil {
		return Key{}
	}
	return p.state.key
}
func (p ProjectSource) View() View {
	if p.state == nil {
		return View{}
	}
	return p.state.view
}

// AcquireSource reads one literal resource key through normal first-fact/content
// admission. It does not select that file, decode its name, or discover config.
func (s *Service) AcquireSource(ctx context.Context, view View, key Key) (ProjectSource, ObservationSpec, error) {
	if s == nil || !s.operation.OwnsView(view) || key.value == "" {
		return ProjectSource{}, ObservationSpec{}, ErrContract
	}
	if err := s.Checkpoint(ctx, int64(len(key.value))+1); err != nil {
		return ProjectSource{}, ObservationSpec{}, err
	}
	if err := s.operation.budget.ChargeNodes(2); err != nil {
		return ProjectSource{}, ObservationSpec{}, err
	}
	q := Request{project: true, view: view, identity: &requestIdentity{}, spec: RequestSpec{Key: key, Class: Local, Capability: TextContent, Required: RegularFile}}
	spec, err := s.Read(ctx, q)
	if spec.Content != Available || spec.Source.Token() == (parser.SourceToken{}) {
		return ProjectSource{}, spec, err
	}
	if stopped := s.Checkpoint(ctx, 1); stopped != nil {
		return ProjectSource{}, spec, errors.Join(err, stopped)
	}
	return ProjectSource{&projectSourceState{s, view, key, spec.Source}}, spec, err
}

// DirectoryLocation describes a literal directory below the existing root. The
// empty spelling represents the real namespace root, never a Key("."). Planning
// conveys no existence evidence; Inspect must prove its actual type.
func (p ProjectSource) DirectoryLocation(ctx context.Context, name string) (Location, error) {
	if p.state == nil {
		return Location{}, ErrContract
	}
	s := p.state.service
	if err := s.Checkpoint(ctx, int64(len(name))+1); err != nil {
		return Location{}, err
	}
	if name == "" {
		return Location{root: s.operation.root, rootDirectory: true}, nil
	}
	key, err := NewKey(name)
	if err != nil {
		return Location{}, err
	}
	n, err := s.projectNormalizer(p.state.view)
	if err != nil {
		return Location{}, err
	}
	if err = n.checkKey(key); err != nil {
		return Location{}, err
	}
	return Location{root: s.operation.root, key: key}, nil
}
func (s *Service) projectNormalizer(view View) (*Normalizer, error) {
	var limits PathLimits
	var policy NamePolicy
	if s.filesystem != nil {
		limits = s.filesystem.filesystem.limits.Path
		policy = NativeNamePolicy
	} else {
		limits = PathLimits{s.supplied.limits.PathBytes, s.supplied.limits.PathDepth}
	}
	return NewNormalizer(view, limits, policy)
}

// Resolve reuses the shared once-decoding pipeline from a literal directory
// base. The supplied span must belong to this exact acquired source. Empty and
// query-only destinations resolve to that directory, never the config itself.
func (p ProjectSource) Resolve(ctx context.Context, span parser.Span, base Location, destination string, leadingSlash bool) (NormalizedLink, error) {
	if p.state == nil || span.SourceToken() != p.state.source.Token() || base.root != p.state.view.Root() {
		return NormalizedLink{}, ErrContract
	}
	if _, err := p.state.source.Text(span); err != nil {
		return NormalizedLink{}, ErrContract
	}
	s := p.state.service
	if err := s.Checkpoint(ctx, int64(len(destination))+1); err != nil {
		return NormalizedLink{}, err
	}
	if strings.HasPrefix(destination, "/") && !strings.HasPrefix(destination, "//") {
		if !leadingSlash {
			return NormalizedLink{class: Ignored}, nil
		}
		destination = destination[1:]
	}
	n, err := s.projectNormalizer(p.state.view)
	if err != nil {
		return NormalizedLink{}, err
	}
	out, err := n.normalize(Link{}, Target{}, destination, base.key.value, true)
	if err == nil && out.class == Local && !base.rootDirectory && out.location.key != base.key && !strings.HasPrefix(out.location.key.value, base.key.value+"/") {
		out.class = BlockedReference
		out.location = Location{}
		out.problem = Problem{Kind: Blocked, Message: "Resource path escapes the documentation subtree."}
	}
	return out, errors.Join(err, s.Checkpoint(ctx, 0))
}

// Inspect acquires an already resolved location with the same service/view and
// lifecycle. A root directory has a dedicated adapter fact, never a fake key.
func (p ProjectSource) Inspect(ctx context.Context, location Location, required RequiredType) (ObservationSpec, error) {
	if p.state == nil || location.root != p.state.view.Root() || required != AnyType && required != RegularFile && required != DirectoryType {
		return ObservationSpec{}, ErrContract
	}
	s := p.state.service
	if err := s.Checkpoint(ctx, 1); err != nil {
		return filesystemStop(Unknown, err), err
	}
	if location.rootDirectory {
		existence, err := s.RootDirectory(ctx, location)
		return ObservationSpec{Existence: existence, Content: Inapplicable, Anchors: Inapplicable}, errors.Join(err, s.Checkpoint(ctx, 0))
	}
	if location.key.value == "" {
		return ObservationSpec{}, ErrContract
	}
	if err := s.operation.budget.ChargeNodes(2); err != nil {
		return ObservationSpec{}, err
	}
	q := Request{project: true, view: p.state.view, identity: &requestIdentity{}, spec: RequestSpec{Key: location.key, Class: Local, Capability: ExistenceOnly, Required: required}}
	return s.Read(ctx, q)
}

// IndexLocation plans the one configured regular-file basename after a proven
// directory. It performs no acquisition and offers no recursive fallback.
func (p ProjectSource) IndexLocation(ctx context.Context, directory Location) (Location, error) {
	if p.state == nil || directory.root != p.state.view.Root() {
		return Location{}, ErrContract
	}
	name := p.state.service.operation.settings.IndexFile()
	if !directory.rootDirectory {
		if directory.key.value == "" {
			return Location{}, ErrContract
		}
		name = directory.key.value + "/" + name
	}
	return p.DirectoryLocation(ctx, name)
}

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
