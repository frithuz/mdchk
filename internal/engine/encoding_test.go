package engine

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func TestMandatoryEncodingGate(t *testing.T) {
	prefix := "<!-- markdownlint-disable -->\r\n雪\t"
	for _, suffix := range [][]byte{{0xff, 0xfe}, {0xe2, 0x82}, {0xf0, 0x28, 0x8c, 0xbc}} {
		raw := append([]byte(prefix), suffix...)
		for _, enabled := range []bool{false, true} {
			got := ValidateEncoding("bad.md", parser.NewSource(raw), enabled)
			if got.Error == nil {
				t.Fatal("invalid encoding accepted")
			}
			e := got.Error
			wantRule := ""
			if enabled {
				wantRule = "MD094"
			}
			if e.Category != result.IO || e.Code != "invalid-utf8" || e.Path != "bad.md" || e.Rule != wantRule || e.ByteOffset == nil || *e.ByteOffset != len(prefix) || e.Range == nil || e.Range.Start != (parser.Position{Line: 2, Column: 3}) || e.Range.Start != e.Range.End {
				t.Fatalf("error=%+v", e)
			}
			if got.Snapshot.SizeBytes != len(raw) || got.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256(raw)) || e.Snapshot == nil || *e.Snapshot != got.Snapshot {
				t.Fatalf("snapshot=%+v", got.Snapshot)
			}
			if got.Index.EncodingFailure() == nil {
				t.Fatal("index lost decoding failure")
			}
		}
	}
	for _, text := range []string{"", "\ufeff雪\r\ntext\rfinal\n", "literal \ufffd"} {
		got := ValidateEncoding("ok.md", parser.NewSource([]byte(text)), true)
		if got.Error != nil || got.Index.EncodingFailure() != nil {
			t.Fatalf("valid source rejected: %q", text)
		}
	}
}
