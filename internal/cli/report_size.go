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
	err := renderText(ctx, w, r)
	return w.n, err
}

type reportCounter struct{ n int64 }

func (w *reportCounter) Write(p []byte) (int, error) { w.n += int64(len(p)); return len(p), nil }

func jsonValueSize(ctx context.Context, v reflect.Value) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !v.IsValid() {
		return 4, nil
	}
	if v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return 4, nil
		}
		return jsonValueSize(ctx, v.Elem())
	}
	if v.Type() == reflect.TypeFor[json.RawMessage]() {
		raw := v.Interface().(json.RawMessage)
		if raw == nil {
			return 4, nil
		}
		// RawMessage is compacted and HTML-escaped by encoding/json. These future
		// operation-owned sections are not arbitrary extension fields.
		encoded, err := json.Marshal(raw)
		return int64(len(encoded)), err
	}
	if v.Type() == reflect.TypeFor[result.Range]() {
		b, err := json.Marshal(v.Interface())
		return int64(len(b)), err
	}
	switch v.Kind() {
	case reflect.String:
		return jsonStringSize(ctx, v.String())
	case reflect.Bool:
		if v.Bool() {
			return 4, nil
		}
		return 5, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int64(len(strconv.FormatInt(v.Int(), 10))), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(len(strconv.FormatUint(v.Uint(), 10))), nil
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return 4, nil
		}
		n := int64(2)
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				n++
			}
			m, err := jsonValueSize(ctx, v.Index(i))
			if err != nil {
				return 0, err
			}
			n += m
		}
		return n, nil
	case reflect.Struct:
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
				n++
			}
			count++
			key, err := jsonStringSize(ctx, name)
			if err != nil {
				return 0, err
			}
			size, err := jsonValueSize(ctx, value)
			if err != nil {
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
