package integration

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/parser"
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
	matcher  *config.Patterns
	patterns []string
	origin   config.Origin
}

func newSlidePolicy(ctx context.Context, operation *resource.Operation, settings SlideSettings) (*slidePolicy, error) {
	if ctx == nil || operation == nil {
		return nil, ErrContract
	}
	if err := errors.Join(ctx.Err(), operation.Context().Err(), operation.Budget().ChargeWork(0)); err != nil {
		return nil, err
	}
	if len(settings.Patterns) > config.MaxPatterns {
		_, err := config.CompilePatterns(settings.Patterns)
		return nil, err
	}
	total := int64(0)
	for i, pattern := range settings.Patterns {
		total += int64(len(pattern))
		if len(pattern) > config.MaxPatternBytes || total > config.MaxPatternListBytes {
			return nil, &config.PatternError{Index: i, Pattern: pattern, Err: config.ErrPatternLimit}
		}
	}
	originBytes := int64(len(settings.Origin.Path)) + int64(len(settings.Origin.PropertyPath))
	if originBytes > operation.Budget().Limits().DocumentBytes {
		return nil, &parser.LimitExceeded{Name: "document-bytes", Observed: originBytes, Allowed: operation.Budget().Limits().DocumentBytes}
	}
	if err := operation.Checkpoint(8*(total+originBytes) + 4*int64(len(settings.Patterns)) + 1); err != nil {
		return nil, err
	}
	segments := int64(0)
	for _, pattern := range settings.Patterns {
		segments += int64(strings.Count(pattern, "/") + 1)
	}
	if err := operation.Budget().ChargeNodes(2*segments + int64(len(settings.Patterns)) + 2); err != nil {
		return nil, err
	}
	if !utf8.ValidString(settings.Origin.Path) || !utf8.ValidString(settings.Origin.PropertyPath) || strings.ContainsRune(settings.Origin.Path, 0) || strings.ContainsRune(settings.Origin.PropertyPath, 0) || settings.Origin.Position != nil && (settings.Origin.Position.Line < 1 || settings.Origin.Position.Column < 1) {
		return nil, ErrContract
	}
	matcher, err := config.CompilePatterns(settings.Patterns)
	if err != nil {
		return nil, err
	}
	policy := &slidePolicy{matcher: matcher, patterns: slices.Clone(settings.Patterns), origin: copySlideOrigin(settings.Origin)}
	if err := errors.Join(ctx.Err(), operation.Context().Err(), operation.Budget().ChargeWork(0)); err != nil {
		return nil, err
	}
	return policy, nil
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
	if !inBase {
		out.status = Available
		return out, nil
	}
	// The matcher charges each actual visited memo row and name segment before
	// allocation, and shares its one work allowance across the complete list.
	i, matched, err := s.slides.matcher.MatchFirstBounded(ctx, s.operation.Budget(), name, false, true)
	if err != nil {
		return out, errors.Join(err, s.checkpoint(ctx, 0, 0))
	}
	if err := s.checkpoint(ctx, 0, 0); err != nil {
		return out, err
	}
	out.status = Available
	if matched {
		out.matched = s.slides.patterns[i]
	}
	return out, nil
}
