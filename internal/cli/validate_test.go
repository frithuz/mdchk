package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/configscan"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

func runValidate(t *testing.T, root string, want int, extra ...string) result.Result {
	t.Helper()
	args := append([]string{"config", "validate", "--format=json", root}, extra...)
	var out, logs bytes.Buffer
	if got := Run(args, &out, &logs, "test"); got != want || logs.Len() != 0 {
		t.Fatalf("%v: status=%d want=%d stdout=%s stderr=%s", args, got, want, &out, &logs)
	}
	r := validateEmitted(t, t.TempDir(), out.Bytes())
	if r.Operation != "config validate" || r.ConfigValidation == nil {
		t.Fatalf("wrong operation: %+v", r)
	}
	if len(r.Diagnostics)+len(r.Coverage)+len(r.Changes)+len(r.DeferredFixes)+len(r.Documents)+len(r.SuppressedDiagnostics) != 0 {
		t.Fatalf("Markdown work escaped: %+v", r)
	}
	return r
}

func TestValidateIndependentDefinitionsAndEvidence(t *testing.T) {
	root := validationProject(t)
	parent := "version: 1\nflavor: commonmark\nallow_nested_excludes: false\n"
	child := "version: 1\nflavor: commonmark\nexclude: ['**']\n"
	duplicate := "version: 1\nflavor: commonmark\n# 雪\nflavor: commonmark\n"
	for name, body := range map[string]string{".mdchk.yaml": parent, "child/.mdchk.yaml": child, "child/grand/.mdchk.yaml": "version: 1\nflavor: commonmark\n", "other/.mdchk.yaml": duplicate, "markdown.md": "# Unchanged\n"} {
		put(t, filepath.Join(root, name), body)
	}
	r := runValidate(t, root, 12)
	s := r.ConfigValidation
	if !r.Complete || !s.ScanComplete || s.DefinitionsChecked != 4 || s.ValidDefinitions != 2 || s.InvalidDefinitions != 2 || r.Status != "error" || len(r.Errors) != 4 {
		t.Fatalf("incorrect aggregate %+v errors=%+v", s, r.Errors)
	}
	for _, f := range r.Files {
		if f.Analysis != "complete" || f.InputSnapshot == nil || f.ResultSnapshot == nil {
			t.Fatalf("invalid readable outcome %+v", f)
		}
	}
	for _, e := range r.Errors {
		if e.Range == nil || e.Snapshot == nil || filepath.IsAbs(e.Path) {
			t.Fatalf("unlocated origin %+v", e)
		}
		if e.Path == ".mdchk.yaml" && e.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(parent))) {
			t.Fatalf("wrong ancestor snapshot %+v", e)
		}
	}
	for name, body := range map[string]string{".mdchk.yaml": parent, "child/.mdchk.yaml": child, "other/.mdchk.yaml": duplicate, "markdown.md": "# Unchanged\n"} {
		got, e := os.ReadFile(filepath.Join(root, name)) //nolint:gosec // Controlled temporary fixture paths.
		if e != nil || string(got) != body {
			t.Fatalf("fixture changed %s", name)
		}
	}
	var out bytes.Buffer
	if got := Run([]string{"config", "validate", root}, &out, io.Discard, "test"); got != 12 || !strings.Contains(out.String(), "Checked 4 definitions: 2 valid, 2 invalid") {
		t.Fatalf("text %d %s", got, &out)
	}
}

func TestValidateEmptyPruningAndRuntimeLimits(t *testing.T) {
	root := validationProject(t)
	r := runValidate(t, root, 0)
	if !r.Complete || r.ConfigValidation.DefinitionsChecked != 0 {
		t.Fatal(r)
	}
	put(t, filepath.Join(root, ".mdchk.yaml"), "version: 1\nflavor: commonmark\nexclude: ['pruned', 'hidden/.mdchk.yaml']\n")
	put(t, filepath.Join(root, "pruned/.mdchk.yaml"), "invalid: [")
	put(t, filepath.Join(root, "hidden/.mdchk.yaml"), "invalid: [")
	r = runValidate(t, root, 0)
	if r.ConfigValidation.DefinitionsChecked != 1 || r.ConfigValidation.SkippedDefinitions != 1 || len(r.SkippedFiles) != 3 {
		t.Fatalf("pruned aggregate %+v", r)
	}
	r = runValidate(t, root, 15, "--limit=config-bytes=16B")
	if r.ConfigValidation.ScanComplete || len(r.Errors) != 0 || len(r.Interruptions) != 1 || r.Interruptions[0].Limit == nil {
		t.Fatal(r)
	}
	r = runValidate(t, root, 15, "--limit=operation-time=1ns")
	if r.Complete || len(r.Interruptions) != 1 || len(r.Errors) != 0 {
		t.Fatal(r)
	}
	r = runValidate(t, root, 15, "--limit=work-units=1")
	if r.ConfigValidation.DefinitionsChecked != 0 || r.Complete {
		t.Fatal(r)
	}
	r = runValidate(t, root, 15, "--limit=directory-entries=1")
	if r.ConfigValidation.ScanComplete {
		t.Fatal(r)
	}
	r = runValidate(t, root, 15, "--limit=structured-input-depth=1")
	if r.Complete || len(r.Errors) != 0 || r.Interruptions[0].Limit.Name != "structured-input-depth" || len(r.CheckedFiles) != 1 || r.ConfigValidation.DefinitionsChecked != 0 || len(r.Files) != 1 || r.Files[0].Analysis != "partial" {
		t.Fatal(r)
	}
}

func TestValidateUsageMetadataAndPresentation(t *testing.T) {
	root := validationProject(t)
	for _, args := range [][]string{{"config", "--help"}, {"--help", "config", "validate"}, {"config", "validate", "--help"}, {"config", "validate", "--version"}, {"--version", "config", "validate"}} {
		var out bytes.Buffer
		if got := Run(args, &out, io.Discard, "test"); got != 0 {
			t.Fatalf("%v %d %s", args, got, &out)
		}
	}
	for _, extra := range [][]string{{"--config=x"}, {"--dry-run"}, {"--bad"}, {"extra"}, {"--limit=document-bytes=1"}, {"--limit=structured-input-depth=4097"}, {"--limit=path-depth=4097"}} {
		r := runValidate(t, root, 11, extra...)
		if len(r.Files) != 0 || len(r.Errors) != 1 || r.Errors[0].Category != result.Usage {
			t.Fatal(r)
		}
	}
	for _, format := range []string{"vscode", "github"} {
		var out bytes.Buffer
		if got := Run([]string{"config", "validate", "missing", "--format=" + format}, &out, io.Discard, "test"); got != 11 || !strings.Contains(out.String(), "unsupported-format") {
			t.Fatalf("%s %d %s", format, got, &out)
		}
	}
	put(t, filepath.Join(root, "a.md"), "unchanged")
	runValidate(t, filepath.Join(root, "a.md"), 11)
	runValidate(t, filepath.Join(root, "missing"), 11)
	put(t, filepath.Join(root, "--dash/.mdchk.yaml"), "version: 1\nflavor: commonmark\n")
	var out bytes.Buffer
	if got := Run([]string{"config", "validate", "--format=json", "--", "--dash"}, &out, io.Discard, "test"); got != 0 {
		t.Fatalf("literal path %d %s", got, &out)
	}
	validateEmitted(t, t.TempDir(), out.Bytes())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out.Reset()
	if got := RunContext(ctx, []string{"config", "validate", "--format=json", root}, &out, io.Discard, "test"); got != 15 {
		t.Fatalf("cancel %d %s", got, &out)
	}
	r := validateEmitted(t, t.TempDir(), out.Bytes())
	if r.ConfigValidation.ScanComplete || len(r.Coverage) != 0 {
		t.Fatal(r)
	}
}

func TestValidateFailureRolesAndReportReduction(t *testing.T) {
	root := validationProject(t)
	source := config.SourceSnapshot{Path: filepath.Join(root, ".mdchk.yaml"), SHA256: strings.Repeat("a", 64), SizeBytes: 2}
	scanned := configscan.ValidationResult{Scan: configscan.Result{Complete: false, Errors: []configscan.Failure{{Path: source.Path, Role: configscan.DefinitionSource, Err: fmt.Errorf("unreadable")}, {Path: filepath.Join(root, "ignore/.mdchk.yaml"), Role: configscan.IgnoreSource, Err: fmt.Errorf("ignore unreadable")}}}}
	r := validationResult(scanned)
	if lintExit(r) != 13 || len(r.Files) != 1 || r.Files[0].InputSnapshot != nil || r.ConfigValidation.DefinitionsChecked != 0 {
		t.Fatal(r)
	}
	scanned.Scan.Complete = true
	scanned.Scan.Errors = []configscan.Failure{{Path: source.Path, Role: configscan.DefinitionSource, Err: fmt.Errorf("invalid %s", strings.Repeat("long", 30000))}}
	scanned.Outcomes = []configscan.ValidationOutcome{{Source: source, Err: scanned.Scan.Errors[0].Err}}
	r = validationResult(scanned)
	for _, format := range []string{"json", "text"} {
		limits := map[string]int64{"response-bytes": 65536}
		encoded, e := buildReport(context.Background(), r, format, limits)
		if e != nil {
			t.Fatal(e)
		}
		got := encoded.out
		if len(encoded.data) > 65536 || lintExit(got) != 12 || got.Complete || !got.ConfigValidation.ScanComplete || got.ConfigValidation.InvalidDefinitions != 1 || got.Files[0].Analysis != "complete" || len(got.Omissions) == 0 {
			t.Fatalf("reduction %+v", got)
		}
		if format == "json" {
			validateEmitted(t, t.TempDir(), encoded.data)
		}
		fallback, e := deadlineReport(context.Background(), r, format, limits)
		if e != nil || fallback.out.ConfigValidation.InvalidDefinitions != 1 || fallback.out.Files[0].Analysis != "complete" || lintExit(fallback.out) != 12 {
			t.Fatalf("fallback %+v %v", fallback.out, e)
		}
	}
	r.Interruptions = append(r.Interruptions, result.Interruption{Reason: "cancelled", Phase: "configuration", Message: "Cancelled"})
	if lintExit(r) != 12 {
		t.Fatalf("configuration must outrank cancellation: %d", lintExit(r))
	}
	var short validationShortWriter
	if status := reportResult(r, "json", short, nil); status != 13 {
		t.Fatalf("delivery status %d", status)
	}
}

type validationShortWriter struct{}

func (validationShortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func validationProject(t *testing.T) string {
	t.Helper()
	root := project(t)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "absent-gitconfig"))
	return root
}

func TestValidateOversizedOutcomeManifestIsInterruption(t *testing.T) {
	r := result.NewConfigValidation()
	r.ConfigValidation.DefinitionsChecked = 200
	r.ConfigValidation.ValidDefinitions = 200
	for i := 0; i < 200; i++ {
		path := fmt.Sprintf("%03d/%s/.mdchk.yaml", i, strings.Repeat("x", 400))
		snapshot := result.Snapshot{View: "original", SHA256: strings.Repeat("a", 64), SizeBytes: 1}
		r.CheckedFiles = append(r.CheckedFiles, path)
		r.Files = append(r.Files, result.File{Path: path, Analysis: "complete", WriteStatus: "not-requested", ReturnStatus: "not-requested", InputSnapshot: &snapshot, ResultSnapshot: &snapshot, ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}})
	}
	for _, format := range []string{"json", "text"} {
		encoded, e := buildReport(context.Background(), r, format, map[string]int64{"response-bytes": 65536})
		if e != nil {
			t.Fatal(e)
		}
		out := encoded.out
		if len(encoded.data) > 65536 || lintExit(out) != 15 || out.Complete || !out.ConfigValidation.ScanComplete || out.ConfigValidation.ValidDefinitions != 200 || len(out.Files) != 0 || len(out.CheckedFiles) != 0 {
			t.Fatalf("manifest reduction format=%s status=%d files=%d omissions=%d summary=%+v", format, lintExit(out), len(out.Files), len(out.Omissions), out.ConfigValidation)
		}
		if format == "json" {
			validateEmitted(t, t.TempDir(), encoded.data)
		}
	}
	for i := 0; i < 200; i++ {
		r.Errors = append(r.Errors, result.Error{Path: r.Files[i].Path, Category: result.Configuration, Scope: "operation", Code: "invalid", Message: "Invalid definition."})
	}
	r.Errors = append(r.Errors, result.Error{Category: result.IO, Scope: "operation", Code: "ignore-read", Message: "Ignore source read failed."})
	r.ConfigValidation.ValidDefinitions = 0
	r.ConfigValidation.InvalidDefinitions = 200
	for _, format := range []string{"json", "text"} {
		encoded, e := buildReport(context.Background(), r, format, map[string]int64{"response-bytes": 65536})
		if e != nil {
			t.Fatal(e)
		}
		out := encoded.out
		if lintExit(out) != 13 || len(out.Errors) != 2 || out.ConfigValidation.InvalidDefinitions != 200 || out.Complete || !out.ConfigValidation.ScanComplete {
			t.Fatalf("category retention %+v", out)
		}
		if format == "json" {
			validateEmitted(t, t.TempDir(), encoded.data)
		}
	}
}

func TestValidateExcludedDefinitionSourceKinds(t *testing.T) {
	for _, kind := range []string{"symlink", "directory", "regular"} {
		t.Run(kind, func(t *testing.T) {
			root := validationProject(t)
			put(t, filepath.Join(root, ".mdchk.yaml"), "version: 1\nflavor: commonmark\nexclude: ['child/.mdchk.yaml']\n")
			definition := filepath.Join(root, "child/.mdchk.yaml")
			put(t, filepath.Join(root, "target"), "invalid target source: [")
			switch kind {
			case "symlink":
				if e := os.MkdirAll(filepath.Dir(definition), 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink("../target", definition); e != nil {
					t.Fatal(e)
				}
			case "directory":
				put(t, filepath.Join(definition, ".mdchk.yaml"), "invalid nested source: [")
			case "regular":
				put(t, definition, "invalid excluded source: [")
			}
			r := runValidate(t, root, 0)
			count := 0
			for _, skip := range r.SkippedFiles {
				if skip.Path == "child/.mdchk.yaml" {
					count++
					want := "configuration-exclusion"
					if kind == "symlink" {
						want = "symlink"
					}
					if skip.Reason != want {
						t.Fatalf("wrong source-kind reason %+v", skip)
					}
				}
			}
			expectedSkipped := 0
			if kind == "regular" {
				expectedSkipped = 1
			}
			if count != 1 || !r.Complete || !r.ConfigValidation.ScanComplete || r.ConfigValidation.DefinitionsChecked != 1 || r.ConfigValidation.ValidDefinitions != 1 || r.ConfigValidation.InvalidDefinitions != 0 || r.ConfigValidation.SkippedDefinitions != expectedSkipped || len(r.CheckedFiles) != 1 || len(r.Files) != 1 {
				t.Fatalf("source kind %s duplicate/count mismatch skips=%+v summary=%+v", kind, r.SkippedFiles, r.ConfigValidation)
			}
		})
	}
}

func TestValidateObservedLimitUnitsMatchEffectiveLimits(t *testing.T) {
	for _, tc := range []struct {
		name, unit string
		limits     []string
		completed  int
	}{
		{"config-bytes", "bytes", []string{"config-bytes=16B"}, 0},
		{"total-config-bytes", "bytes", []string{"config-bytes=30B", "total-config-bytes=45B"}, 1},
		{"path-bytes", "bytes", []string{"path-bytes=12B"}, 1},
		{"directory-entries", "observed entries", []string{"directory-entries=1"}, 0},
		{"work-units", "metered work units", []string{"work-units=1"}, 0},
		{"document-nodes", "nodes or source records", []string{"document-nodes=1"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := validationProject(t)
			// Exactly thirty source bytes, independently beneath the aggregate bound.
			source := "version: 1\nflavor: commonmark\n"
			if len(source) != 30 {
				t.Fatal("incorrect fixture byte count")
			}
			put(t, filepath.Join(root, ".mdchk.yaml"), source)
			put(t, filepath.Join(root, "child/.mdchk.yaml"), source)
			args := []string{}
			for _, limit := range tc.limits {
				args = append(args, "--limit="+limit)
			}
			r := runValidate(t, root, 15, args...)
			if r.Complete || r.ConfigValidation.ScanComplete || r.ConfigValidation.DefinitionsChecked != tc.completed || len(r.Errors) != 0 || len(r.Interruptions) != 1 || r.Interruptions[0].Limit == nil {
				t.Fatalf("incorrect quota outcome summary=%+v interruptions=%+v", r.ConfigValidation, r.Interruptions)
			}
			observation := r.Interruptions[0].Limit
			if observation.Name != tc.name || observation.Unit != tc.unit || observation.Observed <= observation.Allowed {
				t.Fatalf("wrong observed units %+v; want %s %s", observation, tc.name, tc.unit)
			}
			matched := false
			for _, effective := range r.EffectiveLimits {
				if effective.Name == tc.name {
					matched = true
					if effective.Unit != tc.unit || effective.Unit != observation.Unit || effective.Value != observation.Allowed || effective.Source != "operator" {
						t.Fatalf("inconsistent quota observation=%+v effective=%+v", observation, effective)
					}
				}
			}
			if !matched {
				t.Fatalf("missing effective limit %s", tc.name)
			}
		})
	}
}

func TestValidateActualTinyReportingDeadline(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		for _, format := range []string{"text", "json"} {
			for _, duration := range []string{"1ns", "1us"} {
				t.Run(fmt.Sprintf("invalid=%t/%s/%s", invalid, format, duration), func(t *testing.T) {
					root := validationProject(t)
					source := "version: 1\nflavor: commonmark\n"
					want := 15
					if invalid {
						source = "version: 2\n"
						want = 12
					}
					put(t, filepath.Join(root, ".mdchk.yaml"), source)
					var out, logs bytes.Buffer
					got := Run([]string{"config", "validate", root, "--format=" + format, "--limit=reporting-time=" + duration}, &out, &logs, "test")
					allowedFastComplete := !invalid && duration == "1us" && got == 0
					if got != want && !allowedFastComplete || out.Len() == 0 || logs.Len() != 0 {
						t.Fatalf("actual tiny deadline status=%d want=%d stdout=%s stderr=%s", got, want, &out, &logs)
					}
					if format == "json" {
						r := validateEmitted(t, t.TempDir(), out.Bytes())
						if !r.ConfigValidation.ScanComplete || r.ConfigValidation.DefinitionsChecked != 1 || r.ConfigValidation.InvalidDefinitions != btoi(invalid) || r.ConfigValidation.ValidDefinitions != btoi(!invalid) || len(r.Coverage) != 0 {
							t.Fatalf("deadline lost actual decisions: %+v", r.ConfigValidation)
						}
						if r.Complete {
							if duration != "1us" || len(r.Interruptions) != 0 {
								t.Fatal("unexpected completed deadline result")
							}
						} else if len(r.Interruptions) == 0 {
							t.Fatal("deadline interruption missing")
						}
						if lintExit(r) != got {
							t.Fatalf("wrong emitted precedence %d", lintExit(r))
						}
					} else if duration == "1ns" && !strings.Contains(out.String(), "reporting-time exhausted") || !strings.Contains(out.String(), "Checked 1 definitions") {
						t.Fatalf("missing partial text result: %s", &out)
					}
				})
			}
		}
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestValidateEmergencyReportingRetainsCategoriesAndDelivery(t *testing.T) {
	r := result.NewConfigValidation()
	r.ConfigValidation.DefinitionsChecked = 2000
	r.ConfigValidation.InvalidDefinitions = 2000
	for i := 0; i < 2000; i++ {
		path := fmt.Sprintf("%04d/%s/.mdchk.yaml", i, strings.Repeat("x", 200))
		r.CheckedFiles = append(r.CheckedFiles, path)
		r.Files = append(r.Files, result.File{Path: path, Analysis: "complete", WriteStatus: "not-requested", ReturnStatus: "not-requested", ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}})
		r.Errors = append(r.Errors, result.Error{Path: path, Category: result.Configuration, Scope: "operation", Code: "config-version", Message: strings.Repeat("invalid ", 200)})
	}
	// Finalize exactly as the command does, accumulating bounded category counts
	// while the already-existing canonical error pass visits each observation.
	engine.FinalizeLintResult(&r)
	for _, format := range []string{"json", "text"} {
		for _, duration := range []int64{1, 1000, 1000000} {
			var out bytes.Buffer
			if got := reportResult(r, format, &out, map[string]int64{"reporting-time": duration}); got != 12 || out.Len() == 0 {
				t.Fatalf("large deadline %s/%d status=%d", format, duration, got)
			}
			if format == "json" {
				got := validateEmitted(t, t.TempDir(), out.Bytes())
				if !got.ConfigValidation.ScanComplete || got.ConfigValidation.InvalidDefinitions != 2000 || got.Complete || len(got.Errors) == 0 || len(got.Omissions) == 0 {
					t.Fatalf("lost large result summary %+v", got.ConfigValidation)
				}
			}
		}
	}
	r.Errors = append(r.Errors, result.Error{Category: result.IO, Scope: "operation", Code: "ignore-read", Message: "ignore source unavailable"})
	engine.FinalizeLintResult(&r)
	var out bytes.Buffer
	if got := reportResult(r, "json", &out, map[string]int64{"reporting-time": 1}); got != 13 {
		t.Fatalf("lost IO precedence %d", got)
	}
	got := validateEmitted(t, t.TempDir(), out.Bytes())
	if len(got.Errors) != 2 || !strings.Contains(got.Errors[0].Message, "2000") {
		t.Fatalf("lost aggregate categories %+v", got.Errors)
	}
	r.Errors = append(r.Errors, result.Error{Category: result.Internal, Scope: "operation", Code: "internal", Message: "internal failure"})
	engine.FinalizeLintResult(&r)
	out.Reset()
	if status := reportResult(r, "json", &out, map[string]int64{"reporting-time": 1}); status != 14 {
		t.Fatalf("lost internal precedence %d", status)
	}
	validateEmitted(t, t.TempDir(), out.Bytes())
	if status := reportResult(r, "json", validationShortWriter{}, map[string]int64{"reporting-time": 1}); status != 14 {
		t.Fatalf("delivery must retain internal 14: %d", status)
	}
	r.Errors = slices.DeleteFunc(r.Errors, func(e result.Error) bool { return e.Category == result.Internal })
	engine.FinalizeLintResult(&r)
	if status := reportResult(r, "json", validationShortWriter{}, map[string]int64{"reporting-time": 1}); status != 13 {
		t.Fatalf("actual failed delivery must return IO 13: %d", status)
	}
}

func TestValidateActualLargeReportingDeadline(t *testing.T) {
	root := validationProject(t)
	put(t, filepath.Join(root, ".mdchk.yaml"), "version: 2\n")
	for i := 0; i < 1000; i++ {
		put(t, filepath.Join(root, fmt.Sprintf("%04d-%s", i, strings.Repeat("x", 180)), ".mdchk.yaml"), "version: 1\nflavor: commonmark\n")
	}
	for _, format := range []string{"json", "text"} {
		var out, logs bytes.Buffer
		status := Run([]string{"config", "validate", root, "--format=" + format, "--limit=reporting-time=1us", "--limit=work-units=500000000"}, &out, &logs, "test")
		if status != 12 || out.Len() == 0 || logs.Len() != 0 {
			t.Fatalf("large actual command status=%d stdout bytes=%d stderr=%s", status, out.Len(), &logs)
		}
		if format == "json" {
			r := validateEmitted(t, t.TempDir(), out.Bytes())
			if !r.ConfigValidation.ScanComplete || r.ConfigValidation.DefinitionsChecked != 1001 || r.ConfigValidation.ValidDefinitions != 1000 || r.ConfigValidation.InvalidDefinitions != 1 || r.Complete || len(r.Errors) == 0 || len(r.Interruptions) == 0 || len(r.Omissions) == 0 {
				t.Fatalf("large actual deadline lost outcomes: %+v", r.ConfigValidation)
			}
			found := false
			for _, stop := range r.Interruptions {
				found = found || stop.Phase == "reporting" && stop.Reason == "deadline-exceeded"
			}
			if !found {
				t.Fatal("actual reporting deadline did not occur")
			}
		} else if !strings.Contains(out.String(), "Checked 1001 definitions: 1000 valid, 1 invalid") || !strings.Contains(out.String(), "reporting-time exhausted") {
			t.Fatalf("large actual text omitted authoritative outcome: %s", &out)
		}
	}
}
