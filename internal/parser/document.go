package parser

import (
	"context"
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
// Parse's reference provider (or a constructed-model caller).
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
	// A recognized header is separate from the ordinary body AST, like the
	// non-rendered reference inventory. Only parsing can establish this record.
	frontmatter Frontmatter
	// Set only by a finished block pass that accounted for the entire source
	// without inline payload. Constructed nodes or available references alone
	// cannot establish this private parse handoff.
	inlineFree bool
	// Only a successful, complete block pass establishes this handoff. Model
	// constructors cannot manufacture recognition of the original source.
	blocksRecognized bool
	parsedComplete   bool
	commentOperation *operationIdentity
}

// NewDocument binds a complete source envelope to records. It validates supplied
// normalized-label selection and resolved reference-Link/Image bindings/projections,
// but neither recognizes syntax nor normalizes labels. It does not establish or
// inherit a parsed frontmatter record from gaps in the supplied body tree.
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
	if err := validateReferenceLinks(context.Background(), root, references); err != nil {
		return nil, err
	}
	return &Document{root: cloneNode(root), references: cloneReferences(references)}, nil
}

func referenceOrigin(origin string) bool {
	return origin == "full" || origin == "collapsed" || origin == "shortcut"
}

func referenceIdentifier(v Value, collapsed bool) bool {
	if !v.Semantic.Present || len(v.Segments) == 0 || (v.Semantic.Value == "") != collapsed {
		return false
	}
	mappedPipe := false
	for _, s := range v.Segments {
		if !s.Source.Present() {
			return false
		}
		switch s.Transformation {
		case Exact, Normalized, PartialTab:
		case Escape:
			// Table row mapping contributes a pipe while retaining its complete
			// original spelling. Ordinary core identifiers keep raw escapes;
			// no other decoded punctuation becomes an identifier mapping here.
			if s.Source.End()-s.Source.Start() != 2 || s.Semantic != Some("|") {
				return false
			}
			raw, err := s.Source.source.Text(s.Source)
			if err != nil || raw != "\\|" {
				return false
			}
			mappedPipe = true
		default:
			return false
		}
	}
	if mappedPipe {
		// A caller cannot use an allowed pipe record to justify an unrelated
		// aggregate identifier. Compare contributions without allocating a copy.
		offset := 0
		for _, s := range v.Segments {
			if !s.Semantic.Present || len(s.Semantic.Value) > len(v.Semantic.Value)-offset || v.Semantic.Value[offset:offset+len(s.Semantic.Value)] != s.Semantic.Value {
				return false
			}
			offset += len(s.Semantic.Value)
		}
		if offset != len(v.Semantic.Value) {
			return false
		}
	}
	return !collapsed || len(v.Segments) == 1 && v.Segments[0].Source.Start() == v.Segments[0].Source.End() && v.Segments[0].Transformation == Exact
}

// A projection is semantic data at a use site, never editable source spelling.
// The owning document's first-definition record retains its original provenance.
func referenceProjection(v Value) bool {
	return v.Semantic.Present && len(v.Segments) == 1 &&
		!v.Segments[0].Source.Present() && v.Segments[0].Transformation == Synthetic &&
		v.Segments[0].Semantic == v.Semantic
}

func validateReferenceLinks(ctx context.Context, root *Node, references ReferenceCollection) error {
	meterWork(ctx, 1)
	stack := []*Node{root}
	for len(stack) != 0 {
		meterWork(ctx, 1)
		if interruption(ctx) != "" {
			return nil // the parser withholds publication on interruption
		}
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		stack = meteredAppend(ctx, stack, n.spec.Children...)
		p := n.spec.Properties.Link
		if (n.spec.Kind != Link && n.spec.Kind != Image) || p == nil || !referenceOrigin(p.Origin) || p.Resolution != Available {
			continue
		}
		meterWork(ctx, len(p.NormalizedLabel.Value)+len(p.Destination.Semantic.Value))
		if p.Title != nil {
			meterWork(ctx, len(p.Title.Semantic.Value))
		}
		i, found := references.Lookup[p.NormalizedLabel.Value]
		if references.Availability != Available || references.LookupAvailability != Available || !p.NormalizedLabel.Present || !found || i < 0 || i >= len(references.Definitions) {
			return ErrModel
		}
		d := references.Definitions[i]
		if d == nil || !contains(root.Span(), d.spec.Envelope) || p.Destination.Semantic != d.spec.Destination.Semantic || !referenceProjection(p.Destination) || (p.Title == nil) != (d.spec.Title == nil) {
			return ErrModel
		}
		if p.Title != nil && (!referenceProjection(*p.Title) || p.Title.Semantic != d.spec.Title.Semantic) {
			return ErrModel
		}
	}
	return nil
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

// Source retains the immutable snapshot; a nil or zero Document has no source.
func (d *Document) Source() Source {
	if d == nil || d.root == nil {
		return Source{}
	}
	return d.root.Span().source
}
func (d *Document) Root() *Node                     { return cloneNode(d.root) }
func (d *Document) References() ReferenceCollection { return cloneReferences(d.references) }

// Frontmatter returns the opaque header established before body recognition.
// The boolean distinguishes absence from a present empty payload. The returned
// value has no mutable storage, and its spans belong to the original Source.
// It remains reliable when a recognized unterminated header makes ParseResult
// incomplete; callers must still honor the result's uncertainty and interruption.
func (d *Document) Frontmatter() (Frontmatter, bool) {
	return d.frontmatter, d.frontmatter.boundary.region.Present()
}
