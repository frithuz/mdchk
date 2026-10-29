package rules

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func md049Settings(style string) engine.Settings {
	s := md037Settings()
	off := false
	s["MD037"] = engine.RuleSetting{Enabled: &off}
	s["MD049"] = engine.RuleSetting{Options: engine.Options{"style": style}}
	return s
}

func TestMD049FrozenAnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md049.json")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != "5faab73b3e65c2dbd6c967058e7aa8666a8f12ed5d787007c666514cf9fef1b6" {
		t.Fatal("root oracle identity")
	}
	var fixtures struct {
		Cases []struct {
			ID, Source, Style string
			SHA               string `json:"source_sha256"`
			Bytes             int    `json:"source_bytes"`
			Flavors           []string
			Frontmatter       *bool
			Enabled           *bool
			Allow             *bool `json:"allow_directives"`
			Directives        *bool `json:"directives_enabled"`
			Findings          []struct {
				Bytes, Start, End [2]int
				Text, Message     string
			}
		}
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures.Cases) != 64 {
		t.Fatal(len(fixtures.Cases))
	}
	executions := 0
	for _, f := range fixtures.Cases {
		for _, flavor := range f.Flavors {
			executions++
			t.Run(f.ID+"/"+flavor, func(t *testing.T) {
				if len(f.Source) != f.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source))) != f.SHA {
					t.Fatal("literal identity")
				}
				s := md049Settings(f.Style)
				setting := s["MD049"]
				setting.Enabled = f.Enabled
				setting.AllowDirectives = f.Allow
				s["MD049"] = setting
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
					denied["MD049"] = true
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
					if d.Rule != "MD049" || d.Message != w.Message || [2]int{d.ByteRange.Start, d.ByteRange.End} != w.Bytes || d.Range.Start != (result.Position{Line: w.Start[0], Column: w.Start[1]}) || d.Range.End != (result.Position{Line: w.End[0], Column: w.End[1]}) || f.Source[w.Bytes[0]:w.Bytes[1]] != w.Text || d.Snapshot.SHA256 != f.SHA || d.Snapshot.SizeBytes != len(f.Source) || d.Snapshot.View != "original" || d.FixAvailable {
						t.Fatal(d, w)
					}
				}
			})
		}
	}
	if executions != 123 {
		t.Fatal(executions)
	}
}

func TestMD049OptionsAvailabilityAndDefaults(t *testing.T) {
	r := registry(t, Registrations())
	on, off := 0, 0
	executable := false
	for _, registration := range Registrations() {
		executable = executable || registration.ID == "MD049" && registration.Check != nil
	}
	if !executable {
		t.Fatal("MD049 executable registration missing")
	}
	for _, m := range r.Metadata() {
		if m.DefaultEnabled {
			on++
		} else {
			off++
		}
		if m.ID == "MD049" && (!m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.PlannedFix) {
			t.Fatal(m)
		}
	}
	if on != 59 || off != 28 || !r.Available("MD049") {
		t.Fatal(on, off)
	}
	for _, enabled := range []bool{false, true} {
		for _, options := range []engine.Options{{"style": nil}, {"style": true}, {"style": "Asterisk"}, {"style": ""}, {"style": "dash"}, {"style": []string{"asterisk"}}, {"extra": true}, {"style": "asterisk", "extra": true}} {
			if _, err := r.ResolveSettings(engine.Settings{"MD049": {Enabled: &enabled, Options: options}}, engine.ValidationProviders{}); err == nil {
				t.Fatal(options)
			}
		}
	}
	s := md049Settings("consistent")
	s["MD049"] = engine.RuleSetting{Options: engine.Options{}}
	out := analyzeHeadings(t, r, "*a* _b_", s, engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange.Start != 4 {
		t.Fatal(out)
	}
}

func TestMD049DocumentIsolationBothOrdersAndConcurrent(t *testing.T) {
	r := registry(t, Registrations())
	for _, reverse := range []bool{false, true} {
		a, b := "*one* and _two_", "_one_ and *two*"
		if reverse {
			a, b = b, a
		}
		in1, err := engine.NewInput("a.md", []byte(a), md049Settings("consistent"), engine.DocumentContext{})
		if err != nil {
			t.Fatal(err)
		}
		in2, err := engine.NewInput("b.md", []byte(b), md049Settings("consistent"), engine.DocumentContext{})
		if err != nil {
			t.Fatal(err)
		}
		out, err := r.Lint(context.Background(), []engine.Input{in2, in1}, engine.RunOptions{})
		if err != nil || !out.Complete || len(out.Diagnostics) != 2 {
			t.Fatal(out, err)
		}
		for i, d := range out.Diagnostics {
			if d.Path != []string{"a.md", "b.md"}[i] || d.ByteRange.Start != 10 || d.ByteRange.End != 11 {
				t.Fatal(d)
			}
		}
	}
	var wg sync.WaitGroup
	for _, source := range []string{"*one* and _two_", "_one_ and *two*"} {
		wg.Go(func() {
			out := analyzeHeadings(t, r, source, md049Settings("consistent"), engine.DocumentContext{}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange.Start != 10 {
				t.Error(out)
			}
		})
	}
	wg.Wait()
}

func TestMD049GenuineEmphasisTransformedChildren(t *testing.T) {
	for _, source := range []string{"_a\x00b_ *c*", "_a\tb_ *c*", "_a \\* b_ *c*", "_a &amp; b_ *c*"} {
		// Independent anchor: final literal asterisk is three original bytes before EOF.
		for _, flavor := range []string{"commonmark", "gfm"} {
			out := analyzeHeadings(t, registry(t, Registrations()), source, md049Settings("consistent"), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange.Start != len(source)-3 {
				t.Fatal(source, out)
			}
		}
	}
}
