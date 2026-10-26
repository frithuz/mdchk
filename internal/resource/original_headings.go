package resource

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// OriginalHeadingAnchors projects only heading facts from an actual completed
// original parse. It reuses the shared anchor builder and allowance, without
// creating a Service, acquiring bytes or returning cache/view/content authority.
// Returned slice and container storage are independently owned snapshots.
func OriginalHeadingAnchors(ctx context.Context, parsed parser.ParseResult, budget *parser.OperationBudget, index parser.LineIndex, options parser.ParseOptions, style string) (out []HeadingAnchor, availability Availability, err error) {
	availability = Unavailable
	if parsed.Document == nil || parsed.Source != parsed.Document.Source() || (style != "github" && style != "explicit") {
		return nil, availability, ErrContract
	}
	if err := parsed.Document.HeadingAnchorCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		if _, interrupted := InterruptionProblem(err); interrupted {
			availability = Interrupted
		}
		return nil, availability, err
	}
	defer func() {
		err = errors.Join(err, parsed.Document.HeadingAnchorCheckpoint(ctx, budget, index, options, 0, 0))
		if err != nil {
			out = nil
			if _, interrupted := InterruptionProblem(err); interrupted {
				availability = Interrupted
			} else if availability == Available {
				availability = Failed
			}
		}
	}()
	// This wrapper has no Root, selected targets or service. It is never exposed;
	// only the builder's charged local facts survive the call.
	operation := &Operation{ctx: ctx, budget: budget}
	key := ParseCacheKey{source: parsed.Source, options: options, anchor: style}
	anchors, err := buildAnchorIndex(ctx, operation, key, parsed)
	availability = anchors.Availability()
	if err != nil {
		return nil, availability, err
	}
	computed, ok := anchors.Index()
	if !ok {
		return nil, availability, nil
	}
	out, err = computed.Headings(ctx)
	return out, availability, err
}
