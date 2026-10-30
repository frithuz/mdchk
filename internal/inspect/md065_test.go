package inspect

import (
	"encoding/json"
	"fmt"
	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
	"path/filepath"
	"strings"
	"testing"
)

func md065Definition(setting string) string {
	var b strings.Builder
	b.WriteString("rules:\n")
	for _, m := range engine.Catalog() {
		if m.ID == "MD065" {
			fmt.Fprintf(&b, "  MD065: %s\n", setting)
		} else {
			fmt.Fprintf(&b, "  %s: {enabled: false}\n", m.ID)
		}
	}
	return b.String()
}

func TestMD065InspectSuppressedOwnersAndFullContext(t *testing.T) {
	for _, tc := range []struct {
		text, setting, kind, owner, line string
		points                           [2]int
	}{
		{"Text\n<!-- mdchk:disable MD065 -->\n<!-- mdchk:disable MD065 -->\n***\nMore", "{}", "directive", "directive-2", "4", [2]int{63, 66}},
		{"Text\n***\nMore", "{enabled: false}", "configuration", "", "2", [2]int{5, 8}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			s, _ := projectionFixture(t, tc.text, md065Definition(tc.setting), md065ProductionRegistrations())
			a := analyze(t, s, true)
			if !a.Result.Complete || len(a.Result.Diagnostics) != 0 || len(a.Result.SuppressedDiagnostics) != 2 {
				t.Fatal(a.Result)
			}
			for i, raw := range a.Result.SuppressedDiagnostics {
				var hidden result.SuppressedDiagnostic
				if e := json.Unmarshal(raw, &hidden); e != nil {
					t.Fatal(e)
				}
				if hidden.SuppressedBy.Kind != tc.kind || hidden.SuppressedBy.DirectiveID != tc.owner || hidden.Diagnostic.Rule != "MD065" || hidden.Diagnostic.ByteRange != (result.ByteRange{Start: tc.points[i], End: tc.points[i]}) || hidden.Diagnostic.Message != []string{"Horizontal rule requires a blank body line before it.", "Horizontal rule requires a blank body line after it."}[i] || hidden.Diagnostic.FixAvailable {
					t.Fatal(hidden)
				}
			}
			selected := filter(t, s, a, tc.line)
			if len(selected.Result.SuppressedDiagnostics) != 2 {
				t.Fatal(selected.Result)
			}
			nextLine := "3"
			if tc.line == "4" {
				nextLine = "5"
			}
			next := filter(t, s, a, nextLine)
			if len(next.Result.SuppressedDiagnostics) != 0 || len(next.Result.Diagnostics) != 0 {
				t.Fatal(next.Result)
			}
		})
	}
}

func TestMD065InheritedDirectiveDenials(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprint(global), func(t *testing.T) {
			root, o := fixture(t)
			r, e := engine.NewCatalogRegistry(md065ProductionRegistrations())
			if e != nil {
				t.Fatal(e)
			}
			o.Registry = r
			parent := "version: 1\nflavor: commonmark\n"
			if global {
				parent += "directives: {enabled: false}\n"
			} else {
				parent += "rules: {MD065: {allow_directives: false}}\n"
			}
			write(t, filepath.Join(root, "project", config.Filename), []byte(parent))
			write(t, filepath.Join(root, "project", "docs", config.Filename), []byte("version: 1\nflavor: commonmark\ndirectives: {enabled: true}\n"+md065Definition("{allow_directives: true}")))
			source := "Text\n<!-- mdchk:disable MD065 -->\n***\nMore"
			write(t, filepath.Join(root, "project", "docs", "x.md"), []byte(source))
			s := acquire(t, filepath.Join("project", "docs", "x.md"), o)
			a := analyze(t, s, true)
			if !a.Result.Complete || len(a.Result.Diagnostics) != 2 || len(a.Result.SuppressedDiagnostics) != 0 {
				t.Fatal(a.Result)
			}
			for i, d := range a.Result.Diagnostics {
				want := []int{34, 37}[i]
				if d.Rule != "MD065" || d.ByteRange != (result.ByteRange{Start: want, End: want}) {
					t.Fatal(d)
				}
			}
			if rule(t, a, "MD065").ActiveEvaluation != "complete" {
				t.Fatal(rule(t, a, "MD065"))
			}
		})
	}
}

func md065ProductionRegistrations() []engine.Registration {
	for _, registration := range rules.Registrations() {
		if registration.ID == "MD065" {
			return []engine.Registration{registration}
		}
	}
	panic("MD065 production registration missing")
}
