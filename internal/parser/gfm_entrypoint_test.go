package parser

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestGFMEntryPointIsolationAndSharedBudget(t *testing.T) {
	source := NewSource([]byte("- [X] ~~www.a.b~~\n\n| a |\n| - |\n\n<title>x</title>\n"))
	assert := func(gfm bool, b *OperationBudget) {
		t.Helper()
		parse := ParseWithBudget
		if gfm {
			parse = ParseGFMWithBudget
		}
		r := parse(context.Background(), source, b)
		if r.Status != Complete || r.AST != Available || r.Source != source || r.Failure != nil || r.Phases.Blocks != PhaseComplete || r.Phases.Inlines != PhaseComplete || r.Document.References().Availability != Available || len(r.Uncertainty) != 0 {
			t.Errorf("incomplete parse %+v", r)
			return
		}
		table, strike, task, bare, filter := 0, 0, 0, 0, 0
		Walk(r.Document.Root(), func(n *Node) bool {
			p := n.Properties()
			if n.Kind() == Table {
				table++
			}
			if n.Kind() == Strike {
				strike++
			}
			if p.Task != nil {
				task++
			}
			if p.Link != nil && p.Link.Origin == "bare-gfm" {
				bare++
			}
			if p.HTML != nil {
				filter += len(p.HTML.FilteredTags)
			}
			return true
		})
		if gfm {
			if table != 1 || strike != 1 || task != 2 || bare != 1 || filter != 2 {
				t.Errorf("GFM facts %d/%d/%d/%d/%d", table, strike, task, bare, filter)
			}
		} else if table+strike+task+bare+filter != 0 {
			t.Error("legacy core enabled extensions")
		}
		if _, err := r.Document.ProtectedNodes(context.Background(), b); err != nil {
			t.Error(err)
		}
	}
	shared, _ := NewOperationBudget(DefaultLimits())
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			<-start
			local, _ := NewOperationBudget(DefaultLimits())
			assert(i%2 == 0, local)
			assert(i%2 == 0, shared)
		})
	}
	close(start)
	wg.Wait()
	limits := DefaultLimits()
	limits.WorkUnits = 100
	b, _ := NewOperationBudget(limits)
	r := ParseGFMWithBudget(context.Background(), source, b)
	if r.Status != Incomplete || r.AST != Partial || r.Limit == nil || r.Limit.Name != "work-units" || r.Limit.Allowed != 100 || r.Limit.Observed <= 100 {
		t.Fatal(r)
	}
	if err := b.ChargeWork(0); err == nil {
		t.Fatal("exhaustion not sticky")
	} else {
		var limit *LimitExceeded
		if !errors.As(err, &limit) || limit.Name != "work-units" {
			t.Fatal(err)
		}
	}
	if r.Document != nil {
		if _, err := r.Document.ProtectedNodes(context.Background(), b); err == nil {
			t.Fatal("partial authority")
		}
	}
	if Parse(context.Background(), source).Status != Complete {
		t.Fatal("legacy core changed")
	}
}
