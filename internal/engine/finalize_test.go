package engine

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
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
				FinalizeLintResult(&out)
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
	FinalizeLintResult(&out)
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

func TestCanonicalCoverageIDsAndLinksAcrossPermutations(t *testing.T) {
	fixture := func(reverse bool) result.Result {
		out := result.NewLint("cli")
		out.Files = []result.File{{Path: "a", Analysis: "complete", ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}}}
		out.Errors = []result.Error{
			{ID: "old-io", Path: "a", Category: result.IO, Scope: "operation", Code: "read-failed", Message: "failed"},
			{ID: "old-rule", Path: "a", Rule: "MD001", Category: result.Internal, Scope: "operation", Code: "rule-failure", Message: "failed"},
		}
		out.Interruptions = []result.Interruption{
			{ID: "old-stop-a", Path: "a", Reason: "cancelled", Phase: "analysis", Message: "cancelled"},
			{ID: "old-stop-b", Path: "a", Reason: "deadline-exceeded", Phase: "analysis", Message: "deadline"},
		}
		out.Coverage = []result.Coverage{
			{ID: "old-coverage-1", Path: "a", Rule: "MD001", Reason: result.ContextUnavailable, Message: "context", CauseIDs: []string{"old-io", "old-stop-b", "old-io"}},
			{ID: "old-coverage-2", Path: "a", Rule: "MD001", Reason: result.ContextUnavailable, Message: "context", CauseIDs: []string{"old-stop-b", "old-io"}},
			{Path: "a", Rule: "MD001", Reason: result.ContextUnavailable, Message: "context", CauseIDs: []string{"old-stop-a"}},
			{Path: "a", Reason: "cancelled", Message: "remaining"},
			{Rule: "MD003", Reason: result.RuleUnavailable, Message: "build-wide"},
		}
		if reverse {
			slices.Reverse(out.Coverage)
			slices.Reverse(out.Errors)
			slices.Reverse(out.Interruptions)
		}
		FinalizeLintResult(&out)
		return out
	}
	a, b := fixture(false), fixture(true)
	first, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(b)
	if err != nil || string(first) != string(second) {
		t.Fatalf("unstable full result/IDs: %s\n%s (%v)", first, second, err)
	}
	if len(a.Coverage) != 4 || len(a.Files[0].CoverageIDs) != 3 || a.Files[0].Analysis != "failed" {
		t.Fatal(a)
	}
	causes := map[string]bool{}
	for _, e := range a.Errors {
		causes[e.ID] = true
	}
	for _, i := range a.Interruptions {
		causes[i.ID] = true
	}
	coverage := map[string]bool{}
	for _, c := range a.Coverage {
		coverage[c.ID] = true
		for _, id := range c.CauseIDs {
			if !causes[id] {
				t.Fatalf("dangling remapped cause %s: %+v", id, c)
			}
		}
		if c.Rule == "MD001" && len(c.CauseIDs) < 2 {
			t.Fatalf("lost explicit or automatic cause: %+v", c)
		}
	}
	for _, id := range a.Files[0].CoverageIDs {
		if !coverage[id] {
			t.Fatalf("dangling file coverage %s", id)
		}
	}
	FinalizeLintResult(&a)
	again, err := json.Marshal(a)
	if err != nil || string(again) != string(first) {
		t.Fatalf("repeat finalization duplicated links: %s", again)
	}
}

func TestLocatedCoverageRetainsDiagnosticCoordinatesAndSnapshot(t *testing.T) {
	source := parser.NewSource([]byte("a\tπ\r\nβ"))
	index := parser.NewLineIndex(source)
	start, err := index.Position(2)
	if err != nil {
		t.Fatal(err)
	}
	end, err := index.Position(8)
	if err != nil {
		t.Fatal(err)
	}
	snap := snapshot(source)
	location := result.Range{Start: start, End: end}
	out := result.NewLint("cli")
	out.Files = append(out.Files, result.File{Path: "a", Analysis: "complete", InputSnapshot: &snap, ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}})
	out.Diagnostics = append(out.Diagnostics, result.Diagnostic{Path: "a", Rule: "MD001", Message: "observed", Range: location, ByteRange: result.ByteRange{Start: 2, End: 8}, Snapshot: snap})
	out.Coverage = append(out.Coverage, result.Coverage{Path: "a", Rule: "MD003", Reason: result.ContextUnavailable, Message: "located context", Range: &location, Snapshot: &snap})
	FinalizeLintResult(&out)
	c, d := out.Coverage[0], out.Diagnostics[0]
	if *c.Range != d.Range || *c.Snapshot != d.Snapshot || c.Range.Start != (result.Position{Line: 1, Column: 3}) || c.Range.End != (result.Position{Line: 2, Column: 2}) {
		t.Fatal(c, d)
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Range    struct{ Start, End struct{ Line, Column int } }
		Snapshot result.Snapshot
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Range.Start.Line != 1 || wire.Range.Start.Column != 3 || wire.Range.End.Line != 2 || wire.Range.End.Column != 2 || wire.Snapshot != snap {
		t.Fatalf("wire coordinates/snapshot changed: %s", data)
	}
}
