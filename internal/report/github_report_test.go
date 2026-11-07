package report

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
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
func githubTestContext() context.Context {
	return WithGitHubPaths(context.Background(), GithubPaths{Root: "/repo", CWD: "/repo/docs", Flavor: "unix"})
}

func TestGitHubProtocolAndBothStreamInjection(t *testing.T) {
	if parseWorkflow("prefix ##[error]legacy") == nil || parseWorkflow(" \t::group::modern") == nil {
		t.Fatal("ineffective runner fixture")
	}
	for _, attack := range []string{"::error file=forged,line=1::fake", "::stop-commands::token", "::add-mask::secret", "::group::title", "##[error]fake", "prefix ##[group]nested", "##[stop-commands]token", "##[add-mask]secret", "# Markdown\n##[error]new"} {
		r := result.NewFix(false)
		message := "%,:=%0A\r\n雪😀 " + attack
		r.Diagnostics = []result.Diagnostic{{Path: "a,:=%0A\r\n雪.md", Rule: "MD047,:=", Message: message, Range: result.Range{Start: result.Position{Line: 2, Column: 8}}}}
		r.Files = []result.File{{Path: attack, WriteStatus: "unknown", Leftovers: []string{attack}}}
		r.Errors = []result.Error{{Category: result.IO, Code: "failed", Message: attack}}
		engine.FinalizeLintResult(&r)
		frozen, _ := json.Marshal(r)
		var stdout, stderr bytes.Buffer
		if status := ReportResultStreamsContext(githubTestContext(), r, "github", &stdout, &stderr, nil); status != 13 {
			t.Fatal(status)
		}
		lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
		first := parseWorkflow(lines[0])
		if first == nil || first.name != "error" || first.data != message || !reflect.DeepEqual(first.properties, map[string]string{"file": "docs/a,:=%0A\r\n雪.md", "line": "2", "title": "MD047,:="}) {
			t.Fatalf("%+v %q", first, stdout.String())
		}
		for _, line := range lines {
			if command := parseWorkflow(line); command == nil || command.name != "error" {
				t.Fatal("unintended stdout command", line, command)
			}
		}
		for _, line := range strings.Split(stderr.String(), "\n") {
			if command := parseWorkflow(line); command != nil {
				t.Fatal("stderr command", line, command)
			}
		}
		after, _ := json.Marshal(r)
		if !bytes.Equal(frozen, after) {
			t.Fatal("mutated result")
		}
	}
}

func TestGitHubTrustedPathMapping(t *testing.T) {
	tests := []struct{ flavor, root, cwd, name, want string }{
		{"unix", "/repo", "/repo/docs", "a.md", "docs/a.md"},
		{"unix", "/repo", "/repo/docs", "../a.md", "a.md"},
		{"unix", "/repo", "/repo/docs", "/repo/雪 a:b.md", "雪 a:b.md"},
		{"unix", "/repo", "/repo/docs", `a\b.md`, `docs/a\b.md`},
		{"unix", "/repo", "/repo/docs", "../../outside.md", ""},
		{"unix", "/repo", "/repo", "/repository/a.md", ""},
		{"unix", "", "/repo", "a.md", ""},
		{"unix", "/repo", "/repo", `C:\repo\a.md`, ""},
		{"unix", "/repo", "/repo", `\\host\share\a.md`, ""},
		{"windows", `C:\repo`, `c:\repo\docs`, `..\a.md`, "a.md"},
		{"windows", `C:\repo`, `C:\repo\docs`, `c:\REPO\雪 a.md`, "雪 a.md"},
		{"windows", `C:\repo`, `C:\repo`, `D:\repo\a.md`, ""},
		{"windows", `C:\repo`, `C:\repo`, `C:a.md`, ""},
		{"windows", `C:\repo`, `C:\repo`, `\repo\a.md`, ""},
		{"windows", `C:\repo`, `C:\repo`, `..\a.md`, ""},
		{"windows", `\\host\share\repo`, `\\HOST\SHARE\repo\docs`, `..\a.md`, "a.md"},
		{"windows", `\\host\share\repo`, `\\host\share\repo`, `\\other\share\repo\a.md`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := WithGitHubPaths(context.Background(), GithubPaths{Root: tt.root, CWD: tt.cwd, Flavor: tt.flavor})
			got, err := githubPath(ctx, tt.name)
			if err != nil || got != tt.want {
				t.Fatalf("got %q %v want %q", got, err, tt.want)
			}
			r := result.NewLint("cli")
			r.Diagnostics = []result.Diagnostic{{Path: tt.name, Rule: "MD047", Message: "bad", Range: result.Range{Start: result.Position{Line: 3, Column: 8}}}}
			encoded, err := EncodeGitHub(ctx, r, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			c := parseWorkflow(strings.TrimSuffix(string(encoded.Data), "\n"))
			if tt.want == "" {
				if c.properties["file"] != "" || c.properties["line"] != "" || !strings.Contains(c.data, "Location unavailable") {
					t.Fatal(c)
				}
			} else if c.properties["file"] != tt.want || c.properties["line"] != "3" {
				t.Fatal(c)
			}
		})
	}
	ctx := &reportCheckpointContext{Context: githubTestContext(), stop: 2}
	if _, err := githubPath(ctx, strings.Repeat("x", 1<<20)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestGitHubBoundsBidiCancellationDelivery(t *testing.T) {
	r := result.NewFix(false)
	controls := "\u061c\u200e\u200f\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069\x00\x1b\x7f\u0085\u2028\u2029"
	visible := `\u061c\u200e\u200f\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069\x00\x1b\x7f\u0085\u2028\u2029`
	r.Diagnostics = []result.Diagnostic{{Path: "a.md", Rule: "MD047", Message: controls + "%\r\n,:", Range: result.Range{Start: result.Position{Line: 2}}}}
	r.Errors = []result.Error{{Category: result.IO, Code: "failed", Message: "failure"}}
	r.Files = []result.File{{Path: "a.md", WriteStatus: "unknown", Leftovers: []string{controls + "##[error]"}}}
	engine.FinalizeLintResult(&r)
	full, err := EncodeGitHub(githubTestContext(), r, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(full.Data, []byte(visible+"%25%0D%0A,:")) || !bytes.Contains(full.Stderr, []byte(visible+`##\x5berror]`)) {
		t.Fatal(string(full.Data), string(full.Stderr))
	}
	size := int64(len(full.Data) + len(full.Stderr))
	count, err := ReportSize(githubTestContext(), r, "github")
	if err != nil || count != size {
		t.Fatal(count, size, err)
	}
	if _, err := EncodeGitHub(githubTestContext(), r, size); err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeGitHub(githubTestContext(), r, size-1); !errors.Is(err, ErrReportBudget) {
		t.Fatal(err)
	}
	for _, bound := range []int64{-1, 0} {
		if _, err := EncodeGitHub(githubTestContext(), r, bound); !errors.Is(err, ErrReportBudget) {
			t.Fatal(err)
		}
	}
	measure := &reportCheckpointContext{Context: githubTestContext(), stop: 1 << 30}
	if _, err := EncodeGitHub(measure, r, 1<<20); err != nil {
		t.Fatal(err)
	}
	last := &reportCheckpointContext{Context: githubTestContext(), stop: measure.checks - 1}
	if _, err := EncodeGitHub(last, r, 1<<20); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	large := r
	large.Diagnostics = append([]result.Diagnostic(nil), r.Diagnostics...)
	large.Diagnostics[0].Message = strings.Repeat("%", 2<<20)
	cancel := &reportCheckpointContext{Context: githubTestContext(), stop: 10}
	if _, err := BuildReport(cancel, large, "github", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err := DeadlineReport(last, r, "github", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	for _, category := range []result.ErrorCategory{result.IO, result.Internal, result.Configuration} {
		for _, short := range []bool{false, true} {
			for _, fail := range []int{0, 1, 2} {
				current := r
				current.Errors = []result.Error{{Category: category, Message: "failed"}}
				engine.FinalizeLintResult(&current)
				a, b := &vscodeFailWriter{short: short}, &vscodeFailWriter{short: short}
				var stdout, stderr io.Writer = a, b
				if fail == 0 {
					stdout = io.Discard
				}
				if fail == 1 {
					stderr = io.Discard
				}
				status := ReportResultStreamsContext(githubTestContext(), current, "github", stdout, stderr, nil)
				want := 13
				if category == result.Internal {
					want = 14
				}
				if status != want || fail != 0 && a.calls == 0 || fail != 1 && b.calls == 0 {
					t.Fatal(status, want, a.calls, b.calls)
				}
			}
		}
	}
	for _, timeout := range []int64{1, 1000} {
		var stdout, stderr bytes.Buffer
		status := ReportResultStreamsContext(githubTestContext(), r, "github", &stdout, &stderr, map[string]int64{"reporting-time": timeout})
		if status != 13 || !strings.Contains(stderr.String(), "outcome a.md unknown") {
			t.Fatal(status, stdout.String(), stderr.String())
		}
	}
}

func TestGitHubTinyCleanAndMaximalWholePrefix(t *testing.T) {
	for _, operation := range []string{"lint", "fix", "config validate"} {
		for _, timeout := range []int64{1, 1000} {
			r := result.NewLint("cli")
			if operation == "fix" {
				r = result.NewFix(true)
			}
			if operation == "config validate" {
				r = result.NewConfigValidation()
			}
			var stdout, stderr bytes.Buffer
			status := ReportResultStreams(r, "github", &stdout, &stderr, map[string]int64{"reporting-time": timeout})
			if stderr.Len() != 0 {
				t.Fatal(stderr.String())
			}
			switch status {
			case 0:
				if stdout.Len() != 0 {
					t.Fatal(stdout.String())
				}
			case 15:
				if !strings.Contains(stdout.String(), "reporting") || !strings.Contains(stdout.String(), "incomplete") {
					t.Fatal(stdout.String())
				}
			default:
				t.Fatal(status)
			}
		}
	}
	r := result.NewLint("cli")
	for i := 0; i < 20; i++ {
		r.Diagnostics = append(r.Diagnostics, result.Diagnostic{Path: "a.md", Rule: "MD047", Message: strings.Repeat("finding%,:", 20), Range: result.Range{Start: result.Position{Line: i + 1, Column: 2}}})
	}
	engine.FinalizeLintResult(&r)
	bounded, err := BuildReport(githubTestContext(), r, "github", map[string]int64{"response-bytes": 1200})
	if err != nil {
		t.Fatal(err)
	}
	retained := len(bounded.Out.Diagnostics)
	if retained == 0 || retained >= 20 || len(bounded.Data)+len(bounded.Stderr) > 1200 || len(bounded.Out.Omissions) != 1 || *bounded.Out.Omissions[0].Count != 20-retained {
		t.Fatal(retained, bounded.Out.Omissions, string(bounded.Data))
	}
	for i, line := range strings.Split(strings.TrimSuffix(string(bounded.Data), "\n"), "\n")[:retained] {
		c := parseWorkflow(line)
		if c == nil || c.properties["line"] != strconv.Itoa(i+1) {
			t.Fatal(c)
		}
	}
	next := bounded.Out
	next.Diagnostics = r.Diagnostics[:retained+1]
	next.Omissions = append([]result.Omission(nil), next.Omissions...)
	count := 20 - retained - 1
	next.Omissions[0].Count = &count
	if _, err := EncodeGitHub(githubTestContext(), next, 1200); !errors.Is(err, ErrReportBudget) {
		t.Fatal("prefix not maximal", err)
	}
	again, err := BuildReport(githubTestContext(), r, "github", map[string]int64{"response-bytes": 1200})
	if err != nil || !bytes.Equal(again.Data, bounded.Data) {
		t.Fatal("unstable prefix", err)
	}
}

// Actual producers use the fixture host; synthetic path tests remain explicit.
func githubFixturePaths(root, goos string) GithubPaths {
	flavor := "unix"
	if goos == "windows" {
		flavor = "windows"
	}
	return GithubPaths{Root: root, CWD: root, Flavor: flavor}
}

func TestGitHubNativeFixtureFlavors(t *testing.T) {
	for _, tt := range []struct{ goos, root string }{
		{"windows", `C:\work\repo`}, {"windows", `\\host\share\repo`},
		{"darwin", "/private/work/repo"}, {"linux", "/work/repo"},
	} {
		r := result.NewLint("cli")
		r.Diagnostics = []result.Diagnostic{{Path: "doc.md", Rule: "MD047", Message: "missing newline", Range: result.Range{Start: result.Position{Line: 1, Column: 6}}}}
		ctx := WithGitHubPaths(context.Background(), githubFixturePaths(tt.root, tt.goos))
		encoded, err := EncodeGitHub(ctx, r, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		c := parseWorkflow(strings.TrimSuffix(string(encoded.Data), "\n"))
		if c == nil || c.properties["file"] != "doc.md" || c.properties["line"] != "1" || c.properties["title"] != "MD047" || c.data != "missing newline" {
			t.Fatalf("platform %s root %s: %+v %s", tt.goos, tt.root, c, encoded.Data)
		}
	}
}

func TestGitHubUnlocatedPathProseCodecAndBounds(t *testing.T) {
	name := "name,:=%0A\r\n##[error]::stop-commands::雪.md"
	expectedPath := "docs/" + name
	r := result.NewLint("cli")
	r.Errors = []result.Error{{Path: name, Category: result.IO, Code: "acquisition-failed", Message: "open failed"}}
	r.Coverage = []result.Coverage{{Path: name, Reason: "not-started", Message: "validation unavailable"}}
	r.Interruptions = []result.Interruption{{Path: name, Phase: "acquisition", Reason: "cancelled", Message: "operation stopped"}}
	count := 2
	r.Omissions = []result.Omission{{Path: name, Section: "/coverage", Reason: "output-limit", Count: &count, Message: "whole records omitted"}}
	engine.FinalizeLintResult(&r)
	frozen, _ := json.Marshal(r)
	full, err := EncodeGitHub(githubTestContext(), r, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		"acquisition-failed": expectedPath + ": io open failed",
		"coverage":           expectedPath + ":  not-started validation unavailable",
		"interrupted":        expectedPath + ": acquisition cancelled operation stopped",
		"omitted":            expectedPath + ": 2 /coverage output-limit whole records omitted",
		"incomplete":         "Requested processing is incomplete.",
	}
	lines := strings.Split(strings.TrimSuffix(string(full.Data), "\n"), "\n")
	if len(lines) != len(expected) || len(full.Stderr) != 0 {
		t.Fatal(string(full.Data), string(full.Stderr))
	}
	for _, line := range lines {
		c := parseWorkflow(line)
		if c == nil || c.name != "error" {
			t.Fatal("unintended command", line, c)
		}
		if len(c.properties) != 1 || c.data != expected[c.properties["title"]] {
			t.Fatalf("unexpected fields/data: %+v", c)
		}
		delete(expected, c.properties["title"])
	}
	if len(expected) != 0 {
		t.Fatal(expected)
	}
	size := int64(len(full.Data) + len(full.Stderr))
	measured, err := ReportSize(githubTestContext(), r, "github")
	if err != nil || measured != size {
		t.Fatal(measured, size, err)
	}
	if _, err := EncodeGitHub(githubTestContext(), r, size); err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeGitHub(githubTestContext(), r, size-1); !errors.Is(err, ErrReportBudget) {
		t.Fatal(err)
	}
	after, _ := json.Marshal(r)
	if !bytes.Equal(frozen, after) {
		t.Fatal("mutated canonical records")
	}
}
