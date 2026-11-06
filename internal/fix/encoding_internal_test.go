package fix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"slices"
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

// encodingFailureState analyzes source to the recoverable original failure.
func encodingFailureState(t *testing.T, source []byte) (validationState, meter, error) {
	t.Helper()
	r, err := engine.NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := r.SelectRules(engine.Settings{}, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	m := meter{context.Background(), b}
	bad := parser.NewSource(source)
	state, cause := analyze(context.Background(), Target{Path: "bad.md", Source: bad, Plan: plan}, bad, m)
	if cause == nil {
		t.Fatal("generic analysis must stay fatal")
	}
	return state, m, cause
}

func TestOriginalEncodingRecoveryAcceptsManyAndCapped(t *testing.T) {
	for name, tc := range map[string]struct {
		source  []byte
		errors  int
		omitted int
	}{
		"many":   {[]byte("a\xffb\xe2\x82c\xff\r\nd\n"), 3, 0},
		"cap":    {bytes.Repeat([]byte{0xFF}, engine.MaxFileEncodingErrors), engine.MaxFileEncodingErrors, 0},
		"capped": {bytes.Repeat([]byte{0xFF}, engine.MaxFileEncodingErrors+5), engine.MaxFileEncodingErrors, 5},
	} {
		t.Run(name, func(t *testing.T) {
			state, m, cause := encodingFailureState(t, tc.source)
			if len(state.retained.Errors) != tc.errors || len(state.retained.Omissions) != min(tc.omitted, 1) {
				t.Fatal(len(state.retained.Errors), state.retained.Omissions)
			}
			if local, err := originalEncodingFailure(state, cause, m); err != nil || !local {
				t.Fatal(local, err)
			}
		})
	}
}

func TestOriginalEncodingRecoveryFailsClosed(t *testing.T) {
	source := append([]byte("a\xffb\xe2\x82c"), bytes.Repeat([]byte{0xFF}, engine.MaxFileEncodingErrors)...)
	state, m, cause := encodingFailureState(t, source)
	if local, err := originalEncodingFailure(state, cause, m); err != nil || !local {
		t.Fatal(local, err)
	}
	zero, wrongPath := 0, "other.md"
	for name, mutate := range map[string]func(*validationState, *result.Result){
		"another error code": func(_ *validationState, r *result.Result) { r.Errors[1].Code = "read-failed" },
		"another category":   func(_ *validationState, r *result.Result) { r.Errors[1].Category = result.Internal },
		"extra non-encoding error": func(_ *validationState, r *result.Result) {
			r.Errors = append(r.Errors, result.Error{Category: result.Internal, Code: "rule-failure", Scope: "operation", Path: "bad.md"})
		},
		"too many errors": func(_ *validationState, r *result.Result) { r.Errors = append(r.Errors, r.Errors[len(r.Errors)-1]) },
		"no errors":       func(_ *validationState, r *result.Result) { r.Errors = nil },
		"non-errors omission": func(_ *validationState, r *result.Result) {
			r.Omissions[0].Section = "/diagnostics"
		},
		"path-less errors omission": func(_ *validationState, r *result.Result) { r.Omissions[0].Path = "" },
		"zero-count omission":       func(_ *validationState, r *result.Result) { r.Omissions[0].Count = &zero },
		"count-less omission":       func(_ *validationState, r *result.Result) { r.Omissions[0].Count = nil },
		"another file's omission":   func(_ *validationState, r *result.Result) { r.Omissions[0].Path = wrongPath },
		"second omission": func(_ *validationState, r *result.Result) {
			r.Omissions = append(r.Omissions, r.Omissions[0])
		},
		"interruption": func(_ *validationState, r *result.Result) {
			r.Interruptions = []result.Interruption{{Reason: "limit-exceeded"}}
		},
		"diagnostic": func(_ *validationState, r *result.Result) { r.Diagnostics = []result.Diagnostic{{Path: "bad.md"}} },
		"errors out of order": func(_ *validationState, r *result.Result) {
			r.Errors[0], r.Errors[1] = r.Errors[1], r.Errors[0]
		},
		"missing byte range": func(_ *validationState, r *result.Result) { r.Errors[1].ByteRange = nil },
		"byte range past source": func(_ *validationState, r *result.Result) {
			r.Errors[1].ByteRange = &result.ByteRange{Start: *r.Errors[1].ByteOffset, End: len(source) + 1}
		},
		"generated candidate": func(s *validationState, _ *result.Result) {
			s.target.Source = parser.NewSource([]byte("valid original"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := state
			r := *state.retained
			r.Errors = slices.Clone(r.Errors)
			r.Omissions = slices.Clone(r.Omissions)
			s.retained = &r
			mutate(&s, &r)
			if local, err := originalEncodingFailure(s, cause, m); err != nil || local {
				t.Fatal("recovered", local, err)
			}
		})
	}
}

func TestOriginalEncodingRecoveryChargesPerError(t *testing.T) {
	state, _, cause := encodingFailureState(t, bytes.Repeat([]byte{0xFF}, engine.MaxFileEncodingErrors))
	limits := parser.DefaultLimits()
	limits.WorkUnits = 4*int64(len(state.target.Path)) + 512 + 32*engine.MaxFileEncodingErrors - 1
	tight, _ := parser.NewOperationBudget(limits)
	local, err := originalEncodingFailure(state, cause, meter{context.Background(), tight})
	var stop *parser.LimitExceeded
	if local || !errors.As(err, &stop) {
		t.Fatal(local, err)
	}
	limits.WorkUnits++
	enough, _ := parser.NewOperationBudget(limits)
	if local, err := originalEncodingFailure(state, cause, meter{context.Background(), enough}); err != nil || !local {
		t.Fatal(local, err)
	}
}
