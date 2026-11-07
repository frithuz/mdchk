package report

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type inspectionCheckpointContext struct {
	context.Context
	calls, stop int
}

func (c *inspectionCheckpointContext) Err() error {
	c.calls++
	if c.calls >= c.stop {
		return context.DeadlineExceeded
	}
	return nil
}

func TestTextFieldTerminalControls(t *testing.T) {
	input := "\x00\x07\b\t\n\r\v\f\x1b\x7f\u0085\u009b\u202e\u2066雪🙂"
	want := `\x00\x07\b\t\n\r\v\f\x1b\x7f\u0085\u009b\u202e\u2066雪🙂`
	if got := TextField(input); got != want {
		t.Fatalf("terminal field %q, expected %q", got, want)
	}
}

func TestInspectionTerminalSafeSizeMatrix(t *testing.T) {
	var controls strings.Builder
	for r := rune(0); r <= 0x9f; r++ {
		if r < 0x20 || r >= 0x7f {
			controls.WriteRune(r)
		}
	}
	controls.WriteString("\u2028\u2029\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069")
	for _, input := range []struct{ name, text string }{
		{"DEL", strings.Repeat("\x7f", 100)},
		{"all controls", controls.String()},
		{"Unicode and HTML", "雪🙂👩‍💻<>&\"\\\ufffd"},
		{"literal escapes", `\x7f\u007f\u009b\n\u2028`},
	} {
		t.Run(input.name, func(t *testing.T) {
			for _, value := range []any{input.text, map[string]any{input.text: []any{input.text, map[string]any{"value": input.text}}}} {
				encoded, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				for _, v := range []any{value, json.RawMessage(append(append([]byte(" \n\t"), encoded...), []byte("\n ")...))} {
					var rendered bytes.Buffer
					if err := writeInspectionJSON(context.Background(), &rendered, v); err != nil {
						t.Fatal(err)
					}
					counter := &ReportCounter{}
					if err := writeInspectionJSON(context.Background(), counter, v); err != nil || counter.N != int64(rendered.Len()) {
						t.Fatalf("%T counter=%d actual=%d err=%v", v, counter.N, rendered.Len(), err)
					}
					ordinary, err := jsonValueSize(context.Background(), reflect.ValueOf(v))
					if err != nil || ordinary != int64(len(encoded)) {
						t.Fatalf("%T ordinary JSON count=%d expected=%d err=%v", v, ordinary, len(encoded), err)
					}
					wire, err := json.Marshal(v)
					if err != nil || !bytes.Equal(encoded, wire) {
						t.Fatalf("structured JSON identity changed for %T", v)
					}
					if rendered.Len() > len(encoded) {
						capBytes := (int64(rendered.Len()) + int64(len(encoded))) / 2
						capped := &ReportCounter{limit: capBytes}
						if err := writeInspectionJSON(context.Background(), capped, v); !errors.Is(err, ErrReportBudget) || capped.N != 0 {
							t.Fatalf("%T cap=%d incorrectly admitted actual=%d: count=%d err=%v", v, capBytes, rendered.Len(), capped.N, err)
						}
					}
				}
			}
		})
	}
}

func TestInspectionTerminalSafeScalarCheckpoints(t *testing.T) {
	// A one-byte prefix followed by multibyte controls exercises checkpoint
	// boundaries that do not align with UTF-8 rune starts.
	value := "x" + strings.Repeat("\u009b", 8000) + strings.Repeat("\x7f", 8000)
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []any{value, json.RawMessage(encoded)} {
		ctx := &inspectionCheckpointContext{Context: context.Background(), stop: 5}
		sizer := jsonSizer{ctx: ctx, terminalSafe: true}
		if _, err := sizer.value(reflect.ValueOf(v)); !errors.Is(err, context.DeadlineExceeded) || ctx.calls < 5 {
			t.Fatalf("%T missing scalar checkpoint: calls=%d err=%v", v, ctx.calls, err)
		}
	}
}
