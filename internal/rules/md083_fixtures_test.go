package rules

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func md083Settings(sequences []string) engine.Settings {
	s := md038Settings()
	off, on := false, true
	s["MD038"] = engine.RuleSetting{Enabled: &off}
	s["MD083"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"sequences": sequences}}
	return s
}

func TestMD083FrozenAnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md083.json")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != "68907c5e98b79f9c284283860890f2df63669e5a7094bdde158bb962c88f2f45" {
		t.Fatal("root oracle identity")
	}
	var fixtures struct {
		Cases []struct {
			ID, Source         string
			Sequences, Flavors []string
			Frontmatter        bool  `json:"frontmatter"`
			Allow              *bool `json:"allow_directives"`
			Findings           []struct {
				Bytes, Start, End [2]int
				Text, Message     string
			}
		}
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures.Cases) != 57 {
		t.Fatal(len(fixtures.Cases))
	}
	fm, err := parser.NewFrontmatterOptions(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	flavors := map[string]bool{}
	for _, f := range fixtures.Cases {
		for _, flavor := range f.Flavors {
			flavors[flavor] = true
			t.Run(f.ID+"/"+flavor, func(t *testing.T) {
				settings := md083Settings(f.Sequences)
				rule := settings["MD083"]
				rule.AllowDirectives = f.Allow
				settings["MD083"] = rule
				dc := engine.DocumentContext{Flavor: flavor}
				if f.Frontmatter {
					dc.Frontmatter = fm
				}
				out := analyzeHeadings(t, registry(t, Registrations()), f.Source, settings, dc, engine.RunOptions{})
				if !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(f.Findings) || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(out)
				}
				hash := fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source)))
				for i, d := range out.Diagnostics {
					w := f.Findings[i]
					if d.Rule != "MD083" || d.Message != w.Message || [2]int{d.ByteRange.Start, d.ByteRange.End} != w.Bytes || d.Range.Start != (result.Position{Line: w.Start[0], Column: w.Start[1]}) || d.Range.End != (result.Position{Line: w.End[0], Column: w.End[1]}) || f.Source[w.Bytes[0]:w.Bytes[1]] != w.Text || d.Snapshot.SHA256 != hash || d.Snapshot.SizeBytes != len(f.Source) || d.Snapshot.View != "original" || d.FixAvailable {
						t.Fatal(d, w)
					}
				}
			})
		}
	}
	if !flavors["commonmark"] || !flavors["gfm"] {
		t.Fatal(flavors)
	}
}
