package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
)

// FragmentResources lazily supplies one operation's shared fragment resolver and
// its Original view. It is consulted only when a rule needs a resource decision,
// so documents without such links never open a root or bind a target. A non-nil
// error other than a stop means the context is unavailable; callers must report
// reduced coverage rather than guess. Implementations own resource lifetime.
type FragmentResources interface {
	Fragments(ctx context.Context) (*resource.FragmentResolver, resource.View, error)
}

// FragmentResolution is one prepared and completed shared query. Available is
// false when no resource context could be supplied for this operation.
type FragmentResolution struct {
	Resolution resource.FragmentResolution
	Available  bool
}

// ResolveFragment resolves one parser-semantic destination at its original span
// through the operation's shared resolver, honoring the rule's cross_file option.
// Stops are returned as errors; an unavailable context is not an error.
func (c RuleContext) ResolveFragment(ctx context.Context, destination string, span parser.Span) (FragmentResolution, error) {
	if ctx == nil || c.operation == nil || !c.syntaxReliable {
		return FragmentResolution{}, parser.ErrModel
	}
	if c.fragments == nil {
		return FragmentResolution{}, nil
	}
	resolver, _, err := c.fragments.Fragments(ctx)
	if err != nil {
		if isInterruption(err) {
			return FragmentResolution{}, err
		}
		return FragmentResolution{}, nil
	}
	link, err := resource.NewLink(c.input.path, "MD051", destination, c.input.source, span)
	if err != nil {
		return FragmentResolution{}, err
	}
	query, err := resolver.Prepare(link)
	if err != nil {
		// An over-long destination is a property of this one link, not a stop of
		// the operation: it has no resource identity, so the caller reports
		// reduced coverage. Budget, deadline and cancellation stops still return.
		var limit *parser.LimitExceeded
		if errors.As(err, &limit) && (limit.Name == "path-bytes" || limit.Name == "path-depth") {
			return FragmentResolution{}, nil
		}
		return FragmentResolution{}, err
	}
	resolution, err := resolver.Resolve(ctx, query, c.CrossFile())
	if err != nil && resolution.Outcome() == "" {
		return FragmentResolution{}, err
	}
	// Stops after a completed outcome remain on the resolution and its report.
	if err != nil && isInterruption(err) {
		return FragmentResolution{Resolution: resolution, Available: true}, err
	}
	return FragmentResolution{Resolution: resolution, Available: true}, nil
}

func (c RuleContext) reportFragments(ctx context.Context, out result.Result, resolutions []resource.FragmentResolution) (result.Result, error) {
	if c.fragments == nil {
		return out, errors.New("fragment resolutions without a resource context")
	}
	resolver, view, err := c.fragments.Fragments(ctx)
	if err != nil {
		return out, err
	}
	report, err := resource.NewFragmentReport(resolver, out)
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
