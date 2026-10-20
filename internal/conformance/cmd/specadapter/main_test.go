package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/frithuz/mdchk/internal/conformance"
)

func TestProtocolBoundary(t *testing.T) {
	for _, tt := range []struct {
		input, status, html string
		code                int
	}{
		{"", "rendered", "", 0}, {"[foo]: /url\n", "rendered", "", 0},
		{"[foo]: /url\n[foo]\n", "rendered", "<p><a href=\"/url\">foo</a></p>\n", 0},
		{"![foo](url)", "pending", "", 2}, {"\xff", "error", "", 1},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(nil, bytes.NewBufferString(tt.input), &stdout, &stderr); code != tt.code || stdout.String() != tt.html {
			t.Fatalf("HTML protocol: %d %q %q", code, stdout.String(), stderr.String())
		}
		if tt.code == 0 && stderr.Len() != 0 {
			t.Fatal("successful empty output has diagnostics")
		}
		if tt.code != 0 {
			var r conformance.Result
			if err := json.Unmarshal(stderr.Bytes(), &r); err != nil || r.Status != tt.status {
				t.Fatalf("diagnostic outcome: %+v %v", r, err)
			}
		}
		stdout.Reset()
		stderr.Reset()
		code := run([]string{"--json"}, bytes.NewBufferString(tt.input), &stdout, &stderr)
		var r conformance.Result
		if err := json.Unmarshal(stdout.Bytes(), &r); code != 0 || err != nil || r.Status != tt.status || r.HTML != tt.html || stderr.Len() != 0 {
			t.Fatalf("machine protocol: %d %+v %v %q", code, r, err, stderr.String())
		}
	}
}
