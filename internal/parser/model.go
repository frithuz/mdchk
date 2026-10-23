package parser

import (
	"errors"
	"slices"
)

// Kind names model syntax, not the availability of a complete dialect profile.
type Kind string

const (
	DocumentKind   Kind = "document"
	Paragraph      Kind = "paragraph"
	Heading        Kind = "heading"
	ThematicBreak  Kind = "thematic-break"
	BlockQuote     Kind = "block-quote"
	List           Kind = "list"
	ListItem       Kind = "list-item"
	CodeBlock      Kind = "code-block"
	RawBlock       Kind = "raw-block"
	Div            Kind = "div"
	HTMLBlock      Kind = "html-block"
	InlineHTML     Kind = "inline-html"
	Text           Kind = "text"
	SoftBreak      Kind = "soft-break"
	HardBreak      Kind = "hard-break"
	CodeSpan       Kind = "code-span"
	Emphasis       Kind = "emphasis"
	Strong         Kind = "strong"
	Strike         Kind = "strike"
	Link           Kind = "link"
	Image          Kind = "image"
	Table          Kind = "table"
	TableRow       Kind = "table-row"
	TableCell      Kind = "table-cell"
	DefinitionList Kind = "definition-list"
	DefinitionItem Kind = "definition-item"
	DefinitionTerm Kind = "definition-term"
	DefinitionBody Kind = "definition-body"
	LineBlock      Kind = "line-block"
	LineBlockLine  Kind = "line-block-line"
)

// Transformation states how semantic content relates to original source. Exact
// segments require a source span; synthetic/unavailable mappings may lack one.
type Transformation string

const (
	Exact              Transformation = "exact"
	Escape             Transformation = "escape"
	Entity             Transformation = "entity"
	Normalized         Transformation = "normalized"
	PartialTab         Transformation = "partial-tab"
	Synthetic          Transformation = "synthetic"
	MappingUnavailable Transformation = "unavailable"
)

// Segment retains original provenance independently of optional semantic output.
// Even a transformed segment's Source always selects original, unmodified bytes.
type Segment struct {
	Source         Span
	Transformation Transformation
	Semantic       Optional[string]
}

// Value keeps raw spelling in ordered segments; Semantic can be known empty.
type Value struct {
	Segments []Segment
	Semantic Optional[string]
}

// Marker identifies a source spelling by its documented role in model.md.
type Marker struct {
	Role   string
	Source Span
}

// HeadingProperties describe ATX/Setext origin and level. Marker ranges use
// opening/closing/underline roles in Node.Markers, not derived semantic offsets.
type HeadingProperties struct {
	Level int
	Style string
}
type ListProperties struct {
	Ordered   bool
	Start     Optional[int]
	Delimiter Optional[byte]
	Bullet    Optional[byte]
	Tight     Optional[bool]
	// Item-only first-line facts. ContentIndent is relative to the enclosing
	// container, including leading indentation, marker width and effective padding.
	// Prefix maps actual consumed bytes/columns; empty-item padding may be virtual.
	MarkerWidth   Optional[int]
	Padding       Optional[int]
	ContentIndent Optional[int]
	Prefix        *Value
}
type CodeProperties struct {
	Style string
	Fence Optional[byte]
	Width Optional[int]
	Info  *Value
}
type HTMLProperties struct {
	BlockForm    Optional[int]
	FilteredTags []HTMLFilteredTag
}

// HTMLFilteredTag identifies an original raw prefix, not a new syntax owner.
// Prefix selects <[/]name without its boundary; Affected is its original '<'.
// Tag is present only when the shared HTML lexer proves a complete construct.
type HTMLFilteredTag struct {
	Name     string
	Closing  bool
	Prefix   Value
	Affected Span
	Tag      *Value
}
type DelimiterProperties struct {
	Marker byte
	Width  int
}
type BreakProperties struct{ Origin string }

// LinkProperties keeps visible use syntax separate from reference identifiers.
// Origin is inline/full/collapsed/shortcut, autolink (angle), or bare-gfm.
// For resolved reference origins, Destination/Title are spanless semantic
// projections; NormalizedLabel selects authoritative provenance in the owning
// document's first-definition lookup. See model.md for exact source/marker roles.
type LinkProperties struct {
	Origin string
	Label  Value
	// ReferenceLabel preserves the second bracket interior: present for full
	// and collapsed references (known empty for collapsed), nil otherwise.
	ReferenceLabel  *Value
	Destination     Value
	Title           *Value
	NormalizedLabel Optional[string]
	Resolution      Availability
}

// Properties are typed, kind-specific metadata. Nil means inapplicable/absent,
// not a known zero. Source segments are validated against the node envelope.
type Properties struct {
	Attributes  *Attributes
	PandocFence *PandocFenceProperties
	Div         *DivProperties
	Definition  *DefinitionProperties
	PandocList  *PandocListProperties
	PandocTask  *PandocTaskProperties
	PandocLine  *PandocLineProperties
	Task        *TaskProperties
	Table       *TableProperties
	Row         *TableRowProperties
	Cell        *TableCellProperties
	Heading     *HeadingProperties
	List        *ListProperties
	Code        *CodeProperties
	HTML        *HTMLProperties
	Delimiter   *DelimiterProperties
	Break       *BreakProperties
	Link        *LinkProperties
	Literal     Optional[string]
}

// NodeSpec is construction input. NewNode snapshots every mutable slice/pointer;
// subsequent input or accessor-result mutation cannot alter a constructed node.
type NodeSpec struct {
	Kind       Kind
	Envelope   Span
	Markers    []Marker
	Content    []Segment
	Properties Properties
	Children   []*Node
}

// Node is an immutable source-bound model value. It can be constructed for contract
// fixtures without implying that Parse recognizes its syntax.
type Node struct {
	spec                      NodeSpec
	depth                     int
	commentOpen, commentClose Span
	rawTag                    string
	rawClosing                bool
	htmlTail                  []*Node
}

var ErrModel = errors.New("invalid source model")

func NewNode(spec NodeSpec) (*Node, error) {
	if !validKind(spec.Kind) || !spec.Envelope.Present() {
		return nil, ErrModel
	}
	if err := validateMarkers(spec.Envelope, spec.Markers); err != nil {
		return nil, err
	}
	if err := validateSegments(spec.Envelope, spec.Content); err != nil {
		return nil, err
	}
	if err := validateProperties(spec.Kind, spec.Envelope, spec.Properties); err != nil {
		return nil, err
	}
	if err := validateValue(spec.Envelope, Value{Segments: spec.Content, Semantic: spec.Properties.Literal}); err != nil {
		return nil, err
	}
	if err := validateBareSpec(spec); err != nil {
		return nil, err
	}
	if err := validateFilteredSpec(spec); err != nil {
		return nil, err
	}
	if err := validateStrikeSpec(spec); err != nil {
		return nil, err
	}
	if err := validateAttributes(spec); err != nil {
		return nil, err
	}
	if err := validatePandocSpec(spec); err != nil {
		return nil, err
	}
	if err := validateTaskSpec(spec); err != nil {
		return nil, err
	}
	if err := validateTableSpec(spec); err != nil {
		return nil, err
	}
	previous := -1
	for _, child := range spec.Children {
		if child == nil || !contains(spec.Envelope, child.Span()) || child.Span().Start() < previous {
			return nil, ErrModel
		}
		previous = child.Span().Start()
	}
	spec.Markers = slices.Clone(spec.Markers)
	spec.Content = slices.Clone(spec.Content)
	spec.Children = cloneNodes(spec.Children)
	spec.Properties = cloneProperties(spec.Properties)
	return &Node{spec: spec}, nil
}

func (n *Node) Kind() Kind             { return n.spec.Kind }
func (n *Node) Span() Span             { return n.spec.Envelope }
func (n *Node) Markers() []Marker      { return slices.Clone(n.spec.Markers) }
func (n *Node) Content() []Segment     { return slices.Clone(n.spec.Content) }
func (n *Node) Properties() Properties { return cloneProperties(n.spec.Properties) }
func (n *Node) Children() []*Node      { return cloneNodes(n.spec.Children) }

// Detach exported handles as well as slice storage. A caller can replace an
// entire opaque struct through its pointer even without access to private fields.
// Private nested data is immutable and may be shared behind these handle copies.
func cloneNode(n *Node) *Node {
	if n == nil {
		return nil
	}
	copy := *n
	return &copy
}

func cloneNodes(nodes []*Node) []*Node {
	if nodes == nil {
		return nil
	}
	copies := make([]*Node, len(nodes))
	for i, n := range nodes {
		copies[i] = cloneNode(n)
	}
	return copies
}

// Walk visits root then children in supplied source order (pre-order). Equal-start
// nodes retain supplied order. Returning false stops the entire walk. Nil is empty.
func Walk(root *Node, visit func(*Node) bool) {
	if root == nil {
		return
	}
	stack := []*Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !visit(cloneNode(n)) {
			return
		}
		for i := len(n.spec.Children) - 1; i >= 0; i-- {
			stack = append(stack, n.spec.Children[i])
		}
	}
}

func validKind(k Kind) bool {
	switch k {
	case RawBlock, Div, DefinitionList, DefinitionItem, DefinitionTerm, DefinitionBody, LineBlock, LineBlockLine, DocumentKind, Paragraph, Heading, ThematicBreak, BlockQuote, List, ListItem, CodeBlock, HTMLBlock, InlineHTML, Text, SoftBreak, HardBreak, CodeSpan, Emphasis, Strong, Strike, Link, Image, Table, TableRow, TableCell:
		return true
	}
	return false
}

func validateMarkers(envelope Span, markers []Marker) error {
	previous := -1
	for _, m := range markers {
		if m.Role == "" || !contains(envelope, m.Source) || m.Source.Start() < previous {
			return ErrModel
		}
		previous = m.Source.Start()
	}
	return nil
}

func validateSegments(envelope Span, segments []Segment) error {
	previous := -1
	for _, s := range segments {
		switch s.Transformation {
		case Exact, Escape, Entity, Normalized, PartialTab, Synthetic, MappingUnavailable:
		default:
			return ErrModel
		}
		if !s.Source.Present() {
			if s.Transformation != Synthetic && s.Transformation != MappingUnavailable {
				return ErrModel
			}
			continue
		}
		if !contains(envelope, s.Source) || s.Source.Start() < previous {
			return ErrModel
		}
		previous = s.Source.Start()
		if s.Transformation == Exact && s.Semantic.Present {
			text, err := envelope.source.Text(s.Source)
			if err != nil || text != s.Semantic.Value {
				return ErrModel
			}
		}
	}
	return nil
}

func validateValue(envelope Span, v Value) error {
	if err := validateSegments(envelope, v.Segments); err != nil {
		return err
	}
	if !v.Semantic.Present {
		return nil
	}
	// Without a transformation record, known semantic text must be exactly the
	// concatenated original slices. Use MappingUnavailable/Synthetic explicitly
	// when the provider cannot supply a mapping; never silently imply identity.
	// First establish whether a transformation exists anywhere in the value.
	// A later transformed segment may account for aggregate normalization.
	for _, segment := range v.Segments {
		if segment.Transformation != Exact {
			return nil
		}
	}
	offset := 0
	for _, segment := range v.Segments {
		text, err := envelope.source.Text(segment.Source)
		if err != nil {
			return err
		}
		if len(text) > len(v.Semantic.Value)-offset || text != v.Semantic.Value[offset:offset+len(text)] {
			return ErrModel
		}
		offset += len(text)
	}
	if offset != len(v.Semantic.Value) {
		return ErrModel
	}
	return nil
}
func validateProperties(k Kind, e Span, p Properties) error {
	if p.Heading != nil && (k != Heading || p.Heading.Level < 1 || p.Heading.Level > 6 || (p.Heading.Style != "atx" && p.Heading.Style != "setext")) {
		return ErrModel
	}
	if p.List != nil && k != List && k != ListItem {
		return ErrModel
	}
	if p.List != nil && p.PandocList == nil {
		item := p.List
		if k != ListItem && (item.MarkerWidth.Present || item.Padding.Present || item.ContentIndent.Present || item.Prefix != nil) {
			return ErrModel
		}
		if item.MarkerWidth.Present && ((!item.Ordered && item.MarkerWidth.Value != 1) || (item.Ordered && (item.MarkerWidth.Value < 2 || item.MarkerWidth.Value > 10))) {
			return ErrModel
		}
		if item.Padding.Present && (item.Padding.Value < 1 || item.Padding.Value > 4) {
			return ErrModel
		}
		if item.ContentIndent.Present {
			if item.ContentIndent.Value < 2 || item.ContentIndent.Value > 17 {
				return ErrModel
			}
			if item.MarkerWidth.Present && item.Padding.Present {
				leading := item.ContentIndent.Value - item.MarkerWidth.Value - item.Padding.Value
				if leading < 0 || leading > 3 {
					return ErrModel
				}
			}
		}
		if item.Prefix != nil {
			if err := validateValue(e, *item.Prefix); err != nil {
				return err
			}
		}
		if p.List.Ordered {
			if p.List.Bullet.Present || (p.List.Start.Present && (p.List.Start.Value < 0 || p.List.Start.Value > 999999999)) {
				return ErrModel
			}
			if p.List.Delimiter.Present && p.List.Delimiter.Value != '.' && p.List.Delimiter.Value != ')' {
				return ErrModel
			}
		} else {
			if p.List.Start.Present || p.List.Delimiter.Present {
				return ErrModel
			}
			if p.List.Bullet.Present && p.List.Bullet.Value != '-' && p.List.Bullet.Value != '+' && p.List.Bullet.Value != '*' {
				return ErrModel
			}
		}
	}
	if p.Code != nil {
		if k != CodeBlock && k != RawBlock || (p.Code.Style != "indented" && p.Code.Style != "fenced") {
			return ErrModel
		}
		if p.Code.Width.Present && p.Code.Width.Value < 3 {
			return ErrModel
		}
		if p.Code.Fence.Present && p.Code.Fence.Value != '`' && p.Code.Fence.Value != '~' {
			return ErrModel
		}
		if p.Code.Style == "indented" && (p.Code.Fence.Present || p.Code.Width.Present || p.Code.Info != nil) {
			return ErrModel
		}
		if p.Code.Info != nil {
			if err := validateValue(e, *p.Code.Info); err != nil {
				return err
			}
		}
	}
	if p.HTML != nil {
		if k != HTMLBlock && k != InlineHTML {
			return ErrModel
		}
		if p.HTML.BlockForm.Present && (k != HTMLBlock || p.HTML.BlockForm.Value < 1 || p.HTML.BlockForm.Value > 7) {
			return ErrModel
		}
	}
	if p.Delimiter != nil {
		if k != CodeSpan && k != Emphasis && k != Strong && k != Strike {
			return ErrModel
		}
		if p.Delimiter.Width < 1 {
			return ErrModel
		}
		if k == CodeSpan && p.Delimiter.Marker != '`' {
			return ErrModel
		}
		if k == Strike && (p.Delimiter.Marker != '~' || p.Delimiter.Width > 2) {
			return ErrModel
		}
		if k == Emphasis || k == Strong {
			if p.Delimiter.Marker != '*' && p.Delimiter.Marker != '_' {
				return ErrModel
			}
			wantWidth := 1
			if k == Strong {
				wantWidth = 2
			}
			if p.Delimiter.Width != wantWidth {
				return ErrModel
			}
		}
	}
	if p.Break != nil && k != SoftBreak && k != HardBreak {
		return ErrModel
	}
	if p.Break != nil {
		if (k == SoftBreak && p.Break.Origin != "line-ending") || (k == HardBreak && p.Break.Origin != "spaces" && p.Break.Origin != "backslash") {
			return ErrModel
		}
	}
	if p.Link != nil {
		if k != Link && k != Image {
			return ErrModel
		}
		switch p.Link.Origin {
		case "inline", "full", "collapsed", "shortcut", "autolink", "bare-gfm":
		default:
			return ErrModel
		}
		if !validAvailability(p.Link.Resolution) {
			return ErrModel
		}
		if err := validateValue(e, p.Link.Label); err != nil {
			return err
		}
		if p.Link.ReferenceLabel != nil {
			if err := validateValue(e, *p.Link.ReferenceLabel); err != nil {
				return err
			}
		}
		l := p.Link
		switch l.Origin {
		case "full", "collapsed":
			if l.ReferenceLabel == nil || !referenceIdentifier(*l.ReferenceLabel, l.Origin == "collapsed") {
				return ErrModel
			}
		default:
			if l.ReferenceLabel != nil {
				return ErrModel
			}
		}
		if referenceOrigin(l.Origin) && l.Resolution == Available {
			if !l.NormalizedLabel.Present || l.NormalizedLabel.Value == "" || !referenceProjection(l.Destination) || l.Title != nil && !referenceProjection(*l.Title) {
				return ErrModel
			}
		}
		if err := validateValue(e, p.Link.Destination); err != nil {
			return err
		}
		if p.Link.Title != nil {
			if err := validateValue(e, *p.Link.Title); err != nil {
				return err
			}
		}
	}
	return nil
}

func cloneValue(v Value) Value {
	v.Segments = slices.Clone(v.Segments)
	return v
}
func clonePointer[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
func cloneValuePointer(p *Value) *Value {
	if p == nil {
		return nil
	}
	v := cloneValue(*p)
	return &v
}
func cloneProperties(p Properties) Properties {
	clonePandocProperties(&p)
	p.Attributes = cloneAttributes(p.Attributes)
	p.Div = clonePointer(p.Div)
	p.PandocFence = clonePointer(p.PandocFence)
	if p.PandocFence != nil {
		p.PandocFence.Classes = slices.Clone(p.PandocFence.Classes)
		p.PandocFence.Info = cloneValue(p.PandocFence.Info)
		p.PandocFence.RawFormat = cloneValuePointer(p.PandocFence.RawFormat)
	}
	p.Task = clonePointer(p.Task)
	if p.Task != nil {
		p.Task.Segments = slices.Clone(p.Task.Segments)
	}
	p.Table = clonePointer(p.Table)
	if p.Table != nil {
		p.Table.Alignments = slices.Clone(p.Table.Alignments)
	}
	p.Row = clonePointer(p.Row)
	if p.Row != nil {
		p.Row.Separators = slices.Clone(p.Row.Separators)
	}
	p.Cell = clonePointer(p.Cell)
	if p.Cell != nil {
		p.Cell.EscapedPipes = slices.Clone(p.Cell.EscapedPipes)
	}
	p.Heading = clonePointer(p.Heading)
	p.List = clonePointer(p.List)
	if p.List != nil {
		p.List.Prefix = cloneValuePointer(p.List.Prefix)
	}
	p.Code = clonePointer(p.Code)
	p.HTML = clonePointer(p.HTML)
	if p.HTML != nil {
		p.HTML.FilteredTags = slices.Clone(p.HTML.FilteredTags)
		for i := range p.HTML.FilteredTags {
			f := &p.HTML.FilteredTags[i]
			f.Prefix = cloneValue(f.Prefix)
			f.Tag = cloneValuePointer(f.Tag)
		}
	}
	p.Delimiter = clonePointer(p.Delimiter)
	p.Break = clonePointer(p.Break)
	p.Link = clonePointer(p.Link)
	if p.Code != nil {
		p.Code.Info = cloneValuePointer(p.Code.Info)
	}
	if p.Link != nil {
		p.Link.Label = cloneValue(p.Link.Label)
		p.Link.ReferenceLabel = cloneValuePointer(p.Link.ReferenceLabel)
		p.Link.Destination = cloneValue(p.Link.Destination)
		p.Link.Title = cloneValuePointer(p.Link.Title)
	}
	return p
}
