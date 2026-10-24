package resource_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	r "github.com/frithuz/mdchk/internal/resource"
)

func TestValidatedContentSurvivesLaterAnchorStops(t *testing.T) {
	for _, kind := range []string{"cancel", "deadline", "quota"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				f := newReportFixture(t, ctx, "雪 [one](asset.md)\r\n")
				q := f.request(t, "asset.md", false, r.ParsedAnchors)
				source := parser.NewSource([]byte("# acquired\n"))
				evidence, err := f.operation.ValidateContent(source)
				if err != nil {
					t.Fatal(err)
				}
				if evidence.Source() != source {
					t.Fatal("validated source identity changed")
				}
				invalid := parser.NewSource([]byte{255})
				if _, err := f.operation.ValidateContent(invalid); !errors.Is(err, r.ErrContract) {
					t.Fatal("invalid UTF-8 validated", err)
				}
				var stop error
				switch kind {
				case "cancel":
					cancel()
					stop = f.operation.Checkpoint(0)
				case "deadline":
					time.Sleep(time.Second)
					synctest.Wait()
					stop = f.operation.Checkpoint(0)
				case "quota":
					stop = f.operation.Checkpoint(f.operation.Budget().Limits().WorkUnits)
				}
				problem, ok := r.InterruptionProblem(stop)
				if !ok {
					t.Fatal("no real stop", stop)
				}
				spec := r.ObservationSpec{Existence: r.File, Content: r.Available, Anchors: r.Interrupted, Source: source, Validated: evidence, Problem: problem}
				observation, err := f.operation.Record(q, spec)
				if err != nil || observation.Treatment() != r.ContextIncomplete || observation.Existence() != r.File {
					t.Fatal("completed acquisition lost", observation, err)
				}
				content, available := observation.Content()
				if !available || content != source || observation.AnchorAvailability() != r.Interrupted {
					t.Fatal("independent capabilities lost")
				}
				spec.Validated = r.ContentEvidence{}
				if _, err := f.operation.Record(q, spec); err == nil {
					t.Fatal("unvalidated post-stop work accepted")
				}
				spec.Validated = evidence
				spec.Source = invalid
				if _, err := f.operation.Record(q, spec); !errors.Is(err, r.ErrContract) {
					t.Fatal("mismatched content evidence trusted", err)
				}
				if _, err := f.operation.ValidateContent(source); err == nil {
					t.Fatal("post-stop validation started")
				}
				if err := f.operation.Checkpoint(0); err == nil {
					t.Fatal("execution budget reset")
				}
				out := f.result(t, false)
				if err := f.view.AppendEffects(&out, []r.Observation{observation}); err != nil {
					t.Fatal(err)
				}
				engine.FinalizeLintResult(&out)
				if out.Status != "incomplete" || len(out.Interruptions) != 1 || len(out.Coverage) != 1 || len(out.Coverage[0].CauseIDs) != 1 || out.Coverage[0].CauseIDs[0] != out.Interruptions[0].ID {
					t.Fatal("stop attribution lost", out)
				}
				assertResourceTargetsUnchanged(t, out)
				validateResourceResult(t, out)
			})
		})
	}
}

func TestContentEvidenceRejectsForeignOperationAndPreservesEmptyContent(t *testing.T) {
	op, view, source := operation(t, context.Background(), parser.DefaultLimits())
	other, _, _ := operation(t, context.Background(), parser.DefaultLimits())
	q := request(t, view, source, "MD051", r.ParsedAnchors, r.RegularFile)
	empty := parser.NewSource(nil)
	evidence, err := other.ValidateContent(empty)
	if err != nil {
		t.Fatal(err)
	}
	spec := r.ObservationSpec{Existence: r.File, Content: r.Available, Anchors: r.NotRequested, Source: empty, Validated: evidence}
	if _, err := op.Record(q, spec); !errors.Is(err, r.ErrContract) {
		t.Fatal("foreign evidence accepted", err)
	}
	spec.Validated, err = op.ValidateContent(empty)
	if err != nil {
		t.Fatal(err)
	}
	obs, err := op.Record(q, spec)
	content, ok := obs.Content()
	if err != nil || !ok || content != empty || content.Len() != 0 {
		t.Fatal("empty acquired source lost", obs, err)
	}
	spec.Content = r.Unavailable
	spec.Source = parser.Source{}
	if _, err := op.Record(q, spec); !errors.Is(err, r.ErrContract) {
		t.Fatal("evidence attached to unavailable content", err)
	}
}

func TestReservedSavedFactsSurviveExecutionStops(t *testing.T) {
	for _, kind := range []string{"cancel", "deadline", "quota"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				root, _ := r.NewRoot(r.SuppliedRoot)
				settings, _ := r.NewSettings(parser.ParseOptions{}, "index.md", "github", "v1")
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				source := parser.NewSource([]byte("original\n"))
				candidateSource := parser.NewSource([]byte("candidate\n"))
				op, err := r.NewOperation(ctx, budget, root, settings, []r.Target{{ID: "a.md", Source: source}, {ID: "stale.md", Source: source}})
				if err != nil {
					t.Fatal(err)
				}
				original, err := op.NewView(r.Original, nil)
				if err != nil {
					t.Fatal(err)
				}
				candidate, err := op.NewView(r.Candidate, []r.Replacement{{TargetID: "a.md", Source: candidateSource}})
				if err != nil {
					t.Fatal(err)
				}
				reserved, err := op.ReserveSavedView()
				if err != nil {
					t.Fatal(err)
				}
				foreign, err := op.ReserveSavedView()
				if err != nil {
					t.Fatal(err)
				}
				duplicate, err := op.ReserveSavedView()
				if err != nil {
					t.Fatal(err)
				}
				var stop error
				switch kind {
				case "cancel":
					cancel()
					stop = op.Checkpoint(0)
				case "deadline":
					time.Sleep(time.Second)
					synctest.Wait()
					stop = op.Checkpoint(0)
				case "quota":
					stop = op.Checkpoint(budget.Limits().WorkUnits)
				}
				if stop == nil {
					t.Fatal("missing real stop")
				}
				confirmed := parser.NewSource([]byte("confirmed saved\n"))
				facts := []r.Replacement{{TargetID: "a.md", Source: confirmed}, {TargetID: "stale.md", Unavailable: true}}
				copyOfReserve := *reserved
				saved, err := reserved.Bind(facts)
				if err != nil || saved.Phase() != r.Saved || saved.Same(original) || saved.Same(candidate) {
					t.Fatal("saved facts lost", err)
				}
				facts[0].Source = source
				target, available, found := saved.Target("a.md")
				if !found || !available || target.Source != confirmed {
					t.Fatal("confirmed source lost or aliased")
				}
				target, available, found = saved.Target("stale.md")
				if !found || available || target.Source != (parser.Source{}) {
					t.Fatal("unavailable saved fact presumed")
				}
				target, _, _ = original.Target("a.md")
				if target.Source != source {
					t.Fatal("original changed")
				}
				target, _, _ = candidate.Target("a.md")
				if target.Source != candidateSource {
					t.Fatal("candidate changed")
				}
				if _, err := copyOfReserve.Bind(nil); !errors.Is(err, r.ErrContract) {
					t.Fatal("copied reserve reused", err)
				}
				if _, err := reserved.Bind(nil); !errors.Is(err, r.ErrContract) {
					t.Fatal("reserve reused", err)
				}
				if _, err := foreign.Bind([]r.Replacement{{TargetID: "resource-only.md", Source: confirmed}}); !errors.Is(err, r.ErrContract) {
					t.Fatal("resource promoted", err)
				}
				if _, err := foreign.Bind(nil); !errors.Is(err, r.ErrContract) {
					t.Fatal("invalid attempt did not consume reserve")
				}
				if _, err := duplicate.Bind([]r.Replacement{{TargetID: "a.md", Source: confirmed}, {TargetID: "a.md", Unavailable: true}}); !errors.Is(err, r.ErrContract) {
					t.Fatal("duplicate saved fact accepted", err)
				}
				if _, err := op.ReserveSavedView(); err == nil {
					t.Fatal("new post-stop capacity admitted")
				}
				if _, err := op.NewView(r.Candidate, nil); err == nil {
					t.Fatal("new post-stop execution admitted")
				}
				if _, err := op.NewView(r.Saved, nil); err == nil {
					t.Fatal("unreserved post-stop saved view admitted")
				}
				span, _ := confirmed.Span(0, 1)
				link, _ := r.NewLink("a.md", "MD051", "x", confirmed, span)
				key, _ := r.NewKey("x")
				if _, err := saved.Request(r.RequestSpec{Link: link, Key: key, Class: r.Local, Capability: r.ParsedAnchors, Required: r.RegularFile}); err == nil {
					t.Fatal("saved reconciliation granted query execution")
				}
				if op.Budget() != budget || op.Checkpoint(0) == nil {
					t.Fatal("shared budget reset")
				}
			})
		})
	}
}

func TestSavedReservationCopiesShareOneConcurrentAttempt(t *testing.T) {
	op, _, source := operation(t, context.Background(), parser.DefaultLimits())
	reservation, err := op.ReserveSavedView()
	if err != nil {
		t.Fatal(err)
	}
	copied := *reservation
	outcomes := make(chan error, 2)
	for _, reserve := range []*r.SavedViewReservation{reservation, &copied} {
		go func() {
			_, err := reserve.Bind([]r.Replacement{{TargetID: "source.md", Source: source}})
			outcomes <- err
		}()
	}
	accepted := 0
	for range 2 {
		err := <-outcomes
		if err == nil {
			accepted++
		} else if !errors.Is(err, r.ErrContract) {
			t.Fatal(err)
		}
	}
	if accepted != 1 {
		t.Fatal("reserve did not admit exactly one fact binding", accepted)
	}
}
