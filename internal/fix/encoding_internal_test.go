package fix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
)

func TestOriginalEncodingRecoveryRejectsGeneratedAndJoinedFailures(t *testing.T) {
	r, err := engine.NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := engine.Settings{}
	off := false
	for _, meta := range engine.Catalog() {
		settings[meta.ID] = engine.RuleSetting{Enabled: &off}
	}
	selection, err := r.SelectRules(settings, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	m := meter{context.Background(), b}
	bad := parser.NewSource([]byte{'x', 0xff})
	target := Target{Path: "bad.md", Source: bad, Plan: plan}
	state, cause := analyze(context.Background(), target, bad, m)
	if cause == nil {
		t.Fatal("generic analysis must stay fatal")
	}
	local, err := originalEncodingFailure(state, cause, m)
	if err != nil || !local {
		t.Fatal(local, err, cause)
	}
	for _, tc := range []struct {
		name  string
		state validationState
		cause error
	}{
		{"joined-cancellation", state, errors.Join(cause, context.Canceled)},
		{"wrong-path", state, &AnalysisFailure{Path: "foreign.md", Code: "invalid-utf8", Category: "io"}},
		{"generated-candidate", func() validationState {
			s := state
			s.target.Source = parser.NewSource([]byte("valid original"))
			return s
		}(), cause},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local, err := originalEncodingFailure(tc.state, tc.cause, m)
			if err != nil || local {
				t.Fatal("non-original/nonlocal cause recovered", local, err)
			}
		})
	}
}

func TestOriginalEncodingRecoveryPreservesLateCauses(t *testing.T) {
	for _, name := range []string{"sentinel-and-cancellation", "encoding-and-work"} {
		t.Run(name, func(t *testing.T) {
			sentinel := errors.New("actual trusted callback cause")
			registry, err := engine.NewRegistry([]engine.Metadata{{ID: "MD001", Name: "cause fixture", DefaultEnabled: true, Fix: engine.NoFix}}, []engine.Registration{{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { return engine.RuleOutcome{Err: sentinel} }}})
			if err != nil {
				t.Fatal(err)
			}
			selection, err := registry.SelectRules(nil, engine.ValidationProviders{})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := selection.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			source := parser.NewSource([]byte("x"))
			if name == "encoding-and-work" {
				source = parser.NewSource([]byte{'x', 0xff})
			}
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			m := meter{context.Background(), budget}
			target := Target{Path: "cause.md", Source: source, Plan: plan}
			state, cause := analyze(context.Background(), target, source, m)
			if cause == nil || state.retained == nil || len(state.retained.Errors) != 1 {
				t.Fatal("actual initial failure required", cause, state.retained)
			}
			sourceBefore, analysisBefore := source.Bytes(), bytes.Clone(state.analysis)
			retainedBefore, err := json.Marshal(state.retained)
			if err != nil {
				t.Fatal(err)
			}
			var original *AnalysisFailure
			if name == "sentinel-and-cancellation" {
				if !errors.Is(cause, sentinel) {
					t.Fatal(cause)
				}
				caller, cancel := context.WithCancel(context.Background())
				cancel()
				m.ctx = caller
			} else {
				if !errors.As(cause, &original) || original.Code != "invalid-utf8" {
					t.Fatal(cause)
				}
				var exhausted *parser.LimitExceeded
				if !errors.As(budget.ChargeWork(budget.Limits().WorkUnits), &exhausted) {
					t.Fatal("same original allowance must be exhausted")
				}
			}
			for range 2 {
				local, joined := originalEncodingFailure(state, cause, m)
				if local {
					t.Fatal("late stop cannot authorize local recovery")
				}
				if name == "sentinel-and-cancellation" {
					if !errors.Is(joined, sentinel) || !errors.Is(joined, context.Canceled) {
						t.Fatal("both existing cause and late cancellation required", joined)
					}
				} else {
					var actual *AnalysisFailure
					var stop *parser.LimitExceeded
					if !errors.As(joined, &actual) || actual != original || !errors.As(joined, &stop) {
						t.Fatal("both existing structured cause and sticky work stop required", joined)
					}
				}
				retainedAfter, err := json.Marshal(state.retained)
				if err != nil || state.source.Token() != source.Token() || !bytes.Equal(state.source.Bytes(), sourceBefore) || !bytes.Equal(state.analysis, analysisBefore) || !bytes.Equal(retainedAfter, retainedBefore) {
					t.Fatal("late stop changed original observations", err)
				}
			}
		})
	}
}
