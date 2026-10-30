package rules

import (
	"context"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"sync"
	"testing"
)

func TestMD065AvailabilityDefaultsAndStrictOptions(t *testing.T) {
	r := registry(t, Registrations())
	on, off := 0, 0
	executable := false
	for _, registration := range Registrations() {
		executable = executable || registration.ID == "MD065" && registration.Check != nil
	}
	for _, m := range r.Metadata() {
		if m.DefaultEnabled {
			on++
		} else {
			off++
		}
		if m.ID == "MD065" && (!m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.PlannedFix) {
			t.Fatal(m)
		}
	}
	if !executable || !r.Available("MD065") || on != 59 || off != 28 {
		t.Fatal(executable, on, off)
	}
	for _, enabled := range []bool{false, true} {
		for _, name := range []string{"before", "after", "style", "minimum", "extra"} {
			for _, value := range []any{nil, true, 0, "", []string{}} {
				if _, e := r.ResolveSettings(engine.Settings{"MD065": {Enabled: &enabled, Options: engine.Options{name: value}}}, engine.ValidationProviders{}); e == nil {
					t.Fatal(enabled, name, value)
				}
			}
		}
	}
	for _, empty := range []bool{false, true} {
		s := md065Settings()
		if empty {
			s["MD065"] = engine.RuleSetting{Options: engine.Options{}}
		}
		out := analyzeHeadings(t, r, "Text\n***\nMore", s, engine.DocumentContext{}, engine.RunOptions{})
		if !out.Complete || len(out.Diagnostics) != 2 {
			t.Fatal(out)
		}
	}
}

func TestMD065UnreliableSyntaxAndInvalidUTF8(t *testing.T) {
	r := registry(t, Registrations())
	fm, e := parser.NewFrontmatterOptions(true, nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		source string
		dc     engine.DocumentContext
	}{
		{"---\n***\nMore", engine.DocumentContext{Frontmatter: fm}},
		{"Text\n***\n\xff", engine.DocumentContext{}},
	} {
		out := analyzeHeadings(t, r, tc.source, md065Settings(), tc.dc, engine.RunOptions{})
		if out.Complete || len(out.Diagnostics) != 0 {
			t.Fatal(out)
		}
		if tc.source[len(tc.source)-1] == 0xff && len(out.Errors) != 1 {
			t.Fatal(out)
		}
	}
}

func TestMD065ClosingTailsAndImageIsolation(t *testing.T) {
	// The HTML block closes on its second line; its inline tail opens a new raw
	// lifetime, which the subsequent inline closer must finish before the target.
	for _, source := range []string{
		"<script>\n</script>a <style>\n\nText\n***\nMore",
		"a <script>\n\n***\n\n</script>\n\nText\n***\nMore",
		"![<script>](x)\n\nText\n***\nMore",
	} {
		for _, flavor := range []string{"commonmark", "gfm"} {
			out := analyzeHeadings(t, registry(t, Registrations()), source, md065Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			want := 2
			if source[0] == '<' {
				want = 0
			}
			if !out.Complete || len(out.Diagnostics) != want {
				t.Fatal(source, out)
			}
		}
	}
}

func TestMD065IsolationBothOrdersAndConcurrent(t *testing.T) {
	r := registry(t, Registrations())
	for _, reverse := range []bool{false, true} {
		sources := []string{"Text\n***\nMore", "a <script>\n\n***\nMore"}
		if reverse {
			sources[0], sources[1] = sources[1], sources[0]
		}
		var inputs []engine.Input
		for i, source := range sources {
			in, e := engine.NewInput([]string{"a.md", "b.md"}[i], []byte(source), md065Settings(), engine.DocumentContext{})
			if e != nil {
				t.Fatal(e)
			}
			inputs = append(inputs, in)
		}
		out, e := r.Lint(context.Background(), inputs, engine.RunOptions{})
		if e != nil || !out.Complete || len(out.Diagnostics) != 2 {
			t.Fatal(out, e)
		}
		wantPath := "a.md"
		if reverse {
			wantPath = "b.md"
		}
		for _, d := range out.Diagnostics {
			if d.Path != wantPath {
				t.Fatal(d)
			}
		}
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			out := analyzeHeadings(t, r, "Text\n***\nMore", md065Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != 2 {
				t.Error(out)
			}
		})
	}
	wg.Wait()
}
