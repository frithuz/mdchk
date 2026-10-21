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
	encoded, err := encodeInspection(context.Background(), out, p, "json", nil)
	if err != nil {
		t.Fatal(err)
	}
	size, _, err := inspectionJSONSize(context.Background(), out, p, 1<<30)
	if err != nil || int64(len(encoded.data)) != size {
		t.Fatal(size, len(encoded.data), err)
	}
	if !json.Valid(encoded.out.Inspection) {
		t.Fatal("retained admitted inspection byte view is invalid")
	}
	var typed, wire bytes.Buffer
	if err := renderTypedInspectionReport(context.Background(), &typed, out, p); err != nil {
		t.Fatal(err)
	}
	if err := renderText(context.Background(), &wire, encoded.out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(typed.Bytes(), wire.Bytes()) {
		t.Fatal("typed and wire text differ")
	}
	counter := &reportCounter{}
	if err := renderTypedInspectionReport(context.Background(), counter, out, p); err != nil || counter.n != int64(typed.Len()) {
		t.Fatal(counter.n, typed.Len(), err)
	}
	validateEmitted(t, t.TempDir(), encoded.data)
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
			_, err := encodeInspection(context.Background(), out, p, format, map[string]int64{"response-bytes": 98304})
			if !errors.Is(err, errReportBudget) || calls != 0 {
				t.Fatalf("oversize admission: err=%v encoding calls=%d", err, calls)
			}
			ctx := &inspectionCheckpointContext{Context: context.Background(), stop: 500}
			_, err = encodeInspection(ctx, out, p, format, nil)
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
					status := reportInspection(out, p, format, &output, map[string]int64{"response-bytes": 98304, "reporting-time": int64(deadline)})
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

func TestTextFieldTerminalControls(t *testing.T) {
	input := "\x00\x07\b\t\n\r\v\f\x1b\x7f\u0085\u009b\u202e\u2066雪🙂"
	want := `\x00\x07\b\t\n\r\v\f\x1b\x7f\u0085\u009b\u202e\u2066雪🙂`
	if got := textField(input); got != want {
		t.Fatalf("terminal field %q, expected %q", got, want)
	}
}

func TestInspectionTerminalSafeSizeMatrix(t *testing.T) {
	var controls strings.Builder
	for r := rune(0); r <= 0x9f; r++ {
		if r < 0x20 || r >= 0x7f {
			controls.WriteRune(r)
		}
	}
	controls.WriteString("\u2028\u2029\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069")
	for _, input := range []struct{ name, text string }{
		{"DEL", strings.Repeat("\x7f", 100)},
		{"all controls", controls.String()},
		{"Unicode and HTML", "雪🙂👩‍💻<>&\"\\\ufffd"},
		{"literal escapes", `\x7f\u007f\u009b\n\u2028`},
	} {
		t.Run(input.name, func(t *testing.T) {
			for _, value := range []any{input.text, map[string]any{input.text: []any{input.text, map[string]any{"value": input.text}}}} {
				encoded, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				for _, v := range []any{value, json.RawMessage(append(append([]byte(" \n\t"), encoded...), []byte("\n ")...))} {
					var rendered bytes.Buffer
					if err := writeInspectionJSON(context.Background(), &rendered, v); err != nil {
						t.Fatal(err)
					}
					counter := &reportCounter{}
					if err := writeInspectionJSON(context.Background(), counter, v); err != nil || counter.n != int64(rendered.Len()) {
						t.Fatalf("%T counter=%d actual=%d err=%v", v, counter.n, rendered.Len(), err)
					}
					ordinary, err := jsonValueSize(context.Background(), reflect.ValueOf(v))
					if err != nil || ordinary != int64(len(encoded)) {
						t.Fatalf("%T ordinary JSON count=%d expected=%d err=%v", v, ordinary, len(encoded), err)
					}
					wire, err := json.Marshal(v)
					if err != nil || !bytes.Equal(encoded, wire) {
						t.Fatalf("structured JSON identity changed for %T", v)
					}
					if rendered.Len() > len(encoded) {
						capBytes := (int64(rendered.Len()) + int64(len(encoded))) / 2
						capped := &reportCounter{limit: capBytes}
						if err := writeInspectionJSON(context.Background(), capped, v); !errors.Is(err, errReportBudget) || capped.n != 0 {
							t.Fatalf("%T cap=%d incorrectly admitted actual=%d: count=%d err=%v", v, capBytes, rendered.Len(), capped.n, err)
						}
					}
				}
			}
		})
	}
}

func TestInspectionDELAdmissionBeforeTextMaterialization(t *testing.T) {
	out, p := inspectionReportFixture(t, 1)
	p.AST.Data.Root.Properties["DEL"] = strings.Repeat("\x7f", 100)
	var actual bytes.Buffer
	if err := renderTypedInspectionReport(context.Background(), &actual, out, p); err != nil {
		t.Fatal(err)
	}
	counter := &reportCounter{}
	if err := renderTypedInspectionReport(context.Background(), counter, out, p); err != nil || counter.n != int64(actual.Len()) {
		t.Fatal(counter.n, actual.Len(), err)
	}
	// Before the repair the 100 literal DELs were each underestimated by 3 bytes.
	// This cap lies between that old estimate and the actual escaped report.
	capBytes := int64(actual.Len()) - 150
	probeCalls := 0
	p.AST.Data.Root.Properties["preencoding_probe"] = reportEncodingProbe{&probeCalls}
	_, err := encodeInspection(context.Background(), out, p, "text", map[string]int64{"response-bytes": capBytes})
	if !errors.Is(err, errReportBudget) || probeCalls != 0 {
		t.Fatalf("text materialized before exact DEL admission: err=%v encoding calls=%d", err, probeCalls)
	}
}

func TestInspectionTerminalSafeScalarCheckpoints(t *testing.T) {
	// A one-byte prefix followed by multibyte controls exercises checkpoint
	// boundaries that do not align with UTF-8 rune starts.
	value := "x" + strings.Repeat("\u009b", 8000) + strings.Repeat("\x7f", 8000)
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []any{value, json.RawMessage(encoded)} {
		ctx := &inspectionCheckpointContext{Context: context.Background(), stop: 5}
		sizer := jsonSizer{ctx: ctx, terminalSafe: true}
		if _, err := sizer.value(reflect.ValueOf(v)); !errors.Is(err, context.DeadlineExceeded) || ctx.calls < 5 {
			t.Fatalf("%T missing scalar checkpoint: calls=%d err=%v", v, ctx.calls, err)
		}
	}
}
