package resource_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
)

type reportFixture struct {
	operation *resource.Operation
	view      resource.View
	source    parser.Source
	text      string
}

func newReportFixture(t *testing.T, ctx context.Context, text string) reportFixture {
	t.Helper()
	budget, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	root, err := resource.NewRoot(resource.SuppliedRoot)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := resource.NewSettings(parser.ParseOptions{}, "index.md", "github", "contract-test-v1")
	if err != nil {
		t.Fatal(err)
	}
	source := parser.NewSource([]byte(text))
	key, err := resource.NewKey("docs/a.md")
	if err != nil {
		t.Fatal(err)
	}
	operation, err := resource.NewOperation(ctx, budget, root, settings, []resource.Target{{ID: "docs/a.md", Source: source, ResourceKey: key}})
	if err != nil {
		t.Fatal(err)
	}
	view, err := operation.NewView(resource.Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	return reportFixture{operation, view, source, text}
}

func (f reportFixture) request(t *testing.T, destination string, last bool, capability resource.Capability) resource.Request {
	t.Helper()
	start := strings.Index(f.text, destination)
	if last {
		start = strings.LastIndex(f.text, destination)
	}
	if start < 0 {
		t.Fatal("destination absent in fixture")
	}
	span, err := f.source.Span(start, start+len(destination))
	if err != nil {
		t.Fatal(err)
	}
	link, err := resource.NewLink("docs/a.md", "MD051", destination, f.source, span)
	if err != nil {
		t.Fatal(err)
	}
	key, err := resource.NewKey(destination)
	if err != nil {
		t.Fatal(err)
	}
	request, err := f.view.Request(resource.RequestSpec{Key: key, Link: link, Capability: capability, Required: resource.RegularFile, Class: resource.Local})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func (f reportFixture) outcome(t *testing.T, request resource.Request, spec resource.ObservationSpec) resource.Observation {
	t.Helper()
	observation, err := f.operation.Record(request, spec)
	if err != nil {
		t.Fatal(err)
	}
	return observation
}

func (f reportFixture) result(t *testing.T, diagnostic bool) result.Result {
	t.Helper()
	out := result.NewLint("cli")
	hash := sha256.Sum256([]byte(f.text))
	snapshot := result.Snapshot{View: "original", SHA256: hex.EncodeToString(hash[:]), SizeBytes: len(f.text)}
	out.CheckedFiles = []string{"docs/a.md"}
	out.Files = []result.File{{Path: "docs/a.md", Analysis: "partial", WriteStatus: "not-requested", ReturnStatus: "not-requested", InputSnapshot: &snapshot, ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}}}
	if diagnostic {
		out.Diagnostics = []result.Diagnostic{{Path: "docs/a.md", Rule: "MD001", Severity: "error", Message: "independent body observation", Range: result.Range{Start: parser.Position{Line: 1, Column: 1}, End: parser.Position{Line: 1, Column: 2}}, ByteRange: result.ByteRange{Start: 0, End: 3}, Snapshot: snapshot}}
	}
	return out
}

func validateResourceResult(t *testing.T, out result.Result) {
	t.Helper()
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(":null")) {
		t.Fatalf("unexpected null in shared result: %s", raw)
	}
	python := os.Getenv("MDCHK_RESULT_VALIDATOR_PYTHON")
	if python == "" {
		return
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("missing source path")
	}
	validator := filepath.Join(filepath.Dir(file), "../../docs/contracts/results/validate.py")
	script := `import json,runpy,sys
v=runpy.run_path(sys.argv[1]);x=json.load(sys.stdin);v['validator'].validate(x);v['semantic'](x)`
	command := exec.Command(python, "-B", "-c", script, validator) //nolint:gosec // Explicit test validator interpreter and fixed checked-in schema path.
	command.Stdin = bytes.NewReader(raw)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("shared resource result schema: %v\n%s\n%s", err, output, raw)
	}
}

func assertResourceTargetsUnchanged(t *testing.T, out result.Result) {
	t.Helper()
	if !reflect.DeepEqual(out.CheckedFiles, []string{"docs/a.md"}) || len(out.Files) != 1 || out.Files[0].Path != "docs/a.md" || len(out.Changes) != 0 || len(out.Documents) != 0 || out.Files[0].WriteStatus != "not-requested" {
		t.Fatalf("resource observation acquired target/write authority: %+v", out)
	}
}

func TestResourceSharedResultDeduplicatesCauseAndRetainsSourceCoverage(t *testing.T) {
	f := newReportFixture(t, context.Background(), "雪 [one](asset.md)\r\n[two](asset.md)\n")
	spec := resource.ObservationSpec{Existence: resource.File, Content: resource.Failed, Anchors: resource.Unavailable, Problem: resource.Problem{Kind: resource.ReadFailure, Message: "resource read failed"}}
	first := f.outcome(t, f.request(t, "asset.md", false, resource.ParsedAnchors), spec)
	second := f.outcome(t, f.request(t, "asset.md", true, resource.ParsedAnchors), spec)
	out := f.result(t, true)
	wantDiagnostic := out.Diagnostics[0]
	if err := f.view.AppendEffects(&out, []resource.Observation{first, second}); err != nil {
		t.Fatal(err)
	}
	engine.FinalizeLintResult(&out)
	if out.Status != "error" || out.Complete || len(out.Errors) != 1 || out.Errors[0].Category != result.IO || out.Errors[0].Path != "asset.md" || len(out.Coverage) != 2 || len(out.Diagnostics) != 1 || len(out.Interruptions) != 0 {
		t.Fatalf("resource/source facts lost: %+v", out)
	}
	expected := []result.Range{{Start: parser.Position{Line: 1, Column: 9}, End: parser.Position{Line: 1, Column: 17}}, {Start: parser.Position{Line: 2, Column: 7}, End: parser.Position{Line: 2, Column: 15}}}
	for i, coverage := range out.Coverage {
		if coverage.Path != "docs/a.md" || coverage.Rule != "MD051" || coverage.Reason != result.ContextUnavailable || coverage.Range == nil || *coverage.Range != expected[i] || coverage.Snapshot == nil || *coverage.Snapshot != *out.Files[0].InputSnapshot || !reflect.DeepEqual(coverage.CauseIDs, []string{out.Errors[0].ID}) {
			t.Fatalf("coverage%d source/cause mismatch: %+v", i, coverage)
		}
	}
	if len(out.Files[0].CoverageIDs) != 2 {
		t.Fatal("source file lost coverage links", out.Files[0])
	}
	wantDiagnostic.ID = out.Diagnostics[0].ID // The shared finalizer assigns canonical IDs.
	if wantDiagnostic.ID == "" || !reflect.DeepEqual(out.Diagnostics[0], wantDiagnostic) {
		t.Fatal("resource effects changed unrelated diagnostic", out.Diagnostics[0])
	}
	assertResourceTargetsUnchanged(t, out)
	validateResourceResult(t, out)
}

func TestResourceContextProblemsDoNotInventIOOrMissingFindings(t *testing.T) {
	for _, kind := range []resource.ProblemKind{resource.NoProblem, resource.PartialInventory, resource.MalformedReference, resource.Blocked, resource.NotSupplied, resource.ParserUncertain} {
		t.Run(string(kind), func(t *testing.T) {
			f := newReportFixture(t, context.Background(), "雪 [one](asset.md)\r\n")
			request := f.request(t, "asset.md", false, resource.ParsedAnchors)
			spec := resource.ObservationSpec{Existence: resource.Unknown, Content: resource.Unavailable, Anchors: resource.Unavailable, Problem: resource.Problem{Kind: kind, Message: "required context unavailable"}}
			switch kind {
			case resource.NoProblem:
				spec = resource.ObservationSpec{Existence: resource.File, Content: resource.NotRequested, Anchors: resource.NotRequested}
			case resource.NotSupplied:
				spec.Existence = resource.File
			case resource.ParserUncertain:
				spec.Existence, spec.Content, spec.Anchors, spec.Source = resource.File, resource.Available, resource.Uncertain, f.source
			case resource.MalformedReference, resource.Blocked:
				class := resource.InvalidReference
				if kind == resource.Blocked {
					class = resource.BlockedReference
				}
				var err error
				request, err = f.view.Request(resource.RequestSpec{Link: request.Link(), Capability: resource.ParsedAnchors, Required: resource.RegularFile, Class: class})
				if err != nil {
					t.Fatal(err)
				}
			case resource.PartialInventory:
			case resource.ReadFailure, resource.DecodeFailure, resource.Cancelled, resource.Deadline, resource.Limited:
				t.Fatal("operational cause is outside this coverage-only fixture")
			}
			observation := f.outcome(t, request, spec)
			if observation.Treatment() != resource.ContextIncomplete {
				t.Fatal("unavailable context became authoritative absence", observation.Treatment())
			}
			out := f.result(t, false)
			if err := f.view.AppendEffects(&out, []resource.Observation{observation}); err != nil {
				t.Fatal(err)
			}
			engine.FinalizeLintResult(&out)
			if out.Status != "incomplete" || out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != 0 || len(out.Interruptions) != 0 || len(out.Coverage) != 1 || len(out.Coverage[0].CauseIDs) != 0 {
				t.Fatalf("invented cause or finding: %+v", out)
			}
			assertResourceTargetsUnchanged(t, out)
			validateResourceResult(t, out)
		})
	}
}

func TestResourceIOAndInterruptionKeepExistingPrecedence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := newReportFixture(t, ctx, "雪 [one](asset.md)\r\n[two](other.md)\n")
	failedRequest := f.request(t, "asset.md", false, resource.ParsedAnchors)
	stoppedRequest := f.request(t, "other.md", false, resource.ParsedAnchors)
	failed := f.outcome(t, failedRequest, resource.ObservationSpec{Existence: resource.File, Content: resource.Failed, Anchors: resource.Unavailable, Problem: resource.Problem{Kind: resource.DecodeFailure, Message: "resource text is not strict UTF-8"}})
	cancel()
	problem, ok := resource.InterruptionProblem(f.operation.Checkpoint(0))
	if !ok || problem.Kind != resource.Cancelled {
		t.Fatal("lost controlled cancellation", problem)
	}
	stopped := f.outcome(t, stoppedRequest, resource.ObservationSpec{Existence: resource.File, Content: resource.Interrupted, Anchors: resource.Interrupted, Problem: problem})
	if stopped.Existence() != resource.File {
		t.Fatal("cancellation erased known existence")
	}
	out := f.result(t, true)
	if err := f.view.AppendEffects(&out, []resource.Observation{failed, stopped}); err != nil {
		t.Fatal(err)
	}
	engine.FinalizeLintResult(&out)
	if out.Status != "error" || out.Complete || len(out.Errors) != 1 || len(out.Interruptions) != 1 || out.Interruptions[0].Reason != "cancelled" || len(out.Coverage) != 2 || len(out.Diagnostics) != 1 {
		t.Fatalf("precedence discarded observations: %+v", out)
	}
	for _, coverage := range out.Coverage {
		if len(coverage.CauseIDs) != 1 {
			t.Fatal("missing shared cause", coverage)
		}
	}
	assertResourceTargetsUnchanged(t, out)
	validateResourceResult(t, out)
	out.Errors = append(out.Errors, result.Error{Category: result.Internal, Scope: "operation", Code: "internal", Message: "independent internal failure"})
	engine.FinalizeLintResult(&out)
	if out.Status != "error" || len(out.Errors) != 2 || len(out.Interruptions) != 1 || len(out.Diagnostics) != 1 {
		t.Fatal("internal failure erased partial work", out)
	}
	validateResourceResult(t, out)
}

func TestResourceEffectsRejectForeignAuthorityAndDuplicateRequests(t *testing.T) {
	f := newReportFixture(t, context.Background(), "[one](asset.md)\n")
	request := f.request(t, "asset.md", false, resource.ParsedAnchors)
	spec := resource.ObservationSpec{Existence: resource.File, Content: resource.Unavailable, Anchors: resource.Unavailable, Problem: resource.Problem{Kind: resource.NotSupplied, Message: "content not supplied"}}
	observation := f.outcome(t, request, spec)
	other := newReportFixture(t, context.Background(), f.text)
	if _, err := other.operation.Record(request, spec); !errors.Is(err, resource.ErrContract) {
		t.Fatal("foreign operation accepted request", err)
	}
	foreignOperation := other.outcome(t, other.request(t, "asset.md", false, resource.ParsedAnchors), spec)
	otherView, err := f.operation.NewView(resource.Candidate, nil)
	if err != nil {
		t.Fatal(err)
	}
	foreignRequest, err := otherView.Request(resource.RequestSpec{Key: request.Key(), Link: request.Link(), Capability: resource.ParsedAnchors, Required: resource.RegularFile, Class: resource.Local})
	if err != nil {
		t.Fatal(err)
	}
	foreign := f.outcome(t, foreignRequest, spec)
	for _, observations := range [][]resource.Observation{{observation, foreign}, {observation, foreignOperation}, {observation, observation}, {observation, {}}} {
		out := f.result(t, false)
		before, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.view.AppendEffects(&out, observations); !errors.Is(err, resource.ErrContract) {
			t.Fatal("foreign/duplicate/zero observation accepted", err)
		}
		after, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatal("rejected batch partially altered result")
		}
	}
}

func TestResourceSatisfiedExistenceRetainsIOCause(t *testing.T) {
	for _, kind := range []resource.ProblemKind{resource.ReadFailure, resource.DecodeFailure} {
		t.Run(string(kind), func(t *testing.T) {
			f := newReportFixture(t, context.Background(), "雪 [one](asset.md)\r\n[two](asset.md)\n")
			spec := resource.ObservationSpec{Existence: resource.File, Content: resource.Failed, Anchors: resource.Unavailable, Problem: resource.Problem{Kind: kind, Message: "encountered resource I/O failure"}}
			exists := f.outcome(t, f.request(t, "asset.md", false, resource.ExistenceOnly), spec)
			content := f.outcome(t, f.request(t, "asset.md", true, resource.TextContent), spec)
			if exists.Treatment() != resource.Complete || content.Treatment() != resource.ContextIncomplete {
				t.Fatal("capability facts were conflated")
			}
			for _, observations := range [][]resource.Observation{{exists}, {exists, content}} {
				out := f.result(t, false)
				if err := f.view.AppendEffects(&out, observations); err != nil {
					t.Fatal(err)
				}
				engine.FinalizeLintResult(&out)
				if out.Status != "error" || len(out.Errors) != 1 || out.Errors[0].Category != result.IO || len(out.Coverage) != len(observations)-1 || len(out.Interruptions) != 0 {
					t.Fatal("satisfied existence lost its encountered cause or invented coverage", out)
				}
				if len(out.Coverage) == 1 && !reflect.DeepEqual(out.Coverage[0].CauseIDs, []string{out.Errors[0].ID}) {
					t.Fatal("content requester lost shared I/O cause", out.Coverage)
				}
				assertResourceTargetsUnchanged(t, out)
				validateResourceResult(t, out)
			}
		})
	}
}

func TestResourceSatisfiedExistenceRetainsActualStops(t *testing.T) {
	for _, kind := range []string{"cancel", "deadline", "quota"} {
		t.Run(kind, func(t *testing.T) {
			var outputs []result.Result
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
				defer cancel()
				f := newReportFixture(t, ctx, "雪 [one](asset.md)\r\n[two](asset.md)\n")
				existsRequest := f.request(t, "asset.md", false, resource.ExistenceOnly)
				contentRequest := f.request(t, "asset.md", true, resource.ParsedAnchors)
				var stop error
				switch kind {
				case "cancel":
					cancel()
					stop = f.operation.Checkpoint(0)
				case "deadline":
					time.Sleep(time.Hour)
					synctest.Wait()
					stop = f.operation.Checkpoint(0)
				case "quota":
					stop = f.operation.Checkpoint(f.operation.Budget().Limits().WorkUnits)
				}
				problem, ok := resource.InterruptionProblem(stop)
				if !ok {
					t.Fatal("actual operation stop was not retained", stop)
				}
				spec := resource.ObservationSpec{Existence: resource.File, Content: resource.Interrupted, Anchors: resource.Interrupted, Problem: problem}
				exists := f.outcome(t, existsRequest, spec)
				content := f.outcome(t, contentRequest, spec)
				if exists.Treatment() != resource.Complete || content.Treatment() != resource.ContextIncomplete {
					t.Fatal("stop erased established existence")
				}
				for _, observations := range [][]resource.Observation{{exists}, {exists, content}} {
					out := f.result(t, true)
					if err := f.view.AppendEffects(&out, observations); err != nil {
						t.Fatal(err)
					}
					engine.FinalizeLintResult(&out)
					if out.Status != "incomplete" || out.Complete || len(out.Errors) != 0 || len(out.Interruptions) != 1 || out.Interruptions[0].Reason != string(problem.Kind) || len(out.Coverage) != len(observations)-1 || len(out.Diagnostics) != 1 {
						t.Fatal("stop precedence/capability facts changed", out)
					}
					if len(out.Coverage) == 1 && !reflect.DeepEqual(out.Coverage[0].CauseIDs, []string{out.Interruptions[0].ID}) {
						t.Fatal("dependent coverage lost shared stop", out.Coverage)
					}
					assertResourceTargetsUnchanged(t, out)
					outputs = append(outputs, out)
				}
			})
			for _, out := range outputs {
				validateResourceResult(t, out)
			}
		})
	}
}

func (f reportFixture) keyedRequest(t *testing.T, key string) resource.Request {
	t.Helper()
	base := f.request(t, "asset.md", false, resource.ParsedAnchors)
	logicalKey, err := resource.NewKey(key)
	if err != nil {
		t.Fatal(err)
	}
	request, err := f.view.Request(resource.RequestSpec{Key: logicalKey, Link: base.Link(), Capability: resource.ParsedAnchors, Required: resource.RegularFile, Class: resource.Local})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestResourceCauseIDsAvoidSparseMixedCollectionCollisions(t *testing.T) {
	f := newReportFixture(t, context.Background(), "雪 [one](asset.md)\r\n[two](asset.md)\n")
	failure := resource.ObservationSpec{Existence: resource.File, Content: resource.Failed, Anchors: resource.Unavailable, Problem: resource.Problem{Kind: resource.ReadFailure, Message: "new read failure"}}
	observations := []resource.Observation{
		f.outcome(t, f.request(t, "asset.md", false, resource.ParsedAnchors), failure),
		f.outcome(t, f.request(t, "asset.md", true, resource.ParsedAnchors), failure),
		f.outcome(t, f.keyedRequest(t, "different.md"), failure),
	}
	problem, ok := resource.InterruptionProblem(context.Canceled)
	if !ok {
		t.Fatal("missing cancellation problem")
	}
	stopped := resource.ObservationSpec{Existence: resource.File, Content: resource.Interrupted, Anchors: resource.Interrupted, Problem: problem}
	observations = append(observations, f.outcome(t, f.keyedRequest(t, "stopped.md"), stopped), f.outcome(t, f.keyedRequest(t, "another-stop.md"), stopped))
	out := f.result(t, false)
	for i, id := range []string{"resource-error-1", "resource-stop-2", "resource-error-7"} {
		out.Errors = append(out.Errors, result.Error{ID: id, Category: result.IO, Scope: "operation", Code: "read-failure", Message: "existing I/O failure", Path: fmt.Sprintf("existing-%d.md", i)})
	}
	for _, id := range []string{"resource-stop-1", "resource-error-3", "resource-stop-5"} {
		out.Interruptions = append(out.Interruptions, result.Interruption{ID: id, Reason: "cancelled", Phase: "analysis", Message: id})
	}
	if err := f.view.AppendEffects(&out, observations); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, err := range out.Errors {
		if err.ID == "" || seen[err.ID] {
			t.Fatal("error ID collides across result collections", err.ID)
		}
		seen[err.ID] = true
	}
	for _, stop := range out.Interruptions {
		if stop.ID == "" || seen[stop.ID] {
			t.Fatal("stop ID collides across result collections", stop.ID)
		}
		seen[stop.ID] = true
	}
	if !reflect.DeepEqual(out.Coverage[0].CauseIDs, out.Coverage[1].CauseIDs) || reflect.DeepEqual(out.Coverage[0].CauseIDs, out.Coverage[2].CauseIDs) || reflect.DeepEqual(out.Coverage[3].CauseIDs, out.Coverage[4].CauseIDs) {
		t.Fatal("resource identity cause associations changed", out.Coverage)
	}
	engine.FinalizeLintResult(&out)
	if len(out.Errors) != 5 || len(out.Interruptions) != 5 || len(out.Coverage) != 5 || out.Status != "error" || out.Complete {
		t.Fatal("distinct/repeated causes changed", out)
	}
	counts := map[string]int{}
	for _, coverage := range out.Coverage {
		if len(coverage.CauseIDs) != 1 {
			t.Fatal("coverage lost unique cause", coverage)
		}
		counts[coverage.CauseIDs[0]]++
	}
	if len(counts) != 4 {
		t.Fatal("canonicalization changed distinct causes", counts)
	}
	for _, err := range out.Errors {
		if err.Path == "asset.md" && counts[err.ID] != 2 || err.Path == "different.md" && counts[err.ID] != 1 {
			t.Fatal("canonicalization changed repeated-resource cause links", counts)
		}
	}
	assertResourceTargetsUnchanged(t, out)
	validateResourceResult(t, out)
}

func TestResourceCauseProjectionAllocationGrowth(t *testing.T) {
	allocations := map[int]float64{}
	for _, count := range []int{100, 400, 1600} {
		f := newReportFixture(t, context.Background(), "[one](asset.md)\n")
		spec := resource.ObservationSpec{Existence: resource.File, Content: resource.Failed, Anchors: resource.Unavailable, Problem: resource.Problem{Kind: resource.ReadFailure, Message: "failed resource"}}
		observations := make([]resource.Observation, count)
		for i := range observations {
			observations[i] = f.outcome(t, f.keyedRequest(t, fmt.Sprintf("resource-%d.md", i)), spec)
		}
		allocations[count] = testing.AllocsPerRun(3, func() {
			out := result.NewLint("cli")
			if err := f.view.AppendEffects(&out, observations); err != nil {
				t.Fatal(err)
			}
			if len(out.Errors) != count || len(out.Coverage) != count {
				t.Fatal("projection omitted distinct resource effects")
			}
		})
		t.Logf("distinct causes=%d allocations=%.0f", count, allocations[count])
	}
	if allocations[400] > 5*allocations[100]+1000 || allocations[1600] > 5*allocations[400]+1000 {
		t.Fatalf("cause projection allocation growth exceeds linear allowance: %v", allocations)
	}
}
