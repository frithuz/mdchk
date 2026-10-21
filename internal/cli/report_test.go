package cli

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
		got, err := reportSize(context.Background(), r, "json")
		if err != nil || got != int64(len(want)+1) {
			t.Fatalf("s=%q size=%d want=%d err=%v JSON=%s", s, got, len(want)+1, err, want)
		}
	}
}

func TestReportWholeRecordOmissionsAndReferences(t *testing.T) {
	r := result.NewLint("project")
	snapshot := result.Snapshot{View: "original", SHA256: strings.Repeat("0", 64), SizeBytes: 200}
	r.Errors = append(r.Errors, result.Error{ID: "read-error", Category: result.IO, Code: "read", Message: "failed", Scope: "operation", Path: "x.md"})
	r.CheckedFiles = append(r.CheckedFiles, "x.md")
	r.Files = append(r.Files, result.File{Path: "x.md", Analysis: "failed", WriteStatus: "not-requested", ReturnStatus: "not-requested", ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}})
	for i := 0; i < 100; i++ {
		r.Diagnostics = append(r.Diagnostics, result.Diagnostic{Path: "x.md", Rule: "MD013", Severity: "error", Snapshot: snapshot, Message: strings.Repeat("<&\n", 100), Range: result.Range{Start: result.Position{Line: i + 1, Column: 1}, End: result.Position{Line: i + 1, Column: 2}}, ByteRange: result.ByteRange{Start: i, End: i + 1}})
		r.Coverage = append(r.Coverage, result.Coverage{Reason: "parser-uncertain", Message: strings.Repeat("large", 100), Path: "x.md", Rule: "MD013", CauseIDs: []string{"read-error"}})
	}
	original, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	report, err := buildReport(context.Background(), r, "json", map[string]int64{"response-bytes": 5000})
	if err != nil {
		t.Fatal(err)
	}
	validateEmitted(t, t.TempDir(), report.data)
	if len(report.data) > 5000 || report.out.Complete || report.out.Status != "error" {
		t.Fatalf("bad bounded outcome: bytes=%d status=%s complete=%t", len(report.data), report.out.Status, report.out.Complete)
	}
	if len(report.out.Files) != 1 || len(report.out.Errors) != 1 {
		t.Fatal("lost mandatory records")
	}
	omitted := 0
	for _, o := range report.out.Omissions {
		if o.Count == nil || o.Reason != "output-limit" {
			t.Fatalf("bad omission: %+v", o)
		}
		if o.Section == "/diagnostics" {
			omitted += *o.Count
		}
	}
	if len(report.out.Diagnostics)+omitted != 100 {
		t.Fatal("inexact omission count")
	}
	ids := map[string]bool{}
	for _, e := range report.out.Errors {
		ids[e.ID] = true
	}
	for _, e := range report.out.Interruptions {
		ids[e.ID] = true
	}
	coverageIDs := map[string]bool{}
	for _, c := range report.out.Coverage {
		coverageIDs[c.ID] = true
		for _, id := range c.CauseIDs {
			if !ids[id] {
				t.Fatal("dangling cause")
			}
		}
	}
	for _, f := range report.out.Files {
		for _, id := range f.CoverageIDs {
			if !coverageIDs[id] {
				t.Fatal("dangling coverage")
			}
		}
	}
	after, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, after) {
		t.Fatal("reporter mutated input")
	}
}

func TestReportExactFitAndOneByteOver(t *testing.T) {
	r := result.NewLint("project")
	r.Errors = append(r.Errors, result.Error{Category: result.IO, Scope: "operation", Code: "read", Message: "read failed"})
	// The numeric effective budget itself changes encoding size. Iterate once to
	// make the asserted exact boundary include that visible limit value.
	limits := map[string]int64{"response-bytes": 9999}
	first, err := buildReport(context.Background(), r, "json", limits)
	if err != nil {
		t.Fatal(err)
	}
	limits["response-bytes"] = int64(len(first.data))
	first, err = buildReport(context.Background(), r, "json", limits)
	if err != nil {
		t.Fatal(err)
	}
	limits["response-bytes"] = int64(len(first.data))
	exact, err := buildReport(context.Background(), r, "json", limits)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(exact.data)) != limits["response-bytes"] {
		t.Fatalf("not exact: %d vs%d", len(exact.data), limits["response-bytes"])
	}
	limits["response-bytes"]--
	_, err = buildReport(context.Background(), r, "json", limits)
	if !errors.Is(err, errReportBudget) {
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
			want := exitIO
			if internal {
				r.Errors = append(r.Errors, result.Error{Category: result.Internal, Code: "internal", Message: "failed", Scope: "operation"})
				want = 14
			}
			if got := reportResult(r, "json", w, nil); got != want {
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
	text, err := buildReport(context.Background(), r, "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(text.data, []byte{'\r'}) || bytes.Contains(text.data, []byte("\nforged")) {
		t.Fatalf("forged record: %s", text.data)
	}
	if strings.Count(string(text.data), "\n") != 5 {
		t.Fatalf("physical lines=%d", strings.Count(string(text.data), "\n"))
	}
	encoded, err := buildReport(context.Background(), r, "json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Errors []result.Error `json:"errors"`
	}
	if err := json.Unmarshal(encoded.data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Errors[0].Message, hostile) {
		t.Fatal("JSON string changed")
	}
}

func TestReportOversizeErrorKeepsSemanticIdentity(t *testing.T) {
	r := result.NewLint("project")
	r.Errors = append(r.Errors, result.Error{ID: "configuration-error", Category: result.Configuration, Scope: "operation", Code: "config-definition", Path: "雪.yml", Message: strings.Repeat("<&\n", 10000)})
	encoded, err := buildReport(context.Background(), r, "json", map[string]int64{"response-bytes": 2000})
	if err != nil {
		t.Fatal(err)
	}
	validateEmitted(t, t.TempDir(), encoded.data)
	if len(encoded.data) > 2000 || len(encoded.out.Errors) != 1 || encoded.out.Errors[0].Category != result.Configuration || encoded.out.Errors[0].Path != "雪.yml" || encoded.out.Errors[0].Code != "config-definition" {
		t.Fatalf("lost error identity: %+v", encoded.out)
	}
	if encoded.out.Status != "error" || encoded.out.Complete {
		t.Fatalf("oversize error outcome: %s %t", encoded.out.Status, encoded.out.Complete)
	}
	var count int
	for _, o := range encoded.out.Omissions {
		if o.Section == "/errors" {
			count += *o.Count
		}
	}
	if count != 1 {
		t.Fatalf("error omission count=%d", count)
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
func TestReportReservedDeadlineEnvelope(t *testing.T) {
	r := result.NewLint("project")
	for i := 0; i < 100; i++ {
		r.Diagnostics = append(r.Diagnostics, result.Diagnostic{Path: "x.md", Rule: "MD013", Message: "finding"})
	}
	measure := &reportCheckpointContext{Context: context.Background(), stop: 1000000}
	if _, err := deadlineReport(measure, r, "json", nil); err != nil {
		t.Fatal(err)
	}
	ctx := &reportCheckpointContext{Context: context.Background(), stop: measure.checks + 20}
	got, err := buildReport(ctx, r, "json", nil)
	if err != nil {
		t.Fatal(err)
	}
	validateEmitted(t, t.TempDir(), got.data)
	if got.out.Complete || got.out.Status != "incomplete" {
		t.Fatalf("deadline lost: %s %t", got.out.Status, got.out.Complete)
	}
	if len(got.out.Interruptions) != 1 || got.out.Interruptions[0].Reason != "deadline-exceeded" || got.out.Interruptions[0].Phase != "reporting" {
		t.Fatalf("missing report deadline: %+v", got.out.Interruptions)
	}
	count := 0
	for _, o := range got.out.Omissions {
		if o.Section == "/diagnostics" {
			count += *o.Count
		}
	}
	if count != 100 {
		t.Fatalf("omissions=%d", count)
	}
}

func TestReportFeasibilityReservesManifestInAddition(t *testing.T) {
	r := result.NewLint("project")
	limits := map[string]int64{"response-bytes": 65536}
	if err := reportFeasibility(r, "json", limits); err != nil {
		t.Fatal(err)
	}
	r.Files = append(r.Files, result.File{Path: "x.md", Analysis: "not-started", WriteStatus: "not-requested", ReturnStatus: "not-requested", ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}})
	if err := reportFeasibility(r, "json", limits); !errors.Is(err, errReportBudget) {
		t.Fatalf("manifest not reserved: %v", err)
	}
	limits["response-bytes"] = 131072
	if err := reportFeasibility(r, "json", limits); err != nil {
		t.Fatal(err)
	}
	// Raising the operator reserve consumes capacity before analysis as well.
	limits["report-reserve-bytes"] = 131072
	if err := reportFeasibility(r, "json", limits); !errors.Is(err, errReportBudget) {
		t.Fatalf("operator reserve not enforced: %v", err)
	}
}
