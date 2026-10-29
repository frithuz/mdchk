package rules

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

func md044Settings(names []string, code bool) engine.Settings {
	s := md038Settings()
	off, on := false, true
	s["MD038"] = engine.RuleSetting{Enabled: &off}
	s["MD044"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"names": names, "code_blocks": code}}
	return s
}

func TestMD044FrozenAnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md044.json")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != "d5254705d0132a6a40587e1ab958bf2e98a6a8eb8b50012214a6b873696da7e0" {
		t.Fatal("root oracle identity")
	}
	var fixtures struct {
		Cases []struct {
			ID, Source     string
			Names, Flavors []string
			Code           bool  `json:"code_blocks"`
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
	if len(fixtures.Cases) != 67 {
		t.Fatal(len(fixtures.Cases))
	}
	for _, f := range fixtures.Cases {
		for _, flavor := range f.Flavors {
			t.Run(f.ID+"/"+flavor, func(t *testing.T) {
				settings := md044Settings(f.Names, f.Code)
				rule := settings["MD044"]
				rule.AllowDirectives = f.Allow
				settings["MD044"] = rule
				out := analyzeHeadings(t, registry(t, Registrations()), f.Source, settings, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(f.Findings) || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(out)
				}
				hash := fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source)))
				for i, d := range out.Diagnostics {
					w := f.Findings[i]
					if d.Rule != "MD044" || d.Message != w.Message || [2]int{d.ByteRange.Start, d.ByteRange.End} != w.Bytes || d.Range.Start != (result.Position{Line: w.Start[0], Column: w.Start[1]}) || d.Range.End != (result.Position{Line: w.End[0], Column: w.End[1]}) || f.Source[w.Bytes[0]:w.Bytes[1]] != w.Text || d.Snapshot.SHA256 != hash || d.Snapshot.SizeBytes != len(f.Source) || d.Snapshot.View != "original" || d.FixAvailable {
						t.Fatal(d, w)
					}
				}
			})
		}
	}
}
