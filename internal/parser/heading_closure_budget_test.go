package parser

import (
	"context"
	"errors"
	"testing"
)

func TestHeadingClosingMarkerScanSharesOriginalBudget(t *testing.T) {
	for _, remaining := range []int64{8, 10, 11} {
		budget, _ := NewOperationBudget(DefaultLimits())
		parsed := ParseWithBudget(context.Background(), NewSource([]byte("# #\n")), budget)
		if parsed.Status != Complete {
			t.Fatal(parsed)
		}
		// Root, heading, visible text and construction consume five work units;
		// the two actual marker visits and raw-content presence inspection
		// require three more; closed-gap construction reserves another three.
		if err := budget.ChargeWork(budget.Limits().WorkUnits - budget.work - remaining); err != nil {
			t.Fatal(err)
		}
		facts, err := parsed.Document.Headings(context.Background(), budget)
		if remaining < 11 {
			var limit *LimitExceeded
			if !errors.As(err, &limit) || limit.Name != "work-units" || facts != nil {
				t.Fatal(facts, err)
			}
		} else if err != nil || len(facts) != 1 || !facts[0].Closed {
			t.Fatal(facts, err)
		}
	}
}
