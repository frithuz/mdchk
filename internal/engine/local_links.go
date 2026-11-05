package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
)

// LocalLinks retains both caller and originating controls and returns only
// detached Link and Image destination observations. It grants no alternate parse or correction authority.
func (c RuleContext) LocalLinks(ctx context.Context) (out parser.LocalLinksInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.LocalLinksInventory{}
		}
	}()
	if _, ok := c.options["check_images"].(bool); !ok || len(c.options) != 1 {
		return out, parser.ErrModel
	}
	return c.document.LocalLinks(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// CheckImages reports the validated rule option; it does not grant resource access.
func (c RuleContext) CheckImages() bool {
	v, _ := c.options["check_images"].(bool)
	return v
}

// ExistenceResolution is one prepared and completed shared existence query.
// Available is false when no resource context could be supplied for this
// operation.
type ExistenceResolution struct {
	Resolution resource.ExistenceResolution
	Available  bool
	// Oversized marks a destination beyond the shared path bounds: the context
	// exists, but this one link cannot be named, so no query was admitted.
	Oversized bool
}

// ResolveExistence resolves one parser-semantic destination at its original
// span through the operation's shared resources, reading no content. Stops are
// returned as errors; an unavailable context is not an error.
func (c RuleContext) ResolveExistence(ctx context.Context, destination string, span parser.Span) (ExistenceResolution, error) {
	if ctx == nil || c.operation == nil || !c.syntaxReliable {
		return ExistenceResolution{}, parser.ErrModel
	}
	if c.fragments == nil {
		return ExistenceResolution{}, nil
	}
	fragments, _, err := c.fragments.Fragments(ctx)
	if err != nil {
		if isInterruption(err) {
			return ExistenceResolution{}, err
		}
		return ExistenceResolution{}, nil
	}
	resolver, err := fragments.Existence()
	if err != nil {
		return ExistenceResolution{}, err
	}
	link, err := resource.NewLink(c.input.path, "MD057", destination, c.input.source, span)
	if err != nil {
		return ExistenceResolution{}, err
	}
	query, err := resolver.Prepare(link)
	if err != nil {
		// As in ResolveFragment, an over-long destination reduces this link's
		// coverage; budget, deadline and cancellation stops still return.
		var limit *parser.LimitExceeded
		if errors.As(err, &limit) && (limit.Name == "path-bytes" || limit.Name == "path-depth") {
			return ExistenceResolution{Oversized: true}, nil
		}
		return ExistenceResolution{}, err
	}
	resolution, err := resolver.Resolve(ctx, query)
	if err != nil && resolution.Outcome() == "" {
		return ExistenceResolution{}, err
	}
	// Stops after a completed outcome remain on the resolution and its report.
	if err != nil && isInterruption(err) {
		return ExistenceResolution{Resolution: resolution, Available: true}, err
	}
	return ExistenceResolution{Resolution: resolution, Available: true}, nil
}

func (c RuleContext) reportExistence(ctx context.Context, out result.Result, resolutions []resource.ExistenceResolution) (result.Result, error) {
	if c.fragments == nil {
		return out, errors.New("existence resolutions without a resource context")
	}
	fragments, view, err := c.fragments.Fragments(ctx)
	if err != nil {
		return out, err
	}
	resolver, err := fragments.Existence()
	if err != nil {
		return out, err
	}
	report, err := resource.NewExistenceReport(resolver, out)
	if err != nil {
		return out, err
	}
	appendErr := report.Append(view, resolutions)
	finished, finishErr := report.Finish()
	if finishErr != nil {
		return out, errors.Join(appendErr, finishErr)
	}
	return finished, appendErr
}
