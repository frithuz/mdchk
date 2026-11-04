package inspect

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
)

// Inactive observation is a supported programmatic inspection option; the CLI
// has no corresponding flag. Keep its findings separate from active diagnostics.
func TestMD051RequestedInactiveInspection(t *testing.T) {
	const source = "[x](#nope)"
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, observe := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/observe-%v", flavor, observe), func(t *testing.T) {
				root, options := fixture(t)
				registry, err := engine.NewCatalogRegistry(rules.Registrations())
				if err != nil {
					t.Fatal(err)
				}
				options.Registry = registry
				options.Capabilities = config.Capabilities{CommonMark: true, GFM: true, DirectiveProcessing: true}
				definition := "version: 1\nflavor: " + flavor + "\n" + md087Definition(false)
				write(t, filepath.Join(root, "x.md"), []byte(source))
				write(t, filepath.Join(root, config.Filename), []byte(definition))
				snapshot := acquire(t, "x.md", options)
				out, err := Analyze(context.Background(), snapshot, AnalyzeOptions{ObserveInactive: observe})
				if err != nil || !out.Result.Complete || len(out.Result.Diagnostics)+len(out.Result.Errors)+len(out.Result.Coverage)+len(out.Result.Changes)+len(out.Result.Documents)+len(out.Result.DeferredFixes) != 0 {
					t.Fatal(out, err)
				}
				state := rule(t, out, "MD051")
				wantStatus, wantCount := "not-requested", 0
				if observe {
					wantStatus, wantCount = "complete", 1
				}
				if state.ConfiguredEnabled || state.ActiveEvaluation != "skipped" || state.ObservationalEvaluation != wantStatus {
					t.Fatal(state)
				}
				found := 0
				for _, raw := range out.Result.SuppressedDiagnostics {
					var hidden result.SuppressedDiagnostic
					if err := json.Unmarshal(raw, &hidden); err != nil {
						t.Fatal(err)
					}
					if hidden.Diagnostic.Rule != "MD051" {
						continue
					}
					found++
					d := hidden.Diagnostic
					if d.ByteRange != (result.ByteRange{Start: 4, End: 9}) || d.Range != (result.Range{Start: result.Position{Line: 1, Column: 5}, End: result.Position{Line: 1, Column: 10}}) || d.Message != "Fragment target is not defined in this document." || d.FixAvailable || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: 10, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}) || hidden.SuppressedBy.Kind != "configuration" {
						t.Fatal(hidden)
					}
				}
				if found != wantCount {
					t.Fatal(out.Result)
				}
				validateProduced(t, out)
				for name, want := range map[string]string{"x.md": source, config.Filename: definition} {
					bytes, err := os.ReadFile(filepath.Join(root, name)) //nolint:gosec // G304: test-owned root and the two fixed fixture names above.
					if err != nil || string(bytes) != want {
						t.Fatal(name, err)
					}
				}
			})
		}
	}
}
