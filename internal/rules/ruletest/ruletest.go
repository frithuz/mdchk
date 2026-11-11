// Package ruletest holds the helpers shared by rule tests: registries,
// settings, analysis and correction runs over production registrations.
//
// It is for tests only. Production code must not import it, and it must not
// import internal/rules, so that rule packages and their external test
// packages can both use it.
package ruletest

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
)

// Registry builds a catalog registry from registrations.
func Registry(t testing.TB, registrations []engine.Registration) *engine.Registry {
	t.Helper()
	r, err := engine.NewCatalogRegistry(registrations)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Settings enables only the named rules and disables every other catalog rule.
func Settings(ids ...string) engine.Settings {
	out := engine.Settings{}
	for _, m := range engine.Catalog() {
		v := slices.Contains(ids, m.ID)
		out[m.ID] = engine.RuleSetting{Enabled: &v}
	}
	return out
}

// Analyze lints text as CommonMark doc.md and fails if analysis changed the
// source or requested edits.
func Analyze(t *testing.T, r *engine.Registry, text string, s engine.Settings, opts engine.RunOptions) result.Result {
	t.Helper()
	caller := []byte(text)
	in, err := engine.NewInput("doc.md", caller, s, engine.DocumentContext{Flavor: "commonmark"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Lint(context.Background(), []engine.Input{in}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if string(caller) != text || string(in.Source().Bytes()) != text || len(out.Changes) != 0 || len(out.Documents) != 0 || out.Files[0].WriteStatus != "not-requested" {
		t.Fatal("analysis changed source or requested edits", out)
	}
	return out
}

// CorrectionSetup returns a validator over checkers and a CommonMark plan for
// s, both sharing one registry built from registrations.
func CorrectionSetup(t *testing.T, registrations []engine.Registration, checkers []fix.CheckerRegistration, s engine.Settings) (*fix.Validator, *engine.ExecutionPlan) {
	t.Helper()
	r := Registry(t, registrations)
	selection, err := r.SelectRules(s, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := fix.NewValidator(r, checkers)
	if err != nil {
		t.Fatal(err)
	}
	return validator, plan
}

// Corrected validates producer's corrections of text as doc.md with the
// default limits.
func Corrected(t *testing.T, v *fix.Validator, plan *engine.ExecutionPlan, text string, producer fix.Producer) (fix.FileView, fix.Validated) {
	t.Helper()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	out, err := v.Validate(context.Background(), []fix.Target{{Path: "doc.md", Source: parser.NewSource([]byte(text)), Plan: plan, Produce: producer}}, fix.ValidationOptions{Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	view, ok := out.File("doc.md")
	if !ok {
		t.Fatal("missing file")
	}
	return view, out
}

// AnalyzeDocument lints source as heading.md in dc. It fails if analysis
// changed the source or proposed edits, or if a suppressed finding or a
// report-only rule's finding grants a fix.
func AnalyzeDocument(t *testing.T, r *engine.Registry, source string, s engine.Settings, dc engine.DocumentContext, opts engine.RunOptions) result.Result {
	t.Helper()
	caller := []byte(source)
	in, err := engine.NewInput("heading.md", caller, s, dc)
	if err != nil {
		t.Fatal(err)
	}
	token := in.Source().Token()
	out, err := r.Lint(context.Background(), []engine.Input{in}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if string(caller) != source || string(in.Source().Bytes()) != source || in.Source().Token() != token || len(out.Changes) != 0 || len(out.Documents) != 0 || out.Files[0].WriteStatus != "not-requested" {
		t.Fatal("analysis changed original or proposed edits", out)
	}
	for _, raw := range out.SuppressedDiagnostics {
		var suppressed result.SuppressedDiagnostic
		if err := json.Unmarshal(raw, &suppressed); err != nil || suppressed.Diagnostic.FixAvailable {
			t.Fatal("suppressed heading grants fix", string(raw), err)
		}
	}
	for _, d := range out.Diagnostics {
		if d.FixAvailable && d.Rule != "MD003" && d.Rule != "MD004" && d.Rule != "MD005" && d.Rule != "MD007" && d.Rule != "MD009" && d.Rule != "MD010" && d.Rule != "MD019" && d.Rule != "MD021" && d.Rule != "MD022" && d.Rule != "MD023" && d.Rule != "MD029" && d.Rule != "MD030" && d.Rule != "MD032" && d.Rule != "MD063" && d.Rule != "MD076" && d.Rule != "MD077" {
			t.Fatal("report-only heading grants fix", d)
		}
	}
	return out
}

// LateStopContext counts the production callback's context checks so a test
// can stop after a completed inventory and the first emission, without fixing
// a parser work count. From the Stop-th call on, Err returns Reason.
type LateStopContext struct {
	context.Context
	Calls, Stop int
	Reason      error
}

func (c *LateStopContext) Err() error {
	c.Calls++
	if c.Stop > 0 && c.Calls >= c.Stop {
		return c.Reason
	}
	return c.Context.Err()
}

// CorrectedFlavor validates producer's corrections of text as doc.md in
// flavor with the default limits, with only the named rules enabled.
func CorrectedFlavor(t *testing.T, registrations []engine.Registration, checkers []fix.CheckerRegistration, text, flavor string, producer fix.Producer, ids ...string) (fix.FileView, fix.Validated) {
	t.Helper()
	r := Registry(t, registrations)
	selection, err := r.SelectRules(Settings(ids...), engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: flavor}, nil)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := fix.NewValidator(r, checkers)
	if err != nil {
		t.Fatal(err)
	}
	return Corrected(t, validator, plan, text, producer)
}

// HeadingFacts are the parser and anchor facts of one heading that a
// correction must preserve.
type HeadingFacts struct {
	Level                                   int
	Style, Visible, Label, Containers, Base string
	Closed                                  bool
}

// HeadingFactsOf parses text in flavor and returns the facts of each heading.
func HeadingFactsOf(t *testing.T, text, flavor string) []HeadingFacts {
	t.Helper()
	ctx := context.Background()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	source := parser.NewSource([]byte(text))
	options := engine.DocumentContext{Flavor: flavor}.ParseOptions()
	parsed := parser.ParseWithOptions(ctx, source, budget, options)
	headings, err := parsed.Document.Headings(ctx, budget)
	if err != nil {
		t.Fatal(err)
	}
	anchors, availability, err := resource.OriginalHeadingAnchors(ctx, parsed, budget, parser.NewLineIndex(source), options, "github")
	if err != nil || availability != resource.Available || len(anchors) != len(headings) {
		t.Fatal(err, availability, len(anchors))
	}
	out := make([]HeadingFacts, len(headings))
	for i, h := range headings {
		label, _ := source.Text(h.Label)
		kinds := make([]string, len(h.Containers))
		for j, c := range h.Containers {
			kinds[j] = string(c.Kind)
		}
		out[i] = HeadingFacts{h.Level, h.Style, h.Visible, label, strings.Join(kinds, ","), anchors[i].Base.Value, h.Closed}
	}
	return out
}

// GateStopContext stops at a named function.
//
// Function identities come from held source, not sampled checkpoint counters.
// The context interrupts only a named deferred publication gate; it also proves
// that gate was reached after the actual positive comparison precondition.
type GateStopContext struct {
	context.Context
	Gate    string
	Reason  error
	Reached bool
}

func (c *GateStopContext) Err() error {
	if c.Gate == "" {
		return c.Context.Err()
	}
	var pcs [32]uintptr
	n := runtime.Callers(1, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if strings.HasSuffix(frame.Function, c.Gate) {
			c.Reached = true
			return c.Reason
		}
		if !more {
			break
		}
	}
	return c.Context.Err()
}

// GateQuotaContext exhausts a budget at a named function.
//
// Exhaustion is injected at the actual deferred outer gate after production
// evaluation. It uses the original allowance and no measured setup subtraction.
type GateQuotaContext struct {
	context.Context
	Budget         *parser.OperationBudget
	Kind           string
	Gate           string
	Armed, Reached bool
}

func (c *GateQuotaContext) Err() error {
	if c.Armed {
		var pcs [32]uintptr
		n := runtime.Callers(1, pcs[:])
		frames := runtime.CallersFrames(pcs[:n])
		for {
			f, more := frames.Next()
			gate := c.Gate
			if gate == "" {
				gate = ".shellPrompts.func1"
			}
			if strings.HasSuffix(f.Function, gate) {
				c.Reached = true
				if c.Kind == "elapsed" {
					c.Armed = false
					time.Sleep(61 * time.Second)
				} else if c.Kind == "nodes" {
					_ = c.Budget.ChargeNodes(c.Budget.Limits().TotalNodes)
				} else {
					_ = c.Budget.ChargeWork(c.Budget.Limits().WorkUnits)
				}
				break
			}
			if !more {
				break
			}
		}
	}
	return c.Context.Err()
}

// NativeFragments is a minimal native provider over a temporary root, so
// rule-level tests exercise the real shared resolver rather than a stand-in.
type NativeFragments struct {
	resolver *resource.FragmentResolver
	view     resource.View
	service  *resource.Service
	root     *resource.FilesystemRoot
}

// Fragments returns the resolver and its original view.
func (r *NativeFragments) Fragments(context.Context) (*resource.FragmentResolver, resource.View, error) {
	return r.resolver, r.view, nil
}

// NewNativeFragments writes files and source as docs/self.md under a temporary
// root and returns a provider for flavor bound to targetID.
func NewNativeFragments(t *testing.T, files map[string]string, flavor string, targetID string, source parser.Source) *NativeFragments {
	t.Helper()
	files = maps.Clone(files)
	files["docs/self.md"] = string(source.Bytes())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	ctx := context.Background()
	root, err := resource.OpenFilesystemRoot(ctx, budget, dir, resource.DefaultFilesystemLimits())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := root.BindTarget(filepath.Join(dir, "docs", "self.md"), targetID, source)
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := resource.NewSettings(parser.ParseOptions{GFM: flavor == "gfm"}, "index.md", "github", "test-1")
	operation, err := resource.NewOperation(ctx, budget, root.Root(), settings, []resource.Target{receipt.Target()})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := root.Reader(operation, receipt)
	if err != nil {
		t.Fatal(err)
	}
	service, err := resource.NewFilesystemService(reader, resource.DefaultServiceLimits())
	if err != nil {
		t.Fatal(err)
	}
	view, err := operation.NewView(resource.Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	normalizer, err := resource.NewNormalizer(view, resource.DefaultPathLimits(), resource.NativeNamePolicy)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := resource.NewFragmentResolver(service, normalizer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close(); _ = root.Close() })
	return &NativeFragments{resolver, view, service, root}
}

// Without returns registrations without the named rules, which keeps implicit
// unavailable-rule coverage testable after those rules gained production
// implementations.
func Without(registrations []engine.Registration, ids ...string) []engine.Registration {
	return slices.DeleteFunc(registrations, func(g engine.Registration) bool { return slices.Contains(ids, g.ID) })
}

// OnlyDefault disables every catalog rule except id, which keeps its default
// setting.
func OnlyDefault(id string) engine.Settings {
	s := engine.Settings{}
	off := false
	for _, m := range engine.Catalog() {
		s[m.ID] = engine.RuleSetting{Enabled: &off}
	}
	delete(s, id)
	return s
}

// OriginalPosition is an independent scalar/physical-ending count over
// original bytes, never parser normalized Literal or the product's line-index
// conversion.
func OriginalPosition(source string, at int) result.Position {
	p := result.Position{Line: 1, Column: 1}
	for i := 0; i < at; {
		if source[i] == '\r' {
			p.Line++
			p.Column = 1
			i++
			if i < at && source[i] == '\n' {
				i++
			}
			continue
		}
		if source[i] == '\n' {
			p.Line++
			p.Column = 1
			i++
			continue
		}
		_, w := utf8.DecodeRuneInString(source[i:])
		i += w
		p.Column++
	}
	return p
}

// OracleFinding is one expected diagnostic from a fixture oracle.
type OracleFinding struct {
	Rule, Message, Marker string
	ByteRange             result.ByteRange `json:"byte_range"`
	Range                 result.Range
}

// AuditDecision is only a test expectation for detached original facts.
type AuditDecision struct {
	Owner, Effect int
	Rule, Reason  string
	Token         result.ByteRange
	Interval      struct {
		Start, End int
		EOF        bool `json:"includes_final_eof"`
	}
}

// EqualAuditDecisions reports whether a and b hold the same decisions in order.
func EqualAuditDecisions(a, b []AuditDecision) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// AssertRawDecisions checks got against want and returns got as decisions.
// Every raw accessor row is checked before any named requested projection.
func AssertRawDecisions(t *testing.T, source parser.Source, got []engine.SuppressionDecision, want []AuditDecision) []AuditDecision {
	t.Helper()
	if len(got) != len(want) {
		t.Fatal("complete raw decision inventory", got, want)
	}
	out := make([]AuditDecision, 0, len(got))
	for i, d := range got {
		w := want[i]
		if d.Owner != w.Owner || d.Effect != w.Effect || d.Rule != w.Rule || d.Reason != w.Reason || d.Token.SourceToken() != source.Token() || d.Token.Start() != w.Token.Start || d.Token.End() != w.Token.End || d.Token.Start() < 0 || d.Token.End() <= d.Token.Start() || d.Token.End() > source.Len() || d.Interval.Start != w.Interval.Start || d.Interval.End != w.Interval.End || d.Interval.Start < 0 || d.Interval.End < d.Interval.Start || d.Interval.End > source.Len() || d.Interval.Enabled || d.Interval.Owner != w.Owner || d.Interval.Token != d.Token || (d.Interval.End == source.Len()) != w.Interval.EOF {
			t.Fatal("raw original decision", d, w)
		}
		text, err := source.Text(d.Token)
		if err != nil || text != d.Rule {
			t.Fatal("original decision token spelling", text, err, d)
		}
		actual := AuditDecision{Owner: d.Owner, Effect: d.Effect, Rule: d.Rule, Reason: d.Reason, Token: result.ByteRange{Start: d.Token.Start(), End: d.Token.End()}}
		actual.Interval.Start, actual.Interval.End, actual.Interval.EOF = d.Interval.Start, d.Interval.End, d.Interval.End == source.Len()
		out = append(out, actual)
	}
	return out
}

// AssertOracleDiagnostic checks one report-only original diagnostic against want.
func AssertOracleDiagnostic(t *testing.T, source string, got result.Diagnostic, want OracleFinding) {
	t.Helper()
	if got.Rule != want.Rule || got.Severity != "error" || got.Message != want.Message || got.ByteRange != want.ByteRange || got.Range != want.Range || got.FixAvailable || got.Snapshot != (result.Snapshot{View: "original", SizeBytes: len(source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}) {
		t.Fatal(got, want)
	}
	if want.ByteRange.Start < 0 || want.ByteRange.End > len(source) || want.ByteRange.End <= want.ByteRange.Start || source[want.ByteRange.Start:want.ByteRange.End] != want.Marker {
		t.Fatal("invalid oracle range", want)
	}
}
