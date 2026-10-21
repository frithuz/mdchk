package result_test

import (
	"encoding/json"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func TestCoverageCanonicalSemanticIdentity(t *testing.T) {
	point := &result.Range{Start: result.Position{Line: 1, Column: 3}, End: result.Position{Line: 1, Column: 4}}
	longer := &result.Range{Start: point.Start, End: result.Position{Line: 2, Column: 1}}
	snap := &result.Snapshot{View: "original", SHA256: "one", SizeBytes: 8}
	other := *snap
	other.SHA256 = "two"
	base := result.Coverage{Path: "a", Range: point, Rule: "MD001", Reason: result.ContextUnavailable, Message: "context", Snapshot: snap, CauseIDs: []string{"error-2", "error-1", "error-1"}}
	duplicate := base
	duplicate.ID = "old-generated-id"
	duplicate.CauseIDs = []string{"error-1", "error-2"}
	end, snapshot, cause := base, base, base
	end.Range, snapshot.Snapshot, cause.CauseIDs = longer, &other, []string{"error-3"}
	// These two records exercise rule before reason, and reason before end.
	rule, reason := end, base
	rule.Rule = "MD003"
	reason.Reason = result.ParserUncertain
	input := []result.Coverage{
		base, duplicate, end, snapshot, cause, rule, reason,
		{Reason: result.RuleUnavailable, Message: "build-wide", Rule: "MD004"},
		{Path: "a", Reason: result.RuleUnavailable, Message: "unlocated"},
		{Path: "z", Reason: "saved-view-unavailable", Message: "open vocabulary"},
	}
	want := result.NewLint("cli")
	want.Coverage = input
	want.CanonicalizeCoverage()
	if len(want.Coverage) != len(input)-1 {
		t.Fatalf("lost semantic distinctions or kept duplicate: %+v", want.Coverage)
	}
	if want.Coverage[0].Path != "" || want.Coverage[1].Range != nil || want.Coverage[2].Range == nil || want.Coverage[6].Reason != result.ParserUncertain || want.Coverage[7].Rule != "MD003" {
		t.Fatalf("required absent-first path/start/rule/reason order: %+v", want.Coverage)
	}
	encoded, err := json.Marshal(want.Coverage)
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewPCG(216, 1)) //nolint:gosec // G404: deterministic test permutations, not security-sensitive randomness.
	for range 100 {
		got := result.NewLint("cli")
		got.Coverage = slices.Clone(input)
		rng.Shuffle(len(got.Coverage), func(i, j int) { got.Coverage[i], got.Coverage[j] = got.Coverage[j], got.Coverage[i] })
		got.CanonicalizeCoverage()
		data, err := json.Marshal(got.Coverage)
		if err != nil || string(data) != string(encoded) {
			t.Fatalf("permutation changed canonical JSON: %s vs %s (%v)", data, encoded, err)
		}
	}
	if !reflect.DeepEqual(base.CauseIDs, []string{"error-2", "error-1", "error-1"}) || input[1].ID == "" {
		t.Fatal("canonicalization mutated caller-owned records")
	}
}

func TestCoverageJSONRequiredFieldsAndAbsentLocation(t *testing.T) {
	c := result.Coverage{ID: "coverage-1", Rule: "MD001", Reason: result.RuleUnavailable, Message: "Unavailable in this build."}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"coverage-1","reason":"rule-unavailable","message":"Unavailable in this build.","rule":"MD001"}`
	if string(data) != want {
		t.Fatalf("fabricated source attribution or missing required/catalog fields: %s", data)
	}
	data, err = json.Marshal(result.Coverage{})
	if err != nil || string(data) != `{"id":"","reason":"","message":""}` {
		t.Fatalf("required reason/message omitted: %s (%v)", data, err)
	}
}

func TestLintFixOutcomePrecedenceAndPreservation(t *testing.T) {
	for _, operation := range []string{"lint", "fix"} {
		for _, tc := range []struct {
			name, status                                        string
			coverage, findings, failure, interruption, omission bool
		}{
			{name: "clean", status: "clean"},
			{name: "violations", findings: true, status: "violations"},
			{name: "deficit", coverage: true, status: "incomplete"},
			{name: "deficit and findings", coverage: true, findings: true, status: "incomplete"},
			{name: "failure with observations", coverage: true, findings: true, failure: true, status: "error"},
			{name: "failure after complete analysis", failure: true, status: "error"},
			{name: "interruption", interruption: true, findings: true, status: "incomplete"},
			{name: "omission", omission: true, status: "incomplete"},
			{name: "failure and interruption", interruption: true, failure: true, status: "error"},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				out := result.NewLint("cli")
				out.Operation = operation
				if tc.coverage {
					out.Coverage = append(out.Coverage, result.Coverage{Reason: result.ContextUnavailable, Message: "missing context"})
				}
				if tc.findings {
					out.Diagnostics = append(out.Diagnostics, result.Diagnostic{Message: "observed"})
				}
				if tc.failure {
					out.Errors = append(out.Errors, result.Error{Scope: "operation", Category: result.IO, Message: "failed"})
				}
				if tc.interruption {
					out.Interruptions = append(out.Interruptions, result.Interruption{Reason: "cancelled"})
				}
				if tc.omission {
					out.Omissions = append(out.Omissions, result.Omission{Section: "/diagnostics", Reason: "output-limit"})
				}
				before, err := json.Marshal(out)
				if err != nil {
					t.Fatal(err)
				}
				out.FinalizeLintFixOutcome()
				if out.Status != tc.status || out.Complete != !(tc.coverage || tc.interruption || tc.omission) {
					t.Fatal(out)
				}
				out.Status, out.Complete = "clean", true
				after, err := json.Marshal(out)
				if err != nil || string(before) != string(after) {
					t.Fatalf("lost observations: %s vs %s", before, after)
				}
			})
		}
	}
}

func TestOutcomeDoesNotOverrideInspectionOrConfigurationValidation(t *testing.T) {
	for _, tc := range []struct {
		operation, status string
		complete          bool
	}{
		{"inspect", "inspected", false},
		{"config validate", "error", true},
		{"config validate", "clean", true},
	} {
		out := result.NewLint("cli")
		out.Operation, out.Status, out.Complete = tc.operation, tc.status, tc.complete
		if tc.operation == "inspect" {
			out.Coverage = append(out.Coverage, result.Coverage{Reason: result.RuleUnavailable, Message: "observed unavailable section"})
			out.Errors = append(out.Errors, result.Error{Scope: "observation", Category: result.IO, Message: "observed invalid encoding"})
		} else if tc.status == "error" {
			out.Errors = append(out.Errors, result.Error{Scope: "operation", Category: result.Configuration, Message: "invalid configuration"})
		}
		before, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		out.FinalizeLintFixOutcome()
		after, err := json.Marshal(out)
		if err != nil || string(before) != string(after) {
			t.Fatalf("operation-specific state changed: %s", after)
		}
	}
}
