package md092_test

import (
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules/md092"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

func registry(t testing.TB, registrations []engine.Registration) *engine.Registry {
	t.Helper()
	return ruletest.Registry(t, registrations)
}

func analyzeHeadings(t *testing.T, r *engine.Registry, source string, s engine.Settings, dc engine.DocumentContext, opts engine.RunOptions) result.Result {
	t.Helper()
	return ruletest.AnalyzeDocument(t, r, source, s, dc, opts)
}

func onlyMD092Settings() engine.Settings { return ruletest.Settings("MD092") }

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

var (
	compareConflictMarkers = md092.CompareConflictMarkers
	conflictMarkers        = md092.ConflictMarkers
)
