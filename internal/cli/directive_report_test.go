package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

func TestBoundedReportsWithPopulatedDirectiveObservations(t *testing.T) {
	r, err := engine.NewRegistry([]engine.Metadata{
		{ID: "MD001", Name: "synthetic finding", DefaultEnabled: true, Fix: engine.NoFix},
		{ID: "MD003", Name: "synthetic policy", DefaultEnabled: true, Fix: engine.NoFix},
	}, []engine.Registration{
		{ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
			var findings []engine.Finding
			for i := range 50 {
				s, _ := c.Source().Span(c.Source().Len(), c.Source().Len())
				findings = append(findings, engine.Finding{Message: fmt.Sprintf("hidden %d %s", i, strings.Repeat("detail ", 50)), Span: s})
			}
			return engine.RuleOutcome{Findings: findings}
		}},
		{ID: "MD003", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { return engine.RuleOutcome{} }},
	})
	if err != nil {
		t.Fatal(err)
	}
	protected := false
	in, err := engine.NewInput("logical/雪.md", []byte("<!-- mdchk:disable MD001 -->"+strings.Repeat("<!-- mdchk:disable MD003 -->", 50)), engine.Settings{"MD003": {AllowDirectives: &protected}}, engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	original, err := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(original.Notes) != 50 || len(original.SuppressedDiagnostics) != 50 || !original.Complete {
		t.Fatal(original)
	}
	before, _ := json.Marshal(original)
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			encoded, err := buildReport(context.Background(), original, format, map[string]int64{"response-bytes": 5000})
			if err != nil {
				t.Fatal(err)
			}
			if len(encoded.data) > 5000 || encoded.out.Complete || encoded.out.Status != "incomplete" {
				t.Fatal(encoded.out)
			}
			omitted := map[string]int{}
			for _, o := range encoded.out.Omissions {
				if o.Count != nil {
					omitted[o.Section] += *o.Count
				}
			}
			if omitted["/notes"]+len(encoded.out.Notes) != 50 || omitted["/suppressed_diagnostics"]+len(encoded.out.SuppressedDiagnostics) != 50 || omitted["/notes"] == 0 || omitted["/suppressed_diagnostics"] == 0 {
				t.Fatal(omitted)
			}
			if format == "json" {
				validateEmitted(t, t.TempDir(), encoded.data)
			}
			if strings.Contains(string(encoded.data), "AnalysisRecords") || strings.Contains(string(encoded.data), "directive_id") {
				t.Fatal("private/dangling analysis leaked")
			}
			a := encoded.out.AnalysisRecords()[0].(engine.DirectiveAnalysis)
			if len(a.Suppressed()) != 50 || len(a.State().Notes()) != 50 {
				t.Fatal("wire omission destroyed analysis")
			}
		})
	}
	after, _ := json.Marshal(original)
	if string(before) != string(after) {
		t.Fatal("report mutated caller's result")
	}
}
