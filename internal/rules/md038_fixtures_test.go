package rules

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
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
				out := analyzeHeadings(t, registry(t, Registrations()), f.Source, md038Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(f.Ranges) || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(out)
				}
				for i, d := range out.Diagnostics {
					w := f.Ranges[i]
					if [2]int{d.ByteRange.Start, d.ByteRange.End} != w || d.Range.Start != md038OriginalPosition(f.Source, w[0]) || d.Range.End != md038OriginalPosition(f.Source, w[1]) || d.Rule != "MD038" || d.Message != "Whitespace within inline code delimiters." || d.FixAvailable || d.Snapshot.View != "original" || d.Snapshot.SHA256 != f.SHA256 || d.Snapshot.SizeBytes != len(f.Source) {
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

// Independent scalar/physical-ending count over original bytes, never parser
// normalized Literal or the product's line-index conversion.
func md038OriginalPosition(source string, at int) result.Position {
	p := result.Position{Line: 1, Column: 1}
	for i := 0; i < at; {
		if source[i] == '\r' {
			p.Line++
			p.Column = 1
			i++
			if i < at && source[i] == '\n' {
				i++
			}
			continue
		}
		if source[i] == '\n' {
			p.Line++
			p.Column = 1
			i++
			continue
		}
		_, w := utf8.DecodeRuneInString(source[i:])
		i += w
		p.Column++
	}
	return p
}
