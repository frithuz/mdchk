package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

// Complete registrations are test fixtures, never production availability.
func executionRegistry(t *testing.T, checks map[string]engine.Check, sourceOnly bool) *engine.Registry {
	t.Helper()
	catalog := engine.Catalog()
	var registrations []engine.Registration
	for i := range catalog {
		m := &catalog[i]
		if sourceOnly && m.ID == "MD009" {
			m.RequiresSyntax = false
		}
		if m.ID == "MD094" {
			continue
		}
		check := checks[m.ID]
		if check == nil {
			check = func(context.Context, engine.RuleContext) engine.RuleOutcome { return engine.RuleOutcome{} }
		}
		registrations = append(registrations, engine.Registration{ID: m.ID, Check: check})
	}
	registry, err := engine.NewRegistry(catalog, registrations)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func executeFixture(t *testing.T, ctx context.Context, registry *engine.Registry, caps config.Capabilities, args ...string) (int, *lintInvocation, string) {
	t.Helper()
	var inv *lintInvocation
	var stderr bytes.Buffer
	prepare := func(options lintOptions) (*lintInvocation, error) {
		var err error
		inv, err = prepareLintWith(options, registry, engine.ValidationProviders{}, caps)
		return inv, err
	}
	status := runContext(ctx, append([]string{"lint"}, args...), io.Discard, &stderr, "test", prepare,
		func(ctx context.Context, in *lintInvocation, _ io.Writer, stderr io.Writer) int {
			return acquireLintContext(ctx, in, stderr)
		})
	return status, inv, stderr.String()
}

func assertReadOnlyResult(t *testing.T, r *result.Result) {
	t.Helper()
	if r == nil {
		t.Fatal("missing execution result")
	}
	if len(r.Changes) != 0 || len(r.Documents) != 0 {
		t.Fatal("lint produced edits or returned documents")
	}
	for _, file := range r.Files {
		if file.WriteStatus != "not-requested" || len(file.ChangeIDs) != 0 {
			t.Fatalf("lint requested writes: %+v", file)
		}
	}
}

func TestExecutionSyntaxOptionsSpansAndDisabledRule(t *testing.T) {
	root := validProject(t)
	raw := "# 雪\r\nπ wide\r\n"
	path := filepath.Join(root, "doc.md")
	put(t, path, raw)
	calls := 0
	registry := executionRegistry(t, map[string]engine.Check{"MD013": func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		calls++
		if c.Document() == nil || len(c.Document().Root().Children()) != 2 || c.Options()["length"] != 3 {
			t.Fatalf("parser/options did not reach callback: %+v", c.Options())
		}
		start := bytes.Index(c.Source().Bytes(), []byte("wide"))
		span, err := c.Source().Span(start, start+4)
		if err != nil {
			t.Fatal(err)
		}
		return engine.RuleOutcome{Findings: []engine.Finding{{Message: "test width", Span: span, FixEligible: true}}}
	}}, false)
	for _, enabled := range []bool{true, false} {
		body := availableConfig + fmt.Sprintf("rules: {MD013: {enabled: %t, length: 3}}\n", enabled)
		put(t, filepath.Join(root, config.Filename), body)
		status, inv, stderr := executeFixture(t, context.Background(), registry, config.Capabilities{CommonMark: true}, "doc.md")
		want := 0
		if enabled {
			want = 10
		}
		if status != want {
			t.Fatalf("status=%d want=%d stderr=%s", status, want, stderr)
		}
		assertReadOnlyResult(t, inv.result)
		if !inv.result.Complete || len(inv.result.CheckedFiles) != 1 {
			t.Fatalf("incomplete: %+v", inv.result)
		}
		if enabled {
			if len(inv.result.Diagnostics) != 1 {
				t.Fatal(inv.result.Diagnostics)
			}
			d := inv.result.Diagnostics[0]
			start := strings.Index(raw, "wide")
			if d.Path != "doc.md" || d.Rule != "MD013" || d.ByteRange.Start != start || d.ByteRange.End != start+4 || d.Range.Start.Line != 2 || d.Range.Start.Column != 3 || d.Range.End.Column != 7 || d.Snapshot.SizeBytes != len(raw) || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(raw))) || d.FixAvailable {
				t.Fatalf("incorrect original diagnostic or future fix: %+v", d)
			}
		} else if len(inv.result.Diagnostics) != 0 {
			t.Fatal(inv.result.Diagnostics)
		}
		//nolint:gosec // Read only test-owned source/configuration to verify immutability.
		got, err := os.ReadFile(path)
		if err != nil || string(got) != raw {
			t.Fatal("source changed")
		}
		//nolint:gosec // Read only test-owned configuration.
		got, err = os.ReadFile(filepath.Join(root, config.Filename))
		if err != nil || string(got) != body {
			t.Fatal("configuration changed")
		}
	}
	if calls != 1 {
		t.Fatalf("disabled callback ran: %d calls", calls)
	}
}

func TestExecutionCancellationRetainsFindingAndStopsLaterWork(t *testing.T) {
	root := validProject(t)
	put(t, filepath.Join(root, "a.md"), "# first\n")
	put(t, filepath.Join(root, "b.md"), "# later\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first, later := 0, 0
	registry := executionRegistry(t, map[string]engine.Check{
		"MD001": func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
			first++
			span, err := c.Source().Span(2, 7)
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			return engine.RuleOutcome{Findings: []engine.Finding{{Message: "reliable before cancellation", Span: span}}}
		},
		"MD003": func(context.Context, engine.RuleContext) engine.RuleOutcome { later++; return engine.RuleOutcome{} },
	}, false)
	status, inv, stderr := executeFixture(t, ctx, registry, config.Capabilities{CommonMark: true}, "b.md", "a.md")
	if status != 15 || first != 1 || later != 0 || len(inv.result.Diagnostics) != 1 || len(inv.result.CheckedFiles) != 1 || len(inv.result.Interruptions) == 0 {
		t.Fatalf("cancelled: status=%d first=%d later=%d stderr=%s result=%+v", status, first, later, stderr, inv.result)
	}
	if len(inv.result.Files) != 2 || inv.result.Files[1].Path != "b.md" || inv.result.Files[1].Analysis != "not-started" {
		t.Fatalf("later file lost or started: %+v", inv.result.Files)
	}
	assertReadOnlyResult(t, inv.result)
}

func TestExecutionUncertainParserBlocksSyntaxAndKeepsSourceOnly(t *testing.T) {
	root := project(t)
	put(t, filepath.Join(root, config.Filename), "version: 1\nflavor: pandoc\ndirectives: {enabled: false}\n")
	put(t, filepath.Join(root, "doc.md"), "# heading\n")
	sourceCalls := 0
	registry := executionRegistry(t, map[string]engine.Check{
		"MD001": func(context.Context, engine.RuleContext) engine.RuleOutcome {
			t.Fatal("syntax callback ran with unsupported parser profile")
			return engine.RuleOutcome{}
		},
		"MD009": func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
			sourceCalls++
			if c.Document() != nil {
				t.Fatal("partial tree escaped to source-only callback")
			}
			return engine.RuleOutcome{}
		},
	}, true)
	status, inv, stderr := executeFixture(t, context.Background(), registry, config.Capabilities{Pandoc3121FullProfile: true}, "doc.md")
	if status != 15 || sourceCalls != 1 || len(inv.result.Coverage) == 0 || inv.result.Complete {
		t.Fatalf("uncertainty: status=%d calls=%d stderr=%s result=%+v", status, sourceCalls, stderr, inv.result)
	}
}

func TestExecutionRuleFailureOutranksIO(t *testing.T) {
	root := validProject(t)
	put(t, filepath.Join(root, "doc.md"), "# heading\n")
	registry := executionRegistry(t, map[string]engine.Check{"MD001": func(context.Context, engine.RuleContext) engine.RuleOutcome {
		return engine.RuleOutcome{Err: errors.New("fixture callback failure")}
	}}, false)
	status, inv, stderr := executeFixture(t, context.Background(), registry, config.Capabilities{CommonMark: true}, "missing.md", "doc.md")
	if status != 14 || len(inv.result.Errors) < 2 {
		t.Fatalf("failure precedence: status=%d stderr=%s result=%+v", status, stderr, inv.result)
	}
}

func TestExecutionMixedOutcomesCanonicalAndSchema(t *testing.T) {
	root := validProject(t)
	put(t, filepath.Join(root, config.Filename), availableConfig+"exclude: ['ignored.md']\n")
	put(t, filepath.Join(root, "good.md"), "# good\n")
	put(t, filepath.Join(root, "bad.md"), "雪\xff")
	put(t, filepath.Join(root, "ignored.md"), "excluded")
	put(t, filepath.Join(root, "violation.md"), "# violation\r\n")
	put(t, filepath.Join(root, "z.md"), "quota")
	put(t, filepath.Join(root, "zz.md"), "not started")
	registry := executionRegistry(t, map[string]engine.Check{"MD001": func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		if !strings.Contains(c.Path(), "violation") {
			return engine.RuleOutcome{}
		}
		span, err := c.Source().Span(2, 11)
		if err != nil {
			t.Fatal(err)
		}
		return engine.RuleOutcome{Findings: []engine.Finding{{Message: "fixture violation", Span: span}}}
	}}, false)
	paths := []string{"violation.md", "missing.md", "ignored.md", "good.md", "bad.md", "z.md", "zz.md", "--limit=document-bytes=16", "--limit=total-content-bytes=24"}
	status, inv, stderr := executeFixture(t, context.Background(), registry, config.Capabilities{CommonMark: true}, paths...)
	if status != 13 || len(inv.result.Diagnostics) != 1 || len(inv.result.Errors) != 2 || len(inv.result.SkippedFiles) != 1 || len(inv.result.CheckedFiles) != 3 || len(inv.result.Files) != 6 || len(inv.result.Interruptions) != 1 {
		t.Fatalf("mixed: status=%d stderr=%s result=%+v", status, stderr, inv.result)
	}
	for i, j := 0, len(paths)-1; i < j; i, j = i+1, j-1 {
		paths[i], paths[j] = paths[j], paths[i]
	}
	status2, inv2, stderr2 := executeFixture(t, context.Background(), registry, config.Capabilities{CommonMark: true}, paths...)
	if status2 != status || !reflect.DeepEqual(inv.result, inv2.result) {
		t.Fatalf("argument order changed canonical envelope: status=%d stderr=%s\nfirst=%+v\nsecond=%+v", status2, stderr2, inv.result, inv2.result)
	}
	assertReadOnlyResult(t, inv.result)
	validateExecutionEnvelope(t, root, inv.result)
}

func validateExecutionEnvelope(t *testing.T, root string, r *result.Result) {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), ":null") {
		t.Fatalf("null required collection: %s", data)
	}
	python := os.Getenv("MDCHK_RESULT_VALIDATOR_PYTHON")
	if python == "" {
		t.Log("schema execution requires MDCHK_RESULT_VALIDATOR_PYTHON")
		return
	}
	path := filepath.Join(root, "result.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	// Tests chdir into fixtures; resolve the validator through the source file's
	// package location before entering the fixture, supplied by go test's cwd.
	validator := filepath.Join(executionPackageDir, "..", "..", "docs", "contracts", "results", "validate.py")
	script := "import json,runpy,sys; v=runpy.run_path(sys.argv[1]); x=json.load(open(sys.argv[2])); v['validator'].validate(x); v['semantic'](x)"
	output, err := exec.Command(python, "-c", script, validator, path).CombinedOutput() //nolint:gosec // Test-only interpreter is explicitly provided; code and paths are controlled fixtures.
	if err != nil {
		t.Fatalf("pipeline producer schema: %v\n%s", err, output)
	}
}

var executionPackageDir = func() string {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return cwd
}()

func TestExecutionSharesAcquisitionWorkBudget(t *testing.T) {
	root := validProject(t)
	raw := "# x\n"
	put(t, filepath.Join(root, "doc.md"), raw)
	calls := 0
	registry := executionRegistry(t, map[string]engine.Check{"MD001": func(context.Context, engine.RuleContext) engine.RuleOutcome { calls++; return engine.RuleOutcome{} }}, false)
	options := lintOptions{paths: []string{"doc.md"}, limits: map[string]int64{"work-units": 25}}
	inv, err := prepareLintWith(options, registry, engine.ValidationProviders{}, config.Capabilities{CommonMark: true})
	if err != nil {
		t.Fatal(err)
	}
	status := acquireLintContext(context.Background(), inv, io.Discard)
	if status != 15 || calls != 0 || len(inv.acquisition.Documents) != 1 || len(inv.result.Interruptions) != 1 {
		t.Fatalf("shared budget: status=%d calls=%d acquired=%+v result=%+v", status, calls, inv.acquisition, inv.result)
	}
	stop := inv.result.Interruptions[0]
	// Reading and retained snapshot scans spend 3n+2 units; the engine's single
	// mandatory validation reservation then spends 4n+1. A fresh engine budget
	// would permit that reservation and incorrectly start Markdown parsing.
	if stop.Phase != "acquisition" || stop.Limit == nil || stop.Limit.Name != "work-units" || stop.Limit.Allowed != 25 || stop.Limit.Observed != 7*int64(len(raw))+3 {
		t.Fatalf("budget was reset or source validation duplicated: %+v", stop)
	}
}
