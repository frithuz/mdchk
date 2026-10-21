package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

func TestReportFindingsAndCoverageSurviveOperationalOverrides(t *testing.T) {
	root := validProject(t)
	put(t, filepath.Join(root, "doc.md"), "# heading\n")
	for _, tc := range []struct {
		name              string
		missing, internal bool
		exit              int
		status            string
	}{
		{"incomplete", false, false, 15, "incomplete"},
		{"io", true, false, 13, "error"},
		{"internal-over-io", true, true, 14, "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := executionRegistry(t, map[string]engine.Check{
				"MD001": func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
					span, err := c.Source().Span(2, 9)
					if err != nil {
						t.Fatal(err)
					}
					return engine.RuleOutcome{Findings: []engine.Finding{{Span: span, Message: "retained finding"}}, IncompleteReason: "required context unavailable"}
				},
				"MD003": func(context.Context, engine.RuleContext) engine.RuleOutcome {
					if tc.internal {
						return engine.RuleOutcome{Err: errors.New("callback failed")}
					}
					return engine.RuleOutcome{}
				},
			}, false)
			for _, format := range []string{"text", "json"} {
				args := []string{"lint", "--format=" + format, "doc.md"}
				if tc.missing {
					args = append(args, "missing.md")
				}
				var stdout, stderr bytes.Buffer
				status := runContext(context.Background(), args, &stdout, &stderr, "test", func(o lintOptions) (*lintInvocation, error) {
					return prepareLintWith(o, registry, engine.ValidationProviders{}, config.Capabilities{CommonMark: true})
				}, func(ctx context.Context, inv *lintInvocation, _ io.Writer, w io.Writer) int {
					return acquireLintContext(ctx, inv, w)
				})
				if status != tc.exit || stderr.Len() != 0 {
					t.Fatalf("%s status=%d stderr=%q", format, status, &stderr)
				}
				if format == "json" {
					out := validateEmitted(t, root, stdout.Bytes())
					wantCoverage := 1
					if tc.missing {
						wantCoverage++
					}
					if tc.internal {
						wantCoverage++
					}
					if out.Status != tc.status || out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Message != "retained finding" || len(out.Coverage) != wantCoverage {
						t.Fatalf("lost partial observations: %+v", out)
					}
					c := out.Coverage[0]
					if c.Rule != "MD001" || c.Path != "doc.md" || c.Reason != result.ContextUnavailable || c.Message != "required context unavailable" {
						t.Fatalf("coverage: %+v", c)
					}
					var hasIO, hasInternal bool
					for _, e := range out.Errors {
						hasIO = hasIO || e.Category == result.IO
						hasInternal = hasInternal || e.Category == result.Internal
					}
					if hasIO != tc.missing || hasInternal != tc.internal {
						t.Fatalf("errors: %+v", out.Errors)
					}
				} else {
					text := stdout.String()
					for _, want := range []string{"doc.md:1:3 MD001: retained finding", "coverage doc.md MD001 context-unavailable: required context unavailable", tc.status + " (complete: false)"} {
						if !strings.Contains(text, want) {
							t.Fatalf("missing %q: %s", want, text)
						}
					}
					if tc.missing && !strings.Contains(text, "(io):") {
						t.Fatalf("missing IO error: %s", text)
					}
					if tc.internal && !strings.Contains(text, "(internal):") {
						t.Fatalf("missing internal error: %s", text)
					}
				}
			}
		})
	}
}

func TestReportGlobalAndLocatedCoverage(t *testing.T) {
	root := t.TempDir()
	raw := "# 😀é\r\n\t雪 target\n"
	path, message := "雪\r\nforged.md", "context\r\nforged"
	snapshot := result.Snapshot{View: "original", SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(raw))), SizeBytes: len(raw)}
	location := result.Range{Start: result.Position{Line: 2, Column: 4}, End: result.Position{Line: 2, Column: 10}}
	global := result.Coverage{Rule: "MD003", Reason: result.RuleUnavailable, Message: "implementation unavailable"}
	located := result.Coverage{Path: path, Rule: "MD001", Reason: result.ContextUnavailable, Message: message, Range: &location, Snapshot: &snapshot}
	var canonical []byte
	for _, reversed := range []bool{false, true} {
		r := result.NewLint("project")
		r.CheckedFiles = append(r.CheckedFiles, path)
		r.Files = append(r.Files, result.File{Path: path, Analysis: "partial", InputSnapshot: &snapshot, WriteStatus: "not-requested", ReturnStatus: "not-requested", ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}})
		r.Coverage = append(r.Coverage, located, global)
		if reversed {
			r.Coverage[0], r.Coverage[1] = r.Coverage[1], r.Coverage[0]
		}
		for _, format := range []string{"json", "text"} {
			var stdout bytes.Buffer
			if status := reportResult(r, format, &stdout, nil); status != 15 {
				t.Fatalf("%s exit=%d", format, status)
			}
			if format == "json" {
				out := validateEmitted(t, root, stdout.Bytes())
				if out.Status != "incomplete" || out.Complete || len(out.Coverage) != 2 {
					t.Fatalf("result: %+v", out)
				}
				g, c := out.Coverage[0], out.Coverage[1]
				if g.Path != "" || g.Range != nil || g.Snapshot != nil || c.Path != path || c.Message != message || !reflect.DeepEqual(c.Range, &location) || !reflect.DeepEqual(c.Snapshot, &snapshot) {
					t.Fatalf("coverage: %+v", out.Coverage)
				}
				if !reversed {
					canonical = append([]byte{}, stdout.Bytes()...)
				} else if !bytes.Equal(canonical, stdout.Bytes()) {
					t.Fatal("coverage input order changed emitted JSON")
				}
			} else {
				lines := strings.Split(stdout.String(), "\n")
				if lines[0] != "coverage  MD003 rule-unavailable: implementation unavailable" || lines[1] != `coverage 雪\r\nforged.md:2:4 MD001 context-unavailable: context\r\nforged` {
					t.Fatalf("coverage locations/escaping/order: %q", &stdout)
				}
				if strings.Contains(stdout.String(), "\r") || strings.Contains(stdout.String(), "\nforged") {
					t.Fatalf("forged text record: %q", &stdout)
				}

			}
		}
	}
}
