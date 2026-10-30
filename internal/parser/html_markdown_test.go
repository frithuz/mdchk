package parser

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func md091Parse(t *testing.T, source string) (*Document, *OperationBudget, LineIndex) {
	t.Helper()
	s := NewSource([]byte(source))
	b, _ := NewOperationBudget(DefaultLimits())
	p := ParseWithOptions(context.Background(), s, b, ParseOptions{})
	if p.Status != Complete {
		t.Fatal(p)
	}
	return p.Document, b, NewLineIndex(s)
}

func TestHTMLMarkdownOriginalPrivateAuthority(t *testing.T) {
	d, b, index := md091Parse(t, "<div>**x**")
	public, _ := NewDocument(d.Root(), d.References())
	foreign, _ := NewOperationBudget(DefaultLimits())
	for _, tc := range []struct {
		d *Document
		b *OperationBudget
		i LineIndex
		o ParseOptions
	}{
		{nil, b, index, ParseOptions{}}, {public, b, index, ParseOptions{}}, {d, nil, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}}, {d, b, NewLineIndex(NewSource([]byte("<div>**x**"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}},
	} {
		got, err := tc.d.HTMLMarkdownCandidates(context.Background(), tc.b, tc.i, tc.o)
		if !errors.Is(err, ErrModel) || got.Candidates != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Deliberately absent caller.
	if got, err := d.HTMLMarkdownCandidates(nil, b, index, ParseOptions{}); !errors.Is(err, ErrModel) || got.Candidates != nil {
		t.Fatal(got, err)
	}
	for _, profile := range []parserProfile{tablesOnlyProfile, tasksOnlyProfile, strikeOnlyProfile, bareOnlyProfile, filterOnlyProfile} {
		for _, fullFlags := range []bool{false, true} {
			t.Run(fmt.Sprintf("profile-%d/full-flags-%v", profile, fullFlags), func(t *testing.T) {
				s := NewSource([]byte("<div>**x**"))
				budget, _ := NewOperationBudget(DefaultLimits())
				p := parseWithProfile(context.Background(), s, budget, profile)
				if p.Status != Complete {
					t.Fatal(p)
				}
				if fullFlags {
					p.Document.parsedOptions.GFM = true
				}
				got, err := p.Document.HTMLMarkdownCandidates(context.Background(), budget, NewLineIndex(s), p.Document.parsedOptions)
				if !errors.Is(err, ErrModel) || got.Candidates != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
	d.parsedComplete = false
	if got, err := d.HTMLMarkdownCandidates(context.Background(), b, index, ParseOptions{}); !errors.Is(err, ErrModel) || got.Candidates != nil {
		t.Fatal(got, err)
	}
	source := NewSource([]byte("<div>**x**"))
	budget, _ := NewOperationBudget(DefaultLimits())
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	partial := ParseWithOptions(cancelled, source, budget, ParseOptions{})
	if partial.Status == Complete {
		t.Fatal("genuine interrupted parse completed")
	}
	if got, err := partial.Document.HTMLMarkdownCandidates(context.Background(), budget, NewLineIndex(source), ParseOptions{}); !errors.Is(err, ErrModel) || got.Candidates != nil {
		t.Fatal(got, err)
	}
}

type md091ProducerLoopStop struct {
	context.Context
	calls int
}

func (c *md091ProducerLoopStop) Err() error {
	var pcs [32]uintptr
	n := runtime.Callers(1, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	for {
		f, more := frames.Next()
		if strings.HasSuffix(f.Function, ".(*htmlMarkdownCollector).recognize.func1") {
			c.calls++
			if c.calls >= 2 {
				return context.Canceled
			}
			break
		}
		if !more {
			break
		}
	}
	return c.Context.Err()
}

func TestHTMLMarkdownCancellationAfterActualPositivePrefix(t *testing.T) {
	d, b, index := md091Parse(t, "<div>**a** **b**")
	before, err := d.HTMLMarkdownCandidates(context.Background(), b, index, ParseOptions{})
	if err != nil || len(before.Candidates) != 2 || before.Candidates[0].Marker.Start() != 5 || before.Candidates[0].Marker.End() != 7 {
		t.Fatal("actual positive precondition", before, err)
	}
	ctx := &md091ProducerLoopStop{Context: context.Background()}
	got, err := d.HTMLMarkdownCandidates(ctx, b, index, ParseOptions{})
	if ctx.calls < 2 || !errors.Is(err, context.Canceled) || got.Candidates != nil {
		t.Fatal(got, err, ctx.calls)
	}
}

func TestHTMLMarkdownCheckedGrowthReservations(t *testing.T) {
	// Explicit prefix/capacity rows: first0->1 costs3W1N; 1->2 costs5W2N;
	// 2->4 costs8W4N. These include the copied prefix but no logical append.
	for _, row := range []struct {
		length, capacity int
		work, nodes      int64
	}{{0, 1, 3, 1}, {1, 2, 5, 2}, {2, 4, 8, 4}} {
		for _, nodes := range []bool{false, true} {
			for _, delta := range []int64{-1, 0} {
				t.Run(fmt.Sprintf("len-%d/nodes-%v/delta-%d", row.length, nodes, delta), func(t *testing.T) {
					_, b, _ := md091Parse(t, "")
					charge, limit, setup, phase := b.ChargeWork, b.Limits().WorkUnits, int64(25), row.work
					if nodes {
						charge, limit, setup, phase = b.ChargeNodes, b.Limits().TotalNodes, 2, row.nodes
					}
					if err := charge(limit - setup - phase - delta); err != nil {
						t.Fatal(err)
					}
					prefix := make([]int, row.length)
					for i := range prefix {
						prefix[i] = i + 1
					}
					ctx := &parserContext{Context: context.Background(), budget: &parseBudget{operation: b}}
					var stop *LimitExceeded
					var grown []int
					func() {
						b.mu.Lock()
						defer b.mu.Unlock()
						defer func() {
							if p := recover(); p != nil {
								if s, ok := p.(budgetStop); ok {
									stop = s.exceeded
								} else {
									panic(p)
								}
							}
						}()
						grown = htmlMarkdownGrow(ctx, prefix)
					}()
					if delta == 0 {
						if stop != nil || len(grown) != row.length || cap(grown) != row.capacity || !reflect.DeepEqual(grown, prefix) {
							t.Fatal(grown, stop)
						}
					} else {
						if stop == nil || grown != nil {
							t.Fatal(grown, stop)
						}
					}
				})
			}
		}
	}
}

func TestHTMLMarkdownLateAssociationWithholdsPositivePrefix(t *testing.T) {
	for _, kind := range []string{"absent", "foreign", "semantic", "transformation", "order", "reversed", "properties"} {
		t.Run(kind, func(t *testing.T) {
			d, b, index := md091Parse(t, "<div>**a**\n\n<div>**b**")
			before, err := d.HTMLMarkdownCandidates(context.Background(), b, index, ParseOptions{})
			if err != nil || len(before.Candidates) != 2 || before.Candidates[0].Marker.Start() != 5 || before.Candidates[0].Marker.End() != 7 {
				t.Fatal("positive prefix precondition", before, err)
			}
			n := d.root.spec.Children[1]
			switch kind {
			case "absent":
				n.spec.Content[0].Source = Span{}
			case "foreign":
				n.spec.Content[0].Source, _ = NewSource([]byte("<div>**b**")).Span(0, 10)
			case "semantic":
				n.spec.Content[0].Semantic = Some("<div>**c**")
			case "transformation":
				n.spec.Content[0].Transformation = Synthetic
			case "order":
				n.spec.Content = append(n.spec.Content, n.spec.Content[0])
			case "reversed":
				n.spec.Content[0].Source.end = n.spec.Content[0].Source.start - 1
			case "properties":
				n.spec.Properties.HTML = nil
			}
			got, err := d.HTMLMarkdownCandidates(context.Background(), b, index, ParseOptions{})
			if !errors.Is(err, ErrModel) || got.Candidates != nil {
				t.Fatal(got, err)
			}
		})
	}
}

// Setup25/2 and projection48/2 are hand-derived from held source, not counters.
func TestHTMLMarkdownStaticEmptyOriginalReservations(t *testing.T) {
	for _, nodes := range []bool{false, true} {
		for _, delta := range []int64{-1, 0} {
			t.Run(fmt.Sprintf("nodes-%v/delta-%d", nodes, delta), func(t *testing.T) {
				d, b, index := md091Parse(t, "")
				charge, limit, setup, phase := b.ChargeWork, b.Limits().WorkUnits, int64(25), int64(48)
				if nodes {
					charge, limit, setup, phase = b.ChargeNodes, b.Limits().TotalNodes, 2, 2
				}
				if err := charge(limit - setup - phase - delta); err != nil {
					t.Fatal(err)
				}
				got, err := d.HTMLMarkdownCandidates(context.Background(), b, index, ParseOptions{})
				if delta == 0 {
					if err != nil || got.Availability != Available || len(got.Candidates) != 0 {
						t.Fatal(got, err)
					}
				} else {
					for range 2 {
						var stop *LimitExceeded
						if !errors.As(err, &stop) || got.Candidates != nil {
							t.Fatal(got, err)
						}
						got, err = d.HTMLMarkdownCandidates(context.Background(), b, index, ParseOptions{})
					}
				}
			})
		}
	}
}

func TestHTMLMarkdownStickyControlsAndDetachedFacts(t *testing.T) {
	for _, source := range []string{"", "clean", "`code`", "<pre>**x**", "<div>**x**", "<div>**x** <x a=\""} {
		for _, kind := range []string{"work", "nodes", "elapsed", "cancelled", "deadline"} {
			t.Run(source+"/"+kind, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					d, b, index := md091Parse(t, source)
					ctx := context.Background()
					switch kind {
					case "work":
						_ = b.ChargeWork(b.Limits().WorkUnits)
					case "nodes":
						_ = b.ChargeNodes(b.Limits().TotalNodes)
					case "elapsed":
						time.Sleep(61 * time.Second)
					case "cancelled":
						cancelled, cancel := context.WithCancel(ctx)
						cancel()
						ctx = cancelled
					case "deadline":
						expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
						defer cancel()
						ctx = expired
					}
					for range 2 {
						got, err := d.HTMLMarkdownCandidates(ctx, b, index, ParseOptions{})
						if err == nil || got.Candidates != nil {
							t.Fatal(got, err)
						}
					}
				})
			})
		}
	}
	d, b, index := md091Parse(t, "<div>**x**")
	first, err := d.HTMLMarkdownCandidates(context.Background(), b, index, ParseOptions{})
	if err != nil || len(first.Candidates) != 1 {
		t.Fatal(first, err)
	}
	saved := append([]HTMLMarkdownCandidate(nil), first.Candidates...)
	first.Candidates[0] = HTMLMarkdownCandidate{}
	next, err := d.HTMLMarkdownCandidates(context.Background(), b, index, ParseOptions{})
	if err != nil || !reflect.DeepEqual(next.Candidates, saved) {
		t.Fatal(next, err)
	}
}

func TestHTMLMarkdownBoundedFailedFamilies(t *testing.T) {
	for _, count := range []int{1, 8, 64, 512} {
		for _, family := range []string{"labels", "destinations", "raw-quotes", "runs"} {
			t.Run(fmt.Sprintf("%s/%d", family, count), func(t *testing.T) {
				var source string
				switch family {
				case "labels":
					source = "<div>" + strings.Repeat("[", count) + "x"
				case "destinations":
					source = "<div>" + strings.Repeat("[x](", count) + "u"
				case "raw-quotes":
					source = "<div><pre><x a='" + strings.Repeat("<x a='", count) + "unfinished"
				case "runs":
					source = "<div>" + strings.Repeat("***x ", count)
				}
				d, b, index := md091Parse(t, source)
				got, err := d.HTMLMarkdownCandidates(context.Background(), b, index, ParseOptions{})
				if err != nil || got.Availability != Available || len(got.Candidates) != 0 {
					t.Fatal(got, err)
				}
			})
		}
	}
}

// Helper-only charges are handwritten from held source short-circuit paths,
// independent ledger v2 and its preserved literal-length correction. Setup25/2
// is the actual original empty production parse; no counters or reset allowance.
func TestHTMLMarkdownConfirmedOpaquePrefixReservations(t *testing.T) {
	for _, row := range []struct {
		text string
		want bool
		work int64
	}{
		{"", false, 0}, {"<", false, 4}, {"<!", false, 16},
		{"<! ", false, 18}, {"<!1", false, 18}, {"<!A", true, 18},
		{"<!a", true, 18}, {"<!--", true, 4}, {"<?", true, 4},
		{"<![CDATA[", true, 15}, {"<![cdata[", false, 25},
		{"<![oops", false, 23}, {"<!DOCTYPE", true, 25},
		{"<x>", false, 10}, {"</div>", false, 14},
	} {
		for _, delta := range []int64{-1, 0} {
			if row.work == 0 && delta < 0 {
				continue // There is no negative remaining-work allowance.
			}
			t.Run(fmt.Sprintf("%q/delta-%d", row.text, delta), func(t *testing.T) {
				_, budget, _ := md091Parse(t, "")
				if err := budget.ChargeWork(budget.Limits().WorkUnits - 25 - row.work - delta); err != nil {
					t.Fatal(err)
				}
				work := &parserContext{Context: context.Background(), budget: &parseBudget{operation: budget}}
				invoke := func() (got bool, stopped *LimitExceeded) {
					budget.mu.Lock()
					defer budget.mu.Unlock()
					defer func() {
						if p := recover(); p != nil {
							if stop, ok := p.(budgetStop); ok {
								stopped = stop.exceeded
							} else {
								panic(p)
							}
						}
					}()
					got = htmlMarkdownOpaqueOpen(work, row.text)
					return got, nil
				}
				got, stop := invoke()
				if delta == 0 {
					if stop != nil || got != row.want {
						t.Fatal(got, stop, row)
					}
				} else {
					if stop == nil || stop.Name != "work-units" || got {
						t.Fatal(got, stop, row)
					}
					if _, again := invoke(); again == nil {
						t.Fatal("original exhaustion was not sticky")
					}
				}
			})
		}
	}
}
