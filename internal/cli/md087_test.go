package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/report"
)

func TestMD087BoundedOutputKeepsPrivateAudit(t *testing.T) {
	r, err := engine.NewCatalogRegistry([]engine.Registration{{ID: "MD087", Check: engine.CheckUnusedSuppressions}, {ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { return engine.RuleOutcome{} }}})
	if err != nil {
		t.Fatal(err)
	}
	s := engine.Settings{}
	off, on := false, true
	for _, m := range engine.Catalog() {
		s[m.ID] = engine.RuleSetting{Enabled: &off}
	}
	s["MD087"] = engine.RuleSetting{Enabled: &on}
	s["MD001"] = engine.RuleSetting{Enabled: &on}
	const owners = 300
	in, err := engine.NewInput("bounded.md", []byte(strings.Repeat("<!-- mdchk:disable MD001 -->\n", owners)), s, engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil || !out.Complete || len(out.Diagnostics) != owners {
		t.Fatal(out, err)
	}
	original, _ := json.Marshal(out)
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			encoded, err := report.BuildReport(context.Background(), out, format, map[string]int64{"response-bytes": 10000})
			if err != nil {
				t.Fatal(err)
			}
			if len(encoded.Data) > 10000 || encoded.Out.Complete {
				t.Fatal(encoded.Out)
			}
			omitted := 0
			for _, o := range encoded.Out.Omissions {
				if o.Section == "/diagnostics" && o.Count != nil {
					omitted += *o.Count
				}
			}
			if omitted == 0 || omitted+len(encoded.Out.Diagnostics) != owners {
				t.Fatal(encoded.Out)
			}
			if format == "json" {
				validateEmitted(t, t.TempDir(), encoded.Data)
			}
			if strings.Contains(string(encoded.Data), "SuppressionDecisions") || strings.Contains(string(encoded.Data), "suppression-unused") {
				t.Fatal("private audit records leaked into lint")
			}
		})
	}
	after, _ := json.Marshal(out)
	if string(original) != string(after) {
		t.Fatal("bounded reporting mutated full analysis")
	}
}
