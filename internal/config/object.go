package config

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"unicode/utf8"
)

// DecodeObject validates an already typed version-1 configuration and returns an
// owned syntax snapshot. It accepts only exact bool, string, int, float64,
// []string, []any, and map[string]any values; named types, null (including nil
// maps/slices), and coercions are rejected. Floating-point values must be finite.
// Strings and mapping keys must be valid UTF-8. Object origins carry property
// paths, without file paths or text positions.
// The YAML structured-depth, document-node, and work limits also bound this
// traversal. Rule IDs, option domains, and capabilities need semantic validation.
func DecodeObject(object map[string]any) (*Definition, error) {
	b := objectBuilder{active: make(map[objectIdentity]bool)}
	root, err := b.value(object, "", 0)
	if err != nil {
		return nil, err
	}
	return definition(root)
}

// A slice view is its type, starting pointer and length. A shorter prefix can
// share an active backing array without containing the parent view or a cycle.
// Capacity does not affect the elements traversed by this decoder.
type objectIdentity struct {
	typ     reflect.Type
	pointer uintptr
	length  int
}
type objectBuilder struct {
	active map[objectIdentity]bool
	nodes  int
	work   int64
}

func (b *objectBuilder) value(input any, path string, depth int) (LocatedValue, error) {
	origin := Origin{PropertyPath: path}
	fail := func(message string) (LocatedValue, error) {
		return LocatedValue{}, &DecodeError{Message: message, Origin: origin}
	}
	b.nodes++
	if b.nodes > 250000 {
		return fail("document-nodes limit exceeded during object validation")
	}
	b.work += int64(len(path)) + 1
	if b.work > 250000000 {
		return fail("work-units limit exceeded during object validation")
	}
	v := LocatedValue{node: &syntaxValue{origin: origin}}
	scalar := func(kind ValueKind, text string, value any) (LocatedValue, error) {
		b.work += int64(len(text))
		if b.work > 250000000 {
			return fail("work-units limit exceeded during object validation")
		}
		if kind == StringValue && !utf8.ValidString(text) {
			return fail("object strings must be UTF-8")
		}
		v.node.kind, v.node.text = kind, text
		// Objects have no lexical spelling. Conversion preserves their exact type.
		v.node.conversion = &scalarConversion{value: value, representable: true}
		return v, nil
	}
	var id objectIdentity
	var size int
	switch x := input.(type) {
	case nil:
		return fail("null settings are not permitted")
	case bool:
		return scalar(BooleanValue, strconv.FormatBool(x), x)
	case string:
		return scalar(StringValue, x, x)
	case int:
		return scalar(IntegerValue, strconv.Itoa(x), x)
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return fail("non-finite numbers are not permitted")
		}
		return scalar(NumberValue, strconv.FormatFloat(x, 'g', -1, 64), x)
	case map[string]any:
		if x == nil {
			return fail("null settings are not permitted")
		}
		id = objectIdentity{typ: reflect.TypeOf(x), pointer: uintptr(reflect.ValueOf(x).UnsafePointer())}
		size = len(x)
	case []any:
		if x == nil {
			return fail("null settings are not permitted")
		}
		id = objectIdentity{typ: reflect.TypeOf(x), pointer: reflect.ValueOf(x).Pointer(), length: len(x)}
		size = len(x)
	case []string:
		if x == nil {
			return fail("null settings are not permitted")
		}
		id = objectIdentity{typ: reflect.TypeOf(x), pointer: reflect.ValueOf(x).Pointer(), length: len(x)}
		size = len(x)
	default:
		return fail(fmt.Sprintf("unsupported object value type %T", input))
	}
	if depth >= structuredDepth {
		return fail("structured-input-depth limit exceeded")
	}
	if size > 0 && b.active[id] {
		return fail("cyclic object is not permitted")
	}
	if size > 250000-b.nodes {
		return fail("document-nodes limit exceeded during object validation")
	}
	if size > 0 {
		b.active[id] = true
		defer delete(b.active, id)
	}
	switch x := input.(type) {
	case map[string]any:
		v.node.kind = MappingValue
		// Sort keys for stable diagnostics, charging their inspection before sorting.
		keys := make([]string, 0, len(x))
		for key := range x {
			b.work += int64(len(path)) + int64(len(key)) + 1
			if b.work > 250000000 {
				return fail("work-units limit exceeded during object validation")
			}
			keys = append(keys, key)
		}
		slices.Sort(keys)
		v.node.entries = make([]LocatedEntry, 0, len(x))
		for _, key := range keys {
			if !utf8.ValidString(key) {
				return LocatedValue{}, &DecodeError{Message: "object mapping keys must be UTF-8", Origin: Origin{PropertyPath: childPath(path, key)}}
			}
			b.nodes++ // Mapping keys are syntax nodes, just as in YAML.
			if b.nodes > 250000 {
				return fail("document-nodes limit exceeded during object validation")
			}
			child, err := b.value(x[key], childPath(path, key), depth+1)
			if err != nil {
				return LocatedValue{}, err
			}
			v.node.entries = append(v.node.entries, LocatedEntry{Key: key, Origin: child.Origin(), Value: child})
		}
	case []any:
		v.node.kind = SequenceValue
		v.node.elements = make([]LocatedValue, 0, len(x))
		for i, item := range x {
			child, err := b.value(item, fmt.Sprintf("%s[%d]", path, i), depth+1)
			if err != nil {
				return LocatedValue{}, err
			}
			v.node.elements = append(v.node.elements, child)
		}
	case []string:
		v.node.kind = SequenceValue
		v.node.elements = make([]LocatedValue, 0, len(x))
		for i, item := range x {
			child, err := b.value(item, fmt.Sprintf("%s[%d]", path, i), depth+1)
			if err != nil {
				return LocatedValue{}, err
			}
			v.node.elements = append(v.node.elements, child)
		}
	}
	return v, nil
}
