package config

import (
	"fmt"
	"slices"
)

// ValueKind is the uncoerced syntax kind, before rule-option domain validation.
type ValueKind string

const (
	MappingValue  ValueKind = "mapping"
	SequenceValue ValueKind = "sequence"
	StringValue   ValueKind = "string"
	IntegerValue  ValueKind = "integer"
	NumberValue   ValueKind = "number"
	BooleanValue  ValueKind = "boolean"
)

// LocatedValue is an immutable, owned syntax snapshot. Accessors return fresh
// collection/position storage. No third-party syntax object crosses this API.
// Zero is an absent value, distinct from an explicitly empty mapping/sequence.
// Spelling retains scalar notation (including quotes), independently of its
// decoded string value. Alias descendants carry the alias use-site origin.
type LocatedValue struct{ node *syntaxValue }

// scalarConversion is immutable and shared by all uses of one source scalar.
// Failed/unrepresentable conversion is cached too; fallback LocatedValues keep
// their own origins instead of caching an origin-bearing wrapper.
type scalarConversion struct {
	value         any
	representable bool
}
type syntaxValue struct {
	conversion     *scalarConversion
	kind           ValueKind
	spelling, text string
	origin         Origin
	entries        []LocatedEntry
	elements       []LocatedValue
}
type LocatedEntry struct {
	Key    string
	Origin Origin
	Value  LocatedValue
}

func (v LocatedValue) Kind() ValueKind {
	if v.node == nil {
		return ""
	}
	return v.node.kind
}
func (v LocatedValue) Spelling() string {
	if v.node == nil {
		return ""
	}
	return v.node.spelling
}
func (v LocatedValue) StringValue() string {
	if v.node == nil {
		return ""
	}
	return v.node.text
}
func (v LocatedValue) Origin() Origin {
	if v.node == nil {
		return Origin{}
	}
	return cloneOrigin(v.node.origin)
}
func (v LocatedValue) Entries() []LocatedEntry {
	if v.node == nil {
		return nil
	}
	r := slices.Clone(v.node.entries)
	for i := range r {
		r[i].Origin = cloneOrigin(r[i].Origin)
	}
	return r
}
func (v LocatedValue) Elements() []LocatedValue {
	if v.node == nil {
		return nil
	}
	return slices.Clone(v.node.elements)
}
func cloneOrigin(o Origin) Origin {
	if o.Position != nil {
		p := *o.Position
		o.Position = &p
	}
	return o
}

// Syntax returns the complete immutable supplied definition for semantic
// validation, retaining option spelling and nested provenance. Mutating the
// passive Definition fields does not mutate this snapshot.
func (d *Definition) Syntax() LocatedValue {
	if d == nil {
		return LocatedValue{}
	}
	return d.syntax
}

// DecodeError identifies the rejected value/key and, for duplicate keys, both
// occurrences before a map can overwrite either one.
type DecodeError struct {
	Message  string
	Origin   Origin
	Previous *Origin
}

func (e *DecodeError) Error() string {
	s := fmt.Sprintf("%s: %s", formatOrigin(e.Origin), e.Message)
	if e.Previous != nil {
		s += fmt.Sprintf(" (previous occurrence at %s)", formatOrigin(*e.Previous))
	}
	return s
}
func formatOrigin(o Origin) string {
	s := o.Path
	if s == "" {
		s = "configuration"
	}
	if o.Position != nil {
		s += fmt.Sprintf(":%d:%d", o.Position.Line, o.Position.Column)
	}
	if o.PropertyPath != "" {
		s += " (" + o.PropertyPath + ")"
	}
	return s
}
func invalid(v LocatedValue, message string) error {
	return &DecodeError{Message: message, Origin: v.Origin()}
}
