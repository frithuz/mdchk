package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/parser"
)

func TestBoundedDirectoryAdmitsBeforeEnumeration(t *testing.T) {
	root := selectionRoot(t)
	selectionFile(t, root, "entry.md", nil)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"nodes", "work", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			limits := parser.DefaultLimits()
			if kind == "nodes" {
				limits.TotalNodes = 7
				limits.DocumentNodes = 7
			}
			if kind == "work" {
				limits.WorkUnits = 5
			}
			budget, err := parser.NewOperationBudget(limits)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			_, err = (boundedSelectionFS{ctx, budget}).readEntries(func(n int) ([]fs.DirEntry, error) {
				if n != 1 {
					t.Fatalf("unadmitted batch %d", n)
				}
				calls++
				if kind == "cancel" {
					cancel()
				}
				if calls > 10000 {
					return nil, io.EOF
				}
				return entries, nil
			})
			if err == nil {
				t.Fatal("unbounded enumeration succeeded")
			}
			if kind == "nodes" {
				var limit *parser.LimitExceeded
				if !errors.As(err, &limit) || limit.Name != "total-nodes" || calls != 7 {
					t.Fatal(calls, err)
				}
			}
			if kind == "work" && calls > 1 {
				t.Fatal("enumeration continued after work exhaustion", calls)
			}
			if kind == "cancel" && (!errors.Is(err, context.Canceled) || calls != 1) {
				t.Fatal(calls, err)
			}
		})
	}
}
func TestBoundedDirectoryRealQuotaAndSelectionParity(t *testing.T) {
	root := selectionRoot(t)
	for i := 0; i < 2000; i++ {
		selectionFile(t, root, fmt.Sprintf("file-%04d.md", i), nil)
	}
	limits := parser.DefaultLimits()
	limits.TotalNodes = 16
	limits.DocumentNodes = 16
	budget, _ := parser.NewOperationBudget(limits)
	selected, err := SelectBounded(context.Background(), Options{WorkingDirectory: root}, budget)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Targets) != 0 || len(selected.Errors) != 1 {
		t.Fatal(selected)
	}
	var limit *parser.LimitExceeded
	if !errors.As(selected.Errors[0], &limit) || limit.Name != "total-nodes" || limit.Observed != 17 || limit.Allowed != 16 {
		t.Fatal(selected.Errors)
	}
	// Fully admitted enumeration retains legacy deterministic selection semantics.
	normal, err := Select(Options{WorkingDirectory: root})
	if err != nil {
		t.Fatal(err)
	}
	budget, _ = parser.NewOperationBudget(parser.DefaultLimits())
	bounded, err := SelectBounded(context.Background(), Options{WorkingDirectory: root}, budget)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(targetPaths(normal), targetPaths(bounded)) {
		t.Fatal("bounded enumeration changed target order")
	}
}
func TestBoundedExclusionsConsumeResidualMeterAndCancel(t *testing.T) {
	root := selectionRoot(t)
	selectionFile(t, root, "doc.md", []byte("x"))
	names := make([]string, 1024)
	for i := range names {
		names[i] = "unmatched*pattern*"
	}
	patterns, err := config.CompilePatterns(names)
	if err != nil {
		t.Fatal(err)
	}
	remaining := func(patterns *config.Patterns) int64 {
		limits := parser.DefaultLimits()
		limits.WorkUnits = 1000000
		budget, _ := parser.NewOperationBudget(limits)
		selected, err := SelectBounded(context.Background(), Options{WorkingDirectory: root, Paths: []string{"doc.md"}, Exclusions: patterns}, budget)
		if err != nil || len(selected.Errors) > 0 || len(selected.Targets) != 1 {
			t.Fatal(selected, err)
		}
		var n int64
		for budget.ChargeWork(1) == nil {
			n++
		}
		return n
	}
	without, with := remaining(nil), remaining(patterns)
	if with >= without || without-with < 1000 {
		t.Fatal("matching did not reduce original residual budget", without, with)
	}
	limits := parser.DefaultLimits()
	limits.WorkUnits = 10
	budget, _ := parser.NewOperationBudget(limits)
	matcher := boundedSelectionFS{context.Background(), budget}
	_, err = matcher.excludes(root, filepath.Join(root, "doc.md"), false, patterns)
	var exceeded *parser.LimitExceeded
	if !errors.As(err, &exceeded) || exceeded.Name != "work-units" {
		t.Fatal(err)
	}
	budget, _ = parser.NewOperationBudget(parser.DefaultLimits())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = (boundedSelectionFS{ctx, budget}).excludes(root, filepath.Join(root, "doc.md"), false, patterns)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// A quota reached inside the matcher remains wrapped and typed at selection's
	// public boundary, rather than becoming a nonmatch or ordinary I/O failure.
	limits = parser.DefaultLimits()
	limits.WorkUnits = 15000
	budget, _ = parser.NewOperationBudget(limits)
	selected, err := SelectBounded(context.Background(), Options{WorkingDirectory: root, Paths: []string{"doc.md"}, Exclusions: patterns}, budget)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Errors) == 0 || !errors.As(selected.Errors[0], &exceeded) {
		t.Fatal(selected)
	}
}

type matchingCancellationContext struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *matchingCancellationContext) Err() error {
	c.checks++
	if c.checks == 20 {
		c.cancel()
	}
	return c.Context.Err()
}
func TestBoundedMatcherCancellationInsideTraversal(t *testing.T) {
	root := selectionRoot(t)
	names := make([]string, 1024)
	for i := range names {
		names[i] = "unmatched*pattern*"
	}
	patterns, err := config.CompilePatterns(names)
	if err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &matchingCancellationContext{Context: parent, cancel: cancel}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	_, err = (boundedSelectionFS{ctx, budget}).excludes(root, filepath.Join(root, "doc.md"), false, patterns)
	if !errors.Is(err, context.Canceled) || ctx.checks != 20 {
		t.Fatalf("matching did not checkpoint its traversal: %d %v", ctx.checks, err)
	}
}
