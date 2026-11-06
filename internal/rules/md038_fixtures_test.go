package rules

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

func TestMD038FrozenAnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md038.json")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != "44f2489472ee9aa0aa0bd1063878e632794bdba705c0b173aaafcff549f1b455" {
		t.Fatal("frozen oracle identity")
	}
	var fixtures struct {
		Count        int
		UniqueRanges int `json:"unique_ranges"`
		Cases        []struct {
			Name, Source, SHA256 string
			Ranges               [][2]int
		}
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if fixtures.Count != 60 || len(fixtures.Cases) != 60 || fixtures.UniqueRanges != 61 {
		t.Fatal("frozen case counts")
	}
	ranges := 0
	for _, f := range fixtures.Cases {
		ranges += len(f.Ranges)
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(f.Name+"/"+flavor, func(t *testing.T) {
				if fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source))) != f.SHA256 {
					t.Fatal("source identity")
				}
				out := analyzeHeadings(t, registry(t, Registrations()), f.Source, onlyDefaultMD038Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(f.Ranges) || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(out)
				}
				for i, d := range out.Diagnostics {
					w := f.Ranges[i]
					if [2]int{d.ByteRange.Start, d.ByteRange.End} != w || d.Range.Start != originalPosition(f.Source, w[0]) || d.Range.End != originalPosition(f.Source, w[1]) || d.Rule != "MD038" || d.Message != "Whitespace within inline code delimiters." || d.FixAvailable || d.Snapshot.View != "original" || d.Snapshot.SHA256 != f.SHA256 || d.Snapshot.SizeBytes != len(f.Source) {
						t.Fatal(d, f)
					}
					for _, b := range []byte(f.Source[w[0]:w[1]]) {
						if b != ' ' {
							t.Fatal("non-original SPACE")
						}
					}
				}
			})
		}
	}
	if ranges != 61 {
		t.Fatal(ranges)
	}
}
