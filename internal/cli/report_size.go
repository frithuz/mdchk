package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/result"
)

// Measure the exact standard-library JSON encoding before allocating it. Large
// scalar strings are walked with deadline checkpoints, including HTML and JSON
// escaping. Only fixed-size custom marshalers (currently source Range) are
// invoked here; raw JSON sections already carry their exact encoded spelling.
func reportSize(ctx context.Context, r result.Result, format string) (int64, error) {
	if format == "json" {
		n, err := jsonValueSize(ctx, reflect.ValueOf(r))
		return n + 1, err
	}
	w := &reportCounter{}
	if format == "github" {
		err := renderGitHub(ctx, w, w, r)
		return w.n, err
	}
	if format == "vscode" {
		err := renderVSCode(ctx, w, w, r)
		return w.n, err
	}
	err := renderText(ctx, w, r)
	return w.n, err
}

type reportCounter struct {
	n     int64
	limit int64
}

func (w *reportCounter) Write(p []byte) (int, error) {
	if w.limit > 0 && int64(len(p)) > w.limit-w.n {
		return 0, errReportBudget
	}
	w.n += int64(len(p))
	return len(p), nil
}

func jsonValueSize(ctx context.Context, v reflect.Value) (int64, error) {
	return (&jsonSizer{ctx: ctx}).value(v)
}

type jsonSizer struct {
	ctx          context.Context
	remaining    *int64
	terminalSafe bool
}

func (s *jsonSizer) charge(n int64) error {
	if s.remaining != nil {
		if n > *s.remaining {
			return errReportBudget
		}
		*s.remaining -= n
	}
	return s.ctx.Err()
}
func (s *jsonSizer) scalar(n int64, err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	return n, s.charge(n)
}
func (s *jsonSizer) value(v reflect.Value) (int64, error) {
	ctx := s.ctx
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !v.IsValid() {
		return s.scalar(4, nil)
	}
	if v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return s.scalar(4, nil)
		}
		return s.value(v.Elem())
	}
	if v.Type() == reflect.TypeFor[json.RawMessage]() {
		raw := v.Interface().(json.RawMessage)
		if raw == nil {
			return s.scalar(4, nil)
		}
		return s.scalar(jsonRawSize(ctx, raw, s.terminalSafe))
	}
	if v.Type() == reflect.TypeFor[result.Range]() {
		b, err := json.Marshal(v.Interface())
		return s.scalar(int64(len(b)), err)
	}
	switch v.Kind() {
	case reflect.String:
		return s.scalar(s.stringSize(v.String()))
	case reflect.Bool:
		if v.Bool() {
			return s.scalar(4, nil)
		}
		return s.scalar(5, nil)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return s.scalar(int64(len(strconv.FormatInt(v.Int(), 10))), nil)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return s.scalar(int64(len(strconv.FormatUint(v.Uint(), 10))), nil)
	case reflect.Float32, reflect.Float64:
		b, err := json.Marshal(v.Interface())
		return s.scalar(int64(len(b)), err)
	case reflect.Map:
		if v.IsNil() {
			return s.scalar(4, nil)
		}
		if v.Type().Key().Kind() != reflect.String {
			return 0, fmt.Errorf("unsupported report map key %s", v.Type())
		}
		if err := s.charge(2); err != nil {
			return 0, err
		}
		n := int64(2)
		count := 0
		it := v.MapRange()
		for it.Next() {
			if count > 0 {
				if err := s.charge(1); err != nil {
					return 0, err
				}
				n++
			}
			count++
			key, err := s.scalar(s.stringSize(it.Key().String()))
			if err != nil {
				return 0, err
			}
			if err := s.charge(1); err != nil {
				return 0, err
			}
			value, err := s.value(it.Value())
			if err != nil {
				return 0, err
			}
			n += key + 1 + value
		}
		return n, nil
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return s.scalar(4, nil)
		}
		if err := s.charge(2); err != nil {
			return 0, err
		}
		n := int64(2)
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				if err := s.charge(1); err != nil {
					return 0, err
				}
				n++
			}
			m, err := s.value(v.Index(i))
			if err != nil {
				return 0, err
			}
			n += m
		}
		return n, nil
	case reflect.Struct:
		if err := s.charge(2); err != nil {
			return 0, err
		}
		n := int64(2)
		count := 0
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			if !field.IsExported() {
				continue
			}
			tag := field.Tag.Get("json")
			name, options, _ := strings.Cut(tag, ",")
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			value := v.Field(i)
			if strings.Contains(options, "omitempty") && jsonEmpty(value) {
				continue
			}
			if count > 0 {
				if err := s.charge(1); err != nil {
					return 0, err
				}
				n++
			}
			count++
			key, err := s.scalar(s.stringSize(name))
			if err != nil {
				return 0, err
			}
			size, err := s.value(value)
			if err != nil {
				return 0, err
			}
			if err := s.charge(1); err != nil {
				return 0, err
			}
			n += key + 1 + size
		}
		return n, nil
	default:
		return 0, fmt.Errorf("unsupported report field type %s", v.Type())
	}
}
func jsonEmpty(v reflect.Value) bool {
	//nolint:exhaustive // Only kinds eligible for encoding/json omitempty are empty; structs and unsupported kinds are not.
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Interface, reflect.Pointer:
		return v.IsZero()
	}
	return false
}
func jsonStringSize(ctx context.Context, s string) (int64, error) {
	n := int64(2)
	nextCheck := 0
	for i := 0; i < len(s); {
		if i >= nextCheck {
			nextCheck = i + 1024
			if err := ctx.Err(); err != nil {
				return 0, err
			}
		}
		b := s[i]
		if b < utf8.RuneSelf {
			switch b {
			case '"', '\\', '\n', '\r', '\t', '\b', '\f':
				n += 2
			case '<', '>', '&':
				n += 6
			default:
				if b < 0x20 {
					n += 6
				} else {
					n++
				}
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if size == 1 && r == utf8.RuneError {
			encoded, err := json.Marshal(s[i : i+1])
			if err != nil {
				return 0, err
			}
			n += int64(len(encoded) - 2)
		} else if r == '\u2028' || r == '\u2029' {
			n += 6
		} else {
			n += int64(size)
		}
		i += size
	}
	return n, nil
}

// RawMessage values are validated producer-owned JSON. Count compaction and
// HTML escaping without allocating another encoded copy of the entire section.
func jsonRawSize(ctx context.Context, raw []byte, terminalSafe bool) (int64, error) {
	var n int64
	nextCheck := 0
	quoted, escaped := false, false
	for i := 0; i < len(raw); i++ {
		if i >= nextCheck {
			nextCheck = i + 1024
			if err := ctx.Err(); err != nil {
				return 0, err
			}
		}
		c := raw[i]
		if quoted {
			if escaped {
				escaped = false
				n++
				continue
			}
			if c == '\\' {
				escaped = true
				n++
				continue
			}
			if c == '"' {
				quoted = false
				n++
				continue
			}
			if terminalSafe && c == 0x7f {
				n += 4 // textField renders raw JSON DEL as the four visible bytes \x7f.
				continue
			}
			if terminalSafe && c >= utf8.RuneSelf {
				r, size := utf8.DecodeRune(raw[i:])
				if terminalControl(r) {
					n += 6
					i += size - 1
					continue
				}
			}
			if c == '<' || c == '>' || c == '&' {
				n += 6
				continue
			}
			if c == 0xe2 && i+2 < len(raw) && raw[i+1] == 0x80 && (raw[i+2] == 0xa8 || raw[i+2] == 0xa9) {
				n += 6
				i += 2
				continue
			}
			n++
			continue
		}
		if c == '"' {
			quoted = true
		}
		if c != ' ' && c != '\n' && c != '\r' && c != '\t' {
			n++
		}
	}
	return n, ctx.Err()
}

func (s *jsonSizer) stringSize(value string) (int64, error) {
	n, err := jsonStringSize(s.ctx, value)
	if err != nil || !s.terminalSafe {
		return n, err
	}
	nextCheck := 0
	for i, r := range value {
		if i >= nextCheck {
			nextCheck = i + 1024
			if err := s.ctx.Err(); err != nil {
				return 0, err
			}
		}
		if r == 0x7f {
			n += 3 // JSON emits one byte; terminal-safe text emits four.
		}
		if r >= 0x80 && r != 0x2028 && r != 0x2029 && terminalControl(r) {
			n += 6 - int64(utf8.RuneLen(r))
		}
	}
	return n, s.ctx.Err()
}
