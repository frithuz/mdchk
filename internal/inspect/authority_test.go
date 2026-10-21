package inspect

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

// This context models cancellation arriving just after a successful context
// observation. A named stack frame scopes the trigger to the actual authority
// construction, not acquisition, AST projection, or rule execution. It reads the
// real context before firing once; the real encoder and scanner run unchanged.
type authorityContext struct {
	context.Context
	fired            bool
	observations     int
	afterObservation func()
}

func (c *authorityContext) Err() error {
	err := c.Context.Err()
	if c.fired {
		c.observations++
		return err
	}
	if !c.fired {
		if pc, _, _, ok := runtime.Caller(2); ok {
			if function := runtime.FuncForPC(pc); function != nil && strings.HasSuffix(function.Name(), ".(*builder).authority") {
				c.fired = true
				c.observations++
				c.afterObservation()
			}
		}
	}
	return err
}
func TestAuthorityConstructionCancellation(t *testing.T) {
	s, _ := projectionFixture(t, strings.Repeat("x\n\n", 8000), "", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	trigger := &authorityContext{Context: ctx, afterObservation: cancel}
	a, err := Analyze(trigger, s, AnalyzeOptions{})
	if !trigger.fired || trigger.observations != 2 || !errors.Is(err, context.Canceled) {
		t.Fatalf("authority cancellation: fired=%v err=%v", trigger.fired, err)
	}
	assertStoppedAuthority(t, a, "cancelled")
	if a.Projection.AST.Availability != "available" || len(a.Projection.AST.Data.Root.Children) != 8000 {
		t.Fatal("completed source observations lost")
	}
	if out, again := Filter(context.Background(), s, a, "1"); !errors.Is(again, context.Canceled) || out.Projection.File.SelectedLines != nil {
		t.Fatal("stopped authority usable", again)
	}
	validateProduced(t, a)
}

// A controlled value crosses the real operation deadline from inside JSON
// encoding, after the helper's reservation succeeded. This removes timer races
// around the preconstruction checkpoint while retaining the real encoder,
// structural scan and residual operation meter.
type elapsedAuthorityValue struct {
	until  time.Time
	called bool
}

func (v *elapsedAuthorityValue) MarshalJSON() ([]byte, error) {
	v.called = true
	time.Sleep(time.Until(v.until))
	return []byte(`"source value"`), nil
}
func TestAuthorityConstructionElapsedOperation(t *testing.T) {
	limits := parser.DefaultLimits()
	limits.OperationTime = time.Second
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	slow := &elapsedAuthorityValue{until: time.Now().Add(limits.OperationTime + 10*time.Millisecond)}
	a := Analysis{Projection: Projection{AST: available(AST{Root: Node{Properties: map[string]any{"literal": slow}}})}}
	build := builder{ctx: context.Background(), budget: budget}
	err = build.authority(&a, 1024)
	var limit *parser.LimitExceeded
	if !slow.called || !errors.As(err, &limit) || limit.Name != "operation-time" || build.ctx.Err() != nil {
		t.Fatalf("authority elapsed limit: encoded=%v cause=%v", slow.called, err)
	}
	if len(a.wire) != 0 || a.filterNodes != 0 {
		t.Fatal("elapsed authority was published")
	}
}
func assertStoppedAuthority(t *testing.T, a Analysis, reason string) {
	t.Helper()
	if len(a.wire) != 0 || a.filterNodes != 0 || a.cause == nil || a.Result.Status != "incomplete" || a.Result.Complete || len(a.Result.Interruptions) != 1 || a.Result.Interruptions[0].Reason != reason || a.Result.Interruptions[0].ID == "" {
		t.Fatalf("authority stop not finalized: wire=%d nodes=%d cause=%v status=%s interruptions=%+v", len(a.wire), a.filterNodes, a.cause, a.Result.Status, a.Result.Interruptions)
	}
}
