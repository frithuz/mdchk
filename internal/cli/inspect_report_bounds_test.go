package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/report"
	"github.com/frithuz/mdchk/internal/result"
)

func inspectionReportFixture(t *testing.T, paragraphs int) (result.Result, *inspection.Projection) {
	t.Helper()
	root := validProject(t)
	put(t, filepath.Join(root, "doc.md"), strings.Repeat("p\n\n", paragraphs))
	var output, stderr bytes.Buffer
	// Fixture construction includes a full report encoding, outside the measured
	// report loop below. Give that setup a finite allowance under race/slow CI;
	// leave analysis budgets and every measured reporting limit unchanged.
	status := Run([]string{"inspect", "doc.md", "--format=json", "--limit=reporting-time=30s"}, &output, &stderr, "test")
	var out result.Result
	if err := json.Unmarshal(output.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if status != 0 || out.Status != "inspected" || len(out.Interruptions) != 0 || stderr.Len() != 0 {
		t.Fatalf("fixture paragraphs=%d exit=%d status=%s interruptions=%+v errors=%+v omissions=%+v stderr=%q", paragraphs, status, out.Status, out.Interruptions, out.Errors, out.Omissions, stderr.String())
	}
	var p inspection.Projection
	if err := json.Unmarshal(out.Inspection, &p); err != nil {
		t.Fatal(err)
	}
	if p.AST.Availability != "available" || p.AST.Data == nil || len(p.AST.Data.Root.Children) != paragraphs || p.AST.Data.Root.ByteRange != (result.ByteRange{Start: 0, End: 3 * paragraphs}) || p.File.SizeBytes != int64(3*paragraphs) {
		t.Fatalf("fixture paragraphs=%d lacks full AST/source: file=%+v AST availability=%s", paragraphs, p.File, p.AST.Availability)
	}
	for i, child := range p.AST.Data.Root.Children {
		if child.Kind != "paragraph" || child.ByteRange.Start != 3*i || len(child.Children) != 1 {
			t.Fatalf("fixture paragraph %d incomplete: %+v", i, child)
		}
	}
	return out, &p
}

func TestInspectionTypedSizesAndTextIdentity(t *testing.T) {
	out, p := inspectionReportFixture(t, 8)
	p.AST.Data.Root.Properties["escaped"] = "<&>\u2028\u2029\"\\"
	p.AST.Data.Root.Properties["controls"] = "\u009b\u202e"
	p.AST.Data.Root.Properties["numbers"] = []any{0.0000001, 1e21, 1.25, true, nil}
	out.Notes = append(out.Notes, json.RawMessage(` { "id" : "typed-note", "code": "fixture", "message": "<&>\u2028", "path": "doc.md" } `))
	encoded, err := report.EncodeInspection(context.Background(), out, p, "json", nil)
	if err != nil {
		t.Fatal(err)
	}
	size, _, err := report.InspectionJSONSize(context.Background(), out, p, 1<<30)
	if err != nil || int64(len(encoded.Data)) != size {
		t.Fatal(size, len(encoded.Data), err)
	}
	if !json.Valid(encoded.Out.Inspection) {
		t.Fatal("retained admitted inspection byte view is invalid")
	}
	var typed, wire bytes.Buffer
	if err := report.RenderTypedInspectionReport(context.Background(), &typed, out, p); err != nil {
		t.Fatal(err)
	}
	if err := report.RenderText(context.Background(), &wire, encoded.Out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(typed.Bytes(), wire.Bytes()) {
		t.Fatal("typed and wire text differ")
	}
	counter := &report.ReportCounter{}
	if err := report.RenderTypedInspectionReport(context.Background(), counter, out, p); err != nil || counter.N != int64(typed.Len()) {
		t.Fatal(counter.N, typed.Len(), err)
	}
	validateEmitted(t, t.TempDir(), encoded.Data)
}

type reportEncodingProbe struct{ calls *int }

func (p reportEncodingProbe) MarshalJSON() ([]byte, error) { *p.calls++; return []byte(`"probe"`), nil }

type inspectionCheckpointContext struct {
	context.Context
	calls, stop int
}

func (c *inspectionCheckpointContext) Err() error {
	c.calls++
	if c.calls >= c.stop {
		return context.DeadlineExceeded
	}
	return nil
}

func TestInspectionTypedAdmissionAndDeadlineBeforeEncoding(t *testing.T) {
	out, p := inspectionReportFixture(t, 1000)
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			calls := 0
			p.AST.Data.Root.Properties["encoding_probe"] = reportEncodingProbe{&calls}
			_, err := report.EncodeInspection(context.Background(), out, p, format, map[string]int64{"response-bytes": 98304})
			if !errors.Is(err, report.ErrReportBudget) || calls != 0 {
				t.Fatalf("oversize admission: err=%v encoding calls=%d", err, calls)
			}
			ctx := &inspectionCheckpointContext{Context: context.Background(), stop: 500}
			_, err = report.EncodeInspection(ctx, out, p, format, nil)
			if !errors.Is(err, context.DeadlineExceeded) || ctx.calls < 500 || calls != 0 {
				t.Fatalf("deadline err=%v checkpoints=%d encoding calls=%d", err, ctx.calls, calls)
			}
			delete(p.AST.Data.Root.Properties, "encoding_probe")
		})
	}
}

func TestInspectionReportScaling(t *testing.T) {
	t.Logf("go=%s cpus=%d gomaxprocs=%d; one serial report per case; fixture acquisition/analysis excluded; GC before each report; allocation churn, not live heap/RSS", runtime.Version(), runtime.NumCPU(), runtime.GOMAXPROCS(0))
	for _, paragraphs := range []int{1000, 2000, 4000, 8000} {
		t.Run(fmt.Sprint(paragraphs), func(t *testing.T) {
			out, p := inspectionReportFixture(t, paragraphs)
			beforeView, _ := json.Marshal(p)
			for _, format := range []string{"json", "text"} {
				for _, deadline := range []time.Duration{time.Millisecond, 2 * time.Second} {
					runtime.GC()
					var before, after runtime.MemStats
					runtime.ReadMemStats(&before)
					start := time.Now()
					var output bytes.Buffer
					status := report.ReportInspection(out, p, format, &output, map[string]int64{"response-bytes": 98304, "reporting-time": int64(deadline)})
					elapsed := time.Since(start)
					runtime.ReadMemStats(&after)
					t.Logf("format=%s deadline=%s elapsed=%s B/op=%d allocs/op=%d output=%d status=%d", format, deadline, elapsed, after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs, output.Len(), status)
					if status != 15 || output.Len() > 98304 || output.Len() == 0 {
						t.Fatal("invalid bounded report", status, output.Len())
					}
					if format == "json" {
						validateEmitted(t, t.TempDir(), output.Bytes())
					}
				}
			}
			afterView, _ := json.Marshal(p)
			if !reflect.DeepEqual(beforeView, afterView) {
				t.Fatal("reporter changed typed source views")
			}
		})
	}
}

func TestInspectionDELAdmissionBeforeTextMaterialization(t *testing.T) {
	out, p := inspectionReportFixture(t, 1)
	p.AST.Data.Root.Properties["DEL"] = strings.Repeat("\x7f", 100)
	var actual bytes.Buffer
	if err := report.RenderTypedInspectionReport(context.Background(), &actual, out, p); err != nil {
		t.Fatal(err)
	}
	counter := &report.ReportCounter{}
	if err := report.RenderTypedInspectionReport(context.Background(), counter, out, p); err != nil || counter.N != int64(actual.Len()) {
		t.Fatal(counter.N, actual.Len(), err)
	}
	// Before the repair the 100 literal DELs were each underestimated by 3 bytes.
	// This cap lies between that old estimate and the actual escaped report.
	capBytes := int64(actual.Len()) - 150
	probeCalls := 0
	p.AST.Data.Root.Properties["preencoding_probe"] = reportEncodingProbe{&probeCalls}
	_, err := report.EncodeInspection(context.Background(), out, p, "text", map[string]int64{"response-bytes": capBytes})
	if !errors.Is(err, report.ErrReportBudget) || probeCalls != 0 {
		t.Fatalf("text materialized before exact DEL admission: err=%v encoding calls=%d", err, probeCalls)
	}
}
