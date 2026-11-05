package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func metadataProject(t *testing.T, text string) string {
	t.Helper()
	root := project(t)
	put(t, filepath.Join(root, config.Filename), availableConfig+"frontmatter: {enabled: true}\n")
	put(t, filepath.Join(root, "doc.md"), text)
	return root
}

func metadataInspection(t *testing.T, root string, args ...string) (int, result.Result, inspection.Projection) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	status := Run(append([]string{"inspect", "doc.md", "--format=json"}, args...), &stdout, &stderr, "test")
	out := validateEmitted(t, root, stdout.Bytes())
	var p inspection.Projection
	if err := json.Unmarshal(out.Inspection, &p); err != nil {
		t.Fatal(err)
	}
	return status, out, p
}

func TestMetadataTextEscapesControlsAndJSONKeepsThem(t *testing.T) {
	root := metadataProject(t, "---\ntitle: \"\\e[31m\\u009bred\"\n---\n\n# Body\n")
	var stdout, stderr bytes.Buffer
	if status := Run([]string{"inspect", "doc.md", "--format=text"}, &stdout, &stderr, "test"); status != 0 {
		t.Fatal(status, stdout.String(), stderr.String())
	}
	text := stdout.Bytes()
	if !bytes.Contains(text, []byte("Metadata\n")) || !bytes.Contains(text, []byte(`"status":"found"`)) {
		t.Fatalf("no labeled record: %s", text)
	}
	if bytes.IndexByte(text, 0x1b) >= 0 || bytes.Contains(text, []byte("\u009b")) {
		t.Fatalf("raw terminal control in text output: %q", text)
	}
	status, _, p := metadataInspection(t, root)
	if status != 0 || p.Metadata.Data.Title.Text != "\x1b[31m\u009bred" {
		t.Fatalf("%d %+v", status, p.Metadata.Data.Title)
	}
}

func TestMetadataReportTrimming(t *testing.T) {
	root := metadataProject(t, "---\nz: 1\na: 2\n---\n\n"+strings.Repeat("paragraph\n\n", 200))
	_, out, p := metadataInspection(t, root)
	full, err := json.Marshal(inspectionJSON{&p, out})
	if err != nil {
		t.Fatal(err)
	}
	omitted := func(r result.Result, section string) int {
		for _, o := range r.Omissions {
			if o.Section == section {
				return *o.Count
			}
		}
		return 0
	}
	// Just over budget: dropping the AST is enough, and the record stays.
	var reduced bytes.Buffer
	typed := p
	if status := reportInspection(out, &typed, "json", &reduced, map[string]int64{"response-bytes": int64(len(full)) - 1}); status != 15 {
		t.Fatal(status)
	}
	got := validateEmitted(t, root, reduced.Bytes())
	var q inspection.Projection
	if err := json.Unmarshal(got.Inspection, &q); err != nil {
		t.Fatal(err)
	}
	if q.AST.Data != nil || omitted(got, "/inspection/ast") != 1 || q.Metadata.Availability != "available" || omitted(got, "/inspection/metadata") != 0 {
		t.Fatalf("%+v %+v", q.Metadata, got.Omissions)
	}
	// Forced detail omission removes the whole record with an exact omission.
	status, got, q := metadataInspection(t, root, "--limit=response-bytes=96KiB")
	if status != 15 || q.Metadata.Availability != "unavailable" || q.Metadata.Data != nil || !reflect.DeepEqual(q.Metadata.Reasons, []string{"output-limit"}) || omitted(got, "/inspection/metadata") != 1 {
		t.Fatalf("%d %+v %+v", status, q.Metadata, got.Omissions)
	}
	if text := func() string {
		var stdout, stderr bytes.Buffer
		Run([]string{"inspect", "doc.md", "--format=text", "--limit=response-bytes=96KiB"}, &stdout, &stderr, "test")
		return stdout.String()
	}(); !strings.Contains(text, "Metadata\n") || !strings.Contains(text, "output-limit") {
		t.Fatal(text)
	}
}

func TestMetadataCauseLinksFollowReportTrimming(t *testing.T) {
	p := inspection.Projection{Metadata: inspection.Section[inspection.MetadataRecord]{CauseIDs: []string{"interruption-1", "coverage-1"}}}
	retainInspectionCauses(&p, result.Result{Interruptions: []result.Interruption{{ID: "interruption-1"}}})
	if !reflect.DeepEqual(p.Metadata.CauseIDs, []string{"interruption-1"}) {
		t.Fatal(p.Metadata.CauseIDs)
	}
	clearInspectionCauses(&p)
	if p.Metadata.CauseIDs != nil {
		t.Fatal(p.Metadata.CauseIDs)
	}
}

func TestMetadataManifestDeclaresTheSection(t *testing.T) {
	root := metadataProject(t, "---\nz: 1\n---\n")
	registry, err := engine.NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := inspection.Acquire(context.Background(), "doc.md", inspection.Options{WorkingDirectory: root, BoundaryDirectory: root, Registry: registry})
	if err != nil {
		t.Fatal(err)
	}
	_, p := inspectionManifest(snapshot)
	if p.Metadata.Availability != "unavailable" || p.Metadata.Data != nil || !reflect.DeepEqual(p.Metadata.Reasons, []string{"output-limit"}) {
		t.Fatalf("%+v", p.Metadata)
	}
}

func TestMetadataDecodeLimitReportsIncomplete(t *testing.T) {
	root := metadataProject(t, "---\ntitle: ok\nz: 1\na: 2\n---\n\n# Body\n")
	run := func(limit int64) (int, result.Result, inspection.Projection) {
		return metadataInspection(t, root, "--limit=work-units="+strconv.FormatInt(limit, 10))
	}
	low, high := int64(0), parser.DefaultLimits().WorkUnits
	for low+1 < high {
		middle := low + (high-low)/2
		if _, _, p := run(middle); p.Metadata.Availability == "available" {
			high = middle
		} else {
			low = middle
		}
	}
	status, out, p := run(low)
	if status != 15 || out.Status != "incomplete" || p.Metadata.Availability != "unavailable" || len(p.Metadata.CauseIDs) == 0 {
		t.Fatalf("%d %s %+v", status, out.Status, p.Metadata)
	}
	if p.AST.Availability != "available" || p.Rules.Availability != "available" || p.Directives.Availability != "available" {
		t.Fatalf("%s %s %s", p.AST.Availability, p.Rules.Availability, p.Directives.Availability)
	}
}
