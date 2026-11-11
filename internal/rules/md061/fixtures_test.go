package md061_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
)

func md061Settings(terms []string, caseSensitive bool) engine.Settings {
	s := onlyDefaultMD038Settings()
	off, on := false, true
	s["MD038"] = engine.RuleSetting{Enabled: &off}
	s["MD061"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"terms": terms, "case_sensitive": caseSensitive}}
	return s
}

func TestMD061FrozenAnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md061.json")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != "d2e0ad1036ba0bba1cae3c2d033924133fb5a6fecad169a76cb6700007724757" {
		t.Fatal("root oracle identity")
	}
	var fixtures struct {
		Cases []struct {
			ID, Source     string
			Terms, Flavors []string
			CaseSensitive  bool  `json:"case_sensitive"`
			Frontmatter    bool  `json:"frontmatter"`
			Allow          *bool `json:"allow_directives"`
			Findings       []struct {
				Bytes, Start, End [2]int
				Text, Message     string
			}
		}
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures.Cases) != 56 {
		t.Fatal(len(fixtures.Cases))
	}
	fm, err := parser.NewFrontmatterOptions(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures.Cases {
		for _, flavor := range f.Flavors {
			t.Run(f.ID+"/"+flavor, func(t *testing.T) {
				settings := md061Settings(f.Terms, f.CaseSensitive)
				rule := settings["MD061"]
				rule.AllowDirectives = f.Allow
				settings["MD061"] = rule
				dc := engine.DocumentContext{Flavor: flavor}
				if f.Frontmatter {
					dc.Frontmatter = fm
				}
				out := analyzeHeadings(t, registry(t, rules.Registrations()), f.Source, settings, dc, engine.RunOptions{})
				if !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(f.Findings) || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(out)
				}
				hash := fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source)))
				for i, d := range out.Diagnostics {
					w := f.Findings[i]
					if d.Rule != "MD061" || d.Message != w.Message || [2]int{d.ByteRange.Start, d.ByteRange.End} != w.Bytes || d.Range.Start != (result.Position{Line: w.Start[0], Column: w.Start[1]}) || d.Range.End != (result.Position{Line: w.End[0], Column: w.End[1]}) || f.Source[w.Bytes[0]:w.Bytes[1]] != w.Text || d.Snapshot.SHA256 != hash || d.Snapshot.SizeBytes != len(f.Source) || d.Snapshot.View != "original" || d.FixAvailable {
						t.Fatal(d, w)
					}
				}
			})
		}
	}
}
