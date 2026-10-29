package fix_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules"
	"testing"
)

func encodingProduction(t *testing.T) (*engine.ExecutionPlan, *fix.Validator, *parser.OperationBudget) {
	t.Helper()
	r, err := engine.NewCatalogRegistry(rules.Registrations())
	if err != nil {
		t.Fatal(err)
	}
	settings := engine.Settings{}
	for _, m := range engine.Catalog() {
		enabled := m.ID == "MD037" || m.ID == "MD047" || m.ID == "MD094"
		settings[m.ID] = engine.RuleSetting{Enabled: &enabled}
	}
	selection, err := r.SelectRules(settings, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: "gfm"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := fix.NewValidator(r, rules.CorrectionCheckers())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	return plan, validator, b
}

func TestCandidateOriginalEncodingIsolation(t *testing.T) {
	for _, kind := range []string{"component", "resource-transitive"} {
		t.Run(kind, func(t *testing.T) {
			plan, v, b := encodingProduction(t)
			bad := parser.NewSource([]byte{'x', 0xff})
			good := parser.NewSource([]byte("A * � *"))
			independent := parser.NewSource([]byte("independent"))
			calls := map[string]int{}
			produce := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
				calls[c.Path()]++
				return rules.Corrections(ctx, c)
			}
			targets := []fix.Target{{Path: "a.md", Source: good, Plan: plan, Produce: produce}, {Path: "bad.md", Source: bad, Plan: plan, Produce: produce}, {Path: "z.md", Source: independent, Plan: plan, Produce: produce}}
			var resources []fix.ResourceObservation
			if kind == "component" {
				targets[0].Dependencies = []string{"bad.md"}
			} else {
				targets[0].ResourceDependencies = []string{"bridge.md"}
				targets = append(targets, fix.Target{Path: "bridge.md", Source: parser.NewSource([]byte("bridge")), Plan: plan, Produce: produce, ResourceDependencies: []string{"bad.md"}})
				resources = []fix.ResourceObservation{{Key: "bad.md", State: fix.ResourceContent, Source: bad}, {Key: "bridge.md", State: fix.ResourceContent, Source: targets[3].Source}}
			}
			out, err := v.Validate(context.Background(), targets, fix.ValidationOptions{Budget: b, Resources: resources})
			if err != nil || out.Complete() {
				t.Fatal(out.Complete(), err)
			}
			for _, name := range []string{"a.md", "bad.md"} {
				file, ok := out.File(name)
				if !ok || !file.AnalysisStarted || file.Candidate != file.Original || len(file.Attempts) != 0 || len(file.Reasons) == 0 || calls[name] != 0 {
					t.Fatal(name, file, calls)
				}
			}
			peer, _ := out.File("a.md")
			if len(peer.Analysis.Diagnostics) != 3 {
				t.Fatal("retained original MD037+MD047 findings", peer.Analysis)
			}
			invalid, _ := out.File("bad.md")
			if len(invalid.Analysis.Errors) != 1 || len(invalid.Analysis.Diagnostics) != 0 || len(invalid.Observations) != 0 || !bytes.Equal(invalid.Candidate.Bytes(), []byte{'x', 0xff}) {
				t.Fatal(invalid)
			}
			e := invalid.Analysis.Errors[0]
			if e.Code != "invalid-utf8" || e.Rule != "MD094" || e.ByteOffset == nil || *e.ByteOffset != 1 || e.Snapshot == nil || e.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte{'x', 0xff})) {
				t.Fatal(e)
			}
			approved, _ := out.File("z.md")
			if string(approved.Candidate.Bytes()) != "independent\n" || len(approved.Reasons) != 0 || calls["z.md"] == 0 {
				t.Fatal(approved, calls)
			}
			if kind == "resource-transitive" {
				bridge, _ := out.File("bridge.md")
				if bridge.Candidate != bridge.Original || len(bridge.Reasons) == 0 || calls["bridge.md"] != 0 {
					t.Fatal(bridge, calls)
				}
			}
		})
	}
}

func TestCandidateOriginalEncodingNonlocalFailureRemainsFatal(t *testing.T) {
	sentinel := errors.New("trusted initial callback failure")
	f := semanticSetup(t, nil, func(context.Context, engine.RuleContext) engine.RuleOutcome { return engine.RuleOutcome{Err: sentinel} })
	v, _ := fix.NewValidator(f.registry, nil)
	out, err := v.Validate(context.Background(), []fix.Target{{Path: "a.md", Source: parser.NewSource([]byte("valid")), Plan: f.plan}, {Path: "z.md", Source: parser.NewSource([]byte("unvisited")), Plan: f.plan}}, fix.ValidationOptions{Budget: f.budget})
	if !errors.Is(err, sentinel) || out.Complete() {
		t.Fatal(err)
	}
	later, _ := out.File("z.md")
	if later.AnalysisStarted {
		t.Fatal("fatal shared failure continued", later)
	}
}

func TestCandidateGeneratedInvalidReplacementIsRejected(t *testing.T) {
	f := semanticSetup(t, nil, nil)
	out, err := validateSynthetic(t, f, "hello", func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
		return []fix.Group{syntheticGroup(t, c, 0, 0, string([]byte{0xff}))}, nil
	}, blankPrefixProof)
	if err != nil || !out.Complete() {
		t.Fatal(err)
	}
	file, _ := out.File("a.md")
	if string(file.Candidate.Bytes()) != "hello" || len(file.Analysis.Errors) != 0 || len(file.Attempts) == 0 {
		t.Fatal(file)
	}
	rejected := false
	for _, attempt := range file.Attempts {
		for _, d := range attempt.Decisions {
			for _, reason := range d.Reasons {
				rejected = rejected || reason.Code == "invalid-replacement"
			}
		}
	}
	if !rejected {
		t.Fatal(file.Attempts)
	}
}
