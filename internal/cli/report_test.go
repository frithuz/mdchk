package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/report"
	"github.com/frithuz/mdchk/internal/result"
)

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
	report, err := report.BuildReport(context.Background(), r, "json", map[string]int64{"response-bytes": 5000})
	if err != nil {
		t.Fatal(err)
	}
	validateEmitted(t, t.TempDir(), report.Data)
	if len(report.Data) > 5000 || report.Out.Complete || report.Out.Status != "error" {
		t.Fatalf("bad bounded outcome: bytes=%d status=%s complete=%t", len(report.Data), report.Out.Status, report.Out.Complete)
	}
	if len(report.Out.Files) != 1 || len(report.Out.Errors) != 1 {
		t.Fatal("lost mandatory records")
	}
	omitted := 0
	for _, o := range report.Out.Omissions {
		if o.Count == nil || o.Reason != "output-limit" {
			t.Fatalf("bad omission: %+v", o)
		}
		if o.Section == "/diagnostics" {
			omitted += *o.Count
		}
	}
	if len(report.Out.Diagnostics)+omitted != 100 {
		t.Fatal("inexact omission count")
	}
	ids := map[string]bool{}
	for _, e := range report.Out.Errors {
		ids[e.ID] = true
	}
	for _, e := range report.Out.Interruptions {
		ids[e.ID] = true
	}
	coverageIDs := map[string]bool{}
	for _, c := range report.Out.Coverage {
		coverageIDs[c.ID] = true
		for _, id := range c.CauseIDs {
			if !ids[id] {
				t.Fatal("dangling cause")
			}
		}
	}
	for _, f := range report.Out.Files {
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

func TestReportOversizeErrorKeepsSemanticIdentity(t *testing.T) {
	r := result.NewLint("project")
	r.Errors = append(r.Errors, result.Error{ID: "configuration-error", Category: result.Configuration, Scope: "operation", Code: "config-definition", Path: "雪.yml", Message: strings.Repeat("<&\n", 10000)})
	encoded, err := report.BuildReport(context.Background(), r, "json", map[string]int64{"response-bytes": 2000})
	if err != nil {
		t.Fatal(err)
	}
	validateEmitted(t, t.TempDir(), encoded.Data)
	if len(encoded.Data) > 2000 || len(encoded.Out.Errors) != 1 || encoded.Out.Errors[0].Category != result.Configuration || encoded.Out.Errors[0].Path != "雪.yml" || encoded.Out.Errors[0].Code != "config-definition" {
		t.Fatalf("lost error identity: %+v", encoded.Out)
	}
	if encoded.Out.Status != "error" || encoded.Out.Complete {
		t.Fatalf("oversize error outcome: %s %t", encoded.Out.Status, encoded.Out.Complete)
	}
	var count int
	for _, o := range encoded.Out.Omissions {
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
	if _, err := report.DeadlineReport(measure, r, "json", nil); err != nil {
		t.Fatal(err)
	}
	ctx := &reportCheckpointContext{Context: context.Background(), stop: measure.checks + 20}
	got, err := report.BuildReport(ctx, r, "json", nil)
	if err != nil {
		t.Fatal(err)
	}
	validateEmitted(t, t.TempDir(), got.Data)
	if got.Out.Complete || got.Out.Status != "incomplete" {
		t.Fatalf("deadline lost: %s %t", got.Out.Status, got.Out.Complete)
	}
	if len(got.Out.Interruptions) != 1 || got.Out.Interruptions[0].Reason != "deadline-exceeded" || got.Out.Interruptions[0].Phase != "reporting" {
		t.Fatalf("missing report deadline: %+v", got.Out.Interruptions)
	}
	count := 0
	for _, o := range got.Out.Omissions {
		if o.Section == "/diagnostics" {
			count += *o.Count
		}
	}
	if count != 100 {
		t.Fatalf("omissions=%d", count)
	}
}
