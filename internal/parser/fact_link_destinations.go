package parser

import (
	"context"
	"slices"
)

// LinkDestination is one destination as source spells it, decoded once by the
// parser. Span runs from the first to the last segment with original source.
// Segments map the semantic Destination to source; a Synthetic autolink prefix
// or a MappingUnavailable segment may have no source.
type LinkDestination struct {
	Span        Span
	Destination string
	Segments    []Segment
}

// LinkDestinationsInventory contains detached destinations in source order.
// Unknown raw state or reference availability withholds every item.
type LinkDestinationsInventory struct {
	Destinations []LinkDestination
	Availability Availability
}

// LinkDestinations observes inline Link and Image destinations, angle and GFM
// extended autolinks, and each reference definition's destination once, under
// LocalLinks' traversal and exclusions. Reference-origin Links and Images are
// not observed: their definition owns the destination.
func (d *Document) LinkDestinations(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (LinkDestinationsInventory, error) {
	items, availability, err := proseLinks(d, ctx, budget, index, options, proseDestinations, func(span Span, destination Value, _ bool) (LinkDestination, bool) {
		return LinkDestination{Span: span, Destination: destination.Semantic.Value, Segments: slices.Clone(destination.Segments)}, true
	}, func(l LinkDestination) int { return l.Span.start })
	if err != nil || availability != Available {
		return LinkDestinationsInventory{Availability: availability}, err
	}
	return LinkDestinationsInventory{Destinations: items, Availability: Available}, nil
}
