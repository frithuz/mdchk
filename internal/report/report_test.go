package report

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func TestReportExactJSONSize(t *testing.T) {
	for _, s := range []string{"", "<>&\u2028\u2029\"\\\r\n\t\x00", "雪🙂é\xff", strings.Repeat("\\\"<&\n", 1000)} {
		r := result.NewLint("project")
		r.Errors = append(r.Errors, result.Error{ID: "e", Category: result.IO, Code: "read", Scope: "operation", Message: s})
		want, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ReportSize(context.Background(), r, "json")
		if err != nil || got != int64(len(want)+1) {
			t.Fatalf("s=%q size=%d want=%d err=%v JSON=%s", s, got, len(want)+1, err, want)
		}
	}
}

func TestReportExactFitAndOneByteOver(t *testing.T) {
	r := result.NewLint("project")
	r.Errors = append(r.Errors, result.Error{Category: result.IO, Scope: "operation", Code: "read", Message: "read failed"})
	// The numeric effective budget itself changes encoding size. Iterate once to
	// make the asserted exact boundary include that visible limit value.
	limits := map[string]int64{"response-bytes": 9999}
	first, err := BuildReport(context.Background(), r, "json", limits)
	if err != nil {
		t.Fatal(err)
	}
	limits["response-bytes"] = int64(len(first.Data))
	first, err = BuildReport(context.Background(), r, "json", limits)
	if err != nil {
		t.Fatal(err)
	}
	limits["response-bytes"] = int64(len(first.Data))
	exact, err := BuildReport(context.Background(), r, "json", limits)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(exact.Data)) != limits["response-bytes"] {
		t.Fatalf("not exact: %d vs%d", len(exact.Data), limits["response-bytes"])
	}
	limits["response-bytes"]--
	_, err = BuildReport(context.Background(), r, "json", limits)
	if !errors.Is(err, ErrReportBudget) {
		t.Fatalf("essential overflow=%v", err)
	}
}

type reportFailWriter struct {
	prefix int
	err    error
}

func (w reportFailWriter) Write(p []byte) (int, error) {
	if w.prefix > len(p) {
		return len(p), w.err
	}
	return w.prefix, w.err
}
func TestReportOutputFailuresAndPrecedence(t *testing.T) {
	for _, internal := range []bool{false, true} {
		for _, w := range []io.Writer{reportFailWriter{err: io.ErrClosedPipe}, reportFailWriter{prefix: 2, err: io.ErrClosedPipe}, reportFailWriter{prefix: 2}} {
			r := result.NewLint("project")
			want := ExitIO
			if internal {
				r.Errors = append(r.Errors, result.Error{Category: result.Internal, Code: "internal", Message: "failed", Scope: "operation"})
				want = 14
			}
			if got := ReportResult(r, "json", w, nil); got != want {
				t.Fatalf("got%d want%d", got, want)
			}
		}
	}
}
func TestReportTextEscapesEveryRecordFieldAndJSONPreserves(t *testing.T) {
	hostile := "snow雪\r\nforged"
	r := result.NewLint("project")
	r.Errors = append(r.Errors, result.Error{Category: result.IO, Scope: "operation", Code: hostile, Message: hostile, Path: hostile})
	r.Coverage = append(r.Coverage, result.Coverage{Reason: hostile, Message: hostile, Path: hostile, Rule: hostile})
	r.SkippedFiles = append(r.SkippedFiles, result.SkippedFile{Path: hostile, Reason: hostile, Message: hostile})
	r.Interruptions = append(r.Interruptions, result.Interruption{Phase: "analysis", Reason: hostile, Message: hostile, Path: hostile})
	text, err := BuildReport(context.Background(), r, "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(text.Data, []byte{'\r'}) || bytes.Contains(text.Data, []byte("\nforged")) {
		t.Fatalf("forged record: %s", text.Data)
	}
	if strings.Count(string(text.Data), "\n") != 5 {
		t.Fatalf("physical lines=%d", strings.Count(string(text.Data), "\n"))
	}
	encoded, err := BuildReport(context.Background(), r, "json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Errors []result.Error `json:"errors"`
	}
	if err := json.Unmarshal(encoded.Data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Errors[0].Message, hostile) {
		t.Fatal("JSON string changed")
	}
}

type reportCheckpointContext struct {
	context.Context
	checks, stop int
}

func (c *reportCheckpointContext) Err() error {
	c.checks++
	if c.checks > c.stop {
		return context.DeadlineExceeded
	}
	return nil
}
func TestReportFeasibilityReservesManifestInAddition(t *testing.T) {
	r := result.NewLint("project")
	limits := map[string]int64{"response-bytes": 65536}
	if err := reportFeasibility(r, "json", limits); err != nil {
		t.Fatal(err)
	}
	r.Files = append(r.Files, result.File{Path: "x.md", Analysis: "not-started", WriteStatus: "not-requested", ReturnStatus: "not-requested", ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}})
	if err := reportFeasibility(r, "json", limits); !errors.Is(err, ErrReportBudget) {
		t.Fatalf("manifest not reserved: %v", err)
	}
	limits["response-bytes"] = 131072
	if err := reportFeasibility(r, "json", limits); err != nil {
		t.Fatal(err)
	}
	// Raising the operator reserve consumes capacity before analysis as well.
	limits["report-reserve-bytes"] = 131072
	if err := reportFeasibility(r, "json", limits); !errors.Is(err, ErrReportBudget) {
		t.Fatalf("operator reserve not enforced: %v", err)
	}
}
