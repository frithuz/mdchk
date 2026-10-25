// Package integration defines explicit, bounded document/project context policy.
// Pure policy decisions do not prove parser recognition, resource acquisition or
// complete site inventories. Concrete producers supply that evidence separately.
package integration

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

var ErrContract = errors.New("invalid integration context contract")

type Status string

const (
	Inactive    Status = "inactive"
	Available   Status = "available"
	Unavailable Status = "unavailable"
)

// Settings is explicit adapter-selected policy. It neither admits unavailable
// build capabilities nor changes flavor, frontmatter, rules or target selection.
type Settings struct {
	MkDocs MkDocsPolicy
	Slides SlideSettings
}

// Scope binds contexts to one existing resource operation and immutable view.
// It creates no worker, cache, root, resource reader or allowance.
type Scope struct {
	operation *resource.Operation
	view      resource.View
	mkdocs    MkDocsPolicy
	slides    *slidePolicy
}

func NewScope(ctx context.Context, operation *resource.Operation, view resource.View, settings Settings) (*Scope, error) {
	if ctx == nil || !operation.OwnsView(view) {
		return nil, ErrContract
	}
	s := &Scope{operation: operation, view: view}
	if err := s.checkpoint(ctx, 1, 1); err != nil {
		return nil, err
	}
	if int64(len(settings.MkDocs.ConfigPath)) > resource.DefaultPathLimits().Bytes {
		return nil, ErrContract
	}
	if err := s.checkpoint(ctx, int64(len(settings.MkDocs.ConfigPath))+1, 0); err != nil {
		return nil, err
	}
	policy, err := settings.MkDocs.normalized()
	if err != nil {
		return nil, err
	}
	s.mkdocs = policy
	s.slides, err = newSlidePolicy(ctx, operation, settings.Slides)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Document is selected-source identity, not syntax or cross-file authority.
// Unavailable Saved documents remain unavailable instead of borrowing originals.
type Document struct {
	scope     *Scope
	id        string
	source    parser.Source
	available bool
	proof     *documentProof
}
type documentProof struct {
	scope     *Scope
	id        string
	source    parser.Source
	available bool
}

func (d Document) ID() string            { return d.id }
func (d Document) Source() parser.Source { return d.source }
func (d Document) Available() bool       { return d.scope != nil && d.available }
func (d Document) View() resource.View {
	if d.scope == nil {
		return resource.View{}
	}
	return d.scope.view
}
func (s *Scope) Document(ctx context.Context, id string) (Document, error) {
	if s == nil || ctx == nil || id == "" {
		return Document{}, ErrContract
	}
	if int64(len(id)) > s.operation.Budget().Limits().DocumentBytes {
		return Document{}, ErrContract
	}
	if err := s.checkpoint(ctx, int64(len(id)+1)*int64(s.view.TargetCount()+1), 2); err != nil {
		return Document{}, err
	}
	if !utf8.ValidString(id) {
		return Document{}, ErrContract
	}
	target, available, selected := s.view.Target(id)
	if !selected {
		return Document{}, ErrContract
	}
	if available {
		if int64(target.Source.Len()) > s.operation.Budget().Limits().DocumentBytes {
			return Document{}, &parser.LimitExceeded{Name: "document-bytes", Observed: int64(target.Source.Len()), Allowed: s.operation.Budget().Limits().DocumentBytes}
		}
		if err := s.checkpoint(ctx, int64(target.Source.Len())+1, 0); err != nil {
			return Document{}, err
		}
		whole, _ := target.Source.Span(0, target.Source.Len())
		text, _ := target.Source.Text(whole)
		if !utf8.ValidString(text) {
			return Document{}, ErrContract
		}
	}
	proof := &documentProof{s, id, target.Source, available}
	return Document{scope: s, id: id, source: target.Source, available: available, proof: proof}, nil
}
func (s *Scope) accepts(d Document) bool {
	// The view and proof are immutable. Revalidating through a linear target
	// scan on every policy call would do unmetered work after a stop.
	return s != nil && d.scope == s && d.proof != nil && d.proof.scope == s && d.id == d.proof.id && d.available == d.proof.available && d.source.Token() == d.proof.source.Token()
}
func (s *Scope) checkpoint(ctx context.Context, work, nodes int64) error {
	if s == nil || s.operation == nil || ctx == nil {
		return ErrContract
	}
	// Independently observed operation and caller controls must both survive.
	if err := errors.Join(ctx.Err(), s.operation.Context().Err(), s.operation.Budget().ChargeWork(0)); err != nil {
		return err
	}
	if err := s.operation.Checkpoint(work); err != nil {
		return err
	}
	return s.operation.Budget().ChargeNodes(nodes)
}

// Location owns original byte provenance only, never recognition authority.
type Location struct {
	source parser.Source
	span   parser.Span
}

func NewLocation(source parser.Source, span parser.Span) (Location, error) {
	if source.Token() == (parser.SourceToken{}) || span.SourceToken() != source.Token() {
		return Location{}, ErrContract
	}
	if _, err := source.Text(span); err != nil {
		return Location{}, ErrContract
	}
	return Location{source, span}, nil
}
func (l Location) Source() parser.Source { return l.source }
func (l Location) Span() parser.Span     { return l.span }
func (l Location) valid() bool {
	if l.source.Token() == (parser.SourceToken{}) || l.span.SourceToken() != l.source.Token() {
		return false
	}
	_, err := l.source.Text(l.span)
	return err == nil
}
