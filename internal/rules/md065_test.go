package rules

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func md065Settings() engine.Settings {
	s := md037Settings()
	off := false
	s["MD037"] = engine.RuleSetting{Enabled: &off}
	s["MD065"] = engine.RuleSetting{}
	return s
}

func TestMD065FrozenAnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md065.json")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != "c337a9cb0d793790beecea345baf60c90a72016a10f10c45bd12858f77b10dfa" {
		t.Fatal("root oracle identity")
	}
	var fixtures struct {
		Cases []struct {
			ID, Source  string
			SHA         string `json:"source_sha256"`
			Bytes       int    `json:"source_bytes"`
			Flavors     []string
			Frontmatter *bool
			Enabled     *bool
			Allow       *bool `json:"allow_directives"`
			Directives  *bool `json:"directives_enabled"`
			Findings    []struct {
				Bytes, Start, End [2]int
				Text, Message     string
				Side              string
				Line              int    `json:"authored_line"`
				Prefix            string `json:"authored_body_prefix"`
			}
		}
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures.Cases) != 80 {
		t.Fatal(len(fixtures.Cases))
	}
	executions := 0
	sources := map[string]bool{}
	ranges := 0
	for _, f := range fixtures.Cases {
		sources[f.Source] = true
		ranges += len(f.Findings)
		for _, flavor := range f.Flavors {
			executions++
			t.Run(f.ID+"/"+flavor, func(t *testing.T) {
				if len(f.Source) != f.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source))) != f.SHA {
					t.Fatal("literal identity")
				}
				s := md065Settings()
				setting := s["MD065"]
				setting.Enabled = f.Enabled
				setting.AllowDirectives = f.Allow
				s["MD065"] = setting
				dc := engine.DocumentContext{Flavor: flavor}
				if f.Frontmatter != nil {
					dc.Frontmatter, err = parser.NewFrontmatterOptions(*f.Frontmatter, nil)
					if err != nil {
						t.Fatal(err)
					}
				}
				r := registry(t, Registrations())
				selection, err := r.SelectRules(s, engine.ValidationProviders{})
				if err != nil {
					t.Fatal(err)
				}
				denied := map[string]bool{}
				if f.Directives != nil && !*f.Directives {
					denied["MD065"] = true
				}
				plan, err := selection.Plan(dc, denied)
				if err != nil {
					t.Fatal(err)
				}
				original := []byte(f.Source)
				in, err := plan.NewInput("literal.md", original)
				if err != nil {
					t.Fatal(err)
				}
				out, err := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(f.Findings) || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 || string(original) != f.Source || string(in.Source().Bytes()) != f.Source {
					t.Fatal(out)
				}
				for i, d := range out.Diagnostics {
					w := f.Findings[i]
					if w.Side == "before" {
						lineStart := strings.LastIndexAny(f.Source[:w.Bytes[0]], "\r\n") + 1
						if f.Source[lineStart:w.Bytes[0]] != w.Prefix {
							t.Fatal("authored prefix", w)
						}
					}
					if w.Bytes[0] != w.Bytes[1] || w.Text != "" || w.Line != w.Start[0] || (w.Side != "before" && w.Side != "after") || w.Message != "Horizontal rule requires a blank body line "+w.Side+" it." {
						t.Fatal("oracle witness", w)
					}
					if d.Rule != "MD065" || d.Message != w.Message || [2]int{d.ByteRange.Start, d.ByteRange.End} != w.Bytes || d.Range.Start != (result.Position{Line: w.Start[0], Column: w.Start[1]}) || d.Range.End != (result.Position{Line: w.End[0], Column: w.End[1]}) || f.Source[w.Bytes[0]:w.Bytes[1]] != w.Text || d.Snapshot.SHA256 != f.SHA || d.Snapshot.SizeBytes != len(f.Source) || d.Snapshot.View != "original" || d.FixAvailable {
						t.Fatal(d, w)
					}
				}
			})
		}
	}
	if len(sources) != 76 || ranges != 75 {
		t.Fatal(len(sources), ranges)
	}
	if executions != 159 {
		t.Fatal(executions)
	}
}
