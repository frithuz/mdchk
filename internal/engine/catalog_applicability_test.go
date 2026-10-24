package engine_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

func TestCatalogDialectApplicability(t *testing.T) {
	tables := map[string]bool{"MD055": true, "MD056": true, "MD058": true, "MD060": true, "MD075": true}
	footnotes := map[string]bool{"MD066": true, "MD067": true, "MD068": true}
	for _, flavor := range []string{"commonmark", "gfm", "", "unknown"} {
		t.Run(flavor, func(t *testing.T) {
			for _, m := range engine.Catalog() {
				want := !(tables[m.ID] && flavor == "commonmark" || footnotes[m.ID] && (flavor == "commonmark" || flavor == "gfm"))
				got := m.AppliesTo == nil || m.AppliesTo(engine.DocumentContext{Flavor: flavor})
				if got != want {
					t.Errorf("%s applicability = %v, want %v", m.ID, got, want)
				}
				// Optional project context cannot determine dialect applicability.
				withProject := m.AppliesTo == nil || m.AppliesTo(engine.DocumentContext{Flavor: flavor, Project: "project"})
				if withProject != got {
					t.Errorf("%s applicability depends on optional project context", m.ID)
				}
				if (tables[m.ID] || footnotes[m.ID]) && m.AppliesTo == nil {
					t.Errorf("%s lacks its production dialect predicate", m.ID)
				}
			}
		})
	}
}

func TestFullCatalogUnavailableCoverageRespectsDialect(t *testing.T) {
	// The real catalog has 59 default-enabled contracts, including the MD094
	// mandatory gate. No executable registration is supplied: every applicable
	// ordinary default must remain an explicit coverage deficit.
	cases := []struct {
		flavor  string
		omitted map[string]bool
		count   int
	}{
		{"commonmark", map[string]bool{"MD055": true, "MD056": true, "MD058": true, "MD066": true, "MD067": true, "MD068": true, "MD075": true}, 51},
		{"gfm", map[string]bool{"MD066": true, "MD067": true, "MD068": true}, 55},
	}
	reg, err := engine.NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range cases {
		t.Run(tt.flavor, func(t *testing.T) {
			in, err := engine.NewInput("a.md", []byte("Text[^note]\n\nA | B\n--- | ---\n"), nil, engine.DocumentContext{Flavor: tt.flavor})
			if err != nil {
				t.Fatal(err)
			}
			out, err := reg.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if out.Complete || out.Status != "incomplete" || len(out.Diagnostics) != 0 || len(out.Errors) != 0 {
				t.Fatalf("unexpected outcome: %+v", out)
			}
			got := map[string]bool{}
			for _, c := range out.Coverage {
				if c.Reason == result.RuleUnavailable {
					got[c.Rule] = true
				}
			}
			want := map[string]bool{}
			for _, m := range engine.Catalog() {
				if m.DefaultEnabled && m.ID != "MD094" && !tt.omitted[m.ID] {
					want[m.ID] = true
				}
			}
			if len(want) != tt.count || !reflect.DeepEqual(got, want) {
				t.Fatalf("unavailable coverage = %v, want %d entries: %v", got, tt.count, want)
			}
			// Resource-dependent defaults remain applicable without optional
			// project context; absent context cannot silently certify coverage.
			if !got["MD051"] || !got["MD057"] {
				t.Fatal("missing resource context suppressed applicable rules")
			}
		})
	}
}

// Function values cannot be compared with reflect.DeepEqual. Compare the
// accepted dialect behavior, then compare all remaining metadata structurally.
func catalogMetadataEqual(a, b []engine.Metadata) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if (a[i].AppliesTo == nil) != (b[i].AppliesTo == nil) {
			return false
		}
		for _, flavor := range []string{"commonmark", "gfm", "", "unknown"} {
			for _, project := range []string{"", "project"} {
				dc := engine.DocumentContext{Flavor: flavor, Project: project}
				if a[i].AppliesTo != nil && a[i].AppliesTo(dc) != b[i].AppliesTo(dc) {
					return false
				}
			}
		}
		left, right := a[i], b[i]
		left.AppliesTo, right.AppliesTo = nil, nil
		if !reflect.DeepEqual(left, right) {
			return false
		}
	}
	return true
}
