package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

type md094FixtureCase struct {
	Name      string `json:"name"`
	SourceHex string `json:"source_hex"`
	Expected  []struct {
		ByteOffset int    `json:"byte_offset"`
		ByteEnd    int    `json:"byte_end"`
		Line       int    `json:"line"`
		Column     int    `json:"column"`
		BytesHex   string `json:"bytes_hex"`
	} `json:"expected"`
}

// The fixture bytes are hex because JSON strings cannot hold invalid UTF-8.
// Each case runs with MD094 attribution on and off; only Rule may differ.
func TestMD094Fixtures(t *testing.T) {
	data, err := os.ReadFile("../rules/testdata/md094.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []md094FixtureCase `json:"cases"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&fixture); err != nil || len(fixture.Cases) == 0 {
		t.Fatal(err, len(fixture.Cases))
	}
	for _, tc := range fixture.Cases {
		raw, err := hex.DecodeString(tc.SourceHex)
		if err != nil {
			t.Fatal(tc.Name, err)
		}
		for _, enabled := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/enabled=%v", tc.Name, enabled), func(t *testing.T) {
				original := bytes.Clone(raw)
				got := ValidateEncoding("doc.md", parser.NewSource(raw), enabled)
				if !bytes.Equal(raw, original) {
					t.Fatal("source bytes changed")
				}
				if got.Snapshot.SizeBytes != len(raw) || got.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256(raw)) {
					t.Fatalf("snapshot=%+v", got.Snapshot)
				}
				if len(got.Errors) != len(tc.Expected) || got.Omission != nil {
					t.Fatalf("errors=%+v omission=%+v", got.Errors, got.Omission)
				}
				wantRule := ""
				if enabled {
					wantRule = "MD094"
				}
				for i, want := range tc.Expected {
					e := got.Errors[i]
					point := parser.Position{Line: want.Line, Column: want.Column}
					if e.Category != result.IO || e.Code != "invalid-utf8" || e.Scope != "operation" || e.Path != "doc.md" || e.Rule != wantRule {
						t.Fatalf("error=%+v", e)
					}
					if e.ByteOffset == nil || *e.ByteOffset != want.ByteOffset || e.ByteRange == nil || *e.ByteRange != (result.ByteRange{Start: want.ByteOffset, End: want.ByteEnd}) || e.Range == nil || e.Range.Start != point || e.Range.End != point {
						t.Fatalf("location offset=%v span=%+v range=%+v", e.ByteOffset, e.ByteRange, e.Range)
					}
					if hex.EncodeToString(raw[want.ByteOffset:want.ByteEnd]) != want.BytesHex {
						t.Fatalf("fixture bytes %q at %d..%d", want.BytesHex, want.ByteOffset, want.ByteEnd)
					}
					if e.Snapshot == nil || *e.Snapshot != got.Snapshot {
						t.Fatalf("error snapshot=%+v", e.Snapshot)
					}
				}
			})
		}
	}
}
