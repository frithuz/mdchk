package config

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestPatternDirectBoundedFirstMatch(t *testing.T) {
	patterns, err := CompilePatterns([]string{"slides/", "slides/**", "**/*.slides.md", "**"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		index int
	}{
		{"slides/a.md", 1}, {"talk.slides.md", 2}, {"a.md", 3}, {"SLIDES/a.md", 3},
	} {
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		index, matched, err := patterns.MatchFirstBounded(context.Background(), budget, tc.name, false, true)
		if err != nil || !matched || index != tc.index {
			t.Fatalf("%s: index=%d matched=%v err=%v", tc.name, index, matched, err)
		}
	}
	directoryOnly, _ := CompilePatterns([]string{"slides/"})
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	if matched, err := directoryOnly.MatchBounded(context.Background(), budget, "slides/a.md", false, true); matched || err != nil {
		t.Fatalf("directory ancestor became direct annotation: %v %v", matched, err)
	}
	if matched, err := directoryOnly.MatchBounded(context.Background(), budget, "../invalid\x00", false, false); matched || err != nil {
		t.Fatalf("outside name inspected: %v %v", matched, err)
	}
}

func TestPatternDirectBoundedSharedStops(t *testing.T) {
	patterns, _ := CompilePatterns([]string{"**/*.md"})
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if matched, err := patterns.MatchBounded(ctx, budget, "a.md", false, true); matched || !errors.Is(err, context.Canceled) {
		t.Fatalf("caller stop lost: %v %v", matched, err)
	}
	limits := parser.DefaultLimits()
	limits.WorkUnits = 10
	budget, _ = parser.NewOperationBudget(limits)
	_, err := patterns.MatchBounded(context.Background(), budget, "a.md", false, true)
	var quota *parser.LimitExceeded
	if !errors.As(err, &quota) || quota.Name != "work-units" {
		t.Fatalf("expected shared work stop: %v", err)
	}
	if matched, err := patterns.MatchBounded(context.Background(), budget, "a.md", false, true); matched || !errors.As(err, &quota) {
		t.Fatalf("second call reset exhausted budget: %v %v", matched, err)
	}
	if _, _, err := patterns.MatchFirstBounded(nil, budget, "a.md", false, true); err == nil { //nolint:staticcheck // Exercise the rejected nil-context contract.
		t.Fatal("nil context accepted")
	}
	if _, _, err := patterns.MatchFirstBounded(context.Background(), nil, "a.md", false, true); err == nil {
		t.Fatal("nil budget accepted")
	}
}

func TestPatternDirectBoundedOneListAllowance(t *testing.T) {
	list := make([]string, 20)
	for i := range list {
		list[i] = "**/" + strings.Repeat("a", 100) + "b"
	}
	patterns, err := CompilePatterns(list)
	if err != nil {
		t.Fatal(err)
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	_, matched, err := patterns.MatchFirstBounded(context.Background(), budget, strings.Repeat("a", 5000), false, true)
	if matched || !errors.Is(err, ErrPatternLimit) {
		t.Fatalf("deterministic list-wide allowance reset per pattern: %v %v", matched, err)
	}
}

func TestPatternDirectBoundedChargesOnlyVisitedStorage(t *testing.T) {
	patterns, err := CompilePatterns([]string{"**", "never/a/b/c/d/e/f/g", "never/other/deep/path"})
	if err != nil {
		t.Fatal(err)
	}
	limits := parser.DefaultLimits()
	limits.TotalNodes, limits.DocumentNodes = 5, 5
	budget, _ := parser.NewOperationBudget(limits)
	index, matched, err := patterns.MatchFirstBounded(context.Background(), budget, "a.md", false, true)
	if err != nil || !matched || index != 0 {
		t.Fatalf("unvisited rows consumed budget: index=%d match=%v err=%v", index, matched, err)
	}
	_, matched, err = patterns.MatchFirstBounded(context.Background(), budget, "a.md", false, true)
	var limit *parser.LimitExceeded
	if matched || !errors.As(err, &limit) || limit.Name != "total-nodes" {
		t.Fatalf("actual storage construction reset shared allowance: match=%v err=%v", matched, err)
	}
}
