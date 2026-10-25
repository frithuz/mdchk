package inspect

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func filter(t *testing.T, s *Snapshot, a Analysis, lines string) Analysis {
	t.Helper()
	out, err := Filter(context.Background(), s, a, lines)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestPhysicalLineSelection(t *testing.T) {
	for _, text := range []string{"", "x", "x\n", "x\r\n", "\n", "x\n\n"} {
		t.Run(strings.ReplaceAll(text, "\n", "LF"), func(t *testing.T) {
			s, _ := projectionFixture(t, text, "", nil)
			a := analyze(t, s, false)
			whole := filter(t, s, a, "")
			validateProduced(t, whole)
			if whole.Projection.File.SelectedRegion.ByteRange != (result.ByteRange{Start: 0, End: len(text)}) {
				t.Fatal(whole.Projection.File)
			}
			for n := 1; n <= s.Lines().LineCount(); n++ {
				selected := filter(t, s, a, string(rune('0'+n)))
				region := selected.Projection.File.SelectedRegion
				physical, _ := s.Lines().Line(n)
				if region.ByteRange.Start != physical.Content.Start() || region.ByteRange.End != physical.Ending.End() || region.IncludesEOF != (n == s.Lines().LineCount()) {
					t.Fatal(region)
				}
				validateProduced(t, selected)
			}
			if _, err := ParseLines(s.Lines(), "3"); err == nil {
				t.Fatal("invented terminal line")
			}
		})
	}
	index := parser.NewLineIndex(parser.NewSource([]byte("a\nb\n")))
	for _, bad := range []string{"0", "-1", "1-0", "2-1", "1-3", "3", "1-", "1-2-3", "+1", " 1", "1 ", "1.0", "１", "99999999999999999999999999999999999"} {
		if _, err := ParseLines(index, bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if _, err := ParseLines(index, "1-2"); err != nil {
		t.Fatal(err)
	}
}
func TestFilteredCrossingFindingsEOFAndCallbackOnce(t *testing.T) {
	text := "one\r\ntwo\r\nthree\r\n"
	calls := 0
	s, _ := projectionFixture(t, text, "", []engine.Registration{{ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		calls++
		findings := []engine.Finding{}
		for _, v := range []struct {
			start, end int
			message    string
		}{{0, 8, "crossing"}, {0, 5, "exclusive end"}, {5, 5, "selected anchor"}, {10, 10, "next line anchor"}, {len(text), len(text), "EOF"}} {
			span, _ := c.Source().Span(v.start, v.end)
			findings = append(findings, engine.Finding{Span: span, Message: v.message})
		}
		return engine.RuleOutcome{Findings: findings}
	}}})
	a := analyze(t, s, false)
	out := filter(t, s, a, "2")
	if calls != 1 || len(out.Result.Diagnostics) != 2 || !out.Result.Diagnostics[0].ExtendsRegion {
		t.Fatalf("calls %d findings %+v", calls, out.Result.Diagnostics)
	}
	if !reflect.DeepEqual(rule(t, a, "MD001").SourceCoverage, rule(t, out, "MD001").SourceCoverage) {
		t.Fatal("coverage clipped")
	}
	last := filter(t, s, a, "3")
	if len(last.Result.Diagnostics) != 2 || last.Result.Diagnostics[1].Message != "EOF" {
		t.Fatal(last.Result.Diagnostics)
	}
	if len(a.Result.Diagnostics) != 5 {
		t.Fatal("full analysis mutated")
	}
	validateProduced(t, out)
	validateProduced(t, last)
}
func TestRegionCountsAndSuppressedOriginalOwner(t *testing.T) {
	text := "<!-- mdchk:disable MD001 -->\n<!-- mdchk:disable MD001 -->\nstart\nend\n<!-- mdchk:enable MD003 -->"
	s, _ := projectionFixture(t, text, "rules: {MD003: {enabled: false}}\n", []engine.Registration{{ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		span, _ := c.Source().Span(strings.Index(text, "start"), strings.Index(text, "end")+3)
		return engine.RuleOutcome{Findings: []engine.Finding{{Span: span, Message: "hidden crossing"}}}
	}}})
	a := analyze(t, s, false)
	out := filter(t, s, a, "4")
	if len(out.Result.SuppressedDiagnostics) != 1 {
		t.Fatal(out.Result.SuppressedDiagnostics)
	}
	var hidden result.SuppressedDiagnostic
	_ = json.Unmarshal(out.Result.SuppressedDiagnostics[0], &hidden)
	if hidden.SuppressedBy.DirectiveID != "directive-2" || !hidden.Diagnostic.ExtendsRegion {
		t.Fatal(hidden)
	}
	if *rule(t, out, "MD001").SelectedEnabled || *rule(t, out, "MD003").SelectedEnabled {
		t.Fatal("disabled rules counted active")
	}
	eof := filter(t, s, a, "5")
	if !*rule(t, eof, "MD003").SelectedEnabled {
		t.Fatal("EOF-only enablement omitted")
	}
	whole := filter(t, s, a, "")
	if !*rule(t, whole, "MD001").SelectedEnabled {
		t.Fatal("partial rule inactive")
	}
	if (*out.Projection.Directives.Data)[1].ID != "directive-2" || !(*out.Projection.Directives.Data)[1].ContextOnly {
		t.Fatal("context owner lost")
	}
	for _, v := range []Analysis{out, eof, whole} {
		var count struct{ Known, Active, Inactive int }
		_ = json.Unmarshal(*v.Projection.RuleCounts.Data, &count)
		if count.Known != 87 || count.Active+count.Inactive != 87 {
			t.Fatal(count)
		}
		validateProduced(t, v)
	}
}
func TestFilteredASTSourceFactsAndOwnedViews(t *testing.T) {
	text := "> - first\n>   **second** [x][r]\n\n[r]: /target\n\nother\n"
	s, _ := projectionFixture(t, text, "", nil)
	a := analyze(t, s, false)
	out := filter(t, s, a, "2")
	root := out.Projection.AST.Data.Root
	if root.ByteRange != (result.ByteRange{Start: 0, End: len(text)}) || !root.ContextOnly || len(root.Children) != 1 || !root.ExtendsRegion {
		t.Fatal(root)
	}
	if len(out.Projection.AST.Data.References) != 1 || !out.Projection.AST.Data.References[0].ContextOnly {
		t.Fatal("reference context lost")
	}
	original, _ := json.Marshal(a)
	out.Projection.AST.Data.Root.Children[0].Properties["mutation"] = []any{"bad"}
	(*out.Projection.Rules.Data)[0].Options["mutation"] = []any{"bad"}
	(*out.Projection.Rules.Data)[0].Intervals[0].ByteRange.Start = 999
	if len(*out.Projection.Directives.Data) > 0 {
		(*out.Projection.Directives.Data)[0].Effects = nil
	}
	after, _ := json.Marshal(a)
	if string(original) != string(after) {
		t.Fatal("filtered values alias full analysis")
	}
	// Mutation of the public full result also cannot alter the privately retained
	// original analysis authority used by later filters.
	a.Projection.AST.Data.Root.Children = nil
	again := filter(t, s, a, "2")
	if len(again.Projection.AST.Data.Root.Children) != 1 {
		t.Fatal("public mutation changed authority")
	}
	validateProduced(t, again)
}
func TestFilterSharedResidualCancellationAndConcurrency(t *testing.T) {
	s, _ := projectionFixture(t, "one\ntwo\n", "", nil)
	a := analyze(t, s, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := Filter(ctx, s, a, "1")
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, a) {
		t.Fatal("cancellation lost full analysis", err)
	}
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			out, err := Filter(context.Background(), s, a, "2")
			if err != nil || out.Projection.File.SelectedLines.Start != 2 {
				t.Errorf("concurrent filter: %v", err)
			}
		})
	}
	wg.Wait()
	_ = s.budget.ChargeWork(s.budget.Limits().WorkUnits)
	got, err = Filter(context.Background(), s, a, "1")
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || !reflect.DeepEqual(got, a) {
		t.Fatal("residual exhaustion replaced original", err)
	}
}
func TestCountsUnavailableWithoutSharedDirectiveState(t *testing.T) {
	for _, definition := range []string{"flavor: gfm\n", "rules: {MD001: {enabled: maybe}}\n"} {
		// Duplicate flavor intentionally tests an invalid configuration; an admitted
		// but unavailable parser is tested independently below.
		s, _ := projectionFixture(t, "<!-- mdchk:disable MD001 -->\n", definition, nil)
		a := analyze(t, s, false)
		out := filter(t, s, a, "1")
		if out.Projection.RuleCounts.Availability != "unavailable" || out.Projection.RuleCounts.Data != nil {
			t.Fatal(out.Projection.RuleCounts)
		}
		validateProduced(t, out)
	}
}

func TestFilterScalesOnDefaultResidualBudget(t *testing.T) {
	s, _ := projectionFixture(t, strings.Repeat("x\n\n", 8000), "", nil)
	a := analyze(t, s, false)
	out := filter(t, s, a, "12001")
	if len(out.Projection.AST.Data.Root.Children) != 1 || out.Projection.RuleCounts.Availability != "available" {
		t.Fatal("bounded full analysis did not produce selected view")
	}
	t.Logf("source=%d bytes, authority=%d bytes, copied structures=%d", s.source.Len(), len(a.wire), a.filterNodes)
}

func TestFilteredBlockedLiteralMultiRuleAndNotes(t *testing.T) {
	text := "<!-- mdchk:disable MD001 MD003 -->\n<!-- mdchk:disable MD999 -->\n`<!-- mdchk:disable MD001 -->`\nselected\n"
	s, _ := projectionFixture(t, text, "rules: {MD001: {allow_directives: false}}\n", []engine.Registration{{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { return engine.RuleOutcome{} }}})
	a := analyze(t, s, false)
	out := filter(t, s, a, "4")
	if !*rule(t, out, "MD001").SelectedEnabled || *rule(t, out, "MD003").SelectedEnabled {
		t.Fatal("blocked request changed selected state")
	}
	if len(*out.Projection.Directives.Data) != 2 || len(*out.Projection.RawCandidates.Data) != 3 || (*out.Projection.RawCandidates.Data)[2].Context != "literal" {
		t.Fatal("literal became effective")
	}
	if len(out.Result.Notes) == 0 {
		t.Fatal("whole-document explanatory notes lost")
	}
	for _, raw := range out.Result.Notes {
		var note result.Note
		_ = json.Unmarshal(raw, &note)
		if note.Relevance != "context" {
			t.Fatal(note)
		}
	}
	validateProduced(t, out)
}
func TestEmptyWholeSourceAndUnavailableImplementationsCount(t *testing.T) {
	s, _ := projectionFixture(t, "", "", nil)
	a := filter(t, s, analyze(t, s, false), "")
	known, active := 0, 0
	for _, r := range *a.Projection.Rules.Data {
		known++
		if *r.SelectedEnabled {
			active++
		}
		if r.ConfiguredEnabled != *r.SelectedEnabled {
			t.Fatal("empty EOF did not retain configured state", r.Rule)
		}
	}
	var counts struct{ Known, Active, Inactive int }
	_ = json.Unmarshal(*a.Projection.RuleCounts.Data, &counts)
	if counts.Known != known || counts.Active != active || active == 0 {
		t.Fatal(counts)
	}
	validateProduced(t, a)
}
func TestFilterUnavailableParserAndPartialExecution(t *testing.T) {
	root, o := fixture(t)
	write(t, root+"/x.md", []byte("x\n"))
	write(t, root+"/.mdchk.yaml", []byte("version: 1\nflavor: gfm\n"))
	s := acquire(t, "x.md", o)
	out := filter(t, s, analyze(t, s, false), "1")
	if out.Projection.Configuration.Availability != "available" || out.Projection.RuleCounts.Availability != "unavailable" || out.Projection.RuleCounts.Data != nil {
		t.Fatal("unavailable parser yielded counts")
	}
	validateProduced(t, out)
	s, _ = projectionFixture(t, "one\ntwo\n", "", []engine.Registration{{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome {
		return engine.RuleOutcome{IncompleteReason: "missing context"}
	}}})
	out = filter(t, s, analyze(t, s, false), "2")
	if out.Result.Complete || out.Projection.RuleCounts.Availability != "available" || rule(t, out, "MD001").ActiveEvaluation != "partial" || !*rule(t, out, "MD001").SelectedEnabled {
		t.Fatal("state and execution conflated")
	}
	validateProduced(t, out)
}

// Err cancels at a deterministic checkpoint after initial copy reservations.
type filterCancelContext struct {
	context.Context
	remaining int
	cancel    context.CancelFunc
}

func (c *filterCancelContext) Err() error {
	c.remaining--
	if c.remaining == 0 {
		c.cancel()
	}
	return c.Context.Err()
}
func TestFilterCancellationDuringTraversalRetainsFullAnalysis(t *testing.T) {
	s, _ := projectionFixture(t, "> one\n> two\n", "", nil)
	a := analyze(t, s, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checkpoints := &filterCancelContext{Context: ctx, remaining: 4, cancel: cancel}
	out, err := Filter(checkpoints, s, a, "2")
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(out, a) {
		t.Fatal("partial selected view escaped on traversal cancellation", err)
	}
}

func TestFilteredAllRulesConfiguredOff(t *testing.T) {
	var definition strings.Builder
	definition.WriteString("rules:\n")
	for _, r := range engine.Catalog() {
		definition.WriteString("  " + r.ID + ": {enabled: false}\n")
	}
	s, _ := projectionFixture(t, "one\ntwo\n", definition.String(), nil)
	out := filter(t, s, analyze(t, s, false), "2")
	var counts struct{ Known, Active, Inactive int }
	_ = json.Unmarshal(*out.Projection.RuleCounts.Data, &counts)
	if counts.Known != 87 || counts.Active != 0 || counts.Inactive != 87 {
		t.Fatal(counts)
	}
	if rule(t, out, "MD094").ActiveEvaluation != "complete" {
		t.Fatal("encoding gate execution lost when counts inactive")
	}
	validateProduced(t, out)
}

func TestFilteredProjectionPreservesCanonicalGoValues(t *testing.T) {
	s, _ := projectionFixture(t, "# Heading\n\n[x][r]\n\n[r]: /target\n\n<div>html</div>\n", "", nil)
	full := analyze(t, s, false)
	out := filter(t, s, full, "")
	if !reflect.DeepEqual(full.Projection.AST.Data, out.Projection.AST.Data) {
		t.Fatal("AST source properties changed types or values")
	}
	if !reflect.DeepEqual(full.Projection.Configuration.Data, out.Projection.Configuration.Data) {
		t.Fatal("configuration values changed types or values")
	}
	for i, r := range *full.Projection.Rules.Data {
		if !reflect.DeepEqual(r.Options, (*out.Projection.Rules.Data)[i].Options) {
			t.Fatal("canonical option type lost", r.Rule)
		}
	}
}
