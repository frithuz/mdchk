package fix_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
)

type policy struct {
	rules  map[string]engine.ResolvedRule
	global bool
}

func (p policy) DirectiveConfiguration() (bool, map[string]engine.ResolvedRule, error) {
	return p.global, p.rules, nil
}
func (p policy) DirectivePermission(id string) engine.DirectiveDecision {
	return engine.DirectiveDecision{Allowed: p.rules[id].AllowDirectives}
}
func (p policy) GlobalDirectivePermission() engine.DirectiveDecision {
	return engine.DirectiveDecision{Allowed: p.global}
}
func (p policy) DirectivePermissionWork(string) (int64, error) { return 0, nil }

type fixture struct {
	parsed   parser.ParseResult
	state    engine.DirectiveResult
	registry *engine.Registry
	budget   *parser.OperationBudget
}

func setup(t *testing.T, text string, settings engine.Settings, global bool) fixture {
	return setupLimits(t, text, settings, global, parser.DefaultLimits())
}
func setupLimits(t *testing.T, text string, settings engine.Settings, global bool, limits parser.Limits) fixture {
	t.Helper()
	r, err := engine.NewRegistry([]engine.Metadata{{ID: "MD001", Name: "test", DefaultEnabled: true, Fix: engine.PlannedFix}, {ID: "MD003", Name: "other", DefaultEnabled: true, Fix: engine.PlannedFix}, {ID: "MD004", Name: "report only", DefaultEnabled: true, Fix: engine.NoFix}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := parser.NewOperationBudget(limits)
	parsed := parser.ParseWithBudget(context.Background(), parser.NewSource([]byte(text)), b)
	rules, err := r.ResolveSettings(settings, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	state := engine.ProcessDirectives(context.Background(), parsed, policy{rules, global}, b)
	if !state.Complete() {
		t.Fatal(state.Err())
	}
	return fixture{parsed, state, r, b}
}
func span(t *testing.T, s parser.Source, a, b int) parser.Span {
	t.Helper()
	v, e := s.Span(a, b)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func edit(t *testing.T, f fixture, a, b int, replacement string) fix.Edit {
	t.Helper()
	s := span(t, f.parsed.Source, a, b)
	old, _ := f.parsed.Source.Text(s)
	return fix.Edit{Span: s, Expected: []byte(old), Replacement: []byte(replacement)}
}
func group(t *testing.T, f fixture, id string, edits ...fix.Edit) fix.Group {
	t.Helper()
	return fix.Group{ID: id, Rule: "MD001", Anchor: span(t, f.parsed.Source, 0, 0), Edits: edits, Provenance: "fixture", Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}}
}
func plan(t *testing.T, f fixture, g ...fix.Group) fix.StructuralPlan {
	t.Helper()
	p, e := fix.Plan(context.Background(), f.parsed, f.state, f.registry, g, f.budget)
	if e != nil {
		t.Fatal(e)
	}
	if !p.Complete() {
		t.Fatal("incomplete")
	}
	return p
}
func accepted(p fix.StructuralPlan) []string {
	var ids []string
	for _, d := range p.Decisions() {
		if d.Accepted {
			ids = append(ids, d.Group.ID)
		}
	}
	return ids
}
func reason(p fix.StructuralPlan, id, code string) bool {
	for _, d := range p.Decisions() {
		if d.Group.ID == id {
			for _, r := range d.Reasons {
				if r.Code == code {
					return true
				}
			}
		}
	}
	return false
}

func TestOriginalOffsetsOwnedAtomicApplication(t *testing.T) {
	f := setup(t, "aé\r\nb\rc\nend", nil, true)
	a := group(t, f, "pair", edit(t, f, 0, 1, "PREFIX"), edit(t, f, 9, 12, "!"))
	b := group(t, f, "middle", edit(t, f, 6, 7, "B"))
	p := plan(t, f, a, b)
	if got := string(p.ProvisionalBytes()); got != "PREFIXé\r\nbBc\n!" {
		t.Fatalf("bytes %q", got)
	}
	a.Edits[0].Replacement[0] = '?'
	a.Edits[1].Expected[0] = '?'
	a.Requirements[0].Kind = "unknown"
	views := p.Decisions()
	views[0].Group.Edits[0].Replacement[0] = '?'
	views[0].Group.Requirements[0].Kind = "unknown"
	bytes := p.ProvisionalBytes()
	bytes[0] = '?'
	edits := p.Edits()
	edits[0].Edit.Replacement[0] = '?'
	edits[0].Groups[0] = "?"
	if string(p.ProvisionalBytes()) != "PREFIXé\r\nbBc\n!" || string(f.parsed.Source.Bytes()) != "aé\r\nb\rc\nend" {
		t.Fatal("writable alias")
	}
}
func TestBoundariesOwnershipPreconditionsAndRules(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(*testing.T, fixture, *fix.Group)
		code   string
	}{
		{"foreign anchor", func(t *testing.T, f fixture, g *fix.Group) {
			g.Anchor = span(t, parser.NewSource(f.parsed.Source.Bytes()), 0, 0)
		}, "invalid-anchor"},
		{"foreign edit", func(t *testing.T, f fixture, g *fix.Group) {
			g.Edits[0].Span = span(t, parser.NewSource(f.parsed.Source.Bytes()), 0, 1)
		}, "invalid-span"},
		{"absent", func(_ *testing.T, _ fixture, g *fix.Group) { g.Edits[0].Span = parser.Span{} }, "invalid-span"},
		{"scalar", func(t *testing.T, f fixture, g *fix.Group) { g.Edits[0] = edit(t, f, 2, 3, "x") }, "invalid-span"},
		{"crlf", func(t *testing.T, f fixture, g *fix.Group) { g.Edits[0] = edit(t, f, 3, 4, "x") }, "invalid-span"},
		{"anchor scalar", func(t *testing.T, f fixture, g *fix.Group) { g.Anchor = span(t, f.parsed.Source, 2, 2) }, "invalid-anchor"},
		{"precondition", func(_ *testing.T, _ fixture, g *fix.Group) { g.Edits[0].Expected = []byte("z") }, "precondition-mismatch"},
		{"encoding", func(_ *testing.T, _ fixture, g *fix.Group) { g.Edits[0].Replacement = []byte{0xff} }, "invalid-replacement"},
		{"unknown", func(_ *testing.T, _ fixture, g *fix.Group) { g.Rule = "MD999" }, "ineligible-rule"},
		{"reportonly", func(_ *testing.T, _ fixture, g *fix.Group) { g.Rule = "MD004" }, "ineligible-rule"},
		{"preservation", func(_ *testing.T, _ fixture, g *fix.Group) { g.Requirements = nil }, "preservation-unresolved"},
		{"unknown preservation", func(_ *testing.T, _ fixture, g *fix.Group) { g.Requirements[0].Kind = "trust-me" }, "preservation-unresolved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t, "aé\r\nz", nil, true)
			g := group(t, f, "bad", edit(t, f, 0, 1, "A"))
			tc.modify(t, f, &g)
			p := plan(t, f, g)
			if len(accepted(p)) != 0 || !reason(p, "bad", tc.code) || string(p.ProvisionalBytes()) != "aé\r\nz" {
				t.Fatalf("%+v", p.Decisions())
			}
		})
	}
	for _, text := range []string{"", "a", "é", "a\r\n", "a\r", "a\n"} {
		f := setup(t, text, nil, true)
		p := plan(t, f, group(t, f, "eof", edit(t, f, len(text), len(text), "!")))
		if string(p.ProvisionalBytes()) != text+"!" {
			t.Fatal(text)
		}
	}
}
func TestDuplicateTransitiveAtomicUnits(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		f := setup(t, "abcdefghijk", nil, true)
		x := edit(t, f, 0, 1, "A")
		y := edit(t, f, 2, 3, "C")
		z := edit(t, f, 4, 5, "E")
		w := edit(t, f, 6, 7, "G")
		if invalid {
			w.Expected = []byte("wrong")
		}
		gs := []fix.Group{group(t, f, "A", x, y), group(t, f, "B", y, z), group(t, f, "C", z, w), group(t, f, "D", edit(t, f, 9, 10, "J"))}
		for range 3 {
			p := plan(t, f, gs...)
			want := "AbCdEfGhiJk"
			if invalid {
				want = "abcdefghiJk"
			}
			if string(p.ProvisionalBytes()) != want {
				t.Fatalf("%t %q", invalid, p.ProvisionalBytes())
			}
			if invalid && !reason(p, "A", "atomic-dependency") {
				t.Fatal(p.Decisions())
			}
			if !invalid {
				if len(p.Edits()) != 5 || !reflect.DeepEqual(p.Edits()[1].Groups, []string{"A", "B"}) {
					t.Fatal(p.Edits())
				}
			}
			slices.Reverse(gs)
		}
	}
	// A shared edit with an invalid precondition still joins both groups.
	f := setup(t, "abc", nil, true)
	a := group(t, f, "A", edit(t, f, 0, 1, "A"))
	b := group(t, f, "B", edit(t, f, 0, 1, "A"))
	b.Edits[0].Expected = []byte("wrong")
	p := plan(t, f, a, b)
	if len(accepted(p)) != 0 {
		t.Fatal(p.Decisions())
	}
}
func TestConflictGraphAndEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name       string
		a, b, c, d int
		ar, br     string
		conflict   bool
	}{
		{"adjacent", 1, 3, 3, 5, "A", "B", false}, {"overlap", 1, 4, 3, 5, "A", "B", true},
		{"insertion start", 1, 4, 1, 1, "A", "B", true}, {"insertion middle", 1, 4, 2, 2, "A", "B", true}, {"insertion end", 1, 4, 4, 4, "A", "B", true},
		{"distinct insert", 2, 2, 2, 2, "A", "B", true}, {"same insert", 2, 2, 2, 2, "A", "A", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t, "abcdefghij", nil, true)
			a := group(t, f, "A", edit(t, f, tc.a, tc.b, tc.ar))
			b := group(t, f, "B", edit(t, f, tc.c, tc.d, tc.br))
			d := group(t, f, "D", edit(t, f, 9, 10, "J"))
			gs := []fix.Group{a, b, d}
			p := plan(t, f, gs...)
			slices.Reverse(gs)
			q := plan(t, f, gs...)
			if !reflect.DeepEqual(p.Decisions(), q.Decisions()) || !slices.Equal(p.ProvisionalBytes(), q.ProvisionalBytes()) {
				t.Fatal("order changes result")
			}
			if tc.conflict {
				if !reflect.DeepEqual(accepted(p), []string{"D"}) || !reason(p, "A", "conflict") || !reason(p, "B", "conflict") {
					t.Fatal(p.Decisions())
				}
			} else if len(accepted(p)) != 3 {
				t.Fatal(p.Decisions())
			}
		})
	}
	f := setup(t, "abcdefghij", nil, true)
	p := plan(t, f, group(t, f, "A", edit(t, f, 0, 4, "A")), group(t, f, "B", edit(t, f, 3, 7, "B")), group(t, f, "C", edit(t, f, 6, 9, "C")), group(t, f, "D", edit(t, f, 9, 10, "D")))
	if !reflect.DeepEqual(accepted(p), []string{"D"}) {
		t.Fatal(p.Decisions())
	}
}
func TestPermissionsWholeSpanAnchorAndEOF(t *testing.T) {
	text := "a<!-- mdchk:disable MD001 -->b<!-- mdchk:enable MD001 -->c"
	for _, tc := range []struct {
		name         string
		anchor, a, b int
		code         string
	}{
		{"cross disabled", 0, 0, len(text), "disabled-span"}, {"disabled anchor", strings.Index(text, "b<!--"), len(text) - 1, len(text), "disabled-anchor"},
		{"disabled insertion", 0, strings.Index(text, "b<!--"), strings.Index(text, "b<!--"), "disabled-insertion"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t, text, nil, true)
			g := group(t, f, "A", edit(t, f, tc.a, tc.b, "X"))
			g.Anchor = span(t, f.parsed.Source, tc.anchor, tc.anchor)
			p := plan(t, f, g)
			if !reason(p, "A", tc.code) {
				t.Fatal(p.Decisions())
			}
		})
	}
	for _, last := range []string{"disable", "enable"} {
		text := "<!-- mdchk:disable MD001 --><!-- mdchk:" + last + " MD001 -->"
		f := setup(t, text, nil, true)
		g := group(t, f, "A", edit(t, f, len(text), len(text), "!"))
		g.Anchor = span(t, f.parsed.Source, len(text), len(text))
		p := plan(t, f, g)
		if (len(accepted(p)) == 1) != (last == "enable") {
			t.Fatal(p.Decisions())
		}
	}
	off := false
	f := setup(t, "abc", engine.Settings{"MD001": {Enabled: &off}}, true)
	p := plan(t, f, group(t, f, "A", edit(t, f, 0, 1, "A")))
	if !reason(p, "A", "disabled-anchor") {
		t.Fatal(p.Decisions())
	}
	// Blocked enable cannot rescue configured-off permission.
	text = "<!-- mdchk:enable MD001 -->"
	f = setup(t, text, engine.Settings{"MD001": {Enabled: &off, AllowDirectives: &off}}, true)
	g := group(t, f, "A", edit(t, f, len(text), len(text), "!"))
	g.Anchor = span(t, f.parsed.Source, len(text), len(text))
	if len(accepted(plan(t, f, g))) != 0 {
		t.Fatal("blocked enable authorized")
	}
}
func TestProtectedSyntaxAndPreservation(t *testing.T) {
	for _, text := range []string{"`literal`", "```\ninside\n```\n", "<!-- mdchk:wat MD999 -->", "<!-- mdchk:disable MD001 -->", "<script>literal</script>"} {
		for _, global := range []bool{true, false} {
			f := setup(t, text, nil, global)
			at := len(text) / 2
			g := group(t, f, "A", edit(t, f, at, at+1, "X"))
			p := plan(t, f, g)
			if !reason(p, "A", "protected-syntax") {
				t.Fatalf("%q %+v", text, p.Decisions())
			}
		}
	}
	f := setup(t, "abc def", nil, true)
	a := group(t, f, "A", edit(t, f, 0, 1, "A"))
	a.Requirements = append(a.Requirements, fix.Requirement{Kind: fix.ExactBytes, Span: span(t, f.parsed.Source, 4, 7), Expected: []byte("def")})
	b := group(t, f, "B", edit(t, f, 4, 7, "DEF"))
	p := plan(t, f, a, b)
	if !reflect.DeepEqual(accepted(p), []string{"A"}) {
		t.Fatal(p.Decisions())
	}
}
func TestAuthorityLimitsAndCancellation(t *testing.T) {
	f := setup(t, "abc", nil, true)
	g := group(t, f, "A", edit(t, f, 0, 1, "A"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, err := fix.Plan(ctx, f.parsed, f.state, f.registry, []fix.Group{g}, f.budget)
	if !errors.Is(err, context.Canceled) || p.Complete() || p.ProvisionalBytes() != nil {
		t.Fatal(p, err)
	}
	fresh, _ := parser.NewOperationBudget(parser.DefaultLimits())
	if _, err := fix.Plan(context.Background(), f.parsed, f.state, f.registry, []fix.Group{g}, fresh); err == nil {
		t.Fatal("reset budget accepted")
	}
	if _, err := fix.Plan(context.Background(), f.parsed, engine.DirectiveResult{}, f.registry, []fix.Group{g}, f.budget); err == nil {
		t.Fatal("unresolved state")
	}
	if _, err := fix.Plan(nil, f.parsed, f.state, f.registry, nil, f.budget); err == nil { //nolint:staticcheck // SA1012: exercise the required nil-context rejection.
		t.Fatal("nil context")
	}
	limits := parser.DefaultLimits()
	limits.OperationTime = time.Nanosecond
	b, _ := parser.NewOperationBudget(limits)
	_, err = fix.Plan(context.Background(), f.parsed, f.state, f.registry, nil, b)
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) {
		t.Fatal(err)
	}
	if err := f.budget.ChargeWork(parser.DefaultLimits().WorkUnits); err == nil {
		t.Fatal("expected exhaustion")
	}
	p, err = fix.Plan(context.Background(), f.parsed, f.state, f.registry, []fix.Group{g}, f.budget)
	if !errors.As(err, &limit) || p.Complete() {
		t.Fatal(p, err)
	}
}

// Metered Err deterministically interrupts during planner traversal, not via a
// scheduler race or a sleep. It implements an ordinary caller context.
type countingContext struct {
	context.Context
	checks, stop int
}

func (c *countingContext) Err() error {
	c.checks++
	if c.checks >= c.stop {
		return context.Canceled
	}
	return nil
}
func TestInterruptedPlanningPublishesNoPartialCandidate(t *testing.T) {
	for _, stop := range []int{5, 20, 70, 150, 300} {
		f := setup(t, strings.Repeat("a", 200), nil, true)
		gs := make([]fix.Group, 40)
		for i := range gs {
			gs[i] = group(t, f, string(rune('A'+i)), edit(t, f, i*4, i*4+1, "LONG"))
		}
		ctx := &countingContext{Context: context.Background(), stop: stop}
		p, err := fix.Plan(ctx, f.parsed, f.state, f.registry, gs, f.budget)
		if !errors.Is(err, context.Canceled) || p.Complete() || p.ProvisionalBytes() != nil {
			t.Fatalf("stop %d checks %d %v", stop, ctx.checks, err)
		}
	}
}

func TestReusablePlanningFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/planning.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Source, Candidate string
		Edits                   []json.RawMessage
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			f := setup(t, tc.Source, nil, true)
			var edits []fix.Edit
			for _, raw := range tc.Edits {
				var fields []json.RawMessage
				if err := json.Unmarshal(raw, &fields); err != nil {
					t.Fatal(err)
				}
				var a, b int
				var replacement string
				for _, item := range []struct {
					raw   json.RawMessage
					value any
				}{{fields[0], &a}, {fields[1], &b}, {fields[2], &replacement}} {
					if err := json.Unmarshal(item.raw, item.value); err != nil {
						t.Fatal(err)
					}
				}
				edits = append(edits, edit(t, f, a, b, replacement))
			}
			p := plan(t, f, group(t, f, "atomic", edits...))
			if string(p.ProvisionalBytes()) != tc.Candidate {
				t.Fatalf("got %q, decisions %+v", p.ProvisionalBytes(), p.Decisions())
			}
		})
	}
}

func TestDuplicateDifferentRulesMustEachHavePermission(t *testing.T) {
	off := false
	f := setup(t, "abc", engine.Settings{"MD003": {Enabled: &off}}, true)
	a := group(t, f, "A", edit(t, f, 0, 1, "A"))
	b := group(t, f, "B", edit(t, f, 0, 1, "A"))
	b.Rule = "MD003"
	p := plan(t, f, a, b)
	if len(accepted(p)) != 0 || !reason(p, "A", "atomic-dependency") || !reason(p, "B", "disabled-anchor") {
		t.Fatal(p.Decisions())
	}
}
func TestDenseConflictSweepIsBounded(t *testing.T) {
	f := setup(t, "abc", nil, true)
	gs := make([]fix.Group, 3000)
	for i := range gs {
		gs[i] = group(t, f, fmt.Sprintf("group-%04d", i), edit(t, f, 1, 1, fmt.Sprintf("insert-%04d", i)))
	}
	p := plan(t, f, gs...)
	if len(accepted(p)) != 0 || string(p.ProvisionalBytes()) != "abc" {
		t.Fatal("dense conflicts selected a winner")
	}
}
func TestCancellationAtApplicationAndFinalCheckpoint(t *testing.T) {
	f := setup(t, "abcdef", nil, true)
	gs := []fix.Group{group(t, f, "A", edit(t, f, 0, 1, "LONG"), edit(t, f, 5, 6, "!"))}
	measure := &countingContext{Context: context.Background(), stop: 100000}
	p, err := fix.Plan(measure, f.parsed, f.state, f.registry, gs, f.budget)
	if err != nil || !p.Complete() {
		t.Fatal(err)
	}
	for _, stop := range []int{measure.checks - 2, measure.checks - 1, measure.checks} {
		ctx := &countingContext{Context: context.Background(), stop: stop}
		p, err = fix.Plan(ctx, f.parsed, f.state, f.registry, gs, f.budget)
		if !errors.Is(err, context.Canceled) || p.Complete() || p.ProvisionalBytes() != nil {
			t.Fatalf("late stop %d: %v", stop, err)
		}
	}
}
func TestCombinedOutputLimitAndConstructionExhaustion(t *testing.T) {
	f := setup(t, "abc", nil, true)
	p, err := fix.Plan(context.Background(), f.parsed, f.state, f.registry, []fix.Group{group(t, f, "large", edit(t, f, 3, 3, strings.Repeat("x", int(parser.DefaultLimits().DocumentBytes))))}, f.budget)
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || limit.Name != "document-bytes" || p.Complete() {
		t.Fatal(p, err)
	}
	f = setup(t, "abc", nil, true)
	if err := f.budget.ChargeNodes(parser.DefaultLimits().TotalNodes); err == nil {
		t.Fatal("expected sticky construction exhaustion")
	}
	p, err = fix.Plan(context.Background(), f.parsed, f.state, f.registry, nil, f.budget)
	if !errors.As(err, &limit) || limit.Name != "total-nodes" || p.Complete() {
		t.Fatal(p, err)
	}
}

func TestResidualWorkExhaustsDuringPlanning(t *testing.T) {
	f := setup(t, "abcdef", nil, true)
	g := group(t, f, "A", edit(t, f, 0, 1, "LONG"), edit(t, f, 5, 6, "!"))
	_ = plan(t, f, g)
	var measured *parser.LimitExceeded
	if !errors.As(f.budget.ChargeWork(parser.DefaultLimits().WorkUnits), &measured) {
		t.Fatal("measurement")
	}
	consumed := measured.Observed - parser.DefaultLimits().WorkUnits
	limits := parser.DefaultLimits()
	limits.WorkUnits = consumed - 1
	f = setupLimits(t, "abcdef", nil, true, limits)
	g = group(t, f, "A", edit(t, f, 0, 1, "LONG"), edit(t, f, 5, 6, "!"))
	p, err := fix.Plan(context.Background(), f.parsed, f.state, f.registry, []fix.Group{g}, f.budget)
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || limit.Name != "work-units" || p.Complete() || p.ProvisionalBytes() != nil {
		t.Fatal(p, err)
	}
}
func TestInvalidPreservationDoesNotBlockIndependentWork(t *testing.T) {
	f := setup(t, "abc def", nil, true)
	a := group(t, f, "A", edit(t, f, 0, 1, "A"))
	a.Edits[0].Expected = []byte("wrong")
	a.Requirements = append(a.Requirements, fix.Requirement{Kind: fix.ExactBytes, Span: span(t, f.parsed.Source, 4, 7), Expected: []byte("def")})
	b := group(t, f, "B", edit(t, f, 4, 7, "DEF"))
	p := plan(t, f, a, b)
	if !reflect.DeepEqual(accepted(p), []string{"B"}) || string(p.ProvisionalBytes()) != "abc DEF" {
		t.Fatal(p.Decisions())
	}
}

func TestForeignCatalogAndDirectiveOperationCannotAuthorize(t *testing.T) {
	f := setup(t, "abc", nil, true)
	other := setup(t, "abc", nil, true)
	g := group(t, f, "A", edit(t, f, 0, 1, "A"))
	p, err := fix.Plan(context.Background(), f.parsed, f.state, other.registry, []fix.Group{g}, f.budget)
	if err != nil || len(accepted(p)) != 0 || !reason(p, "A", "ineligible-rule") {
		t.Fatal(p, err)
	}
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), f.parsed.Source, b)
	rules, err := f.registry.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	state := engine.ProcessDirectives(context.Background(), parsed, policy{rules, true}, b)
	p, err = fix.Plan(context.Background(), f.parsed, state, f.registry, []fix.Group{g}, f.budget)
	if err == nil || p.Complete() {
		t.Fatal("foreign directive operation accepted")
	}
	// Retained caller policy-map mutation cannot change private catalog authority.
	rules["MD001"] = engine.ResolvedRule{}
	p, err = fix.Plan(context.Background(), parsed, state, f.registry, []fix.Group{g}, b)
	if err != nil || len(accepted(p)) != 1 {
		t.Fatal(p, err)
	}
}

func TestZeroDocumentCannotManufactureAuthority(t *testing.T) {
	f := setup(t, "abc", nil, true)
	g := group(t, f, "A", edit(t, f, 0, 1, "A"))
	f.parsed.Document = &parser.Document{}
	p, err := fix.Plan(context.Background(), f.parsed, f.state, f.registry, []fix.Group{g}, f.budget)
	if err == nil || p.Complete() {
		t.Fatal("zero document accepted")
	}
}
