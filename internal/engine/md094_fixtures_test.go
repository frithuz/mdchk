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
	Expected  *struct {
		ByteOffset int `json:"byte_offset"`
		Line       int `json:"line"`
		Column     int `json:"column"`
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
				if tc.Expected == nil {
					if got.Error != nil {
						t.Fatalf("valid source rejected: %+v", got.Error)
					}
					return
				}
				e := got.Error
				if e == nil {
					t.Fatal("invalid encoding accepted")
				}
				wantRule := ""
				if enabled {
					wantRule = "MD094"
				}
				point := parser.Position{Line: tc.Expected.Line, Column: tc.Expected.Column}
				if e.Category != result.IO || e.Code != "invalid-utf8" || e.Scope != "operation" || e.Path != "doc.md" || e.Rule != wantRule {
					t.Fatalf("error=%+v", e)
				}
				if e.ByteOffset == nil || *e.ByteOffset != tc.Expected.ByteOffset || e.Range == nil || e.Range.Start != point || e.Range.End != point {
					t.Fatalf("location offset=%v range=%+v", e.ByteOffset, e.Range)
				}
				if e.Snapshot == nil || *e.Snapshot != got.Snapshot {
					t.Fatalf("error snapshot=%+v", e.Snapshot)
				}
			})
		}
	}
}
