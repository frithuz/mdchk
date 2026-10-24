//go:build linux || darwin || windows

package resource_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
)

// Construct the real native authority and receipt. The errors below are injected
// only at the result projection boundary; native fault acquisition has separate
// lifecycle tests.
func newFilesystemReportFixture(t *testing.T) (reportFixture, *resource.FilesystemReader) {
	t.Helper()
	f, readers := newFilesystemReportReadersFixture(t, "雪 [one](asset.md)\r\n[two](asset.md)\n")
	return f, readers[0]
}

func newFilesystemReportReadersFixture(t *testing.T, text string) (reportFixture, [2]*resource.FilesystemReader) {
	t.Helper()
	anchor, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	docs := filepath.Join(anchor, "docs")
	if err := os.Mkdir(docs, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(docs, "a.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	budget, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	native, err := resource.OpenFilesystemRoot(context.Background(), budget, anchor, resource.DefaultFilesystemLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := native.Close(); err != nil {
			t.Error(err)
		}
	})
	source := parser.NewSource([]byte(text))
	target, err := native.BindTarget(path, "docs/a.md", source)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := resource.NewSettings(parser.ParseOptions{}, "index.md", "github", "native-report-test-v1")
	if err != nil {
		t.Fatal(err)
	}
	operation, err := resource.NewOperation(context.Background(), budget, native.Root(), settings, []resource.Target{target.Target()})
	if err != nil {
		t.Fatal(err)
	}
	view, err := operation.NewView(resource.Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := native.Reader(operation, target)
	if err != nil {
		t.Fatal(err)
	}
	second, err := native.Reader(operation, target)
	if err != nil {
		t.Fatal(err)
	}
	return reportFixture{operation: operation, view: view, source: source, text: text}, [2]*resource.FilesystemReader{reader, second}
}

func filesystemReportRequest(t *testing.T, f reportFixture, last bool) resource.Request {
	t.Helper()
	return filesystemReportNamedRequest(t, f, "asset.md", last)
}

func filesystemReportNamedRequest(t *testing.T, f reportFixture, destination string, last bool) resource.Request {
	t.Helper()
	link := f.request(t, destination, last, resource.TextContent).Link()
	normalizer, err := resource.NewNormalizer(f.view, resource.DefaultPathLimits(), resource.NativeNamePolicy)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizer.Normalize(link)
	if err != nil {
		t.Fatal(err)
	}
	key, ok := normalized.Location().Key()
	if !ok || key.String() != "docs/"+destination {
		t.Fatalf("native source membership did not normalize reference: %v, %v", key, ok)
	}
	request, err := f.view.Request(resource.RequestSpec{Key: key, Link: link, Capability: resource.TextContent, Required: resource.RegularFile, Class: normalized.Class()})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestFilesystemErrorProjectionRetainsFactsAndSchema(t *testing.T) {
	for _, primary := range []resource.ProblemKind{resource.ReadFailure, resource.Cancelled} {
		t.Run(string(primary), func(t *testing.T) {
			f, reader := newFilesystemReportFixture(t)
			requests := []resource.Request{filesystemReportRequest(t, f, false), filesystemReportRequest(t, f, true)}
			availability := resource.Failed
			if primary == resource.Cancelled {
				availability = resource.Interrupted
			}
			spec := resource.ObservationSpec{Existence: resource.File, Content: availability, Anchors: resource.NotRequested, Problem: resource.Problem{Kind: primary, Message: "synthetic acquisition boundary failure"}}
			observations := make([]resource.Observation, len(requests))
			for i, request := range requests {
				observations[i] = f.outcome(t, request, spec)
				if observations[i].Existence() != resource.File || observations[i].Treatment() != resource.ContextIncomplete {
					t.Fatal("known file deficit was lost", observations[i])
				}
			}
			out := f.result(t, true)
			wantDiagnostic := out.Diagnostics[0]
			fault := &resource.FilesystemReadError{IO: io.ErrUnexpectedEOF, Stop: context.Canceled}
			failures := make([]resource.FilesystemFailure, len(requests))
			for i, request := range requests {
				failures[i] = resource.FilesystemFailure{Reader: reader, Request: request, Err: fault}
			}
			out = filesystemReportBatch(t, f, out, observations, failures)
			engine.FinalizeLintResult(&out)
			if out.Status != "error" || out.Complete || len(out.Errors) != 1 || out.Errors[0].Category != result.IO || out.Errors[0].Path != "docs/asset.md" || len(out.Interruptions) != 1 || out.Interruptions[0].Reason != "cancelled" || len(out.Coverage) != 2 || len(out.Diagnostics) != 1 {
				t.Fatalf("structured native causes or source deficits lost: %+v", out)
			}
			wantRanges := []result.Range{{Start: parser.Position{Line: 1, Column: 9}, End: parser.Position{Line: 1, Column: 17}}, {Start: parser.Position{Line: 2, Column: 7}, End: parser.Position{Line: 2, Column: 15}}}
			for i, coverage := range out.Coverage {
				if coverage.Path != "docs/a.md" || coverage.Rule != "MD051" || coverage.Reason != result.ContextUnavailable || coverage.Range == nil || *coverage.Range != wantRanges[i] || coverage.Snapshot == nil || *coverage.Snapshot != *out.Files[0].InputSnapshot || len(coverage.CauseIDs) != 2 || !slices.Contains(coverage.CauseIDs, out.Errors[0].ID) || !slices.Contains(coverage.CauseIDs, out.Interruptions[0].ID) {
					t.Fatalf("coverage %d lost source identity or additional causes: %+v", i, coverage)
				}
			}
			wantDiagnostic.ID = out.Diagnostics[0].ID
			if wantDiagnostic.ID == "" || !reflect.DeepEqual(wantDiagnostic, out.Diagnostics[0]) || len(out.Files[0].CoverageIDs) != 2 {
				t.Fatal("native projection changed independent source observations", out)
			}
			assertResourceTargetsUnchanged(t, out)
			validateResourceResult(t, out)
		})
	}

	t.Run("target-path-namespace-collision", func(t *testing.T) {
		f, reader := newFilesystemReportFixture(t)
		request := filesystemReportRequest(t, f, false)
		observation := f.outcome(t, request, resource.ObservationSpec{Existence: resource.File, Content: resource.Unavailable, Anchors: resource.NotRequested})
		out := f.result(t, true)
		// Target report paths use the caller's reporting base, whereas resource
		// keys use the pinned root. Equal text can therefore identify unrelated
		// files. These seeded target failures must not become resource causes.
		const targetIO = "target acquisition failed in a different reporting namespace"
		const targetStop = "target acquisition cancelled in a different reporting namespace"
		out.Errors = append(out.Errors, result.Error{ID: "target-error", Category: result.IO, Code: string(resource.ReadFailure), Scope: "observation", Path: request.Key().String(), Message: targetIO})
		out.Interruptions = append(out.Interruptions, result.Interruption{ID: "target-stop", Reason: "cancelled", Phase: "acquisition", Path: request.Key().String(), Message: targetStop})
		out = filesystemReportBatch(t, f, out, []resource.Observation{observation}, []resource.FilesystemFailure{{Reader: reader, Request: request, Err: &resource.FilesystemReadError{IO: io.ErrUnexpectedEOF, Stop: context.Canceled}}})
		engine.FinalizeLintResult(&out)
		if out.Status != "error" || out.Complete || len(out.Errors) != 2 || len(out.Interruptions) != 2 || len(out.Coverage) != 1 {
			t.Fatalf("equal target/resource path strings merged separate causes: %+v", out)
		}
		var targetErrorID, resourceErrorID, targetStopID, resourceStopID string
		for _, failure := range out.Errors {
			switch failure.Scope {
			case "observation":
				if failure.Message != targetIO {
					t.Fatal("target error changed", failure)
				}
				targetErrorID = failure.ID
			case "operation":
				resourceErrorID = failure.ID
			}
		}
		for _, stop := range out.Interruptions {
			if stop.Message == targetStop {
				targetStopID = stop.ID
			} else {
				resourceStopID = stop.ID
			}
		}
		coverage := out.Coverage[0]
		wantRange := result.Range{Start: parser.Position{Line: 1, Column: 9}, End: parser.Position{Line: 1, Column: 17}}
		if targetErrorID == "" || resourceErrorID == "" || targetStopID == "" || resourceStopID == "" || coverage.Path != "docs/a.md" || coverage.Range == nil || *coverage.Range != wantRange || coverage.Snapshot == nil || *coverage.Snapshot != *out.Files[0].InputSnapshot || len(coverage.CauseIDs) != 2 || !slices.Contains(coverage.CauseIDs, resourceErrorID) || !slices.Contains(coverage.CauseIDs, resourceStopID) || slices.Contains(coverage.CauseIDs, targetErrorID) || slices.Contains(coverage.CauseIDs, targetStopID) {
			t.Fatalf("source coverage borrowed unrelated target causes: %+v", out)
		}
		assertResourceTargetsUnchanged(t, out)
		validateResourceResult(t, out)
	})

	t.Run("satisfied-content-late-close", func(t *testing.T) {
		f, reader := newFilesystemReportFixture(t)
		request := filesystemReportRequest(t, f, false)
		content := parser.NewSource([]byte("# Acquired resource\n"))
		evidence, err := f.operation.ValidateContent(content)
		if err != nil {
			t.Fatal(err)
		}
		observation := f.outcome(t, request, resource.ObservationSpec{Existence: resource.File, Content: resource.Available, Anchors: resource.NotRequested, Source: content, Validated: evidence})
		if observation.Treatment() != resource.Complete {
			t.Fatal("satisfied content became a deficit", observation.Treatment())
		}
		out := f.result(t, true)
		wantSnapshot := *out.Files[0].InputSnapshot
		// This carrier models a close failure observed after validated content,
		// alongside a stop. It must survive even without source coverage.
		out = filesystemReportBatch(t, f, out, []resource.Observation{observation}, []resource.FilesystemFailure{{Reader: reader, Request: request, Err: &resource.FilesystemReadError{IO: io.ErrClosedPipe, Stop: context.Canceled}}})
		engine.FinalizeLintResult(&out)
		if out.Status != "error" || out.Complete || len(out.Errors) != 1 || out.Errors[0].Category != result.IO || out.Errors[0].Path != "docs/asset.md" || len(out.Interruptions) != 1 || len(out.Coverage) != 0 || len(out.Files[0].CoverageIDs) != 0 || *out.Files[0].InputSnapshot != wantSnapshot {
			t.Fatalf("late cause lost or satisfied content acquired invented coverage: %+v", out)
		}
		assertResourceTargetsUnchanged(t, out)
		validateResourceResult(t, out)
	})
}

func filesystemReportBatch(t *testing.T, f reportFixture, initial result.Result, observations []resource.Observation, failures []resource.FilesystemFailure) result.Result {
	t.Helper()
	report, err := resource.NewFilesystemReport(f.operation, initial)
	if err != nil {
		t.Fatal(err)
	}
	if err := report.Append(f.view, observations, failures); err != nil {
		t.Fatal(err)
	}
	out, err := report.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type filesystemCauseOracle struct {
	key      string
	location result.Range
	snapshot result.Snapshot
}

func filesystemRequestOracle(request resource.Request, phase resource.Phase) filesystemCauseOracle {
	link := request.Link()
	text := string(link.Source().Bytes())
	position := func(offset int) parser.Position {
		prefix := text[:offset]
		lineStart := strings.LastIndex(prefix, "\n") + 1
		return parser.Position{Line: strings.Count(prefix, "\n") + 1, Column: utf8.RuneCountInString(prefix[lineStart:]) + 1}
	}
	hash := sha256.Sum256([]byte(text))
	return filesystemCauseOracle{key: request.Key().String(), location: result.Range{Start: position(link.Span().Start()), End: position(link.Span().End())}, snapshot: result.Snapshot{View: string(phase), SHA256: fmt.Sprintf("%x", hash), SizeBytes: len(text)}}
}

func assertFilesystemCauseOracles(t *testing.T, out result.Result, expected []filesystemCauseOracle) {
	t.Helper()
	seen := map[string]bool{}
	addID := func(id string) {
		if id == "" || seen[id] {
			t.Fatalf("empty or duplicate raw record ID %q before canonicalization: %+v", id, out)
		}
		seen[id] = true
	}
	causes := map[string]string{}
	for _, failure := range out.Errors {
		addID(failure.ID)
		if failure.Category != result.IO || failure.Scope != "operation" {
			t.Fatal("resource I/O evidence changed category or scope", failure)
		}
		causes[failure.ID] = "io:" + failure.Path
	}
	for _, stop := range out.Interruptions {
		addID(stop.ID)
		if stop.Reason != "cancelled" || stop.Phase != "acquisition" {
			t.Fatal("resource stop changed identity", stop)
		}
		causes[stop.ID] = "stop:" + stop.Path
	}
	if len(out.Coverage) != len(expected) {
		t.Fatalf("source coverage count = %d, want %d", len(out.Coverage), len(expected))
	}
	matched := make([]bool, len(expected))
	for _, coverage := range out.Coverage {
		// Coverage IDs are assigned by the engine finalizer; cause IDs must
		// already be nonempty and globally unique before that step.
		if coverage.ID != "" {
			addID(coverage.ID)
		}
		index := -1
		for i, want := range expected {
			if !matched[i] && coverage.Path == "docs/a.md" && coverage.Rule == "MD051" && coverage.Range != nil && *coverage.Range == want.location && coverage.Snapshot != nil && *coverage.Snapshot == want.snapshot {
				index = i
				break
			}
		}
		if index < 0 {
			t.Fatal("coverage lost its exact source range/snapshot", coverage)
		}
		matched[index] = true
		want := expected[index]
		got := make([]string, len(coverage.CauseIDs))
		for i, id := range coverage.CauseIDs {
			got[i] = causes[id]
		}
		slices.Sort(got)
		if !reflect.DeepEqual(got, []string{"io:" + want.key, "stop:" + want.key}) {
			t.Fatalf("source coverage borrowed another request's cause: %+v, resolved %v, want resource %s", coverage, got, want.key)
		}
	}
}

func filesystemUnavailableBatch(t *testing.T, f reportFixture, readers []*resource.FilesystemReader, destinations []string) ([]resource.Observation, []resource.FilesystemFailure, []filesystemCauseOracle) {
	t.Helper()
	observations := make([]resource.Observation, len(destinations))
	failures := make([]resource.FilesystemFailure, len(destinations))
	oracles := make([]filesystemCauseOracle, len(destinations))
	for i, destination := range destinations {
		request := filesystemReportNamedRequest(t, f, destination, i == len(destinations)-1)
		observations[i] = f.outcome(t, request, resource.ObservationSpec{Existence: resource.File, Content: resource.Unavailable, Anchors: resource.NotRequested})
		failures[i] = resource.FilesystemFailure{Reader: readers[i%len(readers)], Request: request, Err: &resource.FilesystemReadError{IO: io.ErrUnexpectedEOF, Stop: context.Canceled}}
		oracles[i] = filesystemRequestOracle(request, f.view.Phase())
	}
	return observations, failures, oracles
}

func filesystemReportViews(t *testing.T, original reportFixture) []reportFixture {
	t.Helper()
	views := []reportFixture{original}
	for _, phase := range []resource.Phase{resource.Candidate, resource.Saved} {
		text := "雪 " + string(phase) + " replacement\n[shared](asset.md)\r\n[unique](" + string(phase) + ".md)\n"
		source := parser.NewSource([]byte(text))
		view, err := original.operation.NewView(phase, []resource.Replacement{{TargetID: "docs/a.md", Source: source}})
		if err != nil {
			t.Fatal(err)
		}
		views = append(views, reportFixture{operation: original.operation, view: view, source: source, text: text})
	}
	return views
}

func TestFilesystemReportMultipleReadersKeepCauseIdentity(t *testing.T) {
	f, readers := newFilesystemReportReadersFixture(t, "雪 [one](asset.md)\r\n[two](other.md)\n[three](third.md)\n[repeat](asset.md)\n")
	observations, failures, oracles := filesystemUnavailableBatch(t, f, []*resource.FilesystemReader{readers[0], readers[1]}, []string{"asset.md", "other.md", "third.md", "asset.md"})
	out := filesystemReportBatch(t, f, f.result(t, false), observations, failures)
	// Inspect IDs before the engine can canonicalize them: reused per-reader
	// ordinals must not silently redirect a source link to a different resource.
	assertFilesystemCauseOracles(t, out, oracles)
	if len(out.Errors) != 3 || len(out.Interruptions) != 3 {
		t.Fatal("repeated resource causes were not shared across readers", out)
	}
	engine.FinalizeLintResult(&out)
	assertFilesystemCauseOracles(t, out, oracles)
	if out.Status != "error" || out.Complete {
		t.Fatal("operational acquisition failure became successful", out)
	}
	assertResourceTargetsUnchanged(t, out)
	validateResourceResult(t, out)
}

func TestFilesystemReportFreshViewsReuseOutputVariable(t *testing.T) {
	f, readers := newFilesystemReportReadersFixture(t, "雪 [shared](asset.md)\n[unique](original.md)\n")
	var out result.Result
	for _, view := range filesystemReportViews(t, f) {
		observations, failures, oracles := filesystemUnavailableBatch(t, view, []*resource.FilesystemReader{readers[0]}, []string{"asset.md", string(view.view.Phase()) + ".md"})
		// Each constructor starts a fresh lifecycle even though the operation,
		// reader and caller's output variable are reused.
		out = filesystemReportBatch(t, view, f.result(t, false), observations, failures)
		assertFilesystemCauseOracles(t, out, oracles)
		engine.FinalizeLintResult(&out)
		assertFilesystemCauseOracles(t, out, oracles)
		if out.Status != "error" || out.Complete || len(out.Errors) != 2 || len(out.Interruptions) != 2 {
			t.Fatalf("fresh %s report lost actual causes: %+v", view.view.Phase(), out)
		}
		assertResourceTargetsUnchanged(t, out)
		validateResourceResult(t, out)
	}
}

func TestFilesystemReportSharedViewsKeepSnapshotCauses(t *testing.T) {
	f, readers := newFilesystemReportReadersFixture(t, "雪 [shared](asset.md)\n[unique](original.md)\n")
	report, err := resource.NewFilesystemReport(f.operation, f.result(t, false))
	if err != nil {
		t.Fatal(err)
	}
	var expected []filesystemCauseOracle
	for _, view := range filesystemReportViews(t, f) {
		observations, failures, oracles := filesystemUnavailableBatch(t, view, []*resource.FilesystemReader{readers[0]}, []string{"asset.md", string(view.view.Phase()) + ".md"})
		if err := report.Append(view.view, observations, failures); err != nil {
			t.Fatal(err)
		}
		expected = append(expected, oracles...)
	}
	out, err := report.Finish()
	if err != nil {
		t.Fatal(err)
	}
	assertFilesystemCauseOracles(t, out, expected)
	engine.FinalizeLintResult(&out)
	assertFilesystemCauseOracles(t, out, expected)
	if out.Status != "error" || out.Complete {
		t.Fatal("later source views lost acquisition causes", out)
	}
	assertResourceTargetsUnchanged(t, out)
	validateResourceResult(t, out)
}
