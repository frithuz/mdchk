package engine_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

func TestFinalizeMergedAcquisitionAndEngineResult(t *testing.T) {
	fixture := func(reverse bool) result.Result {
		r := registry(t, []engine.Metadata{metadata("MD094")})
		out := lint(t, r, input(t, "z.md", []byte{0xff}, nil))
		out.Errors = append(out.Errors, result.Error{ID: "acquisition-error-1", Category: result.IO, Code: "read-failed", Scope: "operation", Path: "a.md", Message: "read failed"})
		out.Interruptions = append(out.Interruptions, result.Interruption{ID: "acquisition-stop-1", Path: "m.md", Phase: "acquisition", Reason: "cancelled", Message: "cancelled"})
		out.Coverage = append(out.Coverage,
			result.Coverage{Path: "a.md", Reason: "acquisition-failed", Message: "unread", CauseIDs: []string{"acquisition-error-1"}},
			result.Coverage{Path: "m.md", Reason: "cancelled", Message: "unvisited", CauseIDs: []string{"acquisition-stop-1"}},
		)
		for _, path := range []string{"m.md", "a.md"} {
			out.Files = append(out.Files, result.File{Path: path, Analysis: "not-started", WriteStatus: "not-requested", ReturnStatus: "not-requested", ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}})
		}
		out.SkippedFiles = []result.SkippedFile{{Path: "y.md", Reason: "excluded"}, {Path: "b.md", Reason: "excluded"}}
		if reverse {
			slices.Reverse(out.Errors)
			slices.Reverse(out.Coverage)
			slices.Reverse(out.Files)
			slices.Reverse(out.SkippedFiles)
		}
		engine.FinalizeLintResult(&out)
		return out
	}
	out, reversed := fixture(false), fixture(true)
	first, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(reversed)
	if err != nil || string(first) != string(second) {
		t.Fatalf("merge depends on record order: %s\n%s (%v)", first, second, err)
	}
	if out.Status != "error" || out.Complete || len(out.CheckedFiles) != 1 || out.CheckedFiles[0] != "z.md" {
		t.Fatal(out)
	}
	if len(out.Files) != 3 || out.Files[0].Path != "a.md" || out.Files[0].Analysis != "failed" || out.Files[1].Path != "m.md" || out.Files[1].Analysis != "not-started" || out.Files[2].Path != "z.md" || out.Files[2].Analysis != "failed" {
		t.Fatal(out.Files)
	}
	causes := map[string]string{}
	for _, e := range out.Errors {
		causes[e.ID] = e.Path
	}
	for _, stop := range out.Interruptions {
		causes[stop.ID] = stop.Path
	}
	for _, c := range out.Coverage {
		if len(c.CauseIDs) != 1 || causes[c.CauseIDs[0]] != c.Path {
			t.Fatalf("merge reassigned a cause to another target: %+v", c)
		}
	}
	engine.FinalizeLintResult(&out)
	again, err := json.Marshal(out)
	if err != nil || string(again) != string(first) {
		t.Fatalf("re-finalization retained stale references: %s\n%s (%v)", first, again, err)
	}
}
