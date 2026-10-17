package parser

import (
	"maps"
	"slices"
)

// Availability describes whether data is reliable; empty data alone says nothing.
type Availability string

const (
	Available   Availability = "available"
	Partial     Availability = "partial"
	Unavailable Availability = "unavailable"
)

func validAvailability(a Availability) bool {
	return a == Available || a == Partial || a == Unavailable
}

// ReferenceSpec retains a definition independently of its use or effective lookup.
// Raw spelling is selected by Value segments; normalized labels are supplied by
// later reference providers, not computed by this foundation.
type ReferenceSpec struct {
	Envelope        Span
	Markers         []Marker
	Label           Value
	Destination     Value
	Title           *Value
	NormalizedLabel Optional[string]
}

type ReferenceDefinition struct{ spec ReferenceSpec }

func NewReferenceDefinition(spec ReferenceSpec) (*ReferenceDefinition, error) {
	if !spec.Envelope.Present() {
		return nil, ErrModel
	}
	if err := validateMarkers(spec.Envelope, spec.Markers); err != nil {
		return nil, err
	}
	for _, value := range []Value{spec.Label, spec.Destination} {
		if err := validateValue(spec.Envelope, value); err != nil {
			return nil, err
		}
	}
	if spec.Title != nil {
		if err := validateValue(spec.Envelope, *spec.Title); err != nil {
			return nil, err
		}
	}
	return &ReferenceDefinition{spec: cloneReference(spec)}, nil
}

func cloneReference(spec ReferenceSpec) ReferenceSpec {
	spec.Markers = slices.Clone(spec.Markers)
	spec.Label = cloneValue(spec.Label)
	spec.Destination = cloneValue(spec.Destination)
	spec.Title = cloneValuePointer(spec.Title)
	return spec
}

func (r *ReferenceDefinition) Properties() ReferenceSpec { return cloneReference(r.spec) }

// ReferenceCollection separates source-ordered records from effective lookup.
// Lookup maps already normalized labels to the first matching record's index.
// Unavailable lookup must have no entries; it never proves there are no references.
// NewDocument snapshots its slices/maps. Returned collections are independent copies.
type ReferenceCollection struct {
	Availability       Availability
	LookupAvailability Availability
	Definitions        []*ReferenceDefinition
	Lookup             map[string]int
}

type Document struct {
	root       *Node
	references ReferenceCollection
}

// NewDocument binds a complete source envelope to records. It validates supplied
// normalized-label selection, but neither recognizes nor normalizes definitions.
func NewDocument(root *Node, references ReferenceCollection) (*Document, error) {
	if root == nil || root.Kind() != DocumentKind || !root.Span().Present() {
		return nil, ErrModel
	}
	span := root.Span()
	if span.Start() != 0 || span.End() != span.source.Len() || !validAvailability(references.Availability) || !validAvailability(references.LookupAvailability) {
		return nil, ErrModel
	}
	if references.Availability == Unavailable && len(references.Definitions) != 0 {
		return nil, ErrModel
	}
	if references.LookupAvailability == Unavailable && len(references.Lookup) != 0 {
		return nil, ErrModel
	}
	if references.LookupAvailability == Available && references.Availability != Available {
		return nil, ErrModel
	}
	first := make(map[string]int)
	previous := -1
	for i, definition := range references.Definitions {
		if definition == nil || !contains(span, definition.spec.Envelope) || definition.spec.Envelope.Start() < previous {
			return nil, ErrModel
		}
		previous = definition.spec.Envelope.Start()
		label := definition.spec.NormalizedLabel
		if !label.Present {
			if references.LookupAvailability == Available {
				return nil, ErrModel
			}
			continue
		}
		if _, exists := first[label.Value]; !exists {
			first[label.Value] = i
		}
	}
	for label, i := range references.Lookup {
		if want, exists := first[label]; !exists || i != want {
			return nil, ErrModel
		}
	}
	if references.LookupAvailability == Available && !maps.Equal(first, references.Lookup) {
		return nil, ErrModel
	}
	return &Document{root: cloneNode(root), references: cloneReferences(references)}, nil
}

func cloneReferences(r ReferenceCollection) ReferenceCollection {
	r.Definitions = slices.Clone(r.Definitions)
	for i, definition := range r.Definitions {
		if definition != nil {
			copy := *definition
			r.Definitions[i] = &copy
		}
	}
	r.Lookup = maps.Clone(r.Lookup)
	return r
}

func (d *Document) Source() Source                  { return d.root.Span().source }
func (d *Document) Root() *Node                     { return cloneNode(d.root) }
func (d *Document) References() ReferenceCollection { return cloneReferences(d.references) }
