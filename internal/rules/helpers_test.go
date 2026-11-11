package rules

import (
	"context"
	"testing"

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
	return ruletest.Without(Registrations(), ids...)
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

func onlyDefaultMD038Settings() engine.Settings { return ruletest.OnlyDefault("MD038") }

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

func originalPosition(source string, at int) result.Position { return ruletest.OriginalPosition(source, at) }

type (
	oracleFinding = ruletest.OracleFinding
	auditDecision = ruletest.AuditDecision
)

func equalAuditDecisions(a, b []auditDecision) bool { return ruletest.EqualAuditDecisions(a, b) }

func assertRawDecisions(t *testing.T, source parser.Source, got []engine.SuppressionDecision, want []auditDecision) []auditDecision {
	t.Helper()
	return ruletest.AssertRawDecisions(t, source, got, want)
}

func assertOracleDiagnostic(t *testing.T, source string, got result.Diagnostic, want oracleFinding) {
	t.Helper()
	ruletest.AssertOracleDiagnostic(t, source, got, want)
}
