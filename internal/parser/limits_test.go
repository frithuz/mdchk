package parser

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func budgetFor(t *testing.T, l Limits) *OperationBudget {
	t.Helper()
	b, e := NewOperationBudget(l)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func requireLimit(t *testing.T, r ParseResult, name string) {
	t.Helper()
	if r.Limit == nil || r.Limit.Name != name || r.Limit.Observed <= r.Limit.Allowed || r.Limit.Allowed <= 0 || r.Status != Incomplete || r.AST != Partial || r.Interruption != LimitExceededReason || r.Failure != nil || r.Document == nil || len(r.Uncertainty) != 1 || r.Uncertainty[0].Reason != LimitExceededReason || r.Uncertainty[0].Region.Start() != 0 || r.Uncertainty[0].Region.End() != r.Source.Len() {
		t.Fatalf("bad limit outcome: %+v limit=%+v", r, r.Limit)
	}
	if r.Document.references.Availability == Available || r.Document.references.LookupAvailability == Available || r.Document.blocksRecognized || r.Document.inlineFree {
		t.Fatal("interruption published affirmative handoff")
	}
}

func TestLimitsValidation(t *testing.T) {
	if DefaultLimits() != (Limits{1048576, 256, 250000, 2000000, 250000000, time.Minute}) {
		t.Fatal("accepted defaults changed")
	}
	tests := []Limits{{}}
	for field := 0; field < 6; field++ {
		for _, n := range []int64{0, -1} {
			l := DefaultLimits()
			reflect.ValueOf(&l).Elem().Field(field).SetInt(n)
			tests = append(tests, l)
		}
	}
	l := DefaultLimits()
	l.DocumentNodes = l.TotalNodes + 1
	tests = append(tests, l)
	l = DefaultLimits()
	l.SyntaxDepth = 4097
	tests = append(tests, l)
	l = DefaultLimits()
	l.WorkUnits = math.MaxInt64
	tests = append(tests, l)
	l = DefaultLimits()
	l.TotalNodes = math.MaxInt64
	tests = append(tests, l)
	l = DefaultLimits()
	l.DocumentBytes = math.MaxInt64
	tests = append(tests, l)
	for _, l := range tests {
		if _, err := NewOperationBudget(l); err == nil {
			t.Fatalf("accepted invalid profile %+v", l)
		}
	}
	if got := addBounded(math.MaxInt64-1, 5); got != math.MaxInt64 {
		t.Fatal("overflow wrapped", got)
	}
	b := budgetFor(t, DefaultLimits())
	if err := b.ChargeWork(-1); err == nil || b.work != 0 {
		t.Fatal("negative charge refunded work")
	}
}

func TestDocumentBytesAdmissionAndEncodingPrecedence(t *testing.T) {
	l := DefaultLimits()
	l.DocumentBytes = 3
	for _, n := range []int{2, 3, 4} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			b := budgetFor(t, l)
			r := ParseWithBudget(context.Background(), NewSource([]byte(strings.Repeat("x", n))), b)
			if n <= 3 {
				if r.Status != Complete {
					t.Fatal(r)
				}
			} else {
				requireLimit(t, r, "document-bytes")
				if b.work != 0 || b.nodes != 0 {
					t.Fatal("oversize input scanned")
				}
			}
		})
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, input := range []string{"\xff", "x\xff"} {
		r := ParseWithBudget(cancelled, NewSource([]byte(input)), budgetFor(t, l))
		if r.Failure == nil || r.Failure.Code != InvalidUTF8 || r.Interruption != "" {
			t.Fatal("encoding did not precede cancellation", r)
		}
	}
	r := ParseWithBudget(cancelled, NewSource([]byte("abc\xff")), budgetFor(t, l))
	requireLimit(t, r, "document-bytes")
	// Test actual accepted document-byte boundary rather than only an override.
	for _, n := range []int{1048575, 1048576, 1048577} {
		r := Parse(context.Background(), NewSource([]byte(strings.Repeat("a", n))))
		if n <= 1048576 {
			if r.Status != Complete {
				t.Fatal(n, r)
			}
		} else {
			requireLimit(t, r, "document-bytes")
		}
	}
}

func TestSyntaxDepthBoundaries(t *testing.T) {
	for _, depth := range []int{255, 256, 257} {
		// Paragraph contributes one construct; Text and Document contribute zero.
		for name, input := range map[string]string{
			"quotes":   strings.Repeat("> ", depth-1) + "x",
			"emphasis": strings.Repeat("**", depth-1) + "x" + strings.Repeat("**", depth-1),
			"images":   strings.Repeat("![", depth-1) + "x" + strings.Repeat("](u)", depth-1),
			"mixed":    strings.Repeat("> ", 128) + strings.Repeat("![", depth-129) + "x" + strings.Repeat("](u)", depth-129),
		} {
			t.Run(name+strconv.Itoa(depth), func(t *testing.T) {
				r := Parse(context.Background(), NewSource([]byte(input)))
				if depth <= 256 {
					if r.Status != Complete || r.Document.root.depth != depth {
						t.Fatalf("depth %d: %v actual %d", depth, r, r.Document.root.depth)
					}
				} else {
					requireLimit(t, r, "syntax-depth")
					if r.Limit.Observed != 257 || r.Limit.Allowed != 256 {
						t.Fatal(r.Limit)
					}
				}
			})
		}
	}
	for _, input := range []string{strings.Repeat("[", 2000), strings.Repeat("*", 2000) + "x", strings.Repeat("![", 2000) + "x"} {
		r := Parse(context.Background(), NewSource([]byte(input)))
		if r.Status != Complete {
			t.Fatal("unmatched runs fabricated nesting", r)
		}
	}
}

func TestConstructionAndWorkBudgetExactBoundaries(t *testing.T) {
	source := NewSource([]byte("> - a *b* ![c][k]\n\n[k]: u\n"))
	baseline := budgetFor(t, DefaultLimits())
	r := ParseWithBudget(context.Background(), source, baseline)
	if r.Status != Complete {
		t.Fatal(r)
	}
	for name, used := range map[string]int64{"document-nodes": baseline.nodes, "total-nodes": baseline.nodes, "work-units": baseline.work} {
		for _, delta := range []int64{-1, 0, 1} {
			l := DefaultLimits()
			switch name {
			case "document-nodes":
				l.DocumentNodes = used + delta
			case "total-nodes":
				l.TotalNodes = used + delta
				l.DocumentNodes = used + delta
			case "work-units":
				l.WorkUnits = used + delta
			}
			b := budgetFor(t, l)
			r := ParseWithBudget(context.Background(), source, b)
			if delta < 0 {
				if name == "total-nodes" {
					// Per-document admission takes precedence at an equal cap. A shared
					// operation below isolates cumulative construction exhaustion instead.
					requireLimit(t, r, "document-nodes")
				} else {
					requireLimit(t, r, name)
				}
			} else if r.Status != Complete {
				t.Fatalf("%s %d rejected exact allowance: %+v", name, delta, r)
			}
		}
	}
	// Thousands of source lines create retained records even with no AST payload.
	l := DefaultLimits()
	l.DocumentNodes = 20
	r = ParseWithBudget(context.Background(), NewSource([]byte(strings.Repeat("\n", 1000))), budgetFor(t, l))
	requireLimit(t, r, "document-nodes")
	if r.Phases.Blocks != Interrupted {
		t.Fatal("line index bypassed accounting", r.Phases)
	}
}

func TestOperationBudgetSharedAcrossParsesAndAdapters(t *testing.T) {
	source := NewSource([]byte("a"))
	probe := budgetFor(t, DefaultLimits())
	r := ParseWithBudget(context.Background(), source, probe)
	if r.Status != Complete {
		t.Fatal(r)
	}
	l := DefaultLimits()
	l.DocumentNodes = probe.nodes
	l.TotalNodes = probe.nodes * 2
	b := budgetFor(t, l)
	for i := 0; i < 2; i++ {
		if r := ParseWithBudget(context.Background(), source, b); r.Status != Complete {
			t.Fatal("document allowance did not reset", i, r)
		}
	}
	r = ParseWithBudget(context.Background(), source, b)
	requireLimit(t, r, "total-nodes")
	// Result metadata must not be a mutable handle to the sticky operation state.
	r.Limit.Name = "changed"
	r.EffectiveLimits.WorkUnits = 0
	requireLimit(t, ParseWithBudget(context.Background(), source, b), "total-nodes")
	l = DefaultLimits()
	l.WorkUnits = probe.work*2 + 1
	b = budgetFor(t, l)
	if err := b.ChargeWork(1); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if r := ParseWithBudget(context.Background(), source, b); r.Status != Complete {
			t.Fatal(i, r)
		}
	}
	requireLimit(t, ParseWithBudget(context.Background(), source, b), "work-units")
	var exceeded *LimitExceeded
	if !errors.As(b.ChargeWork(1), &exceeded) || exceeded.Name != "work-units" {
		t.Fatal("lost typed exhaustion")
	}
	exceeded.Name = "changed"
	if !errors.As(b.ChargeWork(1), &exceeded) || exceeded.Name != "work-units" {
		t.Fatal("adapter error mutated shared budget")
	}
}

func TestOperationTimeAndTinyWorkStopBeforeScans(t *testing.T) {
	l := DefaultLimits()
	b := budgetFor(t, l)
	b.started = time.Now().Add(-2 * l.OperationTime)
	r := ParseWithBudget(context.Background(), NewSource([]byte("x")), b)
	requireLimit(t, r, "operation-time")
	if b.work != 0 || b.nodes != 0 {
		t.Fatal("expired operation started scanning")
	}
	for _, input := range []string{strings.Repeat("a", 65536), strings.Repeat("\n", 65536), "<a " + strings.Repeat("x", 65536), "[a]: " + strings.Repeat("x", 65536)} {
		l = DefaultLimits()
		l.WorkUnits = 8
		b = budgetFor(t, l)
		r := ParseWithBudget(context.Background(), NewSource([]byte(input)), b)
		requireLimit(t, r, "work-units")
		if b.work > l.WorkUnits || b.nodes != 0 {
			t.Fatal("tiny budget scanned/allocated beyond allowance", b.work, b.nodes)
		}
	}
}

func TestIndependentConcurrentParsesAndSharedOperation(t *testing.T) {
	source := NewSource([]byte("> - **a** ![b][c]\n\n[c]: u\n"))
	want := Parse(context.Background(), source)
	if want.Status != Complete {
		t.Fatal(want)
	}
	shared := budgetFor(t, DefaultLimits())
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Go(func() {
			for j := 0; j < 8; j++ {
				r := Parse(context.Background(), source)
				if !reflect.DeepEqual(r, want) {
					t.Error("independent concurrent parse changed outcome")
				}
				r = ParseWithBudget(context.Background(), source, shared)
				if !reflect.DeepEqual(r, want) {
					t.Error("shared operation changed outcome")
				}
				root := r.Document.Root()
				*root = Node{}
				if string(source.Bytes()) != "> - **a** ![b][c]\n\n[c]: u\n" {
					t.Error("source changed")
				}
			}
		})
	}
	wg.Wait()
	if shared.nodes == 0 || shared.work == 0 {
		t.Fatal("shared accounting reset")
	}
}

func TestGrowthCopyWorkPrecedesAllocation(t *testing.T) {
	for _, kind := range []string{"slice", "builder"} {
		t.Run(kind, func(t *testing.T) {
			l := DefaultLimits()
			l.WorkUnits = 8
			op := budgetFor(t, l)
			ctx := &parserContext{Context: context.Background(), budget: &parseBudget{operation: op}}
			original := make([]int, 8)
			var builder strings.Builder
			builder.WriteString("12345678")
			stopped := false
			func() {
				defer func() {
					v := recover()
					if v == nil {
						return
					}
					stop, ok := v.(budgetStop)
					if !ok {
						panic(v)
					}
					stopped = true
					if stop.exceeded.Name != "work-units" || stop.exceeded.Observed != 9 || stop.exceeded.Allowed != 8 {
						t.Fatal(stop.exceeded)
					}
				}()
				if kind == "slice" {
					_ = meteredAppend(ctx, original, 1)
				} else {
					meteredWriteString(ctx, &builder, "x")
				}
			}()
			if !stopped || len(original) != 8 || builder.String() != "12345678" {
				t.Fatal("resize copy was not admitted before allocation")
			}
		})
	}
}

func TestDepthAdmissionStopsAtFirstExcess(t *testing.T) {
	for _, allowed := range []int64{1, 2, 256} {
		for _, payload := range []string{"", "x"} {
			t.Run(fmt.Sprintf("quotes/%d/%q", allowed, payload), func(t *testing.T) {
				l := DefaultLimits()
				l.SyntaxDepth = allowed
				if allowed < 100 {
					l.DocumentNodes = 100
				}
				op := budgetFor(t, l)
				r := ParseWithBudget(context.Background(), NewSource([]byte(strings.Repeat("> ", 2000)+payload)), op)
				requireLimit(t, r, "syntax-depth")
				if r.Limit.Observed != allowed+1 {
					t.Fatalf("late quote-stack admission: %+v", r.Limit)
				}
				// Root/index plus one source-prefix record and one stack record
				// per admitted quote, and the next recognized prefix candidate.
				if op.nodes > 3+2*allowed {
					t.Fatalf("constructed excess containers before depth admission: %d", op.nodes)
				}
			})
		}
	}
	for _, quotes := range []int{0, 1} {
		t.Run(fmt.Sprintf("emphasis/%d", quotes), func(t *testing.T) {
			l := DefaultLimits()
			l.SyntaxDepth = int64(quotes + 2)
			// Enough for matching/indexing this input and the allowed frames,
			// but not descent through a thousand already-excessive frames.
			l.WorkUnits = 100000
			source := NewSource([]byte(strings.Repeat("> ", quotes) + strings.Repeat("**", 1000) + "x" + strings.Repeat("**", 1000)))
			op := budgetFor(t, l)
			r := ParseWithBudget(context.Background(), source, op)
			requireLimit(t, r, "syntax-depth")
			if r.Limit.Observed != l.SyntaxDepth+1 || r.Phases.Blocks != PhaseComplete || r.Phases.Inlines != Interrupted {
				t.Fatal("late/mixed inline-depth admission", r)
			}
		})
	}
}

func TestReferenceExpansionAdmittedBeforeReplacement(t *testing.T) {
	const n = 32
	source := NewSource([]byte(strings.Repeat("\x00", n)))
	l := DefaultLimits()
	l.WorkUnits = 4*n + 3
	op := budgetFor(t, l)
	ctx := &parserContext{Context: context.Background(), budget: &parseBudget{operation: op}}
	index := NewLineIndex(source)
	index.ctx = ctx
	paragraph := referenceParagraph{parts: []referencePart{{line: blockLine{physicalLine: physicalLine{end: n, next: n}}}}}
	var value Value
	stopped := false
	func() {
		defer func() {
			caught := recover()
			if caught == nil {
				return
			}
			stop, ok := caught.(budgetStop)
			if !ok {
				panic(caught)
			}
			stopped = true
			if stop.exceeded.Name != "work-units" || stop.exceeded.Observed != 4*n+4 || stop.exceeded.Allowed != 4*n+3 {
				t.Fatal(stop.exceeded)
			}
		}()
		value = paragraph.value(index, 0, n)
	}()
	// Only the four visit/range-selection units precede the full scan/output
	// reservation. The former raw-only reservation reached replacement/retention.
	if !stopped || op.work != 4 || op.nodes != 0 || value.Semantic.Present {
		t.Fatal("replacement preceded expansion admission", op.work, op.nodes, value)
	}
}

func TestExpandedStringAndConcatenationExactWorkBoundaries(t *testing.T) {
	raw := strings.Repeat("\x00", 32)
	for _, kind := range []string{"replacement", "email-prefix", "partial-tab-prefix"} {
		for _, delta := range []int64{-1, 0, 1} {
			t.Run(fmt.Sprintf("%s/%d", kind, delta), func(t *testing.T) {
				prefix, payload := "mailto:", strings.Repeat("a", 128)
				if kind == "partial-tab-prefix" {
					prefix = "   "
				}
				required := int64(len(prefix) + len(payload))
				if kind == "replacement" {
					required = int64(4 * len(raw))
				}
				l := DefaultLimits()
				l.WorkUnits = required + delta
				op := budgetFor(t, l)
				ctx := &parserContext{Context: context.Background(), budget: &parseBudget{operation: op}}
				reached, stopped := false, false
				var result string
				func() {
					defer func() {
						caught := recover()
						if caught == nil {
							return
						}
						stop, ok := caught.(budgetStop)
						if !ok {
							panic(caught)
						}
						stopped = true
						if stop.exceeded.Name != "work-units" || stop.exceeded.Observed != required || stop.exceeded.Allowed != required-1 {
							t.Fatal(stop.exceeded)
						}
					}()
					if kind == "replacement" {
						admitted := meteredExpandedString(ctx, raw)
						reached = true
						result = referenceSemanticText.Replace(admitted)
					} else {
						result = meteredConcat(ctx, prefix, payload)
						reached = true
					}
				}()
				if delta < 0 {
					if !stopped || reached || result != "" || op.work != 0 {
						t.Fatal("allocation happened before admission")
					}
				} else {
					want := prefix + payload
					if kind == "replacement" {
						want = strings.Repeat("\ufffd", 32)
					}
					if stopped || !reached || result != want || op.work != required {
						t.Fatal("exact copying allowance rejected", op.work, result)
					}
				}
			})
		}
	}
}
