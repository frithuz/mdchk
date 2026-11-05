package inspect

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
)

func md058Definition(setting string) string {
	var b strings.Builder
	b.WriteString("rules:\n")
	for _, m := range engine.Catalog() {
		if m.ID == "MD058" {
			fmt.Fprintf(&b, "  MD058: %s\n", setting)
		} else {
			fmt.Fprintf(&b, "  %s: {enabled: false}\n", m.ID)
		}
	}
	return b.String()
}

func md058ProductionRegistrations() []engine.Registration {
	for _, registration := range rules.Registrations() {
		if registration.ID == "MD058" {
			return []engine.Registration{registration}
		}
	}
	panic("MD058 production registration missing")
}

func md058Snapshot(t *testing.T, text, flavor, setting string) *Snapshot {
	t.Helper()
	root, o := fixture(t)
	r, err := engine.NewCatalogRegistry(md058ProductionRegistrations())
	if err != nil {
		t.Fatal(err)
	}
	o.Registry = r
	o.Capabilities.GFM = true
	write(t, filepath.Join(root, "x.md"), []byte(text))
	write(t, filepath.Join(root, config.Filename), []byte("version: 1\nflavor: "+flavor+"\n"+md058Definition(setting)))
	return acquire(t, "x.md", o)
}

func TestMD058InspectSuppressedOwnersAndFullContext(t *testing.T) {
	for _, tc := range []struct {
		text, setting, kind, owner, line string
		points                           [2]int
	}{
		{"<!-- mdchk:disable MD058 -->\n\n# T\n| a |\n| - |\n# N\n", "{}", "directive", "directive-1", "4", [2]int{34, 45}},
		{"# T\n| a |\n| - |\n# N\n", "{enabled: false}", "configuration", "", "2", [2]int{4, 15}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			s := md058Snapshot(t, tc.text, "gfm", tc.setting)
			a := analyze(t, s, true)
			if !a.Result.Complete || len(a.Result.Diagnostics) != 0 || len(a.Result.SuppressedDiagnostics) != 2 {
				t.Fatal(a.Result)
			}
			for i, raw := range a.Result.SuppressedDiagnostics {
				var hidden result.SuppressedDiagnostic
				if e := json.Unmarshal(raw, &hidden); e != nil {
					t.Fatal(e)
				}
				if hidden.SuppressedBy.Kind != tc.kind || hidden.SuppressedBy.DirectiveID != tc.owner || hidden.Diagnostic.Rule != "MD058" || hidden.Diagnostic.ByteRange != (result.ByteRange{Start: tc.points[i], End: tc.points[i]}) || hidden.Diagnostic.Message != []string{"Table requires a blank body line before it.", "Table requires a blank body line after it."}[i] || hidden.Diagnostic.FixAvailable {
					t.Fatal(hidden)
				}
			}
			// The before point is on the table's first line, the after point on its last.
			first := filter(t, s, a, tc.line)
			if len(first.Result.SuppressedDiagnostics) != 1 {
				t.Fatal(first.Result)
			}
			last := filter(t, s, a, fmt.Sprint(int(tc.line[0]-'0')+1))
			if len(last.Result.SuppressedDiagnostics) != 1 {
				t.Fatal(last.Result)
			}
			if rule(t, a, "MD058").ActiveEvaluation == "complete" != (tc.kind == "directive") {
				t.Fatal(rule(t, a, "MD058"))
			}
		})
	}
}

func TestMD058InspectVisibleAndCommonMarkInapplicable(t *testing.T) {
	text := "# T\n| a |\n| - |\n# N\n"
	a := analyze(t, md058Snapshot(t, text, "gfm", "{}"), false)
	if !a.Result.Complete || len(a.Result.Diagnostics) != 2 || len(a.Result.Changes)+len(a.Result.DeferredFixes)+len(a.Result.Coverage) != 0 {
		t.Fatal(a.Result)
	}
	for i, d := range a.Result.Diagnostics {
		if d.Rule != "MD058" || d.FixAvailable || d.ByteRange.Start != []int{4, 15}[i] || d.Range.Start != []result.Position{{Line: 2, Column: 1}, {Line: 3, Column: 6}}[i] {
			t.Fatal(d)
		}
	}
	a = analyze(t, md058Snapshot(t, text, "commonmark", "{}"), false)
	if !a.Result.Complete || len(a.Result.Diagnostics)+len(a.Result.Coverage) != 0 || rule(t, a, "MD058").ActiveEvaluation == "complete" {
		t.Fatal(a.Result, rule(t, a, "MD058"))
	}
}
