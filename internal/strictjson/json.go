// Package strictjson validates bounded raw JSON before lossy map decoding.
// It has no configuration, transport, filesystem or request state.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Value preserves scalar kinds (numbers are json.Number), object entry order,
// and decoded names. Every returned collection is owned by this call.
type Value struct {
	Scalar any
	Object []Entry
	Array  []Value
	Kind   string
}
type Entry struct {
	Name  string
	Value Value
}

// Error identifies a property path when syntax permits one. It never invents a
// textual source location for supplied data.
type Error struct{ Path, Message string }

func (e *Error) Error() string { return fmt.Sprintf("JSON %s: %s", e.Path, e.Message) }

// Decode accepts exactly one JSON value, including scalar/array/null roots.
// maxBytes is the caller's positive raw-input bound. Containers are limited to
// 64 levels, syntax nodes to 250000 and traversal work to 250000000 units.
// Duplicate decoded names, malformed UTF-8 and unpaired surrogate escapes are
// rejected before ordinary decoding can discard or replace their evidence.
func Decode(content []byte, maxBytes int) (Value, error) {
	return DecodeMetered(content, maxBytes, nil)
}

// DecodeMetered adds caller-owned cumulative accounting and cancellation. check
// receives constructed records and work units before bounded inspections. Raw
// bytes reserve three inspections (UTF-8, JSON tokenization and escape checking);
// record/name/path visits are charged separately. It is called between tokens
// and at most 4096 escape-inspection bytes apart. A single encoding/json token
// operation remains bounded by maxBytes, not asynchronously interruptible.
// The fixed depth/node/work safety caps of Decode still apply independently;
// these are implementation ceilings, not configurable operator limits.
func DecodeMetered(content []byte, maxBytes int, check func(nodes, work int64) error) (Value, error) {
	fail := func(s string) (Value, error) { return Value{}, &Error{Message: s} }
	if maxBytes <= 0 || len(content) > maxBytes {
		return fail("input-bytes limit exceeded")
	}
	if check != nil {
		if err := check(0, int64(len(content))*3+1); err != nil {
			return Value{}, err
		}
	}
	if !utf8.Valid(content) {
		return fail("input must be UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(content))
	d.UseNumber()
	p := decoder{d: d, raw: content, check: check}
	v, err := p.value("", 0)
	if err != nil {
		return Value{}, err
	}
	if _, err = d.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return fail("multiple JSON values are not permitted")
		}
		return fail(err.Error())
	}
	if check != nil {
		if err := check(0, 0); err != nil {
			return Value{}, err
		}
	}
	return v, nil
}

type decoder struct {
	d     *json.Decoder
	raw   []byte
	nodes int
	work  int64
	check func(nodes, work int64) error
}

func (p *decoder) charge(path string, amount int) error {
	if p.check != nil {
		if err := p.check(1, int64(len(path))+int64(amount)+1); err != nil {
			return err
		}
	}
	p.nodes++
	p.work += int64(len(path)) + int64(amount) + 1
	if p.nodes > 250000 {
		return &Error{path, "document-nodes limit exceeded"}
	}
	if p.work > 250000000 {
		return &Error{path, "work-units limit exceeded"}
	}
	return nil
}
func (p *decoder) value(path string, depth int) (Value, error) {
	if err := p.charge(path, 0); err != nil {
		return Value{}, err
	}
	start := p.d.InputOffset()
	t, err := p.d.Token()
	if err != nil {
		return Value{}, &Error{path, err.Error()}
	}
	if err := p.unicode(start, path); err != nil {
		return Value{}, err
	}
	v := Value{Scalar: t}
	switch x := t.(type) {
	case json.Delim:
		if depth >= 64 {
			return Value{}, &Error{path, "structured-input-depth limit exceeded"}
		}
		v.Scalar = nil
		switch x {
		case '{':
			v.Kind = "object"
			v.Object = []Entry{}
			seen := map[string]bool{}
			for p.d.More() {
				keyStart := p.d.InputOffset()
				key, e := p.d.Token()
				if e != nil {
					return Value{}, &Error{path, e.Error()}
				}
				name, ok := key.(string)
				if !ok {
					return Value{}, &Error{path, "expected object name"}
				}
				next := name
				if path != "" {
					next = path + "." + name
				}
				if e = p.unicode(keyStart, next); e != nil {
					return Value{}, e
				}
				if e = p.charge(next, len(name)); e != nil {
					return Value{}, e
				}
				if seen[name] {
					return Value{}, &Error{next, "duplicate object name " + strconv.Quote(name)}
				}
				seen[name] = true
				child, e := p.value(next, depth+1)
				if e != nil {
					return Value{}, e
				}
				v.Object = append(v.Object, Entry{name, child})
			}
		case '[':
			v.Kind = "array"
			v.Array = []Value{}
			for p.d.More() {
				child, e := p.value(fmt.Sprintf("%s[%d]", path, len(v.Array)), depth+1)
				if e != nil {
					return Value{}, e
				}
				v.Array = append(v.Array, child)
			}
		default:
			return Value{}, &Error{path, "unexpected delimiter"}
		}
		end, e := p.d.Token()
		if e != nil {
			return Value{}, &Error{path, e.Error()}
		}
		if x == '{' && end != json.Delim('}') || x == '[' && end != json.Delim(']') {
			return Value{}, &Error{path, "unmatched delimiter"}
		}
	case string:
		v.Kind = "string"
		p.work += int64(len(x))
	case json.Number:
		v.Kind = "number"
		p.work += int64(len(x))
	case bool:
		v.Kind = "boolean"
	case nil:
		v.Kind = "null"
	default:
		return Value{}, &Error{path, "unexpected value"}
	}
	if p.work > 250000000 {
		return Value{}, &Error{path, "work-units limit exceeded"}
	}
	return v, nil
}

func (p *decoder) unicode(start int64, path string) error {
	if err := unicodeEscapesChecked(p.raw[start:p.d.InputOffset()], p.check); err != nil {
		var syntax *Error
		if errors.As(err, &syntax) {
			return &Error{Path: path, Message: syntax.Message}
		}
		return err
	}
	return nil
}

// Inspect only actual string escapes: consuming escaped backslashes prevents
// text such as "\\\\ud800" from being confused with a surrogate escape.
func unicodeEscapesChecked(b []byte, check func(nodes, work int64) error) error {
	in := false
	nextCheck := 0
	for i := 0; i < len(b); i++ {
		if check != nil && i >= nextCheck {
			if err := check(0, 0); err != nil {
				return err
			}
			nextCheck = i + 4096
		}
		if b[i] == '"' {
			in = !in
			continue
		}
		if !in || b[i] != '\\' {
			continue
		}
		i++
		if i >= len(b) {
			break
		}
		if b[i] != 'u' {
			continue
		}
		unit, ok := hexUnit(b, i+1)
		if !ok {
			continue
		}
		i += 4
		if unit >= 0xdc00 && unit <= 0xdfff {
			return &Error{Message: "unpaired Unicode surrogate escape"}
		}
		if unit < 0xd800 || unit > 0xdbff {
			continue
		}
		if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
			return &Error{Message: "unpaired Unicode surrogate escape"}
		}
		low, ok := hexUnit(b, i+3)
		if !ok || low < 0xdc00 || low > 0xdfff {
			return &Error{Message: "unpaired Unicode surrogate escape"}
		}
		i += 6
	}
	return nil
}
func hexUnit(b []byte, start int) (uint64, bool) {
	if start+4 > len(b) {
		return 0, false
	}
	s := string(b[start : start+4])
	if strings.ContainsAny(s, "+-") {
		return 0, false
	}
	n, e := strconv.ParseUint(s, 16, 16)
	return n, e == nil
}
