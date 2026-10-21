package parser

import (
	"context"
	"errors"
	"math/bits"
	"strings"
	"testing"
)

func TestBoundedPositionsMatchCanonicalCoordinatesAndChargeActualPrefix(t *testing.T) {
	source := NewSource([]byte("雪\te\u0301\r\n\nlast\r"))
	index := NewLineIndex(source)
	for at := 0; at <= source.Len(); at++ {
		budget, _ := NewOperationBudget(DefaultLimits())
		got, err := index.PositionBounded(context.Background(), budget, at)
		want, wantErr := index.Position(at)
		if got != want || !errors.Is(err, wantErr) {
			t.Fatalf("byte %d got %+v %v want %+v %v", at, got, err, want, wantErr)
		}
	}
	source = NewSource([]byte(strings.Repeat("x\n\n", 8000)))
	index = NewLineIndex(source)
	at := source.Len() - 3
	// This point is at its line start: only searches/checks are charged, despite
	// the 24KB source. A 100-unit residual allowance must suffice.
	limits := DefaultLimits()
	limits.WorkUnits = 100
	budget, _ := NewOperationBudget(limits)
	if _, err := index.PositionBounded(context.Background(), budget, at); err != nil {
		t.Fatal("whole source charged for tiny prefix", err)
	}
	charged := int64(2*bits.Len(uint(index.LineCount())) + 16)
	if err := budget.ChargeWork(100 - charged); err != nil {
		t.Fatal("unexpected prefix work", err)
	}
	var limit *LimitExceeded
	if err := budget.ChargeWork(1); !errors.As(err, &limit) {
		t.Fatal("coordinate work was not charged", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	budget, _ = NewOperationBudget(DefaultLimits())
	if _, err := index.PositionBounded(ctx, budget, at); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
