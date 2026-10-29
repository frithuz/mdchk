package fix_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
)

type semanticFixture struct {
	registry *engine.Registry
	plan     *engine.ExecutionPlan
	budget   *parser.OperationBudget
}

func semanticSetup(t *testing.T, settings engine.Settings, check engine.Check) semanticFixture {
	t.Helper()
	if check == nil {
		check = func(context.Context, engine.RuleContext) engine.RuleOutcome { return engine.RuleOutcome{} }
	}
	r, err := engine.NewRegistry([]engine.Metadata{{ID: "MD001", Name: "fixture whitespace", DefaultEnabled: true, Fix: engine.PlannedFix}, {ID: "MD003", Name: "missing implementation", DefaultEnabled: true, Fix: engine.NoFix}}, []engine.Registration{{ID: "MD001", Check: check}})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := r.SelectRules(settings, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := selection.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	return semanticFixture{r, p, b}
}
func syntheticGroup(t *testing.T, c fix.RoundContext, start, end int, replacement string) fix.Group {
	t.Helper()
	s := c.Current()
	sp := span(t, s, start, end)
	text, _ := s.Text(sp)
	return fix.Group{ID: "fixture", Rule: "MD001", Provenance: "independent synthetic test", Anchor: sp, Edits: []fix.Edit{{Span: sp, Expected: []byte(text), Replacement: []byte(replacement)}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}}
}

// This independently registered checker establishes a narrow test-only policy:
// only leading blank physical lines may change, and their removal must leave the
// identical complete source bytes. No producer-provided claim is consulted.
func blankPrefixProof(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	a, b := c.Before.Current().Bytes(), c.After.Current().Bytes()
	if err := c.Before.Checkpoint(ctx, int64(len(a)+len(b))); err != nil {
		return fix.Unknown, err
	}
	if bytes.Equal(bytes.TrimLeft(a, "\r\n"), bytes.TrimLeft(b, "\r\n")) {
		return fix.Preserved, nil
	}
	return fix.Unsafe, nil
}
func validateSynthetic(t *testing.T, f semanticFixture, source string, producer fix.Producer, checker fix.Checker) (fix.Validated, error) {
	t.Helper()
	var checks []fix.CheckerRegistration
	if checker != nil {
		checks = []fix.CheckerRegistration{{Rule: "MD001", Check: checker}}
	}
	v, err := fix.NewValidator(f.registry, checks)
	if err != nil {
		t.Fatal(err)
	}
	return v.Validate(context.Background(), []fix.Target{{Path: "a.md", Source: parser.NewSource([]byte(source)), Plan: f.plan, Produce: producer}}, fix.ValidationOptions{Budget: f.budget})
}
func TestCandidateTrustedMeaningAndMissingProof(t *testing.T) {
	for _, tt := range []struct {
		name, replacement string
		checker           fix.Checker
		want              string
		complete          bool
	}{
		{"safe", "\n", blankPrefixProof, "\nhello", true},
		{"same AST different meaning", "goodbye", blankPrefixProof, "hello", true},
		{"missing checker", "\n", nil, "hello", true},
		{"uncertain proof", "\n", func(context.Context, fix.CheckContext) (fix.Proof, error) { return fix.Unknown, nil }, "hello", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := semanticSetup(t, nil, nil)
			producer := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
				if strings.HasPrefix(string(c.Current().Bytes()), "\n") {
					return nil, nil
				}
				end := 0
				if tt.replacement == "goodbye" {
					end = c.Current().Len()
				}
				g := syntheticGroup(t, c, 0, end, tt.replacement)
				g.Requirements = []fix.Requirement{{Kind: fix.Meaning}}
				return []fix.Group{g}, nil
			}
			out, err := validateSynthetic(t, f, "hello", producer, tt.checker)
			if err != nil {
				t.Fatal(err)
			}
			file, _ := out.File("a.md")
			if string(file.Candidate.Bytes()) != tt.want || out.Complete() != tt.complete {
				t.Fatalf("candidate=%q complete=%v reasons=%v", file.Candidate.Bytes(), out.Complete(), file.Reasons)
			}
		})
	}
}
func prefixOnce(t *testing.T) fix.Producer {
	return func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
		if bytes.HasPrefix(c.Current().Bytes(), []byte("\n")) {
			return nil, nil
		}
		return []fix.Group{syntheticGroup(t, c, 0, 0, "\n")}, nil
	}
}
func TestCandidateMappedDirectivesProtectionAndConfiguration(t *testing.T) {
	sources := []string{
		"hello\n<!-- mdchk:disable MD001 -->\n<!-- mdchk:disable MD001 -->\nx\n<!-- mdchk:enable MD001 -->",
		"hello\n<!-- mdchk:disable UNKNOWN -->\nend",
		"hello\n```\n<!-- mdchk:disable MD001 -->\n```\nend",
		"hello\n<div>raw</div>\n\nend",
		"hello\n`code`\r\nend",
	}
	for _, source := range sources {
		t.Run(source, func(t *testing.T) {
			f := semanticSetup(t, nil, nil)
			out, err := validateSynthetic(t, f, source, prefixOnce(t), blankPrefixProof)
			if err != nil {
				t.Fatal(err)
			}
			file, _ := out.File("a.md")
			if string(file.Candidate.Bytes()) != "\n"+source {
				t.Fatalf("unexpected deferral: %#v", file.Attempts)
			}
			if !out.Complete() {
				t.Fatal(file.Reasons)
			}
		})
	}
	off := false
	f := semanticSetup(t, engine.Settings{"MD001": {Enabled: &off}}, nil)
	out, err := validateSynthetic(t, f, "hello", prefixOnce(t), blankPrefixProof)
	if err != nil {
		t.Fatal(err)
	}
	file, _ := out.File("a.md")
	if string(file.Candidate.Bytes()) != "hello" {
		t.Fatal("disabled source changed")
	}
	source := "<!-- mdchk:enable MD001 -->\nhello"
	producer := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
		s := string(c.Current().Bytes())
		if strings.HasSuffix(s, "\n") {
			return nil, nil
		}
		return []fix.Group{syntheticGroup(t, c, c.Current().Len(), c.Current().Len(), "\n")}, nil
	}
	proof := func(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
		if err := c.Before.Checkpoint(ctx, int64(c.Before.Current().Len()+c.After.Current().Len())); err != nil {
			return fix.Unknown, err
		}
		if bytes.Equal(bytes.TrimRight(c.Before.Current().Bytes(), "\n"), bytes.TrimRight(c.After.Current().Bytes(), "\n")) {
			return fix.Preserved, nil
		}
		return fix.Unsafe, nil
	}
	out, err = validateSynthetic(t, semanticSetup(t, engine.Settings{"MD001": {Enabled: &off}}, nil), source, producer, proof)
	if err != nil {
		t.Fatal(err)
	}
	file, _ = out.File("a.md")
	if string(file.Candidate.Bytes()) != source+"\n" {
		t.Fatal("configured-off directive-enabled correction rejected")
	}
}
func TestCandidateCommonChecksCannotBeWaived(t *testing.T) {
	// Delimiter insertion would expose literal directive text, despite preserving
	// every original comment byte and a deliberately lying trusted checker.
	for _, source := range []string{"hello\n<!-- mdchk:disable MD003 -->\nend", "hello\n```\nbody\n```\nend", "hello\n`code`\nend"} {
		f := semanticSetup(t, nil, nil)
		producer := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
			return []fix.Group{syntheticGroup(t, c, 0, 0, "```\n")}, nil
		}
		out, err := validateSynthetic(t, f, source, producer, func(context.Context, fix.CheckContext) (fix.Proof, error) { return fix.Preserved, nil })
		if err != nil {
			t.Fatal(err)
		}
		file, _ := out.File("a.md")
		if !out.Complete() {
			t.Fatal("intentional unsafe deferral reported as interrupted")
		}
		if string(file.Candidate.Bytes()) != source {
			t.Fatal("changed comment/literal binding escaped")
		}
	}
	// Two independently harmless boundary insertions jointly create a code span.
	source := "hello"
	f := semanticSetup(t, nil, nil)
	producer := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
		a := syntheticGroup(t, c, 0, 0, "`")
		b := syntheticGroup(t, c, c.Current().Len(), c.Current().Len(), "`")
		b.ID = "other"
		return []fix.Group{a, b}, nil
	}
	out, err := validateSynthetic(t, f, source, producer, func(context.Context, fix.CheckContext) (fix.Proof, error) { return fix.Preserved, nil })
	if err != nil {
		t.Fatal(err)
	}
	file, _ := out.File("a.md")
	if string(file.Candidate.Bytes()) != source {
		t.Fatal("combined syntax escaped")
	}
}
func TestCandidateConvergenceBoundsCyclesAndComponents(t *testing.T) {
	for _, n := range []int{2, 10, 11, -1} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			f := semanticSetup(t, nil, nil)
			producer := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
				b := c.Current().Bytes()
				leading := len(b) - len(bytes.TrimLeft(b, "\n"))
				if n < 0 {
					if leading == 1 {
						return []fix.Group{syntheticGroup(t, c, 0, 1, "")}, nil
					}
				} else if leading >= n {
					return nil, nil
				}
				return []fix.Group{syntheticGroup(t, c, 0, 0, "\n")}, nil
			}
			out, err := validateSynthetic(t, f, "hello", producer, blankPrefixProof)
			if err != nil {
				t.Fatal(err)
			}
			file, _ := out.File("a.md")
			want := "hello"
			if n >= 0 && n <= 10 {
				want = strings.Repeat("\n", n) + "hello"
			}
			if string(file.Candidate.Bytes()) != want || out.Complete() != (n >= 0 && n <= 10) {
				t.Fatalf("%q complete %v reasons %v", file.Candidate.Bytes(), out.Complete(), file.Reasons)
			}
			if n < 0 && len(file.Attempts) > 2 {
				t.Fatal("cycle was not detected early")
			}
			if n == 10 && len(file.Attempts) != 11 {
				t.Fatal("missing final read-only planning check")
			}
		})
	}
	f := semanticSetup(t, nil, nil)
	v, _ := fix.NewValidator(f.registry, []fix.CheckerRegistration{{Rule: "MD001", Check: blankPrefixProof}})
	oscillate := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
		if bytes.HasPrefix(c.Current().Bytes(), []byte("\n")) {
			return []fix.Group{syntheticGroup(t, c, 0, 1, "")}, nil
		}
		return []fix.Group{syntheticGroup(t, c, 0, 0, "\n")}, nil
	}
	out, err := v.Validate(context.Background(), []fix.Target{{Path: "a.md", Source: parser.NewSource([]byte("a")), Plan: f.plan, Produce: oscillate, Dependencies: []string{"b.md"}}, {Path: "b.md", Source: parser.NewSource([]byte("b")), Plan: f.plan, Produce: prefixOnce(t)}, {Path: "c.md", Source: parser.NewSource([]byte("c")), Plan: f.plan, Produce: prefixOnce(t)}}, fix.ValidationOptions{Budget: f.budget})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"a.md", "b.md", "c.md"} {
		file, _ := out.File(path)
		want := path[:1]
		if path == "c.md" {
			want = "\nc"
		}
		if string(file.Candidate.Bytes()) != want {
			t.Fatalf("%s: %q", path, file.Candidate.Bytes())
		}
	}
	if out.Complete() {
		t.Fatal("cycle claimed complete")
	}
}
func TestCandidateSharedAnalysisParityAndOwnership(t *testing.T) {
	check := func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		s := c.Source()
		b := s.Bytes()
		at := bytes.Index(b, []byte("remaining"))
		if at < 0 {
			return engine.RuleOutcome{}
		}
		sp, _ := s.Span(at, at+len("remaining"))
		return engine.RuleOutcome{Findings: []engine.Finding{{Span: sp, Message: "report only"}}}
	}
	f := semanticSetup(t, nil, check)
	out, err := validateSynthetic(t, f, "α remaining", prefixOnce(t), blankPrefixProof)
	if err != nil {
		t.Fatal(err)
	}
	file, _ := out.File("a.md")
	if len(file.Analysis.Diagnostics) != 1 || file.Analysis.Diagnostics[0].ByteRange.Start != 4 || file.Analysis.Diagnostics[0].Range.Start.Line != 2 || file.Analysis.Diagnostics[0].Range.Start.Column != 3 {
		t.Fatalf("coordinates %#v", file.Analysis.Diagnostics)
	}
	d := file.Analysis.Diagnostics[0]
	if d.Snapshot.View != "candidate" || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256(file.Candidate.Bytes())) || d.Snapshot.SizeBytes != file.Candidate.Len() {
		t.Fatal("wrong snapshot")
	}
	directBudget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	in, _ := f.plan.NewSourceInput("a.md", file.Candidate)
	direct, _, err := f.plan.InspectCandidate(context.Background(), in, engine.RunOptions{Budget: directBudget})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(file.Analysis.Diagnostics, direct.Diagnostics) || !reflect.DeepEqual(file.Analysis.Coverage, direct.Coverage) {
		t.Fatal("shared analysis parity failed")
	}
	if file.Analysis.Complete || !out.Complete() {
		t.Fatal("missing implementation must reduce lint coverage without inventing failed fix computation")
	}
	file.Analysis.Diagnostics[0].Message = "mutation"
	file.Attempts[0].Decisions[0].Group.Edits[0].Replacement[0] = 'X'
	again, _ := out.File("a.md")
	if again.Analysis.Diagnostics[0].Message != "report only" || string(again.Candidate.Bytes()) != "\nα remaining" || again.Attempts[0].Decisions[0].Group.Edits[0].Replacement[0] != '\n' {
		t.Fatal("aliased output")
	}
	f.budget, _ = parser.NewOperationBudget(parser.DefaultLimits())
	second, err := validateSynthetic(t, f, string(file.Candidate.Bytes()), prefixOnce(t), blankPrefixProof)
	if err != nil {
		t.Fatal(err)
	}
	stable, _ := second.File("a.md")
	if stable.ChangingRounds != 0 {
		t.Fatal("not idempotent")
	}
}
func TestCandidateRuntimeFailureAndFinalCallbackCheckpoint(t *testing.T) {
	sentinel := errors.New("rule test failure")
	f := semanticSetup(t, nil, func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		if bytes.HasPrefix(c.Source().Bytes(), []byte("\n")) {
			sp, _ := c.Source().Span(1, 2)
			return engine.RuleOutcome{Err: sentinel, Findings: []engine.Finding{{Span: sp, Message: "reliable"}}}
		}
		return engine.RuleOutcome{}
	})
	out, err := validateSynthetic(t, f, "hello", prefixOnce(t), blankPrefixProof)
	if !errors.Is(err, sentinel) || out.Complete() {
		t.Fatalf("typed runtime cause lost: %v", err)
	}
	file, _ := out.File("a.md")
	if string(file.Candidate.Bytes()) != "hello" || len(file.Observations) != 1 || len(file.Observations[0].Analysis.Diagnostics) != 1 || len(file.Observations[0].Analysis.Errors) != 1 {
		t.Fatalf("rollback/observations %#v", file)
	}
	for _, where := range []string{"producer", "checker"} {
		t.Run(where, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := semanticSetup(t, nil, nil)
			p := prefixOnce(t)
			checker := blankPrefixProof
			if where == "producer" {
				p = func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
					g, _ := prefixOnce(t)(ctx, c)
					cancel()
					return g, nil
				}
			} else {
				checker = func(context.Context, fix.CheckContext) (fix.Proof, error) { cancel(); return fix.Preserved, nil }
			}
			v, _ := fix.NewValidator(f.registry, []fix.CheckerRegistration{{Rule: "MD001", Check: checker}})
			out, err := v.Validate(ctx, []fix.Target{{Path: "a.md", Source: parser.NewSource([]byte("hello")), Plan: f.plan, Produce: p}}, fix.ValidationOptions{Budget: f.budget})
			if !errors.Is(err, context.Canceled) || out.Complete() {
				t.Fatalf("late successful %s: %v", where, err)
			}
			file, _ := out.File("a.md")
			if string(file.Candidate.Bytes()) != "hello" {
				t.Fatal("intermediate escaped")
			}
		})
	}
	f = semanticSetup(t, nil, nil)
	limits := parser.DefaultLimits()
	limits.OperationTime = 20 * time.Millisecond
	f.budget, _ = parser.NewOperationBudget(limits)
	out, err = validateSynthetic(t, f, "hello", prefixOnce(t), func(context.Context, fix.CheckContext) (fix.Proof, error) {
		time.Sleep(30 * time.Millisecond)
		return fix.Preserved, nil
	})
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || limit.Name != "operation-time" || out.Complete() {
		t.Fatalf("expired proof accepted %v", err)
	}
}

func TestCandidateResourcesAdmissionAndResidualBudget(t *testing.T) {
	f := semanticSetup(t, nil, nil)
	v, _ := fix.NewValidator(f.registry, []fix.CheckerRegistration{{Rule: "MD001", Check: blankPrefixProof}})
	source := parser.NewSource([]byte("hello"))
	target := fix.Target{Path: "a.md", Source: source, Plan: f.plan, Produce: prefixOnce(t), ResourceDependencies: []string{"reference"}}
	out, err := v.Validate(context.Background(), []fix.Target{target}, fix.ValidationOptions{Budget: f.budget})
	if err != nil {
		t.Fatal(err)
	}
	file, _ := out.File("a.md")
	if out.Complete() || string(file.Candidate.Bytes()) != "hello" {
		t.Fatal("unknown resource treated as absence")
	}
	f.budget, _ = parser.NewOperationBudget(parser.DefaultLimits())
	target.Produce = func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
		r, ok := c.Resource("reference")
		if !ok || r.State != fix.ResourceAbsent {
			t.Fatal("authoritative absence lost")
		}
		if _, ok := c.Source("unselected.md"); ok {
			t.Fatal("undeclared source fabricated")
		}
		return prefixOnce(t)(ctx, c)
	}
	out, err = v.Validate(context.Background(), []fix.Target{target}, fix.ValidationOptions{Budget: f.budget, Resources: []fix.ResourceObservation{{Key: "reference", State: fix.ResourceAbsent}}})
	if err != nil {
		t.Fatal(err)
	}
	file, _ = out.File("a.md")
	if !out.Complete() || string(file.Candidate.Bytes()) != "\nhello" || len(out.Paths()) != 1 {
		t.Fatal("resource-only observation became target or blocked safe proof")
	}
	f.budget, _ = parser.NewOperationBudget(parser.DefaultLimits())
	target.ResourceDependencies = nil
	target.Produce = prefixOnce(t)
	out, err = v.Validate(context.Background(), []fix.Target{target}, fix.ValidationOptions{Budget: f.budget, TotalContentBytes: 5})
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || limit.Name != "total-content-bytes" || out.Complete() {
		t.Fatalf("overlay cap %v", err)
	}
	file, _ = out.File("a.md")
	if string(file.Candidate.Bytes()) != "hello" {
		t.Fatal("oversized overlay escaped")
	}
	// A callback exhausts the exact residual meter after earlier acquisition. No
	// default replacement budget may make this provisional correction succeed.
	for _, where := range []string{"producer", "checker"} {
		t.Run(where, func(t *testing.T) {
			f := semanticSetup(t, nil, nil)
			p := prefixOnce(t)
			checker := blankPrefixProof
			exhaust := func(ctx context.Context, c fix.RoundContext) error {
				return c.Checkpoint(ctx, f.budget.Limits().WorkUnits)
			}
			if where == "producer" {
				p = func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
					g, _ := prefixOnce(t)(ctx, c)
					_ = exhaust(ctx, c)
					return g, nil
				}
			} else {
				checker = func(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
					_ = exhaust(ctx, c.After)
					return fix.Preserved, nil
				}
			}
			out, err := validateSynthetic(t, f, "hello", p, checker)
			var limit *parser.LimitExceeded
			if !errors.As(err, &limit) || out.Complete() {
				t.Fatalf("residual meter reset: %v", err)
			}
			file, _ := out.File("a.md")
			if string(file.Candidate.Bytes()) != "hello" {
				t.Fatal("exhausted provisional bytes retained")
			}
		})
	}
}
func TestCandidateInvalidOriginalAndForeignAuthority(t *testing.T) {
	f := semanticSetup(t, nil, nil)
	out, err := validateSynthetic(t, f, string([]byte{0xff}), prefixOnce(t), blankPrefixProof)
	if err != nil || out.Complete() {
		t.Fatal("invalid original must be locally retained without operation termination", err)
	}
	file, _ := out.File("a.md")
	if !bytes.Equal(file.Candidate.Bytes(), []byte{0xff}) || len(file.Analysis.Errors) != 1 {
		t.Fatal("invalid original observation lost")
	}
	f = semanticSetup(t, nil, nil)
	other := semanticSetup(t, nil, nil)
	v, _ := fix.NewValidator(f.registry, nil)
	if _, err := v.Validate(context.Background(), []fix.Target{{Path: "a.md", Source: parser.NewSource([]byte("hello")), Plan: other.plan, Produce: prefixOnce(t)}}, fix.ValidationOptions{Budget: f.budget}); err == nil {
		t.Fatal("foreign admitted registry accepted")
	}
}
func TestCandidateConflictsAndAtomicDeferral(t *testing.T) {
	f := semanticSetup(t, nil, nil)
	producer := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
		if bytes.HasPrefix(c.Current().Bytes(), []byte("\n")) {
			return nil, nil
		}
		a := syntheticGroup(t, c, 0, 1, "X")
		b := syntheticGroup(t, c, 0, 2, "Y")
		b.ID = "overlap"
		safe := syntheticGroup(t, c, c.Current().Len(), c.Current().Len(), "\n")
		safe.ID = "safe"
		return []fix.Group{a, b, safe}, nil
	}
	proof := func(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
		if err := c.Before.Checkpoint(ctx, int64(c.Before.Current().Len()+c.After.Current().Len())); err != nil {
			return fix.Unknown, err
		}
		if bytes.Equal(bytes.TrimRight(c.Before.Current().Bytes(), "\n"), bytes.TrimRight(c.After.Current().Bytes(), "\n")) {
			return fix.Preserved, nil
		}
		return fix.Unsafe, nil
	}
	// The remaining conflict groups continue to be reported, without forcing a
	// lint-clean candidate or rejecting unrelated safe work.
	p := producer
	producer = func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
		if bytes.HasSuffix(c.Current().Bytes(), []byte("\n")) {
			return nil, nil
		}
		return p(ctx, c)
	}
	out, err := validateSynthetic(t, f, "hello", producer, proof)
	if err != nil {
		t.Fatal(err)
	}
	file, _ := out.File("a.md")
	if !out.Complete() || string(file.Candidate.Bytes()) != "hello\n" {
		t.Fatalf("conflict discarded independent change: %v", file.Reasons)
	}
	if file.Attempts[0].Decisions[0].Accepted || file.Attempts[0].Decisions[1].Accepted || !file.Attempts[0].Decisions[2].Accepted {
		t.Fatal("wrong atomic/conflict decisions")
	}
}

func TestCandidateDefaultResidualScaling(t *testing.T) {
	f := semanticSetup(t, nil, nil)
	source := strings.Repeat("x\n\n", 8000)
	out, err := validateSynthetic(t, f, source, prefixOnce(t), blankPrefixProof)
	if err != nil {
		t.Fatal(err)
	}
	file, _ := out.File("a.md")
	if string(file.Candidate.Bytes()) != "\n"+source || !out.Complete() {
		t.Fatal("bounded short-line candidate failed")
	}
}
func TestCandidateBlockedAndInactiveDirectiveParity(t *testing.T) {
	for _, global := range []bool{true, false} {
		for _, allowed := range []bool{true, false} {
			t.Run(fmt.Sprintf("global=%v allowed=%v", global, allowed), func(t *testing.T) {
				f := semanticSetup(t, engine.Settings{"MD001": {AllowDirectives: &allowed}}, nil)
				rules := f.plan.Rules()
				f.plan = f.plan.WithDirectivePolicy(policy{rules, global})
				source := "hello\n<!-- mdchk:disable MD003 -->\nend"
				out, err := validateSynthetic(t, f, source, prefixOnce(t), blankPrefixProof)
				if err != nil {
					t.Fatal(err)
				}
				file, _ := out.File("a.md")
				if !out.Complete() || string(file.Candidate.Bytes()) != "\n"+source {
					t.Fatalf("policy parity %#v", file.Reasons)
				}
			})
		}
	}
}

func TestCandidateOpaqueHandleOwnership(t *testing.T) {
	f := semanticSetup(t, nil, nil)
	off := false
	replacement := semanticSetup(t, engine.Settings{"MD001": {Enabled: &off}}, nil)
	v, _ := fix.NewValidator(f.registry, []fix.CheckerRegistration{{Rule: "MD001", Check: blankPrefixProof}})
	producer := func(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
		*f.plan = *replacement.plan
		*f.registry = *replacement.registry
		return prefixOnce(t)(ctx, c)
	}
	out, err := v.Validate(context.Background(), []fix.Target{{Path: "a.md", Source: parser.NewSource([]byte("hello")), Plan: f.plan, Produce: producer}}, fix.ValidationOptions{Budget: f.budget})
	if err != nil {
		t.Fatal(err)
	}
	file, _ := out.File("a.md")
	if string(file.Candidate.Bytes()) != "\nhello" || !out.Complete() {
		t.Fatal("caller assignment changed admitted operation authority")
	}
	parsed := file.Parsed()
	if parsed.Document == nil || parsed.Source != file.Candidate || file.Directives().Source() != file.Candidate {
		t.Fatal("final owned parse/directives do not identify final bytes")
	}
	*parsed.Document = parser.Document{}
	again, _ := out.File("a.md")
	if again.Parsed().Document.Source() != again.Candidate {
		t.Fatal("parsed handle aliases authority")
	}
}

func TestCandidateRetainedReanalysisCannotInvalidateProof(t *testing.T) {
	calls := 0
	f := semanticSetup(t, nil, func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		calls++
		if calls < 3 {
			return engine.RuleOutcome{}
		}
		sp, _ := c.Source().Span(1, 2)
		return engine.RuleOutcome{Findings: []engine.Finding{{Span: sp, Message: "new forbidden final observation"}}}
	})
	out, err := validateSynthetic(t, f, "hello", prefixOnce(t), blankPrefixProof)
	var failure *fix.AnalysisFailure
	if !errors.As(err, &failure) || failure.Code != "unstable-reanalysis" || out.Complete() {
		t.Fatalf("changed final result escaped proof %v", err)
	}
	file, _ := out.File("a.md")
	if string(file.Candidate.Bytes()) != "hello" || len(file.Observations) == 0 || len(file.Observations[0].Analysis.Diagnostics) != 1 {
		t.Fatal("unverified final candidate or lost reliable final observation")
	}
}

func TestCandidateForbiddenFindingsUseMappedIdentity(t *testing.T) {
	for _, newOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(newOnly), func(t *testing.T) {
			f := semanticSetup(t, nil, func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
				raw := c.Source().Bytes()
				if newOnly && !bytes.HasPrefix(raw, []byte("\n")) {
					return engine.RuleOutcome{}
				}
				at := bytes.Index(raw, []byte("hello"))
				sp, _ := c.Source().Span(at, at+5)
				return engine.RuleOutcome{Findings: []engine.Finding{{Span: sp, Message: "remaining reliable finding"}}}
			})
			checks := []fix.CheckerRegistration{{Rule: "MD001", Check: blankPrefixProof, ForbidNewFindings: []string{"MD001"}}}
			v, err := fix.NewValidator(f.registry, checks)
			if err != nil {
				t.Fatal(err)
			}
			checks[0].ForbidNewFindings[0] = "MD003"
			out, err := v.Validate(context.Background(), []fix.Target{{Path: "a.md", Source: parser.NewSource([]byte("hello")), Plan: f.plan, Produce: prefixOnce(t)}}, fix.ValidationOptions{Budget: f.budget})
			if err != nil {
				t.Fatal(err)
			}
			file, _ := out.File("a.md")
			want := "\nhello"
			if newOnly {
				want = "hello"
			}
			if string(file.Candidate.Bytes()) != want || !out.Complete() {
				t.Fatalf("mapped forbidden policy %q complete %v", file.Candidate.Bytes(), out.Complete())
			}
			if newOnly && file.Attempts[0].Decisions[0].Reasons[0].Code != "forbidden-new-finding" {
				t.Fatal("missing explicit forbidden-finding deferral")
			}
		})
	}
}

func TestCandidatePairedFenceProofAndInteriorBinding(t *testing.T) {
	for _, unsafe := range []bool{false, true} {
		t.Run(fmt.Sprint(unsafe), func(t *testing.T) {
			source := "``` lang\nbody\n```\n"
			from, to := "```", "````"
			if unsafe {
				source = "```` lang\nbody\n```\nremaining\n````\n"
				from, to = "````", "```"
			}
			f := semanticSetup(t, nil, nil)
			producer := func(_ context.Context, c fix.RoundContext) ([]fix.Group, error) {
				raw := c.Current().Bytes()
				if !bytes.HasPrefix(raw, []byte(from+" lang")) {
					return nil, nil
				}
				end := bytes.LastIndex(raw, []byte(from))
				a := syntheticGroup(t, c, 0, len(from), to)
				b := syntheticGroup(t, c, end, end+len(from), to)
				a.Edits = append(a.Edits, b.Edits...)
				return []fix.Group{a}, nil
			}
			// Independent synthetic checker proves only this fixture's exact paired fence
			// correction. Common validation must still reject a newly closing inner fence.
			checker := func(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
				if err := c.Before.Checkpoint(ctx, int64(c.Before.Current().Len()+c.After.Current().Len())); err != nil {
					return fix.Unknown, err
				}
				edits := c.Edits()
				if len(edits) != 2 {
					return fix.Unsafe, nil
				}
				for _, e := range edits {
					if string(e.Edit.Expected) != from || string(e.Edit.Replacement) != to {
						return fix.Unsafe, nil
					}
				}
				return fix.Preserved, nil
			}
			out, err := validateSynthetic(t, f, source, producer, checker)
			if err != nil {
				t.Fatal(err)
			}
			file, _ := out.File("a.md")
			want := strings.ReplaceAll(source, from, to)
			if unsafe {
				want = source
			}
			if string(file.Candidate.Bytes()) != want || !out.Complete() {
				t.Fatalf("paired binding candidate %q complete %v", file.Candidate.Bytes(), out.Complete())
			}
		})
	}
}

func TestCandidateExhaustedReanalysisRetainsObservedOutcome(t *testing.T) {
	f := semanticSetup(t, nil, func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
		if !bytes.HasPrefix(c.Source().Bytes(), []byte("\n")) {
			return engine.RuleOutcome{}
		}
		sp, _ := c.Source().Span(1, 2)
		var err error
		for err == nil {
			err = c.Checkpoint(ctx, 1000)
		}
		return engine.RuleOutcome{Findings: []engine.Finding{{Span: sp, Message: "known but unprojected"}}, Err: err}
	})
	out, err := validateSynthetic(t, f, "hello", prefixOnce(t), blankPrefixProof)
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || out.Complete() {
		t.Fatalf("lost exhausted reanalysis cause %v", err)
	}
	file, _ := out.File("a.md")
	if string(file.Candidate.Bytes()) != "hello" || len(file.Observations) != 1 || len(file.Observations[0].Analysis.Interruptions) == 0 || len(file.Observations[0].Analysis.Omissions)+len(file.Observations[0].Analysis.Diagnostics) == 0 {
		t.Fatalf("exhausted observations lost %#v", file.Observations)
	}
}
