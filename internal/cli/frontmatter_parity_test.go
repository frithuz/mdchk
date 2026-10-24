package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/result"
	fixtures "github.com/frithuz/mdchk/internal/testutil/frontmatter"
)

func frontmatterParityConfiguration(t *testing.T, row fixtures.Case, flavor string) string {
	t.Helper()
	text := strings.Replace(fixConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
	if row.Frontmatter != nil {
		raw, err := json.Marshal(row.Frontmatter)
		if err != nil {
			t.Fatal(err)
		}
		text += "frontmatter: " + string(raw) + "\n"
	}
	return text
}

func assertFrontmatterParityBytes(t *testing.T, path, want string) {
	t.Helper()
	//nolint:gosec // The caller supplies only its test-owned temporary fixture path.
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != want {
		t.Fatalf("source changed: %v, %q, want %q", err, raw, want)
	}
}

func frontmatterParityRun(t *testing.T, root string, args ...string) (int, result.Result) {
	t.Helper()
	var out, logs bytes.Buffer
	status := Run(args, &out, &logs, "test")
	if logs.Len() != 0 {
		t.Fatalf("%v stderr=%s", args, &logs)
	}
	return status, validateEmitted(t, root, out.Bytes())
}

func assertFrontmatterParityInspection(t *testing.T, row fixtures.Case, out result.Result) {
	t.Helper()
	var p inspection.Projection
	if err := json.Unmarshal(out.Inspection, &p); err != nil {
		t.Fatal(err)
	}
	if p.AST.Data == nil || out.Complete != row.Complete || p.AST.Data.Root.ByteRange != (result.ByteRange{Start: 0, End: len(row.Source)}) {
		t.Fatalf("inconsistent inspection %+v", p)
	}
	h := p.AST.Data.Frontmatter
	if (h != nil) != (row.Header != nil) {
		t.Fatalf("header %+v, want %+v", h, row.Header)
	}
	if h != nil {
		want := row.Header
		if h.Format != want.Format || h.BodyOffset != want.BodyOffset || h.ByteRange != (result.ByteRange{Start: want.Region[0], End: want.Region[1]}) || h.Opener.ByteRange != (result.ByteRange{Start: want.Opener[0], End: want.Opener[1]}) || h.Content.ByteRange != (result.ByteRange{Start: want.Content[0], End: want.Content[1]}) || (h.Closer != nil) != (want.Closer != nil) {
			t.Fatalf("header %+v want %+v", h, want)
		}
		if want.Closer != nil && h.Closer.ByteRange != (result.ByteRange{Start: want.Closer[0], End: want.Closer[1]}) {
			t.Fatal(h.Closer)
		}
	}
	if p.Configuration.Data == nil {
		t.Fatal("missing settings")
	}
	settings := p.Configuration.Data.Effective["frontmatter"].(map[string]any)
	enabled := row.Frontmatter != nil && row.Frontmatter.Enabled
	if settings["enabled"] != enabled {
		t.Fatal("lost enabled setting", settings)
	}
	wantFormats := []any{"yaml", "toml"}
	if row.Frontmatter != nil && row.Frontmatter.Formats != nil {
		wantFormats = nil
		for _, f := range row.Frontmatter.Formats {
			wantFormats = append(wantFormats, f)
		}
	}
	if !reflect.DeepEqual(settings["formats"], wantFormats) {
		t.Fatal("lost formats", settings, wantFormats)
	}
	explicit := false
	for _, origin := range p.Configuration.Data.Provenance {
		if origin.Pointer == "/frontmatter/enabled" && origin.SourceKind == "selected-config" {
			explicit = true
		}
	}
	if explicit != (row.Frontmatter != nil) {
		t.Fatal("incorrect recognition provenance", p.Configuration.Data.Provenance)
	}
}

func TestFrontmatterOperationParityLedger(t *testing.T) {
	rows, err := fixtures.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(row.ID+"/"+flavor, func(t *testing.T) {
				root := project(t)
				path := filepath.Join(root, "doc.md")
				put(t, filepath.Join(root, ".mdchk.yaml"), frontmatterParityConfiguration(t, row, flavor))
				put(t, path, row.Source)
				status, lint := frontmatterParityRun(t, root, "lint", "doc.md", "--format=json")
				want := 0
				if !row.Complete {
					want = 15
				} else if row.Diagnostics > 0 {
					want = 10
				}
				if status != want || lint.Complete != row.Complete || len(lint.Diagnostics) != row.Diagnostics || len(lint.Errors) != 0 {
					t.Fatal("lint", status, lint)
				}
				for _, d := range lint.Diagnostics {
					if d.Rule != "MD047" || d.ByteRange != (result.ByteRange{Start: len(row.Source), End: len(row.Source)}) || d.Range.Start.Line != row.DiagnosticPosition[0] || d.Range.Start.Column != row.DiagnosticPosition[1] || d.FixAvailable != row.Fix {
						t.Fatal(d)
					}
				}
				assertFrontmatterParityBytes(t, path, row.Source)
				status, view := frontmatterParityRun(t, root, "inspect", "doc.md", "--format=json")
				if status != 0 {
					t.Fatal("inspect", status, view)
				}
				assertFrontmatterParityInspection(t, row, view)
				assertFrontmatterParityBytes(t, path, row.Source)
				status, preview := frontmatterParityRun(t, root, "fix", "--dry-run", "doc.md", "--format=json")
				want = 0
				if !row.Complete {
					want = 15
				} else if row.Diagnostics > 0 && !row.Fix {
					want = 10
				}
				changes := 0
				if row.Fix {
					changes = 1
				}
				if status != want || preview.Complete != row.Complete || len(preview.Changes) != changes || len(preview.Errors) != 0 {
					t.Fatal("preview", status, preview)
				}
				if row.Fix && preview.Changes[0].Candidate.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(row.Candidate))) {
					t.Fatal("candidate bytes differ", preview.Changes[0])
				}
				assertFrontmatterParityBytes(t, path, row.Source)
			})
		}
	}
}
