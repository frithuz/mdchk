package parser

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// BenchmarkGFMProfile parses already-owned snapshots. Construction, fixture
// generation and retained-tree counting are excluded; no output/I/O is timed.
func BenchmarkGFMProfile(b *testing.B) {
	cases := []struct {
		name, input string
		limited     bool
	}{{"mixed", gfmMixed, false}, {"url-suffix", gfmAdversarial(32)[2], false}, {"html-tail", gfmAdversarial(32)[3], false}, {"explicit-work-limit", gfmMixed + strings.Repeat("~* www.a.b/a). <title> ", 128), true}}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			source := NewSource([]byte(c.input))
			l := DefaultLimits()
			if c.limited {
				l.WorkUnits = 10000
			}
			observe := func() (ParseResult, *OperationBudget) {
				op, err := NewOperationBudget(l)
				if err != nil {
					b.Fatal(err)
				}
				r := parseWithProfile(context.Background(), source, op, gfmInProgressProfile)
				return r, op
			}
			before, op := observe()
			outcome := "complete"
			if c.limited {
				if before.Limit == nil || before.Limit.Name != "work-units" || before.Status != Incomplete {
					b.Fatal("expected work limit", before)
				}
				outcome = "work-limited"
			} else if before.Status != Complete {
				b.Fatal(before)
			}
			count := 0
			Walk(before.Document.root, func(*Node) bool { count++; return true })
			b.ReportAllocs()
			if !c.limited {
				b.SetBytes(int64(source.Len()))
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r, _ := observe()
				if c.limited {
					if r.Limit == nil || r.Limit.Name != "work-units" || r.Status != Incomplete {
						b.Fatal("changed limit")
					}
				} else if r.Status != Complete {
					b.Fatal("changed complete outcome")
				}
			}
			b.StopTimer()
			b.ReportMetric(1, outcome+"/op")
			b.ReportMetric(float64(source.Len()), "input-B/op")
			b.ReportMetric(float64(count), "nodes/op")
			b.ReportMetric(float64(op.work), "work/op")
			b.ReportMetric(float64(op.nodes), "construction/op")
			b.Logf("%s: bytes=%d nodes=%d work=%d construction=%d outcome=%s", fmt.Sprint(c.name), source.Len(), count, op.work, op.nodes, outcome)
		})
	}
}
