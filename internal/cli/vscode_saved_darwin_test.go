//go:build darwin && arm64 && cgo

package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/files"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/result"
)

func TestVSCodeActualPartialSavedPublication(t *testing.T) {
	ctx := context.Background()
	f := setupApplyFixture(t, ctx, nil)
	out := result.NewFix(false)
	states := publishFixUsing(ctx, f.inv, f.budget, f.native, f.acquired, f.validated, nil, &out, func(ctx context.Context, v fix.Validated, n *files.WriteSet, path string, names map[string]string) files.Publication {
		switch path {
		case "a.md":
			// Actual stale-source refusal, followed by actual saved analysis of an
			// independently edited protected EOF. Original was 1:5; candidate is clean;
			// observed saved finding is 3:8 and must never be mapped back to 1:5.
			put(t, names[path], "\n\n<!-- 😀x")
			return v.PublishMapped(ctx, n, path, names)
		case "c.md":
			if err := os.Remove(names[path]); err != nil {
				t.Fatal(err)
			}
			return files.Publication{Path: path, Started: true, Uncertain: true, Err: errors.New("publication state unknown"), Leftovers: []string{".owned-recovery"}}
		default:
			return v.PublishMapped(ctx, n, path, names)
		}
	})
	if states["a.md"].status != "refused" || !states["a.md"].observed || states["b.md"].status != "written" || states["c.md"].observed {
		t.Fatal(states)
	}
	for _, path := range []string{"a.md", "b.md", "c.md"} {
		state := states[path]
		if state.observed {
			mergeFixAnalysis(&out, state.analysis)
		}
		out.Files = append(out.Files, result.File{Path: path, WriteStatus: state.status, Leftovers: state.leftovers, ResultSnapshot: state.snapshot})
	}
	engine.FinalizeLintResult(&out)
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Snapshot.View != "saved" || out.Diagnostics[0].Range.Start.Line != 3 || out.Diagnostics[0].Range.Start.Column != 8 {
		t.Fatal(out)
	}
	frozen := cloneReport(out)
	var stdout, stderr bytes.Buffer
	if status := reportResultStreams(out, "vscode", &stdout, &stderr, nil); status != 13 {
		t.Fatal(status, stdout.String(), stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "a.md:3:8: error: MD047: ") || strings.Count(stdout.String(), "\n") != 1 {
		t.Fatal(stdout.String())
	}
	for _, evidence := range []string{"publication-failed", "saved-view-unavailable", "outcome a.md refused", "outcome b.md written", "outcome c.md unknown", ".owned-recovery", "incomplete"} {
		if !strings.Contains(stderr.String(), evidence) {
			t.Fatal("missing", evidence, stderr.String())
		}
	}
	if !reflect.DeepEqual(frozen, out) {
		t.Fatal("reporter mutated saved authority")
	}
	// The emergency branch must keep actual mixed outcomes and recovery names.
	stdout.Reset()
	stderr.Reset()
	if status := reportResultStreams(out, "vscode", &stdout, &stderr, map[string]int64{"reporting-time": 1}); status != 13 {
		t.Fatal(status)
	}
	if (stdout.Len() > 0 && !strings.HasPrefix(stdout.String(), "a.md:3:8: error: MD047: ")) || !strings.Contains(stderr.String(), "outcome b.md written") || !strings.Contains(stderr.String(), ".owned-recovery") {
		t.Fatal(stdout.String(), stderr.String())
	}
	reserve, err := emergencyVSCodeReport(out, nil)
	if err != nil || len(reserve.data) != 0 || lintExit(reserve.out) != 13 || !bytes.Contains(reserve.stderr, []byte(".owned-recovery")) || !bytes.Contains(reserve.stderr, []byte("outcome b.md written")) {
		t.Fatal(err, string(reserve.data), string(reserve.stderr))
	}
}
