package integration

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/resource"
)

// SlideSettings carries the selected configuration's explicit annotation policy.
// Origin describes documents.slides; adapters retain its selected-config base.
// Neither these settings nor matching select documents or activate a rule.
type SlideSettings struct {
	Patterns []string
	Origin   config.Origin
}

type slidePolicy struct {
	policy *config.SlidePolicy
	origin config.Origin
}

func newSlidePolicy(ctx context.Context, operation *resource.Operation, settings SlideSettings) (*slidePolicy, error) {
	if ctx == nil || operation == nil {
		return nil, ErrContract
	}
	if err := errors.Join(ctx.Err(), operation.Context().Err(), operation.Budget().ChargeWork(0)); err != nil {
		return nil, err
	}
	policy, err := config.NewSlidePolicy(ctx, operation.Budget(), settings.Patterns, settings.Origin)
	if errors.Is(err, config.ErrInvalidSlideOrigin) {
		return nil, ErrContract
	}
	if err != nil {
		return nil, err
	}
	if err := errors.Join(ctx.Err(), operation.Context().Err(), operation.Budget().ChargeWork(0)); err != nil {
		return nil, err
	}
	return &slidePolicy{policy: policy, origin: copySlideOrigin(settings.Origin)}, nil
}

// SlideContext is annotation of one exact selected snapshot, not target,
// Markdown-flavor, frontmatter, integration or rule-selection authority.
type SlideContext struct {
	document Document
	status   Status
	matched  string
	origin   config.Origin
}

func (c SlideContext) Status() Status     { return c.status }
func (c SlideContext) Document() Document { return c.document }
func (c SlideContext) MatchedPattern() (string, bool) {
	return c.matched, c.status == Available && c.matched != ""
}
func (c SlideContext) Origin() config.Origin { return copySlideOrigin(c.origin) }

func copySlideOrigin(origin config.Origin) config.Origin {
	if origin.Position != nil {
		position := *origin.Position
		origin.Position = &position
	}
	return origin
}

// Slides matches a selected regular document directly against the selected
// configuration's patterns. The adapter supplies its normalized logical name
// relative to that configuration's directory (virtual root in standalone mode)
// and establishes containment before setting inBase. This mapping grants no
// resource or filesystem access. Outside-base names are not inspected.
//
// The first matching configured pattern is retained deterministically. Directory
// patterns do not implicitly annotate their descendants. Unavailable Saved data
// or an actual stop never becomes an ordinary-document determination.
func (s *Scope) Slides(ctx context.Context, document Document, name string, inBase bool) (SlideContext, error) {
	if ctx == nil || !s.accepts(document) || s.slides == nil {
		return SlideContext{}, ErrContract
	}
	out := SlideContext{document: document, status: Unavailable, origin: s.slides.origin}
	if err := s.checkpoint(ctx, 1, 1); err != nil {
		return out, err
	}
	if !document.available {
		return out, nil
	}
	// The matcher charges each actual visited memo row and name segment before
	// allocation, and shares its one work allowance across the complete list.
	annotation, err := s.slides.policy.Match(ctx, s.operation.Budget(), name, inBase)
	if err != nil {
		return out, errors.Join(err, s.checkpoint(ctx, 0, 0))
	}
	if err := s.checkpoint(ctx, 0, 0); err != nil {
		return out, err
	}
	out.status = Available
	if annotation.Slides {
		out.matched = annotation.Pattern
	}
	return out, nil
}
