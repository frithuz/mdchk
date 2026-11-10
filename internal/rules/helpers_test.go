package rules

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

func registry(t testing.TB, registrations []engine.Registration) *engine.Registry {
	t.Helper()
	return ruletest.Registry(t, registrations)
}

// settings enables only MD047 and disables every other catalog rule.
func settings() engine.Settings { return ruletest.Settings("MD047") }

func analyzeHeadings(t *testing.T, r *engine.Registry, source string, s engine.Settings, dc engine.DocumentContext, opts engine.RunOptions) result.Result {
	t.Helper()
	return ruletest.AnalyzeDocument(t, r, source, s, dc, opts)
}

// spacingSettings enables only the named rules.
func spacingSettings(ids ...string) engine.Settings { return ruletest.Settings(ids...) }

type (
	gateStopContext  = ruletest.GateStopContext
	gateQuotaContext = ruletest.GateQuotaContext
)

// registrationsWithout keeps implicit unavailable-rule coverage testable after
// the named rules gained production implementations.
func registrationsWithout(ids ...string) []engine.Registration {
	return slices.DeleteFunc(Registrations(), func(g engine.Registration) bool { return slices.Contains(ids, g.ID) })
}

func onlyMD092Settings() engine.Settings {
	s := engine.Settings{}
	off, on := false, true
	for _, m := range engine.Catalog() {
		s[m.ID] = engine.RuleSetting{Enabled: &off}
	}
	s["MD092"] = engine.RuleSetting{Enabled: &on}
	return s
}

func onlyMD014Settings() engine.Settings {
	s := onlyMD092Settings()
	off, on := false, true
	s["MD092"] = engine.RuleSetting{Enabled: &off}
	s["MD014"] = engine.RuleSetting{Enabled: &on}
	return s
}

func onlyMD031Settings() engine.Settings {
	s := onlyMD014Settings()
	off, on := false, true
	s["MD014"] = engine.RuleSetting{Enabled: &off}
	s["MD031"] = engine.RuleSetting{Enabled: &on}
	return s
}

func onlyMD040Settings() engine.Settings {
	s := onlyMD031Settings()
	off, on := false, true
	s["MD031"] = engine.RuleSetting{Enabled: &off}
	s["MD040"] = engine.RuleSetting{Enabled: &on}
	return s
}

func onlyMD046Settings() engine.Settings {
	s := onlyMD040Settings()
	off, on := false, true
	s["MD040"] = engine.RuleSetting{Enabled: &off}
	s["MD046"] = engine.RuleSetting{Enabled: &on}
	return s
}

func onlyMD048Settings() engine.Settings {
	s := onlyMD046Settings()
	off, on := false, true
	s["MD046"] = engine.RuleSetting{Enabled: &off}
	s["MD048"] = engine.RuleSetting{Enabled: &on}
	return s
}

func onlyMD011Settings() engine.Settings {
	s := onlyMD048Settings()
	off, on := false, true
	s["MD048"] = engine.RuleSetting{Enabled: &off}
	s["MD011"] = engine.RuleSetting{Enabled: &on}
	return s
}

func onlyMD059Settings() engine.Settings {
	s := onlyMD011Settings()
	off, on := false, true
	s["MD011"] = engine.RuleSetting{Enabled: &off}
	s["MD034"] = engine.RuleSetting{Enabled: &off}
	s["MD059"] = engine.RuleSetting{Enabled: &on}
	return s
}

func onlyMD055Settings() engine.Settings {
	s := onlyMD059Settings()
	off, on := false, true
	s["MD059"] = engine.RuleSetting{Enabled: &off}
	s["MD055"] = engine.RuleSetting{Enabled: &on}
	return s
}

func onlyMD056Settings() engine.Settings {
	s := onlyMD055Settings()
	off, on := false, true
	s["MD055"] = engine.RuleSetting{Enabled: &off}
	s["MD056"] = engine.RuleSetting{Enabled: &on}
	return s
}

func onlyDefaultMD037Settings() engine.Settings {
	s := engine.Settings{}
	off := false
	for _, m := range engine.Catalog() {
		s[m.ID] = engine.RuleSetting{Enabled: &off}
	}
	delete(s, "MD037")
	return s
}

func onlyDefaultMD038Settings() engine.Settings {
	s := engine.Settings{}
	off := false
	for _, m := range engine.Catalog() {
		s[m.ID] = engine.RuleSetting{Enabled: &off}
	}
	delete(s, "MD038")
	return s
}

// The callback gets the engine's real immutable context and residual budget.
// Interrupt after it has entered the suffix traversal, rather than cancelling
// before acquisition and accidentally testing only the engine's early gate.
type traversalContext struct {
	context.Context
	calls int
	err   error
}

func (c *traversalContext) Err() error {
	c.calls++
	if c.calls >= 4 {
		return c.err
	}
	return nil
}

// Count the actual production callback's context checks so the stop follows a
// completed inventory and the first emission, without fixing a parser work count.
type lateStopContext struct {
	context.Context
	calls, stop int
	reason      error
}

func (c *lateStopContext) Err() error {
	c.calls++
	if c.stop > 0 && c.calls >= c.stop {
		return c.reason
	}
	return c.Context.Err()
}

type stopAtContext struct {
	context.Context
	calls, stop int
	reason      error
}

func (c *stopAtContext) Err() error {
	c.calls++
	if c.calls >= c.stop {
		return c.reason
	}
	return c.Context.Err()
}

// Independent scalar/physical-ending count over original bytes, never parser
// normalized Literal or the product's line-index conversion.
func originalPosition(source string, at int) result.Position {
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

type oracleFinding struct {
	Rule, Message, Marker string
	ByteRange             result.ByteRange `json:"byte_range"`
	Range                 result.Range
}

// auditDecision is only a test expectation for detached original facts.
type auditDecision struct {
	Owner, Effect int
	Rule, Reason  string
	Token         result.ByteRange
	Interval      struct {
		Start, End int
		EOF        bool `json:"includes_final_eof"`
	}
}

func equalAuditDecisions(a, b []auditDecision) bool {
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

// Every raw accessor row is checked before any named requested projection.
func assertRawDecisions(t *testing.T, source parser.Source, got []engine.SuppressionDecision, want []auditDecision) []auditDecision {
	t.Helper()
	if len(got) != len(want) {
		t.Fatal("complete raw decision inventory", got, want)
	}
	out := make([]auditDecision, 0, len(got))
	for i, d := range got {
		w := want[i]
		if d.Owner != w.Owner || d.Effect != w.Effect || d.Rule != w.Rule || d.Reason != w.Reason || d.Token.SourceToken() != source.Token() || d.Token.Start() != w.Token.Start || d.Token.End() != w.Token.End || d.Token.Start() < 0 || d.Token.End() <= d.Token.Start() || d.Token.End() > source.Len() || d.Interval.Start != w.Interval.Start || d.Interval.End != w.Interval.End || d.Interval.Start < 0 || d.Interval.End < d.Interval.Start || d.Interval.End > source.Len() || d.Interval.Enabled || d.Interval.Owner != w.Owner || d.Interval.Token != d.Token || (d.Interval.End == source.Len()) != w.Interval.EOF {
			t.Fatal("raw original decision", d, w)
		}
		text, err := source.Text(d.Token)
		if err != nil || text != d.Rule {
			t.Fatal("original decision token spelling", text, err, d)
		}
		actual := auditDecision{Owner: d.Owner, Effect: d.Effect, Rule: d.Rule, Reason: d.Reason, Token: result.ByteRange{Start: d.Token.Start(), End: d.Token.End()}}
		actual.Interval.Start, actual.Interval.End, actual.Interval.EOF = d.Interval.Start, d.Interval.End, d.Interval.End == source.Len()
		out = append(out, actual)
	}
	return out
}

func assertOracleDiagnostic(t *testing.T, source string, got result.Diagnostic, want oracleFinding) {
	t.Helper()
	if got.Rule != want.Rule || got.Severity != "error" || got.Message != want.Message || got.ByteRange != want.ByteRange || got.Range != want.Range || got.FixAvailable || got.Snapshot != (result.Snapshot{View: "original", SizeBytes: len(source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}) {
		t.Fatal(got, want)
	}
	if want.ByteRange.Start < 0 || want.ByteRange.End > len(source) || want.ByteRange.End <= want.ByteRange.Start || source[want.ByteRange.Start:want.ByteRange.End] != want.Marker {
		t.Fatal("invalid oracle range", want)
	}
}
