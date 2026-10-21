package strictjson

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestStrictUnicodeAndNames(t *testing.T) {
	for _, input := range []string{
		`{"a":1,"a":2}`, `{"version":1,"\u0076ersion":1}`, `{"𐀀":1,"\ud800\udc00":2}`,
		`{"a":{"x":1,"x":2}}`, `"\ud800"`, `"\udc00"`, `"\udc00\ud800"`, `"\ud800\n"`, `{"\ud800":1}`,
		`{} {}`, `[1,]`, "\"\xff\"", "{\"\xff\":1}",
	} {
		if _, err := Decode([]byte(input), 1048576); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	for _, input := range []string{`null`, `true`, `1`, `1.0`, `"�"`, `"\ufffd"`, `"\ud800\udc00"`, `"𐀀"`, `"\\ud800"`, `{"a":{"x":1},"b":{"x":2}}`, `[null,{},[]]`} {
		if _, err := Decode([]byte(input), 1048576); err != nil {
			t.Errorf("%s: %v", input, err)
		}
	}
	v, err := Decode([]byte(`[1,1.0,1e0,"1"]`), 1024)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []any{json.Number("1"), json.Number("1.0"), json.Number("1e0"), "1"} {
		if v.Array[i].Scalar != want {
			t.Errorf("scalar %d=%#v", i, v.Array[i].Scalar)
		}
	}
}
func TestBoundsAndDuplicatePath(t *testing.T) {
	for _, tc := range []struct {
		input  string
		bound  int
		reason string
	}{
		{`null`, 3, "input-bytes"}, {strings.Repeat("[", 65) + strings.Repeat("]", 65), 1024, "structured-input-depth"},
		{"[" + strings.Repeat("0,", 250000) + "0]", 1048576, "document-nodes"},
	} {
		_, err := Decode([]byte(tc.input), tc.bound)
		if err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Fatalf("want %s got %v", tc.reason, err)
		}
	}
	if _, err := Decode([]byte(strings.Repeat("[", 64)+"0"+strings.Repeat("]", 64)), 1024); err != nil {
		t.Fatal(err)
	}
	_, err := Decode([]byte(`{"rules":{"MD013":{"length":1,"\u006cength":2}}}`), 1024)
	var e *Error
	ok := errors.As(err, &e)
	if !ok || e.Path != "rules.MD013.length" {
		t.Fatal(err)
	}
}

func TestRawWorkBudgetAndUnicodeProperty(t *testing.T) {
	input := "{\"" + strings.Repeat("x", 3000) + "\":[" + strings.Repeat("0,", 90000) + "0]}"
	if _, err := Decode([]byte(input), 1048576); err == nil || !strings.Contains(err.Error(), "work-units") {
		t.Fatal(err)
	}
	_, err := Decode([]byte(`{"resources":{"index_file":"\ud800"}}`), 1024)
	var e *Error
	ok := errors.As(err, &e)
	if !ok || e.Path != "resources.index_file" {
		t.Fatal(err)
	}
}
