package rules

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"os"
	"sync"
	"testing"
)

func md088Settings() engine.Settings {
	s := engine.Settings{}
	off, on := false, true
	for _, m := range engine.Catalog() {
		s[m.ID] = engine.RuleSetting{Enabled: &off}
	}
	s["MD088"] = engine.RuleSetting{Enabled: &on}
	return s
}
func TestMD088FrozenAnalysisFixtures(t *testing.T) {
	data, e := os.ReadFile("testdata/md088.json")
	if e != nil {
		t.Fatal(e)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != "6ef31b6c678d10d662ea1da66c3f65cad236223adf2c5787b37bddba5f533060" {
		t.Fatal("root oracle identity")
	}
	var oracle struct {
		Cases []struct {
			ID, Source           string
			SHA                  string `json:"source_sha256"`
			Bytes                int    `json:"source_bytes"`
			Flavors              []string
			Frontmatter, Enabled *bool
			Allow                *bool `json:"allow_directives"`
			Directives           *bool `json:"directives_enabled"`
			Style                *string
			Complete             bool   `json:"analysis_complete"`
			Shape                string `json:"setting_shape"`
			Candidates           []struct {
				Bytes, Start, End [2]int
				Text, Message     string
				Active            bool `json:"active_at_range_start"`
			}
			Findings []struct {
				Bytes, Start, End [2]int
				Text, Message     string
			}
		}
	}
	if e = json.Unmarshal(data, &oracle); e != nil {
		t.Fatal(e)
	}
	sources := map[string]bool{}
	executions, ranges, candidates := 0, 0, 0
	for _, f := range oracle.Cases {
		sources[f.Source] = true
		ranges += len(f.Findings)
		candidates += len(f.Candidates)
		for _, flavor := range f.Flavors {
			executions++
			t.Run(f.ID+"/"+flavor, func(t *testing.T) {
				if len(f.Source) != f.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source))) != f.SHA {
					t.Fatal("literal identity")
				}
				s := md088Settings()
				setting := s["MD088"]
				setting.Enabled = f.Enabled
				setting.AllowDirectives = f.Allow
				if f.Style != nil {
					setting.Options = engine.Options{"style": *f.Style}
				}
				s["MD088"] = setting
				if f.ID == "default-omitted" {
					delete(s, "MD088")
				}
				dc := engine.DocumentContext{Flavor: flavor}
				if f.Frontmatter != nil {
					dc.Frontmatter, e = parser.NewFrontmatterOptions(*f.Frontmatter, nil)
					if e != nil {
						t.Fatal(e)
					}
				}
				r := registry(t, Registrations())
				sel, e := r.SelectRules(s, engine.ValidationProviders{})
				if e != nil {
					t.Fatal(e)
				}
				denied := map[string]bool{}
				if f.Directives != nil && !*f.Directives {
					denied["MD088"] = true
				}
				plan, e := sel.Plan(dc, denied)
				if e != nil {
					t.Fatal(e)
				}
				original := []byte(f.Source)
				in, e := plan.NewInput("literal.md", original)
				if e != nil {
					t.Fatal(e)
				}
				out, e := plan.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
				if e != nil || out.Complete != f.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(f.Findings) || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 || string(original) != f.Source || string(in.Source().Bytes()) != f.Source {
					t.Fatal(out, e)
				}
				if !f.Complete && len(out.Coverage) == 0 {
					t.Fatal("unterminated header lacks coverage")
				}
				for i, d := range out.Diagnostics {
					w := f.Findings[i]
					if d.Rule != "MD088" || d.Message != "Use ASCII quotes and dashes in prose." || d.Message != w.Message || [2]int{d.ByteRange.Start, d.ByteRange.End} != w.Bytes || d.Range.Start != (result.Position{Line: w.Start[0], Column: w.Start[1]}) || d.Range.End != (result.Position{Line: w.End[0], Column: w.End[1]}) || f.Source[w.Bytes[0]:w.Bytes[1]] != w.Text || d.Snapshot.SHA256 != f.SHA || d.Snapshot.SizeBytes != f.Bytes || d.Snapshot.View != "original" || d.FixAvailable {
						t.Fatal(d, w)
					}
				}
			})
		}
	}
	if len(oracle.Cases) != 93 || len(sources) != 84 || candidates != 210 || ranges != 182 || executions != 181 {
		t.Fatal(len(oracle.Cases), len(sources), candidates, ranges, executions)
	}
}
func TestMD088AvailabilityAndStrictOptions(t *testing.T) {
	r := registry(t, Registrations())
	states, e := r.ResolveSettings(nil, engine.ValidationProviders{})
	if e != nil {
		t.Fatal(e)
	}
	m := states["MD088"]
	if !r.Available("MD088") || m.Enabled || !m.Available || m.Options["style"] != "ascii" {
		t.Fatal(m)
	}
	for _, metadata := range r.Metadata() {
		if metadata.ID == "MD088" && (metadata.DefaultEnabled || !metadata.RequiresSyntax || metadata.UsesFrontmatter || metadata.Fix != engine.PlannedFix) {
			t.Fatal(metadata)
		}
	}
	for _, v := range []any{nil, true, 1, "", "ASCII", "Ascii", []string{"ascii"}} {
		s := md088Settings()
		off := false
		s["MD088"] = engine.RuleSetting{Enabled: &off, Options: engine.Options{"style": v}}
		if _, e := r.ResolveSettings(s, engine.ValidationProviders{}); e == nil {
			t.Fatal(v)
		}
	}
	for _, key := range []string{"replacements", "unknown"} {
		s := md088Settings()
		off := false
		s["MD088"] = engine.RuleSetting{Enabled: &off, Options: engine.Options{key: "ascii"}}
		if _, e := r.ResolveSettings(s, engine.ValidationProviders{}); e == nil {
			t.Fatal(key)
		}
	}
}
func TestMD088ConcurrentIsolation(t *testing.T) {
	r := registry(t, Registrations())
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for _, source := range []string{"“a”—", "clean", "![<script>](x) —"} {
				in, e := engine.NewInput("one.md", []byte(source), md088Settings(), engine.DocumentContext{Flavor: "gfm"})
				if e != nil {
					t.Error(e)
					return
				}
				out, e := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
				want := 3
				if source == "clean" {
					want = 0
				} else if source == "![<script>](x) —" {
					want = 1
				}
				if e != nil || !out.Complete || len(out.Diagnostics) != want {
					t.Error(out, e)
				}
			}
		})
	}
	wg.Wait()
}
