package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/configscan"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/report"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/testenv"
)

func documentedMatcher(t *testing.T) *regexp.Regexp {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(executionPackageDir, "../../docs/output-formats.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range strings.Split(string(raw), "```json\n")[1:] {
		text, _, _ := strings.Cut(block, "```")
		var task struct {
			Tasks []struct {
				ProblemMatcher struct {
					Pattern struct {
						Regexp                                      string
						File, Line, Column, Severity, Code, Message int
					}
				}
			}
		}
		if json.Unmarshal([]byte(text), &task) != nil || len(task.Tasks) == 0 {
			continue
		}
		p := task.Tasks[0].ProblemMatcher.Pattern
		if p.Regexp == "" {
			continue
		}
		if got := []int{p.File, p.Line, p.Column, p.Severity, p.Code, p.Message}; !reflect.DeepEqual(got, []int{1, 2, 3, 4, 5, 6}) {
			t.Fatal(got)
		}
		return regexp.MustCompile(p.Regexp)
	}
	t.Fatal("documented task matcher absent")
	return nil
}

type vscodeFailWriter struct {
	short bool
	calls int
}

func (w *vscodeFailWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.short {
		return len(p) - 1, nil
	}
	return 0, errors.New("stream unavailable")
}
func TestVSCodeActualProductionLintFixAndConfiguration(t *testing.T) {
	root := project(t)
	put(t, filepath.Join(root, config.Filename), fixConfiguration())
	raw := "\t😀é雪"
	put(t, filepath.Join(root, "doc.md"), raw)
	registry, providers, caps, err := configuredProviders()
	if err != nil {
		t.Fatal(err)
	}
	inv, err := prepareLintWith(lintOptions{paths: []string{"doc.md"}}, registry, providers, caps)
	if err != nil {
		t.Fatal(err)
	}
	if got := acquireLintContext(context.Background(), inv, io.Discard); got != 10 {
		t.Fatal(got)
	}
	var stdout, stderr bytes.Buffer
	if got := report.ReportResultStreams(*inv.result, "vscode", &stdout, &stderr, nil); got != 10 {
		t.Fatal(got)
	}
	// Tab, astral, combining and CJK each count one scalar, plus EOF column.
	want := "doc.md:1:6: error: MD047: "
	if !strings.HasPrefix(stdout.String(), want) || stderr.Len() != 0 {
		t.Fatal(stdout.String(), stderr.String())
	}
	validator, produce, err := configuredCorrections(registry)
	if err != nil {
		t.Fatal(err)
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	raw = "hello"
	put(t, filepath.Join(root, "doc.md"), raw)
	before, err := os.Stat(filepath.Join(root, "doc.md"))
	if err != nil {
		t.Fatal(err)
	}
	fixed := executeFix(context.Background(), lintOptions{paths: []string{"doc.md"}, dryRun: true}, budget, registry, providers, caps, validator, produce)
	if len(fixed.Changes) != 1 || len(fixed.Diagnostics) != 0 {
		t.Fatal(fixed)
	}
	stdout.Reset()
	stderr.Reset()
	if got := report.ReportResultStreams(fixed, "vscode", &stdout, &stderr, nil); got != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatal(got, stdout.String(), stderr.String())
	}
	// Actual protected EOF remains manual in the canonical retained candidate.
	put(t, filepath.Join(root, "protected.md"), "\n<!-- 😀x")
	budget, _ = parser.NewOperationBudget(parser.DefaultLimits())
	manual := executeFix(context.Background(), lintOptions{paths: []string{"protected.md"}, dryRun: true}, budget, registry, providers, caps, validator, produce)
	if len(manual.Diagnostics) != 1 || manual.Diagnostics[0].Snapshot.View != "candidate" {
		t.Fatal(manual)
	}
	stdout.Reset()
	stderr.Reset()
	if got := report.ReportResultStreams(manual, "vscode", &stdout, &stderr, nil); got != 10 || !strings.HasPrefix(stdout.String(), "protected.md:2:8: error: MD047: ") || stderr.Len() != 0 {
		t.Fatal(got, stdout.String(), stderr.String())
	}
	// Reporter never reopens the target and never changes inode, mode or mtime.
	after, err := os.Stat(filepath.Join(root, "doc.md"))
	if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("reporter changed source", err)
	}
	//nolint:gosec // Inspect only test-owned fixture contents.
	actual, err := os.ReadFile(filepath.Join(root, "doc.md"))
	if err != nil || string(actual) != raw {
		t.Fatal(string(actual), err)
	}
	put(t, filepath.Join(root, config.Filename), "version: 1\nflavor: commonmark\nrules:\n  MD013: {length: -1}\n")
	_, err = prepareLintWith(lintOptions{}, registry, providers, caps)
	if err == nil {
		t.Fatal("missing configuration error")
	}
	testenv.Clear(t)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "absent-gitconfig"))
	configuration := validationResult(configscan.Validate(context.Background(), configscan.Options{Root: root}, registry, providers, caps))
	if configuration.ConfigValidation.DefinitionsChecked != 1 || len(configuration.Errors) == 0 {
		t.Fatal(configuration)
	}
	stdout.Reset()
	stderr.Reset()
	if got := report.ReportResultStreams(configuration, "vscode", &stdout, &stderr, nil); got != 12 || !strings.HasPrefix(stdout.String(), ".mdchk.yaml:4:") || !strings.Contains(stdout.String(), ": error: config-definition: ") || stderr.Len() != 0 {
		t.Fatal(got, stdout.String(), stderr.String())
	}
	captures := documentedMatcher(t).FindStringSubmatch(strings.TrimSuffix(stdout.String(), "\n"))
	if len(captures) != 7 {
		t.Fatal(captures)
	}
	if _, err := strconv.Atoi(captures[3]); err != nil {
		t.Fatal(err)
	}
}

func TestVSCodeActualConfigurationInterruptionReserve(t *testing.T) {
	root := validationProject(t)
	put(t, filepath.Join(root, config.Filename), fixConfiguration())
	put(t, filepath.Join(root, "first.md"), "first\n")
	put(t, filepath.Join(root, "second.md"), "second\n")
	registry, providers, caps, err := configuredProviders()
	if err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"cancelled", "deadline-exceeded", "limit-exceeded"} {
		t.Run(reason, func(t *testing.T) {
			ctx := context.Background()
			limits := configscan.DefaultLimits()
			switch reason {
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "deadline-exceeded":
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Unix(1, 0))
				defer cancel()
			case "limit-exceeded":
				limits.Entries = 1
			}
			original := validationResult(configscan.Validate(ctx, configscan.Options{Root: root, Limits: limits}, registry, providers, caps))
			if len(original.Interruptions) != 1 || original.Interruptions[0].Phase != "configuration" || original.Interruptions[0].Reason != reason {
				t.Fatal(original)
			}
			if reason == "limit-exceeded" {
				at := original.Interruptions[0].Limit
				if at == nil || at.Name != "directory-entries" || at.Observed != 2 || at.Allowed != 1 || at.Unit != "observed entries" {
					t.Fatal(at)
				}
			}
			for _, category := range []result.ErrorCategory{"", result.Configuration, result.IO, result.Internal} {
				current := report.CloneReport(original)
				want := 15
				if category != "" {
					current.Errors = append(current.Errors, result.Error{Category: category, Code: "encountered", Message: "encountered failure"})
					switch category {
					case result.Usage:
						want = 11
					case result.Configuration:
						want = 12
					case result.IO:
						want = 13
					case result.Internal:
						want = 14
					}
					engine.FinalizeLintResult(&current)
				}
				before, _ := json.Marshal(current)
				reserve, err := report.EmergencyConfigReport(current, "vscode", map[string]int64{"response-bytes": 4096})
				if err != nil {
					t.Fatal(err)
				}
				if len(reserve.Data) != 0 || len(reserve.Stderr) > 4096 || report.LintExit(reserve.Out) != want || len(reserve.Out.Interruptions) != 2 {
					t.Fatal(reserve.Out, err)
				}
				operation, reporting := reserve.Out.Interruptions[0], reserve.Out.Interruptions[1]
				if operation.Phase != "configuration" || operation.Reason != reason || !reflect.DeepEqual(operation.Limit, current.Interruptions[0].Limit) || reporting.Phase != "reporting" || reporting.Reason != "deadline-exceeded" {
					t.Fatal(reserve.Out.Interruptions)
				}
				if !bytes.Contains(reserve.Stderr, []byte("interrupted configuration  "+reason)) || !bytes.Contains(reserve.Stderr, []byte("interrupted reporting  deadline-exceeded")) {
					t.Fatalf("lost independent cause: %s", reserve.Stderr)
				}
				if reason == "limit-exceeded" && !bytes.Contains(reserve.Stderr, []byte("limit directory-entries observed 2 allowed 1 observed entries")) {
					t.Fatal(string(reserve.Stderr))
				}
				counted, err := report.ReportSize(context.Background(), reserve.Out, "vscode")
				if err != nil || counted != int64(len(reserve.Stderr)) {
					t.Fatal(counted, err)
				}
				for _, deadline := range []int64{1, 1000, int64(time.Second)} {
					var stdout, stderr bytes.Buffer
					status := report.ReportResultStreams(current, "vscode", &stdout, &stderr, map[string]int64{"reporting-time": deadline, "response-bytes": 4096})
					if status != want || stdout.Len() != 0 || stderr.Len() > 4096 || !strings.Contains(stderr.String(), "interrupted configuration  "+reason) {
						t.Fatal(reason, category, deadline, status, stdout.String(), stderr.String())
					}
					if reason == "limit-exceeded" && !strings.Contains(stderr.String(), "limit directory-entries observed 2 allowed 1 observed entries") {
						t.Fatal(stderr.String())
					}
				}
				after, _ := json.Marshal(current)
				if !bytes.Equal(before, after) {
					t.Fatal("reserve mutated canonical operation interruption")
				}
			}
		})
	}
}
