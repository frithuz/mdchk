package metadata

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestDecodeUnavailableAdmissionExactAndRepeated(t *testing.T) {
	limits := parser.DefaultLimits()
	limits.DocumentNodes = 4
	limits.TotalNodes = 4
	f := bindBudgetFixture(t, "", mustBudget(t, limits), context.Background())
	_, err := Decode(context.Background(), f.b)
	want := parser.LimitExceeded{Name: "total-nodes", Observed: 5, Allowed: 4}
	if !containsQuota(err, want) {
		t.Fatal("absent Decode constructed unreserved root at full 4/4 quota", err)
	}
	limits.TotalNodes = 8
	f = bindBudgetFixture(t, "", mustBudget(t, limits), context.Background())
	var retained []Result
	for i := 0; i < 4; i++ {
		r, err := Decode(context.Background(), f.b)
		if err != nil || r.Complete() || r.Root().Kind() != Unknown {
			t.Fatal("unavailable allowance pair", i, err)
		}
		title, err := r.Title(context.Background(), f.b)
		if err != nil || title.Known {
			t.Fatal("absent unavailable title", title, err)
		}
		orders, err := r.Ordering(context.Background(), f.b, OrderOptions{Recursive: true})
		if err != nil || len(orders) != 0 {
			t.Fatal("absent unavailable ordering", orders, err)
		}
		retained = append(retained, r)
	}
	_, err = Decode(context.Background(), f.b)
	want = parser.LimitExceeded{Name: "total-nodes", Observed: 9, Allowed: 8}
	if !containsQuota(err, want) {
		t.Fatal("repeated absent Decode evaded finite allowance", err)
	}
	for _, r := range retained {
		if r.Root().Kind() != Unknown || r.Root().Complete() || r.Complete() {
			t.Fatal("retained unavailable facts changed after stop")
		}
	}
}
func TestDecodeUnterminatedAdmissionAndCompoundControls(t *testing.T) {
	limits := parser.DefaultLimits()
	limits.DocumentNodes = 8
	limits.TotalNodes = 8
	f := bindBudgetFixture(t, "---\nx: 1", mustBudget(t, limits), context.Background())
	_, err := Decode(context.Background(), f.b)
	want := parser.LimitExceeded{Name: "document-nodes", Observed: 10, Allowed: 8}
	if !containsQuota(err, want) {
		t.Fatal("unterminated root/problem not reserved together before construction", err)
	}
	limits.DocumentNodes = 10
	limits.TotalNodes = 100
	budget := mustBudget(t, limits)
	operation, cancel := context.WithCancel(context.Background())
	defer cancel()
	f = bindBudgetFixture(t, "---\nx: 1", budget, operation)
	r, err := Decode(context.Background(), f.b)
	if err != nil || r.Complete() || r.Root().Kind() != Unknown || r.ProblemCount() != 1 {
		t.Fatal("unterminated admitted unavailable pair", err)
	}
	problem, _ := r.KnownProblem(0)
	if problem.Code != Malformed || problem.Location != f.b.Header().Content() {
		t.Fatal("unterminated problem provenance")
	}
	title, err := r.Title(context.Background(), f.b)
	if err != nil || title.Known {
		t.Fatal("unterminated title authority")
	}
	_, err = Decode(context.Background(), f.b)
	want = parser.LimitExceeded{Name: "document-nodes", Observed: 12, Allowed: 10}
	if !containsQuota(err, want) {
		t.Fatal("unterminated repeat lost original local stop", err)
	}
	global := budget.ChargeWork(limits.WorkUnits + 1)
	var globalQuota *parser.LimitExceeded
	if !errors.As(global, &globalQuota) {
		t.Fatal("missing global quota")
	}
	cancel()
	caller, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	_, err = Decode(caller, f.b)
	if !containsQuota(err, want) || !containsQuota(err, *globalQuota) || !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("unavailable admission lost compound causes", err)
	}
	if r.Root().Kind() != Unknown || r.ProblemCount() != 1 {
		t.Fatal("retained unavailable record lost after stop")
	}
}
