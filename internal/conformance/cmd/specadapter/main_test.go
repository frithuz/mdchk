package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
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
		{"![foo](url)", "rendered", "<p><img src=\"url\" alt=\"foo\" /></p>\n", 0}, {"\xff", "error", "", 1},
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

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }
func TestAcquisitionAndDeliveryBounds(t *testing.T) {
	var diagnostics bytes.Buffer
	if code := run(nil, brokenReader{}, io.Discard, &diagnostics); code != 1 {
		t.Fatal("read error", code)
	}
	diagnostics.Reset()
	if code := run(nil, strings.NewReader(strings.Repeat("a", (1<<20)+1)), io.Discard, &diagnostics); code != 1 {
		t.Fatal("oversized input", code)
	}
	for _, args := range [][]string{nil, {"--json"}} {
		diagnostics.Reset()
		if code := run(args, strings.NewReader("a"), shortWriter{}, &diagnostics); code != 1 {
			t.Fatal("short write", args, code)
		}
	}
}
