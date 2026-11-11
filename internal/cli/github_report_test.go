package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/configscan"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/files"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/report"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/testutil/testenv"
)

// Independent grammar fixture following actions/runner ActionCommand.cs at
// b6d10b59c178914d331bcf03823133d5f9e366bb (V2 first, legacy anywhere).
// This is parser-compatible evidence, not a hosted runner execution.
type workflowCommand struct {
	name, data string
	properties map[string]string
}

func parseWorkflow(line string) *workflowCommand {
	registered := func(s string) bool { return s == "error" || s == "stop-commands" || s == "add-mask" || s == "group" }
	decode := func(s string, property bool) string {
		s = strings.ReplaceAll(s, "%0D", "\r")
		s = strings.ReplaceAll(s, "%0A", "\n")
		if property {
			s = strings.ReplaceAll(s, "%3A", ":")
			s = strings.ReplaceAll(s, "%2C", ",")
		}
		return strings.ReplaceAll(s, "%25", "%")
	}
	trimmed := strings.TrimLeft(line, " \t\r\n")
	if strings.HasPrefix(trimmed, "::") {
		info, data, ok := strings.Cut(trimmed[2:], "::")
		name, props, _ := strings.Cut(info, " ")
		if ok && registered(name) {
			c := &workflowCommand{name: name, data: decode(data, false), properties: map[string]string{}}
			for _, p := range strings.Split(props, ",") {
				k, v, ok := strings.Cut(p, "=")
				if ok {
					c.properties[k] = decode(v, true)
				}
			}
			return c
		}
	}
	if index := strings.Index(line, "##["); index >= 0 {
		info, data, ok := strings.Cut(line[index+3:], "]")
		name, _, _ := strings.Cut(info, " ")
		if ok && registered(name) {
			return &workflowCommand{name: name, data: data}
		}
	}
	return nil
}
func TestGitHubActualProductionLintFixAndConfiguration(t *testing.T) {
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
	if got := report.ReportResultStreamsContext(report.WithGitHubPaths(context.Background(), githubFixturePaths(root, runtime.GOOS)), *inv.result, "github", &stdout, &stderr, nil); got != 10 {
		t.Fatal(got)
	}
	// Tab, astral, combining and CJK each count one scalar, plus EOF column.
	want := "::error file=doc.md,line=1,title=MD047::"
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
	if got := report.ReportResultStreamsContext(report.WithGitHubPaths(context.Background(), githubFixturePaths(root, runtime.GOOS)), fixed, "github", &stdout, &stderr, nil); got != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
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
	if got := report.ReportResultStreamsContext(report.WithGitHubPaths(context.Background(), githubFixturePaths(root, runtime.GOOS)), manual, "github", &stdout, &stderr, nil); got != 10 || !strings.HasPrefix(stdout.String(), "::error file=protected.md,line=2,title=MD047::") || stderr.Len() != 0 {
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
	if got := report.ReportResultStreamsContext(report.WithGitHubPaths(context.Background(), githubFixturePaths(root, runtime.GOOS)), configuration, "github", &stdout, &stderr, nil); got != 12 || !strings.HasPrefix(stdout.String(), "::error file=.mdchk.yaml,line=4,") || !strings.Contains(stdout.String(), "title=config-definition::") || stderr.Len() != 0 {
		t.Fatal(got, stdout.String(), stderr.String())
	}
	c := parseWorkflow(strings.TrimSuffix(stdout.String(), "\n"))
	if c == nil || c.properties["file"] != ".mdchk.yaml" || c.properties["line"] != "4" || c.properties["title"] != "config-definition" {
		t.Fatal(c)
	}
}

func TestGitHubActualConfigurationInterruptionReserve(t *testing.T) {
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
				reserve, err := report.EmergencyConfigReport(current, "github", map[string]int64{"response-bytes": 4096})
				if err != nil {
					t.Fatal(err)
				}
				if len(reserve.Stderr) != 0 || len(reserve.Data) > 4096 || report.LintExit(reserve.Out) != want || len(reserve.Out.Interruptions) != 2 {
					t.Fatal(reserve.Out, err)
				}
				operation, reporting := reserve.Out.Interruptions[0], reserve.Out.Interruptions[1]
				if operation.Phase != "configuration" || operation.Reason != reason || !reflect.DeepEqual(operation.Limit, current.Interruptions[0].Limit) || reporting.Phase != "reporting" || reporting.Reason != "deadline-exceeded" {
					t.Fatal(reserve.Out.Interruptions)
				}
				if !bytes.Contains(reserve.Data, []byte("title=interrupted::configuration "+reason)) || !bytes.Contains(reserve.Data, []byte("title=interrupted::reporting deadline-exceeded")) {
					t.Fatalf("lost independent cause: %s", reserve.Data)
				}
				if reason == "limit-exceeded" && !bytes.Contains(reserve.Data, []byte("limit directory-entries observed 2 allowed 1 observed entries")) {
					t.Fatal(string(reserve.Data))
				}
				counted, err := report.ReportSize(context.Background(), reserve.Out, "github")
				if err != nil || counted != int64(len(reserve.Data)) {
					t.Fatal(counted, err)
				}
				for _, deadline := range []int64{1, 1000, int64(time.Second)} {
					var stdout, stderr bytes.Buffer
					status := report.ReportResultStreams(current, "github", &stdout, &stderr, map[string]int64{"reporting-time": deadline, "response-bytes": 4096})
					if status != want || stderr.Len() != 0 || stdout.Len() > 4096 || !strings.Contains(stdout.String(), "title=interrupted::configuration "+reason) {
						t.Fatal(reason, category, deadline, status, stdout.String(), stderr.String())
					}
					if reason == "limit-exceeded" && !strings.Contains(stdout.String(), "limit directory-entries observed 2 allowed 1 observed entries") {
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

// Actual producers use the fixture host; synthetic path tests remain explicit.
func githubFixturePaths(root, goos string) report.GithubPaths {
	flavor := "unix"
	if goos == "windows" {
		flavor = "windows"
	}
	return report.GithubPaths{Root: root, CWD: root, Flavor: flavor}
}

func TestGitHubActualRemovedTargetsRetainUnlocatedIdentity(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"first.md", "second.md"}
	for _, name := range names {
		put(t, filepath.Join(root, name), "hello")
	}
	selected, err := files.Select(files.Options{WorkingDirectory: root, Paths: names})
	if err != nil || len(selected.Targets) != 2 {
		t.Fatal(selected, err)
	}
	for _, name := range names {
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	acquired, err := files.Acquire(context.Background(), selected, files.AcquireOptions{})
	if err != nil || len(acquired.Errors) != 2 {
		t.Fatal(acquired, err)
	}
	inv := &lintInvocation{workingDirectory: root}
	r := result.NewLint("cli")
	inv.mergeAcquisition(&r, selected, acquired)
	engine.FinalizeLintResult(&r)
	ctx := report.WithGitHubPaths(context.Background(), githubFixturePaths(root, runtime.GOOS))
	frozen, _ := json.Marshal(r)
	var stdout, stderr bytes.Buffer
	if status := report.ReportResultStreamsContext(ctx, r, "github", &stdout, &stderr, nil); status != 13 || stderr.Len() != 0 {
		t.Fatal(status, stdout.String(), stderr.String())
	}
	seenErrors, seenCoverage := map[string]int{}, map[string]int{}
	for _, line := range strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n") {
		c := parseWorkflow(line)
		if c == nil || c.name != "error" {
			t.Fatal(line, c)
		}
		if c.properties["file"] != "" || c.properties["line"] != "" || c.properties["col"] != "" {
			t.Fatal("invented location", c)
		}
		if c.properties["title"] == "incomplete" {
			continue
		}
		matched := false
		for _, name := range names {
			if strings.HasPrefix(c.data, name+": ") {
				matched = true
				if c.properties["title"] == "acquisition-failed" {
					seenErrors[name]++
				} else if c.properties["title"] == "coverage" {
					seenCoverage[name]++
				} else {
					t.Fatal(c)
				}
			}
		}
		if !matched {
			t.Fatal("target identity lost", c)
		}
	}
	for _, name := range names {
		if seenErrors[name] != 1 || seenCoverage[name] < 1 {
			t.Fatal(name, seenErrors, seenCoverage)
		}
	}
	after, _ := json.Marshal(r)
	if !bytes.Equal(frozen, after) {
		t.Fatal("canonical acquisition changed")
	}
}
