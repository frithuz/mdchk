package inspect

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func projectionFixture(t *testing.T, text, definition string, checks []engine.Registration) (*Snapshot, Options) {
	t.Helper()
	root, o := fixture(t)
	r, err := engine.NewCatalogRegistry(checks)
	if err != nil {
		t.Fatal(err)
	}
	o.Registry = r
	write(t, filepath.Join(root, "x.md"), []byte(text))
	write(t, filepath.Join(root, config.Filename), []byte("version: 1\nflavor: commonmark\n"+definition))
	return acquire(t, "x.md", o), o
}
func analyze(t *testing.T, s *Snapshot, observe bool) Analysis {
	t.Helper()
	a, err := Analyze(context.Background(), s, AnalyzeOptions{ObserveInactive: observe})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func rule(t *testing.T, a Analysis, id string) Rule {
	t.Helper()
	if a.Projection.Rules.Data == nil {
		t.Fatal("rules unavailable")
	}
	for _, r := range *a.Projection.Rules.Data {
		if r.Rule == id {
			return r
		}
	}
	t.Fatal("missing rule", id)
	return Rule{}
}
func TestActualCleanExecutionAndConfiguredOffObservation(t *testing.T) {
	for _, observe := range []bool{false, true} {
		t.Run(strconvBool(observe), func(t *testing.T) {
			calls, offCalls := 0, 0
			s, _ := projectionFixture(t, "clean\n", "rules: {MD001: {enabled: true}, MD003: {enabled: false}}\n", []engine.Registration{{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { calls++; return engine.RuleOutcome{} }}, {ID: "MD003", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
				offCalls++
				span, _ := c.Source().Span(0, 0)
				return engine.RuleOutcome{Findings: []engine.Finding{{Span: span, Message: "observed"}}}
			}}})
			a := analyze(t, s, observe)
			r := rule(t, a, "MD001")
			if calls != 1 || r.ActiveEvaluation != "complete" || r.SourceCoverage.Active.Availability != "complete" || len(r.SourceCoverage.Active.ByteRanges) != 1 || len(a.Result.Diagnostics) != 0 {
				t.Fatalf("clean callback: calls %d rule %+v result %+v", calls, r, a.Result)
			}
			off := rule(t, a, "MD003")
			if off.ActiveEvaluation != "skipped" || off.EffectiveState != "inactive" {
				t.Fatal(off)
			}
			if observe {
				if offCalls != 1 || off.ObservationalEvaluation != "complete" || len(a.Result.SuppressedDiagnostics) != 1 {
					t.Fatalf("off calls %d rule %+v", offCalls, off)
				}
				var hidden result.SuppressedDiagnostic
				_ = json.Unmarshal(a.Result.SuppressedDiagnostics[0], &hidden)
				if hidden.SuppressedBy.Kind != "configuration" || hidden.SuppressedBy.DirectiveID != "" {
					t.Fatal(hidden)
				}
			} else if offCalls != 0 || off.ObservationalEvaluation != "not-requested" || len(a.Result.SuppressedDiagnostics) != 0 {
				t.Fatal("observation ran when disabled")
			}
		})
	}
}
func strconvBool(b bool) string {
	if b {
		return "observe"
	}
	return "active-only"
}
func TestSharedSuppressionParityAndLastOwner(t *testing.T) {
	text := "start\n<!-- mdchk:disable MD001 -->\n<!-- mdchk:disable MD001 -->\nend\n<!-- mdchk:enable MD003 -->"
	checks := []engine.Registration{{ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		first, _ := c.Source().Span(0, 0)
		last, _ := c.Source().Span(strings.Index(text, "end"), strings.Index(text, "end")+3)
		return engine.RuleOutcome{Findings: []engine.Finding{{Span: first, Message: "active"}, {Span: last, Message: "hidden"}}}
	}}, {ID: "MD003", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		eof, _ := c.Source().Span(c.Source().Len(), c.Source().Len())
		return engine.RuleOutcome{Findings: []engine.Finding{{Span: eof, Message: "EOF enabled"}}}
	}}}
	s, _ := projectionFixture(t, text, "rules: {MD003: {enabled: false}}\n", checks)
	a := analyze(t, s, false)
	input, err := s.plan.NewSourceInput(s.file.Path, s.source)
	if err != nil {
		t.Fatal(err)
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	lint, err := s.plan.Lint(context.Background(), []engine.Input{input}, engine.RunOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Result.Diagnostics, lint.Diagnostics) || len(a.Result.Diagnostics) != 2 || len(a.Result.SuppressedDiagnostics) != 1 {
		t.Fatalf("parity %v %v", a.Result.Diagnostics, lint.Diagnostics)
	}
	var hidden result.SuppressedDiagnostic
	_ = json.Unmarshal(a.Result.SuppressedDiagnostics[0], &hidden)
	if hidden.SuppressedBy.DirectiveID != "directive-2" {
		t.Fatal(hidden)
	}
	r := rule(t, a, "MD001")
	if r.Intervals[len(r.Intervals)-1].DirectiveID != "directive-2" || r.Intervals[len(r.Intervals)-1].Enabled {
		t.Fatal(r)
	}
	eof := rule(t, a, "MD003")
	last := eof.Intervals[len(eof.Intervals)-1]
	if last.ByteRange.Start != len(text) || last.ByteRange.End != len(text) || !last.Enabled || eof.ActiveEvaluation != "complete" {
		t.Fatal(eof)
	}
	validateProduced(t, a)
}
func TestBlockedInvalidProtectedAndSpecialDirectives(t *testing.T) {
	text := "<!-- mdchk:disable MD001 MD003 MD074 MD094 -->\n<!-- mdchk:disable MD999 -->\n`<!-- mdchk:disable MD001 -->`\n\n<script>\n<!-- mdchk:disable MD001 -->\n</script>\n"
	s, _ := projectionFixture(t, text, "rules: {MD001: {allow_directives: false}}\n", []engine.Registration{{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { return engine.RuleOutcome{} }}})
	a := analyze(t, s, false)
	d := *a.Projection.Directives.Data
	if len(d) != 2 || d[0].State != "mixed" || d[1].State != "invalid" {
		t.Fatalf("directives %+v", d)
	}
	effects := map[string]DirectiveEffect{}
	for _, e := range d[0].Effects {
		effects[e.Rule] = e
	}
	if effects["MD001"].Effect != "blocked" || len(effects["MD001"].Provenance) == 0 || effects["MD003"].Effect != "disable" || effects["MD074"].Effect != "inapplicable" || effects["MD094"].Effect != "inapplicable" {
		t.Fatal(effects)
	}
	candidates := *a.Projection.RawCandidates.Data
	if len(candidates) != 4 || candidates[2].Context != "literal" || candidates[3].Context != "literal" {
		t.Fatal(candidates)
	}
	for _, in := range rule(t, a, "MD001").Intervals {
		if in.Origin == "directive" {
			t.Fatal("blocked request owns state")
		}
	}
	validateProduced(t, a)
}
func TestProjectionSourceFactsAndImmutableSnapshot(t *testing.T) {
	text := "# 雪 &amp; \\*\r\n\r\n> - **bold** [x][r]\r\n\r\n[r]: /dest \"title\"\r\n\r\n``` go\r\nraw\tcode\r\n```\r\n\r\n<div>html</div>\r\n"
	s, _ := projectionFixture(t, text, "", nil)
	write(t, s.file.Path, []byte("changed"))
	a := analyze(t, s, false)
	ast := a.Projection.AST.Data
	if ast == nil || ast.Root.Kind != "document" || ast.Root.ByteRange.End != len(text) || ast.Snapshot.SHA256 != s.file.SHA256 || ast.Profile != "commonmark" {
		t.Fatal("wrong AST", ast)
	}
	kinds := map[string]int{}
	var walk func(Node)
	next := 1
	walk = func(n Node) {
		if n.ID != "node-"+strconv.Itoa(next) {
			t.Fatal(n.ID, next)
		}
		next++
		kinds[n.Kind]++
		if n.ContextOnly {
			t.Fatal("full projection context node")
		}
		span, _ := s.source.Span(n.ByteRange.Start, n.ByteRange.End)
		if _, err := s.source.Text(span); err != nil {
			t.Fatal(err)
		}
		start, _ := s.lines.Position(n.ByteRange.Start)
		end, _ := s.lines.Position(n.ByteRange.End)
		if n.Range.Start != start || n.Range.End != end {
			t.Fatal("wrong coordinates", n)
		}
		last := -1
		for _, c := range n.Children {
			if c.ByteRange.Start < last || c.ByteRange.Start < n.ByteRange.Start || c.ByteRange.End > n.ByteRange.End {
				t.Fatal("child order/envelope")
			}
			last = c.ByteRange.Start
			walk(c)
		}
	}
	walk(ast.Root)
	for _, kind := range []string{"heading", "text", "block-quote", "list", "list-item", "strong", "link", "code-block", "html-block"} {
		if kinds[kind] == 0 {
			t.Fatal("missing actual kind", kind, kinds)
		}
	}
	if len(ast.Root.Children[0].Children) == 0 || len(ast.References) != 1 || !ast.References[0].Effective || ast.References[0].Destination.Semantic == nil || *ast.References[0].Destination.Semantic != "/dest" {
		t.Fatal("lost inline/reference facts")
	}
	children := ast.Root.Children
	children[0].Properties["level"] = 99
	children[0].Children = nil
	a.Projection.Rules.Data = nil
	again := analyze(t, s, false)
	if again.Projection.AST.Data.Root.Children[0].Properties["level"] != 1 || len(again.Projection.AST.Data.Root.Children[0].Children) == 0 {
		t.Fatal("projection mutated retained snapshot")
	}
	validateProduced(t, again)
}
func TestInvalidConfigurationFlavorEncodingAndLiteralReplacement(t *testing.T) {
	for _, definition := range []string{"version: [", "version: 1\nflavor: gfm\n", "version: 1\nflavor: commonmark\n"} {
		t.Run(definition, func(t *testing.T) {
			root, o := fixture(t)
			text := []byte("<!-- mdchk:disable MD001 -->\n")
			write(t, filepath.Join(root, "x.md"), text)
			write(t, filepath.Join(root, config.Filename), []byte(definition))
			s := acquire(t, "x.md", o)
			a := analyze(t, s, false)
			if strings.Contains(definition, "commonmark") {
				if a.Projection.AST.Availability != "available" {
					t.Fatal(a.Projection)
				}
			} else {
				if a.Projection.AST.Data != nil || a.Projection.Rules.Data != nil || a.Projection.Directives.Data != nil {
					t.Fatal("fabricated parsing")
				}
				if len(*a.Projection.RawCandidates.Data) != 1 {
					t.Fatal("lost raw candidates")
				}
				if strings.Contains(definition, "gfm") && a.Projection.Flavor.Data == nil {
					t.Fatal("lost known flavor")
				}
			}
			validateProduced(t, a)
		})
	}
	for _, data := range [][]byte{[]byte("\xef\xbf\xbd\n"), append([]byte("<!-- mdchk:disable MD001 -->\n"), 255)} {
		s, _ := projectionFixture(t, string(data), "", nil)
		a := analyze(t, s, false)
		if bytes.Contains(data, []byte{255}) {
			if a.Projection.AST.Data != nil || len(a.Result.Errors) != 1 || a.Result.Errors[0].ByteOffset == nil || *a.Result.Errors[0].ByteOffset != len(data)-1 {
				t.Fatal("invalid UTF8 parsed", a.Result)
			}
			raw, _ := base64.StdEncoding.DecodeString((*a.Projection.RawCandidates.Data)[0].RawBase64)
			if !bytes.Equal(raw, data[:len(data)-2]) {
				t.Fatal("raw identity lost", raw)
			}
		} else if a.Projection.AST.Data == nil {
			t.Fatal("literal replacement rejected")
		}
		validateProduced(t, a)
	}
}
func TestSharedResidualBudgetAndCancellation(t *testing.T) {
	s, _ := projectionFixture(t, "source\n", "", nil)
	_ = s.budget.ChargeWork(s.budget.Limits().WorkUnits)
	a, err := Analyze(context.Background(), s, AnalyzeOptions{})
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || a.Result.Status != "incomplete" || len(a.Result.Interruptions) == 0 || a.Projection.AST.Data != nil {
		t.Fatalf("budget reset: %v %+v", err, a)
	}
	s, _ = projectionFixture(t, "source\n", "", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a, err = Analyze(ctx, s, AnalyzeOptions{})
	if !errors.Is(err, context.Canceled) || a.Result.Status != "incomplete" || a.Projection.AST.Data != nil {
		t.Fatal("cancellation lost")
	}
	validateProduced(t, a)
}
func TestConcurrentIndependentProjectionViews(t *testing.T) {
	s, _ := projectionFixture(t, "# title\n", "", nil)
	var g sync.WaitGroup
	for range 6 {
		g.Go(func() {
			a, err := Analyze(context.Background(), s, AnalyzeOptions{})
			if err != nil {
				t.Error(err)
				return
			}
			a.Projection.AST.Data.Root.Children[0].Properties["level"] = 9
			(*a.Projection.Rules.Data)[0].Options["changed"] = true
		})
	}
	g.Wait()
	if analyze(t, s, false).Projection.AST.Data.Root.Children[0].Properties["level"] != 1 {
		t.Fatal("concurrent mutation")
	}
}
func validateProduced(t *testing.T, a Analysis) {
	t.Helper()
	python := os.Getenv("MDCHK_RESULT_VALIDATOR_PYTHON")
	if python == "" {
		t.Log("producer schema validation requires MDCHK_RESULT_VALIDATOR_PYTHON")
		return
	}
	data, err := json.Marshal(a.Result)
	if err != nil {
		t.Fatal(err)
	}
	script := `import importlib.util,json,sys
from pathlib import Path
p=Path('../../docs/contracts/results/validate.py')
s=importlib.util.spec_from_file_location('contract',p);m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
x=json.load(sys.stdin);m.validator.validate(x);m.semantic(x)
import copy
rules=x['inspection']['rules'].get('data',[])
if rules:
 bad=copy.deepcopy(x);bad['inspection']['rules']['data'][0]['source_coverage']['active']={'availability':'complete','ranges':[],'byte_ranges':[{'start':0,'end':x['inspection']['file']['size_bytes']+1}]}
 try:m.validator.validate(bad);m.semantic(bad)
 except Exception:pass
 else:raise AssertionError('invalid source coverage accepted')
if x['inspection']['file'].get('selected_region'):
 if x['inspection']['rule_counts']['availability']=='available':
  bad=copy.deepcopy(x);bad['inspection']['rule_counts']['data']['active']+=1;bad['inspection']['rule_counts']['data']['inactive']-=1
  try:m.validator.validate(bad);m.semantic(bad)
  except Exception:pass
  else:raise AssertionError('incorrect selected counts accepted')
 if 'selected_lines' in x['inspection']['file']:
  bad=copy.deepcopy(x);bad['inspection']['file']['selected_lines']['end']=bad['inspection']['file']['physical_line_count']+1
  try:m.validator.validate(bad);m.semantic(bad)
  except Exception:pass
  else:raise AssertionError('nonphysical selected line accepted')
for hidden in x['suppressed_diagnostics']:
 if hidden['suppressed_by']['kind']=='directive':
  bad=copy.deepcopy(x);bad['suppressed_diagnostics'][0]['suppressed_by']['directive_id']='dangling'
  try:m.validator.validate(bad);m.semantic(bad)
  except Exception:pass
  else:raise AssertionError('dangling suppression accepted')
  break
`
	//nolint:gosec // G204: explicitly selected test interpreter; fixed local validator code.
	cmd := exec.Command(python, "-B", "-c", script)
	cmd.Stdin = bytes.NewReader(data)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("actual producer contract: %v\n%s\n%s", err, out, data)
	}
}
func TestContractFixtureRuleAndDirectiveShapes(t *testing.T) {
	s, _ := projectionFixture(t, "<!-- mdchk:disable MD001 -->\nnext\n", "", []engine.Registration{{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { return engine.RuleOutcome{} }}})
	a := analyze(t, s, false)
	for _, pair := range []struct {
		file   string
		actual any
	}{{"rule-inspection.json", rule(t, a, "MD001")}, {"directive-inspection.json", (*a.Projection.Directives.Data)[0]}} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "contracts", "results", pair.file))
		if err != nil {
			t.Fatal(err)
		}
		fixture := map[string]json.RawMessage{}
		actual := map[string]json.RawMessage{}
		_ = json.Unmarshal(raw, &fixture)
		encoded, _ := json.Marshal(pair.actual)
		_ = json.Unmarshal(encoded, &actual)
		for key := range fixture {
			if _, ok := actual[key]; !ok {
				t.Fatal("missing contract field", key)
			}
		}
		for key := range fixture {
			// The rule fixture's empty interval list is an illustrative shape. The
			// actual full-source configuration/directive intervals are checked above.
			if pair.file == "rule-inspection.json" && key == "intervals" {
				continue
			}
			var fv, av any
			_ = json.Unmarshal(fixture[key], &fv)
			_ = json.Unmarshal(actual[key], &av)
			if !reflect.DeepEqual(fv, av) {
				t.Fatalf("fixture %s field %s: %s %s", pair.file, key, fixture[key], actual[key])
			}
		}
	}
	validateProduced(t, a)
}

func TestIncompleteObservationHasUnknownCoverage(t *testing.T) {
	s, _ := projectionFixture(t, "source\n", "rules: {MD001: {enabled: false}}\n", []engine.Registration{{ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		span, _ := c.Source().Span(0, 0)
		return engine.RuleOutcome{Findings: []engine.Finding{{Span: span, Message: "reliable partial"}}, IncompleteReason: "fixture context unavailable"}
	}}})
	a := analyze(t, s, true)
	r := rule(t, a, "MD001")
	if r.ObservationalEvaluation != "partial" || r.SourceCoverage.Observational.Availability != "unknown" || len(r.SourceCoverage.Observational.ByteRanges) != 0 || a.Result.Complete || len(a.Result.SuppressedDiagnostics) != 1 {
		t.Fatal("partial callback overstated coverage", r)
	}
	validateProduced(t, a)
}
func TestObservedBudgetStopRetainsCompletedActiveFinding(t *testing.T) {
	s, _ := projectionFixture(t, "source\n", "rules: {MD003: {enabled: false}}\n", []engine.Registration{{ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		span, _ := c.Source().Span(0, 0)
		return engine.RuleOutcome{Findings: []engine.Finding{{Span: span, Message: "completed active observation"}}}
	}}, {ID: "MD003", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
		return engine.RuleOutcome{Err: c.Checkpoint(ctx, 1<<50)}
	}}})
	a, err := Analyze(context.Background(), s, AnalyzeOptions{ObserveInactive: true})
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || len(a.Result.Diagnostics) != 1 || a.Result.Diagnostics[0].Message != "completed active observation" || a.Result.Status != "incomplete" || len(a.Result.Interruptions) == 0 {
		t.Fatalf("lost completed observation: %v, diagnostics %v", err, a.Result.Diagnostics)
	}
	validateProduced(t, a)
}
func TestInheritedDirectivePermissionOrigins(t *testing.T) {
	root, o := fixture(t)
	r, err := engine.NewCatalogRegistry([]engine.Registration{{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { return engine.RuleOutcome{} }}})
	if err != nil {
		t.Fatal(err)
	}
	o.Registry = r
	write(t, filepath.Join(root, config.Filename), []byte("version: 1\nrules: {MD001: {allow_directives: false}}\n"))
	write(t, filepath.Join(root, "nested", config.Filename), []byte("version: 1\nflavor: commonmark\nrules: {MD001: {allow_directives: false}}\n"))
	write(t, filepath.Join(root, "nested", "x.md"), []byte("<!-- mdchk:disable MD001 -->"))
	s := acquire(t, "nested/x.md", o)
	a := analyze(t, s, false)
	origins := (*a.Projection.Directives.Data)[0].Effects[0].Provenance
	if len(origins) != 2 || origins[0].Path == origins[1].Path {
		t.Fatal("lost permission origins", origins)
	}
	inherited := 0
	for _, p := range origins {
		if p.Inherited {
			inherited++
		}
	}
	if inherited != 1 {
		t.Fatal("wrong inherited attribution", origins)
	}
	validateProduced(t, a)
}

func TestEmptyAndCompleteCommonMarkKindCatalog(t *testing.T) {
	s, _ := projectionFixture(t, "", "", nil)
	empty := analyze(t, s, false)
	if empty.Projection.AST.Data == nil || len(empty.Projection.AST.Data.Root.Children) != 0 || empty.Projection.AST.Data.Root.ByteRange != (result.ByteRange{}) {
		t.Fatal("empty root unavailable")
	}
	validateProduced(t, empty)
	text := "paragraph\nsoft  \nhard\\\nbreak *em* **strong** `code` [link](/url) ![image](/img) <https://example.org> <b>inline</b>\n\n---\n\n> quote\n\n1. ordered\n\n    indented\n\n~~~ info\nfenced\n~~~\n\n# heading\n\n<div>block</div>\n"
	s, _ = projectionFixture(t, text, "", nil)
	a := analyze(t, s, false)
	found := map[string]bool{}
	parser.Walk(parser.Parse(context.Background(), s.source).Document.Root(), func(n *parser.Node) bool { found[string(n.Kind())] = true; return true })
	actual := map[string]bool{}
	var walk func(Node)
	walk = func(n Node) {
		actual[n.Kind] = true
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(a.Projection.AST.Data.Root)
	if !reflect.DeepEqual(found, actual) {
		t.Fatal("projection dropped parser kinds", found, actual)
	}
	for _, kind := range []string{"document", "paragraph", "soft-break", "hard-break", "emphasis", "strong", "code-span", "link", "image", "inline-html", "thematic-break", "block-quote", "list", "list-item", "code-block", "heading", "html-block", "text"} {
		if !actual[kind] {
			t.Fatal("missing representative kind", kind)
		}
	}
	validateProduced(t, a)
}
func TestRawInventoryDoesNotHideLaterNestedSpelling(t *testing.T) {
	text := "```\n<!-- unfinished\n```\n\n<!-- mdchk:disable MD001 -->\n"
	s, _ := projectionFixture(t, text, "", nil)
	a := analyze(t, s, false)
	raw := *a.Projection.RawCandidates.Data
	if len(raw) != 1 || raw[0].Context != "confirmed" || raw[0].ByteRange.Start != strings.Index(text, "<!-- mdchk") {
		t.Fatal("raw opener masked confirmed directive", raw)
	}
	validateProduced(t, a)
}

func TestParseLimitAndCallbackCancellationRetainTruthfulSections(t *testing.T) {
	root, o := fixture(t)
	write(t, filepath.Join(root, "x.md"), []byte("> > > deeply nested\n"))
	write(t, filepath.Join(root, config.Filename), []byte("version: 1\nflavor: commonmark\n"))
	limits := parser.DefaultLimits()
	limits.SyntaxDepth = 1
	o.Budget, _ = parser.NewOperationBudget(limits)
	s := acquire(t, "x.md", o)
	a, err := Analyze(context.Background(), s, AnalyzeOptions{})
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || a.Projection.AST.Data != nil || len(a.Result.Interruptions) != 1 || a.Projection.RawCandidates.Data == nil {
		t.Fatalf("parse limit %v %+v", err, a.Projection.AST)
	}
	validateProduced(t, a)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, _ = projectionFixture(t, "original\n", "", []engine.Registration{{ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		cancel()
		span, _ := c.Source().Span(0, 0)
		return engine.RuleOutcome{Findings: []engine.Finding{{Span: span, Message: "completed fact before cancellation"}}}
	}}})
	a, err = Analyze(ctx, s, AnalyzeOptions{})
	if !errors.Is(err, context.Canceled) || a.Projection.AST.Data == nil || len(a.Result.Diagnostics) != 1 || rule(t, a, "MD001").ActiveEvaluation != "partial" || rule(t, a, "MD001").SourceCoverage.Active.Availability != "unknown" {
		t.Fatalf("callback cancellation: %v %+v", err, a.Projection.Rules)
	}
	validateProduced(t, a)
}
func TestInactiveMD087IsNeverRecursivelyObserved(t *testing.T) {
	calls := 0
	s, _ := projectionFixture(t, "source", "rules: {MD087: {enabled: false}}\n", []engine.Registration{{ID: "MD087", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { calls++; return engine.RuleOutcome{} }}})
	a := analyze(t, s, true)
	if calls != 0 || rule(t, a, "MD087").ObservationalEvaluation != "not-requested" {
		t.Fatal("recursive unused-suppression observation")
	}
}

func TestObservationalCancellationDoesNotProveFullCoverage(t *testing.T) {
	for _, finding := range []bool{false, true} {
		t.Run(strconvBool(finding), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s, _ := projectionFixture(t, "original\n", "rules: {MD001: {enabled: false}}\n", []engine.Registration{{ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
				cancel()
				out := engine.RuleOutcome{}
				if finding {
					span, _ := c.Source().Span(0, 0)
					out.Findings = []engine.Finding{{Span: span, Message: "reliable observation"}}
				}
				return out
			}}})
			a, err := Analyze(ctx, s, AnalyzeOptions{ObserveInactive: true})
			r := rule(t, a, "MD001")
			if !errors.Is(err, context.Canceled) || r.ObservationalEvaluation != "partial" || r.SourceCoverage.Observational.Availability != "unknown" || len(r.SourceCoverage.Observational.ByteRanges) != 0 {
				t.Fatalf("cancelled observation overstated coverage: %v %+v", err, r)
			}
			if finding && len(a.Result.SuppressedDiagnostics) != 1 {
				t.Fatal("lost reliable partial finding")
			}
			validateProduced(t, a)
		})
	}
}
func TestProjectionScalesWithinDefaultSharedBudget(t *testing.T) {
	for _, paragraphs := range []int{1000, 2000, 4000, 8000} {
		t.Run(strconv.Itoa(paragraphs), func(t *testing.T) {
			s, _ := projectionFixture(t, strings.Repeat("x\n\n", paragraphs), "", nil)
			a := analyze(t, s, false)
			if a.Projection.AST.Availability != "available" || len(a.Projection.AST.Data.Root.Children) != paragraphs || len(a.Result.Interruptions) != 0 {
				t.Fatalf("projection did not finish %d paragraphs", paragraphs)
			}
		})
	}
}
func TestInspectionLimitUnitsMatchTypedCause(t *testing.T) {
	for _, kind := range []string{"total-nodes", "operation-time"} {
		t.Run(kind, func(t *testing.T) {
			root, o := fixture(t)
			write(t, filepath.Join(root, "x.md"), []byte("source\n"))
			write(t, filepath.Join(root, config.Filename), []byte("version: 1\nflavor: commonmark\n"))
			limits := parser.DefaultLimits()
			start := time.Now()
			if kind == "operation-time" {
				limits.OperationTime = time.Second
			}
			o.Budget, _ = parser.NewOperationBudget(limits)
			s := acquire(t, "x.md", o)
			if kind == "total-nodes" {
				_ = s.budget.ChargeNodes(limits.TotalNodes)
			} else {
				time.Sleep(time.Until(start.Add(1100 * time.Millisecond)))
			}
			a, err := Analyze(context.Background(), s, AnalyzeOptions{})
			var cause *parser.LimitExceeded
			if !errors.As(err, &cause) || cause.Name != kind || len(a.Result.Interruptions) != 1 {
				t.Fatalf("typed cause missing: %v", err)
			}
			observation := a.Result.Interruptions[0].Limit
			if observation == nil || observation.Name != cause.Name || observation.Observed != cause.Observed || observation.Allowed != cause.Allowed || observation.Unit != result.LimitUnit(cause.Name) {
				t.Fatal("incorrect units", observation, cause)
			}
			validateProduced(t, a)
		})
	}
}

func TestElapsedCallbackBudgetCannotProveCompleteCoverage(t *testing.T) {
	for _, observed := range []bool{false, true} {
		for _, finding := range []bool{false, true} {
			t.Run(strconv.FormatBool(observed)+"/finding="+strconv.FormatBool(finding), func(t *testing.T) {
				root, o := fixture(t)
				calls := 0
				registry, err := engine.NewCatalogRegistry([]engine.Registration{{ID: "MD001", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					calls++
					if err := c.Checkpoint(ctx, 1); err != nil {
						return engine.RuleOutcome{Err: err}
					}
					// A single finite operation may finish beyond the shared operation clock
					// without a context deadline or a loop that can insert another checkpoint.
					time.Sleep(1100 * time.Millisecond)
					out := engine.RuleOutcome{}
					if finding {
						span, _ := c.Source().Span(0, 0)
						out.Findings = []engine.Finding{{Span: span, Message: "reliable finding after finite operation"}}
					}
					return out
				}}})
				if err != nil {
					t.Fatal(err)
				}
				o.Registry = registry
				write(t, filepath.Join(root, "x.md"), []byte("original\n"))
				definition := "version: 1\nflavor: commonmark\n"
				if observed {
					definition += "rules: {MD001: {enabled: false}}\n"
				}
				write(t, filepath.Join(root, config.Filename), []byte(definition))
				limits := parser.DefaultLimits()
				limits.OperationTime = time.Second
				o.Budget, _ = parser.NewOperationBudget(limits)
				s := acquire(t, "x.md", o)
				ctx := context.Background()
				a, err := Analyze(ctx, s, AnalyzeOptions{ObserveInactive: observed})
				var limit *parser.LimitExceeded
				if !errors.As(err, &limit) || limit.Name != "operation-time" || ctx.Err() != nil || calls != 1 || a.Result.Status != "incomplete" {
					t.Fatalf("elapsed callback interruption: %v calls=%d status=%s", err, calls, a.Result.Status)
				}
				r := rule(t, a, "MD001")
				status, coverage := r.ActiveEvaluation, r.SourceCoverage.Active
				if observed {
					status, coverage = r.ObservationalEvaluation, r.SourceCoverage.Observational
				}
				if status != "partial" || coverage.Availability != "unknown" || len(coverage.ByteRanges) != 0 || len(coverage.Ranges) != 0 {
					t.Fatalf("elapsed callback proves complete evaluation: %+v", r)
				}
				if finding {
					if observed {
						if len(a.Result.SuppressedDiagnostics) != 1 {
							t.Fatal("lost reliable suppressed observation")
						}
					} else if len(a.Result.Diagnostics) != 1 || a.Result.Diagnostics[0].Message != "reliable finding after finite operation" {
						t.Fatal("lost reliable active finding")
					}
				}
				if len(a.Result.Interruptions) != 1 || a.Result.Interruptions[0].Limit == nil || a.Result.Interruptions[0].Limit.Unit != "nanoseconds" {
					t.Fatal("typed elapsed observation missing", a.Result.Interruptions)
				}
				validateProduced(t, a)
			})
		}
	}
}
