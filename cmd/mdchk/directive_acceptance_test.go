package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

func directiveFixtureConfiguration(finding, failure bool) string {
	return strings.Replace(fixtureConfiguration(false, finding, failure), "directives: {enabled: false}\n", "", 1)
}

func TestCompiledDirectiveSuppressionAndDirectEngineParity(t *testing.T) {
	binary := buildConfigurationFixtureExecutable(t)
	for _, tc := range []struct {
		name, source                                    string
		finding, failure, globalOff, blocked, missingIO bool
		status                                          int
		active, suppressed, notes, coverage             int
	}{
		{name: "suppressed Unicode CRLF", source: "雪\r\n<!-- mdchk:disable MD001 -->π fixture violation\r\n", finding: true, status: 0, suppressed: 1},
		{name: "configured-off enabled full context", source: "<!-- mdchk:enable MD001 -->fixture violation\n", status: 10, active: 1},
		{name: "invalid non-disableable", source: "<!-- mdchk:disable MD001 --><!-- mdchk:disable MD999 -->", status: 10, active: 1},
		{name: "globally inactive malformed", source: "<!-- mdchk:wat MD999 -->", globalOff: true, status: 0, notes: 1},
		{name: "known missing at EOF", source: "<!-- mdchk:enable MD013 -->", status: 15, coverage: 1},
		{name: "protected configured-on", source: "<!-- mdchk:disable MD001 -->fixture violation", finding: true, blocked: true, status: 10, active: 1, notes: 1},
		{name: "IO precedence retains suppression", source: "<!-- mdchk:disable MD001 -->fixture violation", finding: true, missingIO: true, status: 13, suppressed: 1, coverage: 1},
		{name: "internal precedence retains suppression and IO", source: "<!-- mdchk:disable MD001 -->fixture violation\nfixture failure\n", finding: true, failure: true, missingIO: true, status: 14, suppressed: 1, coverage: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			yaml := directiveFixtureConfiguration(tc.finding, tc.failure)
			if tc.globalOff {
				yaml = strings.Replace(yaml, "rules:\n", "directives: {enabled: false}\nrules:\n", 1)
			}
			if tc.blocked {
				yaml = strings.Replace(yaml, "rules:\n", "rules:\n  MD001: {allow_directives: false}\n", 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", yaml)
			putConfigurationFile(t, root, "雪.md", tc.source)
			args := []string{"lint", "--format=json", "雪.md"}
			if tc.missingIO {
				args = append(args, "missing.md")
			}
			got := runConfiguration(t, binary, root, root, args...)
			out := got.Result
			if got.Status != tc.status || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.suppressed || len(out.Notes) != tc.notes || len(out.Coverage) != tc.coverage {
				t.Fatalf("status%d %+v", got.Status, out)
			}
			for _, raw := range out.SuppressedDiagnostics {
				var s result.SuppressedDiagnostic
				if err := json.Unmarshal(raw, &s); err != nil {
					t.Fatal(err)
				}
				start := strings.Index(tc.source, "fixture violation")
				if s.Diagnostic.ByteRange.Start != start || s.Diagnostic.Rule != "MD001" || s.SuppressedBy.Kind != "directive" || s.SuppressedBy.DirectiveID != "" {
					t.Fatal(s)
				}
			}
			textArgs := append([]string{}, args...)
			textArgs[1] = "--format=text"
			text := runConfiguration(t, binary, root, root, textArgs...)
			if text.Status != tc.status || strings.Contains(text.Stdout, root) {
				t.Fatal(text)
			}
			if tc.suppressed > 0 && !strings.Contains(text.Stdout, "suppressed MD001 (directive): fixture violation") {
				t.Fatal(text.Stdout)
			}
			if tc.notes > 0 && !strings.Contains(text.Stdout, "note directive") {
				t.Fatal(text.Stdout)
			}
			if !tc.missingIO {
				direct := directiveDirectResult(t, yaml, tc.source)
				for _, pair := range [][2]any{{out.Diagnostics, direct.Diagnostics}, {out.SuppressedDiagnostics, direct.SuppressedDiagnostics}, {out.Notes, direct.Notes}, {out.Coverage, direct.Coverage}, {out.Files, direct.Files}} {
					if !reflect.DeepEqual(pair[0], pair[1]) {
						t.Fatalf("compiled/direct parity\ncompiled%+v\ndirect%+v", pair[0], pair[1])
					}
				}
				if out.Status != direct.Status || out.Complete != direct.Complete {
					t.Fatal(out.Status, direct.Status)
				}
			}
		})
	}
}

func directiveDirectResult(t *testing.T, yaml, source string) result.Result {
	t.Helper()
	// These explicit synthetic callbacks match the separately compiled overlay's
	// deterministic probes. They never stand in for actual MD001/MD003 behavior.
	r, err := engine.NewCatalogRegistry([]engine.Registration{
		{ID: "MD001", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			if e := c.Checkpoint(ctx, 1); e != nil {
				return engine.RuleOutcome{Err: e}
			}
			marker := []byte("fixture violation")
			start := bytes.Index(c.Source().Bytes(), marker)
			if start < 0 {
				return engine.RuleOutcome{}
			}
			if c.Document() == nil {
				t.Fatal("direct callback lost original syntax")
			}
			s, e := c.Source().Span(start, start+len(marker))
			if e != nil {
				return engine.RuleOutcome{Err: e}
			}
			return engine.RuleOutcome{Findings: []engine.Finding{{Message: "fixture violation", Span: s}}}
		}},
		{ID: "MD003", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			if e := c.Checkpoint(ctx, 1); e != nil {
				return engine.RuleOutcome{Err: e}
			}
			if bytes.Contains(c.Source().Bytes(), []byte("fixture failure")) {
				return engine.RuleOutcome{Err: errors.New("fixture callback failure")}
			}
			return engine.RuleOutcome{}
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	d, err := config.DecodeYAML(".mdchk.yaml", []byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	v, err := config.ValidateDefinition(d, r, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	view, err := v.Admit(config.Capabilities{CommonMark: true, DirectiveProcessing: true}, config.Permissions{})
	if err != nil {
		t.Fatal(err)
	}
	p := view.ExecutionPlan()
	in, err := p.NewInput("雪.md", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestProductionDirectiveCapabilityAndTruthfulCoverage(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	yaml := strings.Replace(cleanConfiguration(), "directives: {enabled: false}\n", "", 1)
	putConfigurationFile(t, root, ".mdchk.yaml", yaml)
	for _, tc := range []struct {
		source                       string
		status, violations, coverage int
	}{
		{"plain", 0, 0, 0},
		{"<!-- mdchk:wat MD999 -->", 10, 1, 0},
		{"<!-- mdchk:enable MD009 -->", 15, 0, 1},
		{"<!-- mdchk:disable MD009 -->", 0, 0, 0},
		{"<!-- mdchk:disable MD074 MD094 -->", 0, 0, 0},
	} {
		t.Run(fmt.Sprint(tc.status, tc.source), func(t *testing.T) {
			putConfigurationFile(t, root, "doc.md", tc.source)
			got := runConfiguration(t, binary, root, root, "lint", "--format=json", "doc.md")
			if got.Status != tc.status || len(got.Result.Diagnostics) != tc.violations || len(got.Result.Coverage) != tc.coverage {
				t.Fatal(got.Status, got.Result)
			}
			if tc.coverage > 0 && (got.Result.Coverage[0].Rule != "MD009" || got.Result.Coverage[0].Reason != "rule-unavailable") {
				t.Fatal(got.Result.Coverage)
			}
		})
	}
}

func TestCompiledInheritedDirectivePolicyAndDocumentIsolation(t *testing.T) {
	binary := buildConfigurationFixtureExecutable(t)
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\nrules: {MD001: {allow_directives: false}}\n")
	leaf := filepath.Join(root, "leaf")
	putConfigurationFile(t, root, "leaf/.mdchk.yaml", strings.Replace(directiveFixtureConfiguration(true, false), "rules:\n", "rules:\n  MD001: {allow_directives: true}\n", 1))
	putConfigurationFile(t, root, "leaf/a.md", "雪\r\n<!-- mdchk:disable MD001 MD003 -->fixture violation\r\n")
	putConfigurationFile(t, root, "leaf/b.md", "fixture violation\n")
	first := runConfiguration(t, binary, leaf, root, "lint", "--format=json", "b.md", "a.md")
	second := runConfiguration(t, binary, leaf, root, "lint", "--format=json", "a.md", "b.md")
	if first.Status != 10 || first.Stdout != second.Stdout || len(first.Result.Diagnostics) != 2 || len(first.Result.Notes) != 1 || len(first.Result.SuppressedDiagnostics) != 0 {
		t.Fatal(first, second)
	}
	var note result.Note
	if err := json.Unmarshal(first.Result.Notes[0], &note); err != nil {
		t.Fatal(err)
	}
	if note.Path != "a.md" || note.Rule != "MD001" || !strings.Contains(note.Message, "rules.MD001.allow_directives") || strings.Contains(note.Message, root) {
		t.Fatal(note)
	}
	// A second process with an independent per-file disable must not inherit any
	// state or observations from the previous request.
	putConfigurationFile(t, root, "leaf/.mdchk.yaml", strings.Replace(directiveFixtureConfiguration(true, false), "rules:\n", "rules:\n  MD001: {allow_directives: true}\n", 1))
	putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\n")
	third := runConfiguration(t, binary, leaf, root, "lint", "--format=json", "a.md", "b.md")
	if third.Status != 10 || len(third.Result.Diagnostics) != 1 || third.Result.Diagnostics[0].Path != "b.md" || len(third.Result.SuppressedDiagnostics) != 1 || len(third.Result.Notes) != 0 {
		t.Fatal(third)
	}
}
