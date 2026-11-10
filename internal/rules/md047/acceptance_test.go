package md047_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
)

// TestMain removes this process's private builds once every test has finished
// with them.
func TestMain(m *testing.M) {
	m.Run()
	acceptance.Cleanup()
}

// runFix runs the compiled binary in root and validates a JSON fix report.
func runFix(t *testing.T, binary, root string, args ...string) acceptance.Run {
	t.Helper()
	got := acceptance.RunProcess(t, binary, root, args...)
	for _, arg := range args {
		if arg == "--format=json" {
			got.Result = acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
		}
	}
	return got
}

// Ordinary production binaries exercise the real APFS replacement boundary.
func TestCompiledProductionMD047ApplySavedWorkflow(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	data, err := os.ReadFile("testdata/md047_corrections.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Source, Candidate, Config string
		Status                          int
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			for _, format := range []string{"json", "text"} {
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", acceptance.MD047Configuration(tc.Config != "off", tc.Config != "blocked"))
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				path := filepath.Join(root, "doc.md")
				//nolint:gosec // Access only synthetic test fixtures or the fixed repository source overlay.
				if err := os.Chmod(path, 0640); err != nil {
					t.Fatal(err)
				}
				before, _ := acceptance.StableStat(path)
				inventory := acceptance.ConfigurationBytes(t, root)
				preview := runFix(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
				if !reflect.DeepEqual(inventory, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal("dry-run changed directory inventory or bytes")
				}
				got := runFix(t, binary, root, "fix", "doc.md", "--format="+format)
				if got.Status != tc.Status {
					t.Fatalf("status %d want %d: %s", got.Status, tc.Status, got.Stdout)
				}
				//nolint:gosec // Access only synthetic test fixtures or the fixed repository source overlay.
				saved, err := os.ReadFile(path)
				if err != nil || string(saved) != tc.Candidate {
					t.Fatalf("saved %q want %q: %v %s", saved, tc.Candidate, err, got.Stdout)
				}
				after, _ := acceptance.StableStat(path)
				changed := tc.Source != tc.Candidate
				if os.SameFile(before, after) == changed || before.Mode() != after.Mode() {
					t.Fatal("publication identity/mode", before, after)
				}
				inventory[path] = sha256.Sum256([]byte(tc.Candidate))
				if !reflect.DeepEqual(inventory, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal("unexpected artifacts or changed sibling")
				}
				if format == "json" {
					f := got.Result.Files[0]
					if f.ResultSnapshot == nil || f.ResultSnapshot.View != "saved" || f.ResultSnapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256(saved)) {
						t.Fatal(f)
					}
					if changed && (f.WriteStatus != "written" || len(got.Result.Changes) != 1 || got.Result.Changes[0].Disposition != "written" || got.Result.Changes[0].Candidate != preview.Result.Changes[0].Candidate) {
						t.Fatal(got)
					}
					for _, d := range got.Result.Diagnostics {
						if d.Snapshot != *f.ResultSnapshot {
							t.Fatal("not saved diagnostic", d)
						}
					}
				} else if strings.Contains(got.Stdout, "written doc.md") != changed {
					t.Fatal(got.Stdout)
				}
				second := runFix(t, binary, root, "fix", "doc.md", "--format=json")
				twice, _ := acceptance.StableStat(path)
				if second.Status != tc.Status || len(second.Result.Changes) != 0 || !os.SameFile(after, twice) {
					t.Fatal("non-idempotent", second)
				}
			}
		})
	}
}
