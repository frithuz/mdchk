package rules

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"math"
	"os"
	"sync"
	"testing"
)

func md081Settings() engine.Settings {
	s := engine.Settings{}
	off := false
	for _, m := range engine.Catalog() {
		s[m.ID] = engine.RuleSetting{Enabled: &off}
	}
	on := true
	s["MD081"] = engine.RuleSetting{Enabled: &on}
	return s
}

func TestMD081FrozenAnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md081.json")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != "501e217a9bb3657c605c47964b988ffdcb8cf50e916f5d721af8e9da58410ed5" {
		t.Fatal("root oracle identity")
	}
	var oracle struct {
		Cases []struct {
			ID, Source           string
			SHA                  string `json:"source_sha256"`
			Bytes                int    `json:"source_bytes"`
			Flavors              []string
			Frontmatter, Enabled *bool
			Allow                *bool    `json:"allow_directives"`
			Directives           *bool    `json:"directives_enabled"`
			Ratio                *float64 `json:"maximum_ratio"`
			Minimum              *int     `json:"minimum_characters"`
			Witnesses            []struct {
				Total, Emphasized int
				Ratio             *float64 `json:"binary64_ratio"`
				Active            bool     `json:"active_at_range_start"`
				Report            bool
			} `json:"paragraph_witnesses"`
			Findings []struct {
				Bytes, Start, End [2]int
				Text, Message     string
				Total, Emphasized int
			}
		}
	}
	if err = json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}
	sources := map[string]bool{}
	executions, ranges, witnesses := 0, 0, 0
	for _, f := range oracle.Cases {
		sources[f.Source] = true
		ranges += len(f.Findings)
		witnesses += len(f.Witnesses)
		for _, flavor := range f.Flavors {
			executions++
			t.Run(f.ID+"/"+flavor, func(t *testing.T) {
				if len(f.Source) != f.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source))) != f.SHA {
					t.Fatal("literal source identity")
				}
				minimum, ratio := 20, .5
				if f.Minimum != nil {
					minimum = *f.Minimum
				}
				if f.Ratio != nil {
					ratio = *f.Ratio
				}
				reported := 0
				for _, w := range f.Witnesses {
					if w.Total == 0 {
						if w.Ratio != nil {
							t.Fatal(w)
						}
					} else {
						if w.Ratio == nil || *w.Ratio != float64(w.Emphasized)/float64(w.Total) {
							t.Fatal(w)
						}
					}
					qualifies := w.Active && w.Total > 0 && w.Total >= minimum && float64(w.Emphasized)/float64(w.Total) > ratio
					if qualifies != w.Report {
						t.Fatal(w)
					}
					if w.Report {
						reported++
					}
				}
				if reported != len(f.Findings) {
					t.Fatal("cardinality")
				}
				s := md081Settings()
				setting := s["MD081"]
				setting.Enabled = f.Enabled
				setting.AllowDirectives = f.Allow
				setting.Options = engine.Options{}
				if f.Minimum != nil {
					setting.Options["minimum_characters"] = *f.Minimum
				}
				if f.Ratio != nil {
					setting.Options["maximum_ratio"] = *f.Ratio
				}
				s["MD081"] = setting
				dc := engine.DocumentContext{Flavor: flavor}
				if f.Frontmatter != nil {
					dc.Frontmatter, err = parser.NewFrontmatterOptions(*f.Frontmatter, nil)
					if err != nil {
						t.Fatal(err)
					}
				}
				r := registry(t, Registrations())
				selection, e := r.SelectRules(s, engine.ValidationProviders{})
				if e != nil {
					t.Fatal(e)
				}
				denied := map[string]bool{}
				if f.Directives != nil && !*f.Directives {
					denied["MD081"] = true
				}
				plan, e := selection.Plan(dc, denied)
				if e != nil {
					t.Fatal(e)
				}
				original := []byte(f.Source)
				in, e := plan.NewInput("literal.md", original)
				if e != nil {
					t.Fatal(e)
				}
				out, e := plan.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
				if e != nil || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(f.Findings) || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 || string(original) != f.Source || string(in.Source().Bytes()) != f.Source {
					t.Fatal(out, e)
				}
				for i, d := range out.Diagnostics {
					w := f.Findings[i]
					if d.Rule != "MD081" || d.Message != "Paragraph contains excessive bold or italic text." || d.Message != w.Message || [2]int{d.ByteRange.Start, d.ByteRange.End} != w.Bytes || d.Range.Start != (result.Position{Line: w.Start[0], Column: w.Start[1]}) || d.Range.End != (result.Position{Line: w.End[0], Column: w.End[1]}) || f.Source[w.Bytes[0]:w.Bytes[1]] != w.Text || d.Snapshot.SHA256 != f.SHA || d.Snapshot.SizeBytes != f.Bytes || d.Snapshot.View != "original" || d.FixAvailable {
						t.Fatal(d, w)
					}
				}
			})
		}
	}
	if len(oracle.Cases) != 107 || len(sources) != 88 || witnesses != 108 || ranges != 66 || executions != 206 {
		t.Fatal(len(oracle.Cases), len(sources), witnesses, ranges, executions)
	}
}

func TestMD081ActualAvailabilityAndTypedOptions(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD081") {
		t.Fatal("unavailable")
	}
	states, e := r.ResolveSettings(nil, engine.ValidationProviders{})
	if e != nil {
		t.Fatal(e)
	}
	m := states["MD081"]
	if m.Enabled || !m.Available || m.Options["maximum_ratio"] != .5 || m.Options["minimum_characters"] != 20 {
		t.Fatal(m)
	}
	for _, ratio := range []any{0, 1, 0.0, 1.0, math.Copysign(0, -1), math.Nextafter(1.0/3, 0), 1.0 / 3, math.Nextafter(1.0/3, 1)} {
		s := md081Settings()
		on := true
		s["MD081"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"maximum_ratio": ratio, "minimum_characters": 1}}
		in, e := engine.NewInput("typed.md", []byte("**a** bc"), s, engine.DocumentContext{Flavor: "gfm"})
		if e != nil {
			t.Fatal(e)
		}
		out, e := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
		n := 0.
		switch v := ratio.(type) {
		case int:
			n = float64(v)
		case float64:
			n = v
		}
		want := 0
		if 1.0/3 > n {
			want = 1
		}
		if e != nil || !out.Complete || len(out.Diagnostics) != want {
			t.Fatal(ratio, out, e)
		}
	}
	for _, option := range []string{"maximum_ratio", "minimum_characters"} {
		values := []any{nil, true, "1", int64(1), float32(.5), math.NaN(), math.Inf(1), math.Inf(-1), -1}
		if option == "minimum_characters" {
			values = append(values, 0, 20.0)
		} else {
			values = append(values, 1.0001)
		}
		for _, v := range values {
			s := md081Settings()
			off := false
			s["MD081"] = engine.RuleSetting{Enabled: &off, Options: engine.Options{option: v}}
			if _, e := r.ResolveSettings(s, engine.ValidationProviders{}); e == nil {
				t.Fatal(option, v)
			}
		}
	}
	for _, mapping := range []map[string]any{nil, {"enabled": nil}, {"enabled": "false"}, {"allow_directives": 0}} {
		if _, e := engine.SettingFromMapping(mapping); e == nil {
			t.Fatal(mapping)
		}
	}
	s := md081Settings()
	s["MD081"] = engine.RuleSetting{Options: engine.Options{"unknown": 1}}
	if _, e := r.ResolveSettings(s, engine.ValidationProviders{}); e == nil {
		t.Fatal("unknown disabled option")
	}
}
func TestMD081IsolationAndUncertainSyntax(t *testing.T) {
	r := registry(t, Registrations())
	s := md081Settings()
	on := true
	s["MD081"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"minimum_characters": 1}}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			in, e := engine.NewInput("one.md", []byte("**ab** c"), s, engine.DocumentContext{Flavor: "gfm"})
			if e != nil {
				t.Error(e)
				return
			}
			out, e := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
			if e != nil || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange.End != 8 {
				t.Error(out, e)
			}
		})
	}
	wg.Wait()
	for _, source := range []string{"\xff", "---\na: x"} {
		fm, _ := parser.NewFrontmatterOptions(true, nil)
		in, e := engine.NewInput("unknown.md", []byte(source), s, engine.DocumentContext{Flavor: "gfm", Frontmatter: fm})
		if e != nil {
			t.Fatal(e)
		}
		out, e := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
		if e != nil || out.Complete || len(out.Diagnostics) != 0 || len(out.Coverage) == 0 {
			t.Fatal(out, e)
		}
	}
	for _, source := range []string{"**ab** c", "ordinary", "**cd** e"} {
		in, e := engine.NewInput(source+".md", []byte(source), s, engine.DocumentContext{Flavor: "commonmark"})
		if e != nil {
			t.Fatal(e)
		}
		out, e := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
		want := 1
		if source == "ordinary" {
			want = 0
		}
		if e != nil || !out.Complete || len(out.Diagnostics) != want {
			t.Fatal(out, e)
		}
	}
}

func TestMD081MetadataAndLastSuppressionOwner(t *testing.T) {
	r := registry(t, Registrations())
	on, off := 0, 0
	for _, m := range r.Metadata() {
		if m.DefaultEnabled {
			on++
		} else {
			off++
		}
		if m.ID == "MD081" && (m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.NoFix) {
			t.Fatal(m)
		}
	}
	if on != 59 || off != 28 {
		t.Fatal(on, off)
	}
	source := "<!-- mdchk:disable MD081 -->\n<!-- mdchk:disable MD081 -->\n**ab** c"
	out := analyzeHeadings(t, r, source, md081BoundarySettings(1), engine.DocumentContext{Flavor: "gfm"}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 0 || len(out.SuppressedDiagnostics) != 1 {
		t.Fatal(out)
	}
	found := false
	for _, a := range out.AnalysisRecords() {
		state, ok := a.(engine.DirectiveAnalysis)
		if !ok {
			continue
		}
		suppressed := state.Suppressed()
		if len(suppressed) != 1 || suppressed[0].Owner != 1 || suppressed[0].Token.Start() != 48 || suppressed[0].Diagnostic.Rule != "MD081" {
			t.Fatal(suppressed)
		}
		found = true
	}
	if !found {
		t.Fatal("missing authoritative suppression")
	}
}
