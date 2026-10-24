package inspect

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

func frontmatterFixture(t *testing.T, text, flavor, selection string) *Snapshot {
	t.Helper()
	root, options := fixture(t)
	options.Capabilities.GFM = true
	options.Capabilities.FrontmatterYAML = true
	options.Capabilities.FrontmatterTOML = true
	var definition strings.Builder
	fmt.Fprintf(&definition, "version: 1\nflavor: %s\nfrontmatter: %s\nrules:\n", flavor, selection)
	for _, metadata := range engine.Catalog() {
		fmt.Fprintf(&definition, "  %s: {enabled: false}\n", metadata.ID)
	}
	write(t, filepath.Join(root, config.Filename), []byte(definition.String()))
	write(t, filepath.Join(root, "x.md"), []byte(text))
	return acquire(t, "x.md", options)
}

func TestFrontmatterInspectionSourceAndSelection(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			text := "\ufeff---\r\ntitle: 雪\r\n<!-- mdchk:disable MD001 -->\r\n---\r\n# Body 雪\r\n"
			source := frontmatterFixture(t, text, flavor, "{enabled: true}")
			full := analyze(t, source, false)
			body := strings.Index(text, "# Body")
			if full.Projection.AST.Data == nil || full.Projection.AST.Availability != "available" {
				t.Fatal("missing source AST", full.Projection)
			}
			header := full.Projection.AST.Data.Frontmatter
			if header == nil || header.Format != "yaml" || header.ByteRange != (result.ByteRange{Start: 0, End: body}) || header.BodyOffset != body || header.Closer == nil {
				t.Fatalf("header %+v", header)
			}
			if header.Opener.ByteRange != (result.ByteRange{Start: 3, End: 6}) || header.Opener.Range.Start.Column != 2 || header.Content.ByteRange.Start != 8 || header.Content.ByteRange.End != strings.Index(text, "\r\n---")+2 || header.Range.End.Line != 5 {
				t.Fatalf("original coordinates %+v", header)
			}
			root := full.Projection.AST.Data.Root
			if root.ByteRange != (result.ByteRange{Start: 0, End: len(text)}) || len(root.Children) != 1 || root.Children[0].Kind != "heading" || root.Children[0].ByteRange.Start != body || root.Children[0].Range.Start.Line != 5 {
				t.Fatalf("body/root %+v", root)
			}
			if len(*full.Projection.Directives.Data) != 0 || len(*full.Projection.RawCandidates.Data) != 1 || (*full.Projection.RawCandidates.Data)[0].Context != "literal" {
				t.Fatal("header directive escaped", full.Projection)
			}
			settings := full.Projection.Configuration.Data.Effective["frontmatter"].(map[string]any)
			if settings["enabled"] != true {
				t.Fatal("lost recognition settings", settings)
			}
			provenance := false
			for _, origin := range full.Projection.Configuration.Data.Provenance {
				provenance = provenance || origin.Pointer == "/frontmatter/enabled" && origin.Path == filepath.Join(filepath.Dir(source.File().Path), config.Filename)
			}
			if !provenance {
				t.Fatal("lost recognition provenance")
			}
			validateProduced(t, full)
			for _, selected := range []string{"2", "5"} {
				view, err := Filter(context.Background(), source, full, selected)
				if err != nil {
					t.Fatal(err)
				}
				h := view.Projection.AST.Data.Frontmatter
				if h == nil || h.ContextOnly != (selected == "5") || !h.ExtendsRegion || h.BodyOffset != body || h.ByteRange != header.ByteRange {
					t.Fatalf("selection %s header %+v", selected, h)
				}
				if selected == "2" && len(view.Projection.AST.Data.Root.Children) != 0 {
					t.Fatal("header selection invented body nodes")
				}
				validateProduced(t, view)
			}
			// Public mutation must not replace retained full-source authority.
			header.Content.ByteRange.End = 0
			again, err := Filter(context.Background(), source, full, "5")
			if err != nil {
				t.Fatal(err)
			}
			if again.Projection.AST.Data.Frontmatter.Content.ByteRange.End == 0 {
				t.Fatal("header projection alias changed authority")
			}
			raw, err := os.ReadFile(source.File().Path)
			if err != nil || !bytes.Equal(raw, []byte(text)) {
				t.Fatal("inspection changed bytes", err)
			}
		})
	}
}

func TestFrontmatterInspectionAbsentEmptyAndUnterminated(t *testing.T) {
	for _, tc := range []struct {
		name, text, selection string
		present, terminated   bool
	}{
		{"disabled", "---\nx: y\n---\n", "{enabled: false}", false, false},
		{"unselected", "+++\nx=1\n+++\n", "{enabled: true, formats: [yaml]}", false, false},
		{"no-header", "ordinary\n", "{enabled: true}", false, false},
		{"empty", "---\n---\n", "{enabled: true}", true, true},
		{"toml", "+++\ninvalid [\n+++\n", "{enabled: true}", true, true},
		{"unterminated", "---\n<!-- mdchk:disable MD001 -->\n", "{enabled: true}", true, false},
		{"opener-eof", "---", "{enabled: true}", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := frontmatterFixture(t, tc.text, "commonmark", tc.selection)
			full := analyze(t, source, false)
			if full.Projection.AST.Data == nil {
				t.Fatal("missing known AST", full.Projection)
			}
			header := full.Projection.AST.Data.Frontmatter
			if (header != nil) != tc.present {
				t.Fatalf("presence %+v", header)
			}
			if tc.present && (header.Closer != nil) != tc.terminated {
				t.Fatalf("termination %+v", header)
			}
			if tc.present && !tc.terminated {
				if full.Result.Complete || full.Projection.AST.Availability != "partial" || len(full.Projection.AST.Data.Root.Children) != 0 || full.Projection.ParsingNotes.Data == nil {
					t.Fatal("false completion", full.Projection)
				}
				notes := *full.Projection.ParsingNotes.Data
				if len(notes) != 1 || notes[0].Code != "unterminated-frontmatter" || *notes[0].ByteRange != (result.ByteRange{Start: 0, End: len(tc.text)}) {
					t.Fatal("missing located uncertainty", notes)
				}
				if full.Projection.Rules.Data != nil || full.Projection.Directives.Data != nil {
					t.Fatal("manufactured completed consumers")
				}
				if full.Projection.RawCandidates.Data != nil {
					for _, raw := range *full.Projection.RawCandidates.Data {
						if raw.Context != "literal" || raw.DirectiveID != "" {
							t.Fatal(raw)
						}
					}
				}
			} else if full.Projection.AST.Availability != "available" {
				t.Fatal("ordinary/complete AST unavailable")
			}
			if tc.name == "empty" && header.Content.ByteRange.Start != header.Content.ByteRange.End {
				t.Fatal("empty payload lost")
			}
			validateProduced(t, full)
		})
	}
}
