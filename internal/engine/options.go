package engine

import (
	"fmt"
	"maps"
	"slices"
)

// Options admit only immutable scalars and copied string lists. A rule cannot
// receive arbitrary mutable objects or callbacks through configuration.
type Options map[string]any

type OptionKind string

const (
	Boolean OptionKind = "boolean"
	Integer OptionKind = "integer"
	String  OptionKind = "string"
	Strings OptionKind = "strings"
)

type OptionSpec struct {
	Kind    OptionKind
	Default any
	Minimum *int
	Maximum *int
	Choices []string
}
type OptionSchema map[string]OptionSpec

func cloneValue(v any) any {
	if s, ok := v.([]string); ok {
		return slices.Clone(s)
	}
	return v
}
func cloneOptions(o Options) Options {
	r := make(Options, len(o))
	for k, v := range o {
		r[k] = cloneValue(v)
	}
	return r
}
func cloneSchema(s OptionSchema) OptionSchema {
	r := maps.Clone(s)
	for k, v := range r {
		v.Default = cloneValue(v.Default)
		v.Choices = slices.Clone(v.Choices)
		if v.Minimum != nil {
			x := *v.Minimum
			v.Minimum = &x
		}
		if v.Maximum != nil {
			x := *v.Maximum
			v.Maximum = &x
		}
		r[k] = v
	}
	return r
}
func (s OptionSpec) validate(v any) error {
	valid := false
	switch s.Kind {
	case Boolean:
		_, valid = v.(bool)
	case Integer:
		n, ok := v.(int)
		valid = ok && (s.Minimum == nil || n >= *s.Minimum) && (s.Maximum == nil || n <= *s.Maximum)
	case String:
		x, ok := v.(string)
		valid = ok && (len(s.Choices) == 0 || slices.Contains(s.Choices, x))
	case Strings:
		_, valid = v.([]string)
	}
	if !valid {
		return fmt.Errorf("value outside %s option domain", s.Kind)
	}
	return nil
}
func (s OptionSchema) resolve(o Options) (Options, error) {
	r := make(Options, len(s))
	for _, k := range sortedKeys(s) {
		r[k] = cloneValue(s[k].Default)
	}
	for _, k := range sortedKeys(o) {
		spec, ok := s[k]
		if !ok {
			return nil, fmt.Errorf("unknown option %q", k)
		}
		if err := spec.validate(o[k]); err != nil {
			return nil, fmt.Errorf("option %q: %w", k, err)
		}
		r[k] = cloneValue(o[k])
	}
	return r, nil
}
func sortedKeys[M ~map[string]V, V any](m M) []string {
	r := make([]string, 0, len(m))
	for k := range m {
		r = append(r, k)
	}
	slices.Sort(r)
	return r
}
