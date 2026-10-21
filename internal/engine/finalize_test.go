package engine

import (
	"fmt"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func linkingFixture(n int) result.Result {
	out := result.NewLint("cli")
	for i := 0; i < n; i++ {
		path := fmt.Sprintf("%05d.md", i)
		out.Files = append(out.Files, result.File{Path: path, Analysis: "complete", ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}})
		for _, rule := range []string{"MD001", "MD003", "MD004", "MD005"} {
			out.Coverage = append(out.Coverage, result.Coverage{Path: path, Rule: rule, Reason: "rule-unavailable", Message: "unavailable"})
		}
		if i%10 == 0 {
			out.Errors = append(out.Errors, result.Error{Path: path, Category: result.IO, Code: "invalid-utf8", Scope: "operation", Message: "decode"})
			out.Coverage = append(out.Coverage, result.Coverage{Path: path, Reason: "decoding-failed", Message: "decode"})
		}
	}
	return out
}

func BenchmarkFinalizeLinking(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				out := linkingFixture(n)
				finalize(&out)
				if len(out.Files) != n {
					b.Fatal("lost files")
				}
			}
		})
	}
}

func TestIndexedFinalizationLinksOnlyMatchingCauses(t *testing.T) {
	out := linkingFixture(100)
	out.Errors = append(out.Errors, result.Error{Path: "00000.md", Rule: "MD094", Category: result.IO, Code: "invalid-utf8", Scope: "operation", Message: "attributed decode"}, result.Error{Path: "00001.md", Rule: "MD003", Category: result.Internal, Code: "rule-failure", Scope: "operation", Message: "rule failed"})
	out.Coverage = append(out.Coverage, result.Coverage{Path: "00000.md", Rule: "MD094", Reason: "decoding-failed", Message: "two matching families"}, result.Coverage{Path: "00002.md", Reason: "cancelled", Message: "interrupted"})
	out.Interruptions = append(out.Interruptions, result.Interruption{Path: "00002.md", Reason: "cancelled", Phase: "analysis", Message: "stop"})
	finalize(&out)
	errorsByID := map[string]result.Error{}
	for _, e := range out.Errors {
		errorsByID[e.ID] = e
	}
	for _, c := range out.Coverage {
		seen := map[string]bool{}
		for _, id := range c.CauseIDs {
			if seen[id] {
				t.Fatal("duplicate cause", c)
			}
			seen[id] = true
			if id == "interruption-1" {
				if c.Path != "00002.md" || c.Reason != "cancelled" {
					t.Fatal(c)
				}
				continue
			}
			e, ok := errorsByID[id]
			if !ok || e.Path != c.Path || !(c.Rule != "" && c.Rule == e.Rule || c.Reason == "decoding-failed" && e.Code == "invalid-utf8") {
				t.Fatal("unrelated cause", c, e)
			}
		}
		if c.Path == "00000.md" && c.Rule == "MD094" && len(c.CauseIDs) != 2 {
			t.Fatal("merge lost matching decode/rule causes", c)
		}
		if c.Path == "00001.md" && c.Rule == "MD003" && len(c.CauseIDs) != 1 {
			t.Fatal("missing rule cause", c)
		}
	}
	for _, f := range out.Files {
		for _, id := range f.ErrorIDs {
			if errorsByID[id].Path != f.Path {
				t.Fatal("foreign file error", f)
			}
		}
		count := 0
		for _, c := range out.Coverage {
			if c.Path == f.Path {
				count++
			}
		}
		if len(f.CoverageIDs) != count {
			t.Fatal("file coverage missing", f)
		}
	}
}
