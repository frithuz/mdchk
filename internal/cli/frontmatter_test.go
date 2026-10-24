package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	inspection "github.com/frithuz/mdchk/internal/inspect"
)

func TestFrontmatterCLIRecognitionInspectionAndPreview(t *testing.T) {
	for _, tc := range []struct{ flavor, format, marker string }{{"commonmark", "yaml", "---"}, {"gfm", "toml", "+++"}} {
		t.Run(tc.flavor, func(t *testing.T) {
			root := project(t)
			configuration := strings.Replace(fixConfiguration(), "flavor: commonmark", "flavor: "+tc.flavor, 1) + "frontmatter: {enabled: true, formats: [" + tc.format + "]}\n"
			put(t, filepath.Join(root, ".mdchk.yaml"), configuration)
			header := "\xef\xbb\xbf" + tc.marker + "\r\n<!-- mdchk:disable MD047 -->\r\nbad: [\r\n" + tc.marker + "\r\n"
			original := header + "雪body"
			path := filepath.Join(root, "doc.md")
			put(t, path, original)
			var stdout, stderr bytes.Buffer
			if status := Run([]string{"lint", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 10 || stderr.Len() != 0 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
			lint := validateEmitted(t, root, stdout.Bytes())
			if !lint.Complete || len(lint.Diagnostics) != 1 || lint.Diagnostics[0].Rule != "MD047" || !lint.Diagnostics[0].FixAvailable || lint.Diagnostics[0].Range.Start.Line != 5 {
				t.Fatal(lint)
			}
			stdout.Reset()
			if status := Run([]string{"inspect", "doc.md", "--lines=5", "--format=json"}, &stdout, &stderr, "test"); status != 0 {
				t.Fatal(status, stdout.String())
			}
			view := validateEmitted(t, root, stdout.Bytes())
			var projection inspection.Projection
			if err := json.Unmarshal(view.Inspection, &projection); err != nil {
				t.Fatal(err)
			}
			if projection.AST.Data == nil || projection.AST.Data.Frontmatter == nil {
				t.Fatal("missing opaque source record", projection.AST)
			}
			record := projection.AST.Data.Frontmatter
			if record.Format != tc.format || record.BodyOffset != len(header) || record.ByteRange.Start != 0 || record.ByteRange.End != len(header) || !record.ContextOnly || record.Closer == nil || record.Opener.ByteRange.Start != 3 {
				t.Fatal(record)
			}
			if projection.Configuration.Data == nil {
				t.Fatal("missing effective settings")
			}
			settings := projection.Configuration.Data.Effective["frontmatter"].(map[string]any)
			if settings["enabled"] != true {
				t.Fatal(settings)
			}
			found := false
			for _, origin := range projection.Configuration.Data.Provenance {
				if origin.Pointer == "/frontmatter/enabled" && origin.SourceKind == "selected-config" {
					found = true
				}
			}
			if !found {
				t.Fatal("missing explicit provenance", projection.Configuration.Data.Provenance)
			}
			stdout.Reset()
			if status := Run([]string{"inspect", "doc.md"}, &stdout, &stderr, "test"); status != 0 || !strings.Contains(stdout.String(), "Opaque frontmatter") || !strings.Contains(stdout.String(), tc.format) {
				t.Fatal(status, stdout.String())
			}
			stdout.Reset()
			if status := Run([]string{"fix", "--dry-run", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 {
				t.Fatal(status, stdout.String())
			}
			preview := validateEmitted(t, root, stdout.Bytes())
			if len(preview.Changes) != 1 || preview.Changes[0].Candidate.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(original+"\r\n"))) {
				t.Fatal(preview)
			}
			//nolint:gosec // Read only the test-owned temporary source fixture.
			data, err := os.ReadFile(path)
			if err != nil || string(data) != original {
				t.Fatal("read-only commands wrote source", err, string(data))
			}
		})
	}
}

func TestFrontmatterCLIHeaderOnlyAndUnterminatedStayUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		status       int
		complete     bool
	}{{"header-only", "---\nx: [\n---", 10, true}, {"unterminated", "---\nx: [\n", 15, false}} {
		t.Run(tc.name, func(t *testing.T) {
			root := project(t)
			put(t, filepath.Join(root, ".mdchk.yaml"), fixConfiguration()+"frontmatter: {enabled: true}\n")
			path := filepath.Join(root, "doc.md")
			put(t, path, tc.source)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
				var stdout, stderr bytes.Buffer
				if status := Run(args, &stdout, &stderr, "test"); status != tc.status || stderr.Len() != 0 {
					t.Fatal(args, status, stdout.String(), stderr.String())
				}
				out := validateEmitted(t, root, stdout.Bytes())
				if out.Complete != tc.complete || len(out.Changes) != 0 || len(out.Errors) != 0 {
					t.Fatal(out)
				}
				for _, finding := range out.Diagnostics {
					if finding.FixAvailable {
						t.Fatal("header EOF edit offered", finding)
					}
				}
			}
			var stdout, stderr bytes.Buffer
			if status := Run([]string{"inspect", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 {
				t.Fatal(status, stdout.String())
			}
			out := validateEmitted(t, root, stdout.Bytes())
			var projection inspection.Projection
			if err := json.Unmarshal(out.Inspection, &projection); err != nil {
				t.Fatal(err)
			}
			if projection.AST.Data == nil || projection.AST.Data.Frontmatter == nil || (projection.AST.Data.Frontmatter.Closer != nil) != tc.complete {
				t.Fatal(projection.AST)
			}
			if !tc.complete && (projection.AST.Availability != "partial" || out.Complete || len(out.Notes) == 0) {
				t.Fatal(out, projection.AST)
			}
			//nolint:gosec // Read only the test-owned temporary source fixture.
			data, err := os.ReadFile(path)
			if err != nil || string(data) != tc.source {
				t.Fatal("opaque bytes changed", err)
			}
		})
	}
}
