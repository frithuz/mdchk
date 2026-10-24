//go:build linux || darwin || windows

package resource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func TestFilesystemReportAdmissionAndOwnership(t *testing.T) {
	f := newFilesystemFixture(t)
	request := f.request(t, "asset.md", TextContent, RegularFile)
	observation, err := f.operation.Record(request, ObservationSpec{Existence: File, Content: Unavailable, Anchors: NotRequested})
	if err != nil {
		t.Fatal(err)
	}
	initial := result.NewLint("project")
	initial.Errors = append(initial.Errors, result.Error{ID: "resource-native-error-1", Scope: "observation", Category: result.IO, Code: "read-failure", Path: "asset.md", Message: "unrelated target"})
	report, err := NewFilesystemReport(f.operation, initial)
	if err != nil {
		t.Fatal(err)
	}
	other := newFilesystemFixture(t)
	candidate, err := f.operation.NewView(Candidate, nil)
	if err != nil {
		t.Fatal(err)
	}
	unknown := f.request(t, "other.md", TextContent, RegularFile)
	valid := FilesystemFailure{Reader: f.reader, Request: request, Err: io.ErrUnexpectedEOF}
	var nilFault *FilesystemReadError
	for _, failures := range [][]FilesystemFailure{
		{valid, {Reader: f.reader, Request: request, Err: &FilesystemReadError{Stop: errors.New("not a stop")}}},
		{{Reader: other.reader, Request: request, Err: io.ErrUnexpectedEOF}},
		{{Reader: f.reader, Request: unknown, Err: io.ErrUnexpectedEOF}},
		{{Reader: f.reader, Request: request, Err: nilFault}},
		{{Reader: f.reader, Request: request, Err: &FilesystemReadError{}}},
	} {
		if err := report.Append(f.view, []Observation{observation}, failures); !errors.Is(err, ErrContract) {
			t.Fatal("invalid batch accepted", err)
		}
		if !reflect.DeepEqual(report.state.out, initial) || len(report.state.io)+len(report.state.stops)+len(report.state.ordinals) != 0 {
			t.Fatal("rejected batch mutated report")
		}
	}
	if err := report.Append(candidate, []Observation{observation}, nil); !errors.Is(err, ErrContract) {
		t.Fatal("foreign view admitted", err)
	}
	if err := report.Append(f.view, []Observation{observation, observation}, nil); !errors.Is(err, ErrContract) {
		t.Fatal("duplicate request admitted", err)
	}
	alias := *report
	// Reassignment of caller storage has no effect on the transferred value.
	initial = result.NewLint("new-report")
	if err := alias.Append(f.view, []Observation{observation}, []FilesystemFailure{valid}); err != nil {
		t.Fatal(err)
	}
	out, err := report.Finish()
	if err != nil || len(out.Errors) != 2 || out.Errors[0].Message != "unrelated target" || out.Errors[1].ID != "resource-native-error-2" {
		t.Fatal("ownership or occupied ID lost", out, err)
	}
	snapshot := fmt.Sprintf("%+v", out)
	if err := alias.Append(f.view, []Observation{observation}, []FilesystemFailure{valid}); !errors.Is(err, ErrContract) {
		t.Fatal("copied handle bypassed consumption", err)
	}
	if _, err := alias.Finish(); !errors.Is(err, ErrContract) {
		t.Fatal("copied handle finished twice", err)
	}
	if snapshot != fmt.Sprintf("%+v", out) {
		t.Fatal("consumed alias mutated transferred result")
	}
	var zero FilesystemReport
	if err := zero.Append(f.view, nil, nil); !errors.Is(err, ErrContract) {
		t.Fatal(err)
	}
	if _, err := zero.Finish(); !errors.Is(err, ErrContract) {
		t.Fatal(err)
	}
	if _, err := NewFilesystemReport(nil, initial); !errors.Is(err, ErrContract) {
		t.Fatal(err)
	}
	if _, err := NewFilesystemReport(&Operation{}, initial); !errors.Is(err, ErrContract) {
		t.Fatal(err)
	}
}

func TestFilesystemReportDistinctCauseAllocationGrowth(t *testing.T) {
	f := newFilesystemFixture(t)
	second, err := f.filesystem.Reader(f.operation, f.proof)
	if err != nil {
		t.Fatal(err)
	}
	var prior float64
	for _, count := range []int{100, 400, 1600} {
		observations := make([]Observation, count)
		failures := make([]FilesystemFailure, count)
		keys := make([]string, count)
		for i := range count {
			keys[i] = fmt.Sprintf("resource-%d.md", i)
			request := f.request(t, keys[i], TextContent, RegularFile)
			observations[i], err = f.operation.Record(request, ObservationSpec{Existence: File, Content: Unavailable, Anchors: NotRequested})
			if err != nil {
				t.Fatal(err)
			}
			reader := f.reader
			if i%2 != 0 {
				reader = second
			}
			failures[i] = FilesystemFailure{Reader: reader, Request: request, Err: &FilesystemReadError{IO: io.ErrUnexpectedEOF, Stop: context.Canceled}}
		}
		// Request/source/budget setup is outside measurement. Distinct failures,
		// multiple reader handles and sparse occupied IDs exercise shared indexing.
		run := func() result.Result {
			initial := result.NewLint("project")
			for _, n := range []int{1, 17, 991} {
				initial.Errors = append(initial.Errors, result.Error{ID: fmt.Sprintf("resource-native-error-%d", n), Scope: "observation", Category: result.IO, Code: "read-failure", Path: "target.md", Message: "existing target"})
			}
			initial.Interruptions = append(initial.Interruptions, result.Interruption{ID: "resource-native-stop-17", Reason: "cancelled", Phase: "acquisition", Path: "target.md", Message: "existing target"})
			report, err := NewFilesystemReport(f.operation, initial)
			if err != nil {
				panic(err)
			}
			if err := report.Append(f.view, observations, failures); err != nil {
				panic(err)
			}
			out, err := report.Finish()
			if err != nil {
				panic(err)
			}
			return out
		}
		allocations := testing.AllocsPerRun(3, func() { _ = run() })
		t.Logf("distinct=%d allocations=%.0f", count, allocations)
		if prior > 0 && allocations > prior*5.5 {
			t.Fatal("projection allocations grow faster than batch size", prior, allocations)
		}
		prior = allocations
		out := run()
		if len(out.Errors) != count+3 || len(out.Interruptions) != count+1 || len(out.Coverage) != count {
			t.Fatal("lost distinct causes", count)
		}
		causes := map[string]string{}
		for _, failure := range out.Errors {
			if _, ok := causes[failure.ID]; ok {
				t.Fatal("duplicate cause", failure.ID)
			}
			causes[failure.ID] = failure.Path
		}
		for _, stop := range out.Interruptions {
			if _, ok := causes[stop.ID]; ok {
				t.Fatal("duplicate cause", stop.ID)
			}
			causes[stop.ID] = stop.Path
		}
		for i, coverage := range out.Coverage {
			if len(coverage.CauseIDs) != 2 {
				t.Fatal("lost cause", i, coverage)
			}
			for _, id := range coverage.CauseIDs {
				if causes[id] != keys[i] {
					t.Fatal("wrong resource attribution", i, id, causes[id], keys[i])
				}
			}
		}
	}
}
